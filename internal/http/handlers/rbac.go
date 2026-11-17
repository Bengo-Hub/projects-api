package handlers

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Bengo-Hub/pagination"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/http/authz"
	"github.com/bengobox/projects-service/internal/modules/rbac"
	"github.com/bengobox/projects-service/internal/services/people"
	"github.com/bengobox/projects-service/internal/services/usersync"
)

// RBACHandler serves the caller's projects profile (auth/me), the team and role management
// routes (sprint 17, ported from treasury-api), the people picker and the invite.
type RBACHandler struct {
	log  *zap.Logger
	svc  *rbac.Service
	sync *usersync.Service
}

// NewRBACHandler creates the handler. sync provisions accounts in auth-api (POST /users).
func NewRBACHandler(log *zap.Logger, svc *rbac.Service, sync *usersync.Service) *RBACHandler {
	return &RBACHandler{log: log.Named("rbac.handler"), svc: svc, sync: sync}
}

// RegisterRoutes mounts GET /auth/me for every signed-in user, the people picker, and the role
// management routes, all gated on projects.rbac.manage. Changing a role, revoking one and changing
// a person's status also need admin tier (authz.RequireAdminTier); giving a role is held to the
// caller's own permissions (rbac.CheckGrant).
func (h *RBACHandler) RegisterRoutes(r chi.Router) {
	r.Get("/auth/me", h.Me)
	manage := authz.Require(rbac.PermRBACManage)
	// The people picker serves every screen that adds a person: the project team
	// (members.manage), the invite and roles (rbac.manage) and tender committees
	// (tenders.manage, which the committee routes need and project_manager does not hold).
	r.With(authz.Require(rbac.PermMembersManage, rbac.PermRBACManage, rbac.PermTendersManage)).Get("/people", h.People)
	r.Route("/rbac", func(g chi.Router) {
		g.Use(manage)
		g.Get("/roles", h.ListRoles)
		g.Get("/roles/{roleID}/permissions", h.GetRolePermissions)
		g.Get("/permissions", h.ListPermissions)
		g.Get("/users", h.ListUsers)
		g.Get("/users/{userID}", h.GetUser)
		g.Get("/audit-logs", h.ListAuditLogs)
		g.Post("/users/{userID}/roles", h.AssignRole)
		g.Group(func(admin chi.Router) {
			admin.Use(authz.RequireAdminTier)
			admin.Post("/roles", h.CreateRole)
			admin.Post("/roles/{roleID}/clone", h.CloneRole)
			admin.Patch("/roles/{roleID}", h.UpdateRole)
			admin.Put("/roles/{roleID}/permissions", h.SetRolePermissions)
			admin.Delete("/roles/{roleID}", h.DeleteRole)
			admin.Delete("/users/{userID}/roles/{roleCode}", h.RevokeRole)
			admin.Patch("/users/{userID}", h.SetUserStatus)
		})
	})
	r.With(manage, authz.RequireAdminTier).Post("/users", h.CreateUser)
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

// Me returns the caller's projects role and effective permission codes. When the permissions could
// not be read it answers 503 (code permissions_unavailable), never an empty profile the UI would
// take as "no access".
func (h *RBACHandler) Me(w http.ResponseWriter, r *http.Request) {
	a := accessOf(r)
	if a == nil || a.Service {
		respondError(w, http.StatusUnauthorized, "a signed-in user is required")
		return
	}
	if a.ResolveErr != nil {
		authz.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "your permissions could not be read; try again shortly", "code": "permissions_unavailable",
		})
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

// ready answers 503 when the handler has no service (tests and a misconfigured start).
func (h *RBACHandler) ready(w http.ResponseWriter) bool {
	if h.svc == nil {
		respondError(w, http.StatusServiceUnavailable, "role management is not available")
		return false
	}
	return true
}

func (h *RBACHandler) roleErr(w http.ResponseWriter, err error, msg string) {
	switch {
	case errors.Is(err, rbac.ErrRoleNotFound), errors.Is(err, rbac.ErrUserNotFound):
		respondError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, rbac.ErrNotAssigned):
		respondError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, rbac.ErrSelfChange):
		respondError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, rbac.ErrRoleCodeTaken), errors.Is(err, rbac.ErrRoleInUse), errors.Is(err, rbac.ErrSystemRole),
		errors.Is(err, rbac.ErrReservedRole), errors.Is(err, rbac.ErrSelfLockout), errors.Is(err, rbac.ErrLastAdmin),
		errors.Is(err, rbac.ErrSelfStatus):
		respondError(w, http.StatusConflict, err.Error())
	case errors.Is(err, rbac.ErrInvalidRole), errors.Is(err, rbac.ErrUnknownPermission), errors.Is(err, rbac.ErrPortalMix),
		errors.Is(err, rbac.ErrInvalidExpiry), errors.Is(err, rbac.ErrInvalidStatus):
		respondError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, rbac.ErrGrantAbove), errors.Is(err, rbac.ErrAdminOnly):
		authz.WriteJSON(w, http.StatusForbidden, map[string]string{"error": err.Error(), "code": "grant_refused"})
	default:
		serverError(w, h.log, msg, err)
	}
}

// clientIP is the caller's address; the router's TrustedRealIP has already set RemoteAddr from a
// trusted proxy header.
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// audit records a role or access change by the caller (best effort).
func (h *RBACHandler) audit(r *http.Request, action, resourceType, resourceID string, changes map[string]any) {
	a := accessOf(r)
	if a == nil || h.svc == nil {
		return
	}
	h.svc.Audit(r.Context(), rbac.AuditEntry{
		TenantID: a.TenantID, ActorID: a.AuthUserID, ActorEmail: a.Email, Action: action,
		ResourceType: resourceType, ResourceID: resourceID, Changes: changes, IP: clientIP(r),
	})
}

func roleParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "roleID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid role id")
		return uuid.Nil, false
	}
	return id, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, into any) bool {
	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

// People lists the tenant's staff for pickers (the project team's add member, the invite, tender
// committee members): active people who have signed in or were invited, never portal clients.
// ?search= (or q) matches name or email; paged with limit and page. Needs projects.members.manage,
// projects.rbac.manage or projects.tenders.manage.
func (h *RBACHandler) People(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok || !h.ready(w) {
		return
	}
	p := pagination.Parse(r)
	list, total, err := people.Search(r.Context(), h.svc.Users(), tenantID, pagination.SearchTerm(r), p.Limit, p.Offset)
	if err != nil {
		serverError(w, h.log, "list people failed", err)
		return
	}
	items := make([]PersonItem, 0, len(list))
	for _, person := range list {
		items = append(items, PersonItem{UserID: person.ID, Name: person.Label(), Email: person.Email})
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(items, total, p))
}

// ListRoles returns the roles the tenant sees with their codes, scope, holders and whether a
// default role was customised.
func (h *RBACHandler) ListRoles(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok || !h.ready(w) {
		return
	}
	roles, err := h.svc.ListRoles(r.Context(), tenantID)
	if err != nil {
		serverError(w, h.log, "list roles failed", err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": roles})
}

// ListPermissions returns the permission registry, each code with the codes that imply it.
func (h *RBACHandler) ListPermissions(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]any{"data": rbac.PermissionViews()})
}

// usersPageDefault is GET /rbac/users' page size when no limit is sent (max 500).
const usersPageDefault = 50

// ListUsers pages the tenant's people with their roles and role end dates (?search=, page, limit).
func (h *RBACHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok || !h.ready(w) {
		return
	}
	p := pagination.Parse(r)
	if !r.URL.Query().Has("limit") {
		p.Limit = usersPageDefault
		p.Offset = (p.Page - 1) * p.Limit
	}
	users, total, err := h.svc.ListUsersPaged(r.Context(), tenantID, pagination.SearchTerm(r), p.Limit, p.Offset)
	if err != nil {
		serverError(w, h.log, "list users failed", err)
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(users, total, p))
}

// GetUser returns one person with their roles and effective permissions.
func (h *RBACHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok || !h.ready(w) {
		return
	}
	userID, ok := userParam(w, r)
	if !ok {
		return
	}
	u, err := h.svc.UserDetail(r.Context(), tenantID, userID)
	if err != nil {
		h.roleErr(w, err, "read user failed")
		return
	}
	respondJSON(w, http.StatusOK, u)
}

// AssignRole gives the user ({userID}, the auth-api user id) a role: body {"role_code", "expires_at"
// (optional, RFC 3339, in the future)}. Giving a role they hold renews it with the new end date.
// The user need not have signed in yet. Refused (403 grant_refused) above the caller's own
// permissions unless the caller is admin tier.
func (h *RBACHandler) AssignRole(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok || !h.ready(w) {
		return
	}
	userID, ok := userParam(w, r)
	if !ok {
		return
	}
	var body struct {
		RoleCode  string     `json:"role_code"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.RoleCode) == "" {
		respondError(w, http.StatusBadRequest, "role_code is required")
		return
	}
	code := strings.TrimSpace(body.RoleCode)
	a := accessOf(r)
	if err := h.svc.CheckGrant(r.Context(), tenantID, code, a.Effective(), a.AdminTier()); err != nil {
		h.roleErr(w, err, "assign role failed")
		return
	}
	role, err := h.svc.AssignRoleUntil(r.Context(), tenantID, userID, code, actorID(r), body.ExpiresAt)
	if err != nil {
		h.roleErr(w, err, "assign role failed")
		return
	}
	h.audit(r, "role.assigned", "user", userID.String(), map[string]any{
		"role_id": role.ID, "role_code": role.RoleCode, "name": role.Name, "expires_at": body.ExpiresAt,
	})
	w.WriteHeader(http.StatusNoContent)
}

// RevokeRole removes one role from the user (admin tier). The last active projects administrator
// keeps the role.
func (h *RBACHandler) RevokeRole(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok || !h.ready(w) {
		return
	}
	userID, ok := userParam(w, r)
	if !ok {
		return
	}
	code := chi.URLParam(r, "roleCode")
	if err := h.svc.RevokeRole(r.Context(), tenantID, userID, code, actorID(r)); err != nil {
		h.roleErr(w, err, "revoke role failed")
		return
	}
	h.audit(r, "role.revoked", "user", userID.String(), map[string]any{"role_code": code})
	w.WriteHeader(http.StatusNoContent)
}

// SetUserStatus deactivates or reactivates a person (admin tier): body {"status": "active" |
// "inactive"}. Never oneself, never the last active projects administrator.
func (h *RBACHandler) SetUserStatus(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok || !h.ready(w) {
		return
	}
	userID, ok := userParam(w, r)
	if !ok {
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	status := strings.ToLower(strings.TrimSpace(body.Status))
	changed, err := h.svc.SetUserStatus(r.Context(), tenantID, userID, status, actorID(r))
	if err != nil {
		h.roleErr(w, err, "change user status failed")
		return
	}
	if changed {
		action := "user.reactivated"
		if status == rbac.StatusInactive {
			action = "user.deactivated"
		}
		h.audit(r, action, "user", userID.String(), map[string]any{"status": status})
	}
	u, err := h.svc.UserDetail(r.Context(), tenantID, userID)
	if err != nil {
		h.roleErr(w, err, "read user failed")
		return
	}
	respondJSON(w, http.StatusOK, u)
}

// roleRequest is the body of a new or copied role.
type roleRequest struct {
	RoleCode        string   `json:"role_code"`
	Name            string   `json:"name"`
	Description     *string  `json:"description"`
	PermissionCodes []string `json:"permission_codes"`
}

func (req roleRequest) input() rbac.RoleInput {
	return rbac.RoleInput{Code: req.RoleCode, Name: req.Name, Description: req.Description, PermissionCodes: req.PermissionCodes}
}

// respondRole answers with the role as GET /rbac/roles shows it.
func (h *RBACHandler) respondRole(w http.ResponseWriter, r *http.Request, status int, roleID uuid.UUID) {
	tenantID, _ := parseTenant(w, r)
	view, err := h.svc.RoleByID(r.Context(), tenantID, roleID)
	if err != nil {
		h.roleErr(w, err, "read role failed")
		return
	}
	respondJSON(w, status, view)
}

// CreateRole adds a role of the tenant's own (admin tier): {name, role_code?, description?,
// permission_codes}.
func (h *RBACHandler) CreateRole(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok || !h.ready(w) {
		return
	}
	var req roleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	role, err := h.svc.CreateCustomRole(r.Context(), tenantID, req.input())
	if err != nil {
		h.roleErr(w, err, "create role failed")
		return
	}
	h.audit(r, "role.created", "role", role.ID.String(), map[string]any{
		"role_code": role.RoleCode, "name": role.Name, "permission_codes": req.PermissionCodes,
	})
	h.respondRole(w, r, http.StatusCreated, role.ID)
}

// CloneRole starts a new role from any role the tenant sees (admin tier): {name, role_code?,
// description?}.
func (h *RBACHandler) CloneRole(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok || !h.ready(w) {
		return
	}
	sourceID, ok := roleParam(w, r)
	if !ok {
		return
	}
	var req roleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	role, err := h.svc.CloneRole(r.Context(), tenantID, sourceID, req.input())
	if err != nil {
		h.roleErr(w, err, "copy role failed")
		return
	}
	h.audit(r, "role.cloned", "role", role.ID.String(), map[string]any{
		"role_code": role.RoleCode, "name": role.Name, "source_role_id": sourceID,
	})
	h.respondRole(w, r, http.StatusCreated, role.ID)
}

// UpdateRole renames a role or changes its description (admin tier). A default role is copied for
// the tenant first.
func (h *RBACHandler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok || !h.ready(w) {
		return
	}
	roleID, ok := roleParam(w, r)
	if !ok {
		return
	}
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	role, copied, err := h.svc.UpdateRoleDetails(r.Context(), tenantID, roleID, req.Name, req.Description)
	if err != nil {
		h.roleErr(w, err, "update role failed")
		return
	}
	if copied {
		h.audit(r, "role.customized", "role", role.ID.String(), map[string]any{"role_code": role.RoleCode, "source_role_id": roleID})
	}
	h.audit(r, "role.updated", "role", role.ID.String(), map[string]any{
		"role_code": role.RoleCode, "name": req.Name, "description": req.Description,
	})
	h.respondRole(w, r, http.StatusOK, role.ID)
}

// GetRolePermissions reads a role's current permission codes.
func (h *RBACHandler) GetRolePermissions(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok || !h.ready(w) {
		return
	}
	roleID, ok := roleParam(w, r)
	if !ok {
		return
	}
	codes, err := h.svc.RolePermissionCodes(r.Context(), tenantID, roleID)
	if err != nil {
		h.roleErr(w, err, "read role failed")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"permission_codes": codes})
}

// SetRolePermissions replaces a role's permissions (admin tier): {permission_codes}. The first edit
// of a default role makes the tenant's copy (a new id; the UI selects roles by code) and moves the
// tenant's holders to it. Nobody removes their own projects.rbac.manage.
func (h *RBACHandler) SetRolePermissions(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok || !h.ready(w) {
		return
	}
	roleID, ok := roleParam(w, r)
	if !ok {
		return
	}
	var req struct {
		PermissionCodes []string `json:"permission_codes"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	// An absent list is a broken client; saving it would strip the role of everything.
	if req.PermissionCodes == nil {
		respondError(w, http.StatusUnprocessableEntity, "permission_codes is required")
		return
	}
	a := accessOf(r)
	var guard *uuid.UUID
	if !a.Bypass {
		id := a.AuthUserID
		guard = &id
	}
	role, before, copied, err := h.svc.SetRolePermissions(r.Context(), tenantID, guard, roleID, req.PermissionCodes)
	if err != nil {
		h.roleErr(w, err, "save role permissions failed")
		return
	}
	if copied {
		h.audit(r, "role.customized", "role", role.ID.String(), map[string]any{"role_code": role.RoleCode, "source_role_id": roleID})
	}
	after, _ := rbac.CheckCodes(req.PermissionCodes)
	h.audit(r, "role.permissions_updated", "role", role.ID.String(), map[string]any{
		"role_code": role.RoleCode, "name": role.Name, "before": before, "after": after,
	})
	h.respondRole(w, r, http.StatusOK, role.ID)
}

// DeleteRole removes one of the tenant's own roles (admin tier); 409 for a default role or one an
// active person still holds.
func (h *RBACHandler) DeleteRole(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok || !h.ready(w) {
		return
	}
	roleID, ok := roleParam(w, r)
	if !ok {
		return
	}
	role, err := h.svc.DeleteRole(r.Context(), tenantID, roleID)
	if err != nil {
		h.roleErr(w, err, "delete role failed")
		return
	}
	h.audit(r, "role.deleted", "role", role.ID.String(), map[string]any{"role_code": role.RoleCode, "name": role.Name})
	w.WriteHeader(http.StatusNoContent)
}

// ListAuditLogs pages the tenant's role and access changes, newest first.
func (h *RBACHandler) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok || !h.ready(w) {
		return
	}
	p := pagination.Parse(r)
	rows, total, err := h.svc.AuditLogs(r.Context(), tenantID, p.Limit, p.Offset)
	if err != nil {
		serverError(w, h.log, "list audit logs failed", err)
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(rows, total, p))
}

// CreateUserRequest invites a person by email. role_code is the projects role they get (default
// member); roles are optional global roles for auth-api, never admin-like ones.
type CreateUserRequest struct {
	Email    string   `json:"email"`
	RoleCode string   `json:"role_code,omitempty"`
	Roles    []string `json:"roles,omitempty"`
}

// CreateUser invites a person into the organisation from projects (rbac.manage and admin tier):
// auth-api's tenant member route resolves or creates the account by email (a new account gets a
// temporary password from auth-api), then the local user is recorded with the projects role. A
// person already known here in this tenant is not sent to auth-api again (its member route replaces
// global roles), only given the role.
func (h *RBACHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok || !h.ready(w) {
		return
	}
	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Email) == "" {
		respondError(w, http.StatusBadRequest, "email is required")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if at := strings.Index(email, "@"); at < 1 || at == len(email)-1 || strings.ContainsAny(email, " \t") {
		respondError(w, http.StatusUnprocessableEntity, "enter a valid email address")
		return
	}
	for _, role := range req.Roles {
		if adminTierRole(role) {
			respondError(w, http.StatusBadRequest, "admin, owner and superuser roles are given in the accounts site")
			return
		}
	}
	code := strings.TrimSpace(req.RoleCode)
	if code == "" {
		code = rbac.RoleMember
	}
	a := accessOf(r)
	if err := h.svc.CheckGrant(r.Context(), tenantID, code, a.Effective(), a.AdminTier()); err != nil {
		h.roleErr(w, err, "invite failed")
		return
	}
	var authUserID uuid.UUID
	created := false
	known, err := h.svc.FindUserByEmail(r.Context(), tenantID, email)
	if err != nil {
		serverError(w, h.log, "invite lookup failed", err)
		return
	}
	if known != nil {
		authUserID = known.AuthServiceUserID
	} else {
		if h.sync == nil {
			respondError(w, http.StatusServiceUnavailable, "user provisioning is not configured")
			return
		}
		resp, err := h.sync.SyncUser(r.Context(), usersync.SyncUserRequest{
			Email: email, TenantID: tenantID, Roles: req.Roles, Service: "projects",
		})
		if err != nil {
			h.log.Error("auth-api user provisioning failed", zap.Error(err))
			respondError(w, http.StatusBadGateway, "the account service could not add this person")
			return
		}
		authUserID, created = resp.UserID, resp.Created
	}
	role, err := h.svc.ProvisionInvited(r.Context(), tenantID, authUserID, email, code, actorID(r))
	if err != nil {
		h.roleErr(w, err, "invite failed")
		return
	}
	h.audit(r, "user.invited", "user", authUserID.String(), map[string]any{
		"email": email, "role_code": role.RoleCode, "name": role.Name, "created": created, "already_here": known != nil,
	})
	respondJSON(w, http.StatusCreated, map[string]any{
		"user_id": authUserID, "email": email, "tenant_id": tenantID, "created": created, "role_code": role.RoleCode,
	})
}

// adminTierRole reports a global role only an organisation admin may give, in the accounts site.
func adminTierRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "admin", "administrator", "superuser", "superusers", "super_admin", "owner", "tenant_admin", "system_admin":
		return true
	}
	return false
}
