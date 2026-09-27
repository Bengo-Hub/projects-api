package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Bengo-Hub/pagination"
	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/platform/treasury"
	"github.com/bengobox/projects-service/internal/services/financials"
)

// FinancialsHandler serves project financials (budget vs actual, earned value), the portfolio
// view, and the project budget, which lives in treasury and is proxied here so projects-ui talks
// to one backend. All of it is gated on the budget_tracking feature.
type FinancialsHandler struct {
	log *zap.Logger
	svc *financials.Service
}

func NewFinancialsHandler(log *zap.Logger, svc *financials.Service) *FinancialsHandler {
	return &FinancialsHandler{log: log.Named("financials.handler"), svc: svc}
}

// RegisterRoutes mounts the routes. requireFeature gates them (budget_tracking).
func (h *FinancialsHandler) RegisterRoutes(r chi.Router, requireFeature func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(requireFeature)
		g.Get("/financials/portfolio", h.Portfolio)
		g.Get("/financials/projects/{projectID}", h.Project)
		g.Get("/financials/projects/{projectID}/budgets", h.ListBudgets)
		g.Post("/financials/projects/{projectID}/budgets", h.CreateBudget)
		g.Put("/financials/projects/{projectID}/budgets/{budgetID}", h.UpdateBudget)
		g.Post("/financials/projects/{projectID}/budgets/{budgetID}/submit", h.SubmitBudget)
	})
}

// Project godoc
// @Summary Project financials
// @Description Budget, actual cost and revenue, committed spend, margin (from treasury) and earned value (CPI, SPI, EAC, VAC) with a health rating.
// @Tags financials
// @Produce json
// @Param tenantID path string true "Tenant"
// @Param projectID path string true "Project ID"
// @Success 200 {object} financials.ProjectFinancials
// @Router /api/v1/{tenantID}/financials/projects/{projectID} [get]
func (h *FinancialsHandler) Project(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectParams(w, r)
	if !ok {
		return
	}
	pf, err := h.svc.Project(r.Context(), tenantID, projectID)
	if errors.Is(err, financials.ErrNotFound) {
		respondError(w, http.StatusNotFound, "project not found")
		return
	}
	if err != nil {
		h.log.Error("project financials failed", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "failed to load project financials")
		return
	}
	respondJSON(w, http.StatusOK, pf)
}

// Portfolio godoc
// @Summary Project portfolio
// @Description One page of projects with budget, spend, earned value and health (?status, ?limit, ?page).
// @Tags financials
// @Produce json
// @Param tenantID path string true "Tenant"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/{tenantID}/financials/portfolio [get]
func (h *FinancialsHandler) Portfolio(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	p := pagination.Parse(r)
	rows, total, err := h.svc.Portfolio(r.Context(), tenantID, financials.PortfolioFilter{
		Status: r.URL.Query().Get("status"), Limit: p.Limit, Offset: p.Offset,
	})
	if err != nil {
		h.log.Error("portfolio failed", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "failed to load portfolio")
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(rows, total, p))
}

// treasuryFailed maps a treasury error to a response (treasury's own 4xx body is passed on).
func (h *FinancialsHandler) treasuryFailed(w http.ResponseWriter, err error) {
	var te *treasury.Error
	switch {
	case errors.As(err, &te) && te.Status >= 400 && te.Status < 500:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(te.Status)
		_, _ = w.Write(te.Body)
	case errors.Is(err, treasury.ErrNotConfigured):
		respondError(w, http.StatusServiceUnavailable, "budgets are not available")
	default:
		h.log.Warn("treasury budget call failed", zap.Error(err))
		respondError(w, http.StatusBadGateway, "treasury unavailable")
	}
}

func actingUser(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if ok && claims != nil {
		if uid, err := uuid.Parse(claims.Subject); err == nil {
			return uid, true
		}
	}
	respondError(w, http.StatusUnauthorized, "a signed-in user is required")
	return uuid.Nil, false
}

// projectExists confirms the project is the tenant's before touching its budget.
func (h *FinancialsHandler) projectExists(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	tenantID, projectID, ok := projectParams(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	exists, err := h.svc.ProjectExists(r.Context(), tenantID, projectID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load project")
		return uuid.Nil, uuid.Nil, false
	}
	if !exists {
		respondError(w, http.StatusNotFound, "project not found")
		return uuid.Nil, uuid.Nil, false
	}
	return tenantID, projectID, true
}

// budgetOfProject confirms a treasury budget belongs to the project (so a URL cannot reach
// another project's budget).
func (h *FinancialsHandler) budgetOfProject(w http.ResponseWriter, r *http.Request, tenantID, projectID uuid.UUID) (uuid.UUID, bool) {
	budgetID, err := uuid.Parse(chi.URLParam(r, "budgetID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid budget id")
		return uuid.Nil, false
	}
	raw, err := h.svc.Treasury().GetBudget(r.Context(), tenantID, budgetID)
	if err != nil {
		h.treasuryFailed(w, err)
		return uuid.Nil, false
	}
	var b struct {
		ProjectID *uuid.UUID `json:"project_id"`
	}
	if json.Unmarshal(raw, &b) != nil || b.ProjectID == nil || *b.ProjectID != projectID {
		respondError(w, http.StatusNotFound, "budget not found for this project")
		return uuid.Nil, false
	}
	return budgetID, true
}

// budgetBody reads a budget payload and pins it to the project (type project, this project id).
func budgetBody(w http.ResponseWriter, r *http.Request, projectID uuid.UUID) (map[string]any, bool) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body == nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return nil, false
	}
	body["budget_type"] = "project"
	body["project_id"] = projectID.String()
	return body, true
}

// ListBudgets returns the project's budgets (all versions) from treasury.
func (h *FinancialsHandler) ListBudgets(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := h.projectExists(w, r)
	if !ok {
		return
	}
	raw, err := h.svc.Treasury().ProjectBudgets(r.Context(), tenantID, projectID)
	if err != nil {
		h.treasuryFailed(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

// CreateBudget creates the project's budget in treasury as a draft.
func (h *FinancialsHandler) CreateBudget(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := h.projectExists(w, r)
	if !ok {
		return
	}
	userID, ok := actingUser(w, r)
	if !ok {
		return
	}
	body, ok := budgetBody(w, r, projectID)
	if !ok {
		return
	}
	raw, err := h.svc.Treasury().CreateBudget(r.Context(), tenantID, userID, body)
	if err != nil {
		h.treasuryFailed(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(raw)
}

// UpdateBudget replaces a draft project budget.
func (h *FinancialsHandler) UpdateBudget(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := h.projectExists(w, r)
	if !ok {
		return
	}
	userID, ok := actingUser(w, r)
	if !ok {
		return
	}
	budgetID, ok := h.budgetOfProject(w, r, tenantID, projectID)
	if !ok {
		return
	}
	body, ok := budgetBody(w, r, projectID)
	if !ok {
		return
	}
	raw, err := h.svc.Treasury().UpdateBudget(r.Context(), tenantID, userID, budgetID, body)
	if err != nil {
		h.treasuryFailed(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

// SubmitBudget sends the project budget for approval in treasury.
func (h *FinancialsHandler) SubmitBudget(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := h.projectExists(w, r)
	if !ok {
		return
	}
	userID, ok := actingUser(w, r)
	if !ok {
		return
	}
	budgetID, ok := h.budgetOfProject(w, r, tenantID, projectID)
	if !ok {
		return
	}
	raw, err := h.svc.Treasury().SubmitBudget(r.Context(), tenantID, userID, budgetID)
	if err != nil {
		h.treasuryFailed(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}
