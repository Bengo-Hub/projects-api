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
