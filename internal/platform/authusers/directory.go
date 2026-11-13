// Package authusers reads display names from auth-api, which owns users and tenants. Projects
// keeps no user names of its own and the access token carries none, so a notice that names a
// person (for example "invited by") asks auth-api.
//
// It reuses auth-api's existing S2S member list, GET /api/v1/s2s/{tenant}/users
// (X-API-Key, [{id, email, name, roles, ...}], the same read erp-api's employee backfill uses), and
// the shared cache's tenant details for the organisation name. Both are cached briefly in Redis
// through the shared cache library.
package authusers

import (
	"context"
	"errors"
	"fmt"
	"time"

	sharedcache "github.com/Bengo-Hub/cache"
	serviceclient "github.com/Bengo-Hub/shared-service-client"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// membersTTL is how long a tenant's member list is cached; a renamed user shows the new name
// within this time.
const membersTTL = 5 * time.Minute

// ErrNotFound means the user is not an active member of the tenant (or has no name).
var ErrNotFound = errors.New("authusers: no name for this user")

// ErrNotConfigured is returned when the auth URL or service key is missing.
var ErrNotConfigured = errors.New("authusers: not configured")

// Directory looks names up in auth-api.
type Directory struct {
	authURL string
	apiKey  string
	sc      *serviceclient.Client
	cache   *sharedcache.Aside
}

// New builds the directory. cache may be nil (no caching, every lookup calls auth-api).
func New(authURL, apiKey string, cache *sharedcache.Aside, log *zap.Logger) *Directory {
	d := &Directory{authURL: authURL, apiKey: apiKey, cache: cache}
	if authURL != "" {
		cfg := serviceclient.DefaultConfig(authURL, "auth-api", log.Named("authusers"))
		cfg.Timeout = 5 * time.Second
		cfg.MaxElapsedTime = 5 * time.Second
		d.sc = serviceclient.New(cfg)
	}
	return d
}

type member struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (d *Directory) members(ctx context.Context, tenantID uuid.UUID) ([]member, error) {
	if d == nil || d.sc == nil || d.apiKey == "" {
		return nil, ErrNotConfigured
	}
	key := "projects:authusers:members:" + tenantID.String()
	return sharedcache.GetOrSet(ctx, d.cache, key, membersTTL, func(ctx context.Context) ([]member, error) {
		resp, err := d.sc.Get(ctx, "/api/v1/s2s/"+tenantID.String()+"/users", map[string]string{"X-API-Key": d.apiKey})
		if err != nil {
			return nil, fmt.Errorf("authusers: %w", err)
		}
		if !resp.IsSuccess() {
			return nil, fmt.Errorf("authusers: status %d", resp.StatusCode)
		}
		var out []member
		if err := resp.DecodeJSON(&out); err != nil {
			return nil, fmt.Errorf("authusers: decode: %w", err)
		}
		return out, nil
	})
}

// UserName returns the display name of an active member of the tenant.
func (d *Directory) UserName(ctx context.Context, tenantID, userID uuid.UUID) (string, error) {
	if userID == uuid.Nil {
		return "", ErrNotFound
	}
	list, err := d.members(ctx, tenantID)
	if err != nil {
		return "", err
	}
	id := userID.String()
	for _, m := range list {
		if m.ID == id && m.Name != "" {
			return m.Name, nil
		}
	}
	return "", ErrNotFound
}

// OrgName returns the tenant's name, by its slug (cached by the shared cache library).
func (d *Directory) OrgName(ctx context.Context, tenantSlug string) (string, error) {
	if d == nil || d.authURL == "" {
		return "", ErrNotConfigured
	}
	if tenantSlug == "" {
		return "", ErrNotFound
	}
	t, err := sharedcache.GetTenantDetails(ctx, d.cache, d.authURL, tenantSlug, 0)
	if err != nil {
		return "", err
	}
	if t.Name == "" {
		return "", ErrNotFound
	}
	return t.Name, nil
}
