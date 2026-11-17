package billing

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	entmilestone "github.com/bengobox/projects-service/internal/ent/milestone"
	"github.com/bengobox/projects-service/internal/platform/treasury"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/projects"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// Milestone metadata keys (sprint 15). Read, never filtered.
const (
	MetaAmount        = "amount"         // decimal string, the milestone's price
	MetaAmountPct     = "amount_pct"     // share of the contract value (0 < pct <= 100)
	MetaInvoiceID     = "invoice_id"     // treasury invoice, once created
	MetaInvoiceNumber = "invoice_number" // its number, so lists need no treasury call
	MetaInvoiceRef    = "invoice_ref"    // the treasury reference id it was created under
)

// AmountInput sets a milestone's price: an amount or a share of the contract value, not both.
// Both empty clears it.
type AmountInput struct {
	Amount    *string  `json:"amount"`
	AmountPct *float64 `json:"amount_pct"`
}

// MilestoneBilling is one milestone with its price and, once invoiced, its invoice from treasury.
type MilestoneBilling struct {
	ID            uuid.UUID         `json:"id"`
	Name          string            `json:"name"`
	Status        string            `json:"status"`
	Amount        string            `json:"amount,omitempty"`
	AmountPct     *float64          `json:"amount_pct,omitempty"`
	BillAmount    string            `json:"bill_amount,omitempty"` // what an invoice would charge now
	InvoiceID     string            `json:"invoice_id,omitempty"`
	InvoiceNumber string            `json:"invoice_number,omitempty"`
	Invoice       *treasury.Invoice `json:"invoice,omitempty"`
}

func metaOf(m *ent.Milestone) map[string]any {
	if m.Metadata == nil {
		return map[string]any{}
	}
	return m.Metadata
}

// billAmount is the milestone's price now: its amount, or its share of the contract value.
// Empty when neither is set or the share has no contract value to apply to.
func billAmount(meta map[string]any, t Terms) string {
	if s, _ := meta[MetaAmount].(string); s != "" {
		return s
	}
	pct, ok := meta[MetaAmountPct].(float64)
	if !ok || t.ContractValue == nil {
		return ""
	}
	v := math.Round(*t.ContractValue*pct) / 100
	if v <= 0 {
		return ""
	}
	return money2(v)
}

func milestoneView(m *ent.Milestone, t Terms) MilestoneBilling {
	meta := metaOf(m)
	v := MilestoneBilling{ID: m.ID, Name: m.Name, Status: m.Status, BillAmount: billAmount(meta, t)}
	v.Amount, _ = meta[MetaAmount].(string)
	if pct, ok := meta[MetaAmountPct].(float64); ok {
		v.AmountPct = &pct
	}
	v.InvoiceID, _ = meta[MetaInvoiceID].(string)
	v.InvoiceNumber, _ = meta[MetaInvoiceNumber].(string)
	return v
}

func (s *Service) milestone(ctx context.Context, client *ent.Client, tenantID, projectID, id uuid.UUID) (*ent.Milestone, error) {
	m, err := client.Milestone.Query().
		Where(entmilestone.ID(id), entmilestone.TenantID(tenantID), entmilestone.ProjectID(projectID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load milestone: %w", err)
	}
	return m, nil
}

// SetMilestoneAmount prices a milestone. An invoiced milestone keeps its price: change the
// invoice in treasury instead.
func (s *Service) SetMilestoneAmount(ctx context.Context, tenantID, projectID, id uuid.UUID, in AmountInput) (*MilestoneBilling, error) {
	amount := ""
	if in.Amount != nil {
		amount = strings.TrimSpace(*in.Amount)
		if amount != "" && !projects.ValidPrice(amount) {
			return nil, ValidationError("amount must be above zero with at most two decimal places")
		}
	}
	if in.AmountPct != nil && (*in.AmountPct <= 0 || *in.AmountPct > 100) {
		return nil, ValidationError("amount_pct must be above 0 and at most 100")
	}
	if amount != "" && in.AmountPct != nil {
		return nil, ValidationError("give an amount or a share of the contract value, not both")
	}
	p, err := s.project(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	var out MilestoneBilling
	// The milestone's billing lock (the one InvoiceMilestone holds) serialises its metadata writes,
	// so a price change cannot interleave with an invoice being created and linked.
	err = s.locked(ctx, tenantID, id, func() error {
		m, err := s.milestone(ctx, s.client, tenantID, projectID, id)
		if err != nil {
			return err
		}
		// An invoiced milestone keeps its price while the invoice is live. Once it is voided,
		// cancelled or deleted in treasury the price can change and the link is cleared.
		if invoiced, _ := metaOf(m)[MetaInvoiceID].(string); invoiced != "" {
			if err := s.treasuryReady(); err != nil {
				return err
			}
			_, live, err := s.liveRef(ctx, tenantID, treasury.RefProjectMilestone, m.ID, nil)
			if err != nil {
				return err
			}
			if live != nil {
				return ValidationError("this milestone is already invoiced; change the invoice in treasury")
			}
		}
		return txn.Run(ctx, s.client, func(tx *ent.Tx) error {
			return s.setAmount(ctx, tx, p, tenantID, projectID, id, amount, in.AmountPct, &out)
		})
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *Service) setAmount(ctx context.Context, tx *ent.Tx, p *ent.Project, tenantID, projectID, id uuid.UUID, amount string, pct *float64, out *MilestoneBilling) error {
	m, err := s.milestone(ctx, tx.Client(), tenantID, projectID, id)
	if err != nil {
		return err
	}
	meta := metaOf(m)
	next := make(map[string]any, len(meta)+1)
	for k, v := range meta {
		next[k] = v
	}
	// The caller checked any invoice is dead; the new price will be billed on a new one.
	delete(next, MetaInvoiceID)
	delete(next, MetaInvoiceNumber)
	delete(next, MetaInvoiceRef)
	delete(next, MetaAmount)
	delete(next, MetaAmountPct)
	changes := map[string]any{}
	switch {
	case amount != "":
		next[MetaAmount], changes["amount"] = amount, amount
	case pct != nil:
		next[MetaAmountPct], changes["amount_pct"] = *pct, *pct
	default:
		changes["amount"] = nil
	}
	if m, err = tx.Milestone.UpdateOne(m).SetMetadata(next).Save(ctx); err != nil {
		return fmt.Errorf("price milestone: %w", err)
	}
	*out = milestoneView(m, termsOf(p))
	return activity.Record(ctx, tx, activity.Entry{
		TenantID: tenantID, ProjectID: activity.Ref(projectID), EntityType: "milestone", EntityID: m.ID,
		Action: activity.ActionUpdated, Changes: changes,
	})
}

// InvoiceMilestone creates the milestone's invoice in treasury, or returns the one already
// created for it, and keeps its id and number on the milestone.
func (s *Service) InvoiceMilestone(ctx context.Context, tenantID, projectID, id, userID uuid.UUID) (*treasury.Invoice, error) {
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
	m, err := s.milestone(ctx, s.client, tenantID, projectID, id)
	if err != nil {
		return nil, err
	}
	amount := billAmount(metaOf(m), t)
	if amount == "" {
		return nil, ValidationError("set this milestone's amount before invoicing it")
	}
	var inv *treasury.Invoice
	err = s.locked(ctx, tenantID, m.ID, func() error {
		ref, existing, err := s.liveRef(ctx, tenantID, treasury.RefProjectMilestone, m.ID, nil)
		if err != nil {
			return err
		}
		if existing != nil {
			// Created earlier (perhaps by a request whose link step failed): make sure it is linked.
			inv = existing
			return s.linkMilestone(ctx, tenantID, projectID, m.ID, ref, inv)
		}
		line := treasury.InvoiceLine{Description: p.Name + ": " + m.Name, Quantity: "1", UnitPrice: amount, ItemType: "service"}
		in := s.invoiceInput(p, t, treasury.RefProjectMilestone, ref, "Milestone: "+m.Name, []treasury.InvoiceLine{line})
		in.Metadata["milestone_id"] = m.ID.String()
		if inv, err = s.treasury.CreateInvoice(ctx, tenantID, userID, in); err != nil {
			return err
		}
		return s.linkMilestone(ctx, tenantID, projectID, m.ID, ref, inv)
	})
	if err != nil {
		return nil, err
	}
	return inv, nil
}

// linkMilestone writes the invoice onto the milestone (once) with an activity entry. Callers hold
// the milestone's billing lock.
func (s *Service) linkMilestone(ctx context.Context, tenantID, projectID, id, ref uuid.UUID, inv *treasury.Invoice) error {
	return txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		m, err := s.milestone(ctx, tx.Client(), tenantID, projectID, id)
		if err != nil {
			return err
		}
		meta := metaOf(m)
		if cur, _ := meta[MetaInvoiceID].(string); cur == inv.ID.String() {
			return nil
		}
		next := make(map[string]any, len(meta)+2)
		for k, v := range meta {
			next[k] = v
		}
		next[MetaInvoiceID], next[MetaInvoiceNumber], next[MetaInvoiceRef] = inv.ID.String(), inv.InvoiceNumber, ref.String()
		if _, err := tx.Milestone.UpdateOne(m).SetMetadata(next).Save(ctx); err != nil {
			return fmt.Errorf("link invoice: %w", err)
		}
		return activity.Record(ctx, tx, activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(projectID), EntityType: "milestone", EntityID: id,
			Action: "invoiced", Changes: map[string]any{"invoice_number": inv.InvoiceNumber, "amount": strconv.FormatFloat(float64(inv.TotalAmount), 'f', 2, 64)},
		})
	})
}
