package portal

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	entoutbox "github.com/bengobox/projects-service/internal/ent/outboxevent"
	"github.com/bengobox/projects-service/internal/modules/rbac"
	"github.com/bengobox/projects-service/internal/platform/marketflow"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/comments"
	"github.com/bengobox/projects-service/internal/services/membership"
	"github.com/bengobox/projects-service/internal/services/milestones"
	"github.com/bengobox/projects-service/internal/services/notify"
	"github.com/bengobox/projects-service/internal/services/qa"
	"github.com/bengobox/projects-service/internal/services/tasks"
	"github.com/bengobox/projects-service/internal/testutil"
)

// fakeCRM answers contacts by id, per tenant, like marketflow does.
type fakeCRM map[uuid.UUID]map[uuid.UUID]marketflow.Contact

func (f fakeCRM) GetContact(_ context.Context, tenantID, contactID uuid.UUID) (marketflow.Contact, error) {
	c, ok := f[tenantID][contactID]
	if !ok {
		return marketflow.Contact{}, marketflow.ErrContactNotFound
	}
	return c, nil
}

// fakeVerifier is auth-api's email verification: a user's proven address, if any. calls counts
// how often it was asked.
type fakeVerifier struct {
	proven map[uuid.UUID]string
	calls  int
}

func (f *fakeVerifier) VerifiedEmail(_ context.Context, userID uuid.UUID) (string, bool, error) {
	f.calls++
	email, ok := f.proven[userID]
	return email, ok, nil
}

func payloads(t *testing.T, rows []*ent.OutboxEvent) []map[string]any {
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

type world struct {
	client             *ent.Client
	svc                *Service
	verifier           *fakeVerifier
	tenant, other      uuid.UUID
	owner              uuid.UUID
	project, elsewhere *ent.Project
	contact            uuid.UUID
}

func setup(t *testing.T) (context.Context, world) {
	t.Helper()
	client := testutil.Client(t)
	ctx := notify.WithOrgSlug(context.Background(), "acme")
	w := world{client: client, tenant: uuid.New(), other: uuid.New(), owner: uuid.New(), contact: uuid.New()}
	w.project = client.Project.Create().SetTenantID(w.tenant).SetName("Bridge").SetOwnerID(w.owner).SetBudget(9000000).SaveX(ctx)
	w.elsewhere = client.Project.Create().SetTenantID(w.tenant).SetName("Road").SetOwnerID(w.owner).SaveX(ctx)
	crm := fakeCRM{w.tenant: {w.contact: {ID: w.contact, FirstName: "Joan", LastName: "Client", Email: "Joan.Client@Example.com", Phone: "+254700000001"}}}
	keys, _ := qa.NewKeys("test-hash-key", "")
	w.verifier = &fakeVerifier{proven: map[uuid.UUID]string{}}
	w.svc = NewService(client, crm, keys, zap.NewNop()).WithEmailVerifier(w.verifier)
	return ctx, w
}

// An invite attaches only to an account whose address auth-api has proven: a self-registered
// account typing the client's email gets nothing, a proven different address gets nothing, and
// auth-api is not asked at all when no invite is waiting.
func TestActivateNeedsAProvenEmail(t *testing.T) {
	ctx, w := setup(t)
	if _, _, err := w.svc.Invite(ctx, w.tenant, w.project.ID, w.contact, w.owner); err != nil {
		t.Fatal(err)
	}
	impostor := uuid.New()
	if n, err := w.svc.Activate(ctx, w.tenant, impostor, "joan.client@example.com"); !errors.Is(err, ErrEmailNotVerified) || n != 0 {
		t.Fatalf("unverified account: %d %v, want ErrEmailNotVerified", n, err)
	}
	w.verifier.proven[impostor] = "someone.else@example.com"
	if n, err := w.svc.Activate(ctx, w.tenant, impostor, "joan.client@example.com"); !errors.Is(err, ErrEmailNotVerified) || n != 0 {
		t.Fatalf("proven other address: %d %v, want ErrEmailNotVerified", n, err)
	}
	if ok, _ := w.svc.IsClient(ctx, w.tenant, w.project.ID, impostor); ok {
		t.Fatal("an unproven account must not become a client")
	}

	before := w.verifier.calls
	if n, err := w.svc.Activate(ctx, w.tenant, uuid.New(), "nobody.invited@example.com"); err != nil || n != 0 {
		t.Fatalf("no invite: %d %v", n, err)
	}
	if w.verifier.calls != before {
		t.Error("auth-api must not be asked when no invite is waiting")
	}

	// Without a verifier nothing is attached (fail closed).
	unchecked := NewService(w.client, fakeCRM{}, w.svc.hasher, zap.NewNop())
	if n, err := unchecked.Activate(ctx, w.tenant, uuid.New(), "joan.client@example.com"); !errors.Is(err, ErrEmailNotVerified) || n != 0 {
		t.Fatalf("no verifier: %d %v, want ErrEmailNotVerified", n, err)
	}

	joan := uuid.New()
	w.verifier.proven[joan] = "Joan.Client@Example.com"
	if n, err := w.svc.Activate(ctx, w.tenant, joan, "joan.client@example.com"); err != nil || n != 1 {
		t.Fatalf("proven owner: %d %v, want the invite attached", n, err)
	}
}

// An invite stores no address, sends the email and WhatsApp invite with the contact's details,
// resends on a repeat, and is matched to the client's account by their SSO email in any case,
// in its own tenant only.
func TestInviteAndActivate(t *testing.T) {
	ctx, w := setup(t)
	c, resent, err := w.svc.Invite(ctx, w.tenant, w.project.ID, w.contact, w.owner)
	if err != nil || resent {
		t.Fatalf("invite: %v resent=%v", err, resent)
	}
	if c.Status != StatusInvited || c.UserID != nil || c.Name != "Joan Client" || c.EmailHint != "j***@example.com" {
		t.Errorf("client = %+v", c)
	}
	row := w.client.ProjectMember.GetX(ctx, c.ID)
	for _, v := range row.Metadata {
		if s, _ := v.(string); s == "joan.client@example.com" || s == "Joan.Client@Example.com" {
			t.Error("the row must not store the address")
		}
	}
	ev := payloads(t, w.client.OutboxEvent.Query().Where(entoutbox.EventType(notify.EventClientInvited)).AllX(ctx))
	if len(ev) != 1 || ev[0]["recipient_email"] != "joan.client@example.com" || ev[0]["recipient_phone"] != "+254700000001" ||
		ev[0]["portal_path"] != "/acme/portal" || ev[0]["project_name"] != "Bridge" {
		t.Fatalf("invite event = %v", ev)
	}
	if _, resent, err := w.svc.Invite(ctx, w.tenant, w.project.ID, w.contact, w.owner); err != nil || !resent {
		t.Fatalf("second invite: %v resent=%v", err, resent)
	}
	if n := w.client.ProjectMember.Query().CountX(ctx); n != 1 {
		t.Errorf("a resend must not add a row: %d rows", n)
	}

	// Another tenant's user with the same address never takes this invite.
	stranger := uuid.New()
	if n, err := w.svc.Activate(ctx, w.other, stranger, "joan.client@example.com"); err != nil || n != 0 {
		t.Fatalf("cross-tenant activation: %d %v", n, err)
	}
	user := uuid.New()
	w.verifier.proven[user] = "joan.client@example.com"
	if n, err := w.svc.Activate(ctx, w.tenant, user, "JOAN.client@example.com"); err != nil || n != 1 {
		t.Fatalf("activation: %d %v", n, err)
	}
	if ok, _ := w.svc.IsClient(ctx, w.tenant, w.project.ID, user); !ok {
		t.Error("the activated user must be a client of the project")
	}
	if ok, _ := w.svc.IsClient(ctx, w.tenant, w.elsewhere.ID, user); ok {
		t.Error("a client must not reach another project")
	}
	if ok, _ := w.svc.IsClient(ctx, w.other, w.project.ID, user); ok {
		t.Error("a client must not reach the project through another tenant")
	}
	list, total, err := w.svc.ListProjects(ctx, w.tenant, user, 20, 0)
	if err != nil || total != 1 || list[0].ID != w.project.ID {
		t.Fatalf("client projects = %v %d %v", list, total, err)
	}
	if _, _, err := w.svc.Invite(ctx, w.tenant, w.project.ID, w.contact, w.owner); !errors.Is(err, ErrAlreadyClient) {
		t.Errorf("inviting an active client: %v", err)
	}
	if _, _, err := w.svc.Invite(ctx, w.tenant, w.project.ID, uuid.New(), w.owner); !errors.Is(err, ErrContactNotFound) {
		t.Errorf("unknown contact: %v", err)
	}
}

// A client membership opens the portal only: it never counts for staff scoping or as someone who
// can be mentioned or assigned.
func TestClientRowIsNotTeamMembership(t *testing.T) {
	ctx, w := setup(t)
	user := uuid.New()
	w.client.ProjectMember.Create().SetTenantID(w.tenant).SetProjectID(w.project.ID).SetUserID(user).SetRoleCode(rbac.RoleClient).SaveX(ctx)
	if ok, err := membership.New(w.client).IsMember(ctx, w.tenant, w.project.ID, user); err != nil || ok {
		t.Errorf("staff scoping lets a client in: %v %v", ok, err)
	}
	tx, err := w.client.Tx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	members, err := notify.Members(ctx, tx, w.tenant, w.project.ID, []uuid.UUID{user})
	if err != nil || members[user] {
		t.Errorf("a client must not be mentionable or assignable: %v %v", members, err)
	}
}

// The portal shows only shared tasks and never internal comments; internal lists never show the
// client thread; a teammate cannot slip a comment into the thread through metadata.
func TestSharedTasksAndThreads(t *testing.T) {
	ctx, w := setup(t)
	team, user := uuid.New(), uuid.New()
	w.client.ProjectMember.Create().SetTenantID(w.tenant).SetProjectID(w.project.ID).SetUserID(user).SetRoleCode(rbac.RoleClient).SaveX(ctx)
	ts := tasks.NewService(w.client, nil, zap.NewNop())
	shared := w.client.Task.Create().SetTenantID(w.tenant).SetProjectID(w.project.ID).SetTitle("Design").SaveX(ctx)
	w.client.Task.Create().SetTenantID(w.tenant).SetProjectID(w.project.ID).SetTitle("Internal costing").
		SetMetadata(map[string]any{"note": "margin 40%"}).SaveX(ctx)
	if _, err := ts.SetClientVisible(ctx, w.tenant, w.project.ID, shared.ID, true); err != nil {
		t.Fatal(err)
	}
	// A task update cannot share a task: client_visible is server-owned.
	other := w.client.Task.Create().SetTenantID(w.tenant).SetProjectID(w.project.ID).SetTitle("Sneaky").SaveX(ctx)
	if _, err := ts.UpdateTask(ctx, w.tenant, w.project.ID, other.ID, tasks.UpdateTaskInput{Metadata: map[string]any{tasks.MetaClientVisible: true}}); err != nil {
		t.Fatal(err)
	}
	list, total, err := w.svc.SharedTasks(ctx, w.tenant, w.project.ID, 20, 0)
	if err != nil || total != 1 || list[0].ID != shared.ID {
		t.Fatalf("shared tasks = %+v %d %v", list, total, err)
	}

	cs := comments.NewService(w.client, nil, zap.NewNop())
	if _, _, err := cs.CreateProjectComment(ctx, w.tenant, w.project.ID, comments.CreateCommentInput{
		UserID: team, Content: "<p>internal: our margin is thin</p>", Metadata: map[string]any{comments.MetaThread: comments.ThreadClient},
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := cs.CreateClientThreadComment(ctx, w.tenant, w.project.ID, comments.CreateCommentInput{UserID: user, Content: "<p>Looks good</p>"}); err != nil {
		t.Fatal(err)
	}
	thread, n, err := cs.ListClientThread(ctx, w.tenant, w.project.ID, 50, 0)
	if err != nil || n != 1 || thread[0].Content != "<p>Looks good</p>" {
		t.Fatalf("client thread = %+v %d %v", thread, n, err)
	}
	internal, n, err := cs.ListCommentsByProject(ctx, w.tenant, w.project.ID, 50, 0)
	if err != nil || n != 1 || internal[0].UserID != team {
		t.Fatalf("internal comments = %+v %d %v", internal, n, err)
	}
}

// A client decides only on a milestone up for review; the decision moves the status, keeps who and
// when in the activity feed and tells the project's leads. The team cannot set a client status.
func TestMilestoneReviewAndDecision(t *testing.T) {
	ctx, w := setup(t)
	user, manager := uuid.New(), uuid.New()
	w.client.ProjectMember.Create().SetTenantID(w.tenant).SetProjectID(w.project.ID).SetUserID(user).SetRoleCode(rbac.RoleClient).SaveX(ctx)
	w.client.ProjectMember.Create().SetTenantID(w.tenant).SetProjectID(w.project.ID).SetUserID(manager).SetRoleCode("manager").SaveX(ctx)
	ms := milestones.NewService(w.client, nil, zap.NewNop())
	m, err := ms.CreateMilestone(ctx, w.tenant, w.project.ID, milestones.CreateMilestoneInput{Name: "Foundations"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.Decide(ctx, w.tenant, w.project.ID, m.ID, user, milestones.DecisionInput{Decision: milestones.DecisionApprove}); !errors.Is(err, milestones.ErrNotReady) {
		t.Fatalf("decision before review: %v", err)
	}
	approved := milestones.StatusApproved
	if _, err := ms.UpdateMilestone(ctx, w.tenant, w.project.ID, m.ID, milestones.UpdateMilestoneInput{Status: &approved}); err == nil {
		t.Fatal("the team must not approve on the client's behalf")
	}
	ready := milestones.StatusReadyForReview
	teamCtx := activity.WithActor(ctx, w.owner)
	if _, err := ms.UpdateMilestone(teamCtx, w.tenant, w.project.ID, m.ID, milestones.UpdateMilestoneInput{Status: &ready}); err != nil {
		t.Fatal(err)
	}
	rev := payloads(t, w.client.OutboxEvent.Query().Where(entoutbox.EventType(notify.EventMilestoneReadyForReview)).AllX(ctx))
	if len(rev) != 1 || rev[0]["recipient_user_id"] != user.String() ||
		rev[0]["action_path"] != "/acme/portal/projects/"+w.project.ID.String()+"?milestone="+m.ID.String() {
		t.Fatalf("ready_for_review events = %v", rev)
	}
	if _, err := ms.Decide(ctx, w.tenant, w.project.ID, m.ID, user, milestones.DecisionInput{Decision: milestones.DecisionRequestChanges}); err == nil {
		t.Fatal("request_changes needs a comment")
	}
	clientCtx := activity.WithActor(ctx, user)
	got, err := ms.Decide(clientCtx, w.tenant, w.project.ID, m.ID, user, milestones.DecisionInput{Decision: milestones.DecisionApprove, Comment: "Great work"})
	if err != nil || got.Status != milestones.StatusApproved {
		t.Fatalf("approve: %v %v", got, err)
	}
	done := payloads(t, w.client.OutboxEvent.Query().Where(entoutbox.EventType(notify.EventMilestoneApproved)).AllX(ctx))
	to := map[string]bool{}
	for _, e := range done {
		to[e["recipient_user_id"].(string)] = true
	}
	if len(done) != 2 || !to[w.owner.String()] || !to[manager.String()] || to[user.String()] {
		t.Fatalf("approved events to %v", to)
	}
	views, err := w.svc.Milestones(ctx, w.tenant, w.project.ID)
	if err != nil || len(views) != 1 || views[0].LastDecision == nil || views[0].LastDecision.Comment != "Great work" || views[0].CanDecide {
		t.Fatalf("milestone views = %+v %v", views, err)
	}
	if _, err := ms.Decide(clientCtx, w.tenant, w.project.ID, m.ID, user, milestones.DecisionInput{Decision: milestones.DecisionApprove}); !errors.Is(err, milestones.ErrNotReady) {
		t.Errorf("a second decision: %v", err)
	}
}
