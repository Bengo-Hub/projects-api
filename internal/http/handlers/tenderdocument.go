package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/http/response"
	"github.com/bengobox/projects-service/internal/services/tender"
	"github.com/bengobox/projects-service/internal/shared/validation"
)

// TenderDocumentHandler handles tender document HTTP requests.
type TenderDocumentHandler struct {
	logger  *zap.Logger
	service tender.TenderServiceInterface
}

// NewTenderDocumentHandler creates a new tender document handler.
func NewTenderDocumentHandler(logger *zap.Logger, service tender.TenderServiceInterface) *TenderDocumentHandler {
	return &TenderDocumentHandler{
		logger:  logger,
		service: service,
	}
}

// Request/Response types

type CreateTenderDocumentRequest struct {
	Name         string         `json:"name" validate:"required,min=1,max=255"`
	Description  string         `json:"description,omitempty" validate:"max=1000"`
	DocumentType string         `json:"document_type,omitempty" validate:"omitempty,oneof=rfp rfq tor specification evaluation_criteria contract_template addendum clarification response other"`
	FileURL      string         `json:"file_url" validate:"required,url"`
	FileName     string         `json:"file_name" validate:"required,min=1,max=255"`
	FileSize     int64          `json:"file_size" validate:"required,gt=0"`
	MimeType     string         `json:"mime_type,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

type UpdateTenderDocumentRequest struct {
	Name         *string        `json:"name,omitempty" validate:"omitempty,min=1,max=255"`
	Description  *string        `json:"description,omitempty" validate:"omitempty,max=1000"`
	DocumentType *string        `json:"document_type,omitempty" validate:"omitempty,oneof=rfp rfq tor specification evaluation_criteria contract_template addendum clarification response other"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

type TenderDocumentResponse struct {
	ID                uuid.UUID      `json:"id"`
	TenantID          uuid.UUID      `json:"tenant_id"`
	TenderID          uuid.UUID      `json:"tender_id"`
	Name              string         `json:"name"`
	Description       string         `json:"description,omitempty"`
	DocumentType      string         `json:"document_type"`
	FileURL           string         `json:"file_url"`
	FileName          string         `json:"file_name"`
	FileSize          int64          `json:"file_size"`
	MimeType          string         `json:"mime_type,omitempty"`
	Version           int            `json:"version"`
	IsLatest          bool           `json:"is_latest"`
	PreviousVersionID *uuid.UUID     `json:"previous_version_id,omitempty"`
	UploadedBy        uuid.UUID      `json:"uploaded_by"`
	UploadedAt        time.Time      `json:"uploaded_at"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

type ListTenderDocumentsResponse struct {
	Documents []TenderDocumentResponse `json:"documents"`
	Total     int                      `json:"total"`
	Limit     int                      `json:"limit"`
	Offset    int                      `json:"offset"`
}

// Handlers

func (h *TenderDocumentHandler) Create(w http.ResponseWriter, r *http.Request) {
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

	var req CreateTenderDocumentRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	doc, err := h.service.CreateDocument(r.Context(), tender.DocumentCreateParams{
		TenantID:     tenantID,
		TenderID:     tenderID,
		Name:         req.Name,
		Description:  req.Description,
		DocumentType: req.DocumentType,
		FileURL:      req.FileURL,
		FileName:     req.FileName,
		FileSize:     req.FileSize,
		MimeType:     req.MimeType,
		UploadedBy:   userID,
		Metadata:     req.Metadata,
	})
	if err != nil {
		if errors.Is(err, tender.ErrNotFound) {
			response.NotFound(w, "tender")
			return
		}
		h.logger.Error("failed to create tender document", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.Created(w, h.toDocumentResponse(doc))
}

func (h *TenderDocumentHandler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	documentID, err := h.getDocumentID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid document ID")
		return
	}

	doc, err := h.service.GetDocument(r.Context(), tenantID, documentID)
	if err != nil {
		if errors.Is(err, tender.ErrDocumentNotFound) {
			response.NotFound(w, "document")
			return
		}
		h.logger.Error("failed to get document", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toDocumentResponse(doc))
}

func (h *TenderDocumentHandler) List(w http.ResponseWriter, r *http.Request) {
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

	params := tender.DocumentListParams{
		TenantID:     tenantID,
		TenderID:     tenderID,
		DocumentType: r.URL.Query().Get("document_type"),
		LatestOnly:   r.URL.Query().Get("latest_only") == "true",
		Limit:        h.parseIntQuery(r, "limit", 20),
		Offset:       h.parseIntQuery(r, "offset", 0),
	}

	docs, total, err := h.service.ListDocuments(r.Context(), params)
	if err != nil {
		h.logger.Error("failed to list documents", zap.Error(err))
		response.InternalError(w)
		return
	}

	resp := ListTenderDocumentsResponse{
		Documents: make([]TenderDocumentResponse, len(docs)),
		Total:     total,
		Limit:     params.Limit,
		Offset:    params.Offset,
	}

	for i, doc := range docs {
		resp.Documents[i] = h.toDocumentResponse(doc)
	}

	response.OK(w, resp)
}

func (h *TenderDocumentHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	documentID, err := h.getDocumentID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid document ID")
		return
	}

	var req UpdateTenderDocumentRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	doc, err := h.service.UpdateDocument(r.Context(), tenantID, documentID, tender.DocumentUpdateParams{
		Name:         req.Name,
		Description:  req.Description,
		DocumentType: req.DocumentType,
		Metadata:     req.Metadata,
	})
	if err != nil {
		if errors.Is(err, tender.ErrDocumentNotFound) {
			response.NotFound(w, "document")
			return
		}
		h.logger.Error("failed to update document", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toDocumentResponse(doc))
}

func (h *TenderDocumentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	documentID, err := h.getDocumentID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid document ID")
		return
	}

	err = h.service.DeleteDocument(r.Context(), tenantID, documentID)
	if err != nil {
		if errors.Is(err, tender.ErrDocumentNotFound) {
			response.NotFound(w, "document")
			return
		}
		h.logger.Error("failed to delete document", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.NoContent(w)
}

func (h *TenderDocumentHandler) CreateVersion(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	documentID, err := h.getDocumentID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid document ID")
		return
	}

	userID, err := h.getUserID(r)
	if err != nil {
		response.Unauthorized(w)
		return
	}

	var req CreateTenderDocumentRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	doc, err := h.service.CreateDocumentVersion(r.Context(), tenantID, documentID, tender.DocumentCreateParams{
		TenantID:     tenantID,
		Name:         req.Name,
		Description:  req.Description,
		DocumentType: req.DocumentType,
		FileURL:      req.FileURL,
		FileName:     req.FileName,
		FileSize:     req.FileSize,
		MimeType:     req.MimeType,
		UploadedBy:   userID,
		Metadata:     req.Metadata,
	})
	if err != nil {
		if errors.Is(err, tender.ErrDocumentNotFound) {
			response.NotFound(w, "document")
			return
		}
		h.logger.Error("failed to create document version", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.Created(w, h.toDocumentResponse(doc))
}

// RegisterRoutes registers document routes nested under tenders.
func (h *TenderDocumentHandler) RegisterRoutes(r chi.Router) {
	r.Post("/", h.Create)
	r.Get("/", h.List)
	r.Get("/{documentID}", h.Get)
	r.Patch("/{documentID}", h.Update)
	r.Delete("/{documentID}", h.Delete)
	r.Post("/{documentID}/versions", h.CreateVersion)
}

// Helper methods

func (h *TenderDocumentHandler) getTenantID(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, "tenantID"))
}

func (h *TenderDocumentHandler) getTenderID(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, "tenderID"))
}

func (h *TenderDocumentHandler) getDocumentID(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, "documentID"))
}

func (h *TenderDocumentHandler) getUserID(r *http.Request) (uuid.UUID, error) {
	userIDStr := r.Header.Get("X-User-ID")
	if userIDStr == "" {
		if userID, ok := r.Context().Value("user_id").(uuid.UUID); ok {
			return userID, nil
		}
		return uuid.Nil, errors.New("user ID not found")
	}
	return uuid.Parse(userIDStr)
}

func (h *TenderDocumentHandler) parseIntQuery(r *http.Request, key string, defaultVal int) int {
	val := r.URL.Query().Get(key)
	if val == "" {
		return defaultVal
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return parsed
}

func (h *TenderDocumentHandler) toDocumentResponse(doc *ent.TenderDocument) TenderDocumentResponse {
	resp := TenderDocumentResponse{
		ID:           doc.ID,
		TenantID:     doc.TenantID,
		TenderID:     doc.TenderID,
		Name:         doc.Name,
		DocumentType: doc.DocumentType,
		FileURL:      doc.FileURL,
		FileName:     doc.FileName,
		FileSize:     doc.FileSize,
		Version:      doc.Version,
		IsLatest:     doc.IsLatest,
		UploadedBy:   doc.UploadedBy,
		UploadedAt:   doc.UploadedAt,
	}

	if doc.Description != "" {
		resp.Description = doc.Description
	}
	if doc.MimeType != "" {
		resp.MimeType = doc.MimeType
	}
	if doc.PreviousVersionID != uuid.Nil {
		resp.PreviousVersionID = &doc.PreviousVersionID
	}
	if doc.Metadata != nil {
		resp.Metadata = doc.Metadata
	}

	return resp
}
