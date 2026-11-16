package router

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	sharedcache "github.com/Bengo-Hub/cache"
	httpware "github.com/Bengo-Hub/httpware"
	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/go-chi/chi/v5"
)

const (
	tenantA = "00000000-0000-0000-0000-00000000000a"
	tenantB = "00000000-0000-0000-0000-00000000000b"
)

type slugs map[string]string

func (s slugs) GetTenantDetails(_ context.Context, slug string) (sharedcache.TenantDetails, error) {
	if id, ok := s[slug]; ok {
		return sharedcache.TenantDetails{ID: id, Slug: slug}, nil
	}
	return sharedcache.TenantDetails{}, errors.New("unknown tenant")
}

func guarded(t *testing.T, claims *authclient.Claims, path, header string) (int, string) {
	t.Helper()
	var seen string
	r := chi.NewRouter()
	r.Use(httpware.Tenant) // as in production: the header is copied into the context first
	r.Route("/{tenantID}", func(tr chi.Router) {
		tr.Use(tenantGuard(slugs{"acme": tenantA, "beta": tenantB}))
		tr.Get("/projects", func(w http.ResponseWriter, r *http.Request) {
			seen = httpware.GetTenantID(r.Context())
			if h := r.Header.Get("X-Tenant-ID"); h != seen {
				t.Errorf("header %q differs from context tenant %q", h, seen)
			}
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
	return w.Code, seen
}

func TestTenantGuard(t *testing.T) {
	user := &authclient.Claims{TenantID: tenantA}
	superuser := &authclient.Claims{TenantID: tenantA, Roles: []string{"superuser"}}
	service := &authclient.Claims{IsService: true, ServiceName: "erp-api"}
	boundService := &authclient.Claims{IsService: true, ServiceName: "erp-api", TenantID: tenantA}
	cases := []struct {
		name       string
		claims     *authclient.Claims
		path       string
		header     string
		wantCode   int
		wantTenant string
	}{
		{"own tenant in header and path", user, "/" + tenantA + "/projects", tenantA, 200, tenantA},
		{"another tenant in the header is refused", user, "/" + tenantA + "/projects", tenantB, 403, ""},
		{"another tenant in the path is refused", user, "/" + tenantB + "/projects", "", 403, ""},
		{"missing header is filled from the token", user, "/" + tenantA + "/projects", "", 200, tenantA},
		{"a slug path keeps the token's tenant", user, "/beta/projects", "", 200, tenantA},
		{"a tenant superuser stays in their tenant", superuser, "/" + tenantB + "/projects", tenantB, 403, ""},
		{"platform owner may act on any tenant", &authclient.Claims{TenantID: tenantA, IsPlatformOwner: true}, "/" + tenantB + "/projects", tenantB, 200, tenantB},
		{"platform owner without a header takes the path", &authclient.Claims{TenantID: tenantA, IsPlatformOwner: true}, "/beta/projects", "", 200, tenantB},
		{"no claims is unauthorized", nil, "/" + tenantB + "/projects", tenantB, 401, ""},
		// Service calls: the tenant comes from the path, never from X-Tenant-ID.
		{"service call ignores a header naming another tenant", service, "/" + tenantA + "/projects", tenantB, 200, tenantA},
		{"service call resolves a slug path", service, "/beta/projects", tenantA, 200, tenantB},
		{"service call with an unknown slug", service, "/nobody/projects", "", 404, ""},
		{"a tenant-bound key cannot name another tenant", boundService, "/" + tenantB + "/projects", "", 403, ""},
		{"a tenant-bound key in its own tenant", boundService, "/acme/projects", tenantB, 200, tenantA},
	}
	for _, tc := range cases {
		code, got := guarded(t, tc.claims, tc.path, tc.header)
		if code != tc.wantCode {
			t.Errorf("%s: code = %d, want %d", tc.name, code, tc.wantCode)
		}
		if tc.wantCode == 200 && got != tc.wantTenant {
			t.Errorf("%s: handler saw tenant %q, want %q", tc.name, got, tc.wantTenant)
		}
	}
}
