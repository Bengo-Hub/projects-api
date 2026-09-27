package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/go-chi/chi/v5"
)

const (
	tenantA = "00000000-0000-0000-0000-00000000000a"
	tenantB = "00000000-0000-0000-0000-00000000000b"
)

func guarded(t *testing.T, claims *authclient.Claims, path, header string) (int, string) {
	t.Helper()
	var seenHeader string
	r := chi.NewRouter()
	r.Route("/{tenantID}", func(tr chi.Router) {
		tr.Use(requireOwnTenant)
		tr.Get("/projects", func(w http.ResponseWriter, r *http.Request) {
			seenHeader = r.Header.Get("X-Tenant-ID")
			w.WriteHeader(http.StatusOK)
		})
	})
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if header != "" {
		req.Header.Set("X-Tenant-ID", header)
	}
	if claims != nil {
		req = req.WithContext(authclient.ContextWithClaims(context.Background(), claims))
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, seenHeader
}

func TestRequireOwnTenant(t *testing.T) {
	user := &authclient.Claims{TenantID: tenantA}
	cases := []struct {
		name       string
		claims     *authclient.Claims
		path       string
		header     string
		wantCode   int
		wantHeader string
	}{
		{"own tenant in header and path", user, "/" + tenantA + "/projects", tenantA, 200, tenantA},
		{"another tenant in the header is refused", user, "/" + tenantA + "/projects", tenantB, 403, ""},
		{"another tenant in the path is refused", user, "/" + tenantB + "/projects", "", 403, ""},
		{"missing header is filled from the token", user, "/" + tenantA + "/projects", "", 200, tenantA},
		{"slug path is left to the handler", user, "/acme/projects", "", 200, tenantA},
		{"platform owner may act on any tenant", &authclient.Claims{TenantID: tenantA, IsPlatformOwner: true}, "/" + tenantB + "/projects", tenantB, 200, tenantB},
		{"service call without claims passes", nil, "/" + tenantB + "/projects", tenantB, 200, tenantB},
	}
	for _, tc := range cases {
		code, hdr := guarded(t, tc.claims, tc.path, tc.header)
		if code != tc.wantCode {
			t.Errorf("%s: code = %d, want %d", tc.name, code, tc.wantCode)
		}
		if tc.wantCode == 200 && hdr != tc.wantHeader {
			t.Errorf("%s: handler saw X-Tenant-ID %q, want %q", tc.name, hdr, tc.wantHeader)
		}
	}
}
