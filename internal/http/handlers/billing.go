package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/http/authz"
	"github.com/bengobox/projects-service/internal/modules/rbac"
	"github.com/bengobox/projects-service/internal/platform/erp"
	"github.com/bengobox/projects-service/internal/platform/treasury"
	"github.com/bengobox/projects-service/internal/services/billing"
)

// BillingHandler serves project billing (sprint 15): milestone prices and invoices, time and
// retainer invoices, all created as drafts in treasury. Gated on the milestone_billing feature.
type BillingHandler struct {
	log *zap.Logger
	svc *billing.Service
}

func NewBillingHandler(log *zap.Logger, svc *billing.Service) *BillingHandler {
	return &BillingHandler{log: log.Named("billing.handler"), svc: svc}
}

// RegisterRoutes mounts the routes behind the feature gate.
func (h *BillingHandler) RegisterRoutes(r chi.Router, gate func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(gate)
		view := authz.Require(rbac.PermBillingView)
		manage := authz.Require(rbac.PermBillingManage)
		g.With(view).Get("/projects/{projectID}/billing", h.Overview)
		g.With(manage).Put("/projects/{projectID}/billing/milestones/{milestoneID}", h.SetMilestoneAmount)
		g.With(manage).Post("/projects/{projectID}/billing/milestones/{milestoneID}/invoice", h.InvoiceMilestone)
		g.With(manage).Post("/projects/{projectID}/billing/time-invoice", h.InvoiceTime)
		g.With(manage).Post("/projects/{projectID}/billing/retainer-invoice", h.InvoiceRetainer)
	})
}

// fail maps a billing error to a response with a code the UI can act on.
func (h *BillingHandler) fail(w http.ResponseWriter, err error) {
	var verr billing.ValidationError
	var te *treasury.Error
	var ee *erp.Error
	switch {
	case errors.Is(err, billing.ErrNotFound):
		respondJSON(w, http.StatusNotFound, map[string]any{"error": "project or milestone not found", "code": "not_found"})
	case errors.As(err, &verr):
		respondJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": verr.Error(), "code": "validation_error"})
	case errors.Is(err, billing.ErrNothingToBill):
		respondJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error(), "code": "nothing_to_bill"})
	case errors.Is(err, billing.ErrBusy):
		respondJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "code": "invoice_in_progress"})
	case errors.Is(err, billing.ErrUnavailable), errors.Is(err, treasury.ErrNotConfigured), errors.Is(err, erp.ErrNotConfigured):
		respondJSON(w, http.StatusServiceUnavailable, map[string]any{"error": billing.ErrUnavailable.Error(), "code": "billing_unavailable"})
	case errors.As(err, &te) && te.Status >= 400 && te.Status < 500:
		// Treasury refused the invoice (an unknown client, a closed period): pass its reason on.
		var body struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(te.Body, &body)
		msg := body.Error
		if msg == "" {
			msg = body.Message
		}
		if msg == "" {
			msg = "treasury refused the invoice"
		}
		respondJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": msg, "code": "treasury_refused"})
	case errors.As(err, &ee) && ee.Status >= 400 && ee.Status < 500:
		msg := ee.Message
		if msg == "" {
			msg = "ERP refused the request"
		}
		respondJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": msg, "code": "erp_refused"})
	default:
		h.log.Warn("billing call failed", zap.Error(err))
		respondJSON(w, http.StatusBadGateway, map[string]any{"error": "treasury or ERP is not answering; try again shortly", "code": "upstream_unavailable"})
	}
}

// Overview godoc
// @Summary Project billing
// @Description The project's billing terms, milestones with price and invoice state, latest invoices and unbilled time. Needs projects.billing.view and the milestone_billing feature.
// @Tags billing
// @Produce json
// @Param tenantID path string true "Tenant"
// @Param projectID path string true "Project ID"
// @Success 200 {object} billing.Overview
// @Router /api/v1/{tenantID}/projects/{projectID}/billing [get]
func (h *BillingHandler) Overview(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	out, err := h.svc.Overview(r.Context(), tenantID, projectID)
	if errors.Is(err, billing.ErrNotFound) {
		respondError(w, http.StatusNotFound, "project not found")
		return
	}
	if err != nil {
		serverError(w, h.log, "billing overview failed", err)
		return
	}
	respondJSON(w, http.StatusOK, out)
}

// SetMilestoneAmount godoc
// @Summary Price a milestone
// @Description Sets the milestone's amount (decimal string) or amount_pct (share of the contract value); both null clears it. Refused while a live invoice exists. Needs projects.billing.manage.
// @Tags billing
// @Accept json
// @Produce json
// @Param tenantID path string true "Tenant"
// @Param projectID path string true "Project ID"
// @Param milestoneID path string true "Milestone ID"
// @Param body body billing.AmountInput true "Amount"
// @Success 200 {object} billing.MilestoneBilling
// @Router /api/v1/{tenantID}/projects/{projectID}/billing/milestones/{milestoneID} [put]
func (h *BillingHandler) SetMilestoneAmount(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	milestoneID, ok := uuidParam(w, r, "milestoneID")
	if !ok {
		return
	}
	var in billing.AmountInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	out, err := h.svc.SetMilestoneAmount(r.Context(), tenantID, projectID, milestoneID, in)
	if err != nil {
		h.fail(w, err)
		return
	}
	respondJSON(w, http.StatusOK, out)
}

// InvoiceMilestone godoc
// @Summary Invoice a milestone
// @Description Creates the milestone's draft invoice in treasury for the project's client, or returns the one already created. Needs projects.billing.manage.
// @Tags billing
// @Produce json
// @Param tenantID path string true "Tenant"
// @Param projectID path string true "Project ID"
// @Param milestoneID path string true "Milestone ID"
// @Success 200 {object} treasury.Invoice
// @Router /api/v1/{tenantID}/projects/{projectID}/billing/milestones/{milestoneID}/invoice [post]
func (h *BillingHandler) InvoiceMilestone(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	milestoneID, ok := uuidParam(w, r, "milestoneID")
	if !ok {
		return
	}
	userID, ok := actingUser(w, r)
	if !ok {
		return
	}
	inv, err := h.svc.InvoiceMilestone(r.Context(), tenantID, projectID, milestoneID, userID)
	if err != nil {
		h.fail(w, err)
		return
	}
	respondJSON(w, http.StatusOK, inv)
}

// InvoiceTime godoc
// @Summary Invoice time
// @Description Bills the project's approved, billable, unbilled ERP hours dated from..to (YYYY-MM-DD) at its hourly rate, one line per person. The same period again returns the same invoice. Needs projects.billing.manage.
// @Tags billing
// @Accept json
// @Produce json
// @Param tenantID path string true "Tenant"
// @Param projectID path string true "Project ID"
// @Success 200 {object} billing.PeriodInvoice
// @Router /api/v1/{tenantID}/projects/{projectID}/billing/time-invoice [post]
func (h *BillingHandler) InvoiceTime(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	userID, ok := actingUser(w, r)
	if !ok {
		return
	}
	var in struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	out, err := h.svc.InvoiceTime(r.Context(), tenantID, projectID, userID, in.From, in.To)
	if err != nil {
		h.fail(w, err)
		return
	}
	respondJSON(w, http.StatusOK, out)
}

// InvoiceRetainer godoc
// @Summary Invoice a retainer month
// @Description Bills one month (YYYY-MM) of the project's retainer fee. The same month again returns the same invoice. Needs projects.billing.manage.
// @Tags billing
// @Accept json
// @Produce json
// @Param tenantID path string true "Tenant"
// @Param projectID path string true "Project ID"
// @Success 200 {object} billing.PeriodInvoice
// @Router /api/v1/{tenantID}/projects/{projectID}/billing/retainer-invoice [post]
func (h *BillingHandler) InvoiceRetainer(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	userID, ok := actingUser(w, r)
	if !ok {
		return
	}
	var in struct {
		Month string `json:"month"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	out, err := h.svc.InvoiceRetainer(r.Context(), tenantID, projectID, userID, in.Month)
	if err != nil {
		h.fail(w, err)
		return
	}
	respondJSON(w, http.StatusOK, out)
}
