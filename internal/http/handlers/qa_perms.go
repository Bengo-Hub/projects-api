package handlers

import (
	"errors"
	"net/http"

	"github.com/bengobox/projects-service/internal/services/qa"
)

// QA routes gate on the local projects.qa.* codes (rbac.PermQAView, PermQARun, PermQAManage) via
// authz.Require: run implies view and manage implies every QA action (rbac.Expand).

// qaActor builds the acting user from the request's access and tenant.
func qaActor(w http.ResponseWriter, r *http.Request) (qa.Actor, bool) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return qa.Actor{}, false
	}
	a := qa.Actor{TenantID: tenantID, UserID: actorID(r)}
	if acc := accessOf(r); acc != nil {
		a.Email = acc.Email
		a.PlatformOwner = acc.Claims != nil && acc.Claims.IsPlatformOwner
	}
	return a, true
}

// qaError maps service errors to HTTP answers.
func qaError(w http.ResponseWriter, err error) {
	var verr qa.ValidationError
	switch {
	case errors.As(err, &verr):
		respondError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, qa.ErrNotFound):
		respondError(w, http.StatusNotFound, "not found")
	case errors.Is(err, qa.ErrForbidden):
		respondError(w, http.StatusForbidden, "not allowed")
	case errors.Is(err, qa.ErrAccessDenied):
		respondError(w, http.StatusForbidden, "access denied")
	case errors.Is(err, qa.ErrConflict):
		respondError(w, http.StatusConflict, err.Error())
	default:
		respondError(w, http.StatusInternalServerError, "internal error")
	}
}
