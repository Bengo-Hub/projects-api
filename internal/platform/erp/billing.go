package erp

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// Time billing (sprint 15). ERP owns the timesheet entries; projects-api bills them in three
// steps keyed by one billing reference (the treasury invoice's reference id):
//
//  1. reserve tags the period's approved, billable, unbilled entries with the reference and
//     returns every entry carrying it, so a retry after a failed invoice sees the same set;
//  2. treasury creates the invoice for that reference (idempotent on it);
//  3. confirm writes the invoice id on the entries (overwriting, so a deleted draft's hours move
//     to the invoice that replaces it). release untags entries never confirmed, or with
//     include_confirmed every entry of a voided invoice.
//
// A tagged entry is never reserved again, so no hour is billed twice.

// BillableEntry is one approved, billable entry reserved for an invoice. Hours are regular plus
// overtime, as a decimal string on the wire.
type BillableEntry struct {
	ID           uuid.UUID  `json:"id"`
	Date         string     `json:"date"`
	TaskID       *uuid.UUID `json:"task_id,omitempty"`
	EmployeeID   uuid.UUID  `json:"employee_id"`
	EmployeeName string     `json:"employee_name"`
	Hours        float64    `json:"hours,string"`
	Description  string     `json:"description,omitempty"`
}

// Reservation is ERP's answer to a reserve: the entries carrying the reference and, when they
// were already confirmed, the invoice they are on.
type Reservation struct {
	Entries         []BillableEntry `json:"data"`
	BilledInvoiceID *uuid.UUID      `json:"billed_invoice_id,omitempty"`
}

func (c *Client) post(ctx context.Context, op, path string, tenantID uuid.UUID, body, out any) error {
	if !c.Enabled() {
		return ErrNotConfigured
	}
	resp, err := c.sc.Post(ctx, path, body, c.headers(tenantID))
	if err != nil {
		return fmt.Errorf("erp: %s: %w", op, err)
	}
	if resp.StatusCode != http.StatusOK {
		return failure(op, resp.StatusCode, resp.Body)
	}
	if out == nil {
		return nil
	}
	if err := resp.DecodeJSON(out); err != nil {
		return fmt.Errorf("erp: %s: %w", op, err)
	}
	return nil
}

// ReserveBilling tags the project's approved billable entries dated in [from, to] with ref and
// returns every entry tagged with it.
func (c *Client) ReserveBilling(ctx context.Context, tenantID, projectID, ref uuid.UUID, from, to time.Time) (*Reservation, error) {
	var out Reservation
	body := map[string]any{
		"project_id": projectID.String(), "billing_ref": ref.String(),
		"from": from.Format(dateLayout), "to": to.Format(dateLayout),
	}
	if err := c.post(ctx, "reserve billing", "/api/v1/hrm/attendance/timesheets/billing/reserve", tenantID, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ConfirmBilling records the invoice on the entries tagged with ref. Idempotent.
func (c *Client) ConfirmBilling(ctx context.Context, tenantID, ref, invoiceID uuid.UUID) error {
	body := map[string]any{"billing_ref": ref.String(), "invoice_id": invoiceID.String()}
	return c.post(ctx, "confirm billing", "/api/v1/hrm/attendance/timesheets/billing/confirm", tenantID, body, nil)
}

// ReleaseBilling untags the entries tagged with ref that were never confirmed. includeConfirmed
// also frees confirmed ones; the caller sends it only for an invoice treasury shows as void or
// cancelled, so those hours can be billed again.
func (c *Client) ReleaseBilling(ctx context.Context, tenantID, ref uuid.UUID, includeConfirmed bool) (int, error) {
	var out struct {
		Released int `json:"released"`
	}
	body := map[string]any{"billing_ref": ref.String(), "include_confirmed": includeConfirmed}
	if err := c.post(ctx, "release billing", "/api/v1/hrm/attendance/timesheets/billing/release", tenantID, body, &out); err != nil {
		return 0, err
	}
	return out.Released, nil
}
