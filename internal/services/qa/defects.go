package qa

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/qadefect"
	"github.com/bengobox/projects-service/internal/ent/qaresult"
	"github.com/bengobox/projects-service/internal/platform/ticketing"
	"github.com/bengobox/projects-service/internal/qamodel"
)

// autoTicket: severity 1 and 2 failures open a ticket and an alert on their own; 3 and 4 are
// listed and raised by hand.
func autoTicket(sev int) bool { return sev <= qamodel.SeverityMajor }

// ticketPriority maps a severity to ticketing's priorities.
func ticketPriority(sev int) string {
	switch sev {
	case qamodel.SeverityCritical:
		return "critical"
	case qamodel.SeverityMajor:
		return "high"
	case qamodel.SeverityMinor:
		return "normal"
	default:
		return "low"
	}
}

// handleFailure handles a saved failure. A severity 1 or 2 failure gets the criterion's defect
// (one per session and criterion, whichever tester failed it first), a ticket in ticketing-api,
// and one project.qa.result.failed event for notifications-api to route. Later failures of the
// same criterion join the defect (raising its severity when worse) without a second ticket or
// alert. Problems here are logged, never returned: the verdict is already saved.
func (s *Service) handleFailure(ctx context.Context, sess *ent.QASession, crit qamodel.SnapshotCriterion, res *ent.QAResult) *ent.QADefect {
	if res.Severity == nil || !autoTicket(*res.Severity) {
		return nil
	}
	d, created, err := s.ensureDefect(ctx, sess, crit.ID, res)
	if err != nil {
		s.log.Warn("qa defect not recorded", zap.String("session", sess.ID.String()), zap.Error(err))
		return nil
	}
	if err := s.client.QAResult.UpdateOneID(res.ID).SetDefectID(d.ID).Exec(ctx); err != nil {
		s.log.Warn("qa result not linked to its defect", zap.Error(err))
	}
	if !created {
		return d
	}
	d = s.openTicket(ctx, sess, crit, d, res)
	s.emit(ctx, sess.TenantID, sess.ID, EventResultFailed, s.failurePayload(sess, crit, d, res, false))
	return d
}

// ensureDefect returns the criterion's defect, creating it on the first failure. The unique
// (session, criterion) index settles two testers failing it at once.
func (s *Service) ensureDefect(ctx context.Context, sess *ent.QASession, criterionID uuid.UUID, res *ent.QAResult) (*ent.QADefect, bool, error) {
	sev := qamodel.SeverityMajor
	if res.Severity != nil {
		sev = *res.Severity
	}
	d, err := s.client.QADefect.Create().
		SetTenantID(sess.TenantID).SetSessionID(sess.ID).SetCriterionID(criterionID).
		SetFirstResultID(res.ID).SetSeverity(sev).
		Save(ctx)
	if err == nil {
		return d, true, nil
	}
	if !ent.IsConstraintError(err) {
		return nil, false, err
	}
	d, err = s.client.QADefect.Query().Where(qadefect.SessionID(sess.ID), qadefect.CriterionID(criterionID)).Only(ctx)
	if err != nil {
		return nil, false, err
	}
	if sev < d.Severity {
		if d, err = d.Update().SetSeverity(sev).Save(ctx); err != nil {
			return nil, false, err
		}
	}
	return d, false, nil
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := 0
	for i := range s { // i walks rune starts, so the cut never splits a character
		if i > n-3 {
			break
		}
		cut = i
	}
	return s[:cut] + "..."
}

// openTicket raises the defect's ticket and records the outcome on the defect.
func (s *Service) openTicket(ctx context.Context, sess *ent.QASession, crit qamodel.SnapshotCriterion, d *ent.QADefect, res *ent.QAResult) *ent.QADefect {
	if s.tickets == nil {
		return s.markTicketFailed(ctx, d, "ticketing is not configured")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "QA session: %s\n", sess.Title)
	if crit.AreaCode != "" || crit.RequirementRef != "" {
		fmt.Fprintf(&b, "Area: %s  Requirement: %s\n", crit.AreaCode, crit.RequirementRef)
	}
	fmt.Fprintf(&b, "Criterion: %s\nSeverity: %d\n", crit.Title, d.Severity)
	if crit.Steps != "" {
		fmt.Fprintf(&b, "\nSteps:\n%s\n", crit.Steps)
	}
	if crit.ExpectedResult != "" {
		fmt.Fprintf(&b, "\nExpected:\n%s\n", crit.ExpectedResult)
	}
	if res.Notes != "" {
		fmt.Fprintf(&b, "\nTester notes (%s):\n%s\n", res.TesterHint, res.Notes)
	}
	if res.Device != "" {
		fmt.Fprintf(&b, "Device: %s\n", res.Device)
	}
	if link := s.ownerLink(sess); link != "" {
		fmt.Fprintf(&b, "\nSession: %s\n", link)
	}
	fmt.Fprintf(&b, "Result: %s  Defect: %s\n", res.ID, d.ID)

	t, err := s.tickets.CreateTicket(ctx, ticketing.CreateTicketInput{
		TenantID:    sess.TenantID,
		Title:       clip(fmt.Sprintf("[QA S%d] %s: %s", d.Severity, sess.Title, crit.Title), 200),
		Description: clip(b.String(), 20000),
		Priority:    ticketPriority(d.Severity),
		Category:    "qa",
		SessionID:   sess.ID.String(),
	})
	if err != nil {
		s.log.Warn("qa ticket not opened", zap.String("defect", d.ID.String()), zap.Error(err))
		return s.markTicketFailed(ctx, d, err.Error())
	}
	upd, err := d.Update().SetStatus(qamodel.DefectTicketed).SetTicketID(t.ID).SetTicketNumber(t.TicketNumber).SetLastError("").Save(ctx)
	if err != nil {
		s.log.Warn("qa ticket opened but not recorded", zap.String("ticket", t.TicketNumber), zap.Error(err))
		return d
	}
	return upd
}

func (s *Service) markTicketFailed(ctx context.Context, d *ent.QADefect, msg string) *ent.QADefect {
	upd, err := d.Update().SetStatus(qamodel.DefectTicketFailed).SetLastError(clip(msg, 500)).Save(ctx)
	if err != nil {
		return d
	}
	return upd
}

// failurePayload is project.qa.result.failed. scope "platform" goes to Codevertex ops through the
// platform alert settings; "tenant" goes to the session owner and the tenant's team.
func (s *Service) failurePayload(sess *ent.QASession, crit qamodel.SnapshotCriterion, d *ent.QADefect, res *ent.QAResult, byHand bool) map[string]any {
	return s.sessionPayload(sess, map[string]any{
		"criterion_id":    crit.ID.String(),
		"criterion_title": crit.Title,
		"area_code":       crit.AreaCode,
		"requirement_ref": crit.RequirementRef,
		"feature_code":    featureCode(crit),
		"severity":        d.Severity,
		"result_id":       res.ID.String(),
		"defect_id":       d.ID.String(),
		"ticket_number":   d.TicketNumber,
		"ticket_status":   d.Status,
		"tester_hint":     res.TesterHint,
		"raised_by_hand":  byHand,
	})
}

func featureCode(c qamodel.SnapshotCriterion) string {
	if c.SubjectKind == qamodel.SubjectFeature {
		return c.SubjectRef
	}
	return ""
}

// RaiseDefect raises a ticket by hand for a criterion that is currently failing (any severity),
// or retries a ticket that failed to open. A defect that already has its ticket is returned as is.
func (s *Service) RaiseDefect(ctx context.Context, tenantID, sessionID, criterionID uuid.UUID) (*ent.QADefect, error) {
	sess, err := s.GetSession(ctx, tenantID, sessionID)
	if err != nil {
		return nil, err
	}
	crit, ok := findCriterion(sess, criterionID)
	if !ok {
		return nil, invalid("criterion %s is not part of this session", criterionID)
	}
	res, err := s.client.QAResult.Query().
		Where(qaresult.SessionID(sessionID), qaresult.CriterionID(criterionID), qaresult.IsCurrent(true), qaresult.Result(qamodel.ResultFail)).
		Order(ent.Asc(qaresult.FieldSeverity), ent.Asc(qaresult.FieldCreatedAt)).First(ctx)
	if ent.IsNotFound(err) {
		return nil, conflict("the criterion has no current failure")
	}
	if err != nil {
		return nil, err
	}
	d, created, err := s.ensureDefect(ctx, sess, criterionID, res)
	if err != nil {
		return nil, err
	}
	if res.DefectID == nil {
		_ = s.client.QAResult.UpdateOneID(res.ID).SetDefectID(d.ID).Exec(ctx)
	}
	if d.Status == qamodel.DefectTicketed {
		return d, nil
	}
	d = s.openTicket(ctx, sess, crit, d, res)
	if created && autoTicket(d.Severity) {
		s.emit(ctx, tenantID, sessionID, EventResultFailed, s.failurePayload(sess, crit, d, res, true))
	}
	return d, nil
}

// ListDefects lists a session's defects, most severe first.
func (s *Service) ListDefects(ctx context.Context, tenantID, sessionID uuid.UUID) ([]*ent.QADefect, error) {
	if _, err := s.GetSession(ctx, tenantID, sessionID); err != nil {
		return nil, err
	}
	return s.client.QADefect.Query().
		Where(qadefect.SessionID(sessionID), qadefect.TenantID(tenantID)).
		Order(ent.Asc(qadefect.FieldSeverity), ent.Asc(qadefect.FieldCreatedAt)).All(ctx)
}
