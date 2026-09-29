package financials

import "testing"

func TestCommercialFrom(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	eq := func(a, b *float64) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

	// Fixed price: margin at completion against EAC, unbilled never negative.
	c := commercialFrom(map[string]any{"billing_type": "fixed", "contract_value": 100000.0, "crm_contact_name": "Acme"}, 30000, 40000, 80000)
	if c.MarginBasis != "at_completion" || !eq(c.ProjectedMargin, f(20000)) || !eq(c.ProjectedMarginPct, f(20)) || !eq(c.Unbilled, f(70000)) || c.ClientName != "Acme" {
		t.Errorf("fixed = %+v", c)
	}
	over := commercialFrom(map[string]any{"billing_type": "fixed", "contract_value": "50000"}, 60000, 55000, 70000)
	if !eq(over.Unbilled, f(0)) || !eq(over.ProjectedMargin, f(-20000)) {
		t.Errorf("fixed, over-invoiced and over cost = %+v", over)
	}
	// Fixed without a contract value: nothing to price.
	if none := commercialFrom(map[string]any{"billing_type": "fixed"}, 0, 0, 0); none.ProjectedMargin != nil || none.Unbilled != nil {
		t.Errorf("fixed without value = %+v", none)
	}
	// Time and materials: margin to date from invoicing against cost.
	tm := commercialFrom(map[string]any{"billing_type": "time_and_materials"}, 50000, 35000, 90000)
	if tm.MarginBasis != "to_date" || !eq(tm.ProjectedMargin, f(15000)) || !eq(tm.ProjectedMarginPct, f(30)) || tm.Unbilled != nil {
		t.Errorf("t&m = %+v", tm)
	}
	// Internal work: no margin.
	if nb := commercialFrom(map[string]any{"billing_type": "non_billable"}, 0, 1000, 1000); nb.ProjectedMargin != nil {
		t.Errorf("non-billable = %+v", nb)
	}
	if empty := commercialFrom(nil, 0, 0, 0); empty.BillingType != "" || empty.ProjectedMargin != nil {
		t.Errorf("no metadata = %+v", empty)
	}
}
