// Package authz resolves who a request is for and what it may do (Layer 3 of the trinity
// pattern): JIT provisioning of the local user, their projects permissions from local RBAC, and
// project membership scoping. It runs after authentication, subscription gating and the tenant
// guard; routes then gate on Require.
package authz

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	httpware "github.com/Bengo-Hub/httpware"
	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/modules/rbac"
	"github.com/bengobox/projects-service/internal/services/activity"
)

// RBAC is the slice of the rbac service the access layer needs.
type RBAC interface {
	EnsureUserFromToken(ctx context.Context, id rbac.Identity) error
	Resolve(ctx context.Context, tenantID, authUserID uuid.UUID) (rbac.Resolved, error)
}

// Members answers project membership.
type Members interface {
	IsMember(ctx context.Context, tenantID, projectID, userID uuid.UUID) (bool, error)
}

// Access is everything a handler needs to authorise and scope a request.
type Access struct {
	TenantID    uuid.UUID
	AuthUserID  uuid.UUID // uuid.Nil for service callers
	Email       string
	GlobalRoles []string
	Roles       []string // projects roles, highest first
	Perms       []string // effective projects permission codes
	// Bypass skips permission checks: a superuser or platform owner (never subscription gating,
	// which ran earlier and only honours claims.IsGatingExempt), or a service caller.
	Bypass  bool
	Service bool
	Claims  *authclient.Claims
	// ResolveErr is set when the caller's permissions could not be read: the request runs with
	// none (fail closed) and GET /auth/me answers 503 instead of an empty profile.
	ResolveErr error
	members    Members
}

// AdminTier reports a caller who may change roles, revoke them and change people's status: a
// platform owner, a superuser or admin of the organisation (global roles), or a holder of
// projects_admin. Holding projects.rbac.manage alone is not enough. Service callers never are.
func (a *Access) AdminTier() bool {
	if a == nil || a.Service || a.AuthUserID == uuid.Nil {
		return false
	}
	if a.Claims != nil && (a.Claims.IsPlatformOwner || a.Claims.IsSuperuser() || a.Claims.IsAdmin()) {
		return true
	}
	for _, r := range a.Roles {
		if r == rbac.RoleAdmin {
			return true
		}
	}
	return false
}

type accessKey struct{}

// FromContext returns the request's Access, nil when Resolve did not run.
func FromContext(ctx context.Context) *Access {
	a, _ := ctx.Value(accessKey{}).(*Access)
	return a
}

// WithAccess returns ctx carrying a. Resolve is the production caller; tests use it directly.
func WithAccess(ctx context.Context, a *Access) context.Context {
	return context.WithValue(ctx, accessKey{}, a)
}

// Has reports whether the caller holds any of the codes.
func (a *Access) Has(codes ...string) bool {
	if a == nil {
		return false
	}
	if a.Bypass {
		return true
	}
	for _, c := range codes {
		for _, p := range a.Perms {
			if p == c {
				return true
			}
		}
	}
	return false
}

// VisibleTo returns the user a project read must be limited to, or nil when the caller sees every
// project of the tenant (bypass, service callers and holders of projects.projects.manage).
func (a *Access) VisibleTo() *uuid.UUID {
	if a == nil || a.Has(rbac.PermProjectsManage) {
		return nil
	}
	id := a.AuthUserID
	return &id
}

// CanSeeProject reports whether the caller may see and act on the project (membership scoping).
func (a *Access) CanSeeProject(ctx context.Context, projectID uuid.UUID) (bool, error) {
	if a == nil {
		return false, nil
	}
	user := a.VisibleTo()
	if user == nil {
		return true, nil
	}
	if a.members == nil {
		return false, nil
	}
	return a.members.IsMember(ctx, a.TenantID, projectID, *user)
}

// ServiceRole is the caller's highest projects role ("" when they hold none).
func (a *Access) ServiceRole() string {
	if a == nil {
		return ""
	}
	if a.Bypass && len(a.Roles) == 0 {
		return rbac.RoleAdmin
	}
	if len(a.Roles) == 0 {
		return ""
	}
	return a.Roles[0]
}

// Effective returns the permission codes to show the caller: every registry code under bypass.
func (a *Access) Effective() []string {
	if a == nil {
		return []string{}
	}
	if a.Bypass {
		return rbac.AllCodes()
	}
	if a.Perms == nil {
		return []string{}
	}
	return a.Perms
}

// IsServiceCaller reports an API key (service to service) caller rather than a signed-in user.
func IsServiceCaller(c *authclient.Claims) bool {
	return c != nil && (c.IsService || c.ServiceName != "")
}

// Resolve builds Access for every request of the tenant group, provisioning the local user just in
// time (with their default role) on the way. It runs after the tenant guard, which has fixed the
// request's tenant in the context.
func Resolve(r RBAC, members Members, log *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := req.Context()
			claims, ok := authclient.ClaimsFromContext(ctx)
			if !ok || claims == nil {
				WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized", "code": "unauthorized"})
				return
			}
			tenantID, err := uuid.Parse(httpware.GetTenantID(ctx))
			if err != nil || tenantID == uuid.Nil {
				WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid tenant id", "code": "tenant_required"})
				return
			}
			a := &Access{TenantID: tenantID, Email: claims.Email, GlobalRoles: claims.Roles, Claims: claims, members: members}
			if IsServiceCaller(claims) {
				a.Service, a.Bypass = true, true
				next.ServeHTTP(w, req.WithContext(WithAccess(ctx, a)))
				return
			}
			authUserID, err := claims.UserID()
			if err != nil || authUserID == uuid.Nil {
				WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized", "code": "unauthorized"})
				return
			}
			a.AuthUserID = authUserID
			a.Bypass = claims.IsPlatformOwner || claims.IsSuperuser()
			if r != nil {
				// Provision only in the user's own tenant (a platform owner looking at another
				// tenant gets no local row there), and only when auth-api lets the user into
				// projects (no user_services claim means every service).
				if strings.EqualFold(claims.TenantID, tenantID.String()) && claims.AllowsUserService("projects") {
					if err := r.EnsureUserFromToken(ctx, rbac.Identity{
						TenantID: tenantID, AuthUserID: authUserID, Email: claims.Email, GlobalRoles: claims.Roles,
					}); err != nil {
						log.Warn("jit user provisioning failed", zap.String("user_id", authUserID.String()), zap.Error(err))
					}
				}
				res, err := r.Resolve(ctx, tenantID, authUserID)
				if err != nil {
					// Fail closed: no permissions rather than a guess.
					log.Error("permission resolve failed", zap.String("user_id", authUserID.String()), zap.Error(err))
					a.ResolveErr = err
				}
				a.Roles, a.Perms = res.Roles, res.Perms
			}
			ctx = activity.WithActor(ctx, authUserID)
			ctx = httpware.WithUserID(ctx, authUserID.String())
			next.ServeHTTP(w, req.WithContext(WithAccess(ctx, a)))
		})
	}
}

// Require allows the request when the caller holds any of the codes; otherwise 403 with
// code permission_denied (never upgrade, which marks subscription refusals).
func Require(codes ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if FromContext(r.Context()).Has(codes...) {
				next.ServeHTTP(w, r)
				return
			}
			Forbidden(w, codes...)
		})
	}
}

// RequireAdminTier allows the request only for an admin tier caller (Access.AdminTier); otherwise
// 403 with code admin_required. It sits on top of a permission gate, never instead of one.
func RequireAdminTier(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if FromContext(r.Context()).AdminTier() {
			next.ServeHTTP(w, r)
			return
		}
		WriteJSON(w, http.StatusForbidden, map[string]string{
			"error": "only an administrator of this organisation can do this",
			"code":  "admin_required",
		})
	})
}

// Forbidden writes the permission 403.
func Forbidden(w http.ResponseWriter, codes ...string) {
	WriteJSON(w, http.StatusForbidden, map[string]string{
		"error":               "you do not have permission to perform this action",
		"code":                "permission_denied",
		"required_permission": strings.Join(codes, " | "),
	})
}

// WriteJSON writes a JSON response.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
