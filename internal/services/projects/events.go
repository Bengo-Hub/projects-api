package projects

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
)

// Publisher is the slice of the events publisher the projects service needs.
type Publisher interface {
	PublishProjectEvent(ctx context.Context, tenantID, projectID uuid.UUID, eventType string, payload map[string]any) error
}

// SetPublisher wires project lifecycle events. Optional: without it nothing is published.
func (s *Service) SetPublisher(p Publisher) { s.publisher = p }

// Project event types (subject project.<type>).
const (
	EventCreated = "created"
	EventUpdated = "updated"
	EventClosed  = "closed"
	EventDeleted = "deleted"
)

// closedStatuses are the statuses after which no more spend should be planned for a project.
var closedStatuses = map[string]bool{"completed": true, "closed": true, "cancelled": true, "archived": true}

// IsClosedStatus reports whether a project status ends the project.
func IsClosedStatus(status string) bool { return closedStatuses[status] }

// closesProject reports whether a status change moves a project from open to closed.
func closesProject(prev, next string) bool { return !IsClosedStatus(prev) && IsClosedStatus(next) }

func projectPayload(p *ent.Project) map[string]any {
	out := map[string]any{
		"id":        p.ID.String(),
		"tenant_id": p.TenantID.String(),
		"name":      p.Name,
		"status":    p.Status,
		"currency":  p.Currency,
		"budget":    p.Budget,
		"owner_id":  p.OwnerID.String(),
	}
	if !p.StartDate.IsZero() {
		out["start_date"] = p.StartDate.Format(time.DateOnly)
	}
	if !p.EndDate.IsZero() {
		out["end_date"] = p.EndDate.Format(time.DateOnly)
	}
	return out
}

// emit publishes best-effort: the project change is already saved, so a failed event is logged,
// not returned (the milestone events follow the same rule).
func (s *Service) emit(ctx context.Context, tenantID, projectID uuid.UUID, eventType string, payload map[string]any) {
	if s.publisher == nil {
		return
	}
	if err := s.publisher.PublishProjectEvent(ctx, tenantID, projectID, eventType, payload); err != nil {
		s.log.Warn("project event not published", zap.String("event", eventType), zap.String("project", projectID.String()), zap.Error(err))
	}
}
