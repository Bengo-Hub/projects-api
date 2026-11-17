package billing

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/platform/erp"
	"github.com/bengobox/projects-service/internal/platform/treasury"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/projects"
	"github.com/bengobox/projects-service/internal/services/txn"
)

const dateLayout = "2006-01-02"

// maxBillingSpan matches ERP's reserve limit.
const maxBillingSpan = 366 * 24 * time.Hour

// PeriodInvoice is the answer to a time or retainer invoice: the invoice and, for time, how many
// entries and hours it bills. Warning is set when the invoice exists but ERP did not record it on
// the hours yet; running the same period again finishes that.
type PeriodInvoice struct {
	Invoice *treasury.Invoice `json:"invoice"`
	Entries int               `json:"entries,omitempty"`
	Hours   float64           `json:"hours,omitempty"`
	Warning string            `json:"warning,omitempty"`
}

// timeLines groups the reserved entries into one line per person: their hours at the project's
// hourly rate. Lines are sorted by name so the same entries always give the same invoice.
func timeLines(entries []erp.BillableEntry, rate string, from, to time.Time) ([]treasury.InvoiceLine, float64) {
	type person struct {
		name  string
		hours float64
	}
	byEmployee := map[uuid.UUID]*person{}
	total := 0.0
	for _, e := range entries {
		p := byEmployee[e.EmployeeID]
		if p == nil {
			p = &person{name: e.EmployeeName}
			byEmployee[e.EmployeeID] = p
		}
		p.hours += e.Hours
		total += e.Hours
	}
	people := make([]*person, 0, len(byEmployee))
	for _, p := range byEmployee {
		people = append(people, p)
	}
	sort.Slice(people, func(i, j int) bool { return people[i].name < people[j].name })
	period := from.Format("2 Jan 2006") + " to " + to.Format("2 Jan 2006")
	lines := make([]treasury.InvoiceLine, 0, len(people))
	for _, p := range people {
		name := p.name
		if name == "" {
			name = "Team member"
		}
		lines = append(lines, treasury.InvoiceLine{
			Description: name + ", " + period, Unit: "hour", ItemType: "service",
			Quantity: strconv.FormatFloat(math.Round(p.hours*100)/100, 'f', 2, 64), UnitPrice: rate,
		})
	}
	return lines, math.Round(total*100) / 100
}

func parsePeriod(from, to string) (time.Time, time.Time, error) {
	f, err1 := time.Parse(dateLayout, from)
	t, err2 := time.Parse(dateLayout, to)
	if err1 != nil || err2 != nil {
		return time.Time{}, time.Time{}, ValidationError("from and to must be dates (YYYY-MM-DD)")
	}
	if t.Before(f) {
		return time.Time{}, time.Time{}, ValidationError("to must be on or after from")
	}
	if t.Sub(f) > maxBillingSpan {
		return time.Time{}, time.Time{}, ValidationError("a time invoice covers at most a year")
	}
	return f, t, nil
}

// InvoiceTime bills the project's approved, billable, unbilled ERP hours dated in [from, to] at
// its hourly rate. The period's reference id is stable, so the same period run twice returns the
// same invoice; hours already on an invoice are never reserved again.
func (s *Service) InvoiceTime(ctx context.Context, tenantID, projectID, userID uuid.UUID, fromS, toS string) (*PeriodInvoice, error) {
	from, to, err := parsePeriod(fromS, toS)
	if err != nil {
		return nil, err
	}
	if err := s.treasuryReady(); err != nil {
		return nil, err
	}
	if !s.erp.Enabled() {
		return nil, ErrUnavailable
	}
	p, err := s.project(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	t := termsOf(p)
	if err := requireClient(t); err != nil {
		return nil, err
	}
	if t.BillingType != projects.BillingTimeAndMaterials {
		return nil, ValidationError("only time and materials projects bill their hours")
	}
	if t.HourlyRate == "" {
		return nil, ValidationError("set the project's hourly rate before invoicing time")
	}
	base := periodRef(tenantID, projectID, treasury.RefProjectTime, from.Format(dateLayout)+".."+to.Format(dateLayout))
	var out PeriodInvoice
	err = s.locked(ctx, tenantID, base, func() error {
		// A voided or cancelled invoice gives its hours back, so the next reference bills them.
		release := func(dead uuid.UUID, _ *treasury.Invoice) error {
			_, err := s.erp.ReleaseBilling(ctx, tenantID, dead, true)
			return err
		}
		ref, existing, err := s.liveRef(ctx, tenantID, treasury.RefProjectTime, base, release)
		if err != nil {
			return err
		}
		if existing != nil {
			out.Invoice = existing
			out.Warning = s.confirm(ctx, tenantID, ref, existing.ID)
			return nil
		}
		res, err := s.erp.ReserveBilling(ctx, tenantID, projectID, ref, from, to)
		if err != nil {
			return err
		}
		if len(res.Entries) == 0 {
			return ErrNothingToBill
		}
		lines, hours := timeLines(res.Entries, t.HourlyRate, from, to)
		in := s.invoiceInput(p, t, treasury.RefProjectTime, ref,
			fmt.Sprintf("Time from %s to %s", from.Format("2 Jan 2006"), to.Format("2 Jan 2006")), lines)
		in.Metadata["billing_period"] = map[string]any{"from": from.Format(dateLayout), "to": to.Format(dateLayout)}
		in.Metadata["timesheet_billing_ref"] = ref.String()
		inv, err := s.treasury.CreateInvoice(ctx, tenantID, userID, in)
		if err != nil {
			// The entries stay reserved to this period's reference, so a retry bills the same set.
			return err
		}
		out = PeriodInvoice{Invoice: inv, Entries: len(res.Entries), Hours: hours}
		out.Warning = s.confirm(ctx, tenantID, ref, inv.ID)
		return s.recordProject(ctx, tenantID, projectID, "time_invoiced", map[string]any{
			"invoice_number": inv.InvoiceNumber, "hours": hours, "from": from.Format(dateLayout), "to": to.Format(dateLayout),
		})
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// confirm writes the invoice onto the reserved hours. A failure is not fatal: the invoice exists
// and the hours stay reserved, so nothing can bill them again; the next run of the period confirms.
func (s *Service) confirm(ctx context.Context, tenantID, ref, invoiceID uuid.UUID) string {
	if err := s.erp.ConfirmBilling(ctx, tenantID, ref, invoiceID); err != nil {
		var e *erp.Error
		if errors.As(err, &e) && e.Status == 409 {
			s.log.Error("time billing confirm conflict", zap.String("ref", ref.String()), zap.Error(err))
		} else {
			s.log.Warn("time billing confirm failed", zap.String("ref", ref.String()), zap.Error(err))
		}
		return "The invoice was created, but ERP has not marked the hours as billed yet. Run the same period again to finish."
	}
	return ""
}

// InvoiceRetainer bills one month of the project's retainer fee. month is YYYY-MM.
func (s *Service) InvoiceRetainer(ctx context.Context, tenantID, projectID, userID uuid.UUID, month string) (*PeriodInvoice, error) {
	m, err := time.Parse("2006-01", month)
	if err != nil {
		return nil, ValidationError("month must be YYYY-MM")
	}
	if err := s.treasuryReady(); err != nil {
		return nil, err
	}
	p, err := s.project(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	t := termsOf(p)
	if err := requireClient(t); err != nil {
		return nil, err
	}
	if t.BillingType != projects.BillingRetainer {
		return nil, ValidationError("only retainer projects bill a monthly fee")
	}
	if t.RetainerAmount == "" {
		return nil, ValidationError("set the project's monthly retainer fee first")
	}
	base := periodRef(tenantID, projectID, treasury.RefProjectRetainer, month)
	desc := "Retainer, " + m.Format("January 2006")
	if t.RetainerHours != nil && *t.RetainerHours > 0 {
		desc += fmt.Sprintf(" (%s hours included)", strconv.FormatFloat(*t.RetainerHours, 'f', -1, 64))
	}
	var out PeriodInvoice
	err = s.locked(ctx, tenantID, base, func() error {
		ref, existing, err := s.liveRef(ctx, tenantID, treasury.RefProjectRetainer, base, nil)
		if err != nil {
			return err
		}
		if existing != nil {
			out.Invoice = existing
			return nil
		}
		line := treasury.InvoiceLine{Description: desc, Quantity: "1", UnitPrice: t.RetainerAmount, Unit: "month", ItemType: "service"}
		in := s.invoiceInput(p, t, treasury.RefProjectRetainer, ref, desc, []treasury.InvoiceLine{line})
		in.Metadata["retainer_month"] = month
		inv, err := s.treasury.CreateInvoice(ctx, tenantID, userID, in)
		if err != nil {
			return err
		}
		out.Invoice = inv
		return s.recordProject(ctx, tenantID, projectID, "retainer_invoiced", map[string]any{
			"invoice_number": inv.InvoiceNumber, "month": month,
		})
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// recordProject writes a project activity entry for an invoice.
func (s *Service) recordProject(ctx context.Context, tenantID, projectID uuid.UUID, action string, changes map[string]any) error {
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		return activity.Record(ctx, tx, activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(projectID), EntityType: "project", EntityID: projectID,
			Action: action, Changes: changes,
		})
	})
	if err != nil {
		// The invoice exists in treasury either way; a missing feed row is not worth failing it.
		s.log.Warn("billing activity not recorded", zap.Error(err))
	}
	return nil
}
