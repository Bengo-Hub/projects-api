package handlers

import (
	"errors"
	"net/http"

	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/services/qa"
)

// QA permission codes, in the fleet's {service}.{module}.{action} form. auth-api puts a user's
// codes in the token; tenant admins and the platform owner pass without them.
const (
	PermQAView   = "projects.qa.view"   // read templates, sessions, results, defects and reports
	PermQARun    = "projects.qa.run"    // record results as a signed-in team member (implies view)
	PermQAManage = "projects.qa.manage" // templates, sessions, invites, close and decide (implies all)
)

// QAPermissions lists the codes for the seed and docs.
var QAPermissions = []string{PermQAView, PermQARun, PermQAManage}

// qaAllowed reports whether the claims carry perm, directly or through a wider QA code.
func qaAllowed(c *authclient.Claims, perm string) bool {
	if c == nil {
		return false
	}
	if c.IsPlatformOwner || c.IsAdmin() || c.HasPermission(PermQAManage) {
		return true
	}
	if perm == PermQAView && c.HasPermission(PermQARun) {
		return true
	}
	return c.HasPermission(perm)
}

// requireQA gates a route on a QA permission.
func requireQA(perm string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, _ := authclient.ClaimsFromContext(r.Context())
			if !qaAllowed(claims, perm) {
				respondJSON(w, http.StatusForbidden, map[string]string{"error": "permission denied", "code": "permission_denied", "required_permission": perm})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// qaActor builds the acting user from the token and the route tenant.
func qaActor(w http.ResponseWriter, r *http.Request) (qa.Actor, bool) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return qa.Actor{}, false
	}
	a := qa.Actor{TenantID: tenantID}
	if claims, ok := authclient.ClaimsFromContext(r.Context()); ok && claims != nil {
		a.Email = claims.Email
		a.PlatformOwner = claims.IsPlatformOwner
		if uid, err := uuid.Parse(claims.Subject); err == nil {
			a.UserID = uid
		}
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
