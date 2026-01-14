package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/http/response"
	"github.com/bengobox/projects-service/internal/services/tender"
	"github.com/bengobox/projects-service/internal/shared/validation"
)

// TenderEvaluationHandler handles tender evaluation HTTP requests.
type TenderEvaluationHandler struct {
	logger  *zap.Logger
	service tender.TenderServiceInterface
}

// NewTenderEvaluationHandler creates a new tender evaluation handler.
func NewTenderEvaluationHandler(logger *zap.Logger, service tender.TenderServiceInterface) *TenderEvaluationHandler {
	return &TenderEvaluationHandler{
		logger:  logger,
		service: service,
	}
}

// Request/Response types

type CreateEvaluationRequest struct {
	CommitteeID    *uuid.UUID     `json:"committee_id,omitempty"`
	TechnicalScore *float64       `json:"technical_score,omitempty" validate:"omitempty,min=0,max=100"`
	FinancialScore *float64       `json:"financial_score,omitempty" validate:"omitempty,min=0,max=100"`
	ResourceScore  *float64       `json:"resource_score,omitempty" validate:"omitempty,min=0,max=100"`
	RiskScore      *float64       `json:"risk_score,omitempty" validate:"omitempty,min=0,max=100"`
	Vote           string         `json:"vote,omitempty" validate:"omitempty,oneof=approve reject abstain defer"`
	VoteComment    string         `json:"vote_comment,omitempty" validate:"max=1000"`
	Strengths      []string       `json:"strengths,omitempty"`
	Weaknesses     []string       `json:"weaknesses,omitempty"`
	Opportunities  []string       `json:"opportunities,omitempty"`
	Threats        []string       `json:"threats,omitempty"`
	Recommendation string         `json:"recommendation,omitempty" validate:"max=2000"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type UpdateEvaluationRequest struct {
	TechnicalScore *float64       `json:"technical_score,omitempty" validate:"omitempty,min=0,max=100"`
	FinancialScore *float64       `json:"financial_score,omitempty" validate:"omitempty,min=0,max=100"`
	ResourceScore  *float64       `json:"resource_score,omitempty" validate:"omitempty,min=0,max=100"`
	RiskScore      *float64       `json:"risk_score,omitempty" validate:"omitempty,min=0,max=100"`
	Vote           *string        `json:"vote,omitempty" validate:"omitempty,oneof=approve reject abstain defer"`
	VoteComment    *string        `json:"vote_comment,omitempty" validate:"omitempty,max=1000"`
	Strengths      []string       `json:"strengths,omitempty"`
	Weaknesses     []string       `json:"weaknesses,omitempty"`
	Opportunities  []string       `json:"opportunities,omitempty"`
	Threats        []string       `json:"threats,omitempty"`
	Recommendation *string        `json:"recommendation,omitempty" validate:"omitempty,max=2000"`
	IsFinal        *bool          `json:"is_final,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type EvaluationResponse struct {
	ID             uuid.UUID      `json:"id"`
	TenantID       uuid.UUID      `json:"tenant_id"`
	TenderID       uuid.UUID      `json:"tender_id"`
	EvaluatorID    uuid.UUID      `json:"evaluator_id"`
	CommitteeID    *uuid.UUID     `json:"committee_id,omitempty"`
	TechnicalScore float64        `json:"technical_score,omitempty"`
	FinancialScore float64        `json:"financial_score,omitempty"`
	ResourceScore  float64        `json:"resource_score,omitempty"`
	RiskScore      float64        `json:"risk_score,omitempty"`
	OverallScore   float64        `json:"overall_score,omitempty"`
	Vote           string         `json:"vote,omitempty"`
	VoteComment    string         `json:"vote_comment,omitempty"`
	Strengths      []string       `json:"strengths,omitempty"`
	Weaknesses     []string       `json:"weaknesses,omitempty"`
	Opportunities  []string       `json:"opportunities,omitempty"`
	Threats        []string       `json:"threats,omitempty"`
	Recommendation string         `json:"recommendation,omitempty"`
	IsFinal        bool           `json:"is_final"`
	SubmittedAt    *time.Time     `json:"submitted_at,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type ListEvaluationsResponse struct {
	Evaluations []EvaluationResponse `json:"evaluations"`
	Total       int                  `json:"total"`
}

// Handlers

func (h *TenderEvaluationHandler) Create(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	tenderID, err := h.getTenderID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tender ID")
		return
	}

	userID, err := h.getUserID(r)
	if err != nil {
		response.Unauthorized(w)
		return
	}

	var req CreateEvaluationRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	eval, err := h.service.CreateEvaluation(r.Context(), tender.EvaluationCreateParams{
		TenantID:       tenantID,
		TenderID:       tenderID,
		EvaluatorID:    userID,
		CommitteeID:    req.CommitteeID,
		TechnicalScore: req.TechnicalScore,
		FinancialScore: req.FinancialScore,
		ResourceScore:  req.ResourceScore,
		RiskScore:      req.RiskScore,
		Vote:           req.Vote,
		VoteComment:    req.VoteComment,
		Strengths:      req.Strengths,
		Weaknesses:     req.Weaknesses,
		Opportunities:  req.Opportunities,
		Threats:        req.Threats,
		Recommendation: req.Recommendation,
		Metadata:       req.Metadata,
	})
	if err != nil {
		if errors.Is(err, tender.ErrNotFound) {
			response.NotFound(w, "tender")
			return
		}
		h.logger.Error("failed to create evaluation", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.Created(w, h.toEvaluationResponse(eval))
}

func (h *TenderEvaluationHandler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	evaluationID, err := h.getEvaluationID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid evaluation ID")
		return
	}

	eval, err := h.service.GetEvaluation(r.Context(), tenantID, evaluationID)
	if err != nil {
		if errors.Is(err, tender.ErrEvaluationNotFound) {
			response.NotFound(w, "evaluation")
			return
		}
		h.logger.Error("failed to get evaluation", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toEvaluationResponse(eval))
}

func (h *TenderEvaluationHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	tenderID, err := h.getTenderID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tender ID")
		return
	}

	evals, err := h.service.ListEvaluations(r.Context(), tenantID, tenderID)
	if err != nil {
		h.logger.Error("failed to list evaluations", zap.Error(err))
		response.InternalError(w)
		return
	}

	resp := ListEvaluationsResponse{
		Evaluations: make([]EvaluationResponse, len(evals)),
		Total:       len(evals),
	}

	for i, e := range evals {
		resp.Evaluations[i] = h.toEvaluationResponse(e)
	}

	response.OK(w, resp)
}

func (h *TenderEvaluationHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	evaluationID, err := h.getEvaluationID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid evaluation ID")
		return
	}

	var req UpdateEvaluationRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	eval, err := h.service.UpdateEvaluation(r.Context(), tenantID, evaluationID, tender.EvaluationUpdateParams{
		TechnicalScore: req.TechnicalScore,
		FinancialScore: req.FinancialScore,
		ResourceScore:  req.ResourceScore,
		RiskScore:      req.RiskScore,
		Vote:           req.Vote,
		VoteComment:    req.VoteComment,
		Strengths:      req.Strengths,
		Weaknesses:     req.Weaknesses,
		Opportunities:  req.Opportunities,
		Threats:        req.Threats,
		Recommendation: req.Recommendation,
		IsFinal:        req.IsFinal,
		Metadata:       req.Metadata,
	})
	if err != nil {
		if errors.Is(err, tender.ErrEvaluationNotFound) {
			response.NotFound(w, "evaluation")
			return
		}
		h.logger.Error("failed to update evaluation", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toEvaluationResponse(eval))
}

func (h *TenderEvaluationHandler) Submit(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	evaluationID, err := h.getEvaluationID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid evaluation ID")
		return
	}

	eval, err := h.service.SubmitEvaluation(r.Context(), tenantID, evaluationID)
	if err != nil {
		if errors.Is(err, tender.ErrEvaluationNotFound) {
			response.NotFound(w, "evaluation")
			return
		}
		h.logger.Error("failed to submit evaluation", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toEvaluationResponse(eval))
}

func (h *TenderEvaluationHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	evaluationID, err := h.getEvaluationID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid evaluation ID")
		return
	}

	err = h.service.DeleteEvaluation(r.Context(), tenantID, evaluationID)
	if err != nil {
		if errors.Is(err, tender.ErrEvaluationNotFound) {
			response.NotFound(w, "evaluation")
			return
		}
		h.logger.Error("failed to delete evaluation", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.NoContent(w)
}

func (h *TenderEvaluationHandler) Summary(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	tenderID, err := h.getTenderID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tender ID")
		return
	}

	summary, err := h.service.GetEvaluationSummary(r.Context(), tenantID, tenderID)
	if err != nil {
		h.logger.Error("failed to get evaluation summary", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, summary)
}

// RegisterRoutes registers evaluation routes nested under tenders.
func (h *TenderEvaluationHandler) RegisterRoutes(r chi.Router) {
	r.Post("/", h.Create)
	r.Get("/", h.List)
	r.Get("/summary", h.Summary)
	r.Get("/{evaluationID}", h.Get)
	r.Patch("/{evaluationID}", h.Update)
	r.Post("/{evaluationID}/submit", h.Submit)
	r.Delete("/{evaluationID}", h.Delete)
}

// Helper methods

func (h *TenderEvaluationHandler) getTenantID(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, "tenantID"))
}

func (h *TenderEvaluationHandler) getTenderID(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, "tenderID"))
}

func (h *TenderEvaluationHandler) getEvaluationID(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, "evaluationID"))
}

func (h *TenderEvaluationHandler) getUserID(r *http.Request) (uuid.UUID, error) {
	userIDStr := r.Header.Get("X-User-ID")
	if userIDStr == "" {
		if userID, ok := r.Context().Value("user_id").(uuid.UUID); ok {
			return userID, nil
		}
		return uuid.Nil, errors.New("user ID not found")
	}
	return uuid.Parse(userIDStr)
}

func (h *TenderEvaluationHandler) toEvaluationResponse(e *ent.TenderEvaluation) EvaluationResponse {
	resp := EvaluationResponse{
		ID:          e.ID,
		TenantID:    e.TenantID,
		TenderID:    e.TenderID,
		EvaluatorID: e.EvaluatorID,
		IsFinal:     e.IsFinal,
		CreatedAt:   e.CreatedAt,
		UpdatedAt:   e.UpdatedAt,
	}

	if e.CommitteeID != uuid.Nil {
		resp.CommitteeID = &e.CommitteeID
	}
	if e.TechnicalScore != 0 {
		resp.TechnicalScore = e.TechnicalScore
	}
	if e.FinancialScore != 0 {
		resp.FinancialScore = e.FinancialScore
	}
	if e.ResourceScore != 0 {
		resp.ResourceScore = e.ResourceScore
	}
	if e.RiskScore != 0 {
		resp.RiskScore = e.RiskScore
	}
	if e.OverallScore != 0 {
		resp.OverallScore = e.OverallScore
	}
	if e.Vote != "" {
		resp.Vote = e.Vote
	}
	if e.VoteComment != "" {
		resp.VoteComment = e.VoteComment
	}
	if len(e.Strengths) > 0 {
		resp.Strengths = e.Strengths
	}
	if len(e.Weaknesses) > 0 {
		resp.Weaknesses = e.Weaknesses
	}
	if len(e.Opportunities) > 0 {
		resp.Opportunities = e.Opportunities
	}
	if len(e.Threats) > 0 {
		resp.Threats = e.Threats
	}
	if e.Recommendation != "" {
		resp.Recommendation = e.Recommendation
	}
	if !e.SubmittedAt.IsZero() {
		resp.SubmittedAt = &e.SubmittedAt
	}
	if e.Metadata != nil {
		resp.Metadata = e.Metadata
	}

	return resp
}
