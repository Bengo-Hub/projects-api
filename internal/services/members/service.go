package members

import (
	"context"
	"errors"
	"fmt"

	"entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"go.uber.org/zap"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/predicate"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	entmember "github.com/bengobox/projects-service/internal/ent/projectmember"
	entuser "github.com/bengobox/projects-service/internal/ent/projectsuser"
	"github.com/bengobox/projects-service/internal/modules/rbac"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/people"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// ErrNotFound is returned when a member is not found.
var ErrNotFound = errors.New("not found")

// ErrAlreadyMember is returned when the user is already a member of the project.
var ErrAlreadyMember = errors.New("user is already a member of this project")

// ValidationError is a refused input, answered 400.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

// Service handles project member operations.
type Service struct {
	client  *ent.Client
	cache   *sharedcache.Aside
	log     *zap.Logger
	changed func()
}

// NewService creates a new members service.
func NewService(client *ent.Client, cache *sharedcache.Aside, log *zap.Logger) *Service {
	return &Service{client: client, cache: cache, log: log.Named("members.svc"), changed: func() {}}
}

// OnChange registers a callback run after every committed membership change (the access layer
// drops its cached membership answers).
func (s *Service) OnChange(fn func()) { s.changed = fn }

// AddMemberInput holds data for adding a project member.
type AddMemberInput struct {
	UserID   uuid.UUID `json:"user_id"`
	RoleCode string    `json:"role_code"`
}

// UpdateMemberInput holds data for updating a member's role.
type UpdateMemberInput struct {
	RoleCode string `json:"role_code"`
}

// Member is a project member with the name and email of the local copy of their SSO identity
// (projects_users). Both are empty for someone who has not signed in to projects yet.
type Member struct {
	*ent.ProjectMember
	Name  string `json:"name"`
	Email string `json:"email"`
}

// ListMembersFilter pages and searches a project's members. Search matches the name or email
// (case-insensitive substring, wildcards taken literally).
type ListMembersFilter struct {
	Search string
	Limit  int
	Offset int
}

// matching keeps members whose local user row matches term by name or email. It reads as
// EXISTS (SELECT 1 FROM projects_users u WHERE u.tenant_id = m.tenant_id AND
// u.auth_service_user_id = m.user_id AND (u.name ILIKE ? OR u.email ILIKE ?)): one probe of the
// (tenant_id, auth_service_user_id) unique index per member of the project, whose rows come from
// the (tenant_id, project_id, user_id) index, so the search never scans the tenant's users.
func matching(term string) predicate.ProjectMember {
	return predicate.ProjectMember(func(s *sql.Selector) {
		u := sql.Table(entuser.Table)
		s.Where(sql.Exists(sql.Select(u.C(entuser.FieldID)).From(u).Where(sql.And(
			sql.ColumnsEQ(u.C(entuser.FieldTenantID), s.C(entmember.FieldTenantID)),
			sql.ColumnsEQ(u.C(entuser.FieldAuthServiceUserID), s.C(entmember.FieldUserID)),
			sql.Or(sql.ContainsFold(u.C(entuser.FieldName), term), sql.ContainsFold(u.C(entuser.FieldEmail), term)),
		))))
	})
}

// ListMembers returns one page of a project's members, longest standing first, with the total.
// Names and emails for the whole page come from projects_users in one batched query.
func (s *Service) ListMembers(ctx context.Context, tenantID, projectID uuid.UUID, filter ListMembersFilter) ([]Member, int, error) {
	if filter.Limit <= 0 || filter.Limit > subListLimit {
		filter.Limit = subListLimit
	}
	q := s.client.ProjectMember.Query().Where(entmember.TenantID(tenantID), entmember.ProjectID(projectID), staffRow())
	if filter.Search != "" {
		q = q.Where(matching(filter.Search))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count members: %w", err)
	}
	rows, err := q.Order(ent.Asc(entmember.FieldJoinedAt), ent.Asc(entmember.FieldID)).
		Offset(filter.Offset).Limit(filter.Limit).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list members: %w", err)
	}
	ids := make([]uuid.UUID, len(rows))
	for i, m := range rows {
		ids[i] = m.UserID
	}
	known, err := people.Lookup(ctx, s.client.ProjectsUser, tenantID, ids)
	if err != nil {
		return nil, 0, err
	}
	out := make([]Member, len(rows))
	for i, m := range rows {
		p := known[m.UserID]
		out[i] = Member{ProjectMember: m, Name: p.Name, Email: p.Email}
	}
	return out, total, nil
}

func record(ctx context.Context, tx *ent.Tx, tenantID, projectID, userID uuid.UUID, action string, changes map[string]any) error {
	return activity.Record(ctx, tx, activity.Entry{
		TenantID: tenantID, ProjectID: activity.Ref(projectID), EntityType: "member", EntityID: userID,
		Action: action, Changes: changes,
	})
}

// staffRow keeps the team rows of a project. Client rows (role client) belong to the client
// portal: they are invited, listed and revoked through the portal service only, so the team
// routes neither show nor change them.
func staffRow() predicate.ProjectMember { return entmember.RoleCodeNEQ(rbac.RoleClient) }

// errClientRole refuses the client role on the team routes.
var errClientRole = ValidationError("clients are invited from the project's client portal settings, not added as team members")

// Roles are a project team member's roles: owner and manager lead the project (they hear about a
// client's milestone decision, notify.ProjectLeads), member works on it, viewer follows it. What a
// person may do comes from their projects role (rbac), not from this label.
var Roles = []string{"owner", "manager", "member", "viewer"}

// checkRole refuses the client role (portal only) and anything outside Roles.
func checkRole(code string) error {
	if code == rbac.RoleClient {
		return errClientRole
	}
	for _, r := range Roles {
		if r == code {
			return nil
		}
	}
	return ValidationError("role_code must be owner, manager, member or viewer")
}

// AddMember adds a user to a project of the tenant.
func (s *Service) AddMember(ctx context.Context, tenantID, projectID uuid.UUID, input AddMemberInput) (*ent.ProjectMember, error) {
	if input.UserID == uuid.Nil {
		return nil, ValidationError("user_id is required")
	}
	roleCode := input.RoleCode
	if roleCode == "" {
		roleCode = "member"
	}
	if err := checkRole(roleCode); err != nil {
		return nil, err
	}
	var m *ent.ProjectMember
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		ok, err := tx.Project.Query().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).Exist(ctx)
		if err != nil {
			return fmt.Errorf("validate project: %w", err)
		}
		if !ok {
			return ErrNotFound
		}
		exists, err := tx.ProjectMember.Query().
			Where(entmember.TenantID(tenantID), entmember.ProjectID(projectID), entmember.UserID(input.UserID)).
			Exist(ctx)
		if err != nil {
			return fmt.Errorf("check existing member: %w", err)
		}
		if exists {
			return ErrAlreadyMember
		}
		if m, err = tx.ProjectMember.Create().
			SetTenantID(tenantID).SetProjectID(projectID).SetUserID(input.UserID).SetRoleCode(roleCode).
			Save(ctx); err != nil {
			return fmt.Errorf("add member: %w", err)
		}
		return record(ctx, tx, tenantID, projectID, input.UserID, activity.ActionCreated, map[string]any{"role_code": roleCode})
	})
	if err != nil {
		return nil, err
	}
	s.changed()
	s.log.Info("member added", zap.String("project", projectID.String()), zap.String("user", input.UserID.String()))
	return m, nil
}

// UpdateMemberRole changes a member's role in the project.
func (s *Service) UpdateMemberRole(ctx context.Context, tenantID, projectID, userID uuid.UUID, input UpdateMemberInput) (*ent.ProjectMember, error) {
	if input.RoleCode == "" {
		return nil, ValidationError("role_code is required")
	}
	if err := checkRole(input.RoleCode); err != nil {
		return nil, err
	}
	var updated *ent.ProjectMember
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		m, err := tx.ProjectMember.Query().
			Where(entmember.TenantID(tenantID), entmember.ProjectID(projectID), entmember.UserID(userID), staffRow()).
			Only(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return ErrNotFound
			}
			return fmt.Errorf("fetch member: %w", err)
		}
		if updated, err = m.Update().SetRoleCode(input.RoleCode).Save(ctx); err != nil {
			return fmt.Errorf("update member: %w", err)
		}
		return record(ctx, tx, tenantID, projectID, userID, activity.ActionUpdated,
			map[string]any{"role_code": map[string]any{"from": m.RoleCode, "to": input.RoleCode}})
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// RemoveMember removes a user from a project.
func (s *Service) RemoveMember(ctx context.Context, tenantID, projectID, userID uuid.UUID) error {
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		n, err := tx.ProjectMember.Delete().
			Where(entmember.TenantID(tenantID), entmember.ProjectID(projectID), entmember.UserID(userID), staffRow()).Exec(ctx)
		if err != nil {
			return fmt.Errorf("remove member: %w", err)
		}
		if n == 0 {
			return ErrNotFound
		}
		return record(ctx, tx, tenantID, projectID, userID, activity.ActionDeleted, nil)
	})
	if err != nil {
		return err
	}
	s.changed()
	return nil
}
