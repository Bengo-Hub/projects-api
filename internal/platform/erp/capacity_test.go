package erp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The capacity read sends the ids and the dates with the service key and tenant, and reads the
// decimal-string hours back keyed by auth user id.
func TestCapacity(t *testing.T) {
	tenant, ana, ben := uuid.New(), uuid.New(), uuid.New()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-API-Key") != "k" || r.Header.Get("X-Tenant-ID") != tenant.String() {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		q := r.URL.Query()
		if r.URL.Path != "/api/v1/hrm/attendance/capacity" || q.Get("auth_user_ids") != ana.String()+","+ben.String() ||
			q.Get("from") != "2026-11-16" || q.Get("to") != "2026-11-29" {
			t.Errorf("unexpected request %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"data":[` +
			`{"auth_user_id":"` + ana.String() + `","has_employee":true,"configured":true,"work_shift":"Office","weekly_hours":"40",` +
			`"weeks":[{"week_start":"2026-11-16","hours":"16","leave_hours":"16","holiday_hours":"8"},{"week_start":"2026-11-23","hours":"40"}]},` +
			`{"auth_user_id":"` + ben.String() + `","has_employee":false,"configured":false,"weekly_hours":"0","weeks":[]}]}`))
	}))
	defer srv.Close()

	day := func(s string) time.Time { d, _ := time.Parse(time.DateOnly, s); return d }
	got, err := NewClient(srv.URL, "k").Capacity(context.Background(), tenant, []uuid.UUID{ana, ben}, day("2026-11-16"), day("2026-11-29"))
	if err != nil {
		t.Fatal(err)
	}
	a := got[ana]
	if !a.Configured || a.WeeklyHours != 40 || a.WorkShift != "Office" || len(a.Weeks) != 2 || a.Weeks[0].Hours != 16 || a.Weeks[1].WeekStart != "2026-11-23" {
		t.Fatalf("ana = %+v", a)
	}
	if b := got[ben]; b.Configured || b.HasEmployee {
		t.Fatalf("ben = %+v", b)
	}
	if got, err := NewClient(srv.URL, "k").Capacity(context.Background(), tenant, nil, day("2026-11-16"), day("2026-11-29")); err != nil || len(got) != 0 || calls != 1 {
		t.Fatalf("no ids must not call ERP: %v %v (%d calls)", got, err, calls)
	}
	if _, err := NewClient("", "").Capacity(context.Background(), tenant, []uuid.UUID{ana}, day("2026-11-16"), day("2026-11-29")); err != ErrNotConfigured {
		t.Fatalf("unconfigured client: %v", err)
	}
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad"}`))
	}))
	defer failing.Close()
	if _, err := NewClient(failing.URL, "k").Capacity(context.Background(), tenant, []uuid.UUID{ana}, day("2026-11-16"), day("2026-11-29")); err == nil || !strings.Contains(err.Error(), "capacity") {
		t.Fatalf("a refused read must fail: %v", err)
	}
}
