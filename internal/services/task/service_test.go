package task

import (
	"context"
	"testing"
	"time"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// setupTestProject creates a project for testing tasks.
func setupTestProject(t *testing.T, client *ent.Client, tenantID, ownerID uuid.UUID) *ent.Project {
	t.Helper()
	ctx := context.Background()

	p, err := client.Project.Create().
		SetTenantID(tenantID).
		SetName("Test Project").
		SetOwnerID(ownerID).
		Save(ctx)
	require.NoError(t, err)
	return p
}

func TestService_Create(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := zap.NewNop()
	svc := NewService(logger, client)
	ctx := context.Background()

	tenantID := uuid.New()
	ownerID := uuid.New()
	project := setupTestProject(t, client, tenantID, ownerID)

	tests := []struct {
		name    string
		params  CreateParams
		wantErr bool
		errType error
	}{
		{
			name: "create with required fields only",
			params: CreateParams{
				TenantID:  tenantID,
				ProjectID: project.ID,
				Title:     "Test Task",
			},
			wantErr: false,
		},
		{
			name: "create with all fields",
			params: CreateParams{
				TenantID:    tenantID,
				ProjectID:   project.ID,
				Title:       "Full Task",
				Description: "A complete task",
				Status:      "in_progress",
				Priority:    "high",
				AssigneeID:  &ownerID,
				DueDate:     func() *time.Time { t := time.Now().Add(7 * 24 * time.Hour); return &t }(),
				Metadata:    map[string]any{"labels": []string{"urgent"}},
			},
			wantErr: false,
		},
		{
			name: "create with empty title fails",
			params: CreateParams{
				TenantID:  tenantID,
				ProjectID: project.ID,
				Title:     "",
			},
			wantErr: true,
		},
		{
			name: "create with non-existent project fails",
			params: CreateParams{
				TenantID:  tenantID,
				ProjectID: uuid.New(),
				Title:     "Orphan Task",
			},
			wantErr: true,
			errType: ErrProjectNotFound,
		},
		{
			name: "create with wrong tenant project fails",
			params: CreateParams{
				TenantID:  uuid.New(),
				ProjectID: project.ID,
				Title:     "Wrong Tenant Task",
			},
			wantErr: true,
			errType: ErrProjectNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task, err := svc.Create(ctx, tt.params)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, task)
				if tt.errType != nil {
					assert.ErrorIs(t, err, tt.errType)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, task)
				assert.Equal(t, tt.params.Title, task.Title)
				assert.Equal(t, tt.params.TenantID, task.TenantID)
				assert.Equal(t, tt.params.ProjectID, task.ProjectID)
			}
		})
	}
}

func TestService_Get(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := zap.NewNop()
	svc := NewService(logger, client)
	ctx := context.Background()

	tenantID := uuid.New()
	otherTenantID := uuid.New()
	ownerID := uuid.New()
	project := setupTestProject(t, client, tenantID, ownerID)

	// Create a test task
	created, err := svc.Create(ctx, CreateParams{
		TenantID:  tenantID,
		ProjectID: project.ID,
		Title:     "Get Test Task",
	})
	require.NoError(t, err)

	tests := []struct {
		name     string
		tenantID uuid.UUID
		taskID   uuid.UUID
		wantErr  bool
		errType  error
	}{
		{
			name:     "get existing task",
			tenantID: tenantID,
			taskID:   created.ID,
			wantErr:  false,
		},
		{
			name:     "get non-existent task",
			tenantID: tenantID,
			taskID:   uuid.New(),
			wantErr:  true,
			errType:  ErrNotFound,
		},
		{
			name:     "get task from wrong tenant",
			tenantID: otherTenantID,
			taskID:   created.ID,
			wantErr:  true,
			errType:  ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task, err := svc.Get(ctx, tt.tenantID, tt.taskID)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, task)
				if tt.errType != nil {
					assert.ErrorIs(t, err, tt.errType)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, created.ID, task.ID)
				assert.Equal(t, created.Title, task.Title)
			}
		})
	}
}

func TestService_List(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := zap.NewNop()
	svc := NewService(logger, client)
	ctx := context.Background()

	tenantID := uuid.New()
	ownerID := uuid.New()
	assignee1 := uuid.New()
	assignee2 := uuid.New()
	project := setupTestProject(t, client, tenantID, ownerID)

	// Create test tasks with varying attributes
	statuses := []string{"todo", "in_progress", "done", "todo", "in_progress"}
	priorities := []string{"low", "medium", "high", "medium", "high"}
	assignees := []*uuid.UUID{&assignee1, &assignee1, &assignee2, nil, &assignee2}

	for i := 0; i < 5; i++ {
		_, err := svc.Create(ctx, CreateParams{
			TenantID:   tenantID,
			ProjectID:  project.ID,
			Title:      "Task " + string(rune('A'+i)),
			Status:     statuses[i],
			Priority:   priorities[i],
			AssigneeID: assignees[i],
		})
		require.NoError(t, err)
	}

	tests := []struct {
		name      string
		params    ListParams
		wantCount int
		wantTotal int
	}{
		{
			name: "list all tasks",
			params: ListParams{
				TenantID: tenantID,
				Limit:    10,
			},
			wantCount: 5,
			wantTotal: 5,
		},
		{
			name: "list with pagination",
			params: ListParams{
				TenantID: tenantID,
				Limit:    2,
				Offset:   0,
			},
			wantCount: 2,
			wantTotal: 5,
		},
		{
			name: "list by project",
			params: ListParams{
				TenantID:  tenantID,
				ProjectID: &project.ID,
				Limit:     10,
			},
			wantCount: 5,
			wantTotal: 5,
		},
		{
			name: "list by status",
			params: ListParams{
				TenantID: tenantID,
				Status:   "todo",
				Limit:    10,
			},
			wantCount: 2,
			wantTotal: 2,
		},
		{
			name: "list by priority",
			params: ListParams{
				TenantID: tenantID,
				Priority: "high",
				Limit:    10,
			},
			wantCount: 2,
			wantTotal: 2,
		},
		{
			name: "list by assignee",
			params: ListParams{
				TenantID:   tenantID,
				AssigneeID: &assignee1,
				Limit:      10,
			},
			wantCount: 2,
			wantTotal: 2,
		},
		{
			name: "list from empty tenant",
			params: ListParams{
				TenantID: uuid.New(),
				Limit:    10,
			},
			wantCount: 0,
			wantTotal: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tasks, total, err := svc.List(ctx, tt.params)
			require.NoError(t, err)
			assert.Len(t, tasks, tt.wantCount)
			assert.Equal(t, tt.wantTotal, total)
		})
	}
}

func TestService_Update(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := zap.NewNop()
	svc := NewService(logger, client)
	ctx := context.Background()

	tenantID := uuid.New()
	ownerID := uuid.New()
	project := setupTestProject(t, client, tenantID, ownerID)

	// Create a test task
	created, err := svc.Create(ctx, CreateParams{
		TenantID:  tenantID,
		ProjectID: project.ID,
		Title:     "Update Test Task",
		Status:    "todo",
	})
	require.NoError(t, err)

	newTitle := "Updated Title"
	newStatus := "done"
	newPriority := "high"
	newAssignee := uuid.New()

	tests := []struct {
		name     string
		tenantID uuid.UUID
		taskID   uuid.UUID
		params   UpdateParams
		wantErr  bool
		validate func(t *testing.T, task *ent.Task)
	}{
		{
			name:     "update title",
			tenantID: tenantID,
			taskID:   created.ID,
			params: UpdateParams{
				Title: &newTitle,
			},
			wantErr: false,
			validate: func(t *testing.T, task *ent.Task) {
				assert.Equal(t, newTitle, task.Title)
			},
		},
		{
			name:     "update status to done sets completed_at",
			tenantID: tenantID,
			taskID:   created.ID,
			params: UpdateParams{
				Status: &newStatus,
			},
			wantErr: false,
			validate: func(t *testing.T, task *ent.Task) {
				assert.Equal(t, newStatus, task.Status)
				assert.False(t, task.CompletedAt.IsZero(), "completed_at should be set")
			},
		},
		{
			name:     "update multiple fields",
			tenantID: tenantID,
			taskID:   created.ID,
			params: UpdateParams{
				Priority:   &newPriority,
				AssigneeID: &newAssignee,
			},
			wantErr: false,
			validate: func(t *testing.T, task *ent.Task) {
				assert.Equal(t, newPriority, task.Priority)
				assert.Equal(t, newAssignee, task.AssigneeID)
			},
		},
		{
			name:     "update non-existent task",
			tenantID: tenantID,
			taskID:   uuid.New(),
			params: UpdateParams{
				Title: &newTitle,
			},
			wantErr: true,
		},
		{
			name:     "update task from wrong tenant",
			tenantID: uuid.New(),
			taskID:   created.ID,
			params: UpdateParams{
				Title: &newTitle,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task, err := svc.Update(ctx, tt.tenantID, tt.taskID, tt.params)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				if tt.validate != nil {
					tt.validate(t, task)
				}
			}
		})
	}
}

func TestService_Delete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := zap.NewNop()
	svc := NewService(logger, client)
	ctx := context.Background()

	tenantID := uuid.New()
	ownerID := uuid.New()
	project := setupTestProject(t, client, tenantID, ownerID)

	// Create a test task for deletion
	toDelete, err := svc.Create(ctx, CreateParams{
		TenantID:  tenantID,
		ProjectID: project.ID,
		Title:     "To Delete",
	})
	require.NoError(t, err)

	tests := []struct {
		name     string
		tenantID uuid.UUID
		taskID   uuid.UUID
		wantErr  bool
	}{
		{
			name:     "delete existing task",
			tenantID: tenantID,
			taskID:   toDelete.ID,
			wantErr:  false,
		},
		{
			name:     "delete already deleted task",
			tenantID: tenantID,
			taskID:   toDelete.ID,
			wantErr:  true,
		},
		{
			name:     "delete non-existent task",
			tenantID: tenantID,
			taskID:   uuid.New(),
			wantErr:  true,
		},
		{
			name:     "delete from wrong tenant",
			tenantID: uuid.New(),
			taskID:   toDelete.ID,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.Delete(ctx, tt.tenantID, tt.taskID)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// Verify deletion
				_, err := svc.Get(ctx, tt.tenantID, tt.taskID)
				assert.ErrorIs(t, err, ErrNotFound)
			}
		})
	}
}

func TestService_UpdateStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := zap.NewNop()
	svc := NewService(logger, client)
	ctx := context.Background()

	tenantID := uuid.New()
	ownerID := uuid.New()
	project := setupTestProject(t, client, tenantID, ownerID)

	// Create a test task
	created, err := svc.Create(ctx, CreateParams{
		TenantID:  tenantID,
		ProjectID: project.ID,
		Title:     "Status Test Task",
		Status:    "todo",
	})
	require.NoError(t, err)

	// Update to in_progress
	updated, err := svc.UpdateStatus(ctx, tenantID, created.ID, "in_progress")
	require.NoError(t, err)
	assert.Equal(t, "in_progress", updated.Status)
	assert.True(t, updated.CompletedAt.IsZero(), "completed_at should not be set")

	// Update to done
	updated, err = svc.UpdateStatus(ctx, tenantID, created.ID, "done")
	require.NoError(t, err)
	assert.Equal(t, "done", updated.Status)
	assert.False(t, updated.CompletedAt.IsZero(), "completed_at should be set")

	// Update back to todo (clears completed_at)
	updated, err = svc.UpdateStatus(ctx, tenantID, created.ID, "todo")
	require.NoError(t, err)
	assert.Equal(t, "todo", updated.Status)
	assert.True(t, updated.CompletedAt.IsZero(), "completed_at should be cleared")
}

func TestService_Assign(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := zap.NewNop()
	svc := NewService(logger, client)
	ctx := context.Background()

	tenantID := uuid.New()
	ownerID := uuid.New()
	assigneeID := uuid.New()
	project := setupTestProject(t, client, tenantID, ownerID)

	// Create a test task
	created, err := svc.Create(ctx, CreateParams{
		TenantID:  tenantID,
		ProjectID: project.ID,
		Title:     "Assign Test Task",
	})
	require.NoError(t, err)

	// Assign the task
	updated, err := svc.Assign(ctx, tenantID, created.ID, assigneeID)
	require.NoError(t, err)
	assert.Equal(t, assigneeID, updated.AssigneeID)
}
