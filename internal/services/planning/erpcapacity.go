package planning

import (
	"context"
	"time"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/platform/erp"
)

// ERP capacity for the workload (owner, 17 November 2026). ERP answers each person's working hours
// per ISO week (their standard work shift, net of approved leave and public holidays). A person ERP
// holds hours for gets capacity_source "erp"; everyone else keeps the tenant setting. The answers
// are kept per person and range for capacityTTL in the pod, so a page of workload reads costs one
// ERP call at most every few minutes, and an ERP outage leaves the settings in place.

const (
	capacityTTL     = 3 * time.Minute
	capacityTimeout = 4 * time.Second
	capacityEntries = 20000
)

// capacityReader is the ERP read the workload needs (erp.Client.Capacity; a fake in tests).
type capacityReader interface {
	Enabled() bool
	Capacity(ctx context.Context, tenantID uuid.UUID, userIDs []uuid.UUID, from, to time.Time) (map[uuid.UUID]erp.Capacity, error)
}

// WithERP turns on ERP capacity for the workload.
func (s *Service) WithERP(c capacityReader) *Service {
	s.erpCap = c
	s.capacities = sharedcache.NewLocal[string, erp.Capacity](capacityEntries, capacityTTL)
	return s
}

func capacityKey(tenantID, userID uuid.UUID, start, end time.Time) string {
	return tenantID.String() + "|" + userID.String() + "|" + start.Format(time.DateOnly) + "|" + end.Format(time.DateOnly)
}

// erpCapacity returns ERP's answer for each person it has one for, from the cache where it can and
// one batched read for the rest. A failed read returns what the cache had and the error.
func (s *Service) erpCapacity(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID, start, end time.Time) (map[uuid.UUID]erp.Capacity, error) {
	out := map[uuid.UUID]erp.Capacity{}
	if s.erpCap == nil || !s.erpCap.Enabled() || len(ids) == 0 {
		return out, nil
	}
	var missing []uuid.UUID
	for _, id := range ids {
		if c, ok := s.capacities.Get(capacityKey(tenantID, id, start, end)); ok {
			out[id] = c
		} else {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return out, nil
	}
	rctx, cancel := context.WithTimeout(ctx, capacityTimeout)
	defer cancel()
	got, err := s.erpCap.Capacity(rctx, tenantID, missing, start, end)
	if err != nil {
		s.log.Warn("erp capacity read failed; workload uses the capacity settings", zap.Error(err))
		return out, err
	}
	for _, id := range missing {
		c := got[id] // an id ERP did not answer is kept as unconfigured, so it is not asked again
		c.AuthUserID = id
		s.capacities.Set(capacityKey(tenantID, id, start, end), c)
		out[id] = c
	}
	return out, nil
}

// ApplyERPCapacity puts ERP's capacity on every person ERP holds working hours for: the weekly
// capacity, each week's own hours (leave and holidays already off; a week ERP did not answer takes
// the weekly figure), and the load recomputed against them. Everyone else keeps the setting. The
// grid's capacity_source then says "erp", "settings" or "mixed".
func ApplyERPCapacity(out *Workload, caps map[uuid.UUID]erp.Capacity) {
	fromERP := 0
	for i := range out.People {
		p := &out.People[i]
		c, ok := caps[p.UserID]
		if !ok || !c.Configured {
			continue
		}
		fromERP++
		weeks := make(map[string]float64, len(c.Weeks))
		for _, w := range c.Weeks {
			weeks[w.WeekStart] = w.Hours
		}
		p.CapacitySource, p.WorkShift, p.WeeklyCapacity = "erp", c.WorkShift, round2(c.WeeklyHours)
		p.OverloadedWeeks = 0
		for j := range p.Weeks {
			w := &p.Weeks[j]
			capacity, has := weeks[w.WeekStart]
			if !has {
				capacity = c.WeeklyHours
			}
			w.Capacity, w.LoadPct, w.Overloaded = round2(capacity), 0, false
			switch {
			case capacity > 0:
				w.LoadPct = round2(w.Demand / capacity * 100)
				w.Overloaded = w.LoadPct > 100
			case w.Demand > 0:
				w.LoadPct, w.Overloaded = 100, true
			}
			if w.Overloaded {
				p.OverloadedWeeks++
			}
		}
	}
	if fromERP > 0 && fromERP == len(out.People) {
		out.CapacitySource = "erp"
	} else if fromERP > 0 {
		out.CapacitySource = "mixed"
	}
}
