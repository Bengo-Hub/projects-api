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
	createdBy := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tests := []struct {
		name    string
		params  CreateParams
		wantErr bool
		check   func(t *testing.T, tender interface{})
	}{
		{
			name: "create with required fields only",
			params: CreateParams{
				TenantID:   tenantID,
				Title:      "Test Tender",
				ClientName: "Test Client",
				Deadline:   deadline,
				CreatedBy:  createdBy,
			},
			wantErr: false,
			check: func(t *testing.T, tender interface{}) {
				td := tender.(interface{ GetTitle() string })
				assert.Equal(t, "Test Tender", td.GetTitle())
			},
		},
		{
			name: "create with all optional fields",
			params: CreateParams{
				TenantID:              tenantID,
				Title:                 "Full Tender",
				Description:           "A comprehensive tender",
				ClientName:            "Enterprise Client",
				ClientContact:         "+254700123456",
				ClientEmail:           "client@example.com",
				Source:                "government",
				SourceURL:             "https://tender.go.ke/123",
				Priority:              "high",
				EstimatedValue:        ptr(5000000.0),
				Currency:              "KES",
				PublicationDate:       ptr(time.Now()),
				Deadline:              deadline,
				ClarificationDeadline: ptr(time.Now().Add(15 * 24 * time.Hour)),
				SubmissionMethod:      "email",
				SubmissionAddress:     "tenders@client.com",
				Categories:            []string{"construction", "infrastructure"},
				RequirementsSummary:   map[string]any{"experience": "5 years", "certification": "ISO 9001"},
				CreatedBy:             createdBy,
				Metadata:              map[string]any{"internal_ref": "INT-001"},
			},
			wantErr: false,
		},
		{
			name: "tender number is auto-generated",
			params: CreateParams{
				TenantID:   tenantID,
				Title:      "Auto Number Tender",
				ClientName: "Client Co",
				Deadline:   deadline,
				CreatedBy:  createdBy,
			},
			wantErr: false,
			check: func(t *testing.T, tender interface{}) {
				td := tender.(interface{ GetTenderNumber() string })
				assert.Contains(t, td.GetTenderNumber(), "TND-")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			td, err := svc.Create(ctx, tt.params)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, td)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, td)
				assert.Equal(t, tt.params.Title, td.Title)
				assert.Equal(t, tt.params.TenantID, td.TenantID)
				assert.Equal(t, tt.params.ClientName, td.ClientName)
				assert.NotEmpty(t, td.TenderNumber)
				if tt.check != nil {
					tt.check(t, td)
				}
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
	createdBy := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	created, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Get Test Tender",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	tests := []struct {
		name     string
		tenantID uuid.UUID
		tenderID uuid.UUID
		wantErr  bool
		errType  error
	}{
		{
			name:     "get existing tender",
			tenantID: tenantID,
			tenderID: created.ID,
			wantErr:  false,
		},
		{
			name:     "get non-existent tender",
			tenantID: tenantID,
			tenderID: uuid.New(),
			wantErr:  true,
			errType:  ErrNotFound,
		},
		{
			name:     "get tender from wrong tenant (tenant isolation)",
			tenantID: otherTenantID,
			tenderID: created.ID,
			wantErr:  true,
			errType:  ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			td, err := svc.Get(ctx, tt.tenantID, tt.tenderID)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, td)
				if tt.errType != nil {
					assert.ErrorIs(t, err, tt.errType)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, created.ID, td.ID)
				assert.Equal(t, created.Title, td.Title)
			}
		})
	}
}

func TestService_GetByNumber(t *testing.T) {
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

	created, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Number Test Tender",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	tests := []struct {
		name         string
		tenantID     uuid.UUID
		tenderNumber string
		wantErr      bool
	}{
		{
			name:         "get by valid tender number",
			tenantID:     tenantID,
			tenderNumber: created.TenderNumber,
			wantErr:      false,
		},
		{
			name:         "get by non-existent number",
			tenantID:     tenantID,
			tenderNumber: "TND-NONEXISTENT",
			wantErr:      true,
		},
		{
			name:         "get by number from wrong tenant",
			tenantID:     otherTenantID,
			tenderNumber: created.TenderNumber,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			td, err := svc.GetByNumber(ctx, tt.tenantID, tt.tenderNumber)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, td)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.tenderNumber, td.TenderNumber)
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
	createdBy := uuid.New()
	otherCreator := uuid.New()
	baseDeadline := time.Now().Add(30 * 24 * time.Hour)

	// Create test tenders with varying attributes
	priorities := []string{"high", "high", "medium", "low", "low"}
	statuses := []string{"new", "evaluating", "evaluating", "preparing", "submitted"}
	sources := []string{"government", "government", "private", "government", "private"}

	for i := 0; i < 5; i++ {
		creator := createdBy
		if i%2 == 0 {
			creator = otherCreator
		}

		_, err := svc.Create(ctx, CreateParams{
			TenantID:   tenantID,
			Title:      "Tender " + string(rune('A'+i)),
			ClientName: "Client " + string(rune('A'+i)),
			Priority:   priorities[i],
			Source:     sources[i],
			Deadline:   baseDeadline.Add(time.Duration(i) * 24 * time.Hour),
			CreatedBy:  creator,
		})
		require.NoError(t, err)

		// Update status for non-default statuses
		if statuses[i] != "new" {
			lastCreated, _, _ := svc.List(ctx, ListParams{TenantID: tenantID, Limit: 1})
			if len(lastCreated) > 0 {
				_, err = svc.UpdateStatus(ctx, tenantID, lastCreated[0].ID, statuses[i])
				require.NoError(t, err)
			}
		}
	}

	tests := []struct {
		name      string
		params    ListParams
		wantCount int
		wantTotal int
	}{
		{
			name:      "list all tenders",
			params:    ListParams{TenantID: tenantID, Limit: 10},
			wantCount: 5,
			wantTotal: 5,
		},
		{
			name:      "list with pagination - page 1",
			params:    ListParams{TenantID: tenantID, Limit: 2, Offset: 0},
			wantCount: 2,
			wantTotal: 5,
		},
		{
			name:      "list with pagination - page 2",
			params:    ListParams{TenantID: tenantID, Limit: 2, Offset: 2},
			wantCount: 2,
			wantTotal: 5,
		},
		{
			name:      "filter by status",
			params:    ListParams{TenantID: tenantID, Status: "evaluating", Limit: 10},
			wantCount: 2,
			wantTotal: 2,
		},
		{
			name:      "filter by priority",
			params:    ListParams{TenantID: tenantID, Priority: "high", Limit: 10},
			wantCount: 2,
			wantTotal: 2,
		},
		{
			name:      "filter by source",
			params:    ListParams{TenantID: tenantID, Source: "government", Limit: 10},
			wantCount: 3,
			wantTotal: 3,
		},
		{
			name:      "filter by creator",
			params:    ListParams{TenantID: tenantID, CreatedBy: &createdBy, Limit: 10},
			wantCount: 2,
			wantTotal: 2,
		},
		{
			name:      "filter by deadline range",
			params:    ListParams{TenantID: tenantID, DeadlineFrom: &baseDeadline, DeadlineTo: ptr(baseDeadline.Add(2 * 24 * time.Hour)), Limit: 10},
			wantCount: 3,
			wantTotal: 3,
		},
		{
			name:      "list from empty tenant",
			params:    ListParams{TenantID: uuid.New(), Limit: 10},
			wantCount: 0,
			wantTotal: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tenders, total, err := svc.List(ctx, tt.params)
			require.NoError(t, err)
			assert.Len(t, tenders, tt.wantCount)
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
	createdBy := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	created, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Update Test Tender",
		ClientName: "Original Client",
		Priority:   "low",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	newTitle := "Updated Tender Title"
	newDescription := "Updated description"
	newPriority := "high"
	newValue := 1000000.0

	tests := []struct {
		name     string
		tenantID uuid.UUID
		tenderID uuid.UUID
		params   UpdateParams
		wantErr  bool
		validate func(t *testing.T, tender interface{})
	}{
		{
			name:     "update title",
			tenantID: tenantID,
			tenderID: created.ID,
			params:   UpdateParams{Title: &newTitle},
			wantErr:  false,
			validate: func(t *testing.T, tender interface{}) {
				td := tender.(interface{ GetTitle() string })
				assert.Equal(t, newTitle, td.GetTitle())
			},
		},
		{
			name:     "update multiple fields",
			tenantID: tenantID,
			tenderID: created.ID,
			params: UpdateParams{
				Description:    &newDescription,
				Priority:       &newPriority,
				EstimatedValue: &newValue,
			},
			wantErr: false,
			validate: func(t *testing.T, tender interface{}) {
				type multiField interface {
					GetDescription() string
					GetPriority() string
					GetEstimatedValue() float64
				}
				td := tender.(multiField)
				assert.Equal(t, newDescription, td.GetDescription())
				assert.Equal(t, newPriority, td.GetPriority())
				assert.Equal(t, newValue, td.GetEstimatedValue())
			},
		},
		{
			name:     "update non-existent tender",
			tenantID: tenantID,
			tenderID: uuid.New(),
			params:   UpdateParams{Title: &newTitle},
			wantErr:  true,
		},
		{
			name:     "update tender from wrong tenant",
			tenantID: uuid.New(),
			tenderID: created.ID,
			params:   UpdateParams{Title: &newTitle},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			td, err := svc.Update(ctx, tt.tenantID, tt.tenderID, tt.params)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				if tt.validate != nil {
					tt.validate(t, td)
				}
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
	createdBy := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	created, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Status Test Tender",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)
	assert.Equal(t, "new", created.Status)

	// Test status transitions
	statusTransitions := []string{"evaluating", "preparing", "submitted", "awarded"}
	for _, newStatus := range statusTransitions {
		td, err := svc.UpdateStatus(ctx, tenantID, created.ID, newStatus)
		require.NoError(t, err)
		assert.Equal(t, newStatus, td.Status)
	}
}

func TestService_RecordDecision(t *testing.T) {
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
	decidedBy := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tests := []struct {
		name           string
		decision       string
		rationale      string
		expectedStatus string
	}{
		{
			name:           "go decision moves to preparing",
			decision:       "go",
			rationale:      "Good opportunity with high ROI",
			expectedStatus: "preparing",
		},
		{
			name:           "no_go decision moves to withdrawn",
			decision:       "no_go",
			rationale:      "Too risky for current resources",
			expectedStatus: "withdrawn",
		},
		{
			name:           "evaluate_further stays in evaluating",
			decision:       "evaluate_further",
			rationale:      "Need more information",
			expectedStatus: "evaluating",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fresh tender for each test
			created, err := svc.Create(ctx, CreateParams{
				TenantID:   tenantID,
				Title:      "Decision Test: " + tt.name,
				ClientName: "Test Client",
				Deadline:   deadline,
				CreatedBy:  createdBy,
			})
			require.NoError(t, err)

			td, err := svc.RecordDecision(ctx, tenantID, created.ID, DecisionParams{
				Decision:  tt.decision,
				Rationale: tt.rationale,
				DecidedBy: decidedBy,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.decision, td.Decision)
			assert.Equal(t, tt.rationale, td.DecisionRationale)
			assert.Equal(t, decidedBy, td.DecidedBy)
			assert.Equal(t, tt.expectedStatus, td.Status)
			assert.False(t, td.DecisionDate.IsZero())
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
	createdBy := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	toDelete, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "To Delete",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	tests := []struct {
		name     string
		tenantID uuid.UUID
		tenderID uuid.UUID
		wantErr  bool
	}{
		{
			name:     "delete existing tender",
			tenantID: tenantID,
			tenderID: toDelete.ID,
			wantErr:  false,
		},
		{
			name:     "delete already deleted tender",
			tenantID: tenantID,
			tenderID: toDelete.ID,
			wantErr:  true,
		},
		{
			name:     "delete non-existent tender",
			tenantID: tenantID,
			tenderID: uuid.New(),
			wantErr:  true,
		},
		{
			name:     "delete from wrong tenant",
			tenantID: uuid.New(),
			tenderID: toDelete.ID,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.Delete(ctx, tt.tenantID, tt.tenderID)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// Verify deletion
				_, err := svc.Get(ctx, tt.tenantID, tt.tenderID)
				assert.ErrorIs(t, err, ErrNotFound)
			}
		})
	}
}

func TestService_TenantIsolation(t *testing.T) {
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

	// Create tenders for tenant 1
	t1Tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenant1,
		Title:      "Tenant 1 Tender",
		ClientName: "Client 1",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	// Create tenders for tenant 2
	t2Tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenant2,
		Title:      "Tenant 2 Tender",
		ClientName: "Client 2",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	// Tenant 1 should only see their tender
	t1List, t1Total, err := svc.List(ctx, ListParams{TenantID: tenant1, Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, 1, t1Total)
	assert.Equal(t, t1Tender.ID, t1List[0].ID)

	// Tenant 2 should only see their tender
	t2List, t2Total, err := svc.List(ctx, ListParams{TenantID: tenant2, Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, 1, t2Total)
	assert.Equal(t, t2Tender.ID, t2List[0].ID)

	// Cross-tenant access should fail
	_, err = svc.Get(ctx, tenant1, t2Tender.ID)
	assert.ErrorIs(t, err, ErrNotFound)

	_, err = svc.Get(ctx, tenant2, t1Tender.ID)
	assert.ErrorIs(t, err, ErrNotFound)
}

// ptr returns a pointer to the value.
func ptr[T any](v T) *T {
	return &v
}
