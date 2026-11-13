// Package ticketing is projects-api's S2S client for ticketing-api. QA failures open their tickets
// through ticketing's internal create route (POST /api/v1/internal/tickets, X-API-Key).
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

// Client calls ticketing-api with the shared INTERNAL_SERVICE_KEY.
type Client struct {
	apiKey string
	sc     *serviceclient.Client
}

// NewClient builds the client. Retries are kept short: a create that timed out on our side may
// still have landed, and the QA defect row (not ticketing) is what prevents a second ticket for the
// same criterion.
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

// CreateTicketInput mirrors ticketing's internal create body. Priority is low, normal, high,
// urgent or critical.
type CreateTicketInput struct {
	TenantID    uuid.UUID
	Title       string
	Description string
	Priority    string
	Category    string
	// SessionID links the ticket back to the QA session (stored in the ticket's metadata).
	SessionID string
}

// Ticket is ticketing's answer to a create.
type Ticket struct {
	ID           string `json:"id"`
	TicketNumber string `json:"ticket_number"`
	Status       string `json:"status"`
}

// CreateTicket opens a ticket in the tenant's ticketing workspace.
func (c *Client) CreateTicket(ctx context.Context, in CreateTicketInput) (*Ticket, error) {
	if !c.Enabled() {
		return nil, ErrNotConfigured
	}
	body := map[string]any{
		"tenant_id":   in.TenantID.String(),
		"title":       in.Title,
		"description": in.Description,
		"priority":    in.Priority,
		"category":    in.Category,
		"source":      "projects.qa",
		"channel":     "internal",
		"session_id":  in.SessionID,
	}
	resp, err := c.sc.Post(ctx, "/api/v1/internal/tickets", body, map[string]string{"X-API-Key": c.apiKey})
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
