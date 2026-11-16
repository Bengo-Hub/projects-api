package projects

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/platform/events"
)

// Project event types (subject project.<type>). treasury-api closes a project's budgets and
// releases its open commitments on closed and deleted.
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

// emit writes a project event in the same transaction as the change.
func emit(ctx context.Context, tx *ent.Tx, tenantID, projectID uuid.UUID, eventType string, payload map[string]any) error {
	return events.Write(ctx, tx, events.AggregateProject, eventType, projectID, tenantID, payload)
}
