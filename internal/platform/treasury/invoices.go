package treasury

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Reference types projects-api puts on the invoices it asks treasury to create. Treasury's create
// does NOT dedupe on the reference, so the billing service reads by reference first under a
// per-reference lock; the reference is what makes each billing action idempotent.
const (
	RefProjectMilestone = "project_milestone"
	RefProjectTime      = "project_time"
	RefProjectRetainer  = "project_retainer"
)

// InvoiceLine is one line of a draft invoice. Quantity and UnitPrice are decimal strings so no
// float rounding reaches treasury.
type InvoiceLine struct {
	Description string `json:"description"`
	Unit        string `json:"unit,omitempty"`
	Quantity    string `json:"quantity"`
	UnitPrice   string `json:"unit_price"`
	ItemType    string `json:"item_type,omitempty"`
	// TaxCode and TaxRate (percent on a tax-exclusive price) come from the project's tax_code.
	// Treasury fills no tax on lines without an item, so without them the line carries no VAT.
	TaxCode string `json:"tax_code,omitempty"`
	TaxRate string `json:"tax_rate,omitempty"`
}

// CreateInvoiceInput is the customer invoice projects-api asks treasury to create. The customer is
// the project's CRM contact (treasury keeps crm_customer_id only when the CRM confirms it is the
// tenant's, so the name goes too); metadata.project_id puts the revenue on the project when the
// invoice is sent. invoice_type is left out: treasury's default is its standard invoice. A service
// caller's invoice comes back approved (system origin), not sent and not yet on eTIMS.
type CreateInvoiceInput struct {
	CRMCustomerID string         `json:"crm_customer_id"`
	CustomerName  string         `json:"customer_name,omitempty"`
	InvoiceDate   time.Time      `json:"invoice_date"`
	DueDate       time.Time      `json:"due_date"`
	Currency      string         `json:"currency,omitempty"`
	Notes         string         `json:"notes,omitempty"`
	ReferenceID   uuid.UUID      `json:"reference_id"`
	ReferenceType string         `json:"reference_type"`
	Lines         []InvoiceLine  `json:"lines"`
	Metadata      map[string]any `json:"metadata"`
}

// Invoice is the part of a treasury invoice projects-api reads back after a create or a
// by-reference read.
type Invoice struct {
	ID            uuid.UUID  `json:"id"`
	InvoiceNumber string     `json:"invoice_number"`
	Status        string     `json:"status"`
	PaymentStatus string     `json:"payment_status"`
	Currency      string     `json:"currency"`
	TotalAmount   Money      `json:"total_amount"`
	AmountPaid    Money      `json:"amount_paid"`
	// AmountDue is what is still owed. Treasury's full invoice calls it amount_due and its summary
	// balance_due (net of credit notes); normalize fills both from whichever came back.
	AmountDue  Money `json:"amount_due"`
	BalanceDue Money `json:"balance_due"`
	InvoiceDate   time.Time  `json:"invoice_date"`
	DueDate       time.Time  `json:"due_date"`
	ReferenceType string     `json:"reference_type,omitempty"`
	ReferenceID   *uuid.UUID `json:"reference_id,omitempty"`
	IsOverdue     bool       `json:"is_overdue"`
	PublicURL     string     `json:"public_url,omitempty"`
}

// normalize makes amount_due and balance_due agree, whichever form treasury answered with.
func (inv *Invoice) normalize() {
	if inv.BalanceDue == 0 && inv.AmountDue != 0 {
		inv.BalanceDue = inv.AmountDue
	}
	if inv.AmountDue == 0 && inv.BalanceDue != 0 {
		inv.AmountDue = inv.BalanceDue
	}
}

// CreateInvoice creates an invoice acting as userID. It always creates: callers check the
// reference first (InvoiceByReference) under a lock.
func (c *Client) CreateInvoice(ctx context.Context, tenantID, userID uuid.UUID, in CreateInvoiceInput) (*Invoice, error) {
	var inv Invoice
	if err := c.do(ctx, http.MethodPost, s2s(tenantID, "/invoices"), &userID, in, &inv); err != nil {
		return nil, err
	}
	inv.normalize()
	return &inv, nil
}

// InvoiceByReference returns the invoice created for a reference, or nil when there is none.
func (c *Client) InvoiceByReference(ctx context.Context, tenantID uuid.UUID, refType string, refID uuid.UUID) (*Invoice, error) {
	q := url.Values{"reference_type": {refType}, "reference_id": {refID.String()}}
	var inv Invoice
	err := c.do(ctx, http.MethodGet, s2s(tenantID, "/invoices/by-reference?"+q.Encode()), nil, nil, &inv)
	if e, ok := err.(*Error); ok && e.Status == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	inv.normalize()
	return &inv, nil
}

// maxReferenceBatch is treasury's limit for one by-references read.
const maxReferenceBatch = 200

// InvoicesByReferences returns the invoices of many references of one type, keyed by reference
// id, in batches of 200 (one query each on treasury's side).
func (c *Client) InvoicesByReferences(ctx context.Context, tenantID uuid.UUID, refType string, refIDs []uuid.UUID) (map[uuid.UUID]Invoice, error) {
	out := map[uuid.UUID]Invoice{}
	for start := 0; start < len(refIDs); start += maxReferenceBatch {
		end := min(start+maxReferenceBatch, len(refIDs))
		ids := make([]string, 0, end-start)
		for _, id := range refIDs[start:end] {
			ids = append(ids, id.String())
		}
		q := url.Values{"reference_type": {refType}, "reference_ids": {strings.Join(ids, ",")}}
		var resp struct {
			Data []Invoice `json:"data"`
		}
		if err := c.do(ctx, http.MethodGet, s2s(tenantID, "/invoices/by-references?"+q.Encode()), nil, nil, &resp); err != nil {
			return nil, err
		}
		// Treasury answers every document of a reference, newest first: keep the newest.
		for _, inv := range resp.Data {
			if inv.ReferenceID == nil {
				continue
			}
			if _, seen := out[*inv.ReferenceID]; !seen {
				inv.normalize()
				out[*inv.ReferenceID] = inv
			}
		}
	}
	return out, nil
}

// InvoicesByProject lists a project's invoices, newest first. Drafts are left out unless
// includeDrafts (the team sees them, a client never does).
func (c *Client) InvoicesByProject(ctx context.Context, tenantID, projectID uuid.UUID, includeDrafts bool, limit, offset int) ([]Invoice, int, error) {
	q := url.Values{
		"project_id":     {projectID.String()},
		"include_drafts": {strconv.FormatBool(includeDrafts)},
		"limit":          {strconv.Itoa(limit)},
		"offset":         {strconv.Itoa(offset)},
	}
	var resp struct {
		Data  []Invoice `json:"data"`
		Total int       `json:"total"`
	}
	if err := c.do(ctx, http.MethodGet, s2s(tenantID, "/invoices/by-project?"+q.Encode()), nil, nil, &resp); err != nil {
		return nil, 0, err
	}
	if resp.Data == nil {
		resp.Data = []Invoice{}
	}
	for i := range resp.Data {
		resp.Data[i].normalize()
	}
	return resp.Data, resp.Total, nil
}
