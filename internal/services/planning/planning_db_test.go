package planning

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	entoutbox "github.com/bengobox/projects-service/internal/ent/outboxevent"
	enttask "github.com/bengobox/projects-service/internal/ent/task"
	enttaskdep "github.com/bengobox/projects-service/internal/ent/taskdependency"
	"github.com/bengobox/projects-service/internal/platform/treasury"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/tasks"
	"github.com/bengobox/projects-service/internal/testutil"
)

type fx struct {
	client          *ent.Client
	svc             *Service
	tasks           *tasks.Service
	tenant          uuid.UUID
	owner, ana, ben uuid.UUID
	project         *ent.Project
	ctx             context.Context // as owner
}

func newFx(t *testing.T, tc *treasury.Client) *fx {
	t.Helper()
	client := testutil.Client(t)
	f := &fx{client: client, tenant: uuid.New(), owner: uuid.New(), ana: uuid.New(), ben: uuid.New()}
	f.tasks = tasks.NewService(client, nil, zap.NewNop())
	f.svc = NewService(client, f.tasks, tc, zap.NewNop())
	ctx := context.Background()
	f.project = client.Project.Create().SetTenantID(f.tenant).SetName("Bridge").SetOwnerID(f.owner).SetStartDate(d("2026-11-02")).SaveX(ctx)
	for _, u := range []uuid.UUID{f.ana, f.ben} {
		client.ProjectMember.Create().SetTenantID(f.tenant).SetProjectID(f.project.ID).SetUserID(u).SetRoleCode("member").SaveX(ctx)
	}
	client.ProjectsUser.Create().SetTenantID(f.tenant).SetAuthServiceUserID(f.ana).SetEmail("ana@acme.test").SetName("Ana").SaveX(ctx)
	f.ctx = activity.WithActor(ctx, f.owner)
	return f
}

func (f *fx) task(t *testing.T, title, start, due string, est float64, assignees ...uuid.UUID) *ent.Task {
	t.Helper()
	in := tasks.CreateTaskInput{Title: title, EstimatedHours: &est, AssigneeIDs: assignees}
	if start != "" {
		in.StartDate = &tasks.Date{Time: d(start)}
	}
	if due != "" {
		in.DueDate = &tasks.Date{Time: d(due)}
	}
	tk, err := f.tasks.CreateTask(f.ctx, f.tenant, f.project.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func (f *fx) events(t *testing.T, eventType string) []map[string]any {
	t.Helper()
	rows := f.client.OutboxEvent.Query().Where(entoutbox.EventType(eventType)).AllX(context.Background())
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		var env struct {
			Payload map[string]any `json:"payload"`
		}
		_ = json.Unmarshal(r.Payload, &env)
		out = append(out, env.Payload)
	}
	return out
}

// Demand is spread over working days, split between assignees and grouped per ISO week in SQL;
// weekend-only work spreads over its days; done work and invisible projects are left out.
func TestWorkloadSQL(t *testing.T) {
	f := newFx(t, nil)
	f.task(t, "A", "2026-11-16", "2026-11-20", 10, f.ana, f.ben) // 5h each, week of 16 Nov
	f.task(t, "B", "2026-11-19", "2026-11-24", 8, f.ana)         // Thu Fri Mon Tue: 4h + 4h
	f.task(t, "C", "", "2026-11-21", 6)                          // a Saturday, unassigned
	done := f.task(t, "D", "2026-11-16", "2026-11-16", 50, f.ana)
	if _, _, err := f.tasks.ChangeStatus(f.ctx, f.tenant, f.project.ID, done.ID, "done", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SetWorkloadSettings(f.ctx, f.tenant, WorkloadSettings{WeeklyHours: 40, PersonHours: map[uuid.UUID]float64{f.ana: 8}}); err != nil {
		t.Fatal(err)
	}
	pid := f.project.ID
	w, err := f.svc.Workload(f.ctx, f.tenant, WorkloadQuery{From: d("2026-11-18"), Weeks: 2, ProjectID: &pid})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[uuid.UUID]PersonLoad{}
	for _, p := range w.People {
		byID[p.UserID] = p
	}
	ana, ben := byID[f.ana], byID[f.ben]
	if ana.Weeks[0].Demand != 9 || ana.Weeks[1].Demand != 4 || !ana.Weeks[0].Overloaded || ana.Name != "Ana" || ana.WeeklyCapacity != 8 {
		t.Fatalf("ana: %+v", ana)
	}
	if ben.Weeks[0].Demand != 5 || ben.Weeks[0].Overloaded || ben.WeeklyCapacity != 40 {
		t.Fatalf("ben: %+v", ben)
	}
	if _, ok := byID[f.owner]; !ok || len(w.People) != 3 {
		t.Fatalf("the team (owner included) is listed: %d", len(w.People))
	}
	if len(w.Unassigned) != 1 || w.Unassigned[0].Demand != 6 {
		t.Fatalf("unassigned: %+v", w.Unassigned)
	}
	cell, err := f.svc.WorkloadTasks(f.ctx, f.tenant, &f.ana, d("2026-11-18"), nil, false)
	if err != nil || len(cell) != 2 || cell[0].Title != "A" || cell[0].Hours != 5 || cell[1].Hours != 4 {
		t.Fatalf("cell: %+v %v", cell, err)
	}
	stranger := uuid.New()
	w, err = f.svc.Workload(f.ctx, f.tenant, WorkloadQuery{From: d("2026-11-18"), Weeks: 2, VisibleTo: &stranger})
	if err != nil || len(w.People) != 0 || len(w.Unassigned) != 0 {
		t.Fatalf("a stranger sees demand: %+v %v", w, err)
	}
}

// A baseline keeps the plan; the slips report and the Gantt view compare against it.
func TestBaselinesAndSlips(t *testing.T) {
	f := newFx(t, nil)
	a := f.task(t, "A", "2026-11-02", "2026-11-10", 10)
	f.task(t, "B", "", "2026-11-20", 4)
	b, err := f.svc.SaveBaseline(f.ctx, f.tenant, f.project.ID, "", "kick-off plan")
	if err != nil || b.TaskCount != 2 {
		t.Fatalf("save: %+v %v", b, err)
	}
	if _, err := f.tasks.Reschedule(f.ctx, f.tenant, f.project.ID, a.ID, tasks.RescheduleInput{DueDate: &tasks.Date{Time: d("2026-11-13")}}); err != nil {
		t.Fatal(err)
	}
	f.task(t, "C", "", "2026-11-25", 1)
	rep, err := f.svc.Slips(f.ctx, f.tenant, f.project.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Slips) != 1 || rep.Slips[0].ID != a.ID || rep.Slips[0].FinishSlipDays != 3 || rep.Added != 1 || rep.Compared != 2 {
		t.Fatalf("slips: %+v", rep)
	}
	g, err := f.svc.ForGantt(f.ctx, f.tenant, f.project.ID, nil)
	if err != nil || g.ID != b.ID || g.Tasks[a.ID].DueDate.Format(time.DateOnly) != "2026-11-10" {
		t.Fatalf("gantt baseline: %+v %v", g, err)
	}
	if list, _ := f.svc.ListBaselines(f.ctx, f.tenant, f.project.ID); len(list) != 1 || list[0].Note != "kick-off plan" {
		t.Fatalf("list: %+v", list)
	}
	if err := f.svc.DeleteBaseline(f.ctx, f.tenant, f.project.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if g, err := f.svc.ForGantt(f.ctx, f.tenant, f.project.ID, nil); err != nil || g != nil {
		t.Fatalf("no baseline: %v %v", g, err)
	}
	if _, err := f.svc.Slips(f.ctx, f.tenant, f.project.ID, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("slips without a baseline: %v", err)
	}
	if len(f.events(t, EventBaselineSaved)) != 1 || len(f.events(t, EventBaselineDeleted)) != 1 {
		t.Fatal("baseline events")
	}
}

// The register validates its scale and owner, rescored on change; the heat map groups in SQL.
func TestRisks(t *testing.T) {
	f := newFx(t, nil)
	i := func(v int) *int { return &v }
	s := func(v string) *string { return &v }
	if _, err := f.svc.CreateRisk(f.ctx, f.tenant, f.project.ID, RiskInput{Title: s("Rain"), Likelihood: i(6), Impact: i(2)}); err == nil {
		t.Fatal("likelihood 6 accepted")
	}
	if _, err := f.svc.CreateRisk(f.ctx, f.tenant, f.project.ID, RiskInput{Title: s("Rain"), Likelihood: i(2), Impact: i(2), OwnerID: s(uuid.NewString())}); err == nil {
		t.Fatal("a stranger owner accepted")
	}
	r, err := f.svc.CreateRisk(f.ctx, f.tenant, f.project.ID, RiskInput{Title: s("Rain"), Likelihood: i(3), Impact: i(4), OwnerID: s(f.ana.String()), DueDate: s("2026-12-01")})
	if err != nil || r.Score != 12 || r.OwnerName != "Ana" {
		t.Fatalf("create: %+v %v", r, err)
	}
	f.svc.CreateRisk(f.ctx, f.tenant, f.project.ID, RiskInput{Title: s("Strike"), Likelihood: i(3), Impact: i(4)})
	closed, _ := f.svc.CreateRisk(f.ctx, f.tenant, f.project.ID, RiskInput{Title: s("Old"), Likelihood: i(1), Impact: i(1)})
	if r, err = f.svc.UpdateRisk(f.ctx, f.tenant, f.project.ID, r.ID, RiskInput{Impact: i(5), OwnerID: s("")}); err != nil || r.Score != 15 || r.OwnerID != nil {
		t.Fatalf("update: %+v %v", r, err)
	}
	if _, err := f.svc.UpdateRisk(f.ctx, f.tenant, f.project.ID, closed.ID, RiskInput{Status: s("closed")}); err != nil {
		t.Fatal(err)
	}
	h, err := f.svc.RiskHeatMap(f.ctx, f.tenant, f.project.ID, false)
	if err != nil || h.Total != 2 || h.Grid[2][4] != 1 || h.Grid[2][3] != 1 {
		t.Fatalf("heat map: %+v %v", h, err)
	}
	if h, _ = f.svc.RiskHeatMap(f.ctx, f.tenant, f.project.ID, true); h.Total != 3 {
		t.Fatalf("all: %d", h.Total)
	}
	list, total, err := f.svc.ListRisks(f.ctx, f.tenant, f.project.ID, RiskFilter{MinScore: 10})
	if err != nil || total != 2 || list[0].Score != 15 {
		t.Fatalf("list: %v %d", err, total)
	}
	if err := f.svc.DeleteRisk(f.ctx, f.tenant, f.project.ID, r.ID); err != nil {
		t.Fatal(err)
	}
	if len(f.events(t, EventRiskCreated)) != 3 || len(f.events(t, EventRiskUpdated)) != 2 || len(f.events(t, EventRiskDeleted)) != 1 {
		t.Fatal("risk events")
	}
}

// fakeTreasury answers treasury-api's budget S2S routes; failSubmit makes submit answer 500.
type fakeTreasury struct {
	mu         sync.Mutex
	project    uuid.UUID
	creates    int
	submits    int
	failSubmit bool
	budgets    map[uuid.UUID]bool
}

func (ft *fakeTreasury) handler(t *testing.T, tenant uuid.UUID) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ft.mu.Lock()
		defer ft.mu.Unlock()
		base := "/api/v1/s2s/" + tenant.String() + "/budgets"
		if r.Header.Get("X-API-Key") != "k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == base:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["budget_type"] != "project" || body["project_id"] != ft.project.String() || r.Header.Get("X-User-ID") == "" {
				t.Errorf("create body or user: %v", body)
			}
			ft.creates++
			id := uuid.New()
			ft.budgets[id] = true
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "status": "draft"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/submit"):
			id := uuid.MustParse(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, base+"/"), "/submit"))
			if !ft.budgets[id] {
				t.Errorf("submit of unknown budget %s", id)
			}
			if ft.failSubmit {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"error":"the period is closed"}`))
				return
			}
			ft.submits++
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "pending_approval"})
		default:
			t.Errorf("unexpected treasury call %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

// A change request is approved by someone else; approval shifts the task dates in the same
// transaction and pushes the budget to treasury; a failed push keeps the budget id and a retry
// submits that same budget.
func TestChangeRequestFlow(t *testing.T) {
	ft := &fakeTreasury{budgets: map[uuid.UUID]bool{}, failSubmit: true}
	tenant := uuid.New()
	srv := httptest.NewServer(ft.handler(t, tenant))
	defer srv.Close()
	f := newFx(t, treasury.NewClient(srv.URL, "k"))
	f.tenant = tenant
	f.project = f.client.Project.Create().SetTenantID(tenant).SetName("Road").SetOwnerID(f.owner).SaveX(f.ctx)
	ft.project = f.project.ID
	f.client.ProjectMember.Create().SetTenantID(tenant).SetProjectID(f.project.ID).SetUserID(f.ana).SetRoleCode("member").SaveX(f.ctx)
	a := f.task(t, "Survey", "2026-11-02", "2026-11-06", 8)
	b := f.task(t, "Design", "2026-11-09", "2026-11-13", 8)
	f.tasks.AddDependency(f.ctx, tenant, f.project.ID, b.ID, a.ID, "FS")

	asAna := activity.WithActor(context.Background(), f.ana)
	title, kind := "Extra survey week", "cost"
	shift := 7
	budget := json.RawMessage(`{"name":"Road v2","currency":"KES","lines":[{"category":"labour","name":"Survey","planned_amount":"50000"}]}`)
	if _, err := f.svc.CreateChangeRequest(asAna, tenant, f.project.ID, ChangeRequestInput{Title: &title, Kind: &kind}); err == nil {
		t.Fatal("a cost change without a budget accepted")
	}
	// Shifting only the survey would break the survey to design link: refused, nothing changes.
	cr, err := f.svc.CreateChangeRequest(asAna, tenant, f.project.ID, ChangeRequestInput{Title: &title, Kind: &kind, Budget: budget,
		DateShifts: &[]tasks.DateShift{{TaskID: a.ID, ShiftDays: &shift}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Decide(asAna, tenant, f.project.ID, cr.ID, true, ""); !errors.Is(err, ErrOwnRequest) {
		t.Fatalf("own approval: %v", err)
	}
	var conflict *tasks.ScheduleConflict
	if _, err := f.svc.Decide(f.ctx, tenant, f.project.ID, cr.ID, true, "ok"); !errors.As(err, &conflict) {
		t.Fatalf("conflicting shift: %v", err)
	}
	if got, _ := f.svc.GetChangeRequest(f.ctx, tenant, f.project.ID, cr.ID); got.Status != CRPending {
		t.Fatal("a failed approval changed the request")
	}
	// Shift the chain together: approved.
	if cr, err = f.svc.UpdateChangeRequest(asAna, tenant, f.project.ID, cr.ID, ChangeRequestInput{
		DateShifts: &[]tasks.DateShift{{TaskID: a.ID, ShiftDays: &shift}, {TaskID: b.ID, ShiftDays: &shift}}}, false); err != nil {
		t.Fatal(err)
	}
	cr, err = f.svc.Decide(f.ctx, tenant, f.project.ID, cr.ID, true, "Go ahead")
	if err != nil {
		t.Fatal(err)
	}
	if cr.Status != CRApproved || cr.DecisionComment != "Go ahead" {
		t.Fatalf("approved: %+v", cr)
	}
	if got := f.client.Task.GetX(f.ctx, b.ID); got.DueDate.Format(time.DateOnly) != "2026-11-20" {
		t.Fatalf("design not shifted: %v", got.DueDate)
	}
	st := cr.Metadata[crMetaTreasury].(map[string]any)
	if st["status"] != PushFailed || st["error"] != "the period is closed" || st["budget_id"] == nil || ft.creates != 1 {
		t.Fatalf("failed push: %v (creates %d)", st, ft.creates)
	}
	ft.failSubmit = false
	if cr, err = f.svc.PushBudget(f.ctx, tenant, f.project.ID, cr.ID); err != nil {
		t.Fatal(err)
	}
	if st = cr.Metadata[crMetaTreasury].(map[string]any); st["status"] != PushSubmitted || ft.creates != 1 || ft.submits != 1 {
		t.Fatalf("retry: %v creates %d submits %d", st, ft.creates, ft.submits)
	}
	var serr *StateError
	if _, err := f.svc.PushBudget(f.ctx, tenant, f.project.ID, cr.ID); !errors.As(err, &serr) {
		t.Fatalf("a second push: %v", err)
	}
	if len(f.events(t, EventCRApproved)) != 1 || len(f.events(t, EventCRBudgetPushed)) != 2 || len(f.events(t, EventCRSubmitted)) != 1 {
		t.Fatal("change request events")
	}
	// Reject needs a reason; withdraw is the requester's.
	other, _ := f.svc.CreateChangeRequest(asAna, tenant, f.project.ID, ChangeRequestInput{Title: &title})
	if _, err := f.svc.Decide(f.ctx, tenant, f.project.ID, other.ID, false, ""); err == nil {
		t.Fatal("reject without a reason")
	}
	asBen := activity.WithActor(context.Background(), f.ben)
	if _, err := f.svc.WithdrawChangeRequest(asBen, tenant, f.project.ID, other.ID, false); !errors.Is(err, ErrNotYours) {
		t.Fatalf("withdraw by someone else: %v", err)
	}
	if w, err := f.svc.WithdrawChangeRequest(asAna, tenant, f.project.ID, other.ID, false); err != nil || w.Status != CRWithdrawn {
		t.Fatalf("withdraw: %v", err)
	}
	stranger := uuid.New()
	if list, total, _ := f.svc.ListChangeRequests(f.ctx, tenant, nil, CRFilter{VisibleTo: &stranger}); total != 0 || len(list) != 0 {
		t.Fatal("a stranger sees the queue")
	}
	if _, total, _ := f.svc.ListChangeRequests(f.ctx, tenant, nil, CRFilter{VisibleTo: &f.ana}); total != 2 {
		t.Fatalf("ana's queue: %d", total)
	}
}

// System templates seed idempotently; a project started from one gets the whole plan; a project
// saves back as a template; system templates cannot be deleted.
func TestTemplates(t *testing.T) {
	f := newFx(t, nil)
	for range 2 {
		if err := SeedSystemTemplates(f.ctx, f.client); err != nil {
			t.Fatal(err)
		}
	}
	list, total, err := f.svc.ListTemplates(f.ctx, f.tenant, "", 20, 0)
	if err != nil || total != 3 || !list[0].System {
		t.Fatalf("seeded: %d %v", total, err)
	}
	var rollout TemplateSummary
	for _, tpl := range list {
		if tpl.Name == "IT rollout" {
			rollout = tpl
		}
	}
	res, err := f.svc.CreateFromTemplate(f.ctx, f.tenant, rollout.ID, FromTemplateInput{Name: "Branch rollout", StartDate: tasks.Date{Time: d("2026-12-01")}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Tasks != 12 || res.Milestones != 4 || res.Project.OwnerID != f.owner || res.Project.StartDate.Format(time.DateOnly) != "2026-12-01" {
		t.Fatalf("from template: %+v", res)
	}
	first := f.client.Task.Query().Where(enttask.ProjectID(res.Project.ID), enttask.Title("Site and infrastructure survey")).OnlyX(f.ctx)
	if first.StartDate.Format(time.DateOnly) != "2026-12-03" || first.DueDate.Format(time.DateOnly) != "2026-12-07" {
		t.Fatalf("task dates: %v %v", first.StartDate, first.DueDate)
	}
	deps := f.client.TaskDependency.Query().Where(enttaskdep.HasTaskWith(enttask.ProjectID(res.Project.ID))).CountX(f.ctx)
	if deps != 13 {
		t.Fatalf("dependencies: %d", deps)
	}
	saved, err := f.svc.SaveAsTemplate(f.ctx, f.tenant, res.Project.ID, SaveAsTemplateInput{Name: "Our rollout"})
	if err != nil || saved.System || saved.TaskCount != 12 || len(saved.Definition.Phases) != 4 {
		t.Fatalf("save as: %+v %v", saved, err)
	}
	for _, tk := range saved.Definition.Tasks {
		if tk.Title == "Site and infrastructure survey" && (tk.OffsetDays != 2 || tk.DurationDays != 5 || len(tk.DependsOn) != 1) {
			t.Fatalf("round trip: %+v", tk)
		}
	}
	if err := f.svc.DeleteTemplate(f.ctx, f.tenant, rollout.ID); !errors.Is(err, ErrSystemTemplate) {
		t.Fatalf("system delete: %v", err)
	}
	if err := f.svc.DeleteTemplate(f.ctx, f.tenant, saved.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.GetTemplate(f.ctx, uuid.New(), saved.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("another tenant read the template")
	}
	if len(f.events(t, EventTemplateApplied)) != 1 || len(f.events(t, "created")) != 1 {
		t.Fatal("template events")
	}
}
