package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Bengo-Hub/pagination"
	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/http/authz"
	"github.com/bengobox/projects-service/internal/modules/rbac"
	"github.com/bengobox/projects-service/internal/services/planning"
	"github.com/bengobox/projects-service/internal/services/projects"
	"github.com/bengobox/projects-service/internal/services/tasks"
)

// PlanningHandler serves planning depth (sprint 16): workload, baselines and slips, the risk
// register, change requests, project templates, status rules and automations. Every route under
// /projects/{projectID} goes through projectScope (membership scoping) and is a flat pattern.
type PlanningHandler struct {
	log   *zap.Logger
	svc   *planning.Service
	tasks *tasks.Service
}

func NewPlanningHandler(log *zap.Logger, svc *planning.Service, taskSvc *tasks.Service) *PlanningHandler {
	return &PlanningHandler{log: log.Named("planning.handler"), svc: svc, tasks: taskSvc}
}

// PlanningGates are the plan features of the planning routes.
type PlanningGates struct {
	Tracking, Gantt, Workload, Automation func(http.Handler) http.Handler
}

// RegisterRoutes mounts the routes. Workload is gated on workload_planning; baselines on
// task_tracking and gantt_chart; status rules and automations on task_tracking and
// project_automation; risks, change requests and templates need only the module.
func (h *PlanningHandler) RegisterRoutes(r chi.Router, g PlanningGates) {
	req := authz.Require
	r.Group(func(w chi.Router) {
		w.Use(g.Workload)
		w.With(req(rbac.PermWorkloadView)).Get("/workload", h.Workload)
		w.With(req(rbac.PermWorkloadView)).Get("/workload/tasks", h.WorkloadTasks)
		w.With(req(rbac.PermWorkloadView)).Get("/workload/settings", h.GetWorkloadSettings)
		w.With(req(rbac.PermWorkloadManage)).Put("/workload/settings", h.SetWorkloadSettings)
	})
	r.Group(func(b chi.Router) {
		b.Use(g.Tracking, g.Gantt)
		b.With(req(rbac.PermBaselinesView)).Get("/projects/{projectID}/baselines", h.ListBaselines)
		b.With(req(rbac.PermBaselinesAdd)).Post("/projects/{projectID}/baselines", h.SaveBaseline)
		b.With(req(rbac.PermBaselinesView)).Get("/projects/{projectID}/baselines/{baselineID}", h.GetBaseline)
		b.With(req(rbac.PermBaselinesDelete)).Delete("/projects/{projectID}/baselines/{baselineID}", h.DeleteBaseline)
		b.With(req(rbac.PermBaselinesView)).Get("/projects/{projectID}/slips", h.Slips)
	})
	r.Group(func(a chi.Router) {
		a.Use(g.Tracking, g.Automation)
		a.With(req(rbac.PermTasksView, rbac.PermTasksViewOwn)).Get("/projects/{projectID}/status-rules", h.GetStatusRules)
		a.With(req(rbac.PermAutomationManage)).Put("/projects/{projectID}/status-rules", h.SetStatusRules)
		a.With(req(rbac.PermTasksView, rbac.PermTasksViewOwn)).Get("/projects/{projectID}/task-moves/{taskID}", h.TaskMoves)
		a.With(req(rbac.PermAutomationView)).Get("/projects/{projectID}/automations", h.GetAutomations)
		a.With(req(rbac.PermAutomationManage)).Put("/projects/{projectID}/automations", h.SetAutomations)
	})
	r.With(req(rbac.PermRisksView)).Get("/projects/{projectID}/risks", h.ListRisks)
	r.With(req(rbac.PermRisksAdd)).Post("/projects/{projectID}/risks", h.CreateRisk)
	r.With(req(rbac.PermRisksView)).Get("/projects/{projectID}/risks/heatmap", h.RiskHeatMap)
	r.With(req(rbac.PermRisksView)).Get("/projects/{projectID}/risks/{riskID}", h.GetRisk)
	r.With(req(rbac.PermRisksChange)).Put("/projects/{projectID}/risks/{riskID}", h.UpdateRisk)
	r.With(req(rbac.PermRisksDelete)).Delete("/projects/{projectID}/risks/{riskID}", h.DeleteRisk)

	crView := req(rbac.PermChangeRequestsView, rbac.PermChangeApprovals)
	r.With(crView).Get("/change-requests", h.ChangeRequestQueue)
	r.With(crView).Get("/projects/{projectID}/change-requests", h.ListChangeRequests)
	r.With(req(rbac.PermChangeRequestsAdd)).Post("/projects/{projectID}/change-requests", h.CreateChangeRequest)
	r.With(crView).Get("/projects/{projectID}/change-requests/{crID}", h.GetChangeRequest)
	r.With(req(rbac.PermChangeRequestsAdd)).Put("/projects/{projectID}/change-requests/{crID}", h.UpdateChangeRequest)
	r.With(req(rbac.PermChangeRequestsAdd)).Post("/projects/{projectID}/change-requests/{crID}/withdraw", h.WithdrawChangeRequest)
	r.With(req(rbac.PermChangeApprovals)).Post("/projects/{projectID}/change-requests/{crID}/approve", h.ApproveChangeRequest)
	r.With(req(rbac.PermChangeApprovals)).Post("/projects/{projectID}/change-requests/{crID}/reject", h.RejectChangeRequest)
	r.With(req(rbac.PermChangeApprovals)).Post("/projects/{projectID}/change-requests/{crID}/push-budget", h.PushBudget)

	r.With(req(rbac.PermTemplatesView)).Get("/project-templates", h.ListTemplates)
	r.With(req(rbac.PermTemplatesView)).Get("/project-templates/{templateID}", h.GetTemplate)
	r.With(req(rbac.PermTemplatesDelete)).Delete("/project-templates/{templateID}", h.DeleteTemplate)
	r.With(req(rbac.PermProjectsAdd), req(rbac.PermTemplatesView)).Post("/project-templates/{templateID}/projects", h.CreateFromTemplate)
	r.With(req(rbac.PermTemplatesAdd)).Post("/projects/{projectID}/save-as-template", h.SaveAsTemplate)
}

// FeatureOn reports whether the caller's plan has a feature (gating-exempt callers and service
// calls without claims always do).
func FeatureOn(r *http.Request, code string) bool {
	c, ok := authclient.ClaimsFromContext(r.Context())
	return !ok || c == nil || c.IsGatingExempt() || c.HasFeature(code)
}

// taskWriteCtx is the context of a task write: the notice slug, and whether the project's status
// rules and automations apply (the project_automation feature).
func taskWriteCtx(r *http.Request) context.Context {
	return tasks.WithPlanningRules(noticeCtx(r), FeatureOn(r, "project_automation"))
}

// fail maps a planning or tasks error to a response with a code the UI can act on.
func (h *PlanningHandler) fail(w http.ResponseWriter, err error, msg string) {
	var verr planning.ValidationError
	var tverr tasks.ValidationError
	var pverr projects.ValidationError
	var serr *planning.StateError
	var conflict *tasks.ScheduleConflict
	switch {
	case errors.Is(err, planning.ErrNotFound), errors.Is(err, tasks.ErrNotFound):
		respondJSON(w, http.StatusNotFound, map[string]any{"error": "not found", "code": "not_found"})
	case errors.As(err, &verr):
		respondJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": verr.Error(), "code": "validation_error"})
	case errors.As(err, &tverr):
		respondJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": tverr.Error(), "code": "validation_error"})
	case errors.As(err, &pverr):
		respondJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": pverr.Error(), "code": "validation_error"})
	case errors.As(err, &serr):
		respondJSON(w, http.StatusConflict, map[string]any{"error": serr.Msg, "code": serr.Code})
	case errors.As(err, &conflict):
		ids := make([]string, len(conflict.BlockingTaskIDs))
		for i, id := range conflict.BlockingTaskIDs {
			ids[i] = id.String()
		}
		respondJSON(w, http.StatusConflict, map[string]any{"error": conflict.Error(), "code": "schedule_conflict", "blocking_task_ids": ids})
	case errors.Is(err, planning.ErrOwnRequest):
		respondJSON(w, http.StatusForbidden, map[string]any{"error": err.Error(), "code": "own_change_request"})
	case errors.Is(err, planning.ErrNotYours):
		respondJSON(w, http.StatusForbidden, map[string]any{"error": err.Error(), "code": "not_requester"})
	case errors.Is(err, planning.ErrSystemTemplate):
		respondJSON(w, http.StatusForbidden, map[string]any{"error": err.Error(), "code": "system_template"})
	default:
		serverError(w, h.log, msg, err)
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

// optionalUUID reads an optional id query parameter.
func optionalUUID(w http.ResponseWriter, r *http.Request, name string) (*uuid.UUID, bool) {
	v := strings.TrimSpace(r.URL.Query().Get(name))
	if v == "" {
		return nil, true
	}
	id, err := uuid.Parse(v)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid "+name)
		return nil, false
	}
	return &id, true
}

// ---- Workload ----

// Workload returns capacity against demand per person and ISO week: ?from=YYYY-MM-DD (default
// this week), ?weeks=1..26 (default 8), ?project_id= (that project's team), ?include_done=true.
func (h *PlanningHandler) Workload(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	from := time.Now()
	if d, ok := parseDay(w, q.Get("from"), "from"); !ok {
		return
	} else if d != nil {
		from = *d
	}
	weeks := 0
	if v := q.Get("weeks"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid weeks")
			return
		}
		weeks = n
	}
	projectID, ok := optionalUUID(w, r, "project_id")
	if !ok {
		return
	}
	if projectID != nil {
		visible, err := accessOf(r).CanSeeProject(r.Context(), *projectID)
		if err != nil {
			serverError(w, h.log, "membership check failed", err)
			return
		}
		if !visible {
			respondError(w, http.StatusNotFound, "project not found")
			return
		}
	}
	out, err := h.svc.Workload(r.Context(), tenantID, planning.WorkloadQuery{
		From: from, Weeks: weeks, ProjectID: projectID, VisibleTo: accessOf(r).VisibleTo(), IncludeDone: q.Get("include_done") == "true",
	})
	if err != nil {
		h.fail(w, err, "workload failed")
		return
	}
	respondJSON(w, http.StatusOK, out)
}

// WorkloadTasks lists the tasks behind one cell: ?user_id=<id>|unassigned&week=YYYY-MM-DD.
func (h *PlanningHandler) WorkloadTasks(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	var user *uuid.UUID
	if v := q.Get("user_id"); v != "unassigned" {
		id, err := uuid.Parse(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "user_id must be a user id or unassigned")
			return
		}
		user = &id
	}
	week, ok := parseDay(w, q.Get("week"), "week")
	if !ok {
		return
	}
	if week == nil {
		respondError(w, http.StatusBadRequest, "week is required")
		return
	}
	out, err := h.svc.WorkloadTasks(r.Context(), tenantID, user, *week, accessOf(r).VisibleTo(), q.Get("include_done") == "true")
	if err != nil {
		h.fail(w, err, "workload tasks failed")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *PlanningHandler) GetWorkloadSettings(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	out, err := h.svc.GetWorkloadSettings(r.Context(), tenantID)
	if err != nil {
		h.fail(w, err, "workload settings failed")
		return
	}
	respondJSON(w, http.StatusOK, out)
}

// SetWorkloadSettings replaces the capacity settings: {weekly_hours, person_hours: {user_id: hours}}.
func (h *PlanningHandler) SetWorkloadSettings(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	var in planning.WorkloadSettings
	if !decode(w, r, &in) {
		return
	}
	out, err := h.svc.SetWorkloadSettings(r.Context(), tenantID, in)
	if err != nil {
		h.fail(w, err, "save workload settings failed")
		return
	}
	respondJSON(w, http.StatusOK, out)
}

// ---- Baselines ----

func (h *PlanningHandler) ListBaselines(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	out, err := h.svc.ListBaselines(r.Context(), tenantID, projectID)
	if err != nil {
		h.fail(w, err, "list baselines failed")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}

// SaveBaseline snapshots the plan: {name?, note?}.
func (h *PlanningHandler) SaveBaseline(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	var in struct {
		Name string `json:"name"`
		Note string `json:"note"`
	}
	if r.ContentLength != 0 && !decode(w, r, &in) {
		return
	}
	out, err := h.svc.SaveBaseline(r.Context(), tenantID, projectID, in.Name, in.Note)
	if err != nil {
		h.fail(w, err, "save baseline failed")
		return
	}
	respondJSON(w, http.StatusCreated, out)
}

func (h *PlanningHandler) GetBaseline(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "baselineID")
	if !ok {
		return
	}
	out, err := h.svc.GetBaseline(r.Context(), tenantID, projectID, &id)
	if err != nil {
		h.fail(w, err, "get baseline failed")
		return
	}
	respondJSON(w, http.StatusOK, out)
}

func (h *PlanningHandler) DeleteBaseline(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "baselineID")
	if !ok {
		return
	}
	if err := h.svc.DeleteBaseline(r.Context(), tenantID, projectID, id); err != nil {
		h.fail(w, err, "delete baseline failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Slips compares the plan with a baseline (?baseline_id=, default the latest).
func (h *PlanningHandler) Slips(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	id, ok := optionalUUID(w, r, "baseline_id")
	if !ok {
		return
	}
	out, err := h.svc.Slips(r.Context(), tenantID, projectID, id)
	if err != nil {
		h.fail(w, err, "slips report failed")
		return
	}
	respondJSON(w, http.StatusOK, out)
}

// ---- Status rules and automations ----

func (h *PlanningHandler) GetStatusRules(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	out, err := h.tasks.GetStatusRules(r.Context(), tenantID, projectID)
	if err != nil {
		h.fail(w, err, "get status rules failed")
		return
	}
	respondJSON(w, http.StatusOK, out)
}

// SetStatusRules replaces the rules: {transitions: [{from, to: []}], requirements: [{to, from?, fields: []}]}.
func (h *PlanningHandler) SetStatusRules(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	var in tasks.StatusRules
	if !decode(w, r, &in) {
		return
	}
	out, err := h.tasks.SetStatusRules(r.Context(), tenantID, projectID, in)
	if err != nil {
		h.fail(w, err, "set status rules failed")
		return
	}
	respondJSON(w, http.StatusOK, out)
}

// TaskMoves says, for every other status, whether the task may move there now and why not:
// {data: [{status, allowed, reason, missing_fields, message}]}.
func (h *PlanningHandler) TaskMoves(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	taskID, ok := uuidParam(w, r, "taskID")
	if !ok {
		return
	}
	if own := ownOnly(r, rbac.PermTasksView); own != nil {
		mine, err := h.tasks.IsAssignee(r.Context(), tenantID, projectID, taskID, *own)
		if err != nil {
			serverError(w, h.log, "task assignee check failed", err)
			return
		}
		if !mine {
			respondError(w, http.StatusNotFound, "task not found")
			return
		}
	}
	out, err := h.tasks.CheckMoves(r.Context(), tenantID, projectID, taskID, true)
	if err != nil {
		h.fail(w, err, "task moves failed")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *PlanningHandler) GetAutomations(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	out, err := h.tasks.GetAutomations(r.Context(), tenantID, projectID)
	if err != nil {
		h.fail(w, err, "get automations failed")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out, "limit": tasks.MaxAutomations})
}

// SetAutomations replaces the rule list: {automations: [...]}.
func (h *PlanningHandler) SetAutomations(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	var in struct {
		Automations []tasks.Automation `json:"automations"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := h.tasks.SetAutomations(r.Context(), tenantID, projectID, in.Automations)
	if err != nil {
		h.fail(w, err, "set automations failed")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out, "limit": tasks.MaxAutomations})
}

// ---- Risks ----

func (h *PlanningHandler) ListRisks(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	p := pagination.Parse(r)
	q := r.URL.Query()
	f := planning.RiskFilter{Status: q.Get("status"), Limit: p.Limit, Offset: p.Offset}
	if v := q.Get("min_score"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid min_score")
			return
		}
		f.MinScore = n
	}
	owner, ok := optionalUUID(w, r, "owner_id")
	if !ok {
		return
	}
	f.OwnerID = owner
	items, total, err := h.svc.ListRisks(r.Context(), tenantID, projectID, f)
	if err != nil {
		h.fail(w, err, "list risks failed")
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(items, total, p))
}

func (h *PlanningHandler) CreateRisk(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	var in planning.RiskInput
	if !decode(w, r, &in) {
		return
	}
	out, err := h.svc.CreateRisk(r.Context(), tenantID, projectID, in)
	if err != nil {
		h.fail(w, err, "create risk failed")
		return
	}
	respondJSON(w, http.StatusCreated, out)
}

func (h *PlanningHandler) GetRisk(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "riskID")
	if !ok {
		return
	}
	out, err := h.svc.GetRisk(r.Context(), tenantID, projectID, id)
	if err != nil {
		h.fail(w, err, "get risk failed")
		return
	}
	respondJSON(w, http.StatusOK, out)
}

func (h *PlanningHandler) UpdateRisk(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "riskID")
	if !ok {
		return
	}
	var in planning.RiskInput
	if !decode(w, r, &in) {
		return
	}
	out, err := h.svc.UpdateRisk(r.Context(), tenantID, projectID, id, in)
	if err != nil {
		h.fail(w, err, "update risk failed")
		return
	}
	respondJSON(w, http.StatusOK, out)
}

func (h *PlanningHandler) DeleteRisk(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "riskID")
	if !ok {
		return
	}
	if err := h.svc.DeleteRisk(r.Context(), tenantID, projectID, id); err != nil {
		h.fail(w, err, "delete risk failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RiskHeatMap counts risks per likelihood and impact (?status=all includes closed and occurred).
func (h *PlanningHandler) RiskHeatMap(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	out, err := h.svc.RiskHeatMap(r.Context(), tenantID, projectID, r.URL.Query().Get("status") == "all")
	if err != nil {
		h.fail(w, err, "risk heat map failed")
		return
	}
	respondJSON(w, http.StatusOK, out)
}

// ---- Change requests ----

// ChangeRequestQueue lists the tenant's requests across the projects the caller may see
// (?status=pending for the approvals queue), newest first, paged.
func (h *PlanningHandler) ChangeRequestQueue(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	p := pagination.Parse(r)
	items, total, err := h.svc.ListChangeRequests(r.Context(), tenantID, nil, planning.CRFilter{
		Status: r.URL.Query().Get("status"), VisibleTo: accessOf(r).VisibleTo(), Limit: p.Limit, Offset: p.Offset,
	})
	if err != nil {
		h.fail(w, err, "list change requests failed")
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(items, total, p))
}

func (h *PlanningHandler) ListChangeRequests(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	p := pagination.Parse(r)
	items, total, err := h.svc.ListChangeRequests(r.Context(), tenantID, &projectID, planning.CRFilter{
		Status: r.URL.Query().Get("status"), Limit: p.Limit, Offset: p.Offset,
	})
	if err != nil {
		h.fail(w, err, "list change requests failed")
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(items, total, p))
}

func (h *PlanningHandler) CreateChangeRequest(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	var in planning.ChangeRequestInput
	if !decode(w, r, &in) {
		return
	}
	out, err := h.svc.CreateChangeRequest(noticeCtx(r), tenantID, projectID, in)
	if err != nil {
		h.fail(w, err, "create change request failed")
		return
	}
	respondJSON(w, http.StatusCreated, out)
}

func (h *PlanningHandler) crScope(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, uuid.UUID, bool) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, uuid.Nil, false
	}
	id, ok := uuidParam(w, r, "crID")
	return tenantID, projectID, id, ok
}

func (h *PlanningHandler) GetChangeRequest(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, id, ok := h.crScope(w, r)
	if !ok {
		return
	}
	out, err := h.svc.GetChangeRequest(r.Context(), tenantID, projectID, id)
	if err != nil {
		h.fail(w, err, "get change request failed")
		return
	}
	respondJSON(w, http.StatusOK, out)
}

func (h *PlanningHandler) UpdateChangeRequest(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, id, ok := h.crScope(w, r)
	if !ok {
		return
	}
	var in planning.ChangeRequestInput
	if !decode(w, r, &in) {
		return
	}
	out, err := h.svc.UpdateChangeRequest(r.Context(), tenantID, projectID, id, in, accessOf(r).Has(rbac.PermChangeRequestsManage))
	if err != nil {
		h.fail(w, err, "update change request failed")
		return
	}
	respondJSON(w, http.StatusOK, out)
}

func (h *PlanningHandler) WithdrawChangeRequest(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, id, ok := h.crScope(w, r)
	if !ok {
		return
	}
	out, err := h.svc.WithdrawChangeRequest(r.Context(), tenantID, projectID, id, accessOf(r).Has(rbac.PermChangeRequestsManage))
	if err != nil {
		h.fail(w, err, "withdraw change request failed")
		return
	}
	respondJSON(w, http.StatusOK, out)
}

func (h *PlanningHandler) decide(w http.ResponseWriter, r *http.Request, approve bool) {
	tenantID, projectID, id, ok := h.crScope(w, r)
	if !ok {
		return
	}
	var in struct {
		Comment string `json:"comment"`
	}
	if r.ContentLength != 0 && !decode(w, r, &in) {
		return
	}
	out, err := h.svc.Decide(noticeCtx(r), tenantID, projectID, id, approve, in.Comment)
	if err != nil {
		h.fail(w, err, "decide change request failed")
		return
	}
	respondJSON(w, http.StatusOK, out)
}

// ApproveChangeRequest approves: {comment?}.
func (h *PlanningHandler) ApproveChangeRequest(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, true)
}

// RejectChangeRequest rejects: {comment} (required).
func (h *PlanningHandler) RejectChangeRequest(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, false)
}

// PushBudget retries sending an approved request's budget change to treasury.
func (h *PlanningHandler) PushBudget(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, id, ok := h.crScope(w, r)
	if !ok {
		return
	}
	if _, ok := actingUser(w, r); !ok {
		return
	}
	out, err := h.svc.PushBudget(r.Context(), tenantID, projectID, id)
	if err != nil {
		h.fail(w, err, "push budget failed")
		return
	}
	respondJSON(w, http.StatusOK, out)
}

// ---- Templates ----

func (h *PlanningHandler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	p := pagination.Parse(r)
	items, total, err := h.svc.ListTemplates(r.Context(), tenantID, r.URL.Query().Get("q"), p.Limit, p.Offset)
	if err != nil {
		h.fail(w, err, "list templates failed")
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(items, total, p))
}

func (h *PlanningHandler) GetTemplate(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "templateID")
	if !ok {
		return
	}
	out, err := h.svc.GetTemplate(r.Context(), tenantID, id)
	if err != nil {
		h.fail(w, err, "get template failed")
		return
	}
	respondJSON(w, http.StatusOK, out)
}

func (h *PlanningHandler) DeleteTemplate(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "templateID")
	if !ok {
		return
	}
	if err := h.svc.DeleteTemplate(r.Context(), tenantID, id); err != nil {
		h.fail(w, err, "delete template failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CreateFromTemplate starts a project: {name?, description?, start_date, currency?, metadata?}.
func (h *PlanningHandler) CreateFromTemplate(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "templateID")
	if !ok {
		return
	}
	if _, ok := actingUser(w, r); !ok {
		return
	}
	var in planning.FromTemplateInput
	if !decode(w, r, &in) {
		return
	}
	out, err := h.svc.CreateFromTemplate(r.Context(), tenantID, id, in)
	if err != nil {
		h.fail(w, err, "create project from template failed")
		return
	}
	respondJSON(w, http.StatusCreated, out)
}

// SaveAsTemplate saves the project's plan as a template: {name, description?, category?}.
func (h *PlanningHandler) SaveAsTemplate(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	var in planning.SaveAsTemplateInput
	if !decode(w, r, &in) {
		return
	}
	out, err := h.svc.SaveAsTemplate(r.Context(), tenantID, projectID, in)
	if err != nil {
		h.fail(w, err, "save as template failed")
		return
	}
	respondJSON(w, http.StatusCreated, out)
}
