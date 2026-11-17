package billing

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	entmilestone "github.com/bengobox/projects-service/internal/ent/milestone"
	"github.com/bengobox/projects-service/internal/platform/treasury"
)

// maxOverviewMilestones and overviewInvoices bound one overview: milestones are few per project,
// and the invoice list shows the latest page (the full history is in treasury).
const (
	maxOverviewMilestones = 500
	overviewInvoices      = 50
)

// Overview is a project's billing page in one read: its terms, its milestones with their price
// and invoice state, its latest invoices, and the time waiting to be billed. Treasury or ERP
// being down leaves their parts empty with a flag, never fails the page.
type Overview struct {
	Terms       Terms              `json:"terms"`
	Milestones  []MilestoneBilling `json:"milestones"`
	Invoices    []treasury.Invoice `json:"invoices"`
	InvoicesTotal int              `json:"invoices_total"`
	// UnbilledHours is approved billable time not yet invoiced; UnbilledEstimate prices it at
	// the hourly rate. Both nil without ERP.
	UnbilledHours       *float64 `json:"unbilled_hours,omitempty"`
	UnbilledEstimate    string   `json:"unbilled_estimate,omitempty"`
	TreasuryUnavailable bool     `json:"treasury_unavailable"`
	ERPUnavailable      bool     `json:"erp_unavailable"`
}

// milestoneRef is the reference a milestone's invoice was created under: the stored one, or the
// milestone id for the first invoice.
func milestoneRef(m *ent.Milestone) uuid.UUID {
	if s, _ := metaOf(m)[MetaInvoiceRef].(string); s != "" {
		if id, err := uuid.Parse(s); err == nil {
			return id
		}
	}
	return m.ID
}

// Overview reads the project's billing page.
func (s *Service) Overview(ctx context.Context, tenantID, projectID uuid.UUID) (*Overview, error) {
	p, err := s.project(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	t := termsOf(p)
	ms, err := s.client.Milestone.Query().
		Where(entmilestone.TenantID(tenantID), entmilestone.ProjectID(projectID)).
		Order(ent.Asc(entmilestone.FieldTargetDate)).Limit(maxOverviewMilestones).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list milestones: %w", err)
	}
	out := &Overview{Terms: t, Milestones: make([]MilestoneBilling, len(ms)), Invoices: []treasury.Invoice{}}
	refs := make([]uuid.UUID, 0, len(ms))
	refOf := make(map[uuid.UUID]int, len(ms))
	for i, m := range ms {
		out.Milestones[i] = milestoneView(m, t)
		if out.Milestones[i].InvoiceID != "" {
			ref := milestoneRef(m)
			refs = append(refs, ref)
			refOf[ref] = i
		}
	}
	if s.treasury.Enabled() {
		if len(refs) > 0 {
			byRef, err := s.treasury.InvoicesByReferences(ctx, tenantID, treasury.RefProjectMilestone, refs)
			if err != nil {
				s.log.Warn("milestone invoices unavailable", zap.Error(err))
				out.TreasuryUnavailable = true
			}
			for ref, inv := range byRef {
				if i, ok := refOf[ref]; ok {
					inv := inv
					out.Milestones[i].Invoice = &inv
				}
			}
		}
		invs, total, err := s.treasury.InvoicesByProject(ctx, tenantID, projectID, true, overviewInvoices, 0)
		if err != nil {
			s.log.Warn("project invoices unavailable", zap.Error(err))
			out.TreasuryUnavailable = true
		} else {
			out.Invoices, out.InvoicesTotal = invs, total
		}
	} else {
		out.TreasuryUnavailable = true
	}
	if s.erp.Enabled() {
		hours, err := s.erp.ProjectHours(ctx, tenantID, []uuid.UUID{projectID})
		if err != nil {
			s.log.Warn("unbilled hours unavailable", zap.Error(err))
			out.ERPUnavailable = true
		} else {
			h := hours[projectID].UnbilledBillableHours
			out.UnbilledHours = &h
			if rate, ok := parseRate(t.HourlyRate); ok && h > 0 {
				out.UnbilledEstimate = money2(h * rate)
			}
		}
	} else {
		out.ERPUnavailable = true
	}
	return out, nil
}

// ClientInvoices lists the invoices a project's client may see: everything but drafts, voids and
// cancellations, newest first.
func (s *Service) ClientInvoices(ctx context.Context, tenantID, projectID uuid.UUID, limit, offset int) ([]treasury.Invoice, int, error) {
	if err := s.treasuryReady(); err != nil {
		return nil, 0, err
	}
	return s.treasury.InvoicesByProject(ctx, tenantID, projectID, false, limit, offset)
}
