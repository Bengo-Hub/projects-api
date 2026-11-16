package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/http/authz"
	"github.com/bengobox/projects-service/internal/platform/erp"
	"github.com/bengobox/projects-service/internal/services/timetracking"
)

// erpStub answers every ERP call with err.
type erpStub struct{ err error }

func (s erpStub) UserTimesheets(context.Context, uuid.UUID, uuid.UUID, time.Time, time.Time) (*erp.UserTimesheets, error) {
	return nil, s.err
}

func (s erpStub) LogUserEntry(context.Context, uuid.UUID, erp.LogEntry) (*erp.Logged, error) {
	return nil, s.err
}

func (s erpStub) SubmitUserTimesheets(context.Context, uuid.UUID, uuid.UUID, time.Time, time.Time) ([]erp.Timesheet, error) {
	return nil, s.err
}

func timeRequest(method, target, body string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	return r.WithContext(authz.WithAccess(r.Context(), &authz.Access{TenantID: uuid.New(), AuthUserID: uuid.New(), Bypass: true}))
}

// A user without an ERP employee record gets 409 no_employee_record with a message, and ERP's own
// refusals reach the browser with ERP's code.
func TestTimeHandlerErrors(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{erp.ErrNoEmployee, http.StatusConflict, "no_employee_record"},
		{&erp.Error{Status: http.StatusConflict, Code: "nothing_to_submit", Message: "no draft"}, http.StatusConflict, "nothing_to_submit"},
		{&erp.Error{Status: http.StatusForbidden, Code: "feature_not_available"}, http.StatusForbidden, "feature_not_available"},
		{erp.ErrNotConfigured, http.StatusServiceUnavailable, "erp_unavailable"},
	}
	for _, c := range cases {
		h := NewTimeHandler(zap.NewNop(), timetracking.NewService(nil, erpStub{err: c.err}, zap.NewNop()))
		for name, run := range map[string]func(*httptest.ResponseRecorder){
			"week": func(w *httptest.ResponseRecorder) {
				h.Week(w, timeRequest(http.MethodGet, "/time/week?week_start=2026-11-18", ""))
			},
			"submit": func(w *httptest.ResponseRecorder) {
				h.SubmitWeek(w, timeRequest(http.MethodPost, "/time/week/submit", `{"week_start":"2026-11-18"}`))
			},
		} {
			w := httptest.NewRecorder()
			run(w)
			var body map[string]any
			_ = json.Unmarshal(w.Body.Bytes(), &body)
			if w.Code != c.status || body["code"] != c.code || body["error"] == "" {
				t.Errorf("%s with %v: %d %v, want %d %s", name, c.err, w.Code, body, c.status, c.code)
			}
		}
	}
}
