// Package people reads the local copy of SSO users (projects_users, provisioned just in time on a
// user's first request) by auth-api user id. auth-api owns identity; the name and email here are a
// copy that read routes return next to member, comment and activity rows, always for a whole page
// in one query, never one per row.
package people

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	entuser "github.com/bengobox/projects-service/internal/ent/projectsuser"
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
