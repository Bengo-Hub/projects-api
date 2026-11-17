// Package billing turns finished project work into invoices in treasury (sprint 15): a
// milestone's amount, a period of approved billable ERP hours, or a month of a retainer.
// Treasury approves a service's invoice on create (system origin); the firm sends it from
// treasury, which is when it goes to eTIMS and its revenue posts to the project.
//
// Treasury owns invoices, payments and eTIMS; ERP owns the hours. projects-api keeps only the
// invoice id, number and reference on a milestone's metadata. Every create carries a reference
// (project_milestone with the milestone id, project_time and project_retainer with an id derived
// from the project and period). Treasury's create does not dedupe on it, so the service reads the
// reference first under a Redis lock per reference: a retry or a second click returns the first
// invoice, and two concurrent clicks cannot both create one.
package billing

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	"github.com/bengobox/projects-service/internal/platform/erp"
	"github.com/bengobox/projects-service/internal/platform/treasury"
	"github.com/bengobox/projects-service/internal/services/projects"
)

var (
	// ErrNotFound is a project or milestone the tenant does not have.
	ErrNotFound = errors.New("not found")
	// ErrBusy means another request is creating the same invoice right now.
	ErrBusy = errors.New("this invoice is already being created; try again shortly")
	// ErrUnavailable means treasury (or ERP for time) is not configured or Redis is unreachable.
	ErrUnavailable = errors.New("billing is not available right now")
	// ErrNothingToBill means the period has no approved, billable, unbilled hours.
	ErrNothingToBill = errors.New("there are no approved billable hours to invoice in this period")
)

// ValidationError is a 422 with a message the user can act on.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

// lockTTL bounds one create: a treasury call (10 s timeout) plus ERP reserve and confirm.
const lockTTL = 2 * time.Minute

// refNamespace derives stable reference ids for time and retainer invoices.
var refNamespace = uuid.MustParse("5f0c6a52-6d0e-4bde-9f4e-7f2b6a1c9e11")

// Service creates project invoices and reads their state back from treasury.
type Service struct {
	client   *ent.Client
	treasury *treasury.Client
	erp      *erp.Client
	rdb      redis.UniversalClient
	log      *zap.Logger
	now      func() time.Time
}

// NewService builds the service. erp and rdb may be nil: without ERP, time billing is
// unavailable; without Redis (local runs), creates are not locked.
func NewService(client *ent.Client, tc *treasury.Client, ec *erp.Client, rdb redis.UniversalClient, log *zap.Logger) *Service {
	return &Service{client: client, treasury: tc, erp: ec, rdb: rdb, log: log.Named("billing.svc"), now: time.Now}
}

// Terms are the project's commercial terms, read from its metadata.
type Terms struct {
	BillingType      string   `json:"billing_type"`
	Currency         string   `json:"currency"`
	ContractValue    *float64 `json:"contract_value,omitempty"`
	HourlyRate       string   `json:"hourly_rate,omitempty"`
	RetainerAmount   string   `json:"retainer_amount,omitempty"`
	RetainerHours    *float64 `json:"retainer_hours,omitempty"`
	RetainerRollover bool     `json:"retainer_rollover"`
	ClientID         string   `json:"client_id,omitempty"`
	ClientName       string   `json:"client_name,omitempty"`
	CostCenterID     string   `json:"cost_center_id,omitempty"`
	TaxCode          string   `json:"tax_code,omitempty"`
}

func termsOf(p *ent.Project) Terms {
	m := p.Metadata
	str := func(k string) string { s, _ := m[k].(string); return s }
	num := func(k string) *float64 {
		if f, ok := m[k].(float64); ok {
			return &f
		}
		return nil
	}
	roll, _ := m[projects.MetaRetainerRoll].(bool)
	return Terms{
		BillingType: str(projects.MetaBillingType), Currency: p.Currency,
		ContractValue: num(projects.MetaContractValue), HourlyRate: str(projects.MetaHourlyRate),
		RetainerAmount: str(projects.MetaRetainerAmount), RetainerHours: num(projects.MetaRetainerHours),
		RetainerRollover: roll, ClientID: str(projects.MetaCRMContactID), ClientName: str(projects.MetaCRMContactName),
		CostCenterID: str(projects.MetaCostCenterID), TaxCode: str(projects.MetaTaxCode),
	}
}

func (s *Service) project(ctx context.Context, tenantID, projectID uuid.UUID) (*ent.Project, error) {
	p, err := s.client.Project.Query().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load project: %w", err)
	}
	return p, nil
}

// requireClient checks the project can be invoiced at all: billable, with a client.
func requireClient(t Terms) error {
	if t.BillingType == "" || t.BillingType == projects.BillingNonBillable {
		return ValidationError("this project is not billable; set its billing type first")
	}
	if t.ClientID == "" {
		return ValidationError("choose the project's client before invoicing")
	}
	return nil
}

// invoiceInput is the common part of every project invoice: for the project's CRM contact, dated
// today (EAT) and due in 30 days, tagged with the project (and its cost centre) so the revenue
// lands on the project in treasury, with the project's VAT on every line.
func (s *Service) invoiceInput(p *ent.Project, t Terms, refType string, refID uuid.UUID, notes string, lines []treasury.InvoiceLine) treasury.CreateInvoiceInput {
	today := eatDate(s.now())
	meta := map[string]any{"project_id": p.ID.String(), "project_name": p.Name, "source": "projects"}
	if t.CostCenterID != "" {
		meta["cost_center_id"] = t.CostCenterID
	}
	if rate, ok := projects.TaxRates[t.TaxCode]; ok {
		for i := range lines {
			lines[i].TaxCode, lines[i].TaxRate = t.TaxCode, rate
		}
	}
	return treasury.CreateInvoiceInput{
		CRMCustomerID: t.ClientID, CustomerName: t.ClientName,
		InvoiceDate: today, DueDate: today.AddDate(0, 0, 30), Currency: t.Currency, Notes: notes,
		ReferenceID: refID, ReferenceType: refType, Lines: lines, Metadata: meta,
	}
}

// locked runs fn holding the per-reference lock. Without Redis it just runs fn.
func (s *Service) locked(ctx context.Context, tenantID, refID uuid.UUID, fn func() error) error {
	if s.rdb == nil {
		return fn()
	}
	lock, ok, err := sharedcache.TryLock(ctx, s.rdb, "projects:billing:"+tenantID.String()+":"+refID.String(), lockTTL)
	if err != nil {
		return ErrUnavailable
	}
	if !ok {
		return ErrBusy
	}
	defer func() {
		if rerr := lock.Release(context.WithoutCancel(ctx)); rerr != nil {
			s.log.Warn("billing lock not released", zap.Error(rerr))
		}
	}()
	return fn()
}

// treasuryReady reports a usable treasury client.
func (s *Service) treasuryReady() error {
	if !s.treasury.Enabled() {
		return ErrUnavailable
	}
	return nil
}

// Dead invoices: treasury dedupes creates on the reference, so a voided or cancelled invoice would
// block its milestone or period for good. The next reference is derived from the dead invoice's
// id instead (a chain, the same on every pod and retry), up to maxRefChain voids per item.
const maxRefChain = 20

// IsDead reports a voided or cancelled invoice: it no longer bills anything.
func IsDead(inv *treasury.Invoice) bool {
	return inv != nil && (inv.Status == "void" || inv.Status == "cancelled")
}

// nextRef is the reference that follows a dead invoice's.
func nextRef(ref, deadInvoiceID uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(refNamespace, []byte(ref.String()+"|after|"+deadInvoiceID.String()))
}

// liveRef walks the reference chain from base: it returns the first reference whose invoice is
// live (with that invoice) or has none yet (nil invoice). onDead runs for each dead invoice passed.
func (s *Service) liveRef(ctx context.Context, tenantID uuid.UUID, refType string, base uuid.UUID, onDead func(ref uuid.UUID, inv *treasury.Invoice) error) (uuid.UUID, *treasury.Invoice, error) {
	ref := base
	for range maxRefChain {
		inv, err := s.treasury.InvoiceByReference(ctx, tenantID, refType, ref)
		if err != nil {
			return uuid.Nil, nil, err
		}
		if !IsDead(inv) {
			return ref, inv, nil
		}
		if onDead != nil {
			if err := onDead(ref, inv); err != nil {
				return uuid.Nil, nil, err
			}
		}
		ref = nextRef(ref, inv.ID)
	}
	return uuid.Nil, nil, ValidationError("this item has been invoiced and voided too many times; bill it from treasury directly")
}

// periodRef is the stable reference id of a project's time or retainer invoice for a period.
func periodRef(tenantID, projectID uuid.UUID, kind, period string) uuid.UUID {
	return uuid.NewSHA1(refNamespace, []byte(tenantID.String()+"|"+projectID.String()+"|"+kind+"|"+period))
}

// eat is East Africa Time; treasury dates documents by EAT calendar day.
var eat = time.FixedZone("EAT", 3*60*60)

// eatDate is t's EAT calendar date at midnight UTC, the form treasury stores document dates in.
func eatDate(t time.Time) time.Time {
	y, m, d := t.In(eat).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// money2 formats an amount with two decimals, the precision treasury invoices.
func money2(v float64) string { return fmt.Sprintf("%.2f", v) }

// parseRate reads a stored price (a decimal string) for estimates only; invoices send the string.
func parseRate(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil && v > 0
}
