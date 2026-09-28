package financials

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	"github.com/bengobox/projects-service/internal/platform/erp"
	"github.com/bengobox/projects-service/internal/platform/treasury"
)

// ErrNotFound is returned for a project the tenant does not have.
var ErrNotFound = errors.New("project not found")

// maxPortfolioPage bounds one portfolio page (and so one treasury batch).
const maxPortfolioPage = 100

// Service builds project financials.
type Service struct {
	client   *ent.Client
	treasury *treasury.Client
	erp      *erp.Client
	log      *zap.Logger
	now      func() time.Time
}

// WithERP wires timesheet hours for utilisation. Optional: without it hours are not reported.
func (s *Service) WithERP(c *erp.Client) *Service {
	s.erp = c
	return s
}

// Hours compares the time logged in ERP timesheets with the tasks' estimates.
type Hours struct {
	Estimated float64 `json:"estimated"`
	Logged    float64 `json:"logged"`  // approved timesheet hours
	Pending   float64 `json:"pending"` // submitted, awaiting approval
	// UtilisationPct is logged over estimated; nil without estimates.
	UtilisationPct *float64 `json:"utilisation_pct"`
}

// projectHours fetches logged hours for a page of projects; nil when ERP is not wired or fails
// (the rest of the financials still render).
func (s *Service) projectHours(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) map[uuid.UUID]erp.Hours {
	if !s.erp.Enabled() {
		return nil
	}
	h, err := s.erp.ProjectHours(ctx, tenantID, ids)
	if err != nil {
		s.log.Warn("erp project hours unavailable", zap.Error(err))
		return nil
	}
	return h
}

// hoursSummary builds Hours from the tasks' estimated hours and the logged time.
func hoursSummary(estimated float64, logged *erp.Hours) *Hours {
	h := &Hours{Estimated: estimated}
	if logged == nil {
		return h
	}
	h.Logged, h.Pending = logged.ApprovedHours, logged.SubmittedHours
	if h.Estimated > 0 {
		u := math.Round(h.Logged/h.Estimated*1000) / 10
		h.UtilisationPct = &u
	}
	return h
}

func NewService(client *ent.Client, tc *treasury.Client, log *zap.Logger) *Service {
	return &Service{client: client, treasury: tc, log: log.Named("financials.svc"), now: time.Now}
}

// Treasury exposes the client to the budget proxy handler.
func (s *Service) Treasury() *treasury.Client { return s.treasury }

// ProjectFinancials is the financial picture of one project.
type ProjectFinancials struct {
	ProjectID    uuid.UUID                   `json:"project_id"`
	Name         string                      `json:"name"`
	Status       string                      `json:"status"`
	StartDate    *time.Time                  `json:"start_date,omitempty"`
	EndDate      *time.Time                  `json:"end_date,omitempty"`
	Currency     string                      `json:"currency"`
	Money        *treasury.ProjectFinancials `json:"money,omitempty"`
	MoneyError   string                      `json:"money_error,omitempty"`
	EVM          EVM                         `json:"evm"`
	TasksTotal   int                         `json:"tasks_total"`
	TasksDone    int                         `json:"tasks_done"`
	TasksOverdue int                         `json:"tasks_overdue"`
	Hours        *Hours                      `json:"hours,omitempty"`
}

func (s *Service) build(p *ent.Project, stats taskStats, money *treasury.ProjectFinancials, moneyErr error, hours map[uuid.UUID]erp.Hours) *ProjectFinancials {
	pf := &ProjectFinancials{
		ProjectID: p.ID, Name: p.Name, Status: p.Status, Currency: p.Currency,
		Money: money,
	}
	if !p.StartDate.IsZero() {
		sd := p.StartDate
		pf.StartDate = &sd
	}
	if !p.EndDate.IsZero() {
		ed := p.EndDate
		pf.EndDate = &ed
	}
	if moneyErr != nil {
		pf.MoneyError = "treasury unavailable"
	}
	pf.TasksTotal, pf.TasksDone, pf.TasksOverdue = stats.Total, stats.Done, stats.Overdue
	bac, ac := 0.0, 0.0
	if money != nil {
		bac, ac = float64(money.BudgetCost), float64(money.ActualCost)
		if money.Currency != "" {
			pf.Currency = money.Currency
		}
	}
	pf.EVM = EVMFromWeights(bac, ac, stats.weights())
	if hours != nil {
		var logged *erp.Hours
		if h, ok := hours[p.ID]; ok {
			logged = &h
		}
		pf.Hours = hoursSummary(stats.EstSum, logged)
	}
	return pf
}

// Project returns the financials of one project.
func (s *Service) Project(ctx context.Context, tenantID, projectID uuid.UUID) (*ProjectFinancials, error) {
	p, err := s.client.Project.Query().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	now := s.now()
	stats, err := s.loadTaskStats(ctx, tenantID, []uuid.UUID{projectID}, now)
	if err != nil {
		return nil, err
	}
	money, merr := s.treasury.ProjectsFinancials(ctx, tenantID, []uuid.UUID{projectID})
	var m *treasury.ProjectFinancials
	if merr == nil {
		if f, ok := money[projectID]; ok {
			m = &f
		}
	} else {
		s.log.Warn("treasury financials unavailable", zap.String("project", projectID.String()), zap.Error(merr))
	}
	return s.build(p, stats[projectID], m, merr, s.projectHours(ctx, tenantID, []uuid.UUID{projectID})), nil
}

// PortfolioFilter narrows the portfolio.
type PortfolioFilter struct {
	Status string
	Limit  int
	Offset int
}

// Portfolio returns one page of projects with their financial health: one project query, one
// task query and one treasury batch per page, however many projects the tenant has.
func (s *Service) Portfolio(ctx context.Context, tenantID uuid.UUID, f PortfolioFilter) ([]*ProjectFinancials, int, error) {
	if f.Limit <= 0 || f.Limit > maxPortfolioPage {
		f.Limit = maxPortfolioPage
	}
	q := s.client.Project.Query().Where(entproject.TenantID(tenantID))
	if f.Status != "" {
		q = q.Where(entproject.Status(f.Status))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	projects, err := q.Order(ent.Desc(entproject.FieldCreatedAt)).Limit(f.Limit).Offset(f.Offset).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	if len(projects) == 0 {
		return []*ProjectFinancials{}, total, nil
	}
	ids := make([]uuid.UUID, len(projects))
	for i, p := range projects {
		ids[i] = p.ID
	}
	now := s.now()
	stats, err := s.loadTaskStats(ctx, tenantID, ids, now)
	if err != nil {
		return nil, 0, err
	}
	money, merr := s.treasury.ProjectsFinancials(ctx, tenantID, ids)
	if merr != nil {
		s.log.Warn("treasury portfolio financials unavailable", zap.Error(merr))
	}
	hours := s.projectHours(ctx, tenantID, ids)
	out := make([]*ProjectFinancials, len(projects))
	for i, p := range projects {
		var m *treasury.ProjectFinancials
		if f, ok := money[p.ID]; ok {
			m = &f
		}
		out[i] = s.build(p, stats[p.ID], m, merr, hours)
	}
	return out, total, nil
}

// ProjectExists reports whether the tenant has the project (a cheap check before budget calls).
func (s *Service) ProjectExists(ctx context.Context, tenantID, projectID uuid.UUID) (bool, error) {
	return s.client.Project.Query().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).Exist(ctx)
}
