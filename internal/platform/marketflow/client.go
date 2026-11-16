// Package marketflow is projects-api's S2S client for marketflow-api, the CRM and customer
// master (X-API-Key INTERNAL_SERVICE_KEY). It searches contacts for the project client picker.
package marketflow

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	serviceclient "github.com/Bengo-Hub/shared-service-client"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

var ErrNotConfigured = errors.New("marketflow: not configured")

// Client calls marketflow-api through the shared service client.
type Client struct {
	apiKey string
	sc     *serviceclient.Client
}

// NewClient builds the client; an empty URL leaves it disabled.
func NewClient(baseURL, apiKey string) *Client {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return &Client{apiKey: apiKey}
	}
	cfg := serviceclient.DefaultConfig(baseURL, "marketflow-api", zap.L().Named("marketflow"))
	cfg.Timeout = 8 * time.Second
	cfg.MaxElapsedTime = 3 * time.Second
	return &Client{apiKey: apiKey, sc: serviceclient.New(cfg)}
}

func (c *Client) Enabled() bool { return c != nil && c.sc != nil && c.apiKey != "" }

func (c *Client) headers() map[string]string { return map[string]string{"X-API-Key": c.apiKey} }

// Contact is the minimal CRM profile marketflow returns to sibling services.
type Contact struct {
	ID        uuid.UUID `json:"id"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	Email     string    `json:"email,omitempty"`
	Phone     string    `json:"phone,omitempty"`
}

// Name is the contact's display name.
func (c Contact) Name() string {
	return strings.TrimSpace(c.FirstName + " " + c.LastName)
}

// SearchContacts finds the tenant's contacts by name, email or phone (empty q lists the newest).
// limit is capped at 50.
func (c *Client) SearchContacts(ctx context.Context, tenantID uuid.UUID, q string, limit int) ([]Contact, error) {
	if !c.Enabled() {
		return nil, ErrNotConfigured
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	v := url.Values{"tenant_id": {tenantID.String()}, "q": {q}, "limit": {strconv.Itoa(limit)}}
	resp, err := c.sc.Get(ctx, "/api/v1/internal/contacts/search?"+v.Encode(), c.headers())
	if err != nil {
		return nil, fmt.Errorf("marketflow: contact search: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("marketflow: contact search: status %d", resp.StatusCode)
	}
	var body struct {
		Data []Contact `json:"data"`
	}
	if err := resp.DecodeJSON(&body); err != nil {
		return nil, fmt.Errorf("marketflow: contact search: %w", err)
	}
	return body.Data, nil
}

// ErrContactNotFound means marketflow answered 404 for the contact: deleted, never existed, or
// held under another tenant.
var ErrContactNotFound = errors.New("marketflow: contact not found")

// ErasedFirstName is the first name marketflow gives a contact stripped after the person asked
// to be erased (marketflow-api internal/modules/contacts/erase.go, ErasedName).
const ErasedFirstName = "Erased"

// Erased reports whether c is the minimal record marketflow keeps after an erasure: first name
// "Erased" and no last name, email or phone. marketflow requires an email or phone on every live
// contact, so a live contact is never mistaken for an erased one.
func (c Contact) Erased() bool {
	return strings.TrimSpace(c.FirstName) == ErasedFirstName && strings.TrimSpace(c.LastName) == "" &&
		strings.TrimSpace(c.Email) == "" && strings.TrimSpace(c.Phone) == ""
}

// GetContact reads one contact by id in the tenant (GET /api/v1/internal/contacts/{id}).
// ErrContactNotFound when marketflow answers 404; any other failure is a plain error.
func (c *Client) GetContact(ctx context.Context, tenantID, contactID uuid.UUID) (Contact, error) {
	if !c.Enabled() {
		return Contact{}, ErrNotConfigured
	}
	v := url.Values{"tenant_id": {tenantID.String()}}
	resp, err := c.sc.Get(ctx, "/api/v1/internal/contacts/"+contactID.String()+"?"+v.Encode(), c.headers())
	if err != nil {
		return Contact{}, fmt.Errorf("marketflow: get contact: %w", err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return Contact{}, ErrContactNotFound
	default:
		return Contact{}, fmt.Errorf("marketflow: get contact: status %d", resp.StatusCode)
	}
	var out Contact
	if err := resp.DecodeJSON(&out); err != nil {
		return Contact{}, fmt.Errorf("marketflow: get contact: %w", err)
	}
	return out, nil
}
