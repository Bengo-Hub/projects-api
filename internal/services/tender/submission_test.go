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

func TestService_CreateSubmission(t *testing.T) {
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
		Title:      "Submission Test Tender",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	tests := []struct {
		name    string
		params  SubmissionCreateParams
		wantErr bool
	}{
		{
			name: "create submission with required fields",
			params: SubmissionCreateParams{
				TenantID: tenantID,
				TenderID: tender.ID,
			},
			wantErr: false,
		},
		{
			name: "create email submission",
			params: SubmissionCreateParams{
				TenantID:       tenantID,
				TenderID:       tender.ID,
				SubmissionType: "email",
				RecipientEmail: "tenders@client.gov.ke",
				Documents: []map[string]any{
					{"name": "Technical Proposal", "file_id": "doc-001"},
					{"name": "Financial Proposal", "file_id": "doc-002"},
				},
				TotalPages: ptr(150),
				Notes:      "Submitted per RFP requirements",
			},
			wantErr: false,
		},
		{
			name: "create physical submission",
			params: SubmissionCreateParams{
				TenantID:         tenantID,
				TenderID:         tender.ID,
				SubmissionType:   "physical",
				RecipientAddress: "Ministry of Finance, P.O. Box 30007, Nairobi",
				CourierService:   "DHL Express",
				CopyCount:        ptr(3),
				TotalPages:       ptr(200),
				Metadata:         map[string]any{"package_weight": "2.5kg"},
			},
			wantErr: false,
		},
		{
			name: "create portal submission",
			params: SubmissionCreateParams{
				TenantID:       tenantID,
				TenderID:       tender.ID,
				SubmissionType: "portal",
				PortalURL:      "https://tender.treasury.go.ke/submit/123",
			},
			wantErr: false,
		},
		{
			name: "create submission for non-existent tender",
			params: SubmissionCreateParams{
				TenantID: tenantID,
				TenderID: uuid.New(),
			},
			wantErr: true,
		},
		{
			name: "create submission with wrong tenant",
			params: SubmissionCreateParams{
				TenantID: uuid.New(),
				TenderID: tender.ID,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			submission, err := svc.CreateSubmission(ctx, tt.params)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, submission)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, submission)
				assert.Equal(t, tt.params.TenderID, submission.TenderID)
				assert.Equal(t, "draft", submission.Status)
			}
		})
	}
}

func TestService_GetSubmission(t *testing.T) {
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
		Title:      "Get Submission Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	submission, err := svc.CreateSubmission(ctx, SubmissionCreateParams{
		TenantID:       tenantID,
		TenderID:       tender.ID,
		SubmissionType: "email",
	})
	require.NoError(t, err)

	tests := []struct {
		name         string
		tenantID     uuid.UUID
		submissionID uuid.UUID
		wantErr      bool
		errType      error
	}{
		{
			name:         "get existing submission",
			tenantID:     tenantID,
			submissionID: submission.ID,
			wantErr:      false,
		},
		{
			name:         "get non-existent submission",
			tenantID:     tenantID,
			submissionID: uuid.New(),
			wantErr:      true,
			errType:      ErrSubmissionNotFound,
		},
		{
			name:         "get submission from wrong tenant",
			tenantID:     otherTenantID,
			submissionID: submission.ID,
			wantErr:      true,
			errType:      ErrSubmissionNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.GetSubmission(ctx, tt.tenantID, tt.submissionID)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errType != nil {
					assert.ErrorIs(t, err, tt.errType)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, submission.ID, result.ID)
			}
		})
	}
}

func TestService_ListSubmissions(t *testing.T) {
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
		Title:      "List Submissions Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	// Create multiple submissions
	submissionTypes := []string{"email", "physical", "portal", "email", "physical"}
	for _, sType := range submissionTypes {
		_, err := svc.CreateSubmission(ctx, SubmissionCreateParams{
			TenantID:       tenantID,
			TenderID:       tender.ID,
			SubmissionType: sType,
		})
		require.NoError(t, err)
	}

	// List submissions
	submissions, err := svc.ListSubmissions(ctx, tenantID, tender.ID)
	require.NoError(t, err)
	assert.Len(t, submissions, 5)

	// Verify order is by created_at descending (newest first)
	for i := 0; i < len(submissions)-1; i++ {
		assert.True(t, submissions[i].CreatedAt.After(submissions[i+1].CreatedAt) ||
			submissions[i].CreatedAt.Equal(submissions[i+1].CreatedAt))
	}
}

func TestService_UpdateSubmission(t *testing.T) {
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
		Title:      "Update Submission Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	submission, err := svc.CreateSubmission(ctx, SubmissionCreateParams{
		TenantID:       tenantID,
		TenderID:       tender.ID,
		SubmissionType: "physical",
	})
	require.NoError(t, err)

	newStatus := "prepared"
	newTrackingNumber := "DHL1234567890"
	newCourierService := "DHL Express"
	estimatedDelivery := time.Now().Add(3 * 24 * time.Hour)

	tests := []struct {
		name         string
		tenantID     uuid.UUID
		submissionID uuid.UUID
		params       SubmissionUpdateParams
		wantErr      bool
		validate     func(t *testing.T, submission interface{})
	}{
		{
			name:         "update status",
			tenantID:     tenantID,
			submissionID: submission.ID,
			params:       SubmissionUpdateParams{Status: &newStatus},
			wantErr:      false,
			validate: func(t *testing.T, s interface{}) {
				sub := s.(interface{ GetStatus() string })
				assert.Equal(t, newStatus, sub.GetStatus())
			},
		},
		{
			name:         "update courier details",
			tenantID:     tenantID,
			submissionID: submission.ID,
			params: SubmissionUpdateParams{
				CourierService:    &newCourierService,
				TrackingNumber:    &newTrackingNumber,
				EstimatedDelivery: &estimatedDelivery,
			},
			wantErr: false,
		},
		{
			name:         "update non-existent submission",
			tenantID:     tenantID,
			submissionID: uuid.New(),
			params:       SubmissionUpdateParams{Status: &newStatus},
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.UpdateSubmission(ctx, tt.tenantID, tt.submissionID, tt.params)
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

func TestService_SubmitTender(t *testing.T) {
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
	submittedBy := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Submit Tender Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)
	assert.Equal(t, "new", tender.Status)

	submission, err := svc.CreateSubmission(ctx, SubmissionCreateParams{
		TenantID:       tenantID,
		TenderID:       tender.ID,
		SubmissionType: "portal",
		PortalURL:      "https://tender.go.ke/submit",
	})
	require.NoError(t, err)
	assert.Equal(t, "draft", submission.Status)

	// Submit the tender
	submitted, err := svc.SubmitTender(ctx, tenantID, submission.ID, submittedBy)
	require.NoError(t, err)
	assert.Equal(t, "submitted", submitted.Status)
	assert.Equal(t, submittedBy, submitted.SubmittedBy)
	assert.False(t, submitted.SubmittedAt.IsZero())

	// Verify tender status was updated
	updatedTender, err := svc.Get(ctx, tenantID, tender.ID)
	require.NoError(t, err)
	assert.Equal(t, "submitted", updatedTender.Status)
}

func TestService_ConfirmDelivery(t *testing.T) {
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
		Title:      "Confirm Delivery Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	submission, err := svc.CreateSubmission(ctx, SubmissionCreateParams{
		TenantID:         tenantID,
		TenderID:         tender.ID,
		SubmissionType:   "physical",
		CourierService:   "DHL Express",
		RecipientAddress: "Ministry of Finance, Nairobi",
	})
	require.NoError(t, err)

	// Confirm delivery
	proofURL := "https://storage.example.com/delivery-proof/receipt-001.pdf"
	confirmed, err := svc.ConfirmDelivery(ctx, tenantID, submission.ID, proofURL)
	require.NoError(t, err)
	assert.Equal(t, "confirmed", confirmed.Status)
	assert.Equal(t, proofURL, confirmed.DeliveryProofURL)
	assert.False(t, confirmed.DeliveredAt.IsZero())
}

func TestService_RecordEmailTracking(t *testing.T) {
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
		Title:      "Email Tracking Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	submission, err := svc.CreateSubmission(ctx, SubmissionCreateParams{
		TenantID:       tenantID,
		TenderID:       tender.ID,
		SubmissionType: "email",
		RecipientEmail: "tenders@client.com",
	})
	require.NoError(t, err)

	// Record email sent
	messageID := "msg-abc123xyz"
	tracked, err := svc.RecordEmailTracking(ctx, tenantID, submission.ID, messageID, false)
	require.NoError(t, err)
	assert.Equal(t, messageID, tracked.EmailMessageID)
	assert.False(t, tracked.EmailOpened)

	// Record email opened
	opened, err := svc.RecordEmailTracking(ctx, tenantID, submission.ID, messageID, true)
	require.NoError(t, err)
	assert.True(t, opened.EmailOpened)
	assert.False(t, opened.EmailOpenedAt.IsZero())
}

func TestService_DeleteSubmission(t *testing.T) {
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
		Title:      "Delete Submission Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	submission, err := svc.CreateSubmission(ctx, SubmissionCreateParams{
		TenantID: tenantID,
		TenderID: tender.ID,
	})
	require.NoError(t, err)

	// Delete submission
	err = svc.DeleteSubmission(ctx, tenantID, submission.ID)
	require.NoError(t, err)

	// Verify deletion
	_, err = svc.GetSubmission(ctx, tenantID, submission.ID)
	assert.ErrorIs(t, err, ErrSubmissionNotFound)
}

func TestService_SubmissionWorkflow_Email(t *testing.T) {
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
	submittedBy := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Email Workflow Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	// Step 1: Create email submission
	submission, err := svc.CreateSubmission(ctx, SubmissionCreateParams{
		TenantID:       tenantID,
		TenderID:       tender.ID,
		SubmissionType: "email",
		RecipientEmail: "tenders@ministry.gov.ke",
		Documents: []map[string]any{
			{"name": "Technical Proposal", "file_id": "doc-tech"},
			{"name": "Financial Proposal", "file_id": "doc-fin"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "draft", submission.Status)

	// Step 2: Submit
	submitted, err := svc.SubmitTender(ctx, tenantID, submission.ID, submittedBy)
	require.NoError(t, err)
	assert.Equal(t, "submitted", submitted.Status)

	// Step 3: Track email
	messageID := "email-msg-12345"
	tracked, err := svc.RecordEmailTracking(ctx, tenantID, submission.ID, messageID, false)
	require.NoError(t, err)
	assert.Equal(t, messageID, tracked.EmailMessageID)

	// Step 4: Email opened
	opened, err := svc.RecordEmailTracking(ctx, tenantID, submission.ID, messageID, true)
	require.NoError(t, err)
	assert.True(t, opened.EmailOpened)
	assert.False(t, opened.EmailOpenedAt.IsZero())
}

func TestService_SubmissionWorkflow_Physical(t *testing.T) {
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
	submittedBy := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Physical Workflow Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	// Step 1: Create physical submission
	submission, err := svc.CreateSubmission(ctx, SubmissionCreateParams{
		TenantID:         tenantID,
		TenderID:         tender.ID,
		SubmissionType:   "physical",
		RecipientAddress: "Ministry HQ, Box 30007, Nairobi",
		CopyCount:        ptr(3),
		TotalPages:       ptr(200),
	})
	require.NoError(t, err)

	// Step 2: Add courier details
	trackingNumber := "DHL1234567890"
	courierService := "DHL Express"
	estimatedDelivery := time.Now().Add(2 * 24 * time.Hour)
	_, err = svc.UpdateSubmission(ctx, tenantID, submission.ID, SubmissionUpdateParams{
		CourierService:    &courierService,
		TrackingNumber:    &trackingNumber,
		EstimatedDelivery: &estimatedDelivery,
	})
	require.NoError(t, err)

	// Step 3: Submit
	submitted, err := svc.SubmitTender(ctx, tenantID, submission.ID, submittedBy)
	require.NoError(t, err)
	assert.Equal(t, "submitted", submitted.Status)

	// Step 4: Confirm delivery
	proofURL := "https://storage.example.com/receipts/delivery-001.pdf"
	confirmed, err := svc.ConfirmDelivery(ctx, tenantID, submission.ID, proofURL)
	require.NoError(t, err)
	assert.Equal(t, "confirmed", confirmed.Status)
	assert.Equal(t, proofURL, confirmed.DeliveryProofURL)
}

func TestService_SubmissionTenantIsolation(t *testing.T) {
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

	// Create tender and submission for tenant 1
	tender1, err := svc.Create(ctx, CreateParams{
		TenantID:   tenant1,
		Title:      "Tenant 1 Tender",
		ClientName: "Client 1",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	submission1, err := svc.CreateSubmission(ctx, SubmissionCreateParams{
		TenantID: tenant1,
		TenderID: tender1.ID,
	})
	require.NoError(t, err)

	// Create tender and submission for tenant 2
	tender2, err := svc.Create(ctx, CreateParams{
		TenantID:   tenant2,
		Title:      "Tenant 2 Tender",
		ClientName: "Client 2",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	submission2, err := svc.CreateSubmission(ctx, SubmissionCreateParams{
		TenantID: tenant2,
		TenderID: tender2.ID,
	})
	require.NoError(t, err)

	// Cross-tenant access should fail
	_, err = svc.GetSubmission(ctx, tenant1, submission2.ID)
	assert.ErrorIs(t, err, ErrSubmissionNotFound)

	_, err = svc.GetSubmission(ctx, tenant2, submission1.ID)
	assert.ErrorIs(t, err, ErrSubmissionNotFound)

	// List only shows tenant's own submissions
	t1Submissions, err := svc.ListSubmissions(ctx, tenant1, tender1.ID)
	require.NoError(t, err)
	assert.Len(t, t1Submissions, 1)
	assert.Equal(t, submission1.ID, t1Submissions[0].ID)
}
