package comments

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	entoutbox "github.com/bengobox/projects-service/internal/ent/outboxevent"
	"github.com/bengobox/projects-service/internal/services/notify"
	"github.com/bengobox/projects-service/internal/testutil"
)

func mention(id uuid.UUID, label string) string {
	return `<span data-type="mention" data-id="` + id.String() + `" data-label="` + label + `">@` + label + `</span>`
}

// A mention notifies each mentioned project member once, never the author or a non-member; the
// body is stored sanitised; an edit notifies only people it newly mentions.
func TestTaskCommentMentions(t *testing.T) {
	client := testutil.Client(t)
	ctx := notify.WithOrgSlug(context.Background(), "acme")
	tenant, author, ana, ben, stranger := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	p := client.Project.Create().SetTenantID(tenant).SetName("Bridge").SetOwnerID(author).SaveX(ctx)
	for _, u := range []uuid.UUID{ana, ben} {
		client.ProjectMember.Create().SetTenantID(tenant).SetProjectID(p.ID).SetUserID(u).SetRoleCode("member").SaveX(ctx)
	}
	client.ProjectsUser.Create().SetTenantID(tenant).SetAuthServiceUserID(ana).SetEmail("ana@acme.test").SetName("Ana").SaveX(ctx)
	task := client.Task.Create().SetTenantID(tenant).SetProjectID(p.ID).SetTitle("Pour slab").SaveX(ctx)
	svc := NewService(client, nil, zap.NewNop())

	body := `<p>` + mention(ana, "Ana") + ` ` + mention(ana, "Ana") + ` ` + mention(author, "Me") + ` ` +
		mention(stranger, "X") + ` please check<script>alert(1)</script></p>`
	c, _, err := svc.CreateTaskComment(ctx, tenant, p.ID, task.ID, CreateCommentInput{UserID: author, Content: body})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(c.Content, "script") {
		t.Errorf("stored body not sanitised: %s", c.Content)
	}
	events := mentionEvents(t, client.OutboxEvent.Query().Where(entoutbox.EventType(notify.EventTaskMentioned)).AllX(ctx))
	if len(events) != 1 {
		t.Fatalf("mention events = %v, want one for ana", events)
	}
	e := events[0]
	if e["recipient_user_id"] != ana.String() || e["recipient_email"] != "ana@acme.test" || e["task_title"] != "Pour slab" ||
		e["action_path"] != "/acme/projects/"+p.ID.String()+"/tasks?task="+task.ID.String() ||
		!strings.Contains(e["comment_excerpt"].(string), "please check") {
		t.Errorf("payload = %v", e)
	}

	// Editing to add ben notifies ben only.
	if _, err := svc.UpdateComment(ctx, tenant, p.ID, c.ID, UpdateCommentInput{Content: `<p>` + mention(ana, "Ana") + mention(ben, "Ben") + `</p>`}, nil); err != nil {
		t.Fatal(err)
	}
	events = mentionEvents(t, client.OutboxEvent.Query().Where(entoutbox.EventType(notify.EventTaskMentioned)).AllX(ctx))
	bens := 0
	for _, e := range events {
		if e["recipient_user_id"] == ben.String() {
			bens++
		}
	}
	if len(events) != 2 || bens != 1 {
		t.Errorf("after the edit: %v, want one more event, for ben", events)
	}
	if _, _, err := svc.CreateTaskComment(ctx, tenant, p.ID, task.ID, CreateCommentInput{UserID: author, Content: "<p></p>"}); err == nil {
		t.Error("an empty body must be refused")
	}
}

func mentionEvents(t *testing.T, rows []*ent.OutboxEvent) []map[string]any {
	t.Helper()
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		var env struct {
			Payload map[string]any `json:"payload"`
		}
		if err := json.Unmarshal(r.Payload, &env); err != nil {
			t.Fatal(err)
		}
		out = append(out, env.Payload)
	}
	return out
}
