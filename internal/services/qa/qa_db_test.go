package qa

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent/qainvite"
	"github.com/bengobox/projects-service/internal/ent/qaresult"
	"github.com/bengobox/projects-service/internal/platform/ticketing"
	"github.com/bengobox/projects-service/internal/qamodel"
	"github.com/bengobox/projects-service/internal/shared/fieldcrypt"
	"github.com/bengobox/projects-service/internal/testutil"
)

type fakePublisher struct {
	mu     sync.Mutex
	events []string
	last   map[string]map[string]any
}

func (f *fakePublisher) PublishQAEvent(_ context.Context, _, _ uuid.UUID, eventType string, payload map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, eventType)
	if f.last == nil {
		f.last = map[string]map[string]any{}
	}
	f.last[eventType] = payload
	return nil
}

func (f *fakePublisher) count(eventType string) int {
	n := 0
	for _, e := range f.events {
		if e == eventType {
			n++
		}
	}
	return n
}

type fakeTickets struct {
	calls []ticketing.CreateTicketInput
	fail  bool
}

func (f *fakeTickets) CreateTicket(_ context.Context, in ticketing.CreateTicketInput) (*ticketing.Ticket, error) {
	f.calls = append(f.calls, in)
	if f.fail {
		return nil, errors.New("ticketing down")
	}
	return &ticketing.Ticket{ID: uuid.NewString(), TicketNumber: "TKT-100", Status: "open"}, nil
}

// fakeDirectory stands in for auth-api names: the org lookup only answers for the session's slug.
type fakeDirectory struct {
	names map[uuid.UUID]string
	org   string
}

func (f fakeDirectory) UserName(_ context.Context, _, userID uuid.UUID) (string, error) {
	if n, ok := f.names[userID]; ok {
		return n, nil
	}
	return "", errors.New("not a member")
}

func (f fakeDirectory) OrgName(_ context.Context, slug string) (string, error) {
	if slug != "acme" {
		return "", errors.New("unknown tenant")
	}
	return f.org, nil
}

type fakeOTP struct{ email string }

func (f fakeOTP) Validate(context.Context, string) (string, error) {
	if f.email == "" {
		return "", errors.New("invalid")
	}
	return f.email, nil
}

// TestQAFlow runs a session end to end: template, criteria, session, invites, access, results,
// the severe failure with its single ticket and alert, close with its hash, and the decision.
func TestQAFlow(t *testing.T) {
	client := testutil.Client(t)
	ctx := context.Background()
	keys, _ := NewKeys("test-key", "")
	pub, tickets := &fakePublisher{}, &fakeTickets{}
	box, err := fieldcrypt.New("test-field-key")
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(client, keys, zap.NewNop()).WithPublisher(pub).WithTickets(tickets).
		WithOTP(fakeOTP{email: "Tester.Two@Example.com"}).WithUIBaseURL("https://projects.example.com").
		WithSealer(box)
	tenant := uuid.New()
	owner := Actor{UserID: uuid.New(), Email: "owner@example.com", TenantID: tenant}
	svc.WithDirectory(fakeDirectory{names: map[uuid.UUID]string{owner.UserID: "Olive Owner"}, org: "Acme Ltd"})

	// A tenant may not create a platform template.
	if _, err := svc.CreateTemplate(ctx, owner, TemplateInput{Name: "x", Platform: true}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("platform template by a tenant: %v", err)
	}
	tpl, err := svc.CreateTemplate(ctx, owner, TemplateInput{Name: "Release acceptance", Status: qamodel.TemplateActive})
	if err != nil {
		t.Fatal(err)
	}
	crits, err := svc.AddCriteria(ctx, owner, tpl.ID, []CriterionInput{
		{AreaCode: "OWN", Title: "Owner registers", Weight: qamodel.WeightMust, SubjectKind: qamodel.SubjectFeature, SubjectRef: "hadia_owner_registration"},
		{AreaCode: "REG", Title: "Registry search", Weight: qamodel.WeightShould},
	})
	if err != nil || len(crits) != 2 {
		t.Fatalf("criteria: %v", err)
	}
	if _, err := svc.AddCriteria(ctx, owner, tpl.ID, []CriterionInput{{Title: "bad", SubjectKind: qamodel.SubjectTask, SubjectRef: uuid.NewString()}}); err == nil {
		t.Fatal("a criterion on a missing task was accepted")
	}

	// The catalogue change consumer flags the template.
	if n, err := svc.MarkFeatureChanged(ctx, "hadia_owner_registration"); err != nil || n != 1 {
		t.Fatalf("feature changed: %d %v", n, err)
	}
	if !client.QATemplate.GetX(ctx, tpl.ID).NeedsReview {
		t.Fatal("template not flagged for review")
	}

	// A looser pass rule is refused; the default is frozen into the session.
	loose := qamodel.DefaultPassRule()
	loose.RequireAllMust = false
	if _, err := svc.CreateSession(ctx, owner, SessionInput{TemplateID: tpl.ID, Title: "R1", PassRule: &loose}); err == nil {
		t.Fatal("a looser pass rule was accepted")
	}
	// The release session is Codevertex's own (a platform owner opens it), so its failures alert ops.
	platformOwner := owner
	platformOwner.PlatformOwner = true
	sess, err := svc.CreateSession(ctx, platformOwner, SessionInput{TemplateID: tpl.ID, Title: "R1", SubjectKind: qamodel.SessionSubjectService, SubjectRef: "hadia", TenantSlug: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.CriteriaSnapshot) != 2 || sess.Status != qamodel.SessionDraft {
		t.Fatalf("session: %+v", sess)
	}
	// Only a platform owner's service session alerts Codevertex ops; a tenant testing the same
	// service keeps its alerts in its own team, and the body's tenant_name is never used.
	tenantSess, err := svc.CreateSession(ctx, Actor{UserID: uuid.New(), TenantID: tenant}, SessionInput{TemplateID: tpl.ID, Title: "T1", SubjectKind: qamodel.SessionSubjectService, SubjectRef: "hadia", TenantSlug: "acme", TenantName: "Some Bank"})
	if err != nil {
		t.Fatal(err)
	}
	if scope(tenantSess) != "tenant" || scope(sess) != "platform" {
		t.Errorf("scope: tenant session %q, platform session %q", scope(tenantSess), scope(sess))
	}
	if n := metaString(tenantSess.Metadata, "tenant_name"); n == "Some Bank" {
		t.Errorf("tenant_name was taken from the request: %q", n)
	}

	if _, err := svc.Invite(ctx, owner, sess.ID, []InviteInput{
		{Email: "tester.one@example.com", Role: qamodel.RoleTester},
		{Email: "tester.two@example.com", Role: qamodel.RoleTester},
		{Email: "signer@example.com", Name: "Product owner", Role: qamodel.RoleSigner},
	}); err != nil {
		t.Fatal(err)
	}
	if pub.count(EventInviteCreated) != 3 || pub.last[EventInviteCreated]["invite_link"] != "https://projects.example.com/qa/"+sess.ShareCode {
		t.Fatalf("invite events: %v %v", pub.events, pub.last[EventInviteCreated])
	}
	if pub.last[EventInviteCreated]["invited_by_name"] != "Olive Owner" {
		t.Fatalf("invited_by_name: %v", pub.last[EventInviteCreated]["invited_by_name"])
	}
	// Only the signer's address is kept, sealed; testers stay hash-only.
	for _, inv := range client.QAInvite.Query().Where(qainvite.SessionID(sess.ID)).AllX(ctx) {
		if inv.Role != qamodel.RoleSigner {
			if inv.EmailEnc != "" {
				t.Fatalf("a %s invite kept an address", inv.Role)
			}
			continue
		}
		if inv.EmailEnc == "" || inv.EmailEnc == "signer@example.com" {
			t.Fatalf("signer address not sealed: %q", inv.EmailEnc)
		}
		if got, err := box.Decrypt(inv.EmailEnc); err != nil || got != "signer@example.com" {
			t.Fatalf("signer address does not open: %q %v", got, err)
		}
	}
	// A user auth-api cannot name falls back to the organisation name.
	stranger := Actor{UserID: uuid.New(), TenantID: tenant}
	if _, err := svc.Invite(ctx, stranger, sess.ID, []InviteInput{{Email: "late.tester@example.com"}}); err != nil {
		t.Fatal(err)
	}
	if got := pub.last[EventInviteCreated]["invited_by_name"]; got != "Acme Ltd" {
		t.Fatalf("fallback inviter name: %v", got)
	}
	// A signer turned observer loses the sealed address.
	if _, err := svc.Invite(ctx, owner, sess.ID, []InviteInput{{Email: "second.signer@example.com", Name: "Gone", Role: qamodel.RoleSigner}}); err != nil {
		t.Fatal(err)
	}
	moved, err := svc.Invite(ctx, owner, sess.ID, []InviteInput{{Email: "second.signer@example.com", Role: qamodel.RoleObserver}})
	if err != nil || moved[0].EmailEnc != "" {
		t.Fatalf("observer kept the sealed address: %v", err)
	}

	// A draft's link opens nothing.
	if _, err := svc.GrantByEmail(ctx, sess.ShareCode, "tester.one@example.com"); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("draft access: %v", err)
	}
	if _, err := svc.OpenSession(ctx, tenant, sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GrantByEmail(ctx, sess.ShareCode, "stranger@example.com"); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("stranger access: %v", err)
	}
	g1, err := svc.GrantByEmail(ctx, sess.ShareCode, "TESTER.ONE@example.com")
	if err != nil {
		t.Fatal(err)
	}
	g2, err := svc.GrantByOTP(ctx, sess.ShareCode, "otp-token")
	if err != nil {
		t.Fatal(err)
	}
	inv1, err := svc.ResolveAccess(ctx, g1.Token)
	if err != nil {
		t.Fatal(err)
	}
	inv2, err := svc.ResolveAccess(ctx, g2.Token)
	if err != nil {
		t.Fatal(err)
	}

	mustID, shouldID := sess.CriteriaSnapshot[0].ID, sess.CriteriaSnapshot[1].ID
	open, _ := svc.GetSession(ctx, tenant, sess.ID)
	if _, err := svc.RecordResult(ctx, open, TesterFromInvite(inv1.Invite), ResultInput{CriterionID: mustID, Result: qamodel.ResultFail}); err == nil {
		t.Fatal("a failure without severity was accepted")
	}
	r1, err := svc.RecordResult(ctx, open, TesterFromInvite(inv1.Invite), ResultInput{CriterionID: mustID, Result: qamodel.ResultFail, Severity: sev(1), Notes: "crashes"})
	if err != nil || r1.Defect == nil || r1.Defect.TicketNumber != "TKT-100" {
		t.Fatalf("severe failure: %+v %v", r1, err)
	}
	// The second tester fails the same criterion: same defect, no second ticket or alert.
	r2, err := svc.RecordResult(ctx, open, TesterFromInvite(inv2.Invite), ResultInput{CriterionID: mustID, Result: qamodel.ResultFail, Severity: sev(2)})
	if err != nil || r2.Defect == nil || r2.Defect.ID != r1.Defect.ID {
		t.Fatalf("second failure: %+v %v", r2, err)
	}
	if len(tickets.calls) != 1 || pub.count(EventResultFailed) != 1 {
		t.Fatalf("tickets %d, failure events %d; want one each", len(tickets.calls), pub.count(EventResultFailed))
	}
	if p := pub.last[EventResultFailed]; p["scope"] != "platform" || p["ticket_number"] != "TKT-100" || p["feature_code"] != "hadia_owner_registration" {
		t.Fatalf("failure payload: %v", p)
	}
	if tickets.calls[0].Priority != "critical" || tickets.calls[0].TenantID != tenant {
		t.Fatalf("ticket: %+v", tickets.calls[0])
	}
	// A minor failure is listed but raises nothing on its own.
	r3, err := svc.RecordResult(ctx, open, TesterFromInvite(inv1.Invite), ResultInput{CriterionID: shouldID, Result: qamodel.ResultFail, Severity: sev(4)})
	if err != nil || r3.Defect != nil || len(tickets.calls) != 1 {
		t.Fatalf("minor failure: %+v %v", r3, err)
	}

	// Tester one re-tests and passes: history kept, one current row.
	if _, err := svc.RecordResult(ctx, open, TesterFromInvite(inv1.Invite), ResultInput{CriterionID: mustID, Result: qamodel.ResultPass}); err != nil {
		t.Fatal(err)
	}
	n := client.QAResult.Query().Where(qaresult.SessionID(sess.ID), qaresult.CriterionID(mustID), qaresult.TesterKey(inv1.Invite.EmailHash)).CountX(ctx)
	cur := client.QAResult.Query().Where(qaresult.SessionID(sess.ID), qaresult.CriterionID(mustID), qaresult.TesterKey(inv1.Invite.EmailHash), qaresult.IsCurrent(true)).CountX(ctx)
	if n != 2 || cur != 1 {
		t.Fatalf("history %d current %d", n, cur)
	}

	// Raising the minor one by hand opens its ticket.
	d, err := svc.RaiseDefect(ctx, tenant, sess.ID, shouldID)
	if err != nil || d.Status != qamodel.DefectTicketed || len(tickets.calls) != 2 {
		t.Fatalf("raise by hand: %+v %v", d, err)
	}

	// Revoking an invite ends its access at once.
	if err := svc.RevokeInvite(ctx, tenant, sess.ID, inv2.Invite.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResolveAccess(ctx, g2.Token); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("revoked invite still works: %v", err)
	}

	// Close: tester two's severity 2 failure is still current, so the rule fails.
	closed, err := svc.CloseSession(ctx, tenant, sess.ID)
	if err != nil || closed.Status != qamodel.SessionClosed || closed.DocumentHash == "" || closed.ClosedAt == nil {
		t.Fatalf("close: %+v %v", closed, err)
	}
	if _, err := svc.RecordResult(ctx, closed, TesterFromInvite(inv1.Invite), ResultInput{CriterionID: mustID, Result: qamodel.ResultPass}); !errors.Is(err, ErrConflict) {
		t.Fatalf("result after close: %v", err)
	}
	// A request holding the session as it was before the close (status still open in memory)
	// is refused by the locked status check in the transaction, not let in.
	if _, err := svc.RecordResult(ctx, open, TesterFromInvite(inv1.Invite), ResultInput{CriterionID: mustID, Result: qamodel.ResultPass}); !errors.Is(err, ErrConflict) {
		t.Fatalf("result on a stale open session after close: %v", err)
	}
	if _, err := svc.Invite(ctx, owner, sess.ID, []InviteInput{{Email: "after.close@example.com", Role: qamodel.RoleSigner}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("invite after close: %v", err)
	}
	// The close notice names the signers with their addresses; the frozen data keeps names only.
	wantSigners := []Signer{{Email: "signer@example.com", Name: "Product owner"}}
	if got, _ := pub.last[EventSessionClosed]["signers"].([]Signer); len(got) != 1 || got[0] != wantSigners[0] {
		t.Fatalf("close signers: %#v", pub.last[EventSessionClosed]["signers"])
	}
	frozen, _ := json.Marshal(closed.Metadata["report"])
	if bytes.Contains(frozen, []byte("@example.com")) || !bytes.Contains(frozen, []byte("Product owner")) {
		t.Fatalf("frozen report data must hold signer names only: %s", frozen)
	}
	pdf, hash, err := svc.RenderReport(ctx, closed)
	sum := sha256.Sum256(pdf)
	if err != nil || hash != closed.DocumentHash || hex.EncodeToString(sum[:]) != closed.DocumentHash {
		t.Fatalf("report after close does not match its stored hash: %v", err)
	}
	if _, err := svc.DecideSession(ctx, tenant, sess.ID, true, ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("accepted against the pass rule: %v", err)
	}
	rejected, err := svc.DecideSession(ctx, tenant, sess.ID, false, "S2 failure open")
	if err != nil || rejected.Status != qamodel.SessionRejected || pub.count(EventSessionRejected) != 1 {
		t.Fatalf("reject: %+v %v", rejected, err)
	}
	if got, _ := pub.last[EventSessionRejected]["signers"].([]Signer); len(got) != 1 || got[0] != wantSigners[0] {
		t.Fatalf("rejection signers: %#v", pub.last[EventSessionRejected]["signers"])
	}
	if _, hash2, _ := svc.RenderReport(ctx, rejected); hash2 != closed.DocumentHash {
		t.Fatal("the decision changed the closed report")
	}

	// Another tenant sees nothing.
	if _, err := svc.GetSession(ctx, uuid.New(), sess.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant read: %v", err)
	}
}

// TestTicketFailureKeepsResult: when ticketing is down the verdict is saved and the defect is
// marked for a retry by hand.
func TestTicketFailureKeepsResult(t *testing.T) {
	client := testutil.Client(t)
	ctx := context.Background()
	keys, _ := NewKeys("k", "")
	tickets := &fakeTickets{fail: true}
	svc := NewService(client, keys, zap.NewNop()).WithTickets(tickets)
	owner := Actor{UserID: uuid.New(), TenantID: uuid.New()}
	tpl, _ := svc.CreateTemplate(ctx, owner, TemplateInput{Name: "T", Status: qamodel.TemplateActive})
	if _, err := svc.AddCriteria(ctx, owner, tpl.ID, []CriterionInput{{Title: "Works"}}); err != nil {
		t.Fatal(err)
	}
	sess, err := svc.CreateSession(ctx, owner, SessionInput{TemplateID: tpl.ID, Title: "S"})
	if err != nil {
		t.Fatal(err)
	}
	open, err := svc.OpenSession(ctx, owner.TenantID, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	tester, _ := svc.TesterFromEmail("member@example.com", &owner.UserID, nil)
	out, err := svc.RecordResult(ctx, open, tester, ResultInput{CriterionID: open.CriteriaSnapshot[0].ID, Result: qamodel.ResultFail, Severity: sev(1)})
	if err != nil || out.Defect == nil || out.Defect.Status != qamodel.DefectTicketFailed || out.Defect.LastError == "" {
		t.Fatalf("ticket failure: %+v %v", out, err)
	}
	tickets.fail = false
	d, err := svc.RaiseDefect(ctx, owner.TenantID, sess.ID, open.CriteriaSnapshot[0].ID)
	if err != nil || d.Status != qamodel.DefectTicketed {
		t.Fatalf("retry: %+v %v", d, err)
	}
}
