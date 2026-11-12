// Package crmsync keeps projects' copies of CRM contact details in step with marketflow, the
// customer master. Projects copies one thing: a project's client, kept in the project metadata as
// crm_contact_id plus crm_contact_name (the display name, so lists need no CRM call).
package crmsync

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	eventslib "github.com/Bengo-Hub/shared-events"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/predicate"
	"github.com/bengobox/projects-service/internal/ent/project"
	"github.com/bengobox/projects-service/internal/platform/marketflow"
)

const (
	contactUpdatedSubject = "crm.contact.updated"
	contactDeletedSubject = "crm.contact.deleted"
	contactUpdatedDurable = "projects-crm-contact-updated"
	contactDeletedDurable = "projects-crm-contact-deleted"

	// Metadata keys, the same as projects.MetaCRMContactID and projects.MetaCRMContactName.
	metaContactID   = "crm_contact_id"
	metaContactName = "crm_contact_name"

	// retryDelay spaces out redeliveries while the database or the CRM is failing, so an outage
	// does not use up the durable's deliveries at once.
	retryDelay = 30 * time.Second
	maxDeliver = 5
)

// ContactFetcher reads one CRM contact; *marketflow.Client implements it.
type ContactFetcher interface {
	GetContact(ctx context.Context, tenantID, contactID uuid.UUID) (marketflow.Contact, error)
}

// ContactEventsConsumer clears the copied client name when the person is erased in the CRM.
//
//   - crm.contact.deleted ({contact_id}): crm_contact_id and crm_contact_name are removed from
//     every project that names the contact.
//   - crm.contact.updated ({contact_id, fields}): marketflow publishes ids only and does not say
//     why the contact changed, so when a project still carries a name for the contact the
//     consumer reads it back from the CRM. If it is now erased (first name "Erased" and no last
//     name, email or phone; see marketflow.Contact.Erased) crm_contact_name is removed and the
//     link is kept. Any other edit changes nothing here.
//
// Projects are matched in every tenant on the canonical lower-case contact id: CRM contact ids
// are random UUIDs minted by marketflow, so a match anywhere is the same person (the crm-links
// check makes the same choice). Both handlers are idempotent. Logs carry ids and counts only.
type ContactEventsConsumer struct {
	db  *ent.Client
	crm ContactFetcher
	log *zap.Logger
}

// NewContactEventsConsumer builds the consumer. crm may be a disabled *marketflow.Client: updates
// are then retried until the durable gives up, and deletes still apply.
func NewContactEventsConsumer(db *ent.Client, crm ContactFetcher, log *zap.Logger) *ContactEventsConsumer {
	if log == nil {
		log = zap.NewNop()
	}
	return &ContactEventsConsumer{db: db, crm: crm, log: log.Named("crmsync.contact_events")}
}

// Start binds the two durables on the stream that holds crm.> (creating a "crm" stream when none
// does yet, as pos-api does). Blocks until ctx is done, so run it in a goroutine.
func (c *ContactEventsConsumer) Start(ctx context.Context, nc *nats.Conn) error {
	if nc == nil {
		c.log.Warn("NATS not available; crm contact consumer disabled")
		return nil
	}
	js, err := nc.JetStream()
	if err != nil {
		return fmt.Errorf("crm contact events: jetstream init: %w", err)
	}
	stream, err := crmStream(js)
	if err != nil {
		return fmt.Errorf("crm contact events: %w", err)
	}
	opts := func(durable string) []nats.SubOpt {
		return []nats.SubOpt{
			nats.Durable(durable),
			nats.AckExplicit(),
			nats.AckWait(30 * time.Second),
			nats.MaxDeliver(maxDeliver),
			// Replays what is still in the stream on first start, so an erasure from the last
			// few days is applied too; both handlers are idempotent.
			nats.DeliverAll(),
		}
	}
	eventslib.SubscribeQueueWithRebind(c.log, js, stream, contactUpdatedSubject, contactUpdatedDurable,
		c.wrap(c.handleUpdated), opts(contactUpdatedDurable)...)
	eventslib.SubscribeQueueWithRebind(c.log, js, stream, contactDeletedSubject, contactDeletedDurable,
		c.wrap(c.handleDeleted), opts(contactDeletedDurable)...)
	c.log.Info("crm contact subscribers active", zap.String("stream", stream))

	<-ctx.Done()
	return nil
}

// crmStream finds the stream that holds crm.contact.updated, or creates "crm" for crm.>.
func crmStream(js nats.JetStreamContext) (string, error) {
	if name, err := js.StreamNameBySubject(contactUpdatedSubject); err == nil && name != "" {
		return name, nil
	}
	if _, err := js.AddStream(&nats.StreamConfig{
		Name:      "crm",
		Subjects:  []string{"crm.>"},
		Retention: nats.LimitsPolicy,
		MaxAge:    72 * time.Hour,
		Storage:   nats.FileStorage,
	}); err != nil && !errors.Is(err, nats.ErrStreamNameAlreadyInUse) {
		if name, lookupErr := js.StreamNameBySubject(contactUpdatedSubject); lookupErr == nil && name != "" {
			return name, nil
		}
		return "", fmt.Errorf("ensure crm stream: %w", err)
	}
	return "crm", nil
}

// settleMsg is the part of *nats.Msg wrap uses.
type settleMsg interface {
	Ack(opts ...nats.AckOpt) error
	NakWithDelay(delay time.Duration, opts ...nats.AckOpt) error
}

// wrap decodes the event and settles the message from the handler's result: acked on success or
// when the event cannot be read (a retry would not help), redelivered after retryDelay on error.
// The server stops redelivering at maxDeliver.
func (c *ContactEventsConsumer) wrap(h func(context.Context, *eventslib.Event) error) nats.MsgHandler {
	return func(msg *nats.Msg) { c.settle(msg, msg.Data, h) }
}

func (c *ContactEventsConsumer) settle(m settleMsg, data []byte, h func(context.Context, *eventslib.Event) error) {
	evt, err := eventslib.FromJSON(data)
	if err != nil {
		c.log.Warn("crm contact event: unreadable, acking")
		_ = m.Ack()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := h(ctx, evt); err != nil {
		c.log.Warn("crm contact event: apply failed, retrying later",
			zap.String("tenant_id", evt.TenantID.String()), zap.String("contact_id", payloadContactID(evt).String()))
		_ = m.NakWithDelay(retryDelay)
		return
	}
	_ = m.Ack()
}

func (c *ContactEventsConsumer) handleDeleted(ctx context.Context, evt *eventslib.Event) error {
	id := payloadContactID(evt)
	if id == uuid.Nil {
		return nil
	}
	_, err := c.ContactDeleted(ctx, id)
	return err
}

func (c *ContactEventsConsumer) handleUpdated(ctx context.Context, evt *eventslib.Event) error {
	id := payloadContactID(evt)
	if id == uuid.Nil || evt.TenantID == uuid.Nil {
		return nil
	}
	_, err := c.ContactUpdated(ctx, evt.TenantID, id)
	return err
}

// payloadContactID reads contact_id from the payload, falling back to the event's aggregate id
// (marketflow sets both to the contact). uuid.Nil when neither is a UUID.
func payloadContactID(evt *eventslib.Event) uuid.UUID {
	if s, ok := evt.Payload["contact_id"].(string); ok {
		if id, err := uuid.Parse(strings.TrimSpace(s)); err == nil {
			return id
		}
	}
	return evt.AggregateID
}

// ContactDeleted removes crm_contact_id and crm_contact_name from every project, in any tenant,
// that names the contact. It answers how many projects changed (0 on a repeat delivery).
func (c *ContactEventsConsumer) ContactDeleted(ctx context.Context, contactID uuid.UUID) (int, error) {
	n, err := c.rewrite(ctx, contactID, func(meta map[string]any) bool {
		delete(meta, metaContactID)
		delete(meta, metaContactName)
		return true
	})
	if err != nil {
		return 0, fmt.Errorf("unlink deleted contact: %w", err)
	}
	if n > 0 {
		c.log.Info("crm contact deleted: projects unlinked", zap.String("contact_id", contactID.String()), zap.Int("projects", n))
	}
	return n, nil
}

// ContactUpdated removes the copied crm_contact_name from every project naming the contact when
// the CRM now holds it as erased. A contact no project names with a name, a contact the CRM no
// longer knows (its delete event unlinks it) and a contact that is not erased change nothing. An
// error means the CRM or the database could not answer and the event should be retried.
func (c *ContactEventsConsumer) ContactUpdated(ctx context.Context, tenantID, contactID uuid.UUID) (int, error) {
	named, err := c.db.Project.Query().Where(referencesContact(contactID)).All(ctx)
	if err != nil {
		return 0, fmt.Errorf("find projects: %w", err)
	}
	if !anyHasName(named) {
		return 0, nil
	}
	contact, err := c.crm.GetContact(ctx, tenantID, contactID)
	if errors.Is(err, marketflow.ErrContactNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read crm contact: %w", err)
	}
	if !contact.Erased() {
		return 0, nil
	}
	n, err := c.rewrite(ctx, contactID, func(meta map[string]any) bool {
		if _, ok := meta[metaContactName]; !ok {
			return false
		}
		delete(meta, metaContactName)
		return true
	})
	if err != nil {
		return 0, fmt.Errorf("blank erased contact name: %w", err)
	}
	if n > 0 {
		c.log.Info("crm contact erased: copied name removed from projects",
			zap.String("tenant_id", tenantID.String()), zap.String("contact_id", contactID.String()), zap.Int("projects", n))
	}
	return n, nil
}

func anyHasName(ps []*ent.Project) bool {
	for _, p := range ps {
		if s, _ := p.Metadata[metaContactName].(string); strings.TrimSpace(s) != "" {
			return true
		}
	}
	return false
}

// rewrite applies edit to the metadata of every project naming the contact, in one transaction,
// and saves the ones edit changed. It answers how many were saved.
func (c *ContactEventsConsumer) rewrite(ctx context.Context, contactID uuid.UUID, edit func(map[string]any) bool) (int, error) {
	tx, err := c.db.Tx(ctx)
	if err != nil {
		return 0, err
	}
	ps, err := tx.Project.Query().Where(referencesContact(contactID)).All(ctx)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	n := 0
	for _, p := range ps {
		meta := make(map[string]any, len(p.Metadata))
		for k, v := range p.Metadata {
			meta[k] = v
		}
		if !edit(meta) {
			continue
		}
		if err := tx.Project.UpdateOneID(p.ID).SetMetadata(meta).Exec(ctx); err != nil {
			_ = tx.Rollback()
			return 0, err
		}
		n++
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

// referencesContact matches projects whose metadata names the contact:
// lower(metadata ->> 'crm_contact_id') = the canonical lower-case id. The value goes through
// b.Arg so the driver numbers the placeholder.
func referencesContact(contactID uuid.UUID) predicate.Project {
	return predicate.Project(func(s *sql.Selector) {
		s.Where(sql.P(func(b *sql.Builder) {
			b.WriteString("lower(").Ident(s.C(project.FieldMetadata)).WriteString(" ->> '" + metaContactID + "') = ").Arg(contactID.String())
		}))
	})
}
