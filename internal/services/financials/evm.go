// Package financials joins a project's work (tasks, from projects-api) with its money (budget,
// cost and revenue, from treasury) into earned-value management figures and a portfolio view.
package financials

import (
	"math"
	"time"
)

// TaskWork is the slice of a task earned value needs.
type TaskWork struct {
	Start          *time.Time
	Due            *time.Time
	EstimatedHours *float64
	ProgressPct    int
	Done           bool
}

// EVM holds the standard earned-value figures (PMI): BAC budget at completion, PV planned value,
// EV earned value, AC actual cost; CPI = EV/AC and SPI = EV/PV (1.0 = on plan, below 1 is
// worse); EAC estimate at completion, ETC estimate to complete, VAC variance at completion.
type EVM struct {
	BAC             float64 `json:"bac"`
	PV              float64 `json:"pv"`
	EV              float64 `json:"ev"`
	AC              float64 `json:"ac"`
	CV              float64 `json:"cv"` // EV - AC
	SV              float64 `json:"sv"` // EV - PV
	CPI             float64 `json:"cpi"`
	SPI             float64 `json:"spi"`
	EAC             float64 `json:"eac"`              // BAC / CPI: current efficiency continues
	EACAtBudgetRate float64 `json:"eac_budget_rate"`  // AC + (BAC - EV): remaining work at plan
	ETC             float64 `json:"etc"`
	VAC             float64 `json:"vac"`
	PercentComplete float64 `json:"percent_complete"` // earned share of the work
	PercentPlanned  float64 `json:"percent_planned"`  // share planned done by now
	PercentSpent    float64 `json:"percent_spent"`    // AC / BAC
	Health          string  `json:"health"`           // green | amber | red | none
	HealthReason    string  `json:"health_reason,omitempty"`
}

// plannedFraction is how much of a task should be done at asOf: 0 before its start, 1 after its
// due date, linear in between. Without dates it falls back to the project window; a task with no
// schedule at all is treated as planned-as-done-when-earned (neutral for SPI).
func plannedFraction(t TaskWork, projStart, projEnd *time.Time, asOf time.Time, earned float64) float64 {
	start, end := t.Start, t.Due
	if start == nil {
		start = projStart
	}
	if end == nil {
		end = projEnd
	}
	switch {
	case end != nil && !asOf.Before(*end):
		return 1
	case start != nil && !asOf.After(*start):
		return 0
	case start == nil || end == nil || !end.After(*start):
		// No usable window (no dates, or only one bound and asOf inside it): neutral.
		return earned
	}
	total := end.Sub(*start).Seconds()
	return math.Min(1, math.Max(0, asOf.Sub(*start).Seconds()/total))
}

// Weights are the weighted task totals earned value is computed from: the total weight, the
// weight earned by progress, and the weight planned done by now. Tasks are weighted by their
// estimated hours; when no task has an estimate every task weighs the same. The portfolio reads
// them straight from SQL (taskStats); TaskWeights is the same rule over loaded tasks.
type Weights struct {
	Total, Earned, Planned float64
}

// TaskWeights computes Weights from tasks in Go.
func TaskWeights(tasks []TaskWork, projStart, projEnd *time.Time, asOf time.Time) Weights {
	anyEstimate := false
	for _, t := range tasks {
		if t.EstimatedHours != nil && *t.EstimatedHours > 0 {
			anyEstimate = true
			break
		}
	}
	var w Weights
	for _, t := range tasks {
		weight := 1.0
		if anyEstimate {
			if t.EstimatedHours == nil || *t.EstimatedHours <= 0 {
				continue
			}
			weight = *t.EstimatedHours
		}
		p := float64(t.ProgressPct) / 100
		if t.Done {
			p = 1
		}
		p = math.Min(1, math.Max(0, p))
		w.Total += weight
		w.Earned += weight * p
		w.Planned += weight * plannedFraction(t, projStart, projEnd, asOf, p)
	}
	return w
}

// ComputeEVM derives earned value from task progress and the budget.
func ComputeEVM(bac, ac float64, tasks []TaskWork, projStart, projEnd *time.Time, asOf time.Time) EVM {
	return EVMFromWeights(bac, ac, TaskWeights(tasks, projStart, projEnd, asOf))
}

// EVMFromWeights derives the earned-value figures from task weights, budget and actual cost.
func EVMFromWeights(bac, ac float64, w Weights) EVM {
	e := EVM{BAC: round2(bac), AC: round2(ac), Health: "none"}
	if w.Total > 0 {
		e.PercentComplete = round2(w.Earned / w.Total * 100)
		e.PercentPlanned = round2(w.Planned / w.Total * 100)
	}
	e.EV = round2(bac * e.PercentComplete / 100)
	e.PV = round2(bac * e.PercentPlanned / 100)
	e.CV = round2(e.EV - e.AC)
	e.SV = round2(e.EV - e.PV)
	if e.AC > 0 {
		e.CPI = round2(e.EV / e.AC)
	}
	if e.PV > 0 {
		e.SPI = round2(e.EV / e.PV)
	}
	switch {
	case e.CPI > 0:
		e.EAC = round2(bac / (e.EV / e.AC))
	default:
		e.EAC = round2(math.Max(bac, e.AC))
	}
	e.EACAtBudgetRate = round2(e.AC + (bac - e.EV))
	e.ETC = round2(math.Max(0, e.EAC-e.AC))
	e.VAC = round2(bac - e.EAC)
	if bac > 0 {
		e.PercentSpent = round2(e.AC / bac * 100)
	}
	e.Health, e.HealthReason = health(e, bac)
	return e
}

// health rates a project: red when cost or schedule efficiency is below 0.85 or spend already
// exceeds the budget, amber below 0.95, else green. Projects without a budget or without any
// progress data are "none" (nothing to judge).
func health(e EVM, bac float64) (string, string) {
	if bac <= 0 {
		return "none", "no approved budget"
	}
	if e.AC > bac {
		return "red", "spend exceeds the budget"
	}
	worst, reason := 1.0, ""
	if e.CPI > 0 && e.CPI < worst {
		worst, reason = e.CPI, "over cost for the work done"
	}
	if e.SPI > 0 && e.SPI < worst {
		worst, reason = e.SPI, "behind schedule"
	}
	switch {
	case worst < 0.85:
		return "red", reason
	case worst < 0.95:
		return "amber", reason
	}
	return "green", ""
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
