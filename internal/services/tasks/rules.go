package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Status rules (sprint 16) live in the project's metadata under status_rules, written only by the
// status rules route (the project update drops the key). They say which moves between statuses a
// project allows and which fields a task needs before a move. Every status change goes through
// applyStatus, so the board drag, the drawer and the idempotent status route all obey them.
const MetaStatusRules = "status_rules"

// Required field codes a requirement may list. "metadata.<key>" asks for a non-empty task
// metadata key.
const (
	FieldAssignee       = "assignee"
	FieldEstimatedHours = "estimated_hours"
	FieldActualHours    = "actual_hours"
	FieldStartDate      = "start_date"
	FieldDueDate        = "due_date"
	FieldDescription    = "description"
	FieldComment        = "comment"
	fieldMetadataPrefix = "metadata."
)

// MetaActualHours is the task metadata key the actual_hours requirement reads (a number above
// zero, set from the task drawer).
const MetaActualHours = "actual_hours"

var fieldLabels = map[string]string{
	FieldAssignee: "an assignee", FieldEstimatedHours: "an estimate", FieldActualHours: "actual hours",
	FieldStartDate: "a start date", FieldDueDate: "a due date", FieldDescription: "a description",
	FieldComment: "a comment",
}

// maxRuleEntries bounds the transitions and the requirements of one project.
const maxRuleEntries = 100

// Transition limits the moves out of From to the statuses in To. A status without a transition
// entry may move anywhere.
type Transition struct {
	From string   `json:"from"`
	To   []string `json:"to"`
}

// Requirement lists the fields a task needs before it moves to To (from From, or from anywhere
// when From is empty).
type Requirement struct {
	To     string   `json:"to"`
	From   string   `json:"from,omitempty"`
	Fields []string `json:"fields"`
}

// StatusRules is a project's status rules.
type StatusRules struct {
	Transitions  []Transition  `json:"transitions"`
	Requirements []Requirement `json:"requirements"`
}

// Empty reports rules that block nothing.
func (r StatusRules) Empty() bool { return len(r.Transitions) == 0 && len(r.Requirements) == 0 }

// Block reasons.
const (
	ReasonTransition = "transition_not_allowed"
	ReasonMissing    = "missing_fields"
)

// StatusRuleBlocked refuses a move the project's status rules do not allow (422
// status_rule_blocked). Missing lists the field codes the task still needs.
type StatusRuleBlocked struct {
	From    string   `json:"from"`
	To      string   `json:"to"`
	Reason  string   `json:"reason"`
	Missing []string `json:"missing_fields"`
	Message string   `json:"message"`
}

func (e *StatusRuleBlocked) Error() string { return e.Message }

// MoveFacts is what the requirements are checked against: the task as it will be after the
// change (an update that sets an estimate and moves to done in one request passes).
type MoveFacts struct {
	Assignees      int
	EstimatedHours *float64
	StartDate      *time.Time
	DueDate        time.Time
	Description    string
	Comments       int
	Metadata       map[string]any
}

// fieldLabel names a field code for a message.
func fieldLabel(code string) string {
	if l, ok := fieldLabels[code]; ok {
		return l
	}
	return strings.ReplaceAll(strings.TrimPrefix(code, fieldMetadataPrefix), "_", " ")
}

// validField reports a known field code.
func validField(code string) bool {
	if _, ok := fieldLabels[code]; ok {
		return true
	}
	key, ok := strings.CutPrefix(code, fieldMetadataPrefix)
	return ok && metaKeyPattern.MatchString(key)
}

// has reports whether facts satisfy one field code.
func (f MoveFacts) has(code string) bool {
	switch code {
	case FieldAssignee:
		return f.Assignees > 0
	case FieldEstimatedHours:
		return f.EstimatedHours != nil && *f.EstimatedHours > 0
	case FieldActualHours:
		n, ok := toFloat(f.Metadata[MetaActualHours])
		return ok && n > 0
	case FieldStartDate:
		return f.StartDate != nil && !f.StartDate.IsZero()
	case FieldDueDate:
		return !f.DueDate.IsZero()
	case FieldDescription:
		return strings.TrimSpace(f.Description) != ""
	case FieldComment:
		return f.Comments > 0
	}
	if key, ok := strings.CutPrefix(code, fieldMetadataPrefix); ok {
		return nonEmpty(f.Metadata[key])
	}
	return true
}

func nonEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(x) != ""
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

// NeedsCounts reports whether a move to `to` from `from` has a requirement that reads the
// assignee or comment counts (so the caller loads them only then).
func (r StatusRules) NeedsCounts(from, to string) bool {
	for _, q := range r.Requirements {
		if q.To == to && (q.From == "" || q.From == from) &&
			(slices.Contains(q.Fields, FieldComment) || slices.Contains(q.Fields, FieldAssignee)) {
			return true
		}
	}
	return false
}

// EvaluateMove checks a move from `from` to `to` against the rules, nil when allowed. The labels
// map status keys to their names for the message.
func EvaluateMove(r StatusRules, from, to string, f MoveFacts, labels map[string]string) *StatusRuleBlocked {
	if from == to {
		return nil
	}
	label := func(k string) string {
		if l, ok := labels[k]; ok && l != "" {
			return l
		}
		return k
	}
	for _, t := range r.Transitions {
		if t.From == from && !slices.Contains(t.To, to) {
			return &StatusRuleBlocked{From: from, To: to, Reason: ReasonTransition, Missing: []string{},
				Message: fmt.Sprintf("This project does not allow moving a task from %s to %s.", label(from), label(to))}
		}
	}
	var missing []string
	for _, q := range r.Requirements {
		if q.To != to || (q.From != "" && q.From != from) {
			continue
		}
		for _, code := range q.Fields {
			if !f.has(code) && !slices.Contains(missing, code) {
				missing = append(missing, code)
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	names := make([]string, len(missing))
	for i, m := range missing {
		names[i] = fieldLabel(m)
	}
	return &StatusRuleBlocked{From: from, To: to, Reason: ReasonMissing, Missing: missing,
		Message: fmt.Sprintf("Before moving to %s, this task needs %s.", label(to), strings.Join(names, ", "))}
}

// ValidateStatusRules checks rules against the project's statuses: known keys, known field codes,
// bounded sizes. Transition and requirement lists are returned de-duplicated.
func ValidateStatusRules(r StatusRules, statuses []Status) (StatusRules, error) {
	if len(r.Transitions) > maxRuleEntries || len(r.Requirements) > maxRuleEntries {
		return r, ErrValidation(fmt.Sprintf("status rules hold at most %d transitions and %d requirements", maxRuleEntries, maxRuleEntries))
	}
	known := func(k string) bool { return hasStatus(statuses, k) }
	seenFrom := map[string]bool{}
	out := StatusRules{Transitions: []Transition{}, Requirements: []Requirement{}}
	for _, t := range r.Transitions {
		if !known(t.From) {
			return r, ErrValidation(fmt.Sprintf("transition from %q: not one of this project's statuses", t.From))
		}
		if seenFrom[t.From] {
			return r, ErrValidation(fmt.Sprintf("transitions from %q are listed twice", t.From))
		}
		seenFrom[t.From] = true
		to := []string{}
		for _, k := range t.To {
			if !known(k) {
				return r, ErrValidation(fmt.Sprintf("transition to %q: not one of this project's statuses", k))
			}
			if k != t.From && !slices.Contains(to, k) {
				to = append(to, k)
			}
		}
		out.Transitions = append(out.Transitions, Transition{From: t.From, To: to})
	}
	for _, q := range r.Requirements {
		if !known(q.To) {
			return r, ErrValidation(fmt.Sprintf("requirement for %q: not one of this project's statuses", q.To))
		}
		if q.From != "" && !known(q.From) {
			return r, ErrValidation(fmt.Sprintf("requirement from %q: not one of this project's statuses", q.From))
		}
		if len(q.Fields) == 0 {
			return r, ErrValidation(fmt.Sprintf("the requirement for %q lists no fields", q.To))
		}
		fields := []string{}
		for _, f := range q.Fields {
			f = strings.TrimSpace(f)
			if !validField(f) {
				return r, ErrValidation(fmt.Sprintf("unknown required field %q: use assignee, estimated_hours, actual_hours, start_date, due_date, description, comment or metadata.<key>", f))
			}
			if !slices.Contains(fields, f) {
				fields = append(fields, f)
			}
		}
		out.Requirements = append(out.Requirements, Requirement{To: q.To, From: q.From, Fields: fields})
	}
	return out, nil
}

// statusRulesOf reads a project's rules from its metadata (none when unset or unreadable).
func statusRulesOf(meta map[string]any) StatusRules {
	var r StatusRules
	if raw, ok := meta[MetaStatusRules]; ok && raw != nil {
		if b, err := json.Marshal(raw); err == nil {
			_ = json.Unmarshal(b, &r)
		}
	}
	return r
}

// statusLabels maps the project's status keys to labels.
func statusLabels(statuses []Status) map[string]string {
	out := make(map[string]string, len(statuses))
	for _, s := range statuses {
		out[s.Key] = s.Label
	}
	return out
}

type rulesKey struct{}

// WithPlanningRules returns ctx saying whether the tenant's plan includes project_automation. The
// task handlers set it from the caller's token: without the feature the stored status rules and
// automations stay dormant (kept, not enforced) until the plan has it again. Unset means enabled.
func WithPlanningRules(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, rulesKey{}, enabled)
}

func planningRulesOn(ctx context.Context) bool {
	on, set := ctx.Value(rulesKey{}).(bool)
	return !set || on
}
