package rbac

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	entpermission "github.com/bengobox/projects-service/internal/ent/permission"
	entprojectsuser "github.com/bengobox/projects-service/internal/ent/projectsuser"
	entrole "github.com/bengobox/projects-service/internal/ent/role"
	entroleperm "github.com/bengobox/projects-service/internal/ent/rolepermission"
	entassign "github.com/bengobox/projects-service/internal/ent/userroleassignment"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// cacheTTL bounds how stale another pod's view of a user's permissions can be after a role
// change; this pod's own changes invalidate at once.
const cacheTTL = 60 * time.Second

// ensureEvery throttles JIT writes per pod: a user is re-checked at most this often.
const ensureEvery = 5 * time.Minute

// Errors returned by role management (more in manage.go).
var (
	ErrRoleNotFound = errors.New("role not found")
	ErrSelfChange   = errors.New("you cannot change your own roles")
	ErrNotAssigned  = errors.New("the user does not hold this role")
)

// Resolved is a user's projects roles and effective permission codes in one tenant.
type Resolved struct {
	Roles []string `json:"roles"`
	Perms []string `json:"permissions"`
}

// Service seeds the registry and resolves users, roles and permissions.
type Service struct {
	client *ent.Client
	cache  *sharedcache.Aside
	log    *zap.Logger
	seen   *sharedcache.Local[string, bool]
}

// NewService creates the RBAC service. cache may be nil (no Redis: every check reads the DB).
func NewService(client *ent.Client, cache *sharedcache.Aside, log *zap.Logger) *Service {
	return &Service{client: client, cache: cache, log: log.Named("rbac"), seen: sharedcache.NewLocal[string, bool](20000, ensureEvery)}
}

func permsKey(tenantID, authUserID uuid.UUID) string {
	return "projects:rbac:" + tenantID.String() + ":" + authUserID.String()
}

// Seed upserts the permission registry and the system roles with their permission sets. Idempotent
// and safe to run from several pods at once: it only adds what is missing and removes what the
// registry no longer grants.
func (s *Service) Seed(ctx context.Context) error {
	permIDs := make(map[string]uuid.UUID, len(Catalogue))
	for _, p := range Catalogue {
		id, err := s.client.Permission.Create().
			SetPermissionCode(p.Code).SetName(p.Name).SetModule(p.Module).SetAction(p.Action).
			OnConflictColumns(entpermission.FieldPermissionCode).
			UpdateName().UpdateModule().UpdateAction().
			ID(ctx)
		if err != nil {
			return fmt.Errorf("rbac seed permission %s: %w", p.Code, err)
		}
		permIDs[p.Code] = id
	}
	for _, r := range Roles {
		role, err := s.client.Role.Query().Where(entrole.RoleCode(r.Code), entrole.TenantIDIsNil()).Only(ctx)
		switch {
		case ent.IsNotFound(err):
			role, err = s.client.Role.Create().SetRoleCode(r.Code).SetName(r.Name).
				SetDescription(r.Description).SetIsSystemRole(true).Save(ctx)
			if ent.IsConstraintError(err) {
				// Another pod created it a moment ago.
				role, err = s.client.Role.Query().Where(entrole.RoleCode(r.Code), entrole.TenantIDIsNil()).Only(ctx)
			}
		case err == nil:
			role, err = role.Update().SetName(r.Name).SetDescription(r.Description).SetIsSystemRole(true).Save(ctx)
		}
		if err != nil {
			return fmt.Errorf("rbac seed role %s: %w", r.Code, err)
		}
		want := make([]uuid.UUID, 0, len(r.Permissions))
		bulk := make([]*ent.RolePermissionCreate, 0, len(r.Permissions))
		for _, code := range r.Permissions {
			pid, ok := permIDs[code]
			if !ok {
				return fmt.Errorf("rbac seed role %s: unknown permission %s", r.Code, code)
			}
			want = append(want, pid)
			bulk = append(bulk, s.client.RolePermission.Create().SetRoleID(role.ID).SetPermissionID(pid))
		}
		if len(bulk) > 0 {
			if err := s.client.RolePermission.CreateBulk(bulk...).
				OnConflictColumns(entroleperm.FieldRoleID, entroleperm.FieldPermissionID).DoNothing().Exec(ctx); err != nil {
				return fmt.Errorf("rbac seed role permissions %s: %w", r.Code, err)
			}
		}
		if _, err := s.client.RolePermission.Delete().
			Where(entroleperm.RoleID(role.ID), entroleperm.PermissionIDNotIn(want...)).Exec(ctx); err != nil {
			return fmt.Errorf("rbac seed role permissions %s: %w", r.Code, err)
		}
	}
	// Codes dropped from the registry go with their role links.
	stale, err := s.client.Permission.Query().Where(entpermission.PermissionCodeNotIn(AllCodes()...)).IDs(ctx)
	if err != nil {
		return fmt.Errorf("rbac seed stale permissions: %w", err)
	}
	if len(stale) > 0 {
		if _, err := s.client.RolePermission.Delete().Where(entroleperm.PermissionIDIn(stale...)).Exec(ctx); err != nil {
			return err
		}
		if _, err := s.client.Permission.Delete().Where(entpermission.IDIn(stale...)).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Identity is what the token tells us about a signed-in user.
type Identity struct {
	TenantID    uuid.UUID
	AuthUserID  uuid.UUID
	Email       string
	Name        string
	GlobalRoles []string
}

// EnsureUserFromToken provisions the local user just in time and keeps their default role in step
// with their global roles. A user holding any role a projects admin granted keeps exactly that;
// otherwise they hold the one role MapGlobalRoles gives them (assigned_by is the user, marking it
// automatic). Throttled per pod.
func (s *Service) EnsureUserFromToken(ctx context.Context, id Identity) error {
	if id.TenantID == uuid.Nil || id.AuthUserID == uuid.Nil {
		return nil
	}
	mapped := MapGlobalRoles(id.GlobalRoles)
	key := id.TenantID.String() + ":" + id.AuthUserID.String() + ":" + mapped
	if _, ok := s.seen.Get(key); ok {
		return nil
	}
	userID, err := s.upsertUser(ctx, id.TenantID, id.AuthUserID, id.Email, id.Name)
	if err != nil {
		return err
	}
	assignments, err := s.client.UserRoleAssignment.Query().
		Where(entassign.TenantID(id.TenantID), entassign.UserID(userID)).WithRole().All(ctx)
	if err != nil {
		return fmt.Errorf("rbac ensure user: %w", err)
	}
	hasMapped := false
	var autoOthers []uuid.UUID
	for _, a := range assignments {
		if a.AssignedBy != id.AuthUserID {
			// An admin decided this user's roles; the global role no longer applies.
			s.seen.Set(key, true)
			return nil
		}
		if a.Edges.Role != nil && a.Edges.Role.RoleCode == mapped {
			hasMapped = true
		} else {
			autoOthers = append(autoOthers, a.ID)
		}
	}
	if !hasMapped || len(autoOthers) > 0 {
		// The tenant's own copy of a default role first: once a default role is customised, the
		// copy is what this tenant's people hold, so JIT never adds the default back beside it.
		role, err := roleByCode(ctx, s.client.Role, id.TenantID, mapped)
		if err != nil {
			return err
		}
		tx, err := s.client.Tx(ctx)
		if err != nil {
			return err
		}
		if len(autoOthers) > 0 {
			if _, err := tx.UserRoleAssignment.Delete().Where(entassign.IDIn(autoOthers...)).Exec(ctx); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		if !hasMapped {
			if err := tx.UserRoleAssignment.Create().SetTenantID(id.TenantID).SetUserID(userID).
				SetRoleID(role.ID).SetAssignedBy(id.AuthUserID).
				OnConflictColumns(entassign.FieldTenantID, entassign.FieldUserID, entassign.FieldRoleID).
				DoNothing().Exec(ctx); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		s.invalidate(ctx, id.TenantID, id.AuthUserID)
	}
	s.seen.Set(key, true)
	return nil
}

func (s *Service) upsertUser(ctx context.Context, tenantID, authUserID uuid.UUID, email, name string) (uuid.UUID, error) {
	now := time.Now()
	create := s.client.ProjectsUser.Create().SetTenantID(tenantID).SetAuthServiceUserID(authUserID).
		SetSyncStatus("synced").SetLastSyncAt(now)
	if email != "" {
		create.SetEmail(email)
	}
	if name != "" {
		create.SetName(name)
	}
	id, err := create.OnConflictColumns(entprojectsuser.FieldTenantID, entprojectsuser.FieldAuthServiceUserID).
		Update(func(u *ent.ProjectsUserUpsert) {
			u.SetLastSyncAt(now)
			if email != "" {
				u.SetEmail(email)
			}
			if name != "" {
				u.SetName(name)
			}
		}).ID(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("rbac upsert user: %w", err)
	}
	return id, nil
}

// Resolve returns the user's projects roles and effective permission codes in the tenant, cached.
// A user with no local row (never signed in here) has none.
func (s *Service) Resolve(ctx context.Context, tenantID, authUserID uuid.UUID) (Resolved, error) {
	return sharedcache.GetOrSet(ctx, s.cache, permsKey(tenantID, authUserID), cacheTTL, func(ctx context.Context) (Resolved, error) {
		out := Resolved{Roles: []string{}, Perms: []string{}}
		user, err := s.client.ProjectsUser.Query().
			Where(entprojectsuser.TenantID(tenantID), entprojectsuser.AuthServiceUserID(authUserID)).Only(ctx)
		if ent.IsNotFound(err) {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		if user.Status != "active" {
			return out, nil
		}
		assignments, err := s.client.UserRoleAssignment.Query().
			Where(entassign.TenantID(tenantID), entassign.UserID(user.ID),
				entassign.Or(entassign.ExpiresAtIsNil(), entassign.ExpiresAtGT(time.Now()))).
			WithRole(func(q *ent.RoleQuery) { q.WithPermissions() }).All(ctx)
		if err != nil {
			return out, err
		}
		var held []string
		for _, a := range assignments {
			if a.Edges.Role == nil {
				continue
			}
			out.Roles = append(out.Roles, a.Edges.Role.RoleCode)
			for _, p := range a.Edges.Role.Edges.Permissions {
				held = append(held, p.PermissionCode)
			}
		}
		sort.Slice(out.Roles, func(i, j int) bool { return RoleRank(out.Roles[i]) < RoleRank(out.Roles[j]) })
		out.Perms = Expand(held)
		return out, nil
	})
}

// User returns the local user, or nil when they have never signed in here.
func (s *Service) User(ctx context.Context, tenantID, authUserID uuid.UUID) (*ent.ProjectsUser, error) {
	u, err := s.client.ProjectsUser.Query().
		Where(entprojectsuser.TenantID(tenantID), entprojectsuser.AuthServiceUserID(authUserID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	return u, err
}

// HasPermission reports whether the user holds code (directly or through an implying code).
func (s *Service) HasPermission(ctx context.Context, tenantID, authUserID uuid.UUID, code string) (bool, error) {
	r, err := s.Resolve(ctx, tenantID, authUserID)
	if err != nil {
		return false, err
	}
	for _, p := range r.Perms {
		if p == code {
			return true, nil
		}
	}
	return false, nil
}

// HoldersAmong returns which of ids (auth-api user ids) hold code in the tenant, directly or
// through an implying code, in one query: active local users with an unexpired assignment of a role
// that carries the code. users is client.ProjectsUser or tx.ProjectsUser; unlike HasPermission it
// reads no cache, so it fits inside a transaction and sees roles changed a moment ago.
func HoldersAmong(ctx context.Context, users *ent.ProjectsUserClient, tenantID uuid.UUID, ids []uuid.UUID, code string) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	codes := append([]string{code}, impliers(code)...)
	rows, err := users.Query().
		Where(entprojectsuser.TenantID(tenantID), entprojectsuser.AuthServiceUserIDIn(ids...), entprojectsuser.Status("active"),
			entprojectsuser.HasRoleAssignmentsWith(
				entassign.TenantID(tenantID),
				entassign.Or(entassign.ExpiresAtIsNil(), entassign.ExpiresAtGT(time.Now())),
				entassign.HasRoleWith(entrole.HasPermissionsWith(entpermission.PermissionCodeIn(codes...))),
			)).
		Select(entprojectsuser.FieldAuthServiceUserID).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("rbac holders of %s: %w", code, err)
	}
	for _, u := range rows {
		out[u.AuthServiceUserID] = true
	}
	return out, nil
}

// AssignRole gives a user a role with no end date, granted by actor. See AssignRoleUntil.
func (s *Service) AssignRole(ctx context.Context, tenantID, authUserID uuid.UUID, roleCode string, actor uuid.UUID) error {
	_, err := s.AssignRoleUntil(ctx, tenantID, authUserID, roleCode, actor, nil)
	return err
}

// AssignRoleUntil gives a user a role by code (the tenant's own copy first, then the default role)
// until expiresAt, or with no end when it is nil, granted by actor. Giving a role the user already
// holds renews it with the new end date. The user's automatic default role is dropped, so from
// now on only admin decisions apply. The user need not have signed in yet. The client portal role
// is never given here: only the portal sign-in gives it.
func (s *Service) AssignRoleUntil(ctx context.Context, tenantID, authUserID uuid.UUID, roleCode string, actor uuid.UUID, expiresAt *time.Time) (*ent.Role, error) {
	if actor == authUserID {
		return nil, ErrSelfChange
	}
	if expiresAt != nil && !expiresAt.After(time.Now()) {
		return nil, ErrInvalidExpiry
	}
	if roleCode == RoleClient {
		return nil, ErrReservedRole
	}
	role, err := roleByCode(ctx, s.client.Role, tenantID, roleCode)
	if err != nil {
		return nil, err
	}
	userID, err := s.upsertUser(ctx, tenantID, authUserID, "", "")
	if err != nil {
		return nil, err
	}
	err = txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		if _, err := tx.UserRoleAssignment.Delete().Where(entassign.TenantID(tenantID), entassign.UserID(userID),
			entassign.AssignedBy(authUserID)).Exec(ctx); err != nil {
			return err
		}
		create := tx.UserRoleAssignment.Create().SetTenantID(tenantID).SetUserID(userID).SetRoleID(role.ID).
			SetAssignedBy(actor).SetNillableExpiresAt(expiresAt)
		return create.OnConflictColumns(entassign.FieldTenantID, entassign.FieldUserID, entassign.FieldRoleID).
			Update(func(u *ent.UserRoleAssignmentUpsert) {
				u.UpdateAssignedBy()
				if expiresAt != nil {
					u.SetExpiresAt(*expiresAt)
				} else {
					u.ClearExpiresAt()
				}
			}).Exec(ctx)
	})
	if err != nil {
		return nil, err
	}
	s.invalidate(ctx, tenantID, authUserID)
	return role, nil
}

// RevokeRole removes one role (by code: the tenant's copy or the default) from a user. Nobody
// changes their own roles, and the last active projects administrator keeps the role. A user left
// with no role gets their default role back on their next request.
func (s *Service) RevokeRole(ctx context.Context, tenantID, authUserID uuid.UUID, roleCode string, actor uuid.UUID) error {
	if actor == authUserID {
		return ErrSelfChange
	}
	if _, err := roleByCode(ctx, s.client.Role, tenantID, roleCode); err != nil {
		return err
	}
	if roleCode == RoleAdmin {
		last, err := s.lastActiveAdmin(ctx, tenantID, authUserID)
		if err != nil {
			return err
		}
		if last {
			return ErrLastAdmin
		}
	}
	n, err := s.client.UserRoleAssignment.Delete().Where(entassign.TenantID(tenantID),
		entassign.HasRoleWith(entrole.RoleCode(roleCode), entrole.Or(entrole.TenantIDIsNil(), entrole.TenantID(tenantID))),
		entassign.HasUserWith(entprojectsuser.TenantID(tenantID), entprojectsuser.AuthServiceUserID(authUserID))).Exec(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotAssigned
	}
	s.invalidate(ctx, tenantID, authUserID)
	return nil
}

// RoleView is a role as the role screens show it. scope is global for a default role and tenant
// for the tenant's own; is_customized marks a tenant copy of a default role (same code). holders
// counts the tenant's active people whose assignment has not ended.
type RoleView struct {
	ID           uuid.UUID `json:"id"`
	Code         string    `json:"code"`
	Name         string    `json:"name"`
	Description  string    `json:"description,omitempty"`
	System       bool      `json:"is_system_role"`
	Scope        string    `json:"scope"`
	IsCustomized bool      `json:"is_customized"`
	Permissions  []string  `json:"permissions"`
	Holders      int       `json:"holders"`
}

// ListRoles returns the roles the tenant sees, in rank order (default roles first, then the
// tenant's own by name), with their permission codes and holder counts. A tenant copy of a default
// role replaces the default in the list.
func (s *Service) ListRoles(ctx context.Context, tenantID uuid.UUID) ([]RoleView, error) {
	rows, err := s.client.Role.Query().Where(entrole.Or(entrole.TenantIDIsNil(), entrole.TenantID(tenantID))).
		WithPermissions().All(ctx)
	if err != nil {
		return nil, err
	}
	var counts []struct {
		RoleID uuid.UUID `json:"role_id"`
		Count  int       `json:"count"`
	}
	if err := s.client.UserRoleAssignment.Query().Where(entassign.TenantID(tenantID), notEnded(),
		entassign.HasUserWith(entprojectsuser.Status(StatusActive))).
		GroupBy(entassign.FieldRoleID).Aggregate(ent.Count()).Scan(ctx, &counts); err != nil {
		return nil, err
	}
	held := make(map[uuid.UUID]int, len(counts))
	for _, c := range counts {
		held[c.RoleID] = c.Count
	}
	global := map[string]bool{}
	copied := map[string]bool{}
	for _, r := range rows {
		if r.TenantID == nil {
			global[r.RoleCode] = true
		} else {
			copied[r.RoleCode] = true
		}
	}
	out := make([]RoleView, 0, len(rows))
	for _, r := range rows {
		if r.TenantID == nil && copied[r.RoleCode] {
			continue
		}
		v := RoleView{ID: r.ID, Code: r.RoleCode, Name: r.Name, Description: r.Description, System: r.IsSystemRole,
			Scope: scopeOf(r), IsCustomized: r.TenantID != nil && global[r.RoleCode],
			Holders: held[r.ID], Permissions: make([]string, 0, len(r.Edges.Permissions))}
		for _, p := range r.Edges.Permissions {
			v.Permissions = append(v.Permissions, p.PermissionCode)
		}
		sort.Strings(v.Permissions)
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := RoleRank(out[i].Code), RoleRank(out[j].Code)
		if ri != rj {
			return ri < rj
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// DeleteUser removes a user's local rows in every tenant (auth.user.deleted) inside tx.
func DeleteUser(ctx context.Context, tx *ent.Tx, authUserID uuid.UUID) error {
	ids, err := tx.ProjectsUser.Query().Where(entprojectsuser.AuthServiceUserID(authUserID)).IDs(ctx)
	if err != nil || len(ids) == 0 {
		return err
	}
	if _, err := tx.UserRoleAssignment.Delete().Where(entassign.UserIDIn(ids...)).Exec(ctx); err != nil {
		return err
	}
	_, err = tx.ProjectsUser.Delete().Where(entprojectsuser.IDIn(ids...)).Exec(ctx)
	return err
}

func (s *Service) invalidate(ctx context.Context, tenantID, authUserID uuid.UUID) {
	s.cache.Invalidate(ctx, permsKey(tenantID, authUserID))
}
