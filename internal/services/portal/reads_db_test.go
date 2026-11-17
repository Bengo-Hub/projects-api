package portal

import (
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	entmember "github.com/bengobox/projects-service/internal/ent/projectmember"
	"github.com/bengobox/projects-service/internal/modules/rbac"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/comments"
	"github.com/bengobox/projects-service/internal/services/milestones"
	"github.com/bengobox/projects-service/internal/services/tasks"
)

// The team's milestone list and one milestone carry the client's newest decision (outcome,
// comment, who and when), the same one the portal shows; a milestone nobody decided carries none,
// and another project's decisions never show.
func TestLastDecisionOnTeamAndPortalMilestones(t *testing.T) {
	ctx, w := setup(t)
	jane := uuid.New()
	w.client.ProjectMember.Create().SetTenantID(w.tenant).SetProjectID(w.project.ID).SetUserID(jane).SetRoleCode(rbac.RoleClient).SaveX(ctx)
	w.client.ProjectMember.Create().SetTenantID(w.tenant).SetProjectID(w.elsewhere.ID).SetUserID(jane).SetRoleCode(rbac.RoleClient).SaveX(ctx)
	w.client.ProjectsUser.Create().SetTenantID(w.tenant).SetAuthServiceUserID(jane).SetEmail("jane@client.test").SetName("Jane Client").SaveX(ctx)
	ms := milestones.NewService(w.client, nil, zap.NewNop())
	teamCtx, clientCtx := activity.WithActor(ctx, w.owner), activity.WithActor(ctx, jane)
	ready := milestones.StatusReadyForReview

	mk := func(projectID uuid.UUID, name string) uuid.UUID {
		m, err := ms.CreateMilestone(teamCtx, w.tenant, projectID, milestones.CreateMilestoneInput{Name: name, Status: ready})
		if err != nil {
			t.Fatal(err)
		}
		return m.ID
	}
	decide := func(projectID, id uuid.UUID, decision, comment string) {
		t.Helper()
		if _, err := ms.Decide(clientCtx, w.tenant, projectID, id, jane, milestones.DecisionInput{Decision: decision, Comment: comment}); err != nil {
			t.Fatal(err)
		}
	}
	walls, roof, paint := mk(w.project.ID, "Walls"), mk(w.project.ID, "Roof"), mk(w.project.ID, "Paint")
	decide(w.project.ID, walls, milestones.DecisionRequestChanges, "Move the window")
	if _, err := ms.UpdateMilestone(teamCtx, w.tenant, w.project.ID, walls, milestones.UpdateMilestoneInput{Status: &ready}); err != nil {
		t.Fatal(err)
	}
	decide(w.project.ID, walls, milestones.DecisionApprove, "Window moved, thanks")
	decide(w.project.ID, roof, milestones.DecisionRequestChanges, "Wrong tiles")
	road := mk(w.elsewhere.ID, "Road")
	decide(w.elsewhere.ID, road, milestones.DecisionApprove, "Other project")

	list, err := ms.ListReviewed(ctx, w.tenant, w.project.ID)
	if err != nil || len(list) != 3 {
		t.Fatalf("team list = %+v %v", list, err)
	}
	byID := map[uuid.UUID]milestones.Reviewed{}
	for _, m := range list {
		byID[m.ID] = m
	}
	want := func(where string, d *milestones.Decision, outcome, comment string) {
		t.Helper()
		if d == nil || d.Decision != outcome || d.Comment != comment || d.DecidedByName != "Jane Client" || d.DecidedAt == "" {
			t.Errorf("%s last_decision = %+v, want %s %q by Jane Client", where, d, outcome, comment)
		}
	}
	want("walls", byID[walls].LastDecision, milestones.StatusApproved, "Window moved, thanks")
	want("roof", byID[roof].LastDecision, milestones.StatusChangesRequested, "Wrong tiles")
	if byID[paint].LastDecision != nil {
		t.Errorf("paint was never decided: %+v", byID[paint].LastDecision)
	}

	one, err := ms.GetReviewed(ctx, w.tenant, w.project.ID, roof)
	if err != nil {
		t.Fatal(err)
	}
	want("get roof", one.LastDecision, milestones.StatusChangesRequested, "Wrong tiles")
	if _, err := ms.GetReviewed(ctx, w.tenant, w.project.ID, road); err == nil {
		t.Error("another project's milestone must not be found")
	}

	views, err := w.svc.Milestones(ctx, w.tenant, w.project.ID)
	if err != nil || len(views) != 3 {
		t.Fatalf("portal milestones = %+v %v", views, err)
	}
	for _, v := range views {
		if v.ID == walls {
			want("portal walls", v.LastDecision, milestones.StatusApproved, "Window moved, thanks")
		}
	}
}

// Each client thread entry says whether a client of that project or the team wrote it, on the
// list and on the posted answer. A client of another project posting here counts as team.
func TestThreadAuthorKind(t *testing.T) {
	ctx, w := setup(t)
	jane, ted, other := uuid.New(), uuid.New(), uuid.New()
	w.client.ProjectMember.Create().SetTenantID(w.tenant).SetProjectID(w.project.ID).SetUserID(jane).SetRoleCode(rbac.RoleClient).SaveX(ctx)
	w.client.ProjectMember.Create().SetTenantID(w.tenant).SetProjectID(w.project.ID).SetUserID(ted).SetRoleCode("member").SaveX(ctx)
	w.client.ProjectMember.Create().SetTenantID(w.tenant).SetProjectID(w.elsewhere.ID).SetUserID(other).SetRoleCode(rbac.RoleClient).SaveX(ctx)
	w.client.ProjectsUser.Create().SetTenantID(w.tenant).SetAuthServiceUserID(jane).SetName("Jane Client").SaveX(ctx)
	w.client.ProjectsUser.Create().SetTenantID(w.tenant).SetAuthServiceUserID(ted).SetName("Ted Team").SaveX(ctx)
	cs := comments.NewService(w.client, nil, zap.NewNop())

	post := func(user uuid.UUID, text, kind string) *comments.ThreadComment {
		t.Helper()
		c, _, err := cs.CreateClientThreadComment(ctx, w.tenant, w.project.ID, comments.CreateCommentInput{UserID: user, Content: "<p>" + text + "</p>"}, kind)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	if c := post(jane, "When is the handover?", comments.AuthorClient); c.AuthorKind != comments.AuthorClient || c.AuthorName != "Jane Client" {
		t.Errorf("client post answer = %+v", c)
	}
	if c := post(ted, "Friday.", comments.AuthorTeam); c.AuthorKind != comments.AuthorTeam || c.AuthorName != "Ted Team" {
		t.Errorf("team post answer = %+v", c)
	}
	post(other, "Hello", comments.AuthorTeam)
	if _, _, err := cs.CreateClientThreadComment(ctx, w.tenant, w.project.ID, comments.CreateCommentInput{UserID: jane, Content: "<p>x</p>"}, "admin"); err == nil {
		t.Error("an unknown author kind must be refused")
	}

	wantKinds := []string{comments.AuthorClient, comments.AuthorTeam, comments.AuthorTeam}
	check := func(when string) {
		t.Helper()
		thread, n, err := cs.ListClientThread(ctx, w.tenant, w.project.ID, 50, 0)
		if err != nil || n != 3 {
			t.Fatalf("%s: thread = %+v %d %v", when, thread, n, err)
		}
		for i, c := range thread {
			if c.AuthorKind != wantKinds[i] {
				t.Errorf("%s: entry %d by %s: author_kind %q, want %q", when, i, c.AuthorName, c.AuthorKind, wantKinds[i])
			}
		}
	}
	check("as posted")
	// Revoking Jane's invite deletes her client row; what she said stays hers.
	w.client.ProjectMember.Delete().Where(entmember.UserID(jane)).ExecX(ctx)
	check("after the client was revoked")
}

// The landing list carries each project's progress, computed for the whole page, and it agrees
// with the project page's summary: every task counts (shared or not), and a milestone is done when
// completed or approved. Another tenant's or another project's rows never count.
func TestListProjectsProgressMatchesSummary(t *testing.T) {
	ctx, w := setup(t)
	jane := uuid.New()
	for _, p := range []uuid.UUID{w.project.ID, w.elsewhere.ID} {
		w.client.ProjectMember.Create().SetTenantID(w.tenant).SetProjectID(p).SetUserID(jane).SetRoleCode(rbac.RoleClient).SaveX(ctx)
	}
	task := func(tenant, project uuid.UUID, status string, shared bool) {
		c := w.client.Task.Create().SetTenantID(tenant).SetProjectID(project).SetTitle("t").SetStatus(status)
		if shared {
			c.SetMetadata(map[string]any{tasks.MetaClientVisible: true})
		}
		c.SaveX(ctx)
	}
	task(w.tenant, w.project.ID, tasks.StatusDone, true)
	task(w.tenant, w.project.ID, tasks.StatusDone, false)
	task(w.tenant, w.project.ID, "in_progress", true)
	task(w.tenant, w.project.ID, "todo", false)
	task(w.tenant, w.project.ID, "todo", false)
	task(w.tenant, w.project.ID, "todo", false)
	// Rows in another tenant with the same project id never count.
	task(w.other, w.project.ID, tasks.StatusDone, false)
	for _, st := range []string{"completed", milestones.StatusApproved, milestones.StatusReadyForReview, "pending"} {
		w.client.Milestone.Create().SetTenantID(w.tenant).SetProjectID(w.project.ID).SetName(st).SetStatus(st).SaveX(ctx)
	}
	w.client.Milestone.Create().SetTenantID(w.other).SetProjectID(w.project.ID).SetName("x").SetStatus("completed").SaveX(ctx)

	list, total, err := w.svc.ListProjects(ctx, w.tenant, jane, 20, 0)
	if err != nil || total != 2 || len(list) != 2 {
		t.Fatalf("list = %+v %d %v", list, total, err)
	}
	want := map[uuid.UUID]Progress{
		w.project.ID:   {TasksTotal: 6, TasksDone: 2, Percent: 33, MilestonesTotal: 4, MilestonesDone: 2, AwaitingReview: 1},
		w.elsewhere.ID: {},
	}
	for _, item := range list {
		if item.Progress != want[item.ID] {
			t.Errorf("%s progress = %+v, want %+v", item.Name, item.Progress, want[item.ID])
		}
		s, err := w.svc.ProjectSummary(ctx, w.tenant, item.ID)
		if err != nil {
			t.Fatal(err)
		}
		p := item.Progress
		if s.Progress != p.Percent || s.TotalTasks != p.TasksTotal || s.CompletedTasks != p.TasksDone ||
			s.TotalMilestones != p.MilestonesTotal || s.CompletedMilestones != p.MilestonesDone || s.AwaitingReview != p.AwaitingReview {
			t.Errorf("%s summary %+v disagrees with the card %+v", item.Name, s, p)
		}
	}
	// The next milestone is the earliest open one: done and approved ones are skipped.
	if s, _ := w.svc.ProjectSummary(ctx, w.tenant, w.project.ID); s.NextMilestone == nil ||
		(s.NextMilestone.Status != milestones.StatusReadyForReview && s.NextMilestone.Status != "pending") {
		t.Errorf("next milestone = %+v", s.NextMilestone)
	}
}
