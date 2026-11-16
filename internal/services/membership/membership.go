// Package membership is the one place that decides which projects a user may see: the projects
// they own or are an active member of. Users holding projects.projects.manage are not limited
// (the access layer checks that before calling here).
package membership

import (
	"context"
	"time"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/predicate"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	entmember "github.com/bengobox/projects-service/internal/ent/projectmember"
)

// cacheTTL bounds how long another pod may serve a membership that just changed; this pod drops
// its copy at once through Forget.
const cacheTTL = 30 * time.Second

// Checker answers membership questions, cached per pod.
type Checker struct {
	client *ent.Client
	cache  *sharedcache.Local[string, bool]
}

// New creates a checker.
func New(client *ent.Client) *Checker {
	return &Checker{client: client, cache: sharedcache.NewLocal[string, bool](20000, cacheTTL)}
}

// Visible is the project predicate for a user limited to their own projects.
func Visible(userID uuid.UUID) predicate.Project {
	return entproject.Or(
		entproject.OwnerID(userID),
		entproject.HasMembersWith(entmember.UserID(userID), entmember.LeftAtIsNil()),
	)
}

// IsMember reports whether the user owns or is an active member of the tenant's project. A
// project of another tenant, or one that does not exist, is false.
func (c *Checker) IsMember(ctx context.Context, tenantID, projectID, userID uuid.UUID) (bool, error) {
	key := tenantID.String() + ":" + projectID.String() + ":" + userID.String()
	return c.cache.GetOrLoad(key, func() (bool, error) {
		return c.client.Project.Query().
			Where(entproject.ID(projectID), entproject.TenantID(tenantID), Visible(userID)).Exist(ctx)
	})
}

// Forget drops this pod's cached answers after a membership change.
func (c *Checker) Forget() { c.cache.Purge() }
