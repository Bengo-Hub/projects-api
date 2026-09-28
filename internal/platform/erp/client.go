// Package erp is projects-api's S2S client for erp-api (X-API-Key INTERNAL_SERVICE_KEY with
// X-Tenant-ID). It reads the timesheet hours logged per project for utilisation.
package erp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrNotConfigured = errors.New("erp: not configured")

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func NewClient(baseURL, apiKey string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, http: &http.Client{Timeout: 10 * time.Second}}
}

func (c *Client) Enabled() bool { return c != nil && c.baseURL != "" && c.apiKey != "" }

// Hours is the time logged against one project.
type Hours struct {
	ProjectID      uuid.UUID `json:"project_id"`
	ApprovedHours  float64   `json:"approved_hours,string"`
	SubmittedHours float64   `json:"submitted_hours,string"`
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/hrm/attendance/timesheets/project-hours?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("X-Tenant-ID", tenantID.String())
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("erp: project hours: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("erp: project hours: status %d", resp.StatusCode)
	}
	var body struct {
		Data []Hours `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("erp: project hours: %w", err)
	}
	for _, h := range body.Data {
		out[h.ProjectID] = h
	}
	return out, nil
}
