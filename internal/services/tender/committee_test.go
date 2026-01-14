package tender

import (
	"context"
	"testing"
	"time"

	"github.com/bengobox/projects-service/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestService_CreateCommittee(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := zap.NewNop()
	svc := NewService(logger, client)
	ctx := context.Background()

	tenantID := uuid.New()
	createdBy := uuid.New()
	chairID := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Committee Test Tender",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	tests := []struct {
		name    string
		params  CommitteeCreateParams
		wantErr bool
	}{
		{
			name: "create committee with required fields",
			params: CommitteeCreateParams{
				TenantID: tenantID,
				TenderID: tender.ID,
				Name:     "Evaluation Committee",
			},
			wantErr: false,
		},
		{
			name: "create committee with all optional fields",
			params: CommitteeCreateParams{
				TenantID:      tenantID,
				TenderID:      tender.ID,
				Name:          "Technical Review Board",
				CommitteeType: "technical",
				ChairID:       &chairID,
				Mandate:       "Review technical proposals and provide scoring",
				Metadata:      map[string]any{"department": "engineering"},
			},
			wantErr: false,
		},
		{
			name: "create committee for non-existent tender",
			params: CommitteeCreateParams{
				TenantID: tenantID,
				TenderID: uuid.New(),
				Name:     "Orphan Committee",
			},
			wantErr: true,
		},
		{
			name: "create committee with wrong tenant",
			params: CommitteeCreateParams{
				TenantID: uuid.New(),
				TenderID: tender.ID,
				Name:     "Wrong Tenant Committee",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			committee, err := svc.CreateCommittee(ctx, tt.params)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, committee)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, committee)
				assert.Equal(t, tt.params.Name, committee.Name)
				assert.Equal(t, tt.params.TenderID, committee.TenderID)
				assert.Equal(t, "active", committee.Status)
			}
		})
	}
}

func TestService_GetCommittee(t *testing.T) {
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
	createdBy := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Get Committee Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	committee, err := svc.CreateCommittee(ctx, CommitteeCreateParams{
		TenantID: tenantID,
		TenderID: tender.ID,
		Name:     "Test Committee",
	})
	require.NoError(t, err)

	tests := []struct {
		name        string
		tenantID    uuid.UUID
		committeeID uuid.UUID
		wantErr     bool
		errType     error
	}{
		{
			name:        "get existing committee",
			tenantID:    tenantID,
			committeeID: committee.ID,
			wantErr:     false,
		},
		{
			name:        "get non-existent committee",
			tenantID:    tenantID,
			committeeID: uuid.New(),
			wantErr:     true,
			errType:     ErrCommitteeNotFound,
		},
		{
			name:        "get committee from wrong tenant",
			tenantID:    otherTenantID,
			committeeID: committee.ID,
			wantErr:     true,
			errType:     ErrCommitteeNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.GetCommittee(ctx, tt.tenantID, tt.committeeID)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errType != nil {
					assert.ErrorIs(t, err, tt.errType)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, committee.ID, result.ID)
				assert.Equal(t, committee.Name, result.Name)
			}
		})
	}
}

func TestService_ListCommittees(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := zap.NewNop()
	svc := NewService(logger, client)
	ctx := context.Background()

	tenantID := uuid.New()
	createdBy := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "List Committees Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	// Create multiple committees
	committeeNames := []string{"Evaluation", "Technical", "Financial", "Legal"}
	for _, name := range committeeNames {
		_, err := svc.CreateCommittee(ctx, CommitteeCreateParams{
			TenantID: tenantID,
			TenderID: tender.ID,
			Name:     name + " Committee",
		})
		require.NoError(t, err)
	}

	// List committees for tender
	committees, err := svc.ListCommittees(ctx, tenantID, tender.ID)
	require.NoError(t, err)
	assert.Len(t, committees, 4)

	// Verify order is by created_at ascending
	for i := 0; i < len(committees)-1; i++ {
		assert.True(t, committees[i].CreatedAt.Before(committees[i+1].CreatedAt) ||
			committees[i].CreatedAt.Equal(committees[i+1].CreatedAt))
	}

	// List from other tender should be empty
	otherTender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Other Tender",
		ClientName: "Other Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	otherCommittees, err := svc.ListCommittees(ctx, tenantID, otherTender.ID)
	require.NoError(t, err)
	assert.Len(t, otherCommittees, 0)
}

func TestService_UpdateCommittee(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := zap.NewNop()
	svc := NewService(logger, client)
	ctx := context.Background()

	tenantID := uuid.New()
	createdBy := uuid.New()
	newChairID := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Update Committee Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	committee, err := svc.CreateCommittee(ctx, CommitteeCreateParams{
		TenantID: tenantID,
		TenderID: tender.ID,
		Name:     "Original Committee",
	})
	require.NoError(t, err)

	newName := "Updated Committee Name"
	newType := "financial"
	newMandate := "Updated mandate"

	tests := []struct {
		name        string
		tenantID    uuid.UUID
		committeeID uuid.UUID
		params      CommitteeUpdateParams
		wantErr     bool
		validate    func(t *testing.T, committee interface{})
	}{
		{
			name:        "update name",
			tenantID:    tenantID,
			committeeID: committee.ID,
			params:      CommitteeUpdateParams{Name: &newName},
			wantErr:     false,
			validate: func(t *testing.T, c interface{}) {
				cm := c.(interface{ GetName() string })
				assert.Equal(t, newName, cm.GetName())
			},
		},
		{
			name:        "update multiple fields",
			tenantID:    tenantID,
			committeeID: committee.ID,
			params: CommitteeUpdateParams{
				CommitteeType: &newType,
				ChairID:       &newChairID,
				Mandate:       &newMandate,
			},
			wantErr: false,
		},
		{
			name:        "update non-existent committee",
			tenantID:    tenantID,
			committeeID: uuid.New(),
			params:      CommitteeUpdateParams{Name: &newName},
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.UpdateCommittee(ctx, tt.tenantID, tt.committeeID, tt.params)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				if tt.validate != nil {
					tt.validate(t, result)
				}
			}
		})
	}
}

func TestService_DissolveCommittee(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := zap.NewNop()
	svc := NewService(logger, client)
	ctx := context.Background()

	tenantID := uuid.New()
	createdBy := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Dissolve Committee Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	committee, err := svc.CreateCommittee(ctx, CommitteeCreateParams{
		TenantID: tenantID,
		TenderID: tender.ID,
		Name:     "To Dissolve",
	})
	require.NoError(t, err)
	assert.Equal(t, "active", committee.Status)

	// Dissolve the committee
	dissolved, err := svc.DissolveCommittee(ctx, tenantID, committee.ID)
	require.NoError(t, err)
	assert.Equal(t, "dissolved", dissolved.Status)
	assert.False(t, dissolved.DissolvedAt.IsZero())
}

func TestService_DeleteCommittee(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := zap.NewNop()
	svc := NewService(logger, client)
	ctx := context.Background()

	tenantID := uuid.New()
	createdBy := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Delete Committee Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	committee, err := svc.CreateCommittee(ctx, CommitteeCreateParams{
		TenantID: tenantID,
		TenderID: tender.ID,
		Name:     "To Delete",
	})
	require.NoError(t, err)

	// Delete committee
	err = svc.DeleteCommittee(ctx, tenantID, committee.ID)
	require.NoError(t, err)

	// Verify deletion
	_, err = svc.GetCommittee(ctx, tenantID, committee.ID)
	assert.ErrorIs(t, err, ErrCommitteeNotFound)

	// Delete again should fail
	err = svc.DeleteCommittee(ctx, tenantID, committee.ID)
	assert.Error(t, err)
}

func TestService_AddMember(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := zap.NewNop()
	svc := NewService(logger, client)
	ctx := context.Background()

	tenantID := uuid.New()
	createdBy := uuid.New()
	userID := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Add Member Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	committee, err := svc.CreateCommittee(ctx, CommitteeCreateParams{
		TenantID: tenantID,
		TenderID: tender.ID,
		Name:     "Test Committee",
	})
	require.NoError(t, err)

	tests := []struct {
		name    string
		params  MemberCreateParams
		wantErr bool
		errType error
	}{
		{
			name: "add member with required fields",
			params: MemberCreateParams{
				TenantID:    tenantID,
				CommitteeID: committee.ID,
				UserID:      userID,
				AddedBy:     createdBy,
			},
			wantErr: false,
		},
		{
			name: "add member with all optional fields",
			params: MemberCreateParams{
				TenantID:    tenantID,
				CommitteeID: committee.ID,
				UserID:      uuid.New(),
				Role:        "chair",
				Expertise:   "Financial Analysis",
				AddedBy:     createdBy,
				Metadata:    map[string]any{"department": "finance"},
			},
			wantErr: false,
		},
		{
			name: "add duplicate member",
			params: MemberCreateParams{
				TenantID:    tenantID,
				CommitteeID: committee.ID,
				UserID:      userID, // Already added
				AddedBy:     createdBy,
			},
			wantErr: true,
			errType: ErrDuplicateMember,
		},
		{
			name: "add member to non-existent committee",
			params: MemberCreateParams{
				TenantID:    tenantID,
				CommitteeID: uuid.New(),
				UserID:      uuid.New(),
				AddedBy:     createdBy,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			member, err := svc.AddMember(ctx, tt.params)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errType != nil {
					assert.ErrorIs(t, err, tt.errType)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, member)
				assert.Equal(t, tt.params.UserID, member.UserID)
				assert.Equal(t, tt.params.CommitteeID, member.CommitteeID)
				assert.True(t, member.IsActive)
			}
		})
	}
}

func TestService_GetMember(t *testing.T) {
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
	createdBy := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Get Member Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	committee, err := svc.CreateCommittee(ctx, CommitteeCreateParams{
		TenantID: tenantID,
		TenderID: tender.ID,
		Name:     "Test Committee",
	})
	require.NoError(t, err)

	member, err := svc.AddMember(ctx, MemberCreateParams{
		TenantID:    tenantID,
		CommitteeID: committee.ID,
		UserID:      uuid.New(),
		AddedBy:     createdBy,
	})
	require.NoError(t, err)

	tests := []struct {
		name     string
		tenantID uuid.UUID
		memberID uuid.UUID
		wantErr  bool
		errType  error
	}{
		{
			name:     "get existing member",
			tenantID: tenantID,
			memberID: member.ID,
			wantErr:  false,
		},
		{
			name:     "get non-existent member",
			tenantID: tenantID,
			memberID: uuid.New(),
			wantErr:  true,
			errType:  ErrMemberNotFound,
		},
		{
			name:     "get member from wrong tenant",
			tenantID: otherTenantID,
			memberID: member.ID,
			wantErr:  true,
			errType:  ErrMemberNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.GetMember(ctx, tt.tenantID, tt.memberID)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errType != nil {
					assert.ErrorIs(t, err, tt.errType)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, member.ID, result.ID)
			}
		})
	}
}

func TestService_UpdateMember(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := zap.NewNop()
	svc := NewService(logger, client)
	ctx := context.Background()

	tenantID := uuid.New()
	createdBy := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Update Member Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	committee, err := svc.CreateCommittee(ctx, CommitteeCreateParams{
		TenantID: tenantID,
		TenderID: tender.ID,
		Name:     "Test Committee",
	})
	require.NoError(t, err)

	member, err := svc.AddMember(ctx, MemberCreateParams{
		TenantID:    tenantID,
		CommitteeID: committee.ID,
		UserID:      uuid.New(),
		Role:        "member",
		AddedBy:     createdBy,
	})
	require.NoError(t, err)

	newRole := "chair"
	newExpertise := "Project Management"
	isActive := false

	tests := []struct {
		name     string
		tenantID uuid.UUID
		memberID uuid.UUID
		params   MemberUpdateParams
		wantErr  bool
		validate func(t *testing.T, member interface{})
	}{
		{
			name:     "update role",
			tenantID: tenantID,
			memberID: member.ID,
			params:   MemberUpdateParams{Role: &newRole},
			wantErr:  false,
			validate: func(t *testing.T, m interface{}) {
				mem := m.(interface{ GetRole() string })
				assert.Equal(t, newRole, mem.GetRole())
			},
		},
		{
			name:     "update expertise",
			tenantID: tenantID,
			memberID: member.ID,
			params:   MemberUpdateParams{Expertise: &newExpertise},
			wantErr:  false,
		},
		{
			name:     "deactivate member",
			tenantID: tenantID,
			memberID: member.ID,
			params:   MemberUpdateParams{IsActive: &isActive},
			wantErr:  false,
			validate: func(t *testing.T, m interface{}) {
				type activeCheck interface {
					GetIsActive() bool
					GetLeftAt() time.Time
				}
				mem := m.(activeCheck)
				assert.False(t, mem.GetIsActive())
				assert.False(t, mem.GetLeftAt().IsZero())
			},
		},
		{
			name:     "update non-existent member",
			tenantID: tenantID,
			memberID: uuid.New(),
			params:   MemberUpdateParams{Role: &newRole},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.UpdateMember(ctx, tt.tenantID, tt.memberID, tt.params)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				if tt.validate != nil {
					tt.validate(t, result)
				}
			}
		})
	}
}

func TestService_RemoveMember(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := zap.NewNop()
	svc := NewService(logger, client)
	ctx := context.Background()

	tenantID := uuid.New()
	createdBy := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Remove Member Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	committee, err := svc.CreateCommittee(ctx, CommitteeCreateParams{
		TenantID: tenantID,
		TenderID: tender.ID,
		Name:     "Test Committee",
	})
	require.NoError(t, err)

	member, err := svc.AddMember(ctx, MemberCreateParams{
		TenantID:    tenantID,
		CommitteeID: committee.ID,
		UserID:      uuid.New(),
		AddedBy:     createdBy,
	})
	require.NoError(t, err)

	// Remove member
	err = svc.RemoveMember(ctx, tenantID, member.ID)
	require.NoError(t, err)

	// Verify removal
	_, err = svc.GetMember(ctx, tenantID, member.ID)
	assert.ErrorIs(t, err, ErrMemberNotFound)

	// Remove again should fail
	err = svc.RemoveMember(ctx, tenantID, member.ID)
	assert.Error(t, err)
}

func TestService_CommitteeWithMembers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := zap.NewNop()
	svc := NewService(logger, client)
	ctx := context.Background()

	tenantID := uuid.New()
	createdBy := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Committee With Members Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	committee, err := svc.CreateCommittee(ctx, CommitteeCreateParams{
		TenantID: tenantID,
		TenderID: tender.ID,
		Name:     "Test Committee",
	})
	require.NoError(t, err)

	// Add multiple members
	roles := []string{"chair", "secretary", "member", "member", "observer"}
	for _, role := range roles {
		_, err := svc.AddMember(ctx, MemberCreateParams{
			TenantID:    tenantID,
			CommitteeID: committee.ID,
			UserID:      uuid.New(),
			Role:        role,
			AddedBy:     createdBy,
		})
		require.NoError(t, err)
	}

	// GetCommittee should include members
	result, err := svc.GetCommittee(ctx, tenantID, committee.ID)
	require.NoError(t, err)
	assert.Len(t, result.Edges.Members, 5)
}

func TestService_CommitteeTenantIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := zap.NewNop()
	svc := NewService(logger, client)
	ctx := context.Background()

	tenant1 := uuid.New()
	tenant2 := uuid.New()
	createdBy := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	// Create tender and committee for tenant 1
	tender1, err := svc.Create(ctx, CreateParams{
		TenantID:   tenant1,
		Title:      "Tenant 1 Tender",
		ClientName: "Client 1",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	committee1, err := svc.CreateCommittee(ctx, CommitteeCreateParams{
		TenantID: tenant1,
		TenderID: tender1.ID,
		Name:     "Tenant 1 Committee",
	})
	require.NoError(t, err)

	member1, err := svc.AddMember(ctx, MemberCreateParams{
		TenantID:    tenant1,
		CommitteeID: committee1.ID,
		UserID:      uuid.New(),
		AddedBy:     createdBy,
	})
	require.NoError(t, err)

	// Create tender and committee for tenant 2
	tender2, err := svc.Create(ctx, CreateParams{
		TenantID:   tenant2,
		Title:      "Tenant 2 Tender",
		ClientName: "Client 2",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	committee2, err := svc.CreateCommittee(ctx, CommitteeCreateParams{
		TenantID: tenant2,
		TenderID: tender2.ID,
		Name:     "Tenant 2 Committee",
	})
	require.NoError(t, err)

	// Cross-tenant committee access should fail
	_, err = svc.GetCommittee(ctx, tenant1, committee2.ID)
	assert.ErrorIs(t, err, ErrCommitteeNotFound)

	_, err = svc.GetCommittee(ctx, tenant2, committee1.ID)
	assert.ErrorIs(t, err, ErrCommitteeNotFound)

	// Cross-tenant member access should fail
	_, err = svc.GetMember(ctx, tenant2, member1.ID)
	assert.ErrorIs(t, err, ErrMemberNotFound)
}
