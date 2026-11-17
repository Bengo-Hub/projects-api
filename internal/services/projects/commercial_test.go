package projects

import "testing"

// Ids are stored in the canonical lower-case hyphenated form, whatever form was sent, so the
// crm-links check (which matches the canonical form) never misses a linked contact.
func TestNormalizeCommercialStoresCanonicalIDs(t *testing.T) {
	const canonical = "2b1f6c1e-8a3d-4f2b-9c4e-0d5a7e9b1c2f"
	for _, sent := range []string{
		canonical,
		"2B1F6C1E-8A3D-4F2B-9C4E-0D5A7E9B1C2F",
		"2b1f6c1e8a3d4f2b9c4e0d5a7e9b1c2f",
		"{2b1f6c1e-8a3d-4f2b-9c4e-0d5a7e9b1c2f}",
		"urn:uuid:2b1f6c1e-8a3d-4f2b-9c4e-0d5a7e9b1c2f",
	} {
		meta := map[string]any{MetaCRMContactID: sent, MetaCostCenterID: sent}
		if err := normalizeCommercial(meta); err != nil {
			t.Fatalf("%q: %v", sent, err)
		}
		if meta[MetaCRMContactID] != canonical || meta[MetaCostCenterID] != canonical {
			t.Errorf("%q stored as %v / %v, want %s", sent, meta[MetaCRMContactID], meta[MetaCostCenterID], canonical)
		}
	}
}

func TestNormalizeCommercialRejectsBadIDAndClearsEmpty(t *testing.T) {
	if err := normalizeCommercial(map[string]any{MetaCRMContactID: "not-a-uuid"}); err == nil {
		t.Fatal("a bad id must be refused")
	}
	meta := map[string]any{MetaCRMContactID: "", MetaCRMContactName: "Acme"}
	if err := normalizeCommercial(meta); err != nil || meta[MetaCRMContactID] != nil || meta[MetaCRMContactName] != nil {
		t.Fatalf("empty id: %v %v", meta, err)
	}
}

// hourly_rate is validated server side: a positive amount with at most two places, kept as a
// decimal string; empty clears it.
func TestNormalizeCommercialHourlyRate(t *testing.T) {
	for sent, want := range map[any]string{"2500": "2500", " 2500.50 ": "2500.50", 1800.5: "1800.5", 120: "120"} {
		meta := map[string]any{MetaHourlyRate: sent}
		if err := normalizeCommercial(meta); err != nil || meta[MetaHourlyRate] != want {
			t.Errorf("%v: stored %v (%v), want %q", sent, meta[MetaHourlyRate], err, want)
		}
	}
	for _, bad := range []any{"0", "0.00", "-5", "12.345", "abc", "1e3", true} {
		if err := normalizeCommercial(map[string]any{MetaHourlyRate: bad}); err == nil {
			t.Errorf("%v must be refused", bad)
		}
	}
	meta := map[string]any{MetaHourlyRate: ""}
	if err := normalizeCommercial(meta); err != nil || meta[MetaHourlyRate] != nil {
		t.Fatalf("empty rate: %v %v", meta, err)
	}
}

func TestBillsHours(t *testing.T) {
	if !BillsHours(map[string]any{MetaBillingType: "time_and_materials"}) {
		t.Error("time and materials bills hours")
	}
	for _, meta := range []map[string]any{nil, {}, {MetaBillingType: "fixed"}, {MetaBillingType: "non_billable"}} {
		if BillsHours(meta) {
			t.Errorf("%v must not bill hours", meta)
		}
	}
}

// Retainer terms and the VAT code are validated and stored in their canonical forms.
func TestNormalizeCommercialRetainerAndTax(t *testing.T) {
	meta := map[string]any{"billing_type": "retainer", "retainer_amount": 150000.0, "retainer_hours": "40", "retainer_rollover": true, "tax_code": " vat-16 "}
	if err := normalizeCommercial(meta); err != nil {
		t.Fatal(err)
	}
	if meta["retainer_amount"] != "150000" || meta["retainer_hours"] != 40.0 || meta["tax_code"] != "VAT-16" {
		t.Fatalf("normalised = %v", meta)
	}
	if BillsHours(meta) {
		t.Fatal("a retainer does not bill its hours by default")
	}
	bad := []map[string]any{
		{"billing_type": "hourly"},
		{"retainer_amount": "0"},
		{"retainer_amount": "12.345"},
		{"retainer_hours": -1},
		{"retainer_rollover": "yes"},
		{"tax_code": "VAT-20"},
	}
	for _, m := range bad {
		if err := normalizeCommercial(m); err == nil {
			t.Errorf("%v accepted", m)
		}
	}
	cleared := map[string]any{"tax_code": "", "retainer_amount": ""}
	if err := normalizeCommercial(cleared); err != nil || cleared["tax_code"] != nil || cleared["retainer_amount"] != nil {
		t.Fatalf("empty values = %v %v, want cleared", cleared, err)
	}
}
