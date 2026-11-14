package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/go-chi/chi/v5"

	"github.com/bengobox/projects-service/internal/services/fieldkeys"
)

type fakeFieldKeys struct {
	actor fieldkeys.Actor
	token string
}

func (f *fakeFieldKeys) Health(context.Context) (fieldkeys.Health, error) {
	return fieldkeys.Health{Configured: true, RootFingerprint: "abcd1234"}, nil
}

func (f *fakeFieldKeys) Backup(_ context.Context, a fieldkeys.Actor, token string) (*fieldkeys.BackupFile, error) {
	f.actor, f.token = a, token
	if token != "good" {
		return nil, fieldkeys.ErrOTPInvalid
	}
	return &fieldkeys.BackupFile{Name: "keys.json", Body: []byte(`{"keys":[]}`)}, nil
}

func (f *fakeFieldKeys) Rotate(_ context.Context, a fieldkeys.Actor, token string) (fieldkeys.Health, error) {
	f.actor, f.token = a, token
	if token == "" {
		return fieldkeys.Health{}, fieldkeys.ErrOTPRequired
	}
	return fieldkeys.Health{Configured: true}, fieldkeys.ErrRotationRunning
}

func fieldKeyRouter(svc FieldKeyService) http.Handler {
	r := chi.NewRouter()
	NewFieldKeyHandler(svc, nil).RegisterPlatformRoutes(r)
	return r
}

func callFieldKeys(t *testing.T, h http.Handler, c *authclient.Claims, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if c != nil {
		r = r.WithContext(authclient.ContextWithClaims(r.Context(), c))
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestFieldKeysArePlatformOwnerOnly(t *testing.T) {
	svc := &fakeFieldKeys{}
	h := fieldKeyRouter(svc)
	tenantAdmin := &authclient.Claims{Email: "a@t.test", Roles: []string{"admin", "superuser"}}
	tenantAdmin.Subject = "11111111-1111-1111-1111-111111111111"
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/field-keys"},
		{http.MethodPost, "/field-keys/backup"},
		{http.MethodPost, "/field-keys/rotate"},
	} {
		if w := callFieldKeys(t, h, nil, c.method, c.path, `{"otp_token":"good"}`); w.Code != http.StatusForbidden {
			t.Fatalf("%s %s without claims: %d", c.method, c.path, w.Code)
		}
		if w := callFieldKeys(t, h, tenantAdmin, c.method, c.path, `{"otp_token":"good"}`); w.Code != http.StatusForbidden {
			t.Fatalf("%s %s as a tenant admin: %d", c.method, c.path, w.Code)
		}
	}
	if svc.token != "" {
		t.Fatal("the service was reached by a non-owner")
	}
}

func TestFieldKeyRoutesForTheOwner(t *testing.T) {
	svc := &fakeFieldKeys{}
	h := fieldKeyRouter(svc)
	owner := &authclient.Claims{Email: "owner@codevertex.test", IsPlatformOwner: true}
	owner.Subject = "22222222-2222-2222-2222-222222222222"

	w := callFieldKeys(t, h, owner, http.MethodGet, "/field-keys", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"root_fingerprint":"abcd1234"`) {
		t.Fatalf("health: %d %s", w.Code, w.Body.String())
	}

	w = callFieldKeys(t, h, owner, http.MethodPost, "/field-keys/backup", `{"otp_token":"bad"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("backup with a bad code: %d", w.Code)
	}
	w = callFieldKeys(t, h, owner, http.MethodPost, "/field-keys/backup", `{"otp_token":"good"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("backup: %d", w.Code)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, "keys.json") {
		t.Fatalf("Content-Disposition %q", cd)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("a key file must not be cached")
	}
	if svc.actor.Email != owner.Email || svc.actor.UserID.String() != owner.Subject {
		t.Fatalf("actor: %+v", svc.actor)
	}

	if w := callFieldKeys(t, h, owner, http.MethodPost, "/field-keys/rotate", `{}`); w.Code != http.StatusBadRequest {
		t.Fatalf("rotate without a code: %d", w.Code)
	}
	if w := callFieldKeys(t, h, owner, http.MethodPost, "/field-keys/rotate", `{"otp_token":"x"}`); w.Code != http.StatusConflict {
		t.Fatalf("rotate while running: %d", w.Code)
	}
}
