package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/services/fieldkeys"
)

// FieldKeyService is the field encryption key management the handler needs
// (internal/services/fieldkeys).
type FieldKeyService interface {
	Health(ctx context.Context) (fieldkeys.Health, error)
	Backup(ctx context.Context, actor fieldkeys.Actor, otpToken string) (*fieldkeys.BackupFile, error)
	Rotate(ctx context.Context, actor fieldkeys.Actor, otpToken string) (fieldkeys.Health, error)
}

// FieldKeyHandler serves the platform owner's "Security keys" page: key health, a backup download
// and rotation. Backup and rotation need a fresh auth-api email code (otp_token). Key material only
// ever leaves in the backup download, which is never logged.
type FieldKeyHandler struct {
	svc FieldKeyService
	log *zap.Logger
}

func NewFieldKeyHandler(svc FieldKeyService, log *zap.Logger) *FieldKeyHandler {
	if log == nil {
		log = zap.NewNop()
	}
	return &FieldKeyHandler{svc: svc, log: log}
}

// RegisterPlatformRoutes mounts the routes; every one is platform owner only.
func (h *FieldKeyHandler) RegisterPlatformRoutes(r chi.Router) {
	r.Group(func(g chi.Router) {
		g.Use(requirePlatformOwner)
		g.Get("/field-keys", h.Health)
		g.Post("/field-keys/backup", h.Backup)
		g.Post("/field-keys/rotate", h.Rotate)
	})
}

// requirePlatformOwner lets only the platform owner through. A tenant admin or superuser is not
// enough: these keys protect every tenant's data.
func requirePlatformOwner(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authclient.ClaimsFromContext(r.Context())
		if !ok || claims == nil || !claims.IsPlatformOwner {
			respondJSON(w, http.StatusForbidden, map[string]string{"error": "platform owner only", "code": "platform_owner_required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func fieldKeyActor(r *http.Request) fieldkeys.Actor {
	var a fieldkeys.Actor
	if claims, ok := authclient.ClaimsFromContext(r.Context()); ok && claims != nil {
		a.Email = claims.Email
		if uid, err := uuid.Parse(claims.Subject); err == nil {
			a.UserID = uid
		}
	}
	return a
}

type otpBody struct {
	OTPToken string `json:"otp_token"`
}

func readOTPBody(w http.ResponseWriter, r *http.Request) string {
	var b otpBody
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b)
	return b.OTPToken
}

func (h *FieldKeyHandler) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, fieldkeys.ErrOTPRequired):
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error(), "code": "otp_required"})
	case errors.Is(err, fieldkeys.ErrOTPInvalid):
		respondJSON(w, http.StatusForbidden, map[string]string{"error": err.Error(), "code": "otp_invalid"})
	case errors.Is(err, fieldkeys.ErrOTPUnavailable):
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error(), "code": "otp_unavailable"})
	case errors.Is(err, fieldkeys.ErrRotationRunning):
		respondJSON(w, http.StatusConflict, map[string]string{"error": err.Error(), "code": "rotation_running"})
	case errors.Is(err, fieldkeys.ErrNotConfigured):
		respondJSON(w, http.StatusConflict, map[string]string{"error": err.Error(), "code": "not_configured"})
	default:
		h.log.Error("field key request failed", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "field key request failed")
	}
}

// Health returns the key health card. GET /api/v1/platform/field-keys
func (h *FieldKeyHandler) Health(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Health(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	respondJSON(w, http.StatusOK, out)
}

// Backup downloads the data keys as a JSON file. POST /api/v1/platform/field-keys/backup
// {otp_token}. POST, not GET, so the one-time code never sits in a URL or an access log.
func (h *FieldKeyHandler) Backup(w http.ResponseWriter, r *http.Request) {
	file, err := h.svc.Backup(r.Context(), fieldKeyActor(r), readOTPBody(w, r))
	if err != nil {
		h.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+file.Name+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(file.Body)))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(file.Body)
}

// Rotate starts a rotation. POST /api/v1/platform/field-keys/rotate {otp_token}; 202 with the
// health card, whose rotation block shows the re-seal progress.
func (h *FieldKeyHandler) Rotate(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Rotate(r.Context(), fieldKeyActor(r), readOTPBody(w, r))
	if err != nil {
		h.fail(w, err)
		return
	}
	respondJSON(w, http.StatusAccepted, out)
}
