package project

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

	tests := []struct {
		name    string
		params  CreateParams
		wantErr bool
	}{
		{
			name: "create with required fields only",
			params: CreateParams{
				TenantID: tenantID,
				Name:     "Test Project",
				OwnerID:  ownerID,
			},
			wantErr: false,
		},
		{
			name: "create with all fields",
			params: CreateParams{
				TenantID:    tenantID,
				Name:        "Full Project",
				Description: "A complete project",
				Status:      "planning",
				Budget:      func() *float64 { b := 10000.0; return &b }(),
				Currency:    "KES",
				OwnerID:     ownerID,
				Metadata:    map[string]any{"priority": "high"},
			},
			wantErr: false,
		},
		{
			name: "create with empty name fails",
			params: CreateParams{
				TenantID: tenantID,
				Name:     "",
				OwnerID:  ownerID,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := svc.Create(ctx, tt.params)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, p)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, p)
				assert.Equal(t, tt.params.Name, p.Name)
				assert.Equal(t, tt.params.TenantID, p.TenantID)
				assert.Equal(t, tt.params.OwnerID, p.OwnerID)
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

	// Create a test project
	created, err := svc.Create(ctx, CreateParams{
		TenantID: tenantID,
		Name:     "Get Test Project",
		OwnerID:  ownerID,
	})
	require.NoError(t, err)

	tests := []struct {
		name      string
		tenantID  uuid.UUID
		projectID uuid.UUID
		wantErr   bool
		errType   error
	}{
		{
			name:      "get existing project",
			tenantID:  tenantID,
			projectID: created.ID,
			wantErr:   false,
		},
		{
			name:      "get non-existent project",
			tenantID:  tenantID,
			projectID: uuid.New(),
			wantErr:   true,
			errType:   ErrNotFound,
		},
		{
			name:      "get project from wrong tenant",
			tenantID:  otherTenantID,
			projectID: created.ID,
			wantErr:   true,
			errType:   ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := svc.Get(ctx, tt.tenantID, tt.projectID)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, p)
				if tt.errType != nil {
					assert.ErrorIs(t, err, tt.errType)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, created.ID, p.ID)
				assert.Equal(t, created.Name, p.Name)
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
	otherOwnerID := uuid.New()

	// Create test projects
	for i := 0; i < 5; i++ {
		status := "active"
		owner := ownerID
		if i%2 == 0 {
			status = "completed"
			owner = otherOwnerID
		}
		_, err := svc.Create(ctx, CreateParams{
			TenantID: tenantID,
			Name:     "Project " + string(rune('A'+i)),
			Status:   status,
			OwnerID:  owner,
		})
		require.NoError(t, err)
	}

	tests := []struct {
		name       string
		params     ListParams
		wantCount  int
		wantTotal  int
	}{
		{
			name: "list all projects",
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
			name: "list by status",
			params: ListParams{
				TenantID: tenantID,
				Status:   "active",
				Limit:    10,
			},
			wantCount: 2,
			wantTotal: 2,
		},
		{
			name: "list by owner",
			params: ListParams{
				TenantID: tenantID,
				OwnerID:  &ownerID,
				Limit:    10,
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
			projects, total, err := svc.List(ctx, tt.params)
			require.NoError(t, err)
			assert.Len(t, projects, tt.wantCount)
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

	// Create a test project
	created, err := svc.Create(ctx, CreateParams{
		TenantID: tenantID,
		Name:     "Update Test Project",
		Status:   "active",
		OwnerID:  ownerID,
	})
	require.NoError(t, err)

	newName := "Updated Name"
	newStatus := "completed"
	newBudget := 5000.0

	tests := []struct {
		name      string
		tenantID  uuid.UUID
		projectID uuid.UUID
		params    UpdateParams
		wantErr   bool
		validate  func(t *testing.T, p *ent.Project)
	}{
		{
			name:      "update name",
			tenantID:  tenantID,
			projectID: created.ID,
			params: UpdateParams{
				Name: &newName,
			},
			wantErr: false,
			validate: func(t *testing.T, p *ent.Project) {
				assert.Equal(t, newName, p.Name)
			},
		},
		{
			name:      "update multiple fields",
			tenantID:  tenantID,
			projectID: created.ID,
			params: UpdateParams{
				Status: &newStatus,
				Budget: &newBudget,
			},
			wantErr: false,
			validate: func(t *testing.T, p *ent.Project) {
				assert.Equal(t, newStatus, p.Status)
				assert.Equal(t, newBudget, p.Budget)
			},
		},
		{
			name:      "update non-existent project",
			tenantID:  tenantID,
			projectID: uuid.New(),
			params: UpdateParams{
				Name: &newName,
			},
			wantErr: true,
		},
		{
			name:      "update project from wrong tenant",
			tenantID:  uuid.New(),
			projectID: created.ID,
			params: UpdateParams{
				Name: &newName,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := svc.Update(ctx, tt.tenantID, tt.projectID, tt.params)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				if tt.validate != nil {
					tt.validate(t, p)
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

	// Create test projects for deletion
	toDelete, err := svc.Create(ctx, CreateParams{
		TenantID: tenantID,
		Name:     "To Delete",
		OwnerID:  ownerID,
	})
	require.NoError(t, err)

	tests := []struct {
		name      string
		tenantID  uuid.UUID
		projectID uuid.UUID
		wantErr   bool
	}{
		{
			name:      "delete existing project",
			tenantID:  tenantID,
			projectID: toDelete.ID,
			wantErr:   false,
		},
		{
			name:      "delete already deleted project",
			tenantID:  tenantID,
			projectID: toDelete.ID,
			wantErr:   true,
		},
		{
			name:      "delete non-existent project",
			tenantID:  tenantID,
			projectID: uuid.New(),
			wantErr:   true,
		},
		{
			name:      "delete from wrong tenant",
			tenantID:  uuid.New(),
			projectID: toDelete.ID,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.Delete(ctx, tt.tenantID, tt.projectID)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// Verify deletion
				_, err := svc.Get(ctx, tt.tenantID, tt.projectID)
				assert.ErrorIs(t, err, ErrNotFound)
			}
		})
	}
}

func TestService_CreateWithDates(t *testing.T) {
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
	startDate := time.Now().Add(24 * time.Hour)
	endDate := time.Now().Add(30 * 24 * time.Hour)

	p, err := svc.Create(ctx, CreateParams{
		TenantID:  tenantID,
		Name:      "Dated Project",
		OwnerID:   ownerID,
		StartDate: &startDate,
		EndDate:   &endDate,
	})
	require.NoError(t, err)

	// Verify dates are stored correctly (within 1 second tolerance)
	assert.WithinDuration(t, startDate, p.StartDate, time.Second)
	assert.WithinDuration(t, endDate, p.EndDate, time.Second)
}
