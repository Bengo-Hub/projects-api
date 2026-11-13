// Package catalog reads the feature catalogue that subscriptions-api owns (feature_definition,
// served by GET /api/v1/features/catalog). QA criteria point at catalogue features by code; this
// service never copies the catalogue.
//
// The catalogue is gaining module, shipped_at and exclusive_tenant_id (a custom service's features
// shown only to its tenant) and an S2S read for projects-api. Until that lands the client stays
// behind QA_CATALOG_ENABLED and Disabled answers instead, so criteria keep their feature codes
// unchecked.
package catalog

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	serviceclient "github.com/Bengo-Hub/shared-service-client"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ErrUnavailable is returned when the catalogue cannot be read (disabled or not configured).
var ErrUnavailable = errors.New("catalog: unavailable")

// Feature is one catalogue line, in the documented catalogue shape. Module and ShippedAt are
// empty until subscriptions-api adds them.
type Feature struct {
	Code        string     `json:"featureCode"`
	ServiceTag  string     `json:"serviceTag"`
	Category    string     `json:"category"`
	Label       string     `json:"label"`
	Description string     `json:"description,omitempty"`
	Module      string     `json:"module,omitempty"`
	ShippedAt   *time.Time `json:"shippedAt,omitempty"`
}

// Reader is what the QA module needs from the catalogue. tenantID lets the owner hide another
// tenant's exclusive features once that filter exists.
type Reader interface {
	Features(ctx context.Context, tenantID uuid.UUID, serviceTag string) ([]Feature, error)
}

// Disabled is the stand-in until the catalogue S2S read ships.
type Disabled struct{}

func (Disabled) Features(context.Context, uuid.UUID, string) ([]Feature, error) {
	return nil, ErrUnavailable
}

// Client reads the catalogue from subscriptions-api with the shared service key.
type Client struct {
	apiKey string
	sc     *serviceclient.Client
}

func NewClient(baseURL, apiKey string, log *zap.Logger) *Client {
	cfg := serviceclient.DefaultConfig(baseURL, "subscriptions-api", log.Named("catalog"))
	cfg.Timeout = 5 * time.Second
	return &Client{apiKey: apiKey, sc: serviceclient.New(cfg)}
}

// New returns the live client when enabled and configured, else Disabled.
func New(enabled bool, baseURL, apiKey string, log *zap.Logger) Reader {
	if !enabled || baseURL == "" || apiKey == "" {
		return Disabled{}
	}
	return NewClient(baseURL, apiKey, log)
}

// Features lists active catalogue features, filtered by service tag when given.
func (c *Client) Features(ctx context.Context, tenantID uuid.UUID, serviceTag string) ([]Feature, error) {
	q := url.Values{"kind": {"FEATURE"}}
	if serviceTag != "" {
		q.Set("service", serviceTag)
	}
	if tenantID != uuid.Nil {
		q.Set("tenant_id", tenantID.String())
	}
	resp, err := c.sc.Get(ctx, "/api/v1/features/catalog?"+q.Encode(), map[string]string{"X-API-Key": c.apiKey})
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("catalog: status %d", resp.StatusCode)
	}
	var body struct {
		Features []Feature `json:"features"`
	}
	if err := resp.DecodeJSON(&body); err != nil {
		return nil, fmt.Errorf("catalog: decode: %w", err)
	}
	return body.Features, nil
}
