package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	httpware "github.com/Bengo-Hub/httpware"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/attachments"
	"github.com/bengobox/projects-service/internal/services/comments"
	"github.com/bengobox/projects-service/internal/services/tasks"
	"github.com/bengobox/projects-service/internal/services/tenders"
	"github.com/bengobox/projects-service/internal/testutil"
)

type apiTest struct {
	t      *testing.T
	h      http.Handler
	tenant uuid.UUID
}

func (a apiTest) do(method, path string, body any) (int, map[string]any) {
	a.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, "/api/v1/acme"+path, &buf)
	req.Header.Set("X-Tenant-ID", a.tenant.String())
	w := httptest.NewRecorder()
	a.h.ServeHTTP(w, req)
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func newAPI(t *testing.T, client *ent.Client) apiTest {
	log := zap.NewNop()
	rec := activity.NewRecorder(client, log)
	r := chi.NewRouter()
	r.Use(httpware.Tenant)
	r.Route("/api/v1/{tenantID}", func(tr chi.Router) {
		NewTaskHandler(log, tasks.NewService(client, nil, log)).WithActivity(rec).RegisterRoutes(tr)
		NewCommentHandler(log, comments.NewService(client, nil, log)).WithActivity(rec).RegisterRoutes(tr)
		NewActivityHandler(log, client).RegisterRoutes(tr)
		NewAttachmentHandler(log, attachments.NewService(client, log), rec).RegisterRoutes(tr)
		NewTenderHandler(log, tenders.NewService(client, nil, log)).RegisterRoutes(tr)
	})
	return apiTest{t: t, h: r, tenant: uuid.New()}
}

// TestCollaborationAPI_DB drives the routes projects-ui's task detail page uses and checks the
// activity feed records each change.
func TestCollaborationAPI_DB(t *testing.T) {
	client := testutil.PGClient(t)
	api := newAPI(t, client)
	p := client.Project.Create().SetTenantID(api.tenant).SetName("P").SetOwnerID(uuid.New()).SaveX(t.Context())
	base := "/projects/" + p.ID.String()

	code, task := api.do(http.MethodPost, base+"/tasks", map[string]any{"title": "Site survey"})
	if code != http.StatusCreated {
		t.Fatalf("create task: %d %v", code, task)
	}
	taskPath := base + "/tasks/" + task["id"].(string)

	// limit is honoured on the task list (the UI asks for 100).
	for i := 0; i < 2; i++ {
		api.do(http.MethodPost, base+"/tasks", map[string]any{"title": "extra"})
	}
	if _, list := api.do(http.MethodGet, base+"/tasks?limit=2", nil); len(list["data"].([]any)) != 2 || list["total"].(float64) != 3 {
		t.Fatalf("limit=2 list = %v", list)
	}

	if code, _ := api.do(http.MethodPut, taskPath, map[string]any{"status": "in_progress", "progress_pct": 50}); code != http.StatusOK {
		t.Fatalf("update task: %d", code)
	}
	if code, body := api.do(http.MethodPost, taskPath+"/comments", map[string]any{"content": "Starting Monday"}); code != http.StatusCreated {
		t.Fatalf("task comment: %d %v", code, body)
	}
	if code, list := api.do(http.MethodGet, taskPath+"/comments", nil); code != http.StatusOK || len(list["data"].([]any)) != 1 {
		t.Fatalf("list task comments: %d %v", code, list)
	}
	if code, _ := api.do(http.MethodPost, base+"/tasks/"+uuid.NewString()+"/comments", map[string]any{"content": "x"}); code != http.StatusNotFound {
		t.Fatalf("comment on a missing task: %d", code)
	}
	code, att := api.do(http.MethodPost, taskPath+"/attachments", map[string]any{"file_url": "https://drive.example.com/survey.pdf"})
	if code != http.StatusCreated || att["file_name"] != "survey.pdf" {
		t.Fatalf("attach: %d %v", code, att)
	}
	if code, _ := api.do(http.MethodPost, taskPath+"/attachments", map[string]any{"file_url": "javascript:alert(1)"}); code != http.StatusBadRequest {
		t.Fatalf("bad link: %d", code)
	}
	if code, _ := api.do(http.MethodDelete, base+"/attachments/"+att["id"].(string), nil); code != http.StatusNoContent {
		t.Fatalf("delete attachment: %d", code)
	}

	code, feed := api.do(http.MethodGet, taskPath+"/activities", nil)
	if code != http.StatusOK {
		t.Fatalf("activities: %d", code)
	}
	types := map[string]map[string]any{}
	for _, it := range feed["data"].([]any) {
		a := it.(map[string]any)
		types[a["activity_type"].(string)] = a
	}
	for _, want := range []string{"task.created", "task.updated", "comment.added", "attachment.added", "attachment.removed"} {
		if types[want] == nil {
			t.Errorf("activity %s missing; got %v", want, types)
		}
	}
	if upd := types["task.updated"]; upd != nil {
		ch := upd["payload"].(map[string]any)["changes"].(map[string]any)
		if ch["status"].(map[string]any)["to"] != "in_progress" || ch["progress_pct"] == nil {
			t.Errorf("update changes = %v", ch)
		}
	}
	if c := types["comment.added"]; c != nil && c["payload"].(map[string]any)["title"] != "Site survey" {
		t.Errorf("comment activity payload = %v", c["payload"])
	}

	// Deleting the task leaves a project-level entry.
	if code, _ := api.do(http.MethodDelete, taskPath, nil); code != http.StatusNoContent {
		t.Fatalf("delete task: %d", code)
	}
	_, pf := api.do(http.MethodGet, base+"/activities", nil)
	found := false
	for _, it := range pf["data"].([]any) {
		a := it.(map[string]any)
		if a["activity_type"] == "task.deleted" && a["payload"].(map[string]any)["title"] == "Site survey" {
			found = true
		}
	}
	if !found {
		t.Error("task.deleted not in the project feed")
	}
}

// TestTenderWorkflowAPI_DB checks the HTTP status codes of the tender workflow routes.
func TestTenderWorkflowAPI_DB(t *testing.T) {
	client := testutil.PGClient(t)
	api := newAPI(t, client)
	code, tender := api.do(http.MethodPost, "/tenders", map[string]any{"title": "Bridge", "client_name": "KeRRA"})
	if code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, tender)
	}
	base := "/tenders/" + tender["id"].(string)
	if code, _ := api.do(http.MethodPost, base+"/submit", map[string]any{"method": "online"}); code != http.StatusConflict {
		t.Fatalf("submit before decision: %d", code)
	}
	if code, _ := api.do(http.MethodPost, base+"/decision", map[string]any{"decision": "maybe", "rationale": "x"}); code != http.StatusBadRequest {
		t.Fatalf("bad decision: %d", code)
	}
	if code, body := api.do(http.MethodPost, base+"/decision", map[string]any{"decision": "go", "rationale": "fits"}); code != http.StatusOK || body["status"] != "preparing" {
		t.Fatalf("decision: %d %v", code, body)
	}
	code, sec := api.do(http.MethodPost, base+"/sections", map[string]any{"title": "Method statement"})
	if code != http.StatusCreated {
		t.Fatalf("section: %d %v", code, sec)
	}
	secPath := base + "/sections/" + sec["id"].(string)
	if code, _ := api.do(http.MethodPost, secPath+"/approve", nil); code != http.StatusConflict {
		t.Fatalf("approve before submit: %d", code)
	}
	if code, _ := api.do(http.MethodPost, secPath+"/submit", map[string]any{"document_url": "https://d.example.com/m.docx"}); code != http.StatusOK {
		t.Fatalf("submit section: %d", code)
	}
	if code, _ := api.do(http.MethodPost, secPath+"/approve", nil); code != http.StatusOK {
		t.Fatalf("approve: %d", code)
	}
	if code, list := api.do(http.MethodGet, base+"/sections", nil); code != http.StatusOK || list["summary"].(map[string]any)["all_approved"] != true {
		t.Fatalf("sections: %d %v", code, list)
	}
	if code, _ := api.do(http.MethodPost, base+"/ready", nil); code != http.StatusBadRequest {
		t.Fatalf("ready without final document: %d", code)
	}
	if code, _ := api.do(http.MethodPost, base+"/final-document", map[string]any{"file_url": "https://d.example.com/final.pdf"}); code != http.StatusCreated {
		t.Fatalf("final document: %d", code)
	}
	if code, body := api.do(http.MethodPost, base+"/ready", map[string]any{"ready": true}); code != http.StatusOK || body["ready_for_submission"] != true {
		t.Fatalf("ready: %d %v", code, body)
	}
	if code, _ := api.do(http.MethodPost, base+"/submit", map[string]any{"method": "physical"}); code != http.StatusBadRequest {
		t.Fatalf("physical without address: %d", code)
	}
	if code, _ := api.do(http.MethodPost, base+"/submit", map[string]any{"method": "physical", "address": "KeRRA HQ", "courier": "G4S"}); code != http.StatusCreated {
		t.Fatalf("submit: %d", code)
	}
	if code, body := api.do(http.MethodPost, base+"/outcome", map[string]any{"outcome": "lost", "loss_reason": "price"}); code != http.StatusOK || body["status"] != "lost" {
		t.Fatalf("outcome: %d %v", code, body)
	}
	if code, _ := api.do(http.MethodPost, base+"/status", map[string]any{"status": "awarded"}); code != http.StatusConflict {
		t.Fatalf("lost -> awarded: %d", code)
	}
	if code, got := api.do(http.MethodGet, base, nil); code != http.StatusOK || len(got["edges"].(map[string]any)["submissions"].([]any)) != 1 {
		t.Fatalf("get tender: %d %v", code, got["edges"])
	}
	if code, _ := api.do(http.MethodGet, "/tenders/"+uuid.NewString()+"/sections", nil); code != http.StatusNotFound {
		t.Fatalf("sections of a missing tender: %d", code)
	}
	if code, _ := api.do(http.MethodDelete, base, nil); code != http.StatusNoContent {
		t.Fatalf("delete tender with children: %d", code)
	}
}
