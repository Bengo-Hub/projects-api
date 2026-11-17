package financials

import "testing"

func TestCommercialFrom(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	eq := func(a, b *float64) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

	// Fixed price: margin at completion against EAC, unbilled never negative.
	c := commercialFrom(map[string]any{"billing_type": "fixed", "contract_value": 100000.0, "crm_contact_name": "Acme"}, 30000, 40000, 80000, 0)
	if c.MarginBasis != "at_completion" || !eq(c.ProjectedMargin, f(20000)) || !eq(c.ProjectedMarginPct, f(20)) || !eq(c.Unbilled, f(70000)) || c.ClientName != "Acme" {
		t.Errorf("fixed = %+v", c)
	}
	if !eq(c.MarginToDate, f(-10000)) || c.CostToDate != 40000 {
		t.Errorf("fixed, to date = %+v", c)
	}
	over := commercialFrom(map[string]any{"billing_type": "fixed", "contract_value": "50000"}, 60000, 55000, 70000, 0)
	if !eq(over.Unbilled, f(0)) || !eq(over.ProjectedMargin, f(-20000)) {
		t.Errorf("fixed, over-invoiced and over cost = %+v", over)
	}
	// Labour to date is added to the treasury cost and to EAC.
	lab := commercialFrom(map[string]any{"billing_type": "fixed", "contract_value": 100000.0}, 30000, 40000, 80000, 5000)
	if !eq(lab.ProjectedMargin, f(15000)) || lab.LabourCost != 5000 || lab.CostToDate != 45000 || !eq(lab.MarginToDate, f(-15000)) {
		t.Errorf("fixed with labour = %+v", lab)
	}
	// Fixed without a contract value: nothing to price at completion.
	if none := commercialFrom(map[string]any{"billing_type": "fixed"}, 0, 0, 0, 0); none.ProjectedMargin != nil || none.Unbilled != nil {
		t.Errorf("fixed without value = %+v", none)
	}
	// Time and materials: margin to date from invoicing against cost, labour included.
	tm := commercialFrom(map[string]any{"billing_type": "time_and_materials"}, 50000, 35000, 90000, 0)
	if tm.MarginBasis != "to_date" || !eq(tm.ProjectedMargin, f(15000)) || !eq(tm.ProjectedMarginPct, f(30)) || tm.Unbilled != nil {
		t.Errorf("t&m = %+v", tm)
	}
	tml := commercialFrom(map[string]any{"billing_type": "time_and_materials"}, 50000, 35000, 90000, 10000)
	if !eq(tml.ProjectedMargin, f(5000)) || !eq(tml.ProjectedMarginPct, f(10)) {
		t.Errorf("t&m with labour = %+v", tml)
	}
	// Retainer: margin to date like time and materials.
	ret := commercialFrom(map[string]any{"billing_type": "retainer"}, 120000, 20000, 0, 40000)
	if ret.MarginBasis != "to_date" || !eq(ret.ProjectedMargin, f(60000)) || !eq(ret.ProjectedMarginPct, f(50)) {
		t.Errorf("retainer = %+v", ret)
	}
	// Internal work: no margin.
	if nb := commercialFrom(map[string]any{"billing_type": "non_billable"}, 0, 1000, 1000, 500); nb.ProjectedMargin != nil || nb.MarginToDate != nil || nb.CostToDate != 1500 {
		t.Errorf("non-billable = %+v", nb)
	}
	if empty := commercialFrom(nil, 0, 0, 0, 0); empty.BillingType != "" || empty.ProjectedMargin != nil {
		t.Errorf("no metadata = %+v", empty)
	}
}
