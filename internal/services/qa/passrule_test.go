package qa

import (
	"testing"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/qamodel"
)

func sev(n int) *int { return &n }

func crit(weight string) qamodel.SnapshotCriterion {
	return qamodel.SnapshotCriterion{ID: uuid.New(), Title: "c", Weight: weight}
}

func TestEvaluate(t *testing.T) {
	rule := qamodel.DefaultPassRule()
	must1, must2, should, could := crit(qamodel.WeightMust), crit(qamodel.WeightMust), crit(qamodel.WeightShould), crit(qamodel.WeightCould)
	all := []qamodel.SnapshotCriterion{must1, must2, should, could}

	// Every must passes, the should fails at severity 3: accepted, score (3+3+1)/(3+3+2+1).
	o := Evaluate(rule, all, []ResultView{
		{CriterionID: must1.ID, Result: qamodel.ResultPass},
		{CriterionID: must2.ID, Result: qamodel.ResultPass},
		{CriterionID: should.ID, Result: qamodel.ResultFail, Severity: sev(3)},
		{CriterionID: could.ID, Result: qamodel.ResultPass},
	})
	if !o.Accepted || o.Score != 77.8 || o.MustPassed != 2 || o.OpenBlocking != 0 {
		t.Fatalf("minor failure only: %+v", o)
	}

	// A severity 2 failure on a should blocks acceptance even though every must passed.
	o = Evaluate(rule, all, []ResultView{
		{CriterionID: must1.ID, Result: qamodel.ResultPass},
		{CriterionID: must2.ID, Result: qamodel.ResultPass},
		{CriterionID: should.ID, Result: qamodel.ResultFail, Severity: sev(2)},
	})
	if o.Accepted || o.OpenBlocking != 1 {
		t.Fatalf("blocking failure accepted: %+v", o)
	}

	// Worst verdict wins across testers; a must not run fails the rule.
	o = Evaluate(rule, all, []ResultView{
		{CriterionID: must1.ID, Result: qamodel.ResultPass},
		{CriterionID: must1.ID, Result: qamodel.ResultBlocked},
		{CriterionID: should.ID, Result: qamodel.ResultPass},
	})
	if o.Accepted || o.Criteria[0].Status != qamodel.ResultBlocked || o.Criteria[0].Testers != 2 || o.Counts[qamodel.ResultNotRun] != 2 {
		t.Fatalf("worst verdict: %+v", o)
	}

	// A failure without a severity is treated as blocking.
	o = Evaluate(rule, []qamodel.SnapshotCriterion{could}, []ResultView{{CriterionID: could.ID, Result: qamodel.ResultFail}})
	if o.Accepted || o.OpenBlocking != 1 {
		t.Fatalf("failure without severity: %+v", o)
	}

	// A score threshold applies on top.
	strict := rule
	strict.MinScore = 90
	o = Evaluate(strict, all, []ResultView{
		{CriterionID: must1.ID, Result: qamodel.ResultPass},
		{CriterionID: must2.ID, Result: qamodel.ResultPass},
	})
	if o.Accepted || len(o.Reasons) != 1 {
		t.Fatalf("threshold: %+v", o)
	}

	if o := Evaluate(rule, nil, nil); o.Accepted {
		t.Fatal("an empty session must never be accepted")
	}
}

func TestPassRuleTightening(t *testing.T) {
	base := qamodel.DefaultPassRule()
	ok := base
	ok.BlockingSeverities = []int{1, 2, 3}
	ok.MinScore = 80
	if err := base.CheckTightens(ok); err != nil {
		t.Fatalf("a stricter rule was refused: %v", err)
	}
	for name, mut := range map[string]func(*qamodel.PassRule){
		"drop must":     func(r *qamodel.PassRule) { r.RequireAllMust = false },
		"drop severity": func(r *qamodel.PassRule) { r.BlockingSeverities = []int{1} },
		"change weight": func(r *qamodel.PassRule) { r.Weights = map[string]float64{"must": 1, "should": 2, "could": 1} },
	} {
		next := qamodel.DefaultPassRule()
		mut(&next)
		if err := base.CheckTightens(next); err == nil {
			t.Errorf("%s: a looser rule was accepted", name)
		}
	}
	if _, err := (qamodel.PassRule{BlockingSeverities: []int{5}}).Normalize(); err == nil {
		t.Error("severity 5 accepted")
	}
	n, err := (qamodel.PassRule{BlockingSeverities: []int{2, 1, 2}}).Normalize()
	if err != nil || len(n.BlockingSeverities) != 2 || n.BlockingSeverities[0] != 1 || n.Weights["must"] != 3 {
		t.Fatalf("normalize: %+v %v", n, err)
	}
}
