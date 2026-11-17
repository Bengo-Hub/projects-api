package erp

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// maxCapacityBatch is erp-api's limit of auth user ids per capacity read.
const maxCapacityBatch = 500

// CapacityWeek is one ISO week of a person's working hours in ERP, after approved leave and
// public holidays.
type CapacityWeek struct {
	WeekStart string  `json:"week_start"`
	Hours     float64 `json:"hours,string"`
}

// Capacity is one person's working hours in ERP (owner, 17 November 2026). Configured is true
// only when ERP holds working hours for the person (an employee with a standard work shift); the
// caller falls back to its own setting otherwise.
type Capacity struct {
	AuthUserID  uuid.UUID      `json:"auth_user_id"`
	HasEmployee bool           `json:"has_employee"`
	Configured  bool           `json:"configured"`
	WorkShift   string         `json:"work_shift,omitempty"`
	WeeklyHours float64        `json:"weekly_hours,string"`
	Weeks       []CapacityWeek `json:"weeks"`
}

// Capacity reads the working hours of people per ISO week from the Monday of from to the week
// holding to, keyed by auth user id: GET /api/v1/hrm/attendance/capacity, in batches of 500 ids.
func (c *Client) Capacity(ctx context.Context, tenantID uuid.UUID, userIDs []uuid.UUID, from, to time.Time) (map[uuid.UUID]Capacity, error) {
	out := make(map[uuid.UUID]Capacity, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	if !c.Enabled() {
		return nil, ErrNotConfigured
	}
	for start := 0; start < len(userIDs); start += maxCapacityBatch {
		end := min(start+maxCapacityBatch, len(userIDs))
		ids := make([]string, 0, end-start)
		for _, id := range userIDs[start:end] {
			ids = append(ids, id.String())
		}
		q := url.Values{"auth_user_ids": {strings.Join(ids, ",")}, "from": {from.Format(time.DateOnly)}, "to": {to.Format(time.DateOnly)}}
		resp, err := c.sc.Get(ctx, "/api/v1/hrm/attendance/capacity?"+q.Encode(),
			map[string]string{"X-API-Key": c.apiKey, "X-Tenant-ID": tenantID.String()})
		if err != nil {
			return nil, fmt.Errorf("erp: capacity: %w", err)
		}
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("erp: capacity: status %d", resp.StatusCode)
		}
		var body struct {
			Data []Capacity `json:"data"`
		}
		if err := resp.DecodeJSON(&body); err != nil {
			return nil, fmt.Errorf("erp: capacity: %w", err)
		}
		for _, p := range body.Data {
			out[p.AuthUserID] = p
		}
	}
	return out, nil
}
