package planning

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	entmember "github.com/bengobox/projects-service/internal/ent/projectmember"
	entsetting "github.com/bengobox/projects-service/internal/ent/tenantsetting"
	"github.com/bengobox/projects-service/internal/modules/rbac"
	"github.com/bengobox/projects-service/internal/platform/events"
	"github.com/bengobox/projects-service/internal/services/people"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// Workload (sprint 16). Demand is each open task's estimate spread evenly over the working days
// (Monday to Friday) from its start date to its due date (a task with only a due date is a
// one-day task; a task spanning only a weekend spreads over those days), split equally between
// its assignees, and summed per person and ISO week, all in one SQL statement. Capacity is per
// person and week: ERP has no per-employee working hours read yet (its shifts carry
// total_hours_per_week but are not assigned to employees), so capacity comes from the tenant's
// projects settings: weekly_hours for everyone (40 when unset) and person_hours overrides.
const (
	DefaultWeeklyHours = 40.0
	MaxWeeks           = 26
	defaultWeeks       = 8
	maxWorkloadPeople  = 500
	maxPersonOverrides = 2000
	// settingsWorkload is the tenant_settings key of the workload settings.
	settingsWorkload = "workload"
)

// EventWorkloadSettings is written when the capacity settings change (subject
// project.workload.settings_updated).
const EventWorkloadSettings = "workload.settings_updated"

// WorkloadSettings is the tenant's capacity configuration.
type WorkloadSettings struct {
	WeeklyHours    float64               `json:"weekly_hours"`
	PersonHours    map[uuid.UUID]float64 `json:"person_hours"`
	CapacitySource string                `json:"capacity_source"`
}

// CapacityOf is a person's weekly capacity.
func (w WorkloadSettings) CapacityOf(id uuid.UUID) float64 {
	if h, ok := w.PersonHours[id]; ok {
		return h
	}
	return w.WeeklyHours
}

// workloadSettingsFrom reads the workload settings out of a tenant_settings document.
func workloadSettingsFrom(settings map[string]any) WorkloadSettings {
	out := WorkloadSettings{WeeklyHours: DefaultWeeklyHours, PersonHours: map[uuid.UUID]float64{}, CapacitySource: "settings"}
	m, _ := settings[settingsWorkload].(map[string]any)
	if h, ok := m["weekly_hours"].(float64); ok && h >= 0 {
		out.WeeklyHours = h
	}
	if ph, ok := m["person_hours"].(map[string]any); ok {
		for k, v := range ph {
			id, err := uuid.Parse(k)
			h, isNum := v.(float64)
			if err == nil && isNum && h >= 0 {
				out.PersonHours[id] = h
			}
		}
	}
	return out
}

// GetWorkloadSettings returns the tenant's capacity settings (the defaults when none are saved).
func (s *Service) GetWorkloadSettings(ctx context.Context, tenantID uuid.UUID) (WorkloadSettings, error) {
	row, err := s.client.TenantSetting.Query().Where(entsetting.TenantID(tenantID)).Only(ctx)
	if ent.IsNotFound(err) {
		return workloadSettingsFrom(nil), nil
	}
	if err != nil {
		return WorkloadSettings{}, fmt.Errorf("load settings: %w", err)
	}
	return workloadSettingsFrom(row.Settings), nil
}

// SetWorkloadSettings replaces the capacity settings: weekly_hours 0-168 and per person overrides
// in the same range.
func (s *Service) SetWorkloadSettings(ctx context.Context, tenantID uuid.UUID, in WorkloadSettings) (WorkloadSettings, error) {
	if in.WeeklyHours < 0 || in.WeeklyHours > 168 || math.IsNaN(in.WeeklyHours) {
		return WorkloadSettings{}, ValidationError("weekly_hours must be between 0 and 168")
	}
	if len(in.PersonHours) > maxPersonOverrides {
		return WorkloadSettings{}, ValidationError(fmt.Sprintf("at most %d personal capacities", maxPersonOverrides))
	}
	ph := make(map[string]any, len(in.PersonHours))
	for id, h := range in.PersonHours {
		if h < 0 || h > 168 || math.IsNaN(h) {
			return WorkloadSettings{}, ValidationError("every personal capacity must be between 0 and 168 hours")
		}
		ph[id.String()] = h
	}
	doc := map[string]any{"weekly_hours": in.WeeklyHours, "person_hours": ph}
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		row, err := tx.TenantSetting.Query().Where(entsetting.TenantID(tenantID)).Only(ctx)
		switch {
		case ent.IsNotFound(err):
			err = tx.TenantSetting.Create().SetTenantID(tenantID).SetSettings(map[string]any{settingsWorkload: doc}).Exec(ctx)
		case err == nil:
			settings := make(map[string]any, len(row.Settings)+1)
			for k, v := range row.Settings {
				settings[k] = v
			}
			settings[settingsWorkload] = doc
			err = row.Update().SetSettings(settings).Exec(ctx)
		}
		if err != nil {
			return fmt.Errorf("save settings: %w", err)
		}
		return events.Write(ctx, tx, events.AggregateProject, EventWorkloadSettings, tenantID, tenantID, map[string]any{
			"weekly_hours": in.WeeklyHours, "person_overrides": len(ph), "actor_id": actor(ctx),
		})
	})
	if err != nil {
		return WorkloadSettings{}, err
	}
	in.CapacitySource = "settings"
	if in.PersonHours == nil {
		in.PersonHours = map[uuid.UUID]float64{}
	}
	return in, nil
}

// WorkloadQuery is one workload read.
type WorkloadQuery struct {
	From  time.Time // any day; moved back to its Monday
	Weeks int       // 1..MaxWeeks, default 8
	// ProjectID limits the people to that project's team (owner and staff members). Their demand
	// still counts every project the caller may see, which is what shows an overload.
	ProjectID *uuid.UUID
	// VisibleTo limits demand to the projects that user may see (nil: every project).
	VisibleTo   *uuid.UUID
	IncludeDone bool
}

// WeekLoad is one person's week.
type WeekLoad struct {
	WeekStart  string  `json:"week_start"`
	Demand     float64 `json:"demand_hours"`
	Capacity   float64 `json:"capacity_hours"`
	LoadPct    float64 `json:"load_pct"`
	Overloaded bool    `json:"overloaded"`
	Tasks      int     `json:"task_count"`
}

// PersonLoad is one person's row.
type PersonLoad struct {
	UserID          uuid.UUID  `json:"user_id"`
	Name            string     `json:"name"`
	Email           string     `json:"email"`
	WeeklyCapacity  float64    `json:"weekly_capacity"`
	TotalDemand     float64    `json:"total_demand"`
	OverloadedWeeks int        `json:"overloaded_weeks"`
	Weeks           []WeekLoad `json:"weeks"`
}

// UnassignedWeek is demand from tasks nobody is assigned to.
type UnassignedWeek struct {
	WeekStart string  `json:"week_start"`
	Demand    float64 `json:"demand_hours"`
	Tasks     int     `json:"task_count"`
}

// Workload is the whole grid.
type Workload struct {
	From           string           `json:"from"`
	To             string           `json:"to"`
	Weeks          []string         `json:"weeks"`
	DefaultWeekly  float64          `json:"default_weekly_hours"`
	CapacitySource string           `json:"capacity_source"`
	People         []PersonLoad     `json:"people"`
	Unassigned     []UnassignedWeek `json:"unassigned"`
	Truncated      bool             `json:"truncated"`
}

// DemandRow is one (person, week) sum from SQL; UserID nil is unassigned work.
type DemandRow struct {
	UserID *uuid.UUID
	Week   time.Time
	Hours  float64
	Tasks  int
}

// MondayOf returns the Monday (UTC midnight) of t's ISO week.
func MondayOf(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	day := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	offset := (int(day.Weekday()) + 6) % 7
	return day.AddDate(0, 0, -offset)
}

// WeekRange normalises a query to whole ISO weeks: the Monday of from, weeks (default 8, at most
// MaxWeeks), and the last day (a Sunday).
func WeekRange(from time.Time, weeks int) (time.Time, time.Time, int, error) {
	if weeks == 0 {
		weeks = defaultWeeks
	}
	if weeks < 1 || weeks > MaxWeeks {
		return time.Time{}, time.Time{}, 0, ValidationError(fmt.Sprintf("weeks must be between 1 and %d", MaxWeeks))
	}
	start := MondayOf(from)
	return start, start.AddDate(0, 0, 7*weeks-1), weeks, nil
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// BuildWorkload turns the SQL rows into the grid: one row per person (team first when a team is
// given, then everyone else with demand), every week present, load against capacity, and weeks
// over 100 percent marked overloaded. A person with zero capacity and some demand is overloaded.
func BuildWorkload(rows []DemandRow, team []uuid.UUID, start time.Time, weeks int, settings WorkloadSettings, limitToTeam bool) Workload {
	out := Workload{From: start.Format(time.DateOnly), To: start.AddDate(0, 0, 7*weeks-1).Format(time.DateOnly),
		DefaultWeekly: settings.WeeklyHours, CapacitySource: settings.CapacitySource,
		People: []PersonLoad{}, Unassigned: []UnassignedWeek{}}
	index := map[string]int{}
	for i := 0; i < weeks; i++ {
		k := start.AddDate(0, 0, 7*i).Format(time.DateOnly)
		out.Weeks = append(out.Weeks, k)
		index[k] = i
	}
	type cell struct {
		hours float64
		tasks int
	}
	byPerson := map[uuid.UUID][]cell{}
	unassigned := make([]cell, weeks)
	order := append([]uuid.UUID{}, team...)
	inTeam := map[uuid.UUID]bool{}
	for _, id := range team {
		inTeam[id] = true
	}
	var others []uuid.UUID
	for _, r := range rows {
		i, ok := index[MondayOf(r.Week).Format(time.DateOnly)]
		if !ok {
			continue
		}
		if r.UserID == nil {
			unassigned[i].hours += r.Hours
			unassigned[i].tasks += r.Tasks
			continue
		}
		id := *r.UserID
		if limitToTeam && !inTeam[id] {
			continue
		}
		if _, seen := byPerson[id]; !seen {
			byPerson[id] = make([]cell, weeks)
			if !inTeam[id] {
				others = append(others, id)
			}
		}
		byPerson[id][i].hours += r.Hours
		byPerson[id][i].tasks += r.Tasks
	}
	sort.Slice(others, func(a, b int) bool { return others[a].String() < others[b].String() })
	order = append(order, others...)
	if len(order) > maxWorkloadPeople {
		order, out.Truncated = order[:maxWorkloadPeople], true
	}
	for _, id := range order {
		cells := byPerson[id]
		if cells == nil {
			cells = make([]cell, weeks)
		}
		capacity := settings.CapacityOf(id)
		p := PersonLoad{UserID: id, WeeklyCapacity: capacity, Weeks: make([]WeekLoad, weeks)}
		for i, c := range cells {
			w := WeekLoad{WeekStart: out.Weeks[i], Demand: round2(c.hours), Capacity: capacity, Tasks: c.tasks}
			switch {
			case capacity > 0:
				w.LoadPct = round2(c.hours / capacity * 100)
				w.Overloaded = w.LoadPct > 100
			case c.hours > 0:
				w.LoadPct, w.Overloaded = 100, true
			}
			if w.Overloaded {
				p.OverloadedWeeks++
			}
			p.TotalDemand += c.hours
			p.Weeks[i] = w
		}
		p.TotalDemand = round2(p.TotalDemand)
		out.People = append(out.People, p)
	}
	for i, c := range unassigned {
		if c.tasks > 0 {
			out.Unassigned = append(out.Unassigned, UnassignedWeek{WeekStart: out.Weeks[i], Demand: round2(c.hours), Tasks: c.tasks})
		}
	}
	return out
}

// demandCTE spreads every open, estimated, dated task of the tenant that overlaps [$2, $3] over
// its working days and splits it between its assignees. $1 tenant, $2 first day, $3 last day,
// $4 include done tasks, $5 visible-to user (NULL: every project). The working day count is the
// closed form 5*(n/7) plus the weekdays of the partial week, so a long task is not expanded day
// by day beyond the requested range.
const demandCTE = `
WITH t AS (
	SELECT t.id, t.estimated_hours AS est,
		LEAST(COALESCE(t.start_date, t.due_date), t.due_date)::date AS s,
		t.due_date::date AS e,
		GREATEST((SELECT count(*) FROM task_assignees a WHERE a.tenant_id = $1 AND a.task_id = t.id), 1) AS n
	FROM tasks t
	JOIN projects p ON p.id = t.project_id AND p.tenant_id = $1
	WHERE t.tenant_id = $1 AND t.estimated_hours > 0 AND t.due_date IS NOT NULL
		AND ($4::boolean OR t.status <> 'done')
		AND LEAST(COALESCE(t.start_date, t.due_date), t.due_date)::date <= $3::date
		AND t.due_date::date >= $2::date
		AND ($5::uuid IS NULL OR p.owner_id = $5::uuid OR EXISTS (
			SELECT 1 FROM project_members m WHERE m.tenant_id = $1 AND m.project_id = p.id AND m.user_id = $5::uuid
				AND m.left_at IS NULL AND m.role_code <> 'client'))
), w AS (
	SELECT id, est, n, s, e, (e - s + 1) AS days, extract(isodow FROM s)::int - 1 AS k FROM t
), d AS (
	SELECT id, est, n, s, e, days,
		5 * (days / 7)
		+ GREATEST(0, LEAST(k + days % 7 - 1, 4) - k + 1)
		+ GREATEST(0, LEAST(k + days % 7 - 1, 11) - GREATEST(k, 7) + 1) AS wd
	FROM w
), daily AS (
	SELECT d.id, d.n, g.day::date AS day, d.est / (CASE WHEN d.wd = 0 THEN d.days ELSE d.wd END) AS h
	FROM d, generate_series(GREATEST(d.s, $2::date), LEAST(d.e, $3::date), interval '1 day') AS g(day)
	WHERE d.wd = 0 OR extract(isodow FROM g.day) < 6
)`

// demandQuery sums the spread demand per person (NULL: unassigned) and ISO week.
const demandQuery = demandCTE + `
SELECT a.user_id, date_trunc('week', daily.day)::date AS week, sum(daily.h / daily.n) AS hours, count(DISTINCT daily.id) AS tasks
FROM daily
LEFT JOIN task_assignees a ON a.tenant_id = $1 AND a.task_id = daily.id
GROUP BY a.user_id, date_trunc('week', daily.day)
ORDER BY a.user_id, week`

func nullableUUID(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return id.String()
}

// Workload returns the capacity against demand grid.
func (s *Service) Workload(ctx context.Context, tenantID uuid.UUID, q WorkloadQuery) (Workload, error) {
	start, end, weeks, err := WeekRange(q.From, q.Weeks)
	if err != nil {
		return Workload{}, err
	}
	settings, err := s.GetWorkloadSettings(ctx, tenantID)
	if err != nil {
		return Workload{}, err
	}
	rows, err := s.client.QueryContext(ctx, demandQuery, tenantID, start, end, q.IncludeDone, nullableUUID(q.VisibleTo))
	if err != nil {
		return Workload{}, fmt.Errorf("workload demand: %w", err)
	}
	var demand []DemandRow
	for rows.Next() {
		var r DemandRow
		var uid uuid.NullUUID
		if err := rows.Scan(&uid, &r.Week, &r.Hours, &r.Tasks); err != nil {
			_ = rows.Close()
			return Workload{}, fmt.Errorf("workload demand: %w", err)
		}
		if uid.Valid {
			id := uid.UUID
			r.UserID = &id
		}
		demand = append(demand, r)
	}
	if err := rows.Close(); err != nil {
		return Workload{}, err
	}
	var team []uuid.UUID
	if q.ProjectID != nil {
		if team, err = s.team(ctx, tenantID, *q.ProjectID); err != nil {
			return Workload{}, err
		}
	}
	out := BuildWorkload(demand, team, start, weeks, settings, q.ProjectID != nil)
	ids := make([]uuid.UUID, len(out.People))
	for i, p := range out.People {
		ids[i] = p.UserID
	}
	known, err := people.Lookup(ctx, s.client.ProjectsUser, tenantID, ids)
	if err != nil {
		return Workload{}, err
	}
	for i := range out.People {
		who := known[out.People[i].UserID]
		out.People[i].Name, out.People[i].Email = who.Name, who.Email
	}
	return out, nil
}

// team is a project's owner and active staff members (no clients), owner first.
func (s *Service) team(ctx context.Context, tenantID, projectID uuid.UUID) ([]uuid.UUID, error) {
	p, err := s.project(ctx, s.client.Project, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	members, err := s.client.ProjectMember.Query().
		Where(entmember.TenantID(tenantID), entmember.ProjectID(projectID), entmember.LeftAtIsNil(), entmember.RoleCodeNEQ(rbac.RoleClient)).
		Order(ent.Asc(entmember.FieldJoinedAt)).Select(entmember.FieldUserID).Limit(maxWorkloadPeople).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load team: %w", err)
	}
	out := []uuid.UUID{p.OwnerID}
	for _, m := range members {
		if m.UserID != p.OwnerID {
			out = append(out, m.UserID)
		}
	}
	return out, nil
}

// WorkloadTask is one task behind a person's week, for the reassign drawer.
type WorkloadTask struct {
	TaskID      uuid.UUID `json:"task_id"`
	Title       string    `json:"title"`
	ProjectID   uuid.UUID `json:"project_id"`
	ProjectName string    `json:"project_name"`
	Status      string    `json:"status"`
	DueDate     string    `json:"due_date"`
	Hours       float64   `json:"hours"`
}

// workloadTasksQuery lists the tasks behind one person's week ($6 user, NULL: unassigned), the
// largest share first, at most 100.
const workloadTasksQuery = demandCTE + `
SELECT daily.id, tk.title, tk.project_id, p.name, tk.status, tk.due_date::date, sum(daily.h / daily.n) AS hours
FROM daily
JOIN tasks tk ON tk.id = daily.id
JOIN projects p ON p.id = tk.project_id
LEFT JOIN task_assignees a ON a.tenant_id = $1 AND a.task_id = daily.id
WHERE (($6::uuid IS NULL AND a.user_id IS NULL) OR a.user_id = $6::uuid)
GROUP BY daily.id, tk.title, tk.project_id, p.name, tk.status, tk.due_date
ORDER BY hours DESC, daily.id
LIMIT 100`

// WorkloadTasks lists the tasks making up one person's (nil: unassigned) demand in one week.
func (s *Service) WorkloadTasks(ctx context.Context, tenantID uuid.UUID, userID *uuid.UUID, week time.Time, visibleTo *uuid.UUID, includeDone bool) ([]WorkloadTask, error) {
	start := MondayOf(week)
	rows, err := s.client.QueryContext(ctx, workloadTasksQuery, tenantID, start, start.AddDate(0, 0, 6), includeDone, nullableUUID(visibleTo), nullableUUID(userID))
	if err != nil {
		return nil, fmt.Errorf("workload tasks: %w", err)
	}
	defer rows.Close()
	out := []WorkloadTask{}
	for rows.Next() {
		var t WorkloadTask
		var due time.Time
		if err := rows.Scan(&t.TaskID, &t.Title, &t.ProjectID, &t.ProjectName, &t.Status, &due, &t.Hours); err != nil {
			return nil, fmt.Errorf("workload tasks: %w", err)
		}
		t.DueDate, t.Hours = due.Format(time.DateOnly), round2(t.Hours)
		out = append(out, t)
	}
	return out, rows.Err()
}
