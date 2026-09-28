package financials

import (
	"testing"
	"time"

	"github.com/bengobox/projects-service/internal/platform/erp"
)

func day(s string) *time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return &t
}

func hours(h float64) *float64 { return &h }

func TestComputeEVM_OnPlan(t *testing.T) {
	// Two equal tasks over Jan and Feb; at the end of January the first is done, the second not
	// started: exactly on plan. Half the budget spent.
	tasks := []TaskWork{
		{Start: day("2026-01-01"), Due: day("2026-02-01"), EstimatedHours: hours(10), Done: true},
		{Start: day("2026-02-01"), Due: day("2026-03-01"), EstimatedHours: hours(10)},
	}
	e := ComputeEVM(1000, 500, tasks, nil, nil, *day("2026-02-01"))
	if e.PV != 500 || e.EV != 500 || e.CPI != 1 || e.SPI != 1 || e.EAC != 1000 || e.Health != "green" {
		t.Fatalf("on plan: %+v", e)
	}
}

func TestComputeEVM_OverCostAndLate(t *testing.T) {
	tasks := []TaskWork{
		{Start: day("2026-01-01"), Due: day("2026-02-01"), EstimatedHours: hours(10), ProgressPct: 50},
		{Start: day("2026-02-01"), Due: day("2026-03-01"), EstimatedHours: hours(10)},
	}
	e := ComputeEVM(1000, 600, tasks, nil, nil, *day("2026-02-01"))
	// PV 500 (first task due), EV 250 (half of first task), AC 600.
	if e.PV != 500 || e.EV != 250 || e.CPI != 0.42 || e.SPI != 0.5 {
		t.Fatalf("figures: %+v", e)
	}
	if e.EAC != 2400 || e.EACAtBudgetRate != 1350 || e.VAC != -1400 || e.ETC != 1800 {
		t.Fatalf("forecasts: %+v", e)
	}
	if e.Health != "red" {
		t.Fatalf("health = %s, want red", e.Health)
	}
}

func TestComputeEVM_EstimatesWeightTasks(t *testing.T) {
	// A 30-hour task done and a 10-hour task not: 75% complete by effort, not 50%.
	tasks := []TaskWork{
		{EstimatedHours: hours(30), Done: true},
		{EstimatedHours: hours(10)},
		{}, // no estimate while others have one: excluded from weighting
	}
	e := ComputeEVM(1000, 0, tasks, nil, nil, time.Now())
	if e.PercentComplete != 75 || e.EV != 750 {
		t.Fatalf("weighted: %+v", e)
	}
	// No schedule: planned equals earned, so SPI is neutral.
	if e.SPI != 1 {
		t.Fatalf("SPI without dates = %v, want 1", e.SPI)
	}
}

func TestComputeEVM_EqualWeightsWithoutEstimates(t *testing.T) {
	tasks := []TaskWork{{Done: true}, {ProgressPct: 50}, {}, {ProgressPct: 150}}
	e := ComputeEVM(400, 100, tasks, nil, nil, time.Now())
	// (1 + 0.5 + 0 + 1 capped) / 4 = 62.5%
	if e.PercentComplete != 62.5 || e.EV != 250 {
		t.Fatalf("equal weights: %+v", e)
	}
}

func TestComputeEVM_ProjectWindowFallback(t *testing.T) {
	tasks := []TaskWork{{EstimatedHours: hours(8)}}
	e := ComputeEVM(1000, 0, tasks, day("2026-01-01"), day("2026-01-11"), *day("2026-01-06"))
	if e.PercentPlanned != 50 || e.PV != 500 {
		t.Fatalf("task without dates uses the project window: %+v", e)
	}
}

func TestComputeEVM_Edges(t *testing.T) {
	// No budget: nothing to judge, no division by zero.
	e := ComputeEVM(0, 300, []TaskWork{{Done: true}}, nil, nil, time.Now())
	if e.Health != "none" || e.EV != 0 || e.CPI != 0 {
		t.Fatalf("no budget: %+v", e)
	}
	// No tasks, spend without progress.
	e = ComputeEVM(1000, 300, nil, nil, nil, time.Now())
	if e.EV != 0 || e.CPI != 0 || e.EAC != 1000 || e.PercentSpent != 30 {
		t.Fatalf("no tasks: %+v", e)
	}
	// Spend beyond budget is red regardless of indices.
	e = ComputeEVM(1000, 1200, []TaskWork{{Done: true}}, nil, nil, time.Now())
	if e.Health != "red" || e.HealthReason != "spend exceeds the budget" {
		t.Fatalf("overspent: %+v", e)
	}
	// Before any task starts nothing is planned and nothing earned.
	e = ComputeEVM(1000, 0, []TaskWork{{Start: day("2026-05-01"), Due: day("2026-06-01")}}, nil, nil, *day("2026-04-01"))
	if e.PV != 0 || e.EV != 0 || e.SPI != 0 {
		t.Fatalf("not started: %+v", e)
	}
}

func TestHealthAmber(t *testing.T) {
	if h, _ := health(EVM{CPI: 0.9, SPI: 1.0, AC: 10}, 100); h != "amber" {
		t.Fatalf("CPI 0.9 = %s, want amber", h)
	}
	if h, _ := health(EVM{CPI: 1.0, SPI: 0.96, AC: 10}, 100); h != "green" {
		t.Fatalf("SPI 0.96 = %s, want green", h)
	}
}

func TestHoursSummary(t *testing.T) {
	est := func(v float64) *float64 { return &v }
	tasks := []taskRow{{EstimatedHours: est(10)}, {EstimatedHours: est(30)}, {}}
	h := hoursSummary(tasks, &erp.Hours{ApprovedHours: 15, SubmittedHours: 4})
	if h.Estimated != 40 || h.Logged != 15 || h.Pending != 4 || h.UtilisationPct == nil || *h.UtilisationPct != 37.5 {
		t.Fatalf("summary = %+v", h)
	}
	if none := hoursSummary(nil, &erp.Hours{ApprovedHours: 5}); none.UtilisationPct != nil {
		t.Fatal("no estimates: utilisation must be empty, not infinite")
	}
	if nolog := hoursSummary(tasks, nil); nolog.Logged != 0 || nolog.Estimated != 40 {
		t.Fatalf("no logged time: %+v", nolog)
	}
}
