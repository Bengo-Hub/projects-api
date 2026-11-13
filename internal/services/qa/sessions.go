package qa

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/project"
	"github.com/bengobox/projects-service/internal/ent/qasession"
	"github.com/bengobox/projects-service/internal/qamodel"
)

// SessionInput creates a session from a template. PassRule, when sent, may only tighten the
// template's rule. TenantName and TenantSlug brand the report and the invitee's email code.
type SessionInput struct {
	TemplateID  uuid.UUID         `json:"template_id"`
	Title       string            `json:"title"`
	SubjectKind string            `json:"subject_kind"`
	SubjectRef  string            `json:"subject_ref"`
	PassRule    *qamodel.PassRule `json:"pass_rule"`
	TenantName  string            `json:"tenant_name"`
	TenantSlug  string            `json:"-"`
}

// SessionFilter narrows the session list.
type SessionFilter struct {
	Status string
	Limit  int
	Offset int
}

// templateRule is the template's rule, or the default for a template saved without one.
func templateRule(t *ent.QATemplate) qamodel.PassRule {
	if len(t.PassRule.Weights) == 0 {
		return qamodel.DefaultPassRule()
	}
	return t.PassRule
}

// CreateSession freezes the template's criteria and rule into a new draft session.
func (s *Service) CreateSession(ctx context.Context, actor Actor, in SessionInput) (*ent.QASession, error) {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" || len(in.Title) > 200 {
		return nil, invalid("title is required (at most 200 characters)")
	}
	if in.SubjectKind == "" {
		in.SubjectKind = qamodel.SessionSubjectGeneral
	}
	if !qamodel.ValidSessionSubject(in.SubjectKind) {
		return nil, invalid("subject_kind must be service, project or general")
	}
	in.SubjectRef = strings.TrimSpace(in.SubjectRef)
	switch in.SubjectKind {
	case qamodel.SessionSubjectService:
		in.SubjectRef = strings.ToLower(in.SubjectRef)
		if !serviceTagRe.MatchString(in.SubjectRef) {
			return nil, invalid("a service session needs the catalogue service tag in subject_ref")
		}
	case qamodel.SessionSubjectProject:
		pid, err := uuid.Parse(in.SubjectRef)
		if err != nil {
			return nil, invalid("subject_ref must be the project id")
		}
		ok, err := s.client.Project.Query().Where(project.ID(pid), project.TenantID(actor.TenantID)).Exist(ctx)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, invalid("project %s was not found", pid)
		}
		in.SubjectRef = pid.String()
	default:
		in.SubjectRef = ""
	}

	t, err := s.GetTemplate(ctx, actor, in.TemplateID)
	if err != nil {
		return nil, err
	}
	if t.Status == qamodel.TemplateArchived {
		return nil, conflict("the template is archived")
	}
	crits := t.Edges.Criteria
	if len(crits) == 0 {
		return nil, invalid("the template has no criteria")
	}
	snapshot := make([]qamodel.SnapshotCriterion, len(crits))
	for i, c := range crits {
		snapshot[i] = qamodel.SnapshotCriterion{
			ID: c.ID, AreaCode: c.AreaCode, Title: c.Title, SubjectKind: c.SubjectKind, SubjectRef: c.SubjectRef,
			RequirementRef: c.RequirementRef, Preconditions: c.Preconditions, Steps: c.Steps,
			ExpectedResult: c.ExpectedResult, Weight: c.Weight, AutomatedRef: c.AutomatedRef, SortOrder: c.SortOrder,
		}
	}
	rule := templateRule(t)
	if in.PassRule != nil {
		next, err := in.PassRule.Normalize()
		if err != nil {
			return nil, invalid("pass_rule: %v", err)
		}
		if err := rule.CheckTightens(next); err != nil {
			return nil, invalid("pass_rule: %v", err)
		}
		rule = next
	}
	code, err := newShareCode()
	if err != nil {
		return nil, err
	}
	// The organisation name on the report and in every email comes from auth-api, never from the
	// request: a body value would let a tenant send invitations in another organisation's name
	// from the Codevertex mailer. tenant_name in the body is accepted and ignored.
	orgName := in.TenantSlug
	if s.directory != nil && in.TenantSlug != "" {
		if n, err := s.directory.OrgName(ctx, in.TenantSlug); err == nil && strings.TrimSpace(n) != "" {
			orgName = strings.TrimSpace(n)
		}
	}
	// Platform scope (Codevertex ops alerts) only for a service session a platform owner opens;
	// a tenant testing a service it uses keeps its alerts in its own team.
	sessScope := "tenant"
	if in.SubjectKind == qamodel.SessionSubjectService && actor.PlatformOwner {
		sessScope = "platform"
	}
	meta := map[string]any{"tenant_slug": in.TenantSlug, "tenant_name": orgName, "template_name": t.Name, "scope": sessScope}
	return s.client.QASession.Create().
		SetTenantID(actor.TenantID).SetTemplateID(t.ID).SetTemplateVersion(t.Version).
		SetTitle(in.Title).SetSubjectKind(in.SubjectKind).SetSubjectRef(in.SubjectRef).
		SetCriteriaSnapshot(snapshot).SetPassRule(rule).SetShareCode(code).
		SetOwnerID(actor.UserID).SetMetadata(meta).
		Save(ctx)
}

// ListSessions lists the tenant's sessions, newest first.
func (s *Service) ListSessions(ctx context.Context, tenantID uuid.UUID, f SessionFilter) ([]*ent.QASession, int, error) {
	q := s.client.QASession.Query().Where(qasession.TenantID(tenantID))
	if f.Status != "" {
		q = q.Where(qasession.Status(f.Status))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count sessions: %w", err)
	}
	if f.Limit <= 0 {
		f.Limit = 20
	}
	items, err := q.Order(ent.Desc(qasession.FieldCreatedAt)).Offset(f.Offset).Limit(f.Limit).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list sessions: %w", err)
	}
	return items, total, nil
}

// GetSession returns one of the tenant's sessions.
func (s *Service) GetSession(ctx context.Context, tenantID, id uuid.UUID) (*ent.QASession, error) {
	sess, err := s.client.QASession.Query().Where(qasession.ID(id), qasession.TenantID(tenantID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	return sess, err
}

// transition moves a session from one status to another; the WHERE on the old status makes two
// racing requests change it once.
func (s *Service) transition(ctx context.Context, tenantID, id uuid.UUID, from, to string, set func(*ent.QASessionUpdate) *ent.QASessionUpdate) error {
	u := s.client.QASession.Update().Where(qasession.ID(id), qasession.TenantID(tenantID), qasession.Status(from)).SetStatus(to)
	if set != nil {
		u = set(u)
	}
	n, err := u.Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		if _, err := s.GetSession(ctx, tenantID, id); err != nil {
			return err
		}
		return conflict(fmt.Sprintf("the session is not %s", from))
	}
	return nil
}

// OpenSession lets invitees in and results be recorded.
func (s *Service) OpenSession(ctx context.Context, tenantID, id uuid.UUID) (*ent.QASession, error) {
	now := s.now().UTC()
	if err := s.transition(ctx, tenantID, id, qamodel.SessionDraft, qamodel.SessionOpen, func(u *ent.QASessionUpdate) *ent.QASessionUpdate {
		return u.SetOpenedAt(now)
	}); err != nil {
		return nil, err
	}
	return s.GetSession(ctx, tenantID, id)
}

// currentViews returns the session's current results in the pass rule's shape.
func (s *Service) currentViews(ctx context.Context, sessionID uuid.UUID) ([]ResultView, []*ent.QAResult, error) {
	rows, err := s.currentResults(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	views := make([]ResultView, len(rows))
	for i, r := range rows {
		views[i] = ResultView{CriterionID: r.CriterionID, Result: r.Result, Severity: r.Severity}
	}
	return views, rows, nil
}

// Outcome is the pass rule over the session's current results (live board, or the frozen one
// after close).
func (s *Service) Outcome(ctx context.Context, tenantID, id uuid.UUID) (*Outcome, error) {
	sess, err := s.GetSession(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if data, ok := frozenReport(sess); ok {
		return &data.Outcome, nil
	}
	views, _, err := s.currentViews(ctx, id)
	if err != nil {
		return nil, err
	}
	o := Evaluate(sess.PassRule, sess.CriteriaSnapshot, views)
	return &o, nil
}

// CloseSession stops recording, freezes the outcome, defects and signers, renders the report and
// keeps its SHA-256 on the session. The frozen data is what every later render uses, so the hash
// can be checked by rendering again.
func (s *Service) CloseSession(ctx context.Context, tenantID, id uuid.UUID) (*ent.QASession, error) {
	sess, err := s.GetSession(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if sess.Status != qamodel.SessionOpen {
		return nil, conflict("only an open session can be closed")
	}
	closedAt := s.now().UTC().Truncate(time.Second)
	// Stop recording first. This guarded UPDATE waits for any RecordResult transaction holding the
	// row (results.go), and afterwards no new verdict can be saved, so the data read below is final.
	if err := s.transition(ctx, tenantID, id, qamodel.SessionOpen, qamodel.SessionClosed, func(u *ent.QASessionUpdate) *ent.QASessionUpdate {
		return u.SetClosedAt(closedAt)
	}); err != nil {
		return nil, err
	}
	// reopen puts the session back when the report cannot be frozen, so testing can continue and
	// the close can be tried again.
	reopen := func(cause error) (*ent.QASession, error) {
		if err := s.transition(ctx, tenantID, id, qamodel.SessionClosed, qamodel.SessionOpen, func(u *ent.QASessionUpdate) *ent.QASessionUpdate {
			return u.ClearClosedAt()
		}); err != nil {
			s.log.Error("qa: reopening a session whose report failed", zap.String("session_id", id.String()), zap.Error(err))
		}
		return nil, cause
	}
	sess.Status, sess.ClosedAt = qamodel.SessionClosed, &closedAt
	data, err := s.liveReportData(ctx, sess)
	if err != nil {
		return reopen(err)
	}
	data.ClosedAt = closedAt
	pdf, err := renderReport(sess, data)
	if err != nil {
		return reopen(err)
	}
	sum := sha256.Sum256(pdf)
	hash := hex.EncodeToString(sum[:])
	frozen, err := toMap(data)
	if err != nil {
		return reopen(err)
	}
	meta := sess.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	meta["report"] = frozen
	if err := s.client.QASession.UpdateOneID(id).Where(qasession.TenantID(tenantID), qasession.Status(qamodel.SessionClosed)).
		SetDocumentHash(hash).SetMetadata(meta).Exec(ctx); err != nil {
		return reopen(err)
	}
	out, err := s.GetSession(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	s.emit(ctx, tenantID, id, EventSessionClosed, s.sessionPayload(out, map[string]any{
		"accepted": data.Outcome.Accepted, "score": data.Outcome.Score, "document_hash": hash,
		"signers": s.signersFor(ctx, id),
	}))
	return out, nil
}

// DecideSession records the decision on a closed session. Acceptance is refused while the pass
// rule fails; rejection is always possible. (Once stored signatures land, acceptance follows the
// last signature instead.)
func (s *Service) DecideSession(ctx context.Context, tenantID, id uuid.UUID, accept bool, reason string) (*ent.QASession, error) {
	sess, err := s.GetSession(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	data, ok := frozenReport(sess)
	if sess.Status != qamodel.SessionClosed || !ok {
		return nil, conflict("only a closed session can be accepted or rejected")
	}
	if accept && !data.Outcome.Accepted {
		return nil, conflict("the pass rule is not met: " + strings.Join(data.Outcome.Reasons, "; "))
	}
	reason = strings.TrimSpace(reason)
	if len(reason) > 2000 {
		return nil, invalid("reason is longer than 2000 characters")
	}
	to, event := qamodel.SessionRejected, EventSessionRejected
	if accept {
		to, event = qamodel.SessionAccepted, EventSessionAccepted
	}
	meta := sess.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	meta["decision"] = map[string]any{"status": to, "reason": reason, "at": s.now().UTC().Format(time.RFC3339)}
	if err := s.transition(ctx, tenantID, id, qamodel.SessionClosed, to, func(u *ent.QASessionUpdate) *ent.QASessionUpdate {
		return u.SetMetadata(meta)
	}); err != nil {
		return nil, err
	}
	out, err := s.GetSession(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	s.emit(ctx, tenantID, id, event, s.sessionPayload(out, map[string]any{
		"reason": reason, "document_hash": out.DocumentHash, "signers": s.signersFor(ctx, id),
	}))
	return out, nil
}

// scope says where a session's alerts go: "platform" (Codevertex ops) only for a service session
// a platform owner opened, recorded on the session when it was created; anything else is the
// tenant's own (its owner and team).
func scope(sess *ent.QASession) string {
	if sess.SubjectKind == qamodel.SessionSubjectService && metaString(sess.Metadata, "scope") == "platform" {
		return "platform"
	}
	return "tenant"
}

func metaString(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

// sessionPayload is the common part of every project.qa.* event.
func (s *Service) sessionPayload(sess *ent.QASession, extra map[string]any) map[string]any {
	p := map[string]any{
		"session_id":    sess.ID.String(),
		"tenant_id":     sess.TenantID.String(),
		"tenant_slug":   metaString(sess.Metadata, "tenant_slug"),
		"tenant_name":   metaString(sess.Metadata, "tenant_name"),
		"session_title": sess.Title,
		"status":        sess.Status,
		"scope":         scope(sess),
		"subject_kind":  sess.SubjectKind,
		"subject_ref":   sess.SubjectRef,
		"owner_id":      sess.OwnerID.String(),
		"link":          s.ownerLink(sess),
	}
	for k, v := range extra {
		p[k] = v
	}
	return p
}

func (s *Service) ownerLink(sess *ent.QASession) string {
	if s.uiBaseURL == "" {
		return ""
	}
	return strings.TrimRight(s.uiBaseURL, "/") + "/qa/sessions/" + sess.ID.String()
}

func (s *Service) inviteLink(sess *ent.QASession) string {
	if s.uiBaseURL == "" {
		return ""
	}
	return strings.TrimRight(s.uiBaseURL, "/") + "/qa/" + sess.ShareCode
}

func toMap(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	return m, json.Unmarshal(b, &m)
}
