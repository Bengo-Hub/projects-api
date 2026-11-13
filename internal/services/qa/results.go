package qa

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/qaresult"
	"github.com/bengobox/projects-service/internal/ent/qasession"
	"github.com/bengobox/projects-service/internal/qamodel"
)

// ResultInput is one verdict on one criterion.
type ResultInput struct {
	CriterionID uuid.UUID `json:"criterion_id"`
	Result      string    `json:"result"`
	Severity    *int      `json:"severity"`
	Notes       string    `json:"notes"`
	Device      string    `json:"device"`
}

// Tester is who records a result: the keyed hash and masked hint of their email, and their user id
// or invite when known.
type Tester struct {
	Key      string
	Hint     string
	UserID   *uuid.UUID
	InviteID *uuid.UUID
}

// TesterFromEmail builds a tester from a proven email.
func (s *Service) TesterFromEmail(email string, userID *uuid.UUID, inviteID *uuid.UUID) (Tester, error) {
	hash, norm, err := s.keys.EmailHash(email)
	if err != nil {
		return Tester{}, err
	}
	return Tester{Key: hash, Hint: maskEmail(norm), UserID: userID, InviteID: inviteID}, nil
}

// TesterFromInvite builds a tester from an invitee's invite (the hash is already on it).
func TesterFromInvite(inv *ent.QAInvite) Tester {
	id := inv.ID
	return Tester{Key: inv.EmailHash, Hint: inv.EmailHint, InviteID: &id}
}

// RecordOutcome is a saved result and, for a failure, its defect.
type RecordOutcome struct {
	Result *ent.QAResult `json:"result"`
	Defect *ent.QADefect `json:"defect,omitempty"`
}

func findCriterion(sess *ent.QASession, id uuid.UUID) (qamodel.SnapshotCriterion, bool) {
	for _, c := range sess.CriteriaSnapshot {
		if c.ID == id {
			return c, true
		}
	}
	return qamodel.SnapshotCriterion{}, false
}

// RecordResult saves a verdict as the tester's current one on the criterion, keeping the previous
// one as history. A failure opens (or joins) the criterion's defect; see handleFailure.
func (s *Service) RecordResult(ctx context.Context, sess *ent.QASession, tester Tester, in ResultInput) (*RecordOutcome, error) {
	if sess.Status != qamodel.SessionOpen {
		return nil, conflict("results can only be recorded while the session is open")
	}
	crit, ok := findCriterion(sess, in.CriterionID)
	if !ok {
		return nil, invalid("criterion %s is not part of this session", in.CriterionID)
	}
	if !qamodel.ValidResult(in.Result) {
		return nil, invalid("result must be pass, fail, blocked or not_run")
	}
	if in.Result == qamodel.ResultFail {
		if in.Severity == nil || !qamodel.ValidSeverity(*in.Severity) {
			return nil, invalid("a failure needs a severity from 1 (critical) to 4 (cosmetic)")
		}
	} else {
		in.Severity = nil
	}
	in.Notes = strings.TrimSpace(in.Notes)
	in.Device = strings.TrimSpace(in.Device)
	if len(in.Notes) > 5000 || len(in.Device) > 200 {
		return nil, invalid("notes are limited to 5000 characters and device to 200")
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	// Lock the session row while it is still open: a guarded no-op UPDATE takes the row lock, so
	// CloseSession (whose open-to-closed UPDATE waits for it) can never freeze a report that misses
	// this verdict, and a session closed since sess was loaded matches nothing here.
	if n, err := tx.QASession.Update().
		Where(qasession.ID(sess.ID), qasession.TenantID(sess.TenantID), qasession.Status(qamodel.SessionOpen)).
		SetStatus(qamodel.SessionOpen).Save(ctx); err != nil {
		rollback(tx)
		return nil, err
	} else if n == 0 {
		rollback(tx)
		return nil, conflict("results can only be recorded while the session is open")
	}
	if _, err := tx.QAResult.Update().
		Where(qaresult.SessionID(sess.ID), qaresult.CriterionID(crit.ID), qaresult.TesterKey(tester.Key), qaresult.IsCurrent(true)).
		SetIsCurrent(false).Save(ctx); err != nil {
		rollback(tx)
		return nil, err
	}
	c := tx.QAResult.Create().
		SetTenantID(sess.TenantID).SetSessionID(sess.ID).SetCriterionID(crit.ID).
		SetTesterKey(tester.Key).SetTesterHint(tester.Hint).
		SetNillableTesterUserID(tester.UserID).SetNillableInviteID(tester.InviteID).
		SetResult(in.Result).SetNillableSeverity(in.Severity).
		SetNotes(in.Notes).SetDevice(in.Device)
	res, err := c.Save(ctx)
	if err != nil {
		rollback(tx)
		if ent.IsConstraintError(err) {
			return nil, conflict("another verdict for this criterion was saved at the same time; send it again")
		}
		return nil, fmt.Errorf("save result: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	out := &RecordOutcome{Result: res}
	if res.Result == qamodel.ResultFail {
		out.Defect = s.handleFailure(ctx, sess, crit, res)
		if out.Defect != nil {
			out.Result.DefectID = &out.Defect.ID
		}
	}
	return out, nil
}

// currentResults returns the session's current verdicts.
func (s *Service) currentResults(ctx context.Context, sessionID uuid.UUID) ([]*ent.QAResult, error) {
	return s.client.QAResult.Query().
		Where(qaresult.SessionID(sessionID), qaresult.IsCurrent(true)).
		Order(ent.Asc(qaresult.FieldCreatedAt)).All(ctx)
}

// ListResults lists a session's verdicts: the current ones, or every one when history is set.
func (s *Service) ListResults(ctx context.Context, tenantID, sessionID uuid.UUID, history bool) ([]*ent.QAResult, error) {
	if _, err := s.GetSession(ctx, tenantID, sessionID); err != nil {
		return nil, err
	}
	q := s.client.QAResult.Query().Where(qaresult.SessionID(sessionID), qaresult.TenantID(tenantID))
	if !history {
		q = q.Where(qaresult.IsCurrent(true))
	}
	return q.Order(ent.Asc(qaresult.FieldCreatedAt)).All(ctx)
}

// TesterResults is one tester's current verdicts on a session (the invitee form).
func (s *Service) TesterResults(ctx context.Context, sessionID uuid.UUID, testerKey string) ([]*ent.QAResult, error) {
	return s.client.QAResult.Query().
		Where(qaresult.SessionID(sessionID), qaresult.TesterKey(testerKey), qaresult.IsCurrent(true)).
		All(ctx)
}
