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

func TestService_CreateSection(t *testing.T) {
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
	assigneeID := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Section Test Tender",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	tests := []struct {
		name    string
		params  SectionCreateParams
		wantErr bool
	}{
		{
			name: "create section with required fields",
			params: SectionCreateParams{
				TenantID: tenantID,
				TenderID: tender.ID,
				Title:    "Executive Summary",
			},
			wantErr: false,
		},
		{
			name: "create section with all optional fields",
			params: SectionCreateParams{
				TenantID:      tenantID,
				TenderID:      tender.ID,
				Title:         "Technical Approach",
				Description:   "Detailed technical methodology",
				SectionNumber: "2.0",
				SortOrder:     2,
				SectionType:   "technical",
				AssigneeID:    &assigneeID,
				DueDate:       ptr(time.Now().Add(14 * 24 * time.Hour)),
				PageLimit:     ptr(20),
				ComplianceChecklist: []map[string]any{
					{"item": "ISO 9001 compliance", "required": true},
					{"item": "Local content", "required": true},
				},
				Metadata: map[string]any{"priority": "high"},
			},
			wantErr: false,
		},
		{
			name: "create section for non-existent tender",
			params: SectionCreateParams{
				TenantID: tenantID,
				TenderID: uuid.New(),
				Title:    "Orphan Section",
			},
			wantErr: true,
		},
		{
			name: "create section with wrong tenant",
			params: SectionCreateParams{
				TenantID: uuid.New(),
				TenderID: tender.ID,
				Title:    "Wrong Tenant Section",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			section, err := svc.CreateSection(ctx, tt.params)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, section)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, section)
				assert.Equal(t, tt.params.Title, section.Title)
				assert.Equal(t, tt.params.TenderID, section.TenderID)
				assert.Equal(t, "draft", section.Status)
			}
		})
	}
}

func TestService_CreateSection_Hierarchy(t *testing.T) {
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
		Title:      "Hierarchy Test Tender",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	// Create parent section
	parent, err := svc.CreateSection(ctx, SectionCreateParams{
		TenantID:      tenantID,
		TenderID:      tender.ID,
		Title:         "Technical Approach",
		SectionNumber: "2.0",
		SortOrder:     2,
	})
	require.NoError(t, err)

	// Create child sections
	children := []struct {
		title         string
		sectionNumber string
		sortOrder     int
	}{
		{"Methodology", "2.1", 1},
		{"Tools and Technologies", "2.2", 2},
		{"Quality Assurance", "2.3", 3},
	}

	for _, child := range children {
		section, err := svc.CreateSection(ctx, SectionCreateParams{
			TenantID:      tenantID,
			TenderID:      tender.ID,
			ParentID:      &parent.ID,
			Title:         child.title,
			SectionNumber: child.sectionNumber,
			SortOrder:     child.sortOrder,
		})
		require.NoError(t, err)
		assert.Equal(t, parent.ID, *section.ParentID)
	}

	// Verify parent-child relationship
	parentWithChildren, err := svc.GetSection(ctx, tenantID, parent.ID)
	require.NoError(t, err)
	assert.Len(t, parentWithChildren.Edges.Children, 3)
}

func TestService_GetSection(t *testing.T) {
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
		Title:      "Get Section Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	section, err := svc.CreateSection(ctx, SectionCreateParams{
		TenantID: tenantID,
		TenderID: tender.ID,
		Title:    "Test Section",
	})
	require.NoError(t, err)

	tests := []struct {
		name      string
		tenantID  uuid.UUID
		sectionID uuid.UUID
		wantErr   bool
		errType   error
	}{
		{
			name:      "get existing section",
			tenantID:  tenantID,
			sectionID: section.ID,
			wantErr:   false,
		},
		{
			name:      "get non-existent section",
			tenantID:  tenantID,
			sectionID: uuid.New(),
			wantErr:   true,
			errType:   ErrSectionNotFound,
		},
		{
			name:      "get section from wrong tenant",
			tenantID:  otherTenantID,
			sectionID: section.ID,
			wantErr:   true,
			errType:   ErrSectionNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.GetSection(ctx, tt.tenantID, tt.sectionID)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errType != nil {
					assert.ErrorIs(t, err, tt.errType)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, section.ID, result.ID)
				assert.Equal(t, section.Title, result.Title)
			}
		})
	}
}

func TestService_ListSections(t *testing.T) {
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
	assignee1 := uuid.New()
	assignee2 := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "List Sections Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	// Create parent sections
	parentSections := []struct {
		title     string
		assignee  *uuid.UUID
		sortOrder int
	}{
		{"Executive Summary", &assignee1, 1},
		{"Technical Approach", &assignee2, 2},
		{"Management Plan", &assignee1, 3},
		{"Pricing", nil, 4},
		{"Appendices", &assignee2, 5},
	}

	for _, ps := range parentSections {
		_, err := svc.CreateSection(ctx, SectionCreateParams{
			TenantID:   tenantID,
			TenderID:   tender.ID,
			Title:      ps.title,
			AssigneeID: ps.assignee,
			SortOrder:  ps.sortOrder,
		})
		require.NoError(t, err)
	}

	tests := []struct {
		name      string
		params    SectionListParams
		wantCount int
		wantTotal int
	}{
		{
			name: "list all sections",
			params: SectionListParams{
				TenantID: tenantID,
				TenderID: tender.ID,
				Limit:    10,
			},
			wantCount: 5,
			wantTotal: 5,
		},
		{
			name: "list with pagination",
			params: SectionListParams{
				TenantID: tenantID,
				TenderID: tender.ID,
				Limit:    2,
				Offset:   0,
			},
			wantCount: 2,
			wantTotal: 5,
		},
		{
			name: "filter by assignee",
			params: SectionListParams{
				TenantID:   tenantID,
				TenderID:   tender.ID,
				AssigneeID: &assignee1,
				Limit:      10,
			},
			wantCount: 2,
			wantTotal: 2,
		},
		{
			name: "filter parent sections only",
			params: SectionListParams{
				TenantID:   tenantID,
				TenderID:   tender.ID,
				ParentOnly: true,
				Limit:      10,
			},
			wantCount: 5,
			wantTotal: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sections, total, err := svc.ListSections(ctx, tt.params)
			require.NoError(t, err)
			assert.Len(t, sections, tt.wantCount)
			assert.Equal(t, tt.wantTotal, total)
		})
	}
}

func TestService_UpdateSection(t *testing.T) {
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
		Title:      "Update Section Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	section, err := svc.CreateSection(ctx, SectionCreateParams{
		TenantID: tenantID,
		TenderID: tender.ID,
		Title:    "Original Title",
	})
	require.NoError(t, err)

	newTitle := "Updated Title"
	newContent := "This is the section content."
	newStatus := "in_progress"

	tests := []struct {
		name      string
		tenantID  uuid.UUID
		sectionID uuid.UUID
		params    SectionUpdateParams
		wantErr   bool
		validate  func(t *testing.T, section interface{})
	}{
		{
			name:      "update title",
			tenantID:  tenantID,
			sectionID: section.ID,
			params:    SectionUpdateParams{Title: &newTitle},
			wantErr:   false,
			validate: func(t *testing.T, s interface{}) {
				sec := s.(interface{ GetTitle() string })
				assert.Equal(t, newTitle, sec.GetTitle())
			},
		},
		{
			name:      "update content with word count",
			tenantID:  tenantID,
			sectionID: section.ID,
			params:    SectionUpdateParams{Content: &newContent},
			wantErr:   false,
			validate: func(t *testing.T, s interface{}) {
				type sectionCheck interface {
					GetContent() string
					GetWordCount() int
				}
				sec := s.(sectionCheck)
				assert.Equal(t, newContent, sec.GetContent())
				assert.Greater(t, sec.GetWordCount(), 0)
			},
		},
		{
			name:      "update status tracks started_at",
			tenantID:  tenantID,
			sectionID: section.ID,
			params:    SectionUpdateParams{Status: &newStatus},
			wantErr:   false,
			validate: func(t *testing.T, s interface{}) {
				type sectionCheck interface {
					GetStatus() string
					GetStartedAt() time.Time
				}
				sec := s.(sectionCheck)
				assert.Equal(t, newStatus, sec.GetStatus())
				assert.False(t, sec.GetStartedAt().IsZero())
			},
		},
		{
			name:      "update non-existent section",
			tenantID:  tenantID,
			sectionID: uuid.New(),
			params:    SectionUpdateParams{Title: &newTitle},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.UpdateSection(ctx, tt.tenantID, tt.sectionID, tt.params)
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

func TestService_AssignSection(t *testing.T) {
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
	assigneeID := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Assign Section Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	section, err := svc.CreateSection(ctx, SectionCreateParams{
		TenantID: tenantID,
		TenderID: tender.ID,
		Title:    "Unassigned Section",
	})
	require.NoError(t, err)
	assert.Equal(t, uuid.Nil, section.AssigneeID)

	// Assign the section
	assigned, err := svc.AssignSection(ctx, tenantID, section.ID, assigneeID)
	require.NoError(t, err)
	assert.Equal(t, assigneeID, assigned.AssigneeID)
}

func TestService_SectionReviewWorkflow(t *testing.T) {
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
	reviewerID := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Review Workflow Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	section, err := svc.CreateSection(ctx, SectionCreateParams{
		TenantID: tenantID,
		TenderID: tender.ID,
		Title:    "Technical Approach",
	})
	require.NoError(t, err)
	assert.Equal(t, "draft", section.Status)

	// Submit for review
	submitted, err := svc.SubmitSectionForReview(ctx, tenantID, section.ID, reviewerID)
	require.NoError(t, err)
	assert.Equal(t, "review", submitted.Status)
	assert.Equal(t, "pending_technical", submitted.ReviewStatus)
	assert.Equal(t, reviewerID, submitted.ReviewerID)

	// Approve the section
	approvedComments := "Excellent technical approach. Well-structured."
	approved, err := svc.ApproveSection(ctx, tenantID, section.ID, reviewerID, approvedComments)
	require.NoError(t, err)
	assert.Equal(t, "approved", approved.Status)
	assert.Equal(t, "approved", approved.ReviewStatus)
	assert.Equal(t, approvedComments, approved.ReviewerComments)
	assert.False(t, approved.CompletedAt.IsZero())
}

func TestService_RejectSection(t *testing.T) {
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
	reviewerID := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Reject Section Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	section, err := svc.CreateSection(ctx, SectionCreateParams{
		TenantID: tenantID,
		TenderID: tender.ID,
		Title:    "Needs Improvement",
	})
	require.NoError(t, err)

	// Submit for review
	_, err = svc.SubmitSectionForReview(ctx, tenantID, section.ID, reviewerID)
	require.NoError(t, err)

	// Reject the section
	rejectionComments := "Missing key details about implementation timeline."
	rejected, err := svc.RejectSection(ctx, tenantID, section.ID, reviewerID, rejectionComments)
	require.NoError(t, err)
	assert.Equal(t, "rejected", rejected.Status)
	assert.Equal(t, rejectionComments, rejected.ReviewerComments)
}

func TestService_DeleteSection(t *testing.T) {
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
		Title:      "Delete Section Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	section, err := svc.CreateSection(ctx, SectionCreateParams{
		TenantID: tenantID,
		TenderID: tender.ID,
		Title:    "To Delete",
	})
	require.NoError(t, err)

	// Delete section
	err = svc.DeleteSection(ctx, tenantID, section.ID)
	require.NoError(t, err)

	// Verify deletion
	_, err = svc.GetSection(ctx, tenantID, section.ID)
	assert.ErrorIs(t, err, ErrSectionNotFound)
}

func TestService_GetSectionProgress(t *testing.T) {
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
	assignee1 := uuid.New()
	assignee2 := uuid.New()
	reviewerID := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Progress Test Tender",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	// Create sections with different statuses
	sections := []struct {
		title    string
		assignee *uuid.UUID
		status   string
		overdue  bool
	}{
		{"Section A", &assignee1, "draft", false},
		{"Section B", &assignee1, "in_progress", false},
		{"Section C", &assignee2, "approved", false},
		{"Section D", &assignee2, "approved", false},
		{"Section E", nil, "draft", true}, // Unassigned and overdue
	}

	for _, s := range sections {
		dueDate := time.Now().Add(14 * 24 * time.Hour)
		if s.overdue {
			dueDate = time.Now().Add(-24 * time.Hour) // Past due
		}

		section, err := svc.CreateSection(ctx, SectionCreateParams{
			TenantID:   tenantID,
			TenderID:   tender.ID,
			Title:      s.title,
			AssigneeID: s.assignee,
			DueDate:    &dueDate,
		})
		require.NoError(t, err)

		// Update status if not draft
		if s.status == "approved" {
			_, err = svc.ApproveSection(ctx, tenantID, section.ID, reviewerID, "Approved")
			require.NoError(t, err)
		} else if s.status != "draft" {
			status := s.status
			_, err = svc.UpdateSection(ctx, tenantID, section.ID, SectionUpdateParams{Status: &status})
			require.NoError(t, err)
		}
	}

	// Get progress
	progress, err := svc.GetSectionProgress(ctx, tenantID, tender.ID)
	require.NoError(t, err)

	assert.Equal(t, 5, progress.Total)
	assert.Equal(t, 2, progress.Completed)
	assert.Equal(t, 40.0, progress.CompletionPercent)
	assert.Equal(t, 1, progress.Unassigned)
	assert.Equal(t, 1, progress.Overdue)

	// Check status breakdown
	assert.Equal(t, 2, progress.ByStatus["draft"])
	assert.Equal(t, 1, progress.ByStatus["in_progress"])
	assert.Equal(t, 2, progress.ByStatus["approved"])

	// Check assignee breakdown
	assert.Equal(t, 2, progress.ByAssignee[assignee1.String()])
	assert.Equal(t, 2, progress.ByAssignee[assignee2.String()])
}

func TestService_SectionTenantIsolation(t *testing.T) {
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

	// Create tender and section for tenant 1
	tender1, err := svc.Create(ctx, CreateParams{
		TenantID:   tenant1,
		Title:      "Tenant 1 Tender",
		ClientName: "Client 1",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	section1, err := svc.CreateSection(ctx, SectionCreateParams{
		TenantID: tenant1,
		TenderID: tender1.ID,
		Title:    "Tenant 1 Section",
	})
	require.NoError(t, err)

	// Create tender and section for tenant 2
	tender2, err := svc.Create(ctx, CreateParams{
		TenantID:   tenant2,
		Title:      "Tenant 2 Tender",
		ClientName: "Client 2",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	section2, err := svc.CreateSection(ctx, SectionCreateParams{
		TenantID: tenant2,
		TenderID: tender2.ID,
		Title:    "Tenant 2 Section",
	})
	require.NoError(t, err)

	// Cross-tenant access should fail
	_, err = svc.GetSection(ctx, tenant1, section2.ID)
	assert.ErrorIs(t, err, ErrSectionNotFound)

	_, err = svc.GetSection(ctx, tenant2, section1.ID)
	assert.ErrorIs(t, err, ErrSectionNotFound)

	// List only shows tenant's own sections
	t1Sections, _, err := svc.ListSections(ctx, SectionListParams{
		TenantID: tenant1,
		TenderID: tender1.ID,
		Limit:    10,
	})
	require.NoError(t, err)
	assert.Len(t, t1Sections, 1)
	assert.Equal(t, section1.ID, t1Sections[0].ID)
}
