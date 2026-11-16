package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	eventslib "github.com/Bengo-Hub/shared-events"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
)

// Subjects follow the shared-events convention {aggregate_type}.{event_type}, for example
// project.milestone.reached. Rows land in outbox_events; the shared publisher started in app.Run
// claims, publishes and deletes them.

// Write adds a domain event to the outbox inside the caller's transaction, so the event exists if
// and only if the domain write commits.
func Write(ctx context.Context, tx *ent.Tx, aggregateType, eventType string, aggregateID, tenantID uuid.UUID, payload map[string]any) error {
	return insert(ctx, tx.OutboxEvent, aggregateType, eventType, aggregateID, tenantID, payload)
}

func insert(ctx context.Context, c *ent.OutboxEventClient, aggregateType, eventType string, aggregateID, tenantID uuid.UUID, payload map[string]any) error {
	ev := eventslib.NewEvent(eventType, aggregateType, aggregateID, tenantID, payload)
	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("encode %s.%s: %w", aggregateType, eventType, err)
	}
	if err := c.Create().
		SetID(ev.ID).SetTenantID(tenantID).SetAggregateType(aggregateType).SetAggregateID(aggregateID).
		SetEventType(eventType).SetPayload(body).SetStatus(eventslib.StatusPending).SetAttempts(0).
		SetCreatedAt(time.Now().UTC()).Exec(ctx); err != nil {
		return fmt.Errorf("write %s.%s to outbox: %w", aggregateType, eventType, err)
	}
	return nil
}

// Project event aggregate type: every projects event stays on the project.> stream.
const AggregateProject = "project"

// QAPublisher writes QA events (project.qa.<type>, the session as aggregate). QA changes are
// saved step by step rather than in one transaction, so its events are written after the change,
// best effort, through the same outbox table.
type QAPublisher struct {
	client *ent.Client
	logger *zap.Logger
}

// NewQAPublisher builds the QA event writer.
func NewQAPublisher(client *ent.Client, logger *zap.Logger) *QAPublisher {
	return &QAPublisher{client: client, logger: logger.Named("projects.events")}
}

// PublishQAEvent writes project.qa.<eventType>, for example project.qa.result.failed.
func (p *QAPublisher) PublishQAEvent(ctx context.Context, tenantID, sessionID uuid.UUID, eventType string, payload map[string]any) error {
	if p == nil {
		return nil
	}
	return insert(ctx, p.client.OutboxEvent, AggregateProject, "qa."+eventType, sessionID, tenantID, payload)
}
