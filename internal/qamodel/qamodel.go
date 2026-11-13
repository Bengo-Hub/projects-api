// Package qamodel holds the value types the QA and acceptance tables store as JSON (the pass rule
// and the frozen criteria snapshot) and the fixed vocabularies they use. It has no dependencies on
// ent or the services, so the ent schema and the qa service can both import it.
package qamodel

import (
	"fmt"
	"slices"

	"github.com/google/uuid"
)

// Template kinds.
const (
	KindSoftware = "software"
	KindProject  = "project"
	KindGeneral  = "general"
)

// Template statuses.
const (
	TemplateDraft    = "draft"
	TemplateActive   = "active"
	TemplateArchived = "archived"
)

// Criterion subject kinds: what a criterion checks. A feature is a catalogue feature code owned by
// subscriptions-api; a milestone or task is an id in this service; none is a free criterion.
const (
	SubjectFeature   = "feature"
	SubjectMilestone = "milestone"
	SubjectTask      = "task"
	SubjectNone      = "none"
)

// Session subject kinds: what a session accepts. A service (a catalogue service tag such as
// "hadia" or "pos") is platform work and its failures go to Codevertex ops; a project or a general
// title is the tenant's own work and its failures go to the tenant's team.
const (
	SessionSubjectService = "service"
	SessionSubjectProject = "project"
	SessionSubjectGeneral = "general"
)

// Session statuses.
const (
	SessionDraft    = "draft"
	SessionOpen     = "open"
	SessionClosed   = "closed"
	SessionAccepted = "accepted"
	SessionRejected = "rejected"
)

// Invite roles and statuses.
const (
	RoleTester   = "tester"
	RoleSigner   = "signer"
	RoleObserver = "observer"

	InviteInvited = "invited"
	InviteActive  = "active"
	InviteRevoked = "revoked"
)

// Results.
const (
	ResultPass    = "pass"
	ResultFail    = "fail"
	ResultBlocked = "blocked"
	ResultNotRun  = "not_run"
)

// Defect statuses: open until a ticket is raised, ticketed once ticketing-api accepted it,
// ticket_failed when the call failed (it can be retried by hand).
const (
	DefectOpen         = "open"
	DefectTicketed     = "ticketed"
	DefectTicketFailed = "ticket_failed"
)

// Criterion weights.
const (
	WeightMust   = "must"
	WeightShould = "should"
	WeightCould  = "could"
)

// Severities run from 1 (critical) to 4 (cosmetic).
const (
	SeverityCritical = 1
	SeverityMajor    = 2
	SeverityMinor    = 3
	SeverityCosmetic = 4
)

// ValidKind, ValidSubject and the rest check a value against its vocabulary.
func ValidKind(s string) bool { return s == KindSoftware || s == KindProject || s == KindGeneral }

func ValidSubject(s string) bool {
	return s == SubjectFeature || s == SubjectMilestone || s == SubjectTask || s == SubjectNone
}

func ValidSessionSubject(s string) bool {
	return s == SessionSubjectService || s == SessionSubjectProject || s == SessionSubjectGeneral
}

func ValidWeight(s string) bool { return s == WeightMust || s == WeightShould || s == WeightCould }

func ValidRole(s string) bool { return s == RoleTester || s == RoleSigner || s == RoleObserver }

func ValidResult(s string) bool {
	return s == ResultPass || s == ResultFail || s == ResultBlocked || s == ResultNotRun
}

func ValidSeverity(n int) bool { return n >= SeverityCritical && n <= SeverityCosmetic }

// PassRule decides whether a session is accepted. The template carries the default; a session
// copies it and its owner may only make it stricter.
type PassRule struct {
	// RequireAllMust: every "must" criterion has passed.
	RequireAllMust bool `json:"require_all_must"`
	// BlockingSeverities: an open failure at any of these severities blocks acceptance.
	BlockingSeverities []int `json:"blocking_severities"`
	// MinScore is the lowest weighted score (0 to 100) that is accepted; 0 means no threshold.
	MinScore float64 `json:"min_score"`
	// Weights per criterion weight, for the weighted score shown for tender-style use.
	Weights map[string]float64 `json:"weights"`
}

// DefaultPassRule is the owner's rule: every must passes and no severity 1 or 2 failure is open.
// Weights 3, 2 and 1 feed the weighted score.
func DefaultPassRule() PassRule {
	return PassRule{
		RequireAllMust:     true,
		BlockingSeverities: []int{SeverityCritical, SeverityMajor},
		Weights:            map[string]float64{WeightMust: 3, WeightShould: 2, WeightCould: 1},
	}
}

// Normalize fills missing weights from the default, sorts and de-duplicates the severities, and
// checks every value is in range.
func (r PassRule) Normalize() (PassRule, error) {
	def := DefaultPassRule()
	out := PassRule{RequireAllMust: r.RequireAllMust, MinScore: r.MinScore, Weights: map[string]float64{}}
	for _, s := range r.BlockingSeverities {
		if !ValidSeverity(s) {
			return PassRule{}, fmt.Errorf("blocking severity %d is outside 1 to 4", s)
		}
		if !slices.Contains(out.BlockingSeverities, s) {
			out.BlockingSeverities = append(out.BlockingSeverities, s)
		}
	}
	slices.Sort(out.BlockingSeverities)
	if r.MinScore < 0 || r.MinScore > 100 {
		return PassRule{}, fmt.Errorf("min_score %v is outside 0 to 100", r.MinScore)
	}
	for w, v := range r.Weights {
		if !ValidWeight(w) {
			return PassRule{}, fmt.Errorf("unknown weight %q", w)
		}
		if v <= 0 {
			return PassRule{}, fmt.Errorf("weight %q must be above 0", w)
		}
		out.Weights[w] = v
	}
	for w, v := range def.Weights {
		if _, ok := out.Weights[w]; !ok {
			out.Weights[w] = v
		}
	}
	return out, nil
}

// CheckTightens returns an error when next is looser than r: it may not drop the must rule, drop a
// blocking severity, lower the score threshold, or change the weights (they belong to the
// template, and changing them could lift a score over the threshold).
func (r PassRule) CheckTightens(next PassRule) error {
	if r.RequireAllMust && !next.RequireAllMust {
		return fmt.Errorf("the template requires every must criterion to pass")
	}
	for _, s := range r.BlockingSeverities {
		if !slices.Contains(next.BlockingSeverities, s) {
			return fmt.Errorf("severity %d failures must stay blocking", s)
		}
	}
	if next.MinScore < r.MinScore {
		return fmt.Errorf("min_score may not go below the template's %v", r.MinScore)
	}
	if len(next.Weights) != len(r.Weights) {
		return fmt.Errorf("weights are set by the template")
	}
	for w, v := range r.Weights {
		if next.Weights[w] != v {
			return fmt.Errorf("weights are set by the template")
		}
	}
	return nil
}

// Blocks reports whether an open failure at severity sev blocks acceptance. A failure recorded
// without a severity is treated as blocking.
func (r PassRule) Blocks(sev *int) bool {
	if sev == nil {
		return true
	}
	return slices.Contains(r.BlockingSeverities, *sev)
}

// SnapshotCriterion is one criterion as it stood when a session was created. Sessions keep their
// own copy, so the audit trail never moves when the template changes.
type SnapshotCriterion struct {
	ID             uuid.UUID `json:"id"`
	AreaCode       string    `json:"area_code,omitempty"`
	Title          string    `json:"title"`
	SubjectKind    string    `json:"subject_kind"`
	SubjectRef     string    `json:"subject_ref,omitempty"`
	RequirementRef string    `json:"requirement_ref,omitempty"`
	Preconditions  string    `json:"preconditions,omitempty"`
	Steps          string    `json:"steps,omitempty"`
	ExpectedResult string    `json:"expected_result,omitempty"`
	Weight         string    `json:"weight"`
	AutomatedRef   string    `json:"automated_ref,omitempty"`
	SortOrder      int       `json:"sort_order"`
}
