package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/http/authz"
	"github.com/bengobox/projects-service/internal/modules/rbac"
	"github.com/bengobox/projects-service/internal/services/usersync"
)

// RBACHandler serves the caller's projects profile (auth/me) and role management.
type RBACHandler struct {
	log  *zap.Logger
	svc  *rbac.Service
	sync *usersync.Service
}

// NewRBACHandler creates the handler. sync provisions accounts in auth-api (POST /users).
func NewRBACHandler(log *zap.Logger, svc *rbac.Service, sync *usersync.Service) *RBACHandler {
	return &RBACHandler{log: log.Named("rbac.handler"), svc: svc, sync: sync}
}

// RegisterRoutes mounts GET /auth/me for every signed-in user and the role management routes,
// all gated on projects.rbac.manage.
func (h *RBACHandler) RegisterRoutes(r chi.Router) {
	r.Get("/auth/me", h.Me)
	manage := authz.Require(rbac.PermRBACManage)
	r.Route("/rbac", func(g chi.Router) {
		g.Use(manage)
		g.Get("/roles", h.ListRoles)
		g.Get("/permissions", h.ListPermissions)
		g.Get("/users", h.ListUsers)
		g.Post("/users/{userID}/roles", h.AssignRole)
		g.Delete("/users/{userID}/roles/{roleCode}", h.RevokeRole)
	})
	r.With(manage).Post("/users", h.CreateUser)
}

// MeResponse is GET /api/v1/{tenant}/auth/me: the projects layer of the caller's profile. The UI
// calls it after SSO login (identity itself comes from auth-api's /api/v1/auth/me).
type MeResponse struct {
	UserID          string   `json:"user_id"`
	Email           string   `json:"email"`
	Name            string   `json:"name"`
	TenantID        string   `json:"tenant_id"`
	GlobalRoles     []string `json:"global_roles"`
	ServiceRole     string   `json:"service_role"`
	Roles           []string `json:"roles"`
	Permissions     []string `json:"permissions"`
	IsPlatformOwner bool     `json:"is_platform_owner"`
}

// Me returns the caller's projects role and effective permission codes.
func (h *RBACHandler) Me(w http.ResponseWriter, r *http.Request) {
	a := accessOf(r)
	if a == nil || a.Service {
		respondError(w, http.StatusUnauthorized, "a signed-in user is required")
		return
	}
	out := MeResponse{
		UserID:          a.AuthUserID.String(),
		Email:           a.Email,
		TenantID:        a.TenantID.String(),
		GlobalRoles:     a.GlobalRoles,
		ServiceRole:     a.ServiceRole(),
		Roles:           a.Roles,
		Permissions:     a.Effective(),
		IsPlatformOwner: a.Claims != nil && a.Claims.IsPlatformOwner,
	}
	if out.GlobalRoles == nil {
		out.GlobalRoles = []string{}
	}
	if out.Roles == nil {
		out.Roles = []string{}
	}
	if h.svc != nil {
		if u, err := h.svc.User(r.Context(), a.TenantID, a.AuthUserID); err == nil && u != nil {
			out.Name = u.Name
			if out.Email == "" {
				out.Email = u.Email
			}
		}
	}
	respondJSON(w, http.StatusOK, out)
}

// ListRoles returns the system roles with their permission codes and holder counts.
func (h *RBACHandler) ListRoles(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	roles, err := h.svc.ListRoles(r.Context(), tenantID)
	if err != nil {
		serverError(w, h.log, "list roles failed", err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": roles})
}

// ListPermissions returns the permission registry.
func (h *RBACHandler) ListPermissions(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]any{"data": rbac.Catalogue})
}

// ListUsers returns the tenant's projects users with their roles (?limit= up to 500).
func (h *RBACHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	users, err := h.svc.ListUsers(r.Context(), tenantID, limit)
	if err != nil {
		serverError(w, h.log, "list users failed", err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": users})
}

func (h *RBACHandler) roleErr(w http.ResponseWriter, err error, msg string) {
	switch {
	case errors.Is(err, rbac.ErrRoleNotFound):
		respondError(w, http.StatusNotFound, "role not found")
	case errors.Is(err, rbac.ErrNotAssigned):
		respondError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, rbac.ErrSelfChange):
		respondError(w, http.StatusBadRequest, err.Error())
	default:
		serverError(w, h.log, msg, err)
	}
}

// AssignRole gives the user ({userID}, the auth-api user id) a role: body {"role_code": "..."}.
// The user need not have signed in to projects yet.
func (h *RBACHandler) AssignRole(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	userID, ok := userParam(w, r)
	if !ok {
		return
	}
	var body struct {
		RoleCode string `json:"role_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.RoleCode) == "" {
		respondError(w, http.StatusBadRequest, "role_code is required")
		return
	}
	if err := h.svc.AssignRole(r.Context(), tenantID, userID, strings.TrimSpace(body.RoleCode), actorID(r)); err != nil {
		h.roleErr(w, err, "assign role failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RevokeRole removes one role from the user.
func (h *RBACHandler) RevokeRole(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	userID, ok := userParam(w, r)
	if !ok {
		return
	}
	if err := h.svc.RevokeRole(r.Context(), tenantID, userID, chi.URLParam(r, "roleCode"), actorID(r)); err != nil {
		h.roleErr(w, err, "revoke role failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CreateUserRequest provisions an account in auth-api by email (auth generates a temporary
// password for a new account) in the caller's tenant, with global roles below admin.
type CreateUserRequest struct {
	Email string   `json:"email"`
	Roles []string `json:"roles,omitempty"`
}

// CreateUser adds a member to the organisation through auth-api. Admin-tier global roles are given
// in the accounts site only.
func (h *RBACHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Email) == "" {
		respondError(w, http.StatusBadRequest, "email is required")
		return
	}
	for _, role := range req.Roles {
		if adminTierRole(role) {
			respondError(w, http.StatusBadRequest, "admin, owner and superuser roles are given in the accounts site")
			return
		}
	}
	if h.sync == nil {
		respondError(w, http.StatusServiceUnavailable, "user provisioning is not configured")
		return
	}
	resp, err := h.sync.SyncUser(r.Context(), usersync.SyncUserRequest{
		Email: req.Email, TenantID: tenantID, Roles: req.Roles, Service: "projects",
	})
	if err != nil {
		serverError(w, h.log, "auth-api user provisioning failed", err)
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{
		"user_id": resp.UserID, "email": resp.Email, "tenant_id": resp.TenantID, "created": true,
	})
}

// adminTierRole reports a role only an organisation admin may give, in the accounts site.
func adminTierRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "admin", "administrator", "superuser", "superusers", "super_admin", "owner", "tenant_admin", "system_admin":
		return true
	}
	return false
}
