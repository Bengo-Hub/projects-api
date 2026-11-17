package billing

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/platform/erp"
	"github.com/bengobox/projects-service/internal/platform/treasury"
)

func TestBillAmount(t *testing.T) {
	cv := 120000.0
	terms := Terms{ContractValue: &cv}
	cases := []struct {
		meta map[string]any
		t    Terms
		want string
	}{
		{map[string]any{MetaAmount: "5000.50"}, terms, "5000.50"},
		{map[string]any{MetaAmountPct: 25.0}, terms, "30000.00"},
		{map[string]any{MetaAmountPct: 33.333}, terms, "39999.60"},
		{map[string]any{MetaAmountPct: 25.0}, Terms{}, ""}, // a share needs a contract value
		{map[string]any{}, terms, ""},
		{map[string]any{MetaAmount: "10", MetaAmountPct: 50.0}, terms, "10"}, // amount wins
	}
	for _, c := range cases {
		if got := billAmount(c.meta, c.t); got != c.want {
			t.Errorf("billAmount(%v) = %q, want %q", c.meta, got, c.want)
		}
	}
}

func TestParsePeriod(t *testing.T) {
	if _, _, err := parsePeriod("2026-01-01", "2026-12-31"); err != nil {
		t.Fatalf("a calendar year = %v", err)
	}
	for _, p := range [][2]string{{"2026-02-01", "2026-01-31"}, {"2025-01-01", "2026-12-31"}, {"", "2026-01-01"}} {
		if _, _, err := parsePeriod(p[0], p[1]); err == nil {
			t.Errorf("%v accepted", p)
		}
	}
}

// Late evening in Nairobi is already the next day there; invoices carry the EAT date.
func TestEATDate(t *testing.T) {
	got := eatDate(time.Date(2026, 11, 17, 21, 30, 0, 0, time.UTC))
	if got != time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("eatDate = %v", got)
	}
}

// References are the same on every pod and retry, differ per period and kind, and chain past a
// dead invoice.
func TestReferences(t *testing.T) {
	tenant, project := uuid.New(), uuid.New()
	a := periodRef(tenant, project, treasury.RefProjectTime, "2026-11-01..2026-11-30")
	if a != periodRef(tenant, project, treasury.RefProjectTime, "2026-11-01..2026-11-30") {
		t.Fatal("period reference is not stable")
	}
	if a == periodRef(tenant, project, treasury.RefProjectRetainer, "2026-11-01..2026-11-30") ||
		a == periodRef(tenant, project, treasury.RefProjectTime, "2026-11-01..2026-11-29") {
		t.Fatal("different kinds or periods share a reference")
	}
	dead := uuid.New()
	if nextRef(a, dead) != nextRef(a, dead) || nextRef(a, dead) == a || nextRef(a, dead) == nextRef(a, uuid.New()) {
		t.Fatal("chained reference is not stable and distinct")
	}
	if !IsDead(&treasury.Invoice{Status: "void"}) || !IsDead(&treasury.Invoice{Status: "cancelled"}) || IsDead(&treasury.Invoice{Status: "sent"}) || IsDead(nil) {
		t.Fatal("IsDead")
	}
}

func TestTimeLinesGroupByPerson(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	entries := []erp.BillableEntry{
		{EmployeeID: b, EmployeeName: "Zawadi", Hours: 1.333},
		{EmployeeID: a, EmployeeName: "Achieng", Hours: 2},
		{EmployeeID: b, EmployeeName: "Zawadi", Hours: 1.333},
		{EmployeeID: uuid.New(), Hours: 0.5},
	}
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	lines, total := timeLines(entries, "1500", from, from.AddDate(0, 0, 29))
	if len(lines) != 3 || total != 5.17 {
		t.Fatalf("lines = %+v total %v", lines, total)
	}
	if lines[0].Description != "Team member, 1 Nov 2026 to 30 Nov 2026" || lines[1].Description[:7] != "Achieng" || lines[2].Quantity != "2.67" || lines[2].UnitPrice != "1500" {
		t.Fatalf("lines = %+v", lines)
	}
}
