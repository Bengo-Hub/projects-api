package tasks

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	enttask "github.com/bengobox/projects-service/internal/ent/task"
)

// GanttTask is a task on the chart, with Critical set when it lies on the critical path.
type GanttTask struct {
	*ent.Task
	Critical bool `json:"critical"`
}

// GanttData is the project's plan for the Gantt chart. Truncated is set when the project has more
// than ganttLimit tasks and only the first ganttLimit (oldest first) are returned; the critical
// path is then computed over those. CriticalDays is the length of the critical path in days.
type GanttData struct {
	Tasks        []GanttTask `json:"data"`
	Truncated    bool        `json:"truncated"`
	Limit        int         `json:"limit"`
	CriticalDays int         `json:"critical_days"`
}

// GetGanttData returns tasks with their dependencies for Gantt chart rendering, capped, with the
// critical path marked: one query for the tasks and one for their dependencies.
func (s *Service) GetGanttData(ctx context.Context, tenantID, projectID uuid.UUID) (GanttData, error) {
	tasks, err := s.client.Task.Query().
		Where(enttask.TenantID(tenantID), enttask.ProjectID(projectID)).
		WithDependencies().
		Order(ent.Asc(enttask.FieldCreatedAt), ent.Asc(enttask.FieldID)).
		Limit(ganttLimit + 1).
		All(ctx)
	if err != nil {
		return GanttData{}, fmt.Errorf("get gantt data: %w", err)
	}
	out := GanttData{Limit: ganttLimit}
	if len(tasks) > ganttLimit {
		tasks, out.Truncated = tasks[:ganttLimit], true
	}
	nodes := make([]cpNode, len(tasks))
	var links []cpLink
	for i, t := range tasks {
		nodes[i] = cpNode{ID: t.ID, Days: durationDays(t.StartDate, t.DueDate)}
		for _, d := range t.Edges.Dependencies {
			if finishToStart(d.DependencyType) {
				links = append(links, cpLink{From: d.DependsOnTaskID, To: t.ID})
			}
		}
	}
	critical, days := criticalPath(nodes, links)
	out.CriticalDays = days
	out.Tasks = make([]GanttTask, len(tasks))
	for i, t := range tasks {
		out.Tasks[i] = GanttTask{Task: t, Critical: critical[t.ID]}
	}
	return out, nil
}

// durationDays is a task's length for the critical path: due minus start plus one (both days
// count), one day when only one date is set, zero when it has none.
func durationDays(start *time.Time, due time.Time) int {
	hasStart := start != nil && !start.IsZero()
	switch {
	case hasStart && !due.IsZero():
		d := int(dayOf(due).Sub(dayOf(*start)).Hours()/24) + 1
		if d < 1 {
			d = 1
		}
		return d
	case hasStart || !due.IsZero():
		return 1
	}
	return 0
}

// cpNode is a task in the critical path computation; cpLink a finish-to-start link From must
// finish before To starts.
type cpNode struct {
	ID   uuid.UUID
	Days int
}

type cpLink struct {
	From, To uuid.UUID
}

// criticalPath runs the critical path method over the tasks: a forward pass gives each task its
// earliest finish (the longest chain of durations leading to it), a backward pass its latest
// finish that keeps the whole plan's length, and a task with no slack and a non-zero duration is
// critical. Links to tasks outside the set are ignored, and tasks caught in a dependency cycle
// (which the API refuses, but older data may hold) are left out. It returns the critical tasks and
// the plan's length in days.
func criticalPath(nodes []cpNode, links []cpLink) (map[uuid.UUID]bool, int) {
	days := make(map[uuid.UUID]int, len(nodes))
	for _, n := range nodes {
		days[n.ID] = n.Days
	}
	preds := map[uuid.UUID][]uuid.UUID{}
	succs := map[uuid.UUID][]uuid.UUID{}
	indeg := make(map[uuid.UUID]int, len(nodes))
	seen := map[cpLink]bool{}
	for _, l := range links {
		if _, ok := days[l.From]; !ok {
			continue
		}
		if _, ok := days[l.To]; !ok || l.From == l.To || seen[l] {
			continue
		}
		seen[l] = true
		preds[l.To] = append(preds[l.To], l.From)
		succs[l.From] = append(succs[l.From], l.To)
		indeg[l.To]++
	}
	// Kahn's order, stable in input order.
	order := make([]uuid.UUID, 0, len(nodes))
	queue := make([]uuid.UUID, 0, len(nodes))
	for _, n := range nodes {
		if indeg[n.ID] == 0 {
			queue = append(queue, n.ID)
		}
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		order = append(order, id)
		for _, s := range succs[id] {
			indeg[s]--
			if indeg[s] == 0 {
				queue = append(queue, s)
			}
		}
	}
	inOrder := make(map[uuid.UUID]bool, len(order))
	for _, id := range order {
		inOrder[id] = true
	}
	ef := make(map[uuid.UUID]int, len(order))
	finish := 0
	for _, id := range order {
		es := 0
		for _, p := range preds[id] {
			if inOrder[p] && ef[p] > es {
				es = ef[p]
			}
		}
		ef[id] = es + days[id]
		if ef[id] > finish {
			finish = ef[id]
		}
	}
	lf := make(map[uuid.UUID]int, len(order))
	for i := len(order) - 1; i >= 0; i-- {
		id := order[i]
		latest := finish
		for _, s := range succs[id] {
			if inOrder[s] {
				if ls := lf[s] - days[s]; ls < latest {
					latest = ls
				}
			}
		}
		lf[id] = latest
	}
	critical := map[uuid.UUID]bool{}
	if finish == 0 {
		return critical, 0
	}
	for _, id := range order {
		if days[id] > 0 && lf[id] == ef[id] {
			critical[id] = true
		}
	}
	return critical, finish
}
