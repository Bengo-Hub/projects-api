package erp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestProjectHours(t *testing.T) {
	tenant, p := uuid.New(), uuid.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "k" || r.Header.Get("X-Tenant-ID") != tenant.String() {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/api/v1/hrm/attendance/timesheets/project-hours" || r.URL.Query().Get("project_ids") != p.String() {
			t.Errorf("unexpected request %s", r.URL)
		}
		// erp-api money and hours are decimal strings.
		_, _ = w.Write([]byte(`{"data":[{"project_id":"` + p.String() + `","approved_hours":"15.5","submitted_hours":"3"}]}`))
	}))
	defer srv.Close()

	got, err := NewClient(srv.URL+"/", "k").ProjectHours(context.Background(), tenant, []uuid.UUID{p})
	if err != nil {
		t.Fatal(err)
	}
	if h := got[p]; h.ApprovedHours != 15.5 || h.SubmittedHours != 3 {
		t.Fatalf("hours = %+v", h)
	}
	if _, err := NewClient("", "").ProjectHours(context.Background(), tenant, []uuid.UUID{p}); err != ErrNotConfigured {
		t.Fatalf("unconfigured client: %v", err)
	}
}
