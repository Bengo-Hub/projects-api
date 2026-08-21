package usersync

import (
	"context"
	"fmt"
	"time"

	eventslib "github.com/Bengo-Hub/shared-events"
	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/projectmember"
	"github.com/bengobox/projects-service/internal/ent/userrole"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

const (
	authStream       = "auth"
	authDeletedSub   = "auth.user.deleted"
	authDeletedQueue = "projects-auth-user-deleted"
)

// AuthEventsConsumer reacts to auth-api's real hard-delete (AdminPurgeUser, published
// as auth.user.deleted) of a platform user. projects-api has no local User table at
// all — Service (sync.go) provisions users by calling out to auth-api rather than
// projecting a local copy — but UserRole and ProjectMember both carry a plain
// `user_id` UUID column (no ent edge/FK, since there's no local User to point at), so
// a deleted user leaves orphaned role/membership rows here unless explicitly cleaned
// up. Comment/Activity/TimeLog/TenderCommitteeMember also carry a user_id column but
// are left untouched: they're content/audit/work-record data, not identity data,
// mirroring auth-api's own precedent of leaving LoginAttempt/AuditLog rows in place.
type AuthEventsConsumer struct {
	client *ent.Client
	log    *zap.Logger
}

// NewAuthEventsConsumer creates the auth.user.deleted consumer.
func NewAuthEventsConsumer(client *ent.Client, log *zap.Logger) *AuthEventsConsumer {
	return &AuthEventsConsumer{client: client, log: log.Named("usersync.auth_events")}
}

// Start ensures the shared `auth` JetStream stream exists (projects-api otherwise only
// binds its own project.> stream — see internal/platform/events/nats.go) and registers
// the durable subscription. Blocks until ctx is done, so callers should run it in a
// goroutine.
func (c *AuthEventsConsumer) Start(ctx context.Context, nc *nats.Conn) error {
	if nc == nil {
		c.log.Warn("NATS not available; auth.user.deleted consumer disabled")
		return nil
	}
	js, err := nc.JetStream()
	if err != nil {
		return fmt.Errorf("auth-events: jetstream init: %w", err)
	}

	if _, err := js.StreamInfo(authStream); err != nil {
		if _, addErr := js.AddStream(&nats.StreamConfig{
			Name:      authStream,
			Subjects:  []string{"auth.>"},
			Retention: nats.LimitsPolicy,
			MaxAge:    72 * time.Hour,
			Storage:   nats.FileStorage,
		}); addErr != nil && addErr != nats.ErrStreamNameAlreadyInUse {
			c.log.Warn("auth-events: ensure auth stream failed", zap.Error(addErr))
		}
	}

	eventslib.SubscribeQueueWithRebind(
		c.log, js, authStream, authDeletedSub, authDeletedQueue,
		c.handle,
		nats.Durable(authDeletedQueue),
		nats.AckExplicit(),
		nats.AckWait(30*time.Second),
		nats.MaxDeliver(5),
		nats.DeliverAll(),
	)
	c.log.Info("auth.user.deleted subscriber active", zap.String("subject", authDeletedSub), zap.String("durable", authDeletedQueue))

	<-ctx.Done()
	return nil
}

func (c *AuthEventsConsumer) handle(msg *nats.Msg) {
	evt, err := eventslib.FromJSON(msg.Data)
	if err != nil {
		c.log.Error("auth.user.deleted: bad event, acking to drop", zap.Error(err))
		_ = msg.Ack()
		return
	}

	userID := parseUserID(evt.Payload["user_id"])
	if userID == uuid.Nil {
		userID = evt.AggregateID
	}
	if userID == uuid.Nil {
		c.log.Error("auth.user.deleted: missing user id, acking to drop")
		_ = msg.Ack()
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := c.deleteUser(ctx, userID); err != nil {
		c.log.Error("auth.user.deleted: delete failed, will retry",
			zap.String("user_id", userID.String()), zap.Error(err))
		_ = msg.Nak()
		return
	}
	c.log.Info("user roles/memberships removed for deleted auth user", zap.String("user_id", userID.String()))
	_ = msg.Ack()
}

// deleteUser removes every projects-api row keyed on this auth user id. No FK/ordering
// constraint exists between the two tables (both are plain UUID columns), but the
// delete still runs in one transaction for atomicity.
func (c *AuthEventsConsumer) deleteUser(ctx context.Context, userID uuid.UUID) error {
	tx, err := c.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	if _, err := tx.ProjectMember.Delete().Where(projectmember.UserID(userID)).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("delete project members: %w", err)
	}
	if _, err := tx.UserRole.Delete().Where(userrole.UserID(userID)).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("delete user roles: %w", err)
	}

	return tx.Commit()
}

// parseUserID accepts the payload's user_id value as either a string or, defensively,
// anything else json can hand back — returns uuid.Nil on anything unparsable.
func parseUserID(v interface{}) uuid.UUID {
	s, ok := v.(string)
	if !ok {
		return uuid.Nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return id
}
