package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	entassignee "github.com/bengobox/projects-service/internal/ent/taskassignee"
	"github.com/bengobox/projects-service/internal/platform/events"
	"github.com/bengobox/projects-service/internal/services/notify"
)

// Automations (sprint 16) live in the project's metadata under automations, written only by the
// automations route. When a task's status changes to a rule's trigger (optionally from a given
// status), the rule's actions run in the same transaction as the change: assign people, notify
// people (project.task.automation_notice) or set a field. An action never changes the status, and
// actions write the task directly rather than through applyStatus, so an automation's own change
// can never trigger rules again.
const MetaAutomations = "automations"

// MaxAutomations bounds a project's rules; maxActions bounds one rule's actions.
const (
	MaxAutomations = 25
	maxActions     = 5
	maxNotifyUsers = 20
)

// Action types.
const (
	ActionAssign   = "assign"
	ActionNotify   = "notify"
	ActionSetField = "set_field"
)

// EventAutomationNotice is the notify action's event (subject project.task.automation_notice).
const EventAutomationNotice = "task.automation_notice"

var metaKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,59}$`)

// Priorities a set_field action may set.
var priorities = []string{"low", "medium", "high", "critical"}

// Trigger fires a rule when a task moves to To (from From, or from anywhere when empty).
type Trigger struct {
	To   string `json:"to"`
	From string `json:"from,omitempty"`
}

// AutomationAction is one action of a rule. assign: UserIDs, Mode add (default, keeps the current
// assignees) or replace. notify: UserIDs (plus "assignees" in Recipients for the task's
// assignees), Message. set_field: Field priority or metadata.<key>, Value.
type AutomationAction struct {
	Type       string      `json:"type"`
	UserIDs    []uuid.UUID `json:"user_ids,omitempty"`
	Mode       string      `json:"mode,omitempty"`
	Recipients []string    `json:"recipients,omitempty"`
	Message    string      `json:"message,omitempty"`
	Field      string      `json:"field,omitempty"`
	Value      any         `json:"value,omitempty"`
}

// Automation is one rule.
type Automation struct {
	ID      string             `json:"id"`
	Name    string             `json:"name"`
	Enabled bool               `json:"enabled"`
	Trigger Trigger            `json:"trigger"`
	Actions []AutomationAction `json:"actions"`
}

// MatchAutomations returns the enabled rules a move from `from` to `to` fires, in list order.
func MatchAutomations(list []Automation, from, to string) []Automation {
	if from == to {
		return nil
	}
	var out []Automation
	for _, a := range list {
		if a.Enabled && a.Trigger.To == to && (a.Trigger.From == "" || a.Trigger.From == from) {
			out = append(out, a)
		}
	}
	return out
}

// ValidateAutomations checks a rule list against the project's statuses and assigns ids to new
// rules. Members are the users who may be assigned or notified (project staff).
func ValidateAutomations(list []Automation, statuses []Status, isMember func(uuid.UUID) bool) ([]Automation, error) {
	if len(list) > MaxAutomations {
		return nil, ErrValidation(fmt.Sprintf("a project has at most %d automations", MaxAutomations))
	}
	out := make([]Automation, 0, len(list))
	seen := map[string]bool{}
	for i, a := range list {
		name := strings.TrimSpace(a.Name)
		if name == "" || len([]rune(name)) > 120 {
			return nil, ErrValidation(fmt.Sprintf("automation %d needs a name of at most 120 characters", i+1))
		}
		if !hasStatus(statuses, a.Trigger.To) {
			return nil, ErrValidation(fmt.Sprintf("automation %q: trigger status %q is not one of this project's statuses", name, a.Trigger.To))
		}
		if a.Trigger.From != "" && !hasStatus(statuses, a.Trigger.From) {
			return nil, ErrValidation(fmt.Sprintf("automation %q: from status %q is not one of this project's statuses", name, a.Trigger.From))
		}
		if len(a.Actions) == 0 || len(a.Actions) > maxActions {
			return nil, ErrValidation(fmt.Sprintf("automation %q needs 1 to %d actions", name, maxActions))
		}
		id := strings.TrimSpace(a.ID)
		if _, err := uuid.Parse(id); err != nil {
			id = uuid.NewString()
		}
		if seen[id] {
			id = uuid.NewString()
		}
		seen[id] = true
		actions := make([]AutomationAction, 0, len(a.Actions))
		for _, act := range a.Actions {
			clean, err := validateAction(name, act, isMember)
			if err != nil {
				return nil, err
			}
			actions = append(actions, clean)
		}
		out = append(out, Automation{ID: id, Name: name, Enabled: a.Enabled, Trigger: a.Trigger, Actions: actions})
	}
	return out, nil
}

func validateAction(name string, a AutomationAction, isMember func(uuid.UUID) bool) (AutomationAction, error) {
	checkUsers := func(ids []uuid.UUID) ([]uuid.UUID, error) {
		ids, err := normalizeAssignees(ids)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if !isMember(id) {
				return nil, ErrValidation(fmt.Sprintf("automation %q: user %s is not a member of this project", name, id))
			}
		}
		return ids, nil
	}
	switch a.Type {
	case ActionAssign:
		ids, err := checkUsers(a.UserIDs)
		if err != nil {
			return a, err
		}
		if len(ids) == 0 {
			return a, ErrValidation(fmt.Sprintf("automation %q: assign needs at least one user", name))
		}
		mode := a.Mode
		if mode == "" {
			mode = "add"
		}
		if mode != "add" && mode != "replace" {
			return a, ErrValidation(fmt.Sprintf("automation %q: assign mode must be add or replace", name))
		}
		return AutomationAction{Type: ActionAssign, UserIDs: ids, Mode: mode}, nil
	case ActionNotify:
		ids, err := checkUsers(a.UserIDs)
		if err != nil {
			return a, err
		}
		if len(ids) > maxNotifyUsers {
			return a, ErrValidation(fmt.Sprintf("automation %q: notify at most %d people", name, maxNotifyUsers))
		}
		var recipients []string
		for _, r := range a.Recipients {
			if r != "assignees" {
				return a, ErrValidation(fmt.Sprintf("automation %q: recipients may only list \"assignees\"", name))
			}
			if !slices.Contains(recipients, r) {
				recipients = append(recipients, r)
			}
		}
		if len(ids) == 0 && len(recipients) == 0 {
			return a, ErrValidation(fmt.Sprintf("automation %q: notify needs user_ids or recipients", name))
		}
		msg := strings.TrimSpace(a.Message)
		if len([]rune(msg)) > 500 {
			return a, ErrValidation(fmt.Sprintf("automation %q: the message is at most 500 characters", name))
		}
		return AutomationAction{Type: ActionNotify, UserIDs: ids, Recipients: recipients, Message: msg}, nil
	case ActionSetField:
		switch {
		case a.Field == "priority":
			s, _ := a.Value.(string)
			if !slices.Contains(priorities, s) {
				return a, ErrValidation(fmt.Sprintf("automation %q: priority must be low, medium, high or critical", name))
			}
		case strings.HasPrefix(a.Field, fieldMetadataPrefix):
			key := strings.TrimPrefix(a.Field, fieldMetadataPrefix)
			if !metaKeyPattern.MatchString(key) || slices.Contains(serverOwnedMeta, key) || key == MetaRepeat {
				return a, ErrValidation(fmt.Sprintf("automation %q: metadata key %q cannot be set by an automation", name, key))
			}
			if b, err := json.Marshal(a.Value); err != nil || len(b) > 2000 {
				return a, ErrValidation(fmt.Sprintf("automation %q: the value is too large", name))
			}
		default:
			return a, ErrValidation(fmt.Sprintf("automation %q: set_field takes priority or metadata.<key>", name))
		}
		return AutomationAction{Type: ActionSetField, Field: a.Field, Value: a.Value}, nil
	}
	return a, ErrValidation(fmt.Sprintf("automation %q: action type must be assign, notify or set_field", name))
}

// automationsOf reads a project's rules from its metadata.
func automationsOf(meta map[string]any) []Automation {
	var list []Automation
	if raw, ok := meta[MetaAutomations]; ok && raw != nil {
		if b, err := json.Marshal(raw); err == nil {
			_ = json.Unmarshal(b, &list)
		}
	}
	return list
}

// runAutomations runs the rules a status move from `from` to t.Status fires, on the saved task t
// inside tx, and returns the task as it is afterwards (assignees loaded). The names of the rules
// that ran go into changes["automations"].
func runAutomations(ctx context.Context, tx *ent.Tx, p *ent.Project, t *ent.Task, from string, changes map[string]any) (*ent.Task, error) {
	if !planningRulesOn(ctx) {
		return t, nil
	}
	fired := MatchAutomations(automationsOf(p.Metadata), from, t.Status)
	if len(fired) == 0 {
		return t, nil
	}
	current, err := tx.TaskAssignee.Query().Where(entassignee.TenantID(t.TenantID), entassignee.TaskID(t.ID)).
		Order(assigneeOrderBy()...).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load assignees: %w", err)
	}
	assignees := assigneeIDs(current)
	assigneesChanged := false
	var priority *string
	meta := map[string]any{}
	metaChanged := false
	type pendingNotice struct {
		rule       Automation
		recipients []uuid.UUID
		message    string
	}
	var notices []pendingNotice
	ran := make([]map[string]any, 0, len(fired))
	for _, rule := range fired {
		for _, a := range rule.Actions {
			switch a.Type {
			case ActionAssign:
				if a.Mode == "replace" {
					assignees = slices.Clone(a.UserIDs)
				} else {
					for _, id := range a.UserIDs {
						if !slices.Contains(assignees, id) {
							assignees = append(assignees, id)
						}
					}
				}
				assigneesChanged = true
			case ActionNotify:
				ids := slices.Clone(a.UserIDs)
				if slices.Contains(a.Recipients, "assignees") {
					ids = append(ids, assignees...)
				}
				notices = append(notices, pendingNotice{rule: rule, recipients: ids, message: a.Message})
			case ActionSetField:
				if a.Field == "priority" {
					s, _ := a.Value.(string)
					priority = &s
				} else {
					meta[strings.TrimPrefix(a.Field, fieldMetadataPrefix)] = a.Value
					metaChanged = true
				}
			}
		}
		ran = append(ran, map[string]any{"id": rule.ID, "name": rule.Name})
	}
	changes["automations"] = ran
	if priority != nil || metaChanged {
		u := tx.Task.UpdateOneID(t.ID)
		if priority != nil && *priority != t.Priority {
			u.SetPriority(*priority)
			changes["priority"] = *priority
		}
		if metaChanged {
			merged := make(map[string]any, len(t.Metadata)+len(meta))
			for k, v := range t.Metadata {
				merged[k] = v
			}
			for k, v := range meta {
				merged[k] = v
			}
			u.SetMetadata(merged)
			changes["metadata"] = "changed"
		}
		if t, err = u.Save(ctx); err != nil {
			return nil, fmt.Errorf("automation update: %w", err)
		}
	}
	if assigneesChanged {
		// People who have since left the project are skipped rather than failing the move.
		members, err := notify.Members(ctx, tx, t.TenantID, t.ProjectID, assignees)
		if err != nil {
			return nil, err
		}
		kept := assignees[:0:0]
		for _, id := range assignees {
			if members[id] {
				kept = append(kept, id)
			}
		}
		rows, added, err := setAssignees(ctx, tx, t.TenantID, t.ProjectID, t.ID, kept)
		if err != nil {
			return nil, err
		}
		t.Edges.Assignees = rows
		changes["assignee_ids"] = idStrings(assigneeIDs(rows))
		if len(added) > 0 {
			n, err := notice(ctx, tx, p, t)
			if err != nil {
				return nil, err
			}
			if err := notify.Write(ctx, tx, notify.EventTaskAssigned, n, added); err != nil {
				return nil, err
			}
		}
	} else if err := loadAssignees(ctx, tx, t); err != nil {
		return nil, err
	}
	for _, n := range notices {
		if err := writeAutomationNotice(ctx, tx, p, t, from, n.rule, n.recipients, n.message); err != nil {
			return nil, err
		}
	}
	return t, nil
}

// writeAutomationNotice writes one project.task.automation_notice with every recipient id;
// notifications-api fans it out to one email per recipient, looked up in its own user table.
func writeAutomationNotice(ctx context.Context, tx *ent.Tx, p *ent.Project, t *ent.Task, from string, rule Automation, recipients []uuid.UUID, message string) error {
	ids, err := normalizeAssignees(recipients)
	if err != nil {
		ids = recipients
	}
	if len(ids) == 0 {
		return nil
	}
	return events.Write(ctx, tx, events.AggregateProject, EventAutomationNotice, t.ID, p.TenantID, map[string]any{
		"project_id": p.ID.String(), "project_name": p.Name, "task_id": t.ID.String(), "task_title": t.Title,
		"rule_id": rule.ID, "rule_name": rule.Name, "from_status": from, "to_status": t.Status,
		"to_status_label": StatusLabel(p, t.Status), "message": message, "recipient_ids": idStrings(ids),
		"action_path": notify.ActionPath(ctx, p.ID, t.ID),
	})
}
