package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/testutil"
)

type slugLookup map[string]uuid.UUID

func (l slugLookup) GetTenantDetails(_ context.Context, slug string) (sharedcache.TenantDetails, error) {
	if id, ok := l[slug]; ok {
		return sharedcache.TenantDetails{ID: id.String(), Slug: slug}, nil
	}
	return sharedcache.TenantDetails{}, errors.New("not found")
}

func crmLinksRouter(h *CRMLinksHandler) http.Handler {
	r := chi.NewRouter()
	r.Get("/api/v1/s2s/{tenant}/crm-links/{contactID}", h.Referenced)
	return r
}

func askCRMLinks(r http.Handler, tenant, contact, key string) (int, map[string]any) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/s2s/"+tenant+"/crm-links/"+contact, nil)
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

// marketflow asks, before erasing a CRM contact, whether projects still links it. A project carrying
// it (in any tenant) answers yes whichever tenant asks; no project answers no. A slug resolves through
// auth-api (unknown: 404); the route needs the service key; a failed read is a 500, never "no".
func TestCRMLinksAnswers(t *testing.T) {
	tenantA, tenantB, linked, free := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	h := &CRMLinksHandler{
		referenced: func(_ context.Context, contactID uuid.UUID) (bool, error) {
			return contactID == linked, nil // a project under tenant A
		},
		tenants: slugLookup{"tenant-a": tenantA},
		key:     "svc-key",
		log:     zap.NewNop(),
	}
	r := crmLinksRouter(h)

	if code, body := askCRMLinks(r, tenantA.String(), linked.String(), "svc-key"); code != http.StatusOK || body["referenced"] != true || len(body) != 1 {
		t.Fatalf("referenced by tenant id: %d %v", code, body)
	}
	if code, body := askCRMLinks(r, "tenant-a", linked.String(), "svc-key"); code != http.StatusOK || body["referenced"] != true {
		t.Fatalf("referenced by slug: %d %v", code, body)
	}
	if code, body := askCRMLinks(r, tenantB.String(), linked.String(), "svc-key"); code != http.StatusOK || body["referenced"] != true {
		t.Fatalf("a link under another tenant must count: %d %v", code, body)
	}
	if code, body := askCRMLinks(r, tenantA.String(), free.String(), "svc-key"); code != http.StatusOK || body["referenced"] != false || len(body) != 1 {
		t.Fatalf("unreferenced: %d %v", code, body)
	}
	if code, _ := askCRMLinks(r, "no-such-tenant", linked.String(), "svc-key"); code != http.StatusNotFound {
		t.Fatalf("unknown slug: %d", code)
	}
	if code, _ := askCRMLinks(r, uuid.Nil.String(), linked.String(), "svc-key"); code != http.StatusNotFound {
		t.Fatalf("nil tenant uuid: %d", code)
	}
	if code, _ := askCRMLinks(r, tenantA.String(), "not-a-uuid", "svc-key"); code != http.StatusBadRequest {
		t.Fatalf("bad contact id: %d", code)
	}
	if code, _ := askCRMLinks(r, tenantA.String(), linked.String(), ""); code != http.StatusUnauthorized {
		t.Fatalf("no key: %d", code)
	}
	if code, _ := askCRMLinks(r, tenantA.String(), linked.String(), "wrong"); code != http.StatusUnauthorized {
		t.Fatalf("wrong key: %d", code)
	}
	h.referenced = func(context.Context, uuid.UUID) (bool, error) { return false, errors.New("db down") }
	if code, body := askCRMLinks(r, tenantA.String(), linked.String(), "svc-key"); code != http.StatusInternalServerError || body["referenced"] != nil {
		t.Fatalf("a failed read must not answer false: %d %v", code, body)
	}
}

// With no service key configured the handler refuses every call, even one sending an empty key.
func TestCRMLinksRefusesWhenKeyEmpty(t *testing.T) {
	h := &CRMLinksHandler{
		referenced: func(context.Context, uuid.UUID) (bool, error) { return false, nil },
		tenants:    slugLookup{},
		key:        "",
		log:        zap.NewNop(),
	}
	r := crmLinksRouter(h)
	for _, key := range []string{"", "anything"} {
		if code, _ := askCRMLinks(r, uuid.NewString(), uuid.NewString(), key); code != http.StatusUnauthorized {
			t.Fatalf("empty configured key, sent %q: %d", key, code)
		}
	}
}

// The real query: the contact id sits in project metadata (JSON), any tenant, any letter case.
// Runs against the throwaway Postgres schema, skipped when none is reachable.
func TestProjectReferencesContactDB(t *testing.T) {
	db := testutil.Client(t)
	ctx := context.Background()
	tenantA, tenantB := uuid.New(), uuid.New()
	linked, free := uuid.New(), uuid.New()

	mk := func(tenant uuid.UUID, name string, meta map[string]any) {
		t.Helper()
		c := db.Project.Create().SetTenantID(tenant).SetName(name).SetOwnerID(uuid.New())
		if meta != nil {
			c.SetMetadata(meta)
		}
		if _, err := c.Save(ctx); err != nil {
			t.Fatal(err)
		}
	}
	mk(tenantA, "no metadata", nil)
	mk(tenantA, "other key", map[string]any{"client_id": linked.String()})
	mk(tenantB, "other contact", map[string]any{"crm_contact_id": uuid.NewString()})

	if got, err := ProjectReferencesContact(ctx, db, linked); err != nil || got {
		t.Fatalf("before the link: %v %v", got, err)
	}
	mk(tenantA, "linked", map[string]any{"crm_contact_id": linked.String(), "crm_contact_name": "Acme"})

	h := NewCRMLinksHandler(db, slugLookup{}, "svc-key", zap.NewNop())
	r := crmLinksRouter(h)
	if code, body := askCRMLinks(r, tenantA.String(), linked.String(), "svc-key"); code != http.StatusOK || body["referenced"] != true {
		t.Fatalf("linked contact: %d %v", code, body)
	}
	// Another tenant asking still finds it: the contact id is unique, a mismatch can only keep the contact.
	if code, body := askCRMLinks(r, tenantB.String(), linked.String(), "svc-key"); code != http.StatusOK || body["referenced"] != true {
		t.Fatalf("other tenant: %d %v", code, body)
	}
	if code, body := askCRMLinks(r, tenantA.String(), free.String(), "svc-key"); code != http.StatusOK || body["referenced"] != false {
		t.Fatalf("unlinked contact: %d %v", code, body)
	}

	// Metadata saved with an upper-case id still matches.
	upper := uuid.New()
	mk(tenantB, "upper", map[string]any{"crm_contact_id": upperCase(upper.String())})
	if got, err := ProjectReferencesContact(ctx, db, upper); err != nil || !got {
		t.Fatalf("upper-case metadata: %v %v", got, err)
	}
}

func upperCase(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'f' {
			b[i] = c - 32
		}
	}
	return string(b)
}
