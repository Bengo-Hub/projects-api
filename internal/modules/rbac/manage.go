package rbac

// Role and people management (sprint 17), ported from treasury-api's modules/rbac/roles.go: custom
// roles, copy on write of the default roles, permission sets, the grant ceiling, people with their
// role assignments, deactivation and the audit trail. Every change to a role's permissions clears
// the tenant's whole permission cache, since anyone may hold the role.

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	entpermission "github.com/bengobox/projects-service/internal/ent/permission"
	"github.com/bengobox/projects-service/internal/ent/predicate"
	entprojectsuser "github.com/bengobox/projects-service/internal/ent/projectsuser"
	entaudit "github.com/bengobox/projects-service/internal/ent/rbacauditlog"
	entrole "github.com/bengobox/projects-service/internal/ent/role"
	entroleperm "github.com/bengobox/projects-service/internal/ent/rolepermission"
	entassign "github.com/bengobox/projects-service/internal/ent/userroleassignment"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// User statuses. Only an admin's deactivation sets inactive; sign-in never changes it back.
const (
	StatusActive   = "active"
	StatusInactive = "inactive"
)

// Errors the role management routes turn into a status (see handlers/rbac.go).
var (
	ErrUserNotFound      = errors.New("user not found")
	ErrRoleCodeTaken     = errors.New("a role with this code already exists")
	ErrRoleInUse         = errors.New("people still hold this role; move them to another role first")
	ErrSystemRole        = errors.New("a default role cannot be deleted")
	ErrReservedRole      = errors.New("the projects administrator and client roles are read only and the client role is given by the client portal only")
	ErrSelfLockout       = errors.New("this would remove your own access to manage roles")
	ErrLastAdmin         = errors.New("this is the last active projects administrator; give someone else the role first")
	ErrSelfStatus        = errors.New("you cannot deactivate or reactivate yourself")
	ErrInvalidRole       = errors.New("invalid role details")
	ErrUnknownPermission = errors.New("unknown permission code")
	ErrPortalMix         = errors.New("a role with client portal access cannot also hold staff permissions")
	ErrInvalidExpiry     = errors.New("the end date must be in the future")
	ErrInvalidStatus     = errors.New("status must be active or inactive")
	ErrGrantAbove        = errors.New("you cannot give a role with permissions you do not hold")
	ErrAdminOnly         = errors.New("only an administrator of this organisation can give this role")
)

var roleCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,49}$`)

// reserved roles are read only: projects_admin always holds every code, and client is the portal
// role JIT gives from the portal sign-in. Neither can be created, copied to, customised or deleted.
func reserved(code string) bool { return code == RoleAdmin || code == RoleClient }

func scopeOf(r *ent.Role) string {
	if r.TenantID == nil {
		return "global"
	}
	return "tenant"
}

// notEnded keeps assignments that still grant access: no end date, or one still ahead.
func notEnded() predicate.UserRoleAssignment {
	return entassign.Or(entassign.ExpiresAtIsNil(), entassign.ExpiresAtGT(time.Now()))
}

// roleByCode returns the tenant's own role of a code, else the default (global) role of that code.
func roleByCode(ctx context.Context, roles *ent.RoleClient, tenantID uuid.UUID, code string) (*ent.Role, error) {
	r, err := roles.Query().Where(entrole.RoleCode(code), entrole.TenantID(tenantID)).Only(ctx)
	if err == nil {
		return r, nil
	}
	if !ent.IsNotFound(err) {
		return nil, err
	}
	r, err = roles.Query().Where(entrole.RoleCode(code), entrole.TenantIDIsNil()).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrRoleNotFound
	}
	return r, err
}

// visibleRole reads a role the tenant may see (a default one or its own) with its permissions.
func (s *Service) visibleRole(ctx context.Context, tenantID, roleID uuid.UUID) (*ent.Role, error) {
	r, err := s.client.Role.Query().
		Where(entrole.ID(roleID), entrole.Or(entrole.TenantIDIsNil(), entrole.TenantID(tenantID))).
		WithPermissions().Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrRoleNotFound
	}
	return r, err
}

func codesOf(r *ent.Role) []string {
	out := make([]string, 0, len(r.Edges.Permissions))
	for _, p := range r.Edges.Permissions {
		out = append(out, p.PermissionCode)
	}
	sort.Strings(out)
	return out
}

// CheckCodes cleans a permission set for a custom role: duplicates dropped, every code in the
// registry (an unknown code is refused, since skipping it would save less than the editor showed)
// and client portal access never mixed with staff codes.
func CheckCodes(codes []string) ([]string, error) {
	seen := make(map[string]bool, len(codes))
	out := make([]string, 0, len(codes))
	for _, c := range codes {
		c = strings.TrimSpace(c)
		if c == "" || seen[c] {
			continue
		}
		if !IsCode(c) {
			return nil, fmt.Errorf("%w: %s", ErrUnknownPermission, c)
		}
		seen[c] = true
		out = append(out, c)
	}
	if seen[PermPortalView] && len(out) > 1 {
		return nil, ErrPortalMix
	}
	sort.Strings(out)
	return out, nil
}

func permissionIDs(ctx context.Context, perms *ent.PermissionClient, codes []string) ([]uuid.UUID, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	rows, err := perms.Query().Where(entpermission.PermissionCodeIn(codes...)).All(ctx)
	if err != nil {
		return nil, err
	}
	if len(rows) != len(codes) {
		return nil, fmt.Errorf("%w: the registry is not seeded", ErrUnknownPermission)
	}
	ids := make([]uuid.UUID, len(rows))
	for i, p := range rows {
		ids[i] = p.ID
	}
	return ids, nil
}

// replacePermissions sets a role's permission links to exactly ids.
func replacePermissions(ctx context.Context, tx *ent.Tx, roleID uuid.UUID, ids []uuid.UUID) error {
	if _, err := tx.RolePermission.Delete().Where(entroleperm.RoleID(roleID)).Exec(ctx); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	bulk := make([]*ent.RolePermissionCreate, len(ids))
	for i, id := range ids {
		bulk[i] = tx.RolePermission.Create().SetRoleID(roleID).SetPermissionID(id)
	}
	return tx.RolePermission.CreateBulk(bulk...).Exec(ctx)
}

func (s *Service) invalidateTenant(ctx context.Context, tenantID uuid.UUID) {
	s.cache.InvalidatePattern(ctx, "projects:rbac:"+tenantID.String()+":*")
}

// RoleByID returns one role as ListRoles shows it (for the answer to a role change).
func (s *Service) RoleByID(ctx context.Context, tenantID, roleID uuid.UUID) (RoleView, error) {
	roles, err := s.ListRoles(ctx, tenantID)
	if err != nil {
		return RoleView{}, err
	}
	for _, r := range roles {
		if r.ID == roleID {
			return r, nil
		}
	}
	return RoleView{}, ErrRoleNotFound
}

// RoleInput describes a new role. Code is optional: it is made from the name when empty.
type RoleInput struct {
	Code            string
	Name            string
	Description     *string
	PermissionCodes []string
}

func normalizeRoleCode(code, name string) string {
	if code = strings.TrimSpace(code); code != "" {
		return strings.ToLower(code)
	}
	var b strings.Builder
	for _, c := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteRune(c)
		default:
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "_") {
				b.WriteByte('_')
			}
		}
	}
	out := strings.TrimRight(b.String(), "_")
	if out != "" && (out[0] < 'a' || out[0] > 'z') {
		out = "role_" + out
	}
	return out
}

func validRoleName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return "", fmt.Errorf("%w: a name of up to 100 characters is required", ErrInvalidRole)
	}
	return name, nil
}

func (s *Service) newTenantRole(ctx context.Context, tenantID uuid.UUID, in RoleInput, codes []string) (*ent.Role, error) {
	name, err := validRoleName(in.Name)
	if err != nil {
		return nil, err
	}
	code := normalizeRoleCode(in.Code, name)
	if !roleCodePattern.MatchString(code) {
		return nil, fmt.Errorf("%w: the code must start with a letter and use 2 to 50 lowercase letters, numbers and underscores", ErrInvalidRole)
	}
	if reserved(code) {
		return nil, ErrReservedRole
	}
	// A code in use would shadow that role, the default ones included.
	if _, err := roleByCode(ctx, s.client.Role, tenantID, code); err == nil {
		return nil, ErrRoleCodeTaken
	} else if !errors.Is(err, ErrRoleNotFound) {
		return nil, err
	}
	ids, err := permissionIDs(ctx, s.client.Permission, codes)
	if err != nil {
		return nil, err
	}
	var role *ent.Role
	err = txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		create := tx.Role.Create().SetTenantID(tenantID).SetRoleCode(code).SetName(name).SetIsSystemRole(false)
		if in.Description != nil {
			create.SetDescription(strings.TrimSpace(*in.Description))
		}
		var err error
		if role, err = create.Save(ctx); err != nil {
			return err
		}
		return replacePermissions(ctx, tx, role.ID, ids)
	})
	if ent.IsConstraintError(err) {
		return nil, ErrRoleCodeTaken
	}
	return role, err
}

// CreateCustomRole adds a role of the tenant's own with the given permissions.
func (s *Service) CreateCustomRole(ctx context.Context, tenantID uuid.UUID, in RoleInput) (*ent.Role, error) {
	codes, err := CheckCodes(in.PermissionCodes)
	if err != nil {
		return nil, err
	}
	return s.newTenantRole(ctx, tenantID, in, codes)
}

// CloneRole starts a new tenant role from the permissions of any role the tenant can see.
func (s *Service) CloneRole(ctx context.Context, tenantID, sourceID uuid.UUID, in RoleInput) (*ent.Role, error) {
	source, err := s.visibleRole(ctx, tenantID, sourceID)
	if err != nil {
		return nil, err
	}
	if in.Description == nil && source.Description != "" {
		d := source.Description
		in.Description = &d
	}
	return s.newTenantRole(ctx, tenantID, in, codesOf(source))
}

// copyForTenant gives the tenant its own copy of a default role inside tx: same code, name,
// description and permissions, and this tenant's assignments of the default move to the copy, so
// the edit reaches the people who already hold it and JIT (which resolves the tenant copy first)
// never adds the default back. Other tenants keep the default untouched.
func copyForTenant(ctx context.Context, tx *ent.Tx, tenantID uuid.UUID, global *ent.Role) (*ent.Role, error) {
	copied, err := tx.Role.Create().SetTenantID(tenantID).SetRoleCode(global.RoleCode).SetName(global.Name).
		SetDescription(global.Description).SetIsSystemRole(false).Save(ctx)
	if err != nil {
		return nil, err
	}
	ids, err := tx.Role.QueryPermissions(global).IDs(ctx)
	if err != nil {
		return nil, err
	}
	if err := replacePermissions(ctx, tx, copied.ID, ids); err != nil {
		return nil, err
	}
	if _, err := tx.UserRoleAssignment.Update().
		Where(entassign.TenantID(tenantID), entassign.RoleID(global.ID)).
		SetRoleID(copied.ID).Save(ctx); err != nil {
		return nil, err
	}
	return copied, nil
}

// editRole runs edit on the tenant's own row of a role inside one transaction, copying a default
// role for the tenant first (copy on write). It answers the row edited and whether it was copied
// now. Two first edits racing on one default role: the loser's copy hits the unique index and is
// retried once against the winner's copy.
func (s *Service) editRole(ctx context.Context, tenantID uuid.UUID, visible *ent.Role, edit func(tx *ent.Tx, role *ent.Role) error) (*ent.Role, bool, error) {
	for attempt := 0; ; attempt++ {
		target, copied := visible, false
		err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
			if target.TenantID == nil {
				existing, err := tx.Role.Query().Where(entrole.TenantID(tenantID), entrole.RoleCode(visible.RoleCode)).Only(ctx)
				switch {
				case err == nil:
					target = existing
				case ent.IsNotFound(err):
					if target, err = copyForTenant(ctx, tx, tenantID, visible); err != nil {
						return err
					}
					copied = true
				default:
					return err
				}
			}
			return edit(tx, target)
		})
		if err != nil && attempt == 0 && visible.TenantID == nil && ent.IsConstraintError(err) {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		return target, copied, nil
	}
}

// UpdateRoleDetails renames a role or changes its description, copying a default role first.
func (s *Service) UpdateRoleDetails(ctx context.Context, tenantID, roleID uuid.UUID, name, description *string) (*ent.Role, bool, error) {
	if name != nil {
		clean, err := validRoleName(*name)
		if err != nil {
			return nil, false, err
		}
		name = &clean
	}
	visible, err := s.visibleRole(ctx, tenantID, roleID)
	if err != nil {
		return nil, false, err
	}
	if reserved(visible.RoleCode) {
		return nil, false, ErrReservedRole
	}
	role, copied, err := s.editRole(ctx, tenantID, visible, func(tx *ent.Tx, role *ent.Role) error {
		u := tx.Role.UpdateOneID(role.ID)
		if name != nil {
			u.SetName(*name)
		}
		if description != nil {
			u.SetDescription(strings.TrimSpace(*description))
		}
		return u.Exec(ctx)
	})
	if err != nil {
		return nil, false, err
	}
	if copied {
		s.invalidateTenant(ctx, tenantID)
	}
	// The row as saved, not as it was before the update.
	saved, err := s.client.Role.Get(ctx, role.ID)
	if err != nil {
		return nil, false, err
	}
	return saved, copied, nil
}

// RolePermissionCodes reads a role's current permission codes, for the editor to start from.
func (s *Service) RolePermissionCodes(ctx context.Context, tenantID, roleID uuid.UUID) ([]string, error) {
	role, err := s.visibleRole(ctx, tenantID, roleID)
	if err != nil {
		return nil, err
	}
	return codesOf(role), nil
}

func grantsManage(codes []string) bool { return slices.Contains(codes, PermRBACManage) }

// losesManage reports whether changing roleCode to newCodes (nil when it is taken away) would
// leave the guarded user holding that role without any way to manage roles. guard is nil for a
// caller who passes every permission check anyway (superuser, platform owner).
func (s *Service) losesManage(ctx context.Context, tenantID uuid.UUID, guard *uuid.UUID, roleCode string, newCodes []string) (bool, error) {
	if guard == nil {
		return false, nil
	}
	assignments, err := s.client.UserRoleAssignment.Query().
		Where(entassign.TenantID(tenantID), notEnded(),
			entassign.HasUserWith(entprojectsuser.TenantID(tenantID), entprojectsuser.AuthServiceUserID(*guard))).
		WithRole(func(q *ent.RoleQuery) { q.WithPermissions() }).All(ctx)
	if err != nil {
		return false, err
	}
	holds, keeps := false, false
	for _, a := range assignments {
		if a.Edges.Role == nil {
			continue
		}
		if a.Edges.Role.RoleCode == roleCode {
			holds = true
			keeps = keeps || grantsManage(newCodes)
			continue
		}
		keeps = keeps || grantsManage(codesOf(a.Edges.Role))
	}
	return holds && !keeps, nil
}

// SetRolePermissions replaces a role's permissions, copying a default role for the tenant first.
// It answers the role that now holds them (a new id on the first edit of a default role), the
// codes it had before and whether it was copied now. guard stops the caller removing their own
// access to manage roles.
func (s *Service) SetRolePermissions(ctx context.Context, tenantID uuid.UUID, guard *uuid.UUID, roleID uuid.UUID, codes []string) (*ent.Role, []string, bool, error) {
	codes, err := CheckCodes(codes)
	if err != nil {
		return nil, nil, false, err
	}
	visible, err := s.visibleRole(ctx, tenantID, roleID)
	if err != nil {
		return nil, nil, false, err
	}
	if reserved(visible.RoleCode) {
		return nil, nil, false, ErrReservedRole
	}
	if lose, err := s.losesManage(ctx, tenantID, guard, visible.RoleCode, codes); err != nil {
		return nil, nil, false, err
	} else if lose {
		return nil, nil, false, ErrSelfLockout
	}
	ids, err := permissionIDs(ctx, s.client.Permission, codes)
	if err != nil {
		return nil, nil, false, err
	}
	var before []string
	role, copied, err := s.editRole(ctx, tenantID, visible, func(tx *ent.Tx, role *ent.Role) error {
		var err error
		if before, err = tx.Role.QueryPermissions(role).Select(entpermission.FieldPermissionCode).Strings(ctx); err != nil {
			return err
		}
		sort.Strings(before)
		return replacePermissions(ctx, tx, role.ID, ids)
	})
	if err != nil {
		return nil, nil, false, err
	}
	s.invalidateTenant(ctx, tenantID)
	return role, before, copied, nil
}

// DeleteRole removes one of the tenant's own roles once no active person holds it. Assignments that
// ended, or belong to deactivated people, go with it. A tenant copy of a default role may be
// deleted the same way, which returns the tenant to the default.
func (s *Service) DeleteRole(ctx context.Context, tenantID, roleID uuid.UUID) (*ent.Role, error) {
	role, err := s.visibleRole(ctx, tenantID, roleID)
	if err != nil {
		return nil, err
	}
	if role.TenantID == nil || role.IsSystemRole {
		return nil, ErrSystemRole
	}
	held, err := s.client.UserRoleAssignment.Query().
		Where(entassign.TenantID(tenantID), entassign.RoleID(role.ID), notEnded(),
			entassign.HasUserWith(entprojectsuser.Status(StatusActive))).Exist(ctx)
	if err != nil {
		return nil, err
	}
	if held {
		return nil, ErrRoleInUse
	}
	err = txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		if _, err := tx.UserRoleAssignment.Delete().Where(entassign.RoleID(role.ID)).Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.RolePermission.Delete().Where(entroleperm.RoleID(role.ID)).Exec(ctx); err != nil {
			return err
		}
		return tx.Role.DeleteOneID(role.ID).Exec(ctx)
	})
	if err != nil {
		return nil, err
	}
	s.invalidateTenant(ctx, tenantID)
	return role, nil
}

// CheckGrant refuses a role the caller may not give (treasury's checkGrant): the client role
// always, projects_admin and any role carrying projects.projects.manage (which lifts membership
// scoping) unless the caller is admin tier, and any role with a permission the caller does not
// hold. callerPerms are the caller's effective codes; adminTier passes the ceiling.
func (s *Service) CheckGrant(ctx context.Context, tenantID uuid.UUID, roleCode string, callerPerms []string, adminTier bool) error {
	if roleCode == RoleClient {
		return ErrReservedRole
	}
	role, err := roleByCode(ctx, s.client.Role, tenantID, roleCode)
	if err != nil {
		return err
	}
	if adminTier {
		return nil
	}
	if roleCode == RoleAdmin {
		return ErrAdminOnly
	}
	codes, err := s.client.Role.QueryPermissions(role).Select(entpermission.FieldPermissionCode).Strings(ctx)
	if err != nil {
		return err
	}
	granted := Expand(codes)
	if slices.Contains(granted, PermProjectsManage) {
		return ErrAdminOnly
	}
	have := Expand(callerPerms)
	for _, c := range granted {
		if !slices.Contains(have, c) {
			return ErrGrantAbove
		}
	}
	return nil
}

// AssignmentView is one role a person holds or held. automatic marks the default role sign-in gave
// them (assigned by themselves), which follows their global role until an admin decides.
type AssignmentView struct {
	RoleID     uuid.UUID  `json:"role_id"`
	RoleCode   string     `json:"role_code"`
	Name       string     `json:"name"`
	AssignedAt time.Time  `json:"assigned_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	Expired    bool       `json:"expired"`
	Automatic  bool       `json:"automatic"`
}

// UserView is a person with their roles for the team and role screens. roles lists the codes that
// still grant access; assignments lists every role held or held before, with end dates.
type UserView struct {
	AuthUserID    uuid.UUID        `json:"user_id"`
	Email         string           `json:"email,omitempty"`
	Name          string           `json:"name,omitempty"`
	Status        string           `json:"status"`
	DeactivatedAt *time.Time       `json:"deactivated_at,omitempty"`
	LastSeenAt    *time.Time       `json:"last_seen_at,omitempty"`
	Roles         []string         `json:"roles"`
	Assignments   []AssignmentView `json:"assignments"`
}

// UserDetail is a person with their effective permissions.
type UserDetail struct {
	UserView
	Permissions []string `json:"permissions"`
}

func userView(u *ent.ProjectsUser, now time.Time) UserView {
	v := UserView{AuthUserID: u.AuthServiceUserID, Email: u.Email, Name: u.Name, Status: u.Status,
		DeactivatedAt: u.DeactivatedAt, LastSeenAt: u.LastSyncAt, Roles: []string{}, Assignments: []AssignmentView{}}
	for _, a := range u.Edges.RoleAssignments {
		if a.Edges.Role == nil {
			continue
		}
		expired := a.ExpiresAt != nil && !a.ExpiresAt.After(now)
		v.Assignments = append(v.Assignments, AssignmentView{
			RoleID: a.RoleID, RoleCode: a.Edges.Role.RoleCode, Name: a.Edges.Role.Name, AssignedAt: a.AssignedAt,
			ExpiresAt: a.ExpiresAt, Expired: expired, Automatic: a.AssignedBy == u.AuthServiceUserID,
		})
		if !expired {
			v.Roles = append(v.Roles, a.Edges.Role.RoleCode)
		}
	}
	sort.SliceStable(v.Roles, func(i, j int) bool { return RoleRank(v.Roles[i]) < RoleRank(v.Roles[j]) })
	sort.SliceStable(v.Assignments, func(i, j int) bool {
		if v.Assignments[i].Expired != v.Assignments[j].Expired {
			return !v.Assignments[i].Expired
		}
		return RoleRank(v.Assignments[i].RoleCode) < RoleRank(v.Assignments[j].RoleCode)
	})
	return v
}

// ListUsersPaged pages the tenant's people (name order) with their roles. search matches the name
// or the email in any case.
func (s *Service) ListUsersPaged(ctx context.Context, tenantID uuid.UUID, search string, limit, offset int) ([]UserView, int, error) {
	where := []predicate.ProjectsUser{entprojectsuser.TenantID(tenantID)}
	if search = strings.TrimSpace(search); search != "" {
		where = append(where, entprojectsuser.Or(entprojectsuser.NameContainsFold(search), entprojectsuser.EmailContainsFold(search)))
	}
	q := s.client.ProjectsUser.Query().Where(where...)
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	users, err := q.WithRoleAssignments(func(aq *ent.UserRoleAssignmentQuery) { aq.WithRole() }).
		Order(ent.Asc(entprojectsuser.FieldName), ent.Asc(entprojectsuser.FieldEmail), ent.Asc(entprojectsuser.FieldID)).
		Limit(limit).Offset(offset).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	now := time.Now()
	out := make([]UserView, 0, len(users))
	for _, u := range users {
		out = append(out, userView(u, now))
	}
	return out, total, nil
}

// UserDetail returns one person with their roles and effective permissions.
func (s *Service) UserDetail(ctx context.Context, tenantID, authUserID uuid.UUID) (UserDetail, error) {
	u, err := s.client.ProjectsUser.Query().
		Where(entprojectsuser.TenantID(tenantID), entprojectsuser.AuthServiceUserID(authUserID)).
		WithRoleAssignments(func(aq *ent.UserRoleAssignmentQuery) { aq.WithRole() }).Only(ctx)
	if ent.IsNotFound(err) {
		return UserDetail{}, ErrUserNotFound
	}
	if err != nil {
		return UserDetail{}, err
	}
	res, err := s.Resolve(ctx, tenantID, authUserID)
	if err != nil {
		return UserDetail{}, err
	}
	return UserDetail{UserView: userView(u, time.Now()), Permissions: res.Perms}, nil
}

// lastActiveAdmin reports whether authUserID is the tenant's only active person still holding
// projects_admin.
func (s *Service) lastActiveAdmin(ctx context.Context, tenantID, authUserID uuid.UUID) (bool, error) {
	admins, err := s.client.ProjectsUser.Query().
		Where(entprojectsuser.TenantID(tenantID), entprojectsuser.Status(StatusActive),
			entprojectsuser.HasRoleAssignmentsWith(entassign.TenantID(tenantID), notEnded(),
				entassign.HasRoleWith(entrole.RoleCode(RoleAdmin)))).
		Select(entprojectsuser.FieldAuthServiceUserID).All(ctx)
	if err != nil {
		return false, err
	}
	holds := false
	for _, a := range admins {
		if a.AuthServiceUserID == authUserID {
			holds = true
		}
	}
	return holds && len(admins) == 1, nil
}

// SetUserStatus deactivates (inactive) or reactivates (active) a person in the tenant. A
// deactivated person keeps their roles but gets no permissions until reactivated; sign-in never
// reactivates them. Nobody changes their own status, and the last active projects administrator
// stays active. It answers whether anything changed.
func (s *Service) SetUserStatus(ctx context.Context, tenantID, authUserID uuid.UUID, status string, actor uuid.UUID) (bool, error) {
	if status != StatusActive && status != StatusInactive {
		return false, ErrInvalidStatus
	}
	if actor == authUserID {
		return false, ErrSelfStatus
	}
	u, err := s.client.ProjectsUser.Query().
		Where(entprojectsuser.TenantID(tenantID), entprojectsuser.AuthServiceUserID(authUserID)).Only(ctx)
	if ent.IsNotFound(err) {
		return false, ErrUserNotFound
	}
	if err != nil {
		return false, err
	}
	if u.Status == status {
		return false, nil
	}
	upd := u.Update().SetStatus(status)
	if status == StatusInactive {
		last, err := s.lastActiveAdmin(ctx, tenantID, authUserID)
		if err != nil {
			return false, err
		}
		if last {
			return false, ErrLastAdmin
		}
		upd.SetDeactivatedAt(time.Now()).SetDeactivatedBy(actor)
	} else {
		upd.ClearDeactivatedAt().ClearDeactivatedBy()
	}
	if err := upd.Exec(ctx); err != nil {
		return false, err
	}
	s.invalidate(ctx, tenantID, authUserID)
	return true, nil
}

// FindUserByEmail returns the tenant's local user with this email (any case), or nil.
func (s *Service) FindUserByEmail(ctx context.Context, tenantID uuid.UUID, email string) (*ent.ProjectsUser, error) {
	u, err := s.client.ProjectsUser.Query().
		Where(entprojectsuser.TenantID(tenantID), entprojectsuser.EmailEqualFold(strings.TrimSpace(email))).
		First(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	return u, err
}

// ProvisionInvited records an invited person locally (their email, before they ever sign in) and
// gives them roleCode, granted by actor.
func (s *Service) ProvisionInvited(ctx context.Context, tenantID, authUserID uuid.UUID, email, roleCode string, actor uuid.UUID) (*ent.Role, error) {
	if _, err := s.upsertUser(ctx, tenantID, authUserID, strings.TrimSpace(email), ""); err != nil {
		return nil, err
	}
	return s.AssignRoleUntil(ctx, tenantID, authUserID, roleCode, actor, nil)
}

// Users is the local user table, for the people search (services/people).
func (s *Service) Users() *ent.ProjectsUserClient { return s.client.ProjectsUser }

// AuditEntry is one role or access change to record.
type AuditEntry struct {
	TenantID     uuid.UUID
	ActorID      uuid.UUID
	ActorEmail   string
	Action       string
	ResourceType string // role or user
	ResourceID   string
	Changes      map[string]any
	IP           string
}

// Audit records a change. It never fails the request that made the change: a write error is logged.
func (s *Service) Audit(ctx context.Context, e AuditEntry) {
	if e.TenantID == uuid.Nil || e.ActorID == uuid.Nil {
		return
	}
	create := s.client.RBACAuditLog.Create().SetTenantID(e.TenantID).SetActorUserID(e.ActorID).
		SetAction(e.Action).SetResourceType(e.ResourceType).SetResourceID(e.ResourceID)
	if e.ActorEmail != "" {
		create.SetActorEmail(truncate(e.ActorEmail, 254))
	}
	if e.Changes != nil {
		create.SetChanges(e.Changes)
	}
	if e.IP != "" {
		create.SetIPAddress(truncate(e.IP, 64))
	}
	if err := create.Exec(ctx); err != nil {
		s.log.Warn("rbac audit write failed", zap.String("action", e.Action), zap.String("resource_id", e.ResourceID), zap.Error(err))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// AuditLogs pages the tenant's role and access changes, newest first.
func (s *Service) AuditLogs(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]*ent.RBACAuditLog, int, error) {
	q := s.client.RBACAuditLog.Query().Where(entaudit.TenantID(tenantID))
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	rows, err := q.Order(ent.Desc(entaudit.FieldCreatedAt), ent.Desc(entaudit.FieldID)).Limit(limit).Offset(offset).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}
