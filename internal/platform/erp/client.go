// Package erp is projects-api's S2S client for erp-api (X-API-Key INTERNAL_SERVICE_KEY with
// X-Tenant-ID). It reads the timesheet hours logged per project for utilisation, and reads, logs
// and submits the signed-in user's own timesheet time (timesheets.go).
package erp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	serviceclient "github.com/Bengo-Hub/shared-service-client"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

var ErrNotConfigured = errors.New("erp: not configured")

// Client calls erp-api through the shared service client (circuit breaker, bounded retries of
// reads, tracing).
type Client struct {
	apiKey string
	sc     *serviceclient.Client
}

// NewClient builds the client; an empty URL leaves it disabled.
func NewClient(baseURL, apiKey string) *Client {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return &Client{apiKey: apiKey}
	}
	cfg := serviceclient.DefaultConfig(baseURL, "erp-api", zap.L().Named("erp"))
	cfg.Timeout = 10 * time.Second
	cfg.MaxElapsedTime = 3 * time.Second
	return &Client{apiKey: apiKey, sc: serviceclient.New(cfg)}
}

func (c *Client) Enabled() bool { return c != nil && c.sc != nil && c.apiKey != "" }

// Hours is the time logged against one project.
type Hours struct {
	ProjectID      uuid.UUID `json:"project_id"`
	ApprovedHours  float64   `json:"approved_hours,string"`
	SubmittedHours float64   `json:"submitted_hours,string"`
	// LabourCost is the approved hours priced at each employee's ERP hourly rate (sprint 15).
	LabourCost float64 `json:"labour_cost,string"`
	// UnbilledBillableHours is approved billable time not yet put on an invoice.
	UnbilledBillableHours float64 `json:"unbilled_billable_hours,string"`
}

// ProjectHours returns approved and submitted timesheet hours per project (at most 100 ids).
func (c *Client) ProjectHours(ctx context.Context, tenantID uuid.UUID, projectIDs []uuid.UUID) (map[uuid.UUID]Hours, error) {
	out := map[uuid.UUID]Hours{}
	if len(projectIDs) == 0 {
		return out, nil
	}
	if !c.Enabled() {
		return nil, ErrNotConfigured
	}
	ids := make([]string, len(projectIDs))
	for i, id := range projectIDs {
		ids[i] = id.String()
	}
	q := url.Values{"project_ids": {strings.Join(ids, ",")}}
	resp, err := c.sc.Get(ctx, "/api/v1/hrm/attendance/timesheets/project-hours?"+q.Encode(),
		map[string]string{"X-API-Key": c.apiKey, "X-Tenant-ID": tenantID.String()})
	if err != nil {
		return nil, fmt.Errorf("erp: project hours: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("erp: project hours: status %d", resp.StatusCode)
	}
	var body struct {
		Data []Hours `json:"data"`
	}
	if err := resp.DecodeJSON(&body); err != nil {
		return nil, fmt.Errorf("erp: project hours: %w", err)
	}
	for _, h := range body.Data {
		out[h.ProjectID] = h
	}
	return out, nil
}
