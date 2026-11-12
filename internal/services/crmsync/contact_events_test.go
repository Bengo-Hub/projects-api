package crmsync

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	eventslib "github.com/Bengo-Hub/shared-events"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/platform/marketflow"
	"github.com/bengobox/projects-service/internal/testutil"
)

// fakeCRM answers GetContact from a map and counts calls.
type fakeCRM struct {
	contacts map[uuid.UUID]marketflow.Contact
	err      error
	calls    int
	tenant   uuid.UUID
}

func (f *fakeCRM) GetContact(_ context.Context, tenantID, contactID uuid.UUID) (marketflow.Contact, error) {
	f.calls++
	f.tenant = tenantID
	if f.err != nil {
		return marketflow.Contact{}, f.err
	}
	c, ok := f.contacts[contactID]
	if !ok {
		return marketflow.Contact{}, marketflow.ErrContactNotFound
	}
	return c, nil
}

func mkProject(t *testing.T, db *ent.Client, tenant uuid.UUID, meta map[string]any) *ent.Project {
	t.Helper()
	c := db.Project.Create().SetTenantID(tenant).SetName("p").SetOwnerID(uuid.New())
	if meta != nil {
		c.SetMetadata(meta)
	}
	p, err := c.Save(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func metaOf(t *testing.T, db *ent.Client, id uuid.UUID) map[string]any {
	t.Helper()
	p, err := db.Project.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return p.Metadata
}

// A deleted CRM contact is unlinked from every project that names it, in any tenant and whatever
// the letter case of the stored id; other keys and other projects are untouched, and a second
// delivery changes nothing.
func TestContactDeletedUnlinksEveryProject(t *testing.T) {
	db := testutil.Client(t)
	ctx := context.Background()
	gone, other := uuid.New(), uuid.New()
	a := mkProject(t, db, uuid.New(), map[string]any{"crm_contact_id": gone.String(), "crm_contact_name": "Jane Doe", "billing_type": "fixed"})
	b := mkProject(t, db, uuid.New(), map[string]any{"crm_contact_id": strings.ToUpper(gone.String()), "crm_contact_name": "Jane Doe"})
	keep := mkProject(t, db, uuid.New(), map[string]any{"crm_contact_id": other.String(), "crm_contact_name": "Ann"})
	bare := mkProject(t, db, uuid.New(), nil)

	c := NewContactEventsConsumer(db, &fakeCRM{}, zap.NewNop())
	n, err := c.ContactDeleted(ctx, gone)
	if err != nil || n != 2 {
		t.Fatalf("ContactDeleted = %d, %v; want 2 projects", n, err)
	}
	for _, id := range []uuid.UUID{a.ID, b.ID} {
		m := metaOf(t, db, id)
		if _, ok := m["crm_contact_id"]; ok {
			t.Errorf("project %s still has crm_contact_id", id)
		}
		if _, ok := m["crm_contact_name"]; ok {
			t.Errorf("project %s still has crm_contact_name", id)
		}
	}
	if metaOf(t, db, a.ID)["billing_type"] != "fixed" {
		t.Error("other metadata keys must stay")
	}
	if m := metaOf(t, db, keep.ID); m["crm_contact_id"] != other.String() || m["crm_contact_name"] != "Ann" {
		t.Errorf("another contact's project changed: %v", m)
	}
	if len(metaOf(t, db, bare.ID)) != 0 {
		t.Error("a project with no metadata must stay empty")
	}
	if n, err := c.ContactDeleted(ctx, gone); err != nil || n != 0 {
		t.Fatalf("second delivery = %d, %v; want 0", n, err)
	}
}

// An update that leaves the contact erased in the CRM blanks the copied name on every project
// that names it, keeps the link, and is a no-op the second time (no CRM call either).
func TestContactUpdatedErasedBlanksName(t *testing.T) {
	db := testutil.Client(t)
	ctx := context.Background()
	tenant, erased := uuid.New(), uuid.New()
	a := mkProject(t, db, tenant, map[string]any{"crm_contact_id": erased.String(), "crm_contact_name": "Jane Doe"})
	b := mkProject(t, db, uuid.New(), map[string]any{"crm_contact_id": erased.String(), "crm_contact_name": "Jane Doe"})
	crm := &fakeCRM{contacts: map[uuid.UUID]marketflow.Contact{erased: {ID: erased, FirstName: "Erased"}}}
	c := NewContactEventsConsumer(db, crm, zap.NewNop())

	n, err := c.ContactUpdated(ctx, tenant, erased)
	if err != nil || n != 2 {
		t.Fatalf("ContactUpdated = %d, %v; want 2 projects", n, err)
	}
	if crm.tenant != tenant {
		t.Errorf("CRM asked under tenant %s, want the event's tenant %s", crm.tenant, tenant)
	}
	for _, id := range []uuid.UUID{a.ID, b.ID} {
		m := metaOf(t, db, id)
		if _, ok := m["crm_contact_name"]; ok {
			t.Errorf("project %s still has the copied name", id)
		}
		if m["crm_contact_id"] != erased.String() {
			t.Errorf("project %s lost its link: %v", id, m)
		}
	}
	calls := crm.calls
	if n, err := c.ContactUpdated(ctx, tenant, erased); err != nil || n != 0 || crm.calls != calls {
		t.Fatalf("second delivery = %d, %v, CRM calls %d -> %d; want no change and no call", n, err, calls, crm.calls)
	}
}

// An ordinary edit (the contact is not erased) changes nothing; a contact no project names is
// never looked up; a contact the CRM no longer knows changes nothing (its delete event unlinks it).
func TestContactUpdatedNotErased(t *testing.T) {
	db := testutil.Client(t)
	ctx := context.Background()
	tenant, live, unknown, unlinked := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	p := mkProject(t, db, tenant, map[string]any{"crm_contact_id": live.String(), "crm_contact_name": "Jane Doe"})
	q := mkProject(t, db, tenant, map[string]any{"crm_contact_id": unknown.String(), "crm_contact_name": "Old"})
	crm := &fakeCRM{contacts: map[uuid.UUID]marketflow.Contact{live: {ID: live, FirstName: "Janet", Email: "j@example.com"}}}
	c := NewContactEventsConsumer(db, crm, zap.NewNop())

	if n, err := c.ContactUpdated(ctx, tenant, live); err != nil || n != 0 || metaOf(t, db, p.ID)["crm_contact_name"] != "Jane Doe" {
		t.Fatalf("live contact: %d %v %v", n, err, metaOf(t, db, p.ID))
	}
	if n, err := c.ContactUpdated(ctx, tenant, unknown); err != nil || n != 0 || metaOf(t, db, q.ID)["crm_contact_name"] != "Old" {
		t.Fatalf("unknown contact: %d %v", n, err)
	}
	calls := crm.calls
	if n, err := c.ContactUpdated(ctx, tenant, unlinked); err != nil || n != 0 || crm.calls != calls {
		t.Fatalf("unlinked contact: %d %v, CRM calls %d -> %d", n, err, calls, crm.calls)
	}
}

// A CRM that cannot be reached is an error, so the event is redelivered; nothing changes.
func TestContactUpdatedCRMDownRetries(t *testing.T) {
	db := testutil.Client(t)
	ctx := context.Background()
	tenant, id := uuid.New(), uuid.New()
	p := mkProject(t, db, tenant, map[string]any{"crm_contact_id": id.String(), "crm_contact_name": "Jane Doe"})
	c := NewContactEventsConsumer(db, &fakeCRM{err: errors.New("crm down")}, zap.NewNop())

	if _, err := c.ContactUpdated(ctx, tenant, id); err == nil {
		t.Fatal("want an error so the event is retried")
	}
	if metaOf(t, db, p.ID)["crm_contact_name"] != "Jane Doe" {
		t.Fatal("nothing changes while the CRM is down")
	}
}

// The logs carry ids and counts only, never a name.
func TestContactEventsLogIdsOnly(t *testing.T) {
	db := testutil.Client(t)
	ctx := context.Background()
	tenant, id := uuid.New(), uuid.New()
	mkProject(t, db, tenant, map[string]any{"crm_contact_id": id.String(), "crm_contact_name": "Jane Doe"})
	core, logs := observer.New(zap.DebugLevel)
	c := NewContactEventsConsumer(db, &fakeCRM{contacts: map[uuid.UUID]marketflow.Contact{id: {ID: id, FirstName: "Erased"}}}, zap.New(core))

	if _, err := c.ContactUpdated(ctx, tenant, id); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ContactDeleted(ctx, id); err != nil {
		t.Fatal(err)
	}
	if logs.Len() == 0 {
		t.Fatal("want a log line per change")
	}
	for _, e := range logs.All() {
		for k, v := range e.ContextMap() {
			switch k {
			case "tenant_id", "contact_id", "projects":
			default:
				t.Errorf("unexpected log field %q=%v (ids only)", k, v)
			}
		}
		if strings.Contains(e.Message, "Jane") {
			t.Errorf("log message carries a name: %q", e.Message)
		}
	}
}

type fakeMsg struct {
	acked    bool
	nakDelay time.Duration
}

func (f *fakeMsg) Ack(...nats.AckOpt) error { f.acked = true; return nil }
func (f *fakeMsg) NakWithDelay(d time.Duration, _ ...nats.AckOpt) error {
	f.nakDelay = d
	return nil
}

// An unreadable event is acked (a retry cannot fix it); a failed apply is redelivered after
// retryDelay; a successful one is acked.
func TestSettle(t *testing.T) {
	c := NewContactEventsConsumer(nil, &fakeCRM{}, zap.NewNop())
	ok := func(context.Context, *eventslib.Event) error { return nil }
	fail := func(context.Context, *eventslib.Event) error { return errors.New("db down") }
	evt := eventslib.NewEvent("contact.deleted", "crm", uuid.New(), uuid.New(), map[string]any{"contact_id": uuid.NewString()})
	data, err := evt.ToJSON()
	if err != nil {
		t.Fatal(err)
	}

	m := &fakeMsg{}
	c.settle(m, []byte("not json"), fail)
	if !m.acked || m.nakDelay != 0 {
		t.Fatalf("unreadable: %+v, want an ack", m)
	}
	m = &fakeMsg{}
	c.settle(m, data, fail)
	if m.acked || m.nakDelay != retryDelay {
		t.Fatalf("failed apply: %+v, want a delayed nak", m)
	}
	m = &fakeMsg{}
	c.settle(m, data, ok)
	if !m.acked {
		t.Fatalf("applied: %+v, want an ack", m)
	}
}

// contact_id comes from the payload, else the aggregate id.
func TestPayloadContactID(t *testing.T) {
	id, agg := uuid.New(), uuid.New()
	if got := payloadContactID(eventslib.NewEvent("contact.updated", "crm", agg, uuid.New(), map[string]any{"contact_id": strings.ToUpper(id.String())})); got != id {
		t.Fatalf("payload id: %s, want %s", got, id)
	}
	if got := payloadContactID(eventslib.NewEvent("contact.updated", "crm", agg, uuid.New(), map[string]any{})); got != agg {
		t.Fatalf("aggregate id fallback: %s, want %s", got, agg)
	}
}
