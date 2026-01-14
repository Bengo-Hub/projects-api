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

func TestService_CreateMeeting(t *testing.T) {
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
	organizedBy := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)
	scheduledAt := time.Now().Add(7 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Meeting Test Tender",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	tests := []struct {
		name    string
		params  MeetingCreateParams
		wantErr bool
	}{
		{
			name: "create meeting with required fields",
			params: MeetingCreateParams{
				TenantID:    tenantID,
				TenderID:    tender.ID,
				Title:       "Kickoff Meeting",
				ScheduledAt: scheduledAt,
				OrganizedBy: organizedBy,
			},
			wantErr: false,
		},
		{
			name: "create virtual meeting with all fields",
			params: MeetingCreateParams{
				TenantID:        tenantID,
				TenderID:        tender.ID,
				Title:           "Technical Review",
				Description:     "Review technical proposal sections",
				MeetingType:     "review",
				ScheduledAt:     scheduledAt.Add(24 * time.Hour),
				DurationMinutes: 60,
				Platform:        "google_meet",
				MeetingURL:      "https://meet.google.com/abc-defg-hij",
				MeetingID:       "abc-defg-hij",
				Attendees:       []uuid.UUID{uuid.New(), uuid.New(), uuid.New()},
				Agenda:          "1. Technical Review\n2. Timeline Discussion\n3. Q&A",
				OrganizedBy:     organizedBy,
				Metadata:        map[string]any{"department": "technical"},
			},
			wantErr: false,
		},
		{
			name: "create in-person meeting",
			params: MeetingCreateParams{
				TenantID:        tenantID,
				TenderID:        tender.ID,
				Title:           "Client Site Visit",
				MeetingType:     "site_visit",
				ScheduledAt:     scheduledAt.Add(48 * time.Hour),
				DurationMinutes: 120,
				Location:        "Client HQ, Nairobi",
				OrganizedBy:     organizedBy,
			},
			wantErr: false,
		},
		{
			name: "create meeting for non-existent tender",
			params: MeetingCreateParams{
				TenantID:    tenantID,
				TenderID:    uuid.New(),
				Title:       "Orphan Meeting",
				ScheduledAt: scheduledAt,
				OrganizedBy: organizedBy,
			},
			wantErr: true,
		},
		{
			name: "create meeting with wrong tenant",
			params: MeetingCreateParams{
				TenantID:    uuid.New(),
				TenderID:    tender.ID,
				Title:       "Wrong Tenant Meeting",
				ScheduledAt: scheduledAt,
				OrganizedBy: organizedBy,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meeting, err := svc.CreateMeeting(ctx, tt.params)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, meeting)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, meeting)
				assert.Equal(t, tt.params.Title, meeting.Title)
				assert.Equal(t, tt.params.TenderID, meeting.TenderID)
				assert.Equal(t, "scheduled", meeting.Status)
			}
		})
	}
}

func TestService_GetMeeting(t *testing.T) {
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
		Title:      "Get Meeting Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	meeting, err := svc.CreateMeeting(ctx, MeetingCreateParams{
		TenantID:    tenantID,
		TenderID:    tender.ID,
		Title:       "Test Meeting",
		ScheduledAt: time.Now().Add(7 * 24 * time.Hour),
		OrganizedBy: createdBy,
	})
	require.NoError(t, err)

	tests := []struct {
		name      string
		tenantID  uuid.UUID
		meetingID uuid.UUID
		wantErr   bool
		errType   error
	}{
		{
			name:      "get existing meeting",
			tenantID:  tenantID,
			meetingID: meeting.ID,
			wantErr:   false,
		},
		{
			name:      "get non-existent meeting",
			tenantID:  tenantID,
			meetingID: uuid.New(),
			wantErr:   true,
			errType:   ErrMeetingNotFound,
		},
		{
			name:      "get meeting from wrong tenant",
			tenantID:  otherTenantID,
			meetingID: meeting.ID,
			wantErr:   true,
			errType:   ErrMeetingNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.GetMeeting(ctx, tt.tenantID, tt.meetingID)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errType != nil {
					assert.ErrorIs(t, err, tt.errType)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, meeting.ID, result.ID)
				assert.Equal(t, meeting.Title, result.Title)
			}
		})
	}
}

func TestService_ListMeetings(t *testing.T) {
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
	deadline := time.Now().Add(60 * 24 * time.Hour)
	baseSchedule := time.Now().Add(7 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "List Meetings Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	// Create multiple meetings with different types and dates
	meetingTypes := []string{"kickoff", "review", "review", "decision", "closeout"}
	for i, mType := range meetingTypes {
		_, err := svc.CreateMeeting(ctx, MeetingCreateParams{
			TenantID:    tenantID,
			TenderID:    tender.ID,
			Title:       "Meeting " + string(rune('A'+i)),
			MeetingType: mType,
			ScheduledAt: baseSchedule.Add(time.Duration(i) * 24 * time.Hour),
			OrganizedBy: createdBy,
		})
		require.NoError(t, err)
	}

	tests := []struct {
		name      string
		params    MeetingListParams
		wantCount int
		wantTotal int
	}{
		{
			name: "list all meetings",
			params: MeetingListParams{
				TenantID: tenantID,
				TenderID: tender.ID,
				Limit:    10,
			},
			wantCount: 5,
			wantTotal: 5,
		},
		{
			name: "list with pagination",
			params: MeetingListParams{
				TenantID: tenantID,
				TenderID: tender.ID,
				Limit:    2,
				Offset:   0,
			},
			wantCount: 2,
			wantTotal: 5,
		},
		{
			name: "filter by meeting type",
			params: MeetingListParams{
				TenantID:    tenantID,
				TenderID:    tender.ID,
				MeetingType: "review",
				Limit:       10,
			},
			wantCount: 2,
			wantTotal: 2,
		},
		{
			name: "filter by date range",
			params: MeetingListParams{
				TenantID: tenantID,
				TenderID: tender.ID,
				FromDate: ptr(baseSchedule),
				ToDate:   ptr(baseSchedule.Add(2 * 24 * time.Hour)),
				Limit:    10,
			},
			wantCount: 3,
			wantTotal: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meetings, total, err := svc.ListMeetings(ctx, tt.params)
			require.NoError(t, err)
			assert.Len(t, meetings, tt.wantCount)
			assert.Equal(t, tt.wantTotal, total)
		})
	}
}

func TestService_UpdateMeeting(t *testing.T) {
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
		Title:      "Update Meeting Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	meeting, err := svc.CreateMeeting(ctx, MeetingCreateParams{
		TenantID:    tenantID,
		TenderID:    tender.ID,
		Title:       "Original Title",
		ScheduledAt: time.Now().Add(7 * 24 * time.Hour),
		OrganizedBy: createdBy,
	})
	require.NoError(t, err)

	newTitle := "Updated Meeting Title"
	newDescription := "Updated description"
	newDuration := 90
	newPlatform := "teams"
	newMeetingURL := "https://teams.microsoft.com/meeting/123"

	tests := []struct {
		name      string
		tenantID  uuid.UUID
		meetingID uuid.UUID
		params    MeetingUpdateParams
		wantErr   bool
		validate  func(t *testing.T, meeting interface{})
	}{
		{
			name:      "update title",
			tenantID:  tenantID,
			meetingID: meeting.ID,
			params:    MeetingUpdateParams{Title: &newTitle},
			wantErr:   false,
			validate: func(t *testing.T, m interface{}) {
				mtg := m.(interface{ GetTitle() string })
				assert.Equal(t, newTitle, mtg.GetTitle())
			},
		},
		{
			name:      "update multiple fields",
			tenantID:  tenantID,
			meetingID: meeting.ID,
			params: MeetingUpdateParams{
				Description:     &newDescription,
				DurationMinutes: &newDuration,
				Platform:        &newPlatform,
				MeetingURL:      &newMeetingURL,
			},
			wantErr: false,
		},
		{
			name:      "update non-existent meeting",
			tenantID:  tenantID,
			meetingID: uuid.New(),
			params:    MeetingUpdateParams{Title: &newTitle},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.UpdateMeeting(ctx, tt.tenantID, tt.meetingID, tt.params)
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

func TestService_MeetingLifecycle(t *testing.T) {
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
		Title:      "Meeting Lifecycle Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	// Create a meeting
	meeting, err := svc.CreateMeeting(ctx, MeetingCreateParams{
		TenantID:    tenantID,
		TenderID:    tender.ID,
		Title:       "Lifecycle Test Meeting",
		ScheduledAt: time.Now().Add(1 * time.Hour),
		OrganizedBy: createdBy,
		Agenda:      "1. Item A\n2. Item B",
	})
	require.NoError(t, err)
	assert.Equal(t, "scheduled", meeting.Status)

	// Start the meeting
	started, err := svc.StartMeeting(ctx, tenantID, meeting.ID)
	require.NoError(t, err)
	assert.Equal(t, "in_progress", started.Status)
	assert.False(t, started.StartedAt.IsZero())

	// End the meeting with minutes and decisions
	minutes := "Discussion covered all agenda items. Key decisions were made."
	decisions := []map[string]any{
		{"decision": "Proceed with Phase 1", "approved_by": "Committee"},
		{"decision": "Allocate additional resources", "approved_by": "Project Lead"},
	}
	actionItems := []map[string]any{
		{"action": "Prepare Phase 1 plan", "assignee": "John", "due": "2024-02-01"},
		{"action": "Review budget", "assignee": "Finance", "due": "2024-01-25"},
	}

	ended, err := svc.EndMeeting(ctx, tenantID, meeting.ID, minutes, decisions, actionItems)
	require.NoError(t, err)
	assert.Equal(t, "completed", ended.Status)
	assert.Equal(t, minutes, ended.Minutes)
	assert.Len(t, ended.Decisions, 2)
	assert.Len(t, ended.ActionItems, 2)
	assert.False(t, ended.EndedAt.IsZero())
}

func TestService_CancelMeeting(t *testing.T) {
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
		Title:      "Cancel Meeting Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	meeting, err := svc.CreateMeeting(ctx, MeetingCreateParams{
		TenantID:    tenantID,
		TenderID:    tender.ID,
		Title:       "To Cancel",
		ScheduledAt: time.Now().Add(7 * 24 * time.Hour),
		OrganizedBy: createdBy,
	})
	require.NoError(t, err)
	assert.Equal(t, "scheduled", meeting.Status)

	// Cancel the meeting
	cancelled, err := svc.CancelMeeting(ctx, tenantID, meeting.ID)
	require.NoError(t, err)
	assert.Equal(t, "cancelled", cancelled.Status)
}

func TestService_DeleteMeeting(t *testing.T) {
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
		Title:      "Delete Meeting Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	meeting, err := svc.CreateMeeting(ctx, MeetingCreateParams{
		TenantID:    tenantID,
		TenderID:    tender.ID,
		Title:       "To Delete",
		ScheduledAt: time.Now().Add(7 * 24 * time.Hour),
		OrganizedBy: createdBy,
	})
	require.NoError(t, err)

	// Delete meeting
	err = svc.DeleteMeeting(ctx, tenantID, meeting.ID)
	require.NoError(t, err)

	// Verify deletion
	_, err = svc.GetMeeting(ctx, tenantID, meeting.ID)
	assert.ErrorIs(t, err, ErrMeetingNotFound)
}

func TestService_ListMeetingsByDateRange(t *testing.T) {
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
	deadline := time.Now().Add(90 * 24 * time.Hour)
	baseDate := time.Now()

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Date Range Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	// Create meetings over multiple weeks
	for i := 0; i < 10; i++ {
		_, err := svc.CreateMeeting(ctx, MeetingCreateParams{
			TenantID:    tenantID,
			TenderID:    tender.ID,
			Title:       "Meeting " + string(rune('A'+i)),
			ScheduledAt: baseDate.Add(time.Duration(i*3) * 24 * time.Hour), // Every 3 days
			OrganizedBy: createdBy,
		})
		require.NoError(t, err)
	}

	// Query first week
	firstWeekStart := baseDate
	firstWeekEnd := baseDate.Add(7 * 24 * time.Hour)
	firstWeek, total, err := svc.ListMeetings(ctx, MeetingListParams{
		TenantID: tenantID,
		TenderID: tender.ID,
		FromDate: &firstWeekStart,
		ToDate:   &firstWeekEnd,
		Limit:    100,
	})
	require.NoError(t, err)
	assert.Equal(t, 3, total) // Days 0, 3, 6
	assert.Len(t, firstWeek, 3)

	// Query second week
	secondWeekStart := baseDate.Add(7 * 24 * time.Hour)
	secondWeekEnd := baseDate.Add(14 * 24 * time.Hour)
	secondWeek, total, err := svc.ListMeetings(ctx, MeetingListParams{
		TenantID: tenantID,
		TenderID: tender.ID,
		FromDate: &secondWeekStart,
		ToDate:   &secondWeekEnd,
		Limit:    100,
	})
	require.NoError(t, err)
	assert.Equal(t, 2, total) // Days 9, 12
	assert.Len(t, secondWeek, 2)
}

func TestService_MeetingTenantIsolation(t *testing.T) {
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

	// Create tender and meeting for tenant 1
	tender1, err := svc.Create(ctx, CreateParams{
		TenantID:   tenant1,
		Title:      "Tenant 1 Tender",
		ClientName: "Client 1",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	meeting1, err := svc.CreateMeeting(ctx, MeetingCreateParams{
		TenantID:    tenant1,
		TenderID:    tender1.ID,
		Title:       "Tenant 1 Meeting",
		ScheduledAt: time.Now().Add(7 * 24 * time.Hour),
		OrganizedBy: createdBy,
	})
	require.NoError(t, err)

	// Create tender and meeting for tenant 2
	tender2, err := svc.Create(ctx, CreateParams{
		TenantID:   tenant2,
		Title:      "Tenant 2 Tender",
		ClientName: "Client 2",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	meeting2, err := svc.CreateMeeting(ctx, MeetingCreateParams{
		TenantID:    tenant2,
		TenderID:    tender2.ID,
		Title:       "Tenant 2 Meeting",
		ScheduledAt: time.Now().Add(7 * 24 * time.Hour),
		OrganizedBy: createdBy,
	})
	require.NoError(t, err)

	// Cross-tenant access should fail
	_, err = svc.GetMeeting(ctx, tenant1, meeting2.ID)
	assert.ErrorIs(t, err, ErrMeetingNotFound)

	_, err = svc.GetMeeting(ctx, tenant2, meeting1.ID)
	assert.ErrorIs(t, err, ErrMeetingNotFound)

	// List only shows tenant's own meetings
	t1Meetings, _, err := svc.ListMeetings(ctx, MeetingListParams{
		TenantID: tenant1,
		TenderID: tender1.ID,
		Limit:    10,
	})
	require.NoError(t, err)
	assert.Len(t, t1Meetings, 1)
	assert.Equal(t, meeting1.ID, t1Meetings[0].ID)
}
