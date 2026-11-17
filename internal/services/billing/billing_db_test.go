package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/platform/erp"
	"github.com/bengobox/projects-service/internal/platform/treasury"
	"github.com/bengobox/projects-service/internal/services/milestones"
	"github.com/bengobox/projects-service/internal/testutil"
)

// fakeTreasury mirrors treasury-api's S2S invoice routes: a create always creates (treasury does
// not dedupe on the reference, so a second create of a live reference fails the test), the
// create must carry keep_draft and comes back a draft (the firm approves and sends it in treasury,
// owner 17 November 2026), by-reference answers 404 when there is none, and the
// summaries carry balance_due.
type fakeTreasury struct {
	mu      sync.Mutex
	byRef   map[string]*treasury.Invoice // refType|refID
	creates []map[string]any
	seq     int
}

func (f *fakeTreasury) handler(t *testing.T, tenant uuid.UUID) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("X-API-Key") != "k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		base := "/api/v1/s2s/" + tenant.String() + "/invoices"
		q := r.URL.Query()
		switch {
		case r.Method == http.MethodPost && r.URL.Path == base:
			if r.Header.Get("X-User-ID") == "" {
				t.Error("create without X-User-ID")
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			key := fmt.Sprint(body["reference_type"]) + "|" + fmt.Sprint(body["reference_id"])
			if inv, ok := f.byRef[key]; ok && !IsDead(inv) {
				t.Errorf("duplicate create for live reference %s", key)
			}
			if _, set := body["invoice_type"]; set {
				t.Errorf("invoice_type sent: %v (treasury rejects \"invoice\")", body["invoice_type"])
			}
			if body["keep_draft"] != true {
				t.Errorf("create without keep_draft: %v (treasury would approve it)", body["keep_draft"])
			}
			f.creates = append(f.creates, body)
			f.seq++
			refID := uuid.MustParse(fmt.Sprint(body["reference_id"]))
			inv := &treasury.Invoice{ID: uuid.New(), InvoiceNumber: fmt.Sprintf("INV-%04d", f.seq), Status: "draft",
				PaymentStatus: "unpaid", Currency: "KES", AmountDue: 100, ReferenceType: fmt.Sprint(body["reference_type"]), ReferenceID: &refID}
			f.byRef[key] = inv
			_ = json.NewEncoder(w).Encode(inv)
		case r.Method == http.MethodGet && r.URL.Path == base+"/by-reference":
			inv, ok := f.byRef[q.Get("reference_type")+"|"+q.Get("reference_id")]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"invoice not found"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(inv)
		case r.Method == http.MethodGet && r.URL.Path == base+"/by-references":
			out := []*treasury.Invoice{}
			for _, id := range strings.Split(q.Get("reference_ids"), ",") {
				if inv, ok := f.byRef[q.Get("reference_type")+"|"+id]; ok {
					out = append(out, inv)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": out})
		case r.Method == http.MethodGet && r.URL.Path == base+"/by-project":
			out := []*treasury.Invoice{}
			for _, inv := range f.byRef {
				if q.Get("include_drafts") == "true" || inv.Status != "draft" {
					out = append(out, inv)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": out, "total": len(out)})
		default:
			t.Errorf("unexpected treasury call %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

// void marks the invoice of a reference void, as treasury's void route does.
func (f *fakeTreasury) void(refType string, refID uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byRef[refType+"|"+refID.String()].Status = "void"
}

// fakeERP mirrors erp-api's billing routes: reserve tags entries with a reference and returns the
// tagged set, confirm overwrites the invoice id, release frees them.
type fakeERP struct {
	mu       sync.Mutex
	entries  []fakeEntry
	confirms []string // ref|invoice
	releases []string // ref|include_confirmed
}

type fakeEntry struct {
	erp.BillableEntry
	ref, invoice string
}

func (f *fakeERP) handler(t *testing.T, tenant uuid.UUID) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("X-API-Key") != "k" || r.Header.Get("X-Tenant-ID") != tenant.String() {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		ref := fmt.Sprint(body["billing_ref"])
		switch r.URL.Path {
		case "/api/v1/hrm/attendance/timesheets/billing/reserve":
			out := []map[string]any{}
			for i := range f.entries {
				e := &f.entries[i]
				if e.ref == "" && e.Date >= fmt.Sprint(body["from"]) && e.Date <= fmt.Sprint(body["to"]) {
					e.ref = ref
				}
				if e.ref == ref {
					out = append(out, map[string]any{"id": e.ID, "date": e.Date, "employee_id": e.EmployeeID,
						"employee_name": e.EmployeeName, "hours": fmt.Sprintf("%.2f", e.Hours)})
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": out})
		case "/api/v1/hrm/attendance/timesheets/billing/confirm":
			n := 0
			for i := range f.entries {
				if f.entries[i].ref == ref {
					f.entries[i].invoice = fmt.Sprint(body["invoice_id"])
					n++
				}
			}
			f.confirms = append(f.confirms, ref+"|"+fmt.Sprint(body["invoice_id"]))
			_ = json.NewEncoder(w).Encode(map[string]any{"updated": n})
		case "/api/v1/hrm/attendance/timesheets/billing/release":
			all := body["include_confirmed"] == true
			n := 0
			for i := range f.entries {
				if f.entries[i].ref == ref && (all || f.entries[i].invoice == "") {
					f.entries[i].ref, f.entries[i].invoice = "", ""
					n++
				}
			}
			f.releases = append(f.releases, fmt.Sprintf("%s|%v", ref, all))
			_ = json.NewEncoder(w).Encode(map[string]any{"released": n})
		case "/api/v1/hrm/attendance/timesheets/project-hours":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
		default:
			t.Errorf("unexpected ERP call %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

type rig struct {
	svc      *Service
	client   *ent.Client
	treasury *fakeTreasury
	erp      *fakeERP
	tenant   uuid.UUID
	user     uuid.UUID
}

func newRig(t *testing.T) *rig {
	t.Helper()
	client := testutil.Client(t)
	tenant := uuid.New()
	ft := &fakeTreasury{byRef: map[string]*treasury.Invoice{}}
	fe := &fakeERP{}
	ts := httptest.NewServer(ft.handler(t, tenant))
	es := httptest.NewServer(fe.handler(t, tenant))
	t.Cleanup(ts.Close)
	t.Cleanup(es.Close)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	svc := NewService(client, treasury.NewClient(ts.URL, "k"), erp.NewClient(es.URL, "k"), rdb, zap.NewNop())
	svc.now = func() time.Time { return time.Date(2026, 11, 17, 22, 30, 0, 0, time.UTC) } // 18 Nov in EAT
	return &rig{svc: svc, client: client, treasury: ft, erp: fe, tenant: tenant, user: uuid.New()}
}

func (r *rig) project(t *testing.T, meta map[string]any) *ent.Project {
	t.Helper()
	return r.client.Project.Create().SetTenantID(r.tenant).SetName("Office fit-out").SetCurrency("KES").
		SetOwnerID(r.user).SetMetadata(meta).SaveX(context.Background())
}

func (r *rig) milestone(t *testing.T, p *ent.Project, name string) *ent.Milestone {
	t.Helper()
	return r.client.Milestone.Create().SetTenantID(r.tenant).SetProjectID(p.ID).SetName(name).SaveX(context.Background())
}

func str(s string) *string   { return &s }
func pct(v float64) *float64 { return &v }

// A milestone bills its amount (or its share of the contract value) once: a second click returns
// the same invoice, the milestone keeps its id and number, and its price is locked while the
// invoice is live. Voiding it frees the price and the next invoice is a new one.
func TestMilestoneInvoiceIsIdempotentAndVoidable(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	client := uuid.New()
	p := r.project(t, map[string]any{"billing_type": "fixed", "contract_value": 100000.0, "crm_contact_id": client.String(), "crm_contact_name": "Acme Ltd"})
	survey := r.milestone(t, p, "Site survey")
	handover := r.milestone(t, p, "Handover")

	if _, err := r.svc.InvoiceMilestone(ctx, r.tenant, p.ID, survey.ID, r.user); !isValidation(err) {
		t.Fatalf("invoice without a price = %v, want a validation error", err)
	}
	if _, err := r.svc.SetMilestoneAmount(ctx, r.tenant, p.ID, survey.ID, AmountInput{Amount: str("25000.5")}); err != nil {
		t.Fatal(err)
	}
	view, err := r.svc.SetMilestoneAmount(ctx, r.tenant, p.ID, handover.ID, AmountInput{AmountPct: pct(30)})
	if err != nil || view.BillAmount != "30000.00" {
		t.Fatalf("share of contract = %+v %v, want 30000.00", view, err)
	}
	if _, err := r.svc.SetMilestoneAmount(ctx, r.tenant, p.ID, handover.ID, AmountInput{Amount: str("1"), AmountPct: pct(5)}); !isValidation(err) {
		t.Fatalf("amount and share together = %v", err)
	}

	first, err := r.svc.InvoiceMilestone(ctx, r.tenant, p.ID, survey.ID, r.user)
	if err != nil {
		t.Fatal(err)
	}
	again, err := r.svc.InvoiceMilestone(ctx, r.tenant, p.ID, survey.ID, r.user)
	if err != nil || again.ID != first.ID || len(r.treasury.creates) != 1 {
		t.Fatalf("second click = %+v %v with %d creates, want the first invoice", again, err, len(r.treasury.creates))
	}
	if first.Status != "draft" {
		t.Fatalf("milestone invoice status = %q, want a draft for the firm to approve", first.Status)
	}
	body := r.treasury.creates[0]
	lines, _ := body["lines"].([]any)
	line, _ := lines[0].(map[string]any)
	meta, _ := body["metadata"].(map[string]any)
	if body["crm_customer_id"] != client.String() || body["reference_type"] != treasury.RefProjectMilestone ||
		body["reference_id"] != survey.ID.String() || line["unit_price"] != "25000.5" || meta["project_id"] != p.ID.String() ||
		body["customer_name"] != "Acme Ltd" || line["tax_code"] != nil ||
		!strings.HasPrefix(fmt.Sprint(body["invoice_date"]), "2026-11-18") {
		t.Fatalf("create body = %v", body)
	}
	m := r.client.Milestone.GetX(ctx, survey.ID)
	if m.Metadata[MetaInvoiceID] != first.ID.String() || m.Metadata[MetaInvoiceNumber] != "INV-0001" {
		t.Fatalf("milestone metadata = %v", m.Metadata)
	}
	if _, err := r.svc.SetMilestoneAmount(ctx, r.tenant, p.ID, survey.ID, AmountInput{Amount: str("1")}); !isValidation(err) {
		t.Fatalf("repricing an invoiced milestone = %v, want refused", err)
	}
	ms := milestones.NewService(r.client, nil, zap.NewNop())
	var mverr milestones.ValidationError
	if err := ms.DeleteMilestone(ctx, r.tenant, p.ID, survey.ID); !errors.As(err, &mverr) {
		t.Fatalf("deleting an invoiced milestone = %v, want refused", err)
	}

	r.treasury.void(treasury.RefProjectMilestone, survey.ID)
	if _, err := r.svc.SetMilestoneAmount(ctx, r.tenant, p.ID, survey.ID, AmountInput{Amount: str("24000")}); err != nil {
		t.Fatalf("repricing after a void = %v", err)
	}
	if m := r.client.Milestone.GetX(ctx, survey.ID); m.Metadata[MetaInvoiceID] != nil {
		t.Fatalf("void link kept: %v", m.Metadata)
	}
	second, err := r.svc.InvoiceMilestone(ctx, r.tenant, p.ID, survey.ID, r.user)
	if err != nil || second.ID == first.ID || len(r.treasury.creates) != 2 {
		t.Fatalf("invoice after void = %+v %v (%d creates), want a new invoice", second, err, len(r.treasury.creates))
	}
	if ref := r.treasury.creates[1]["reference_id"]; ref == survey.ID.String() || ref != nextRef(survey.ID, first.ID).String() {
		t.Fatalf("reference after void = %v, want the chained one", ref)
	}

	ov, err := r.svc.Overview(ctx, r.tenant, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	var got *MilestoneBilling
	for i := range ov.Milestones {
		if ov.Milestones[i].ID == survey.ID {
			got = &ov.Milestones[i]
		}
	}
	if got == nil || got.Invoice == nil || got.Invoice.ID != second.ID || ov.TreasuryUnavailable {
		t.Fatalf("overview milestone = %+v (treasury down %v)", got, ov.TreasuryUnavailable)
	}
}

// A project must be billable and have a client before anything is invoiced.
func TestInvoiceNeedsBillableProjectWithClient(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	internal := r.project(t, map[string]any{"billing_type": "non_billable", "crm_contact_id": uuid.NewString()})
	noClient := r.project(t, map[string]any{"billing_type": "fixed"})
	for _, p := range []*ent.Project{internal, noClient} {
		m := r.milestone(t, p, "Phase 1")
		if _, err := r.svc.SetMilestoneAmount(ctx, r.tenant, p.ID, m.ID, AmountInput{Amount: str("100")}); err != nil {
			t.Fatal(err)
		}
		if _, err := r.svc.InvoiceMilestone(ctx, r.tenant, p.ID, m.ID, r.user); !isValidation(err) {
			t.Fatalf("project %v = %v, want a validation error", p.Metadata, err)
		}
	}
	if len(r.treasury.creates) != 0 {
		t.Fatal("nothing may reach treasury")
	}
	if _, err := r.svc.InvoiceMilestone(ctx, uuid.New(), internal.ID, uuid.New(), r.user); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another tenant = %v, want not found", err)
	}
}

// Time bills the period's reserved hours once, one line per person at the hourly rate; running
// the period again returns the same invoice and re-confirms; hours of a voided invoice go back to
// the pool and are billed under the next reference.
func TestTimeInvoiceBillsHoursOnce(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.project(t, map[string]any{"billing_type": "time_and_materials", "hourly_rate": "2500", "tax_code": "VAT-16", "crm_contact_id": uuid.NewString()})
	amina, brian := uuid.New(), uuid.New()
	r.erp.entries = []fakeEntry{
		{BillableEntry: erp.BillableEntry{ID: uuid.New(), Date: "2026-11-03", EmployeeID: brian, EmployeeName: "Brian Kamau", Hours: 2}},
		{BillableEntry: erp.BillableEntry{ID: uuid.New(), Date: "2026-11-04", EmployeeID: amina, EmployeeName: "Amina Otieno", Hours: 3.5}},
		{BillableEntry: erp.BillableEntry{ID: uuid.New(), Date: "2026-11-05", EmployeeID: amina, EmployeeName: "Amina Otieno", Hours: 1.25}},
		{BillableEntry: erp.BillableEntry{ID: uuid.New(), Date: "2026-12-01", EmployeeID: amina, EmployeeName: "Amina Otieno", Hours: 8}},
	}
	out, err := r.svc.InvoiceTime(ctx, r.tenant, p.ID, r.user, "2026-11-01", "2026-11-30")
	if err != nil {
		t.Fatal(err)
	}
	if out.Entries != 3 || out.Hours != 6.75 || out.Warning != "" || len(r.treasury.creates) != 1 {
		t.Fatalf("time invoice = %+v (%d creates)", out, len(r.treasury.creates))
	}
	lines, _ := r.treasury.creates[0]["lines"].([]any)
	l0, _ := lines[0].(map[string]any)
	l1, _ := lines[1].(map[string]any)
	if len(lines) != 2 || !strings.HasPrefix(fmt.Sprint(l0["description"]), "Amina Otieno") || l0["quantity"] != "4.75" ||
		l0["unit_price"] != "2500" || l1["quantity"] != "2.00" || l0["tax_code"] != "VAT-16" || l1["tax_rate"] != "16" {
		t.Fatalf("lines = %v", lines)
	}
	if out.Invoice.BalanceDue != 100 || out.Invoice.AmountDue != 100 {
		t.Fatalf("amounts not normalised: %+v", out.Invoice)
	}
	if len(r.erp.confirms) != 1 || !strings.HasSuffix(r.erp.confirms[0], out.Invoice.ID.String()) {
		t.Fatalf("confirms = %v", r.erp.confirms)
	}

	again, err := r.svc.InvoiceTime(ctx, r.tenant, p.ID, r.user, "2026-11-01", "2026-11-30")
	if err != nil || again.Invoice.ID != out.Invoice.ID || len(r.treasury.creates) != 1 || len(r.erp.confirms) != 2 {
		t.Fatalf("same period again = %+v %v", again, err)
	}
	// The November hours are taken: an overlapping period finds nothing new.
	if _, err := r.svc.InvoiceTime(ctx, r.tenant, p.ID, r.user, "2026-11-02", "2026-11-06"); !errors.Is(err, ErrNothingToBill) {
		t.Fatalf("overlapping period = %v, want nothing to bill", err)
	}

	base := periodRef(r.tenant, p.ID, treasury.RefProjectTime, "2026-11-01..2026-11-30")
	r.treasury.void(treasury.RefProjectTime, base)
	rebill, err := r.svc.InvoiceTime(ctx, r.tenant, p.ID, r.user, "2026-11-01", "2026-11-30")
	if err != nil || rebill.Invoice.ID == out.Invoice.ID || rebill.Entries != 3 {
		t.Fatalf("rebill after void = %+v %v", rebill, err)
	}
	if len(r.erp.releases) != 1 || r.erp.releases[0] != base.String()+"|true" {
		t.Fatalf("releases = %v, want the voided reference freed with its confirmed hours", r.erp.releases)
	}
}

// Time billing needs a time and materials project with an hourly rate and a sane period.
func TestTimeInvoiceValidation(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	fixed := r.project(t, map[string]any{"billing_type": "fixed", "crm_contact_id": uuid.NewString()})
	noRate := r.project(t, map[string]any{"billing_type": "time_and_materials", "crm_contact_id": uuid.NewString()})
	tm := r.project(t, map[string]any{"billing_type": "time_and_materials", "hourly_rate": "100", "crm_contact_id": uuid.NewString()})
	cases := []struct {
		p        *ent.Project
		from, to string
	}{
		{fixed, "2026-11-01", "2026-11-30"},
		{noRate, "2026-11-01", "2026-11-30"},
		{tm, "2026-11-30", "2026-11-01"},
		{tm, "2025-01-01", "2026-11-30"},
		{tm, "1 Nov", "2026-11-30"},
	}
	for _, c := range cases {
		if _, err := r.svc.InvoiceTime(ctx, r.tenant, c.p.ID, r.user, c.from, c.to); !isValidation(err) {
			t.Errorf("%v %s..%s = %v, want a validation error", c.p.Metadata["billing_type"], c.from, c.to, err)
		}
	}
	if _, err := r.svc.InvoiceTime(ctx, r.tenant, tm.ID, r.user, "2026-11-01", "2026-11-30"); !errors.Is(err, ErrNothingToBill) {
		t.Fatalf("no hours = %v, want nothing to bill", err)
	}
}

// A retainer month bills the monthly fee once.
func TestRetainerMonthIsIdempotent(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.project(t, map[string]any{"billing_type": "retainer", "retainer_amount": "150000", "retainer_hours": 40.0, "crm_contact_id": uuid.NewString()})
	a, err := r.svc.InvoiceRetainer(ctx, r.tenant, p.ID, r.user, "2026-11")
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.svc.InvoiceRetainer(ctx, r.tenant, p.ID, r.user, "2026-11")
	if err != nil || b.Invoice.ID != a.Invoice.ID || len(r.treasury.creates) != 1 {
		t.Fatalf("second run = %+v %v", b, err)
	}
	lines, _ := r.treasury.creates[0]["lines"].([]any)
	l0, _ := lines[0].(map[string]any)
	if l0["description"] != "Retainer, November 2026 (40 hours included)" || l0["unit_price"] != "150000" {
		t.Fatalf("retainer line = %v", l0)
	}
	if _, err := r.svc.InvoiceRetainer(ctx, r.tenant, p.ID, r.user, "2026-12"); err != nil || len(r.treasury.creates) != 2 {
		t.Fatalf("next month = %v", err)
	}
	if _, err := r.svc.InvoiceRetainer(ctx, r.tenant, p.ID, r.user, "Nov"); !isValidation(err) {
		t.Fatalf("bad month = %v", err)
	}
}

func isValidation(err error) bool {
	var v ValidationError
	return errors.As(err, &v)
}
