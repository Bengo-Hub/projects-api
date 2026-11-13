package authusers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func TestUserName(t *testing.T) {
	tenant, user := uuid.New(), uuid.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "k" || r.URL.Path != "/api/v1/s2s/"+tenant.String()+"/users" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"` + user.String() + `","email":"o@example.com","name":"Olive Owner"},{"id":"` + uuid.NewString() + `","name":""}]`))
	}))
	defer srv.Close()

	d := New(srv.URL, "k", nil, zap.NewNop())
	if got, err := d.UserName(context.Background(), tenant, user); err != nil || got != "Olive Owner" {
		t.Fatalf("name: %q %v", got, err)
	}
	if _, err := d.UserName(context.Background(), tenant, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a non-member must be ErrNotFound, got %v", err)
	}
	if _, err := New("", "", nil, zap.NewNop()).UserName(context.Background(), tenant, user); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("unconfigured: %v", err)
	}
}
