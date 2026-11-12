// Package marketflow is projects-api's S2S client for marketflow-api, the CRM and customer
// master (X-API-Key INTERNAL_SERVICE_KEY). It searches contacts for the project client picker.
package marketflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrNotConfigured = errors.New("marketflow: not configured")

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func NewClient(baseURL, apiKey string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, http: &http.Client{Timeout: 8 * time.Second}}
}

func (c *Client) Enabled() bool { return c != nil && c.baseURL != "" && c.apiKey != "" }

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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/internal/contacts/search?"+v.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("marketflow: contact search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("marketflow: contact search: status %d", resp.StatusCode)
	}
	var body struct {
		Data []Contact `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/internal/contacts/"+contactID.String()+"?"+v.Encode(), nil)
	if err != nil {
		return Contact{}, err
	}
	req.Header.Set("X-API-Key", c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return Contact{}, fmt.Errorf("marketflow: get contact: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return Contact{}, ErrContactNotFound
	default:
		return Contact{}, fmt.Errorf("marketflow: get contact: status %d", resp.StatusCode)
	}
	var out Contact
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Contact{}, fmt.Errorf("marketflow: get contact: %w", err)
	}
	return out, nil
}
