package planning

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/planmodel"
)

func d(s string) time.Time {
	t, _ := time.Parse(time.DateOnly, s)
	return t
}

func TestWeekRange(t *testing.T) {
	start, end, weeks, err := WeekRange(d("2026-11-19"), 0)
	if err != nil || start.Format(time.DateOnly) != "2026-11-16" || weeks != 8 || end.Format(time.DateOnly) != "2027-01-10" {
		t.Fatalf("default: %s %s %d %v", start, end, weeks, err)
	}
	if MondayOf(d("2026-11-22")).Format(time.DateOnly) != "2026-11-16" {
		t.Fatal("Sunday belongs to the week that started on Monday")
	}
	for _, w := range []int{-1, MaxWeeks + 1} {
		if _, _, _, err := WeekRange(d("2026-11-19"), w); err == nil {
			t.Errorf("weeks %d accepted", w)
		}
	}
}

func TestBuildWorkload(t *testing.T) {
	ana, ben, cid := uuid.New(), uuid.New(), uuid.New()
	start := d("2026-11-16")
	rows := []DemandRow{
		{UserID: &ana, Week: d("2026-11-16"), Hours: 9, Tasks: 2},
		{UserID: &ana, Week: d("2026-11-23"), Hours: 4, Tasks: 1},
		{UserID: &ben, Week: d("2026-11-16"), Hours: 5, Tasks: 1},
		{UserID: &cid, Week: d("2026-11-16"), Hours: 1, Tasks: 1},
		{UserID: nil, Week: d("2026-11-23"), Hours: 6, Tasks: 1},
		{UserID: &ana, Week: d("2027-03-01"), Hours: 99, Tasks: 1}, // outside the range
	}
	settings := WorkloadSettings{WeeklyHours: 40, PersonHours: map[uuid.UUID]float64{ana: 8, ben: 0}, CapacitySource: "settings"}
	w := BuildWorkload(rows, []uuid.UUID{ben, ana}, start, 2, settings, true)
	if len(w.Weeks) != 2 || len(w.People) != 2 || w.People[0].UserID != ben {
		t.Fatalf("team order and filter: %+v", w.People)
	}
	a := w.People[1]
	if a.Weeks[0].Demand != 9 || a.Weeks[0].LoadPct != 112.5 || !a.Weeks[0].Overloaded || a.Weeks[1].Overloaded || a.OverloadedWeeks != 1 || a.TotalDemand != 13 {
		t.Fatalf("ana: %+v", a)
	}
	if b := w.People[0]; !b.Weeks[0].Overloaded || b.Weeks[1].Overloaded {
		t.Fatalf("zero capacity with demand is overloaded, without is not: %+v", b)
	}
	if len(w.Unassigned) != 1 || w.Unassigned[0].WeekStart != "2026-11-23" || w.Unassigned[0].Demand != 6 {
		t.Fatalf("unassigned: %+v", w.Unassigned)
	}
	all := BuildWorkload(rows, nil, start, 2, settings, false)
	if len(all.People) != 3 {
		t.Fatalf("everyone with demand: %d", len(all.People))
	}
}

func TestBuildHeatMap(t *testing.T) {
	h := BuildHeatMap([]HeatCell{{Likelihood: 5, Impact: 5, Count: 2}, {Likelihood: 1, Impact: 3, Count: 1}, {Likelihood: 6, Impact: 1, Count: 9}, {Likelihood: 2, Impact: 2, Count: 0}})
	if h.Grid[4][4] != 2 || h.Grid[0][2] != 1 || h.Total != 3 || len(h.Cells) != 2 {
		t.Fatalf("heat map: %+v", h)
	}
	if Score(4, 5) != 20 {
		t.Fatal("score")
	}
}

func TestCompareSlips(t *testing.T) {
	a, b, gone, m := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	at := func(s string) *time.Time { v := d(s); return &v }
	snap := planmodel.Snapshot{
		Tasks: []planmodel.BaselineTask{
			{ID: a, Title: "A", StartDate: at("2026-11-02"), DueDate: at("2026-11-10"), EstimatedHours: hrs(10)},
			{ID: b, Title: "B", DueDate: at("2026-11-20")},
			{ID: gone, Title: "Gone", DueDate: at("2026-11-20")},
		},
		Milestones: []planmodel.BaselineMilestone{{ID: m, Name: "M", TargetDate: at("2026-11-30")}},
	}
	cur := []CurrentTask{
		{ID: a, Title: "A", Start: at("2026-11-04"), Due: at("2026-11-15"), Estimate: hrs(12)},
		{ID: b, Title: "B", Due: at("2026-11-18")},
	}
	slips, rep := CompareSlips(snap, cur, map[uuid.UUID]*time.Time{m: at("2026-12-07")}, 4)
	if len(slips) != 3 || slips[0].Kind != "milestone" || slips[0].FinishSlipDays != 7 || slips[1].ID != a || slips[2].FinishSlipDays != -2 {
		t.Fatalf("order: %+v", slips)
	}
	if slips[1].StartSlipDays != 2 || slips[1].FinishSlipDays != 5 || slips[1].EstimateChange != 2 {
		t.Fatalf("task A: %+v", slips[1])
	}
	if rep.Compared != 2 || rep.Slipped != 1 || rep.PulledIn != 1 || rep.Removed != 1 || rep.Added != 4 || rep.MaxSlipDays != 5 || rep.MilestonesLate != 1 {
		t.Fatalf("report: %+v", rep)
	}
}

func hrs(v float64) *float64 { return &v }

func TestValidateDefinition(t *testing.T) {
	for _, st := range systemTemplates {
		def := st.def
		if err := ValidateDefinition(&def); err != nil {
			t.Fatalf("system template %s: %v", st.key, err)
		}
		if def.DurationDays < 30 {
			t.Errorf("%s duration %d", st.key, def.DurationDays)
		}
	}
	bad := []planmodel.TemplateDefinition{
		{},
		{Tasks: []planmodel.TemplateTask{{Ref: "a", Title: "A", DurationDays: 0}}},
		{Tasks: []planmodel.TemplateTask{{Ref: "a", Title: "A", DurationDays: 1, DependsOn: []string{"a"}}}},
		{Tasks: []planmodel.TemplateTask{{Ref: "a", Title: "A", DurationDays: 1}, {Ref: "a", Title: "B", DurationDays: 1}}},
		{Tasks: []planmodel.TemplateTask{{Ref: "a", Title: "A", DurationDays: 1, PhaseRef: "x"}}},
	}
	for i, def := range bad {
		if err := ValidateDefinition(&def); err == nil {
			t.Errorf("bad definition %d accepted", i)
		}
	}
	ok := planmodel.TemplateDefinition{Tasks: []planmodel.TemplateTask{{Ref: "a", Title: "A", OffsetDays: 5, DurationDays: 3}}}
	if err := ValidateDefinition(&ok); err != nil || ok.DurationDays != 8 {
		t.Fatalf("duration: %d %v", ok.DurationDays, err)
	}
}
