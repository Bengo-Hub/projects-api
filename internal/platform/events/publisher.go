package events

import (
	"context"
	"database/sql"
	"fmt"

	eventslib "github.com/Bengo-Hub/shared-events"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Publisher writes domain events to the shared-events transactional outbox; the background
// outbox worker (see app.Run) publishes them to JetStream. Subjects follow the shared-events
// convention {aggregate_type}.{event_type}, e.g. "project.milestone.reached".
type Publisher struct {
	repo   eventslib.OutboxRepository
	logger *zap.Logger
}

// NewPublisher builds a projects event publisher backed by the shared-events outbox.
func NewPublisher(sqlDB *sql.DB, logger *zap.Logger) *Publisher {
	return &Publisher{
		repo:   eventslib.NewSQLOutboxRepository(sqlDB),
		logger: logger.Named("projects.events"),
	}
}

func (p *Publisher) publish(ctx context.Context, aggregateType, eventType string, aggregateID, tenantID uuid.UUID, payload map[string]any) error {
	if p == nil {
		return nil
	}
	event := eventslib.NewEvent(eventType, aggregateType, aggregateID, tenantID, payload)

	tx, err := p.repo.BeginTx(ctx)
	if err != nil {
		p.logger.Error("begin tx for event", zap.String("event_type", eventType), zap.Error(err))
		return fmt.Errorf("begin tx: %w", err)
	}
	if err := eventslib.CreateOutboxRecordInTx(ctx, tx, p.repo, event); err != nil {
		_ = tx.Rollback()
		p.logger.Error("write event to outbox", zap.String("event_type", eventType), zap.Error(err))
		return fmt.Errorf("write outbox: %w", err)
	}
	if err := tx.Commit(); err != nil {
		p.logger.Error("commit event", zap.String("event_type", eventType), zap.Error(err))
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// PublishMilestoneReached emits project.milestone.reached, consumed by notifications-api
// to email the tenant when a project milestone is completed.
func (p *Publisher) PublishMilestoneReached(ctx context.Context, tenantID, milestoneID uuid.UUID, payload map[string]any) error {
	return p.publish(ctx, "project", "milestone.reached", milestoneID, tenantID, payload)
}
