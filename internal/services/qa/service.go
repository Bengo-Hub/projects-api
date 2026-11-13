// Package qa is the QA and acceptance module: templates of criteria, sessions that run them with
// invited testers and signers, results, the pass rule, defects raised to ticketing-api, and the
// signed acceptance report. projects-api owns these records; the feature catalogue stays in
// subscriptions-api, identities and email codes in auth-api, tickets in ticketing-api, and alert
// routing in notifications-api (fed by the events published here).
package qa

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/platform/catalog"
	"github.com/bengobox/projects-service/internal/platform/ticketing"
)

var (
	// ErrNotFound: the record does not exist or belongs to another tenant.
	ErrNotFound = errors.New("qa: not found")
	// ErrForbidden: the caller may not change this record (for example a platform template).
	ErrForbidden = errors.New("qa: forbidden")
	// ErrConflict: the record is in a state that does not allow the change.
	ErrConflict = errors.New("qa: conflict")
	// ErrAccessDenied: an invitee could not be let in. One error for every reason, so the answer
	// never tells a stranger whether an address was invited.
	ErrAccessDenied = errors.New("qa: access denied")
)

// ValidationError is a bad input, answered with 400.
type ValidationError struct{ Msg string }

func (e ValidationError) Error() string { return e.Msg }

func invalid(format string, args ...any) error {
	return ValidationError{Msg: fmt.Sprintf(format, args...)}
}

func conflict(msg string) error { return fmt.Errorf("%w: %s", ErrConflict, msg) }

// Publisher is the slice of the events publisher the module needs (project.qa.* subjects).
type Publisher interface {
	PublishQAEvent(ctx context.Context, tenantID, sessionID uuid.UUID, eventType string, payload map[string]any) error
}

// TicketOpener opens a ticket in ticketing-api.
type TicketOpener interface {
	CreateTicket(ctx context.Context, in ticketing.CreateTicketInput) (*ticketing.Ticket, error)
}

// OTPValidator trades an auth-api email-code token for the address it proved.
type OTPValidator interface {
	Validate(ctx context.Context, token string) (string, error)
}

// Sealer encrypts a field at rest (internal/shared/fieldcrypt).
type Sealer interface {
	Encrypt(plain string) (string, error)
	Decrypt(enc string) (string, error)
}

// Directory reads display names from auth-api (internal/platform/authusers): a member's name, and
// the organisation name as the fallback.
type Directory interface {
	UserName(ctx context.Context, tenantID, userID uuid.UUID) (string, error)
	OrgName(ctx context.Context, tenantSlug string) (string, error)
}

// Actor is the signed-in person acting on a tenant's QA records.
type Actor struct {
	UserID        uuid.UUID
	Email         string
	TenantID      uuid.UUID
	PlatformOwner bool
}

// Service implements the module.
type Service struct {
	client    *ent.Client
	log       *zap.Logger
	keys      *Keys
	publisher Publisher
	tickets   TicketOpener
	catalog   catalog.Reader
	otp       OTPValidator
	sealer    Sealer
	directory Directory
	accessTTL time.Duration
	uiBaseURL string
	now       func() time.Time
}

// NewService builds the module. Collaborators are optional and set with the With methods; without
// them nothing is published, no ticket is opened, the catalogue is not checked and email codes are
// refused.
func NewService(client *ent.Client, keys *Keys, log *zap.Logger) *Service {
	return &Service{
		client:    client,
		log:       log.Named("qa.svc"),
		keys:      keys,
		catalog:   catalog.Disabled{},
		accessTTL: 8 * time.Hour,
		now:       time.Now,
	}
}

func (s *Service) WithPublisher(p Publisher) *Service { s.publisher = p; return s }

func (s *Service) WithTickets(t TicketOpener) *Service { s.tickets = t; return s }

func (s *Service) WithCatalog(c catalog.Reader) *Service { s.catalog = c; return s }

func (s *Service) WithOTP(v OTPValidator) *Service { s.otp = v; return s }

// WithSealer keeps signer addresses encrypted for close and decision notices. Without it signers
// stay hash-only like everyone else.
func (s *Service) WithSealer(b Sealer) *Service { s.sealer = b; return s }

// WithDirectory names the inviter on invite notices.
func (s *Service) WithDirectory(d Directory) *Service { s.directory = d; return s }

func (s *Service) WithUIBaseURL(u string) *Service { s.uiBaseURL = u; return s }

func (s *Service) WithAccessTTL(d time.Duration) *Service {
	if d > 0 {
		s.accessTTL = d
	}
	return s
}

// Catalog exposes the catalogue reader for the subject picker.
func (s *Service) Catalog() catalog.Reader { return s.catalog }

// emit publishes best effort: the change is already saved, so a failed event is logged.
func (s *Service) emit(ctx context.Context, tenantID, sessionID uuid.UUID, eventType string, payload map[string]any) {
	if s.publisher == nil {
		return
	}
	if err := s.publisher.PublishQAEvent(ctx, tenantID, sessionID, eventType, payload); err != nil {
		s.log.Warn("qa event not published", zap.String("event", eventType), zap.String("session", sessionID.String()), zap.Error(err))
	}
}

// QA event types (subject project.qa.<type>).
const (
	EventInviteCreated   = "invite.created"
	EventResultFailed    = "result.failed"
	EventSessionClosed   = "session.closed"
	EventSessionAccepted = "session.accepted"
	EventSessionRejected = "session.rejected"
)

// rollback ends a failed transaction; its own error adds nothing to the one being returned.
func rollback(tx *ent.Tx) { _ = tx.Rollback() }
