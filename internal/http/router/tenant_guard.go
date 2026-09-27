package router

import (
	"net/http"
	"strings"

	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// requireOwnTenant stops a signed-in user from reading or changing another tenant's data.
// Handlers resolve the tenant from the X-Tenant-ID header, which the caller controls; without
// this guard any user could name any tenant there. For a user token:
//   - superusers and platform owners may act on any tenant;
//   - an X-Tenant-ID or a UUID {tenantID} path segment naming another tenant is refused (403);
//   - a missing X-Tenant-ID is filled from the token, so callers that only send the JWT (for
//     example erp-api's lookups) resolve their own tenant.
//
// Requests without user claims (service-to-service calls authenticated by API key) pass
// through unchanged.
func requireOwnTenant(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authclient.ClaimsFromContext(r.Context())
		if !ok || claims == nil || claims.TenantID == "" || claims.IsSuperuser() || claims.IsPlatformOwner {
			next.ServeHTTP(w, r)
			return
		}
		own := strings.ToLower(claims.TenantID)
		if h := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Tenant-ID"))); h != "" && h != own {
			forbidTenant(w)
			return
		}
		if p := chi.URLParam(r, "tenantID"); p != "" {
			if _, err := uuid.Parse(p); err == nil && strings.ToLower(p) != own {
				forbidTenant(w)
				return
			}
		}
		if r.Header.Get("X-Tenant-ID") == "" {
			r.Header.Set("X-Tenant-ID", claims.TenantID)
		}
		next.ServeHTTP(w, r)
	})
}

func forbidTenant(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"error":"tenant mismatch","code":"tenant_forbidden"}`))
}
