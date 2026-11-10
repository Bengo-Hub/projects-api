package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	authclient "github.com/Bengo-Hub/shared-auth-client"
)

// Security audit 2026-11-11: POST /users was open to any signed-in member and passed any roles to
// auth-api, so a member could make themselves the organisation's admin. Only an admin may create
// users, and never with an admin-tier role.
func TestCreateUserGuards(t *testing.T) {
	h := &UserHandler{}
	call := func(c *authclient.Claims, body string) int {
		r := httptest.NewRequest(http.MethodPost, "/users", bytes.NewBufferString(body))
		r = r.WithContext(authclient.ContextWithClaims(r.Context(), c))
		w := httptest.NewRecorder()
		h.CreateUser(w, r)
		return w.Code
	}
	const tid = "11111111-1111-1111-1111-111111111111"
	member := &authclient.Claims{TenantID: tid, Roles: []string{"member"}}
	if code := call(member, `{"email":"x@y.z","roles":["member"]}`); code != http.StatusForbidden {
		t.Fatalf("a member creating users: %d, want 403", code)
	}
	admin := &authclient.Claims{TenantID: tid, Roles: []string{"admin"}}
	if code := call(admin, `{"email":"x@y.z","roles":["admin"]}`); code != http.StatusBadRequest {
		t.Fatalf("an admin-tier role: %d, want 400", code)
	}
}
