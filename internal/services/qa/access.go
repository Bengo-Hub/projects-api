package qa

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/qainvite"
	"github.com/bengobox/projects-service/internal/ent/qasession"
	"github.com/bengobox/projects-service/internal/qamodel"
)

// AccessInfo is all a share link reveals before the invitee proves an email: how to sign in, and
// the tenant slug that brands the email code.
type AccessInfo struct {
	TenantSlug string   `json:"tenant_slug,omitempty"`
	Methods    []string `json:"methods"`
}

// AccessGrant is a short invitee session bound to one QA session and invite.
type AccessGrant struct {
	Token     string    `json:"access_token"`
	ExpiresAt time.Time `json:"expires_at"`
	SessionID uuid.UUID `json:"session_id"`
	Role      string    `json:"role"`
}

// Invitee is a let-in invitee: the session and the invite.
type Invitee struct {
	Session *ent.QASession
	Invite  *ent.QAInvite
}

// sessionByCode finds a session that is past draft by its share code.
func (s *Service) sessionByCode(ctx context.Context, code string) (*ent.QASession, error) {
	if code == "" || len(code) > 64 {
		return nil, ErrNotFound
	}
	sess, err := s.client.QASession.Query().Where(qasession.ShareCode(code)).Only(ctx)
	if ent.IsNotFound(err) || (err == nil && sess.Status == qamodel.SessionDraft) {
		return nil, ErrNotFound
	}
	return sess, err
}

// LookupShareCode answers the share link page.
func (s *Service) LookupShareCode(ctx context.Context, code string) (*AccessInfo, error) {
	sess, err := s.sessionByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	return &AccessInfo{TenantSlug: metaString(sess.Metadata, "tenant_slug"), Methods: []string{"sso", "email_code"}}, nil
}

// GrantByOTP lets in an invitee who proved the address with auth-api's email code. The otp_token
// is traded once through auth-api's S2S validate route.
func (s *Service) GrantByOTP(ctx context.Context, code, otpToken string) (*AccessGrant, error) {
	if s.otp == nil {
		return nil, ErrAccessDenied
	}
	email, err := s.otp.Validate(ctx, otpToken)
	if err != nil {
		s.log.Info("qa email code refused", zap.Error(err))
		return nil, ErrAccessDenied
	}
	return s.GrantByEmail(ctx, code, email)
}

// GrantByEmail lets in a proven address (a signed-in token email, or a validated email code) when
// it holds a live invite on the session behind code.
func (s *Service) GrantByEmail(ctx context.Context, code, email string) (*AccessGrant, error) {
	sess, err := s.sessionByCode(ctx, code)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrAccessDenied
	}
	if err != nil {
		return nil, err
	}
	hash, _, err := s.keys.EmailHash(email)
	if err != nil {
		return nil, ErrAccessDenied
	}
	inv, err := s.client.QAInvite.Query().
		Where(qainvite.SessionID(sess.ID), qainvite.EmailHash(hash), qainvite.StatusNEQ(qamodel.InviteRevoked)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrAccessDenied
	}
	if err != nil {
		return nil, err
	}
	now := s.now()
	if err := inv.Update().SetStatus(qamodel.InviteActive).SetLastSeenAt(now).Exec(ctx); err != nil {
		return nil, err
	}
	exp := now.Add(s.accessTTL)
	return &AccessGrant{
		Token:     s.keys.SignAccess(AccessClaims{SessionID: sess.ID, InviteID: inv.ID, Expires: exp.Unix()}),
		ExpiresAt: exp,
		SessionID: sess.ID,
		Role:      inv.Role,
	}, nil
}

// ResolveAccess checks an invitee token and that its invite is still live.
func (s *Service) ResolveAccess(ctx context.Context, token string) (*Invitee, error) {
	now := s.now()
	c, err := s.keys.VerifyAccess(token, now)
	if err != nil {
		return nil, ErrAccessDenied
	}
	inv, err := s.client.QAInvite.Query().
		Where(qainvite.ID(c.InviteID), qainvite.SessionID(c.SessionID), qainvite.StatusNEQ(qamodel.InviteRevoked)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrAccessDenied
	}
	if err != nil {
		return nil, err
	}
	sess, err := s.client.QASession.Get(ctx, c.SessionID)
	if ent.IsNotFound(err) {
		return nil, ErrAccessDenied
	}
	if err != nil {
		return nil, err
	}
	// last_seen_at is written at most once a minute, not on every request.
	if inv.LastSeenAt == nil || now.Sub(*inv.LastSeenAt) > time.Minute {
		_ = inv.Update().SetLastSeenAt(now).Exec(ctx)
	}
	return &Invitee{Session: sess, Invite: inv}, nil
}
