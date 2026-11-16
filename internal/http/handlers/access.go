package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/http/authz"
)

// genericServerError is the only text a 5xx body ever carries; the detail goes to the log.
const genericServerError = "internal server error"

// serverError logs err with msg and answers 500 with the generic message.
func serverError(w http.ResponseWriter, log *zap.Logger, msg string, err error) {
	log.Error(msg, zap.Error(err))
	respondError(w, http.StatusInternalServerError, genericServerError)
}

// respondError writes a JSON error response. A 5xx never carries the given text (it may hold
// database or upstream detail): it is logged and the body says only genericServerError.
func respondError(w http.ResponseWriter, status int, msg string) {
	if status >= http.StatusInternalServerError {
		if msg != genericServerError {
			zap.L().Error("request failed", zap.Int("status", status), zap.String("error", msg))
		}
		msg = genericServerError
	}
	respondJSON(w, status, map[string]string{"error": msg})
}

// accessOf returns the request's access (set by authz.Resolve for every tenant route).
func accessOf(r *http.Request) *authz.Access { return authz.FromContext(r.Context()) }

// parseTenant returns the request's tenant, fixed by the tenant guard and the access layer.
func parseTenant(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	a := accessOf(r)
	if a == nil || a.TenantID == uuid.Nil {
		respondError(w, http.StatusBadRequest, "invalid tenant id")
		return uuid.Nil, false
	}
	return a.TenantID, true
}

// projectScope parses the tenant and the project path parameter and applies membership scoping:
// a caller without projects.projects.manage reaches only projects they own or belong to; any
// other project answers 404, the same as a project that does not exist. Every handler under
// /projects/{projectID} and /financials/projects/{projectID} goes through here.
func projectScope(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	projectID, err := uuid.Parse(chi.URLParam(r, "projectID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid project id")
		return uuid.Nil, uuid.Nil, false
	}
	visible, err := accessOf(r).CanSeeProject(r.Context(), projectID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return uuid.Nil, uuid.Nil, false
	}
	if !visible {
		respondError(w, http.StatusNotFound, "project not found")
		return uuid.Nil, uuid.Nil, false
	}
	return tenantID, projectID, true
}

// actorID returns the signed-in user (uuid.Nil for service callers).
func actorID(r *http.Request) uuid.UUID {
	if a := accessOf(r); a != nil {
		return a.AuthUserID
	}
	return uuid.Nil
}
