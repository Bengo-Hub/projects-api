// Package planning holds the sprint 16 planning depth features that are not task writes:
// workload, baselines, the risk register, change requests and project templates. Status rules,
// automations and recurring tasks change tasks, so they live in the tasks package next to the
// one status change path.
//
// Every write records its activity entry and its project.* outbox event in the same transaction.
package planning

import (
	"context"
	"errors"
	"fmt"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	"github.com/bengobox/projects-service/internal/platform/erp"
	"github.com/bengobox/projects-service/internal/platform/treasury"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/tasks"
)

var (
	// ErrNotFound is a project, baseline, risk, change request or template the tenant does not have.
	ErrNotFound = errors.New("not found")
	// ErrOwnRequest refuses an approver deciding their own change request.
	ErrOwnRequest = errors.New("you cannot decide a change request you raised")
	// ErrNotYours refuses a change to someone else's change request without the manage code.
	ErrNotYours = errors.New("only the requester or a change request manager can do this")
	// ErrSystemTemplate refuses changing or deleting a system template.
	ErrSystemTemplate = errors.New("system templates cannot be changed or deleted")
)

// ValidationError is a 422 with a message the user can act on.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

// StateError is a 409: the record is not in a state that allows the action (code names it).
type StateError struct {
	Code string
	Msg  string
}

func (e *StateError) Error() string { return e.Msg }

// Service is the planning service.
type Service struct {
	client   *ent.Client
	tasks    *tasks.Service
	treasury *treasury.Client
	log      *zap.Logger
	// erpCap reads people's working hours for the workload (WithERP; nil: settings only), and
	// capacities keeps each person's answer briefly (erpcapacity.go).
	erpCap     capacityReader
	capacities *sharedcache.Local[string, erp.Capacity]
}

// NewService builds the service. tasksSvc sends the live hints for tasks a change request moves;
// tc pushes approved cost changes to treasury (nil or unconfigured: the push is recorded as
// failed and can be retried).
func NewService(client *ent.Client, tasksSvc *tasks.Service, tc *treasury.Client, log *zap.Logger) *Service {
	return &Service{client: client, tasks: tasksSvc, treasury: tc, log: log.Named("planning.svc")}
}

// project loads a tenant's project.
func (s *Service) project(ctx context.Context, c *ent.ProjectClient, tenantID, projectID uuid.UUID) (*ent.Project, error) {
	p, err := c.Query().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load project: %w", err)
	}
	return p, nil
}

// record writes an activity entry for a planning record of a project.
func record(ctx context.Context, tx *ent.Tx, tenantID, projectID uuid.UUID, entity string, id uuid.UUID, action string, changes map[string]any) error {
	return activity.Record(ctx, tx, activity.Entry{
		TenantID: tenantID, ProjectID: activity.Ref(projectID), EntityType: entity, EntityID: id,
		Action: action, Changes: changes,
	})
}

// actor returns the acting user as a string ("" for a service call).
func actor(ctx context.Context) string {
	if id := activity.ActorFrom(ctx); id != uuid.Nil {
		return id.String()
	}
	return ""
}
