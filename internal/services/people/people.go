// Package people reads the local copy of SSO users (projects_users, provisioned just in time on a
// user's first request) by auth-api user id. auth-api owns identity; the name and email here are a
// copy that read routes return next to member, comment and activity rows, always for a whole page
// in one query, never one per row.
package people

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/predicate"
	entuser "github.com/bengobox/projects-service/internal/ent/projectsuser"
	entrole "github.com/bengobox/projects-service/internal/ent/role"
	entassign "github.com/bengobox/projects-service/internal/ent/userroleassignment"
	"github.com/bengobox/projects-service/internal/modules/rbac"
)

// Person is a local copy of an SSO user.
type Person struct {
	ID    uuid.UUID
	Email string
	Name  string
}

// Label is the name, else the email, else "" (someone who never signed in to projects).
func (p Person) Label() string {
	if p.Name != "" {
		return p.Name
	}
	return p.Email
}

// DisplayName is the label, else a neutral word (for notices, which always need a name).
func (p Person) DisplayName() string {
	if l := p.Label(); l != "" {
		return l
	}
	return "A teammate"
}

// Lookup returns the tenant's local users among ids, keyed by auth-api user id, in one query
// served by the (tenant_id, auth_service_user_id) unique index. users is client.ProjectsUser or
// tx.ProjectsUser. An id without a local row is absent; repeated and nil ids are fine.
func Lookup(ctx context.Context, users *ent.ProjectsUserClient, tenantID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]Person, error) {
	uniq := make([]uuid.UUID, 0, len(ids))
	seen := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		if id != uuid.Nil && !seen[id] {
			seen[id] = true
			uniq = append(uniq, id)
		}
	}
	out := make(map[uuid.UUID]Person, len(uniq))
	if len(uniq) == 0 {
		return out, nil
	}
	rows, err := users.Query().
		Where(entuser.TenantID(tenantID), entuser.AuthServiceUserIDIn(uniq...)).
		Select(entuser.FieldAuthServiceUserID, entuser.FieldEmail, entuser.FieldName).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load people: %w", err)
	}
	for _, u := range rows {
		out[u.AuthServiceUserID] = Person{ID: u.AuthServiceUserID, Email: u.Email, Name: u.Name}
	}
	return out, nil
}

// Search lists the tenant's staff who have signed in to projects: active local users, without
// anyone holding the client portal role, whose name or email contains q (any case), ordered by
// name, one page at a time. A tenant's staff list is small, so the filter runs within the tenant's
// rows of the (tenant_id, auth_service_user_id) index.
func Search(ctx context.Context, users *ent.ProjectsUserClient, tenantID uuid.UUID, q string, limit, offset int) ([]Person, int, error) {
	where := []predicate.ProjectsUser{
		entuser.TenantID(tenantID),
		entuser.Status("active"),
		entuser.Not(entuser.HasRoleAssignmentsWith(entassign.HasRoleWith(entrole.RoleCode(rbac.RoleClient)))),
	}
	if q = strings.TrimSpace(q); q != "" {
		where = append(where, entuser.Or(entuser.NameContainsFold(q), entuser.EmailContainsFold(q)))
	}
	query := users.Query().Where(where...)
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count people: %w", err)
	}
	rows, err := query.Order(ent.Asc(entuser.FieldName), ent.Asc(entuser.FieldEmail)).
		Limit(limit).Offset(offset).
		Select(entuser.FieldAuthServiceUserID, entuser.FieldEmail, entuser.FieldName).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("search people: %w", err)
	}
	out := make([]Person, 0, len(rows))
	for _, u := range rows {
		out = append(out, Person{ID: u.AuthServiceUserID, Email: u.Email, Name: u.Name})
	}
	return out, total, nil
}
