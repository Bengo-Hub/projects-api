// Package ticketing is projects-api's S2S client for ticketing-api. QA failures open their tickets
// through ticketing's internal create route (POST /api/v1/internal/tickets, X-API-Key and
// X-Tenant-ID). The create is idempotent on (source, source_ref.type, source_ref.id), so one QA
// defect never yields two tickets, even when a retried call had already landed.
package ticketing

import (
	"context"
	"errors"
	"fmt"
	"time"

	serviceclient "github.com/Bengo-Hub/shared-service-client"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ErrNotConfigured is returned when the ticketing URL or service key is missing.
var ErrNotConfigured = errors.New("ticketing: not configured")

// source names projects-api QA as the origin of a ticket; it is part of ticketing's idempotency key.
const source = "projects.qa"

// Client calls ticketing-api with the shared INTERNAL_SERVICE_KEY.
type Client struct {
	apiKey string
	sc     *serviceclient.Client
}

// NewClient builds the client. Retries are safe: ticketing answers a repeat of the same defect
// with the ticket it already opened.
func NewClient(baseURL, apiKey string, log *zap.Logger) *Client {
	if baseURL == "" {
		return &Client{apiKey: apiKey}
	}
	cfg := serviceclient.DefaultConfig(baseURL, "ticketing-api", log.Named("ticketing"))
	cfg.Timeout = 8 * time.Second
	cfg.MaxElapsedTime = 10 * time.Second
	return &Client{apiKey: apiKey, sc: serviceclient.New(cfg)}
}

// Enabled reports whether tickets can be opened.
func (c *Client) Enabled() bool { return c != nil && c.sc != nil && c.apiKey != "" }

// CreateTicketInput is one QA defect's ticket. Priority is low, normal, high, urgent or critical.
type CreateTicketInput struct {
	TenantID    uuid.UUID
	Subject     string
	Description string
	Priority    string
	Category    string
	// DefectID is the idempotency key; SessionID is stored as the parent reference.
	DefectID  uuid.UUID
	SessionID uuid.UUID
}

type ref struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Parent *ref   `json:"parent,omitempty"`
}

type createBody struct {
	Subject     string `json:"subject"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Priority    string `json:"priority"`
	Channel     string `json:"channel"`
	Category    string `json:"category"`
	SourceRef   ref    `json:"source_ref"`
}

// Ticket is ticketing's answer to a create (201 new, 200 the ticket already opened for this defect).
type Ticket struct {
	ID           string `json:"id"`
	TicketNumber string `json:"ticket_number"`
	Status       string `json:"status"`
}

// newBody builds the create request body for a defect.
func newBody(in CreateTicketInput) createBody {
	return createBody{
		Subject:     in.Subject,
		Description: in.Description,
		Source:      source,
		Priority:    in.Priority,
		Channel:     "internal",
		Category:    in.Category,
		SourceRef: ref{
			Type:   "qa_defect",
			ID:     in.DefectID.String(),
			Parent: &ref{Type: "qa_session", ID: in.SessionID.String()},
		},
	}
}

// CreateTicket opens (or returns the existing) ticket for a QA defect in the tenant's workspace.
func (c *Client) CreateTicket(ctx context.Context, in CreateTicketInput) (*Ticket, error) {
	if !c.Enabled() {
		return nil, ErrNotConfigured
	}
	if in.TenantID == uuid.Nil || in.DefectID == uuid.Nil {
		return nil, errors.New("ticketing: tenant and defect are required")
	}
	headers := map[string]string{"X-API-Key": c.apiKey, "X-Tenant-ID": in.TenantID.String()}
	resp, err := c.sc.Post(ctx, "/api/v1/internal/tickets", newBody(in), headers)
	if err != nil {
		return nil, fmt.Errorf("ticketing: create: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("ticketing: create: status %d", resp.StatusCode)
	}
	var t Ticket
	if err := resp.DecodeJSON(&t); err != nil {
		return nil, fmt.Errorf("ticketing: decode: %w", err)
	}
	return &t, nil
}
