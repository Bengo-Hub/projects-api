package qa

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/qainvite"
	"github.com/bengobox/projects-service/internal/qamodel"
)

// maxInvitesPerCall bounds one invite request.
const maxInvitesPerCall = 100

// maxInvitesPerSession bounds a session's invite list (testers, signers and observers together).
const maxInvitesPerSession = 200

// InviteInput invites one email address with a role (tester, signer or observer).
type InviteInput struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

// Invite adds or updates invites on a session. Re-inviting an address updates its role and name
// and lifts a revocation. Each invite publishes project.qa.invite.created carrying the address and
// the link, for notifications-api to send. Only a signer's address is kept, sealed (see sealSigner).
func (s *Service) Invite(ctx context.Context, actor Actor, sessionID uuid.UUID, in []InviteInput) ([]*ent.QAInvite, error) {
	if len(in) == 0 || len(in) > maxInvitesPerCall {
		return nil, invalid("send between 1 and %d invites", maxInvitesPerCall)
	}
	sess, err := s.GetSession(ctx, actor.TenantID, sessionID)
	if err != nil {
		return nil, err
	}
	// Once closed the report and its signer list are frozen; a signer added later would be on the
	// notices but not in the signed report.
	if sess.Status == qamodel.SessionClosed || sess.Status == qamodel.SessionAccepted || sess.Status == qamodel.SessionRejected {
		return nil, conflict("the session is closed; invites are only accepted before it closes")
	}
	// A session's invite list is bounded, so one manager cannot turn it into a mail relay.
	existing, err := s.client.QAInvite.Query().Where(qainvite.SessionID(sessionID)).Count(ctx)
	if err != nil {
		return nil, err
	}
	if existing+len(in) > maxInvitesPerSession {
		return nil, invalid("a session can have at most %d invites (it has %d)", maxInvitesPerSession, existing)
	}
	type prepared struct {
		hash, email, name, role string
	}
	list := make([]prepared, len(in))
	for i, v := range in {
		hash, email, err := s.keys.EmailHash(v.Email)
		if err != nil {
			return nil, invalid("invite %d: a valid email address is required", i+1)
		}
		role := v.Role
		if role == "" {
			role = qamodel.RoleTester
		}
		if !qamodel.ValidRole(role) {
			return nil, invalid("invite %d: role must be tester, signer or observer", i+1)
		}
		name := strings.TrimSpace(v.Name)
		if len(name) > 200 {
			return nil, invalid("invite %d: name is longer than 200 characters", i+1)
		}
		list[i] = prepared{hash: hash, email: email, name: name, role: role}
	}
	inviter := s.inviterName(ctx, actor, sess)
	out := make([]*ent.QAInvite, 0, len(list))
	for _, p := range list {
		inv, err := s.client.QAInvite.Query().Where(qainvite.SessionID(sessionID), qainvite.EmailHash(p.hash)).Only(ctx)
		switch {
		case ent.IsNotFound(err):
			var sealed string
			if sealed, err = s.sealSigner(p.role, p.email); err != nil {
				return nil, err
			}
			c := s.client.QAInvite.Create().
				SetTenantID(actor.TenantID).SetSessionID(sessionID).SetEmailHash(p.hash).
				SetEmailHint(maskEmail(p.email)).SetName(p.name).SetRole(p.role)
			if sealed != "" {
				c = c.SetEmailEnc(sealed)
			}
			if actor.UserID != uuid.Nil {
				c = c.SetInvitedBy(actor.UserID)
			}
			inv, err = c.Save(ctx)
			if ent.IsConstraintError(err) {
				// A parallel request invited the same address first; update that one.
				inv, err = s.client.QAInvite.Query().Where(qainvite.SessionID(sessionID), qainvite.EmailHash(p.hash)).Only(ctx)
				if err == nil {
					inv, err = s.updateInvite(ctx, inv, p.name, p.role, p.email)
				}
			}
		case err == nil:
			inv, err = s.updateInvite(ctx, inv, p.name, p.role, p.email)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
		s.emit(ctx, actor.TenantID, sessionID, EventInviteCreated, s.sessionPayload(sess, map[string]any{
			"invite_id":       inv.ID.String(),
			"email":           p.email,
			"name":            p.name,
			"role":            p.role,
			"invite_link":     s.inviteLink(sess),
			"invited_by_name": inviter,
		}))
	}
	return out, nil
}

// inviterName is the inviting user's display name from auth-api, falling back to the
// organisation's name when the user has none or the lookup fails; empty when neither is known.
// It never comes from the request, so a notice cannot be sent in someone else's name.
func (s *Service) inviterName(ctx context.Context, a Actor, sess *ent.QASession) string {
	if s.directory == nil {
		return ""
	}
	if name, err := s.directory.UserName(ctx, a.TenantID, a.UserID); err == nil && strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	} else if err != nil {
		s.log.Debug("inviter name not found; using the organisation name", zap.Error(err))
	}
	if org, err := s.directory.OrgName(ctx, metaString(sess.Metadata, "tenant_slug")); err == nil {
		return strings.TrimSpace(org)
	}
	return ""
}

// sealSigner encrypts a signer's address for the close and decision notices. Testers and observers
// stay hash-only, and without a sealer signers do too (logged, so the gap is visible).
func (s *Service) sealSigner(role, email string) (string, error) {
	if role != qamodel.RoleSigner {
		return "", nil
	}
	if s.sealer == nil {
		s.log.Warn("FIELD_ENCRYPTION_KEY not set; signer address not kept, close and decision notices will not reach this signer")
		return "", nil
	}
	enc, err := s.sealer.Encrypt(email)
	if err != nil {
		return "", fmt.Errorf("seal signer email: %w", err)
	}
	return enc, nil
}

// updateInvite changes role and name and lifts a revocation. Becoming a signer seals the address;
// leaving the signer role clears it.
func (s *Service) updateInvite(ctx context.Context, inv *ent.QAInvite, name, role, email string) (*ent.QAInvite, error) {
	u := inv.Update().SetRole(role)
	if name != "" {
		u = u.SetName(name)
	}
	if inv.Status == qamodel.InviteRevoked {
		u = u.SetStatus(qamodel.InviteInvited)
	}
	sealed, err := s.sealSigner(role, email)
	if err != nil {
		return nil, err
	}
	if sealed != "" {
		u = u.SetEmailEnc(sealed)
	} else {
		u = u.ClearEmailEnc()
	}
	return u.Save(ctx)
}

// Signer is one signer as the close and decision events carry it.
type Signer struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

// signersFor opens the sealed addresses of a session's live signer invites. A signer whose address
// is not kept (no key at invite time) or cannot be opened is left out and logged.
func (s *Service) signersFor(ctx context.Context, sessionID uuid.UUID) []Signer {
	out := []Signer{}
	if s.sealer == nil {
		return out
	}
	invs, err := s.client.QAInvite.Query().
		Where(qainvite.SessionID(sessionID), qainvite.Role(qamodel.RoleSigner), qainvite.StatusNEQ(qamodel.InviteRevoked)).
		Order(ent.Asc(qainvite.FieldCreatedAt)).All(ctx)
	if err != nil {
		s.log.Warn("signers not read for the notice", zap.Error(err))
		return out
	}
	for _, inv := range invs {
		if inv.EmailEnc == "" {
			continue
		}
		email, err := s.sealer.Decrypt(inv.EmailEnc)
		if err != nil {
			s.log.Warn("signer address not opened", zap.String("invite", inv.ID.String()), zap.Error(err))
			continue
		}
		out = append(out, Signer{Email: email, Name: inv.Name})
	}
	return out
}

// ListInvites lists a session's invites (hashes and masked hints, never addresses).
func (s *Service) ListInvites(ctx context.Context, tenantID, sessionID uuid.UUID) ([]*ent.QAInvite, error) {
	if _, err := s.GetSession(ctx, tenantID, sessionID); err != nil {
		return nil, err
	}
	return s.client.QAInvite.Query().
		Where(qainvite.SessionID(sessionID), qainvite.TenantID(tenantID)).
		Order(ent.Asc(qainvite.FieldCreatedAt)).All(ctx)
}

// RevokeInvite closes an invite; any access token issued for it stops working on the next request.
func (s *Service) RevokeInvite(ctx context.Context, tenantID, sessionID, inviteID uuid.UUID) error {
	n, err := s.client.QAInvite.Update().
		Where(qainvite.ID(inviteID), qainvite.SessionID(sessionID), qainvite.TenantID(tenantID)).
		SetStatus(qamodel.InviteRevoked).Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
