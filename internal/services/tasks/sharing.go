package tasks

import (
	"context"
	"fmt"
	"maps"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	enttask "github.com/bengobox/projects-service/internal/ent/task"
	"github.com/bengobox/projects-service/internal/platform/realtime"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// IsClientVisible reports whether the task is shared with the project's clients.
func IsClientVisible(meta map[string]any) bool {
	v, _ := meta[MetaClientVisible].(bool)
	return v
}

// SetClientVisible shares the task with the project's clients in the portal (visible true) or
// stops sharing it. The write runs under the task row lock like every other metadata write, so
// it never undoes a checklist tick or a timer written meanwhile. Sharing again changes nothing
// and writes no activity entry.
func (s *Service) SetClientVisible(ctx context.Context, tenantID, projectID, taskID uuid.UUID, visible bool) (*ent.Task, error) {
	var updated *ent.Task
	changed := false
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		if err := lockTask(ctx, tx, tenantID, projectID, taskID); err != nil {
			return err
		}
		t, err := tx.Task.Query().Where(enttask.ID(taskID)).Only(ctx)
		if err != nil {
			return fmt.Errorf("fetch task: %w", err)
		}
		if IsClientVisible(t.Metadata) == visible {
			updated = t
			return nil
		}
		meta := make(map[string]any, len(t.Metadata)+1)
		maps.Copy(meta, t.Metadata)
		if visible {
			meta[MetaClientVisible] = true
		} else {
			delete(meta, MetaClientVisible)
		}
		if updated, err = t.Update().SetMetadata(meta).Save(ctx); err != nil {
			return fmt.Errorf("share task: %w", err)
		}
		changed = true
		return activity.Record(ctx, tx, activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(projectID), TaskID: activity.Ref(taskID),
			EntityType: "task", EntityID: taskID, Action: activity.ActionUpdated,
			Changes: map[string]any{MetaClientVisible: visible},
		})
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.emit(ctx, tenantID, realtime.TaskUpdated, updated)
	}
	return updated, nil
}
