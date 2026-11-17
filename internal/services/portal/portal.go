// Package portal is the client portal (sprint 14): a firm invites its client to a project, the
// client signs in through auth-api's projects-portal client (global role client, projects role
// client) and follows the project's progress, shared tasks and files, milestones and the client
// thread.
//
// A client is a project_members row with role_code client. Its metadata holds the CRM contact id
// (marketflow owns the person; projects keeps no contact record), a display snapshot of the
// name, the keyed hash of the invited address with a masked hint, and the portal status:
//
//   - invited: user_id is uuid.Nil until the client first signs in. An invite is matched by the
//     keyed hash of the SSO email, case-insensitive, within the tenant of the request only.
//   - active: user_id is the client's auth-api user id.
//
// Client rows never count as team membership (membership.Visible, notify.Members, the team routes),
// so they open the portal routes and nothing else.
package portal

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/predicate"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	entmember "github.com/bengobox/projects-service/internal/ent/projectmember"
	"github.com/bengobox/projects-service/internal/modules/rbac"
	"github.com/bengobox/projects-service/internal/platform/events"
	"github.com/bengobox/projects-service/internal/platform/marketflow"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/notify"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// Client member metadata keys.
const (
	MetaStatus       = "portal_status"
	MetaCRMContactID = notify.MetaCRMContactID
	MetaContactName  = notify.MetaContactName
	MetaEmailHash    = "email_hash"
	MetaEmailHint    = "email_hint"
	MetaInvitedBy    = "invited_by"
	MetaInvitedAt    = "invited_at"
	MetaActivatedAt  = "activated_at"

	StatusInvited = "invited"
	StatusActive  = "active"
)

// Errors answered by the handlers.
var (
	ErrNotFound        = errors.New("not found")
	ErrContactNotFound = errors.New("the CRM contact was not found")
	ErrCRMUnavailable  = errors.New("the CRM cannot be reached right now")
	ErrAlreadyClient   = errors.New("this contact is already a client of the project")
)

// ValidationError is a refused input, answered 422.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

// Contacts reads one CRM contact; *marketflow.Client implements it.
type Contacts interface {
	GetContact(ctx context.Context, tenantID, contactID uuid.UUID) (marketflow.Contact, error)
}

// EmailHasher keys email hashes; *qa.Keys implements it (QA_HASH_KEY), so an invite never stores
// the address itself.
type EmailHasher interface {
	EmailHash(raw string) (hash, email string, err error)
}

// Service runs invites, activation and the portal reads.
type Service struct {
	client   *ent.Client
	crm      Contacts
	hasher   EmailHasher
	verifier EmailVerifier
	log      *zap.Logger
	changed  func()
}

// EmailVerifier answers the signed-in user's email as auth-api knows it and whether it is proven.
type EmailVerifier interface {
	VerifiedEmail(ctx context.Context, userID uuid.UUID) (email string, verified bool, err error)
}

// ErrEmailNotVerified means invites are waiting for the caller's address, but auth-api has not
// proven that the caller owns it, so none is attached: a self-registered account cannot claim
// someone else's invite by typing their email.
var ErrEmailNotVerified = errors.New("verify your email address to open the projects shared with you")

// WithEmailVerifier sets the check run before an invite is attached. Without one, invites are never
// attached (fail closed).
func (s *Service) WithEmailVerifier(v EmailVerifier) *Service {
	s.verifier = v
	return s
}

// NewService builds the portal service.
func NewService(client *ent.Client, crm Contacts, hasher EmailHasher, log *zap.Logger) *Service {
	return &Service{client: client, crm: crm, hasher: hasher, log: log.Named("portal.svc"), changed: func() {}}
}

// OnChange registers a callback run after a client row is attached to an account or revoked (the
// access layer drops its cached membership answers).
func (s *Service) OnChange(fn func()) { s.changed = fn }

// Client is a project's client as the team sees it.
type Client struct {
	ID           uuid.UUID  `json:"id"`
	ProjectID    uuid.UUID  `json:"project_id"`
	UserID       *uuid.UUID `json:"user_id"`
	CRMContactID string     `json:"crm_contact_id"`
	Name         string     `json:"name"`
	EmailHint    string     `json:"email_hint"`
	Status       string     `json:"status"`
	InvitedAt    string     `json:"invited_at,omitempty"`
	ActivatedAt  string     `json:"activated_at,omitempty"`
}

func str(meta map[string]any, k string) string {
	v, _ := meta[k].(string)
	return v
}

func clientView(m *ent.ProjectMember) Client {
	c := Client{
		ID: m.ID, ProjectID: m.ProjectID, CRMContactID: str(m.Metadata, MetaCRMContactID),
		Name: str(m.Metadata, MetaContactName), EmailHint: str(m.Metadata, MetaEmailHint),
		Status: str(m.Metadata, MetaStatus), InvitedAt: str(m.Metadata, MetaInvitedAt),
		ActivatedAt: str(m.Metadata, MetaActivatedAt),
	}
	if m.UserID != uuid.Nil {
		id := m.UserID
		c.UserID = &id
	}
	if c.Status == "" {
		c.Status = StatusInvited
		if m.UserID != uuid.Nil {
			c.Status = StatusActive
		}
	}
	return c
}

// clientRow matches the live client rows of a tenant.
func clientRow(tenantID uuid.UUID) predicate.ProjectMember {
	return entmember.And(entmember.TenantID(tenantID), entmember.RoleCode(rbac.RoleClient), entmember.LeftAtIsNil())
}

// metaEQ matches project_members whose metadata key holds value.
func metaEQ(key, value string) predicate.ProjectMember {
	return predicate.ProjectMember(func(s *sql.Selector) {
		s.Where(sqljson.ValueEQ(s.C(entmember.FieldMetadata), value, sqljson.Path(key)))
	})
}

// maskEmail keeps the first letter of the local part and the domain: "j***@example.com".
func maskEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return "***"
	}
	return email[:1] + "***" + email[at:]
}

// Invite makes the CRM contact a client of the project and writes project.client.invited for the
// email and WhatsApp invite. The contact's name, email and phone are read from marketflow and go
// into the event only; the row keeps the contact id, a name snapshot for the team's list, and the
// keyed hash and masked hint of the address. Inviting a contact still at the invite stage sends the
// invite again (resent true); an active client answers ErrAlreadyClient.
func (s *Service) Invite(ctx context.Context, tenantID, projectID, contactID, actorID uuid.UUID) (Client, bool, error) {
	if contactID == uuid.Nil {
		return Client{}, false, ValidationError("crm_contact_id is required")
	}
	contact, err := s.crm.GetContact(ctx, tenantID, contactID)
	switch {
	case errors.Is(err, marketflow.ErrContactNotFound):
		return Client{}, false, ErrContactNotFound
	case errors.Is(err, marketflow.ErrNotConfigured):
		return Client{}, false, ErrCRMUnavailable
	case err != nil:
		s.log.Warn("crm contact read failed", zap.String("contact_id", contactID.String()), zap.Error(err))
		return Client{}, false, ErrCRMUnavailable
	}
	if contact.Erased() {
		return Client{}, false, ErrContactNotFound
	}
	if strings.TrimSpace(contact.Email) == "" {
		return Client{}, false, ValidationError("the contact has no email address in the CRM; the client signs in with it")
	}
	hash, email, err := s.hasher.EmailHash(contact.Email)
	if err != nil {
		return Client{}, false, ValidationError("the contact's email address in the CRM is not valid")
	}
	var row *ent.ProjectMember
	resent := false
	err = txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		p, err := tx.Project.Query().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).Only(ctx)
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("load project: %w", err)
		}
		// One advisory lock per (project, address) makes two racing invites write one row.
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
			"portal-invite:"+projectID.String()+":"+hash); err != nil {
			return fmt.Errorf("lock invite: %w", err)
		}
		now := time.Now().UTC().Format(time.RFC3339)
		existing, err := tx.ProjectMember.Query().
			Where(clientRow(tenantID), entmember.ProjectID(projectID),
				entmember.Or(metaEQ(MetaEmailHash, hash), metaEQ(MetaCRMContactID, contactID.String()))).
			First(ctx)
		switch {
		case err == nil && existing.UserID != uuid.Nil:
			return ErrAlreadyClient
		case err == nil:
			meta := make(map[string]any, len(existing.Metadata)+1)
			maps.Copy(meta, existing.Metadata)
			meta[MetaInvitedAt], meta[MetaInvitedBy], meta[MetaContactName] = now, actorID.String(), contact.Name()
			if row, err = existing.Update().SetMetadata(meta).Save(ctx); err != nil {
				return fmt.Errorf("refresh invite: %w", err)
			}
			resent = true
		case ent.IsNotFound(err):
			if row, err = tx.ProjectMember.Create().
				SetTenantID(tenantID).SetProjectID(projectID).SetUserID(uuid.Nil).SetRoleCode(rbac.RoleClient).
				SetMetadata(map[string]any{
					MetaStatus: StatusInvited, MetaCRMContactID: contactID.String(), MetaContactName: contact.Name(),
					MetaEmailHash: hash, MetaEmailHint: maskEmail(email), MetaInvitedBy: actorID.String(), MetaInvitedAt: now,
				}).Save(ctx); err != nil {
				return fmt.Errorf("create invite: %w", err)
			}
		default:
			return fmt.Errorf("find invite: %w", err)
		}
		action := activity.ActionCreated
		if resent {
			action = activity.ActionUpdated
		}
		if err := activity.Record(ctx, tx, activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(projectID), EntityType: "client", EntityID: row.ID,
			Action: action, Changes: map[string]any{"crm_contact_id": contactID.String(), "resent": resent},
		}); err != nil {
			return err
		}
		actor, err := notify.Actor(ctx, tx, tenantID, actorID)
		if err != nil {
			return err
		}
		org := notify.OrgSlug(ctx)
		return events.Write(ctx, tx, events.AggregateProject, notify.EventClientInvited, projectID, tenantID, map[string]any{
			"project_id":      projectID.String(),
			"project_name":    p.Name,
			"member_id":       row.ID.String(),
			"tenant_slug":     org,
			"portal_path":     notify.PortalPath(ctx, uuid.Nil, uuid.Nil),
			"action_path":     notify.PortalPath(ctx, projectID, uuid.Nil),
			"actor_id":        actorID.String(),
			"actor_name":      actor.DisplayName(),
			"recipient_name":  contact.Name(),
			"recipient_email": email,
			"recipient_phone": strings.TrimSpace(contact.Phone),
			"resent":          resent,
		})
	})
	if err != nil {
		return Client{}, false, err
	}
	return clientView(row), resent, nil
}

// ListClients returns the project's clients, invited and active, oldest invite first.
func (s *Service) ListClients(ctx context.Context, tenantID, projectID uuid.UUID) ([]Client, error) {
	rows, err := s.client.ProjectMember.Query().
		Where(clientRow(tenantID), entmember.ProjectID(projectID)).
		Order(ent.Asc(entmember.FieldJoinedAt), ent.Asc(entmember.FieldID)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list clients: %w", err)
	}
	out := make([]Client, len(rows))
	for i, m := range rows {
		out[i] = clientView(m)
	}
	return out, nil
}

// Revoke removes a client (invited or active) from the project: their portal loses it at once.
func (s *Service) Revoke(ctx context.Context, tenantID, projectID, memberID uuid.UUID) error {
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		m, err := tx.ProjectMember.Query().
			Where(entmember.ID(memberID), clientRow(tenantID), entmember.ProjectID(projectID)).Only(ctx)
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("find client: %w", err)
		}
		if err := tx.ProjectMember.DeleteOne(m).Exec(ctx); err != nil {
			return fmt.Errorf("revoke client: %w", err)
		}
		return activity.Record(ctx, tx, activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(projectID), EntityType: "client", EntityID: m.ID,
			Action: activity.ActionDeleted, Changes: map[string]any{"crm_contact_id": str(m.Metadata, MetaCRMContactID)},
		})
	})
	if err != nil {
		return err
	}
	s.changed()
	return nil
}

// Activate attaches the tenant's pending invites for email to the signed-in client userID and
// answers how many it attached. Matching is by the keyed hash of the normalised address (so it is
// case-insensitive) and only within tenantID, the request's tenant: an invite of another tenant is
// never touched. A project where the user is already a client keeps its active row and the
// duplicate invite is dropped. Safe to call on every portal visit.
func (s *Service) Activate(ctx context.Context, tenantID, userID uuid.UUID, email string) (int, error) {
	if tenantID == uuid.Nil || userID == uuid.Nil {
		return 0, nil
	}
	hash, _, err := s.hasher.EmailHash(email)
	if err != nil {
		return 0, nil
	}
	// Cheap local check first: auth-api is asked only when an invite is waiting for this address.
	waiting, err := s.client.ProjectMember.Query().
		Where(clientRow(tenantID), entmember.UserID(uuid.Nil), metaEQ(MetaEmailHash, hash)).Exist(ctx)
	if err != nil {
		return 0, fmt.Errorf("find client invites: %w", err)
	}
	if !waiting {
		return 0, nil
	}
	if s.verifier == nil {
		return 0, ErrEmailNotVerified
	}
	proven, verified, err := s.verifier.VerifiedEmail(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("check the email with auth-api: %w", err)
	}
	if !verified || !strings.EqualFold(strings.TrimSpace(proven), strings.TrimSpace(email)) {
		return 0, ErrEmailNotVerified
	}
	n := 0
	err = txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		pending, err := tx.ProjectMember.Query().
			Where(clientRow(tenantID), entmember.UserID(uuid.Nil), metaEQ(MetaEmailHash, hash)).All(ctx)
		if err != nil || len(pending) == 0 {
			return err
		}
		now := time.Now().UTC().Format(time.RFC3339)
		for _, m := range pending {
			already, err := tx.ProjectMember.Query().
				Where(clientRow(tenantID), entmember.ProjectID(m.ProjectID), entmember.UserID(userID)).Exist(ctx)
			if err != nil {
				return err
			}
			if already {
				if err := tx.ProjectMember.DeleteOne(m).Exec(ctx); err != nil {
					return err
				}
				continue
			}
			meta := make(map[string]any, len(m.Metadata)+2)
			maps.Copy(meta, m.Metadata)
			meta[MetaStatus], meta[MetaActivatedAt] = StatusActive, now
			if err := m.Update().SetUserID(userID).SetJoinedAt(time.Now()).SetMetadata(meta).Exec(ctx); err != nil {
				return fmt.Errorf("activate invite: %w", err)
			}
			if err := activity.Record(ctx, tx, activity.Entry{
				TenantID: tenantID, ProjectID: activity.Ref(m.ProjectID), EntityType: "client", EntityID: m.ID,
				Action: activity.ActionUpdated, Changes: map[string]any{"status": StatusActive},
			}); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("activate client invites: %w", err)
	}
	if n > 0 {
		s.changed()
		s.log.Info("client invites activated", zap.String("tenant_id", tenantID.String()), zap.Int("projects", n))
	}
	return n, nil
}

// IsClient reports whether userID is an active client of the tenant's project. Every portal route
// with a project in its path checks it; anything else answers 404.
func (s *Service) IsClient(ctx context.Context, tenantID, projectID, userID uuid.UUID) (bool, error) {
	if userID == uuid.Nil {
		return false, nil
	}
	return s.client.ProjectMember.Query().
		Where(clientRow(tenantID), entmember.ProjectID(projectID), entmember.UserID(userID)).Exist(ctx)
}
