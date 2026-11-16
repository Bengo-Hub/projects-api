package router

import (
	"context"
	"net/http"
	"strings"

	sharedcache "github.com/Bengo-Hub/cache"
	httpware "github.com/Bengo-Hub/httpware"
	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/http/authz"
)

// TenantLookup resolves a tenant slug to auth-api's tenant (handlers.TenantResolver).
type TenantLookup interface {
	GetTenantDetails(ctx context.Context, slug string) (sharedcache.TenantDetails, error)
}

// tenantGuard fixes the tenant every handler reads (httpware.GetTenantID) before anything else in
// the tenant group runs.
//
// A signed-in user acts in the tenant of their token: an X-Tenant-ID or a UUID {tenantID} path
// segment naming another tenant is refused (403), and a missing header is filled from the token.
// Only a platform owner may act on another tenant (header or path). A tenant superuser is that
// tenant's own admin and is held to their tenant like anyone else.
//
// A service caller (API key, no user) never chooses the tenant through X-Tenant-ID: the tenant is
// the {tenantID} path segment (a UUID, or a slug resolved through auth-api), and a key bound to a
// tenant may only name that tenant.
func tenantGuard(tenants TenantLookup) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := authclient.ClaimsFromContext(r.Context())
			if !ok || claims == nil {
				authz.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized", "code": "unauthorized"})
				return
			}
			if authz.IsServiceCaller(claims) {
				tenantID, status := pathTenant(r, tenants)
				if status != 0 {
					authz.WriteJSON(w, status, map[string]string{"error": "tenant not found", "code": "tenant_not_found"})
					return
				}
				if claims.TenantID != "" && !claims.IsPlatformOwner && !strings.EqualFold(claims.TenantID, tenantID) {
					forbidTenant(w)
					return
				}
				setTenant(next, w, r, tenantID)
				return
			}
			if claims.IsPlatformOwner {
				// The platform owner picks the tenant: ?tenantId=, X-Tenant-ID, or the path.
				if q := r.URL.Query().Get("tenantId"); q != "" {
					if id, err := uuid.Parse(q); err == nil {
						setTenant(next, w, r, id.String())
						return
					}
				}
				if r.Header.Get("X-Tenant-ID") != "" {
					next.ServeHTTP(w, r)
					return
				}
				if tenantID, status := pathTenant(r, tenants); status == 0 {
					setTenant(next, w, r, tenantID)
					return
				}
				setTenant(next, w, r, claims.TenantID)
				return
			}
			own := strings.ToLower(claims.TenantID)
			if own == "" {
				forbidTenant(w)
				return
			}
			if h := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Tenant-ID"))); h != "" && h != own {
				forbidTenant(w)
				return
			}
			if p := chi.URLParam(r, "tenantID"); p != "" {
				// A slug segment is only a label here: the data scope is the token's tenant.
				if _, err := uuid.Parse(p); err == nil && strings.ToLower(p) != own {
					forbidTenant(w)
					return
				}
			}
			setTenant(next, w, r, claims.TenantID)
		})
	}
}

// pathTenant resolves the {tenantID} path segment to a tenant UUID: a UUID as given, a slug
// through auth-api. A non-zero status is the answer when it cannot be resolved.
func pathTenant(r *http.Request, tenants TenantLookup) (string, int) {
	p := strings.TrimSpace(chi.URLParam(r, "tenantID"))
	if p == "" {
		return "", http.StatusBadRequest
	}
	if id, err := uuid.Parse(p); err == nil {
		return id.String(), 0
	}
	if tenants == nil {
		return "", http.StatusNotFound
	}
	d, err := tenants.GetTenantDetails(r.Context(), p)
	if err != nil {
		return "", http.StatusNotFound
	}
	id, err := uuid.Parse(d.ID)
	if err != nil {
		return "", http.StatusNotFound
	}
	return id.String(), 0
}

// setTenant makes tenantID the request's tenant in both the header and the context.
func setTenant(next http.Handler, w http.ResponseWriter, r *http.Request, tenantID string) {
	r.Header.Set("X-Tenant-ID", tenantID)
	next.ServeHTTP(w, r.WithContext(httpware.WithTenantID(r.Context(), tenantID)))
}

func forbidTenant(w http.ResponseWriter) {
	authz.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch", "code": "tenant_forbidden"})
}

// requireFeature gates every method (reads included) on a subscription feature. Tenants exempt
// from gating (platform owner, demo, service-charge, subscription-exempt) pass.
func requireFeature(code string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := authclient.ClaimsFromContext(r.Context())
			if !ok || claims == nil || claims.IsGatingExempt() || claims.HasFeature(code) {
				next.ServeHTTP(w, r)
				return
			}
			authz.WriteJSON(w, http.StatusForbidden, map[string]any{
				"error": "feature_not_available", "code": "feature_not_available", "required_feature": code, "upgrade": true,
			})
		})
	}
}
