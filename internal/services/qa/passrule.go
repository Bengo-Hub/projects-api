package qa

import (
	"math"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/qamodel"
)

// ResultView is the part of a current result the pass rule reads.
type ResultView struct {
	CriterionID uuid.UUID
	Result      string
	Severity    *int
}

// CriterionOutcome is one criterion's standing across all testers.
type CriterionOutcome struct {
	ID       uuid.UUID `json:"id"`
	AreaCode string    `json:"area_code,omitempty"`
	Title    string    `json:"title"`
	Weight   string    `json:"weight"`
	// Status is the worst current verdict: fail, then blocked, then pass; not_run when no tester
	// has a verdict yet.
	Status string `json:"status"`
	// Severity is the most severe open failure, when Status is fail.
	Severity *int `json:"severity,omitempty"`
	Testers  int  `json:"testers"`
}

// Outcome is the pass rule applied to a session's current results.
type Outcome struct {
	Accepted     bool               `json:"accepted"`
	Score        float64            `json:"score"`
	Counts       map[string]int     `json:"counts"`
	MustTotal    int                `json:"must_total"`
	MustPassed   int                `json:"must_passed"`
	OpenBlocking int                `json:"open_blocking"`
	Reasons      []string           `json:"reasons"`
	Criteria     []CriterionOutcome `json:"criteria"`
}

var statusRank = map[string]int{qamodel.ResultNotRun: 0, qamodel.ResultPass: 1, qamodel.ResultBlocked: 2, qamodel.ResultFail: 3}

// Evaluate applies rule to the session's criteria and current results (one per criterion and
// tester). It is pure, so the board, the close and the report all agree.
//
// Accepted means: every "must" criterion passed (when the rule requires it), no failure at a
// blocking severity is open, and the weighted score reaches the threshold. The score is the
// weight of passed criteria over the weight of all criteria, as a percentage.
func Evaluate(rule qamodel.PassRule, criteria []qamodel.SnapshotCriterion, results []ResultView) Outcome {
	byCrit := map[uuid.UUID][]ResultView{}
	for _, r := range results {
		byCrit[r.CriterionID] = append(byCrit[r.CriterionID], r)
	}
	out := Outcome{
		Counts:   map[string]int{qamodel.ResultPass: 0, qamodel.ResultFail: 0, qamodel.ResultBlocked: 0, qamodel.ResultNotRun: 0},
		Reasons:  []string{},
		Criteria: make([]CriterionOutcome, 0, len(criteria)),
	}
	var total, passed float64
	for _, c := range criteria {
		co := CriterionOutcome{ID: c.ID, AreaCode: c.AreaCode, Title: c.Title, Weight: c.Weight, Status: qamodel.ResultNotRun}
		for _, r := range byCrit[c.ID] {
			if r.Result == qamodel.ResultNotRun {
				continue
			}
			co.Testers++
			if statusRank[r.Result] > statusRank[co.Status] {
				co.Status = r.Result
			}
			if r.Result == qamodel.ResultFail {
				co.Severity = mostSevere(co.Severity, r.Severity)
			}
		}
		if co.Status != qamodel.ResultFail {
			co.Severity = nil
		}
		out.Counts[co.Status]++
		w := rule.Weights[c.Weight]
		if w <= 0 {
			w = qamodel.DefaultPassRule().Weights[c.Weight]
		}
		total += w
		if co.Status == qamodel.ResultPass {
			passed += w
		}
		if c.Weight == qamodel.WeightMust {
			out.MustTotal++
			if co.Status == qamodel.ResultPass {
				out.MustPassed++
			}
		}
		if co.Status == qamodel.ResultFail && rule.Blocks(failSeverity(byCrit[c.ID])) {
			out.OpenBlocking++
		}
		out.Criteria = append(out.Criteria, co)
	}
	if total > 0 {
		out.Score = math.Round(passed/total*1000) / 10
	}

	switch {
	case len(criteria) == 0:
		out.Reasons = append(out.Reasons, "the session has no criteria")
	default:
		if rule.RequireAllMust && out.MustPassed < out.MustTotal {
			out.Reasons = append(out.Reasons, "not every must criterion has passed")
		}
		if out.OpenBlocking > 0 {
			out.Reasons = append(out.Reasons, "a blocking failure is open")
		}
		if rule.MinScore > 0 && out.Score < rule.MinScore {
			out.Reasons = append(out.Reasons, "the weighted score is below the threshold")
		}
	}
	out.Accepted = len(out.Reasons) == 0
	return out
}

// failSeverity is the most severe severity among a criterion's failing results; nil when a failure
// carries none (treated as blocking).
func failSeverity(rs []ResultView) *int {
	var sev *int
	for _, r := range rs {
		if r.Result != qamodel.ResultFail {
			continue
		}
		if r.Severity == nil {
			return nil
		}
		sev = mostSevere(sev, r.Severity)
	}
	return sev
}

// mostSevere returns the lower number (1 is critical).
func mostSevere(a, b *int) *int {
	switch {
	case b == nil:
		return a
	case a == nil || *b < *a:
		v := *b
		return &v
	default:
		return a
	}
}
