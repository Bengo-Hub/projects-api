package tenders

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
)

// Publisher emits tender events to the shared-events outbox as project.tender.* (the projects
// stream binds project.>; notifications-api consumes it).
type Publisher interface {
	PublishTenderEvent(ctx context.Context, tenantID, tenderID uuid.UUID, eventType string, payload map[string]any) error
}

// SetPublisher wires the event publisher. Without one, events are skipped.
func (s *Service) SetPublisher(p Publisher) { s.publisher = p }

// emit publishes best-effort: the change is already saved, so a failed event is logged, not
// returned.
func (s *Service) emit(ctx context.Context, tenantID, tenderID uuid.UUID, eventType string, payload map[string]any) {
	if s.publisher == nil {
		return
	}
	if err := s.publisher.PublishTenderEvent(ctx, tenantID, tenderID, eventType, payload); err != nil {
		s.log.Warn("tender event not published", zap.String("event_type", eventType), zap.Error(err))
	}
}

// tenderPayload is the common part of every tender event.
func (s *Service) tenderPayload(t *ent.Tender) map[string]any {
	p := map[string]any{
		"tender_id":   t.ID.String(),
		"number":      t.Number,
		"title":       t.Title,
		"client_name": t.ClientName,
		"status":      t.Status,
		"currency":    t.Currency,
		"created_by":  t.CreatedBy.String(),
	}
	if t.EstimatedValue != 0 {
		p["estimated_value"] = t.EstimatedValue
	}
	if !t.Deadline.IsZero() {
		p["deadline"] = t.Deadline
	}
	return p
}

// emitStatusChanged publishes tender.status.changed, plus tender.submitted, tender.awarded or
// tender.lost for those milestones.
func (s *Service) emitStatusChanged(ctx context.Context, from string, t *ent.Tender, notes string) {
	p := s.tenderPayload(t)
	p["from"] = from
	p["to"] = t.Status
	if notes != "" {
		p["notes"] = notes
	}
	s.emit(ctx, t.TenantID, t.ID, "tender.status.changed", p)
	switch t.Status {
	case StatusAwarded:
		p := s.tenderPayload(t)
		for k, v := range t.Outcome {
			p[k] = v
		}
		s.emit(ctx, t.TenantID, t.ID, "tender.awarded", p)
	case StatusLost:
		p := s.tenderPayload(t)
		for k, v := range t.Outcome {
			p[k] = v
		}
		s.emit(ctx, t.TenantID, t.ID, "tender.lost", p)
	}
}
