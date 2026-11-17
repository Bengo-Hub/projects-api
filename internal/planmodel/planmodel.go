// Package planmodel holds the JSON shapes of the sprint 16 planning tables (baseline snapshots and
// project template definitions) so the ent schema and the services share one definition.
package planmodel

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// BaselineTask is one task as it stood when a baseline was saved.
type BaselineTask struct {
	ID             uuid.UUID  `json:"id"`
	Title          string     `json:"title"`
	Status         string     `json:"status"`
	StartDate      *time.Time `json:"start_date,omitempty"`
	DueDate        *time.Time `json:"due_date,omitempty"`
	EstimatedHours *float64   `json:"estimated_hours,omitempty"`
	ParentID       *uuid.UUID `json:"parent_id,omitempty"`
}

// BaselineMilestone is one milestone as it stood when a baseline was saved.
type BaselineMilestone struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	TargetDate *time.Time `json:"target_date,omitempty"`
}

// Snapshot is a saved baseline: the project's dates, its tasks (at most the Gantt limit) and its
// milestones. Truncated is set when the project had more tasks than were kept.
type Snapshot struct {
	ProjectStart *time.Time          `json:"project_start,omitempty"`
	ProjectEnd   *time.Time          `json:"project_end,omitempty"`
	Tasks        []BaselineTask      `json:"tasks"`
	Milestones   []BaselineMilestone `json:"milestones"`
	Truncated    bool                `json:"truncated"`
}

// TemplatePhase is a milestone of a template, OffsetDays after the project start.
type TemplatePhase struct {
	Ref         string `json:"ref"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	OffsetDays  int    `json:"offset_days"`
}

// TemplateTask is a task of a template: it starts OffsetDays after the project start and lasts
// DurationDays (at least one). PhaseRef, ParentRef and DependsOn name other refs of the template.
// Role is the template role the task is meant for (filled by whoever the project gives it).
type TemplateTask struct {
	Ref            string   `json:"ref"`
	Title          string   `json:"title"`
	Description    string   `json:"description,omitempty"`
	Priority       string   `json:"priority,omitempty"`
	OffsetDays     int      `json:"offset_days"`
	DurationDays   int      `json:"duration_days"`
	EstimatedHours *float64 `json:"estimated_hours,omitempty"`
	PhaseRef       string   `json:"phase_ref,omitempty"`
	ParentRef      string   `json:"parent_ref,omitempty"`
	DependsOn      []string `json:"depends_on,omitempty"`
	Role           string   `json:"role,omitempty"`
	WbsCode        string   `json:"wbs_code,omitempty"`
}

// TemplateRole is a role a project made from the template needs, Count people of it.
type TemplateRole struct {
	Code  string `json:"code"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

// TemplateDefinition is the whole plan of a template. Statuses and StatusRules are the board
// columns and status rules (the tasks package shapes), copied to the new project as they are.
type TemplateDefinition struct {
	DurationDays int             `json:"duration_days"`
	Phases       []TemplatePhase `json:"phases"`
	Tasks        []TemplateTask  `json:"tasks"`
	Roles        []TemplateRole  `json:"roles"`
	Statuses     json.RawMessage `json:"statuses,omitempty"`
	StatusRules  json.RawMessage `json:"status_rules,omitempty"`
}
