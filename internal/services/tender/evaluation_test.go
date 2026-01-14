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

func TestService_CreateEvaluation(t *testing.T) {
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
	evaluatorID := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Evaluation Test Tender",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	tests := []struct {
		name    string
		params  EvaluationCreateParams
		wantErr bool
	}{
		{
			name: "create evaluation with required fields",
			params: EvaluationCreateParams{
				TenantID:    tenantID,
				TenderID:    tender.ID,
				EvaluatorID: evaluatorID,
			},
			wantErr: false,
		},
		{
			name: "create evaluation with scores",
			params: EvaluationCreateParams{
				TenantID:       tenantID,
				TenderID:       tender.ID,
				EvaluatorID:    uuid.New(),
				TechnicalScore: ptr(85.0),
				FinancialScore: ptr(75.0),
				ResourceScore:  ptr(80.0),
				RiskScore:      ptr(30.0),
			},
			wantErr: false,
		},
		{
			name: "create evaluation with SWOT analysis",
			params: EvaluationCreateParams{
				TenantID:       tenantID,
				TenderID:       tender.ID,
				EvaluatorID:    uuid.New(),
				Strengths:      []string{"Strong technical team", "Proven track record"},
				Weaknesses:     []string{"Limited local presence"},
				Opportunities:  []string{"Expand market share", "Build government relations"},
				Threats:        []string{"Tight deadline", "Budget constraints"},
				Recommendation: "Proceed with caution",
			},
			wantErr: false,
		},
		{
			name: "create evaluation with vote",
			params: EvaluationCreateParams{
				TenantID:    tenantID,
				TenderID:    tender.ID,
				EvaluatorID: uuid.New(),
				Vote:        "go",
				VoteComment: "Strong proposal with acceptable risks",
			},
			wantErr: false,
		},
		{
			name: "create evaluation for non-existent tender",
			params: EvaluationCreateParams{
				TenantID:    tenantID,
				TenderID:    uuid.New(),
				EvaluatorID: uuid.New(),
			},
			wantErr: true,
		},
		{
			name: "create duplicate evaluation by same evaluator",
			params: EvaluationCreateParams{
				TenantID:    tenantID,
				TenderID:    tender.ID,
				EvaluatorID: evaluatorID, // Same as first test
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eval, err := svc.CreateEvaluation(ctx, tt.params)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, eval)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, eval)
				assert.Equal(t, tt.params.TenderID, eval.TenderID)
				assert.Equal(t, tt.params.EvaluatorID, eval.EvaluatorID)
				assert.False(t, eval.IsFinal)
			}
		})
	}
}

func TestService_CreateEvaluation_OverallScoreCalculation(t *testing.T) {
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
		Title:      "Score Calculation Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	tests := []struct {
		name          string
		techScore     *float64
		finScore      *float64
		resScore      *float64
		riskScore     *float64
		expectedRange [2]float64 // [min, max] for overall score
	}{
		{
			name:          "all scores provided",
			techScore:     ptr(80.0),
			finScore:      ptr(70.0),
			resScore:      ptr(90.0),
			riskScore:     ptr(20.0), // Low risk is good
			expectedRange: [2]float64{70.0, 90.0},
		},
		{
			name:          "only technical score",
			techScore:     ptr(85.0),
			expectedRange: [2]float64{80.0, 90.0},
		},
		{
			name:          "high risk score",
			techScore:     ptr(90.0),
			finScore:      ptr(90.0),
			resScore:      ptr(90.0),
			riskScore:     ptr(80.0), // High risk reduces overall
			expectedRange: [2]float64{80.0, 95.0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eval, err := svc.CreateEvaluation(ctx, EvaluationCreateParams{
				TenantID:       tenantID,
				TenderID:       tender.ID,
				EvaluatorID:    uuid.New(),
				TechnicalScore: tt.techScore,
				FinancialScore: tt.finScore,
				ResourceScore:  tt.resScore,
				RiskScore:      tt.riskScore,
			})
			require.NoError(t, err)

			if tt.techScore != nil || tt.finScore != nil || tt.resScore != nil || tt.riskScore != nil {
				assert.GreaterOrEqual(t, eval.OverallScore, tt.expectedRange[0])
				assert.LessOrEqual(t, eval.OverallScore, tt.expectedRange[1])
			}
		})
	}
}

func TestService_GetEvaluation(t *testing.T) {
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
		Title:      "Get Evaluation Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	eval, err := svc.CreateEvaluation(ctx, EvaluationCreateParams{
		TenantID:    tenantID,
		TenderID:    tender.ID,
		EvaluatorID: uuid.New(),
	})
	require.NoError(t, err)

	tests := []struct {
		name         string
		tenantID     uuid.UUID
		evaluationID uuid.UUID
		wantErr      bool
		errType      error
	}{
		{
			name:         "get existing evaluation",
			tenantID:     tenantID,
			evaluationID: eval.ID,
			wantErr:      false,
		},
		{
			name:         "get non-existent evaluation",
			tenantID:     tenantID,
			evaluationID: uuid.New(),
			wantErr:      true,
			errType:      ErrEvaluationNotFound,
		},
		{
			name:         "get evaluation from wrong tenant",
			tenantID:     otherTenantID,
			evaluationID: eval.ID,
			wantErr:      true,
			errType:      ErrEvaluationNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.GetEvaluation(ctx, tt.tenantID, tt.evaluationID)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errType != nil {
					assert.ErrorIs(t, err, tt.errType)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, eval.ID, result.ID)
			}
		})
	}
}

func TestService_ListEvaluations(t *testing.T) {
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
		Title:      "List Evaluations Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	// Create multiple evaluations
	for i := 0; i < 5; i++ {
		_, err := svc.CreateEvaluation(ctx, EvaluationCreateParams{
			TenantID:       tenantID,
			TenderID:       tender.ID,
			EvaluatorID:    uuid.New(),
			TechnicalScore: ptr(float64(70 + i*5)),
		})
		require.NoError(t, err)
	}

	// List evaluations
	evals, err := svc.ListEvaluations(ctx, tenantID, tender.ID)
	require.NoError(t, err)
	assert.Len(t, evals, 5)

	// Verify order is by created_at descending (newest first)
	for i := 0; i < len(evals)-1; i++ {
		assert.True(t, evals[i].CreatedAt.After(evals[i+1].CreatedAt) ||
			evals[i].CreatedAt.Equal(evals[i+1].CreatedAt))
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

	otherEvals, err := svc.ListEvaluations(ctx, tenantID, otherTender.ID)
	require.NoError(t, err)
	assert.Len(t, otherEvals, 0)
}

func TestService_UpdateEvaluation(t *testing.T) {
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
		Title:      "Update Evaluation Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	eval, err := svc.CreateEvaluation(ctx, EvaluationCreateParams{
		TenantID:       tenantID,
		TenderID:       tender.ID,
		EvaluatorID:    uuid.New(),
		TechnicalScore: ptr(70.0),
	})
	require.NoError(t, err)

	newTechScore := 85.0
	newFinScore := 80.0
	newVote := "go"
	newRecommendation := "Highly recommended"

	tests := []struct {
		name         string
		tenantID     uuid.UUID
		evaluationID uuid.UUID
		params       EvaluationUpdateParams
		wantErr      bool
		validate     func(t *testing.T, eval interface{})
	}{
		{
			name:         "update technical score",
			tenantID:     tenantID,
			evaluationID: eval.ID,
			params:       EvaluationUpdateParams{TechnicalScore: &newTechScore},
			wantErr:      false,
			validate: func(t *testing.T, e interface{}) {
				ev := e.(interface{ GetTechnicalScore() float64 })
				assert.Equal(t, newTechScore, ev.GetTechnicalScore())
			},
		},
		{
			name:         "update multiple scores",
			tenantID:     tenantID,
			evaluationID: eval.ID,
			params: EvaluationUpdateParams{
				FinancialScore: &newFinScore,
				Vote:           &newVote,
				Recommendation: &newRecommendation,
			},
			wantErr: false,
		},
		{
			name:         "update SWOT analysis",
			tenantID:     tenantID,
			evaluationID: eval.ID,
			params: EvaluationUpdateParams{
				Strengths:     []string{"Updated strength"},
				Weaknesses:    []string{"Updated weakness"},
				Opportunities: []string{"New opportunity"},
				Threats:       []string{"New threat"},
			},
			wantErr: false,
		},
		{
			name:         "update non-existent evaluation",
			tenantID:     tenantID,
			evaluationID: uuid.New(),
			params:       EvaluationUpdateParams{TechnicalScore: &newTechScore},
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.UpdateEvaluation(ctx, tt.tenantID, tt.evaluationID, tt.params)
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

func TestService_SubmitEvaluation(t *testing.T) {
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
		Title:      "Submit Evaluation Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	eval, err := svc.CreateEvaluation(ctx, EvaluationCreateParams{
		TenantID:       tenantID,
		TenderID:       tender.ID,
		EvaluatorID:    uuid.New(),
		TechnicalScore: ptr(85.0),
		Vote:           "go",
	})
	require.NoError(t, err)
	assert.False(t, eval.IsFinal)

	// Submit the evaluation
	submitted, err := svc.SubmitEvaluation(ctx, tenantID, eval.ID)
	require.NoError(t, err)
	assert.True(t, submitted.IsFinal)
	assert.False(t, submitted.SubmittedAt.IsZero())
}

func TestService_DeleteEvaluation(t *testing.T) {
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
		Title:      "Delete Evaluation Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	eval, err := svc.CreateEvaluation(ctx, EvaluationCreateParams{
		TenantID:    tenantID,
		TenderID:    tender.ID,
		EvaluatorID: uuid.New(),
	})
	require.NoError(t, err)

	// Delete evaluation
	err = svc.DeleteEvaluation(ctx, tenantID, eval.ID)
	require.NoError(t, err)

	// Verify deletion
	_, err = svc.GetEvaluation(ctx, tenantID, eval.ID)
	assert.ErrorIs(t, err, ErrEvaluationNotFound)
}

func TestService_GetEvaluationSummary(t *testing.T) {
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
		Title:      "Evaluation Summary Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	// Create evaluations with various scores and votes
	evaluations := []struct {
		techScore float64
		finScore  float64
		vote      string
		isFinal   bool
	}{
		{80.0, 75.0, "go", true},
		{85.0, 80.0, "go", true},
		{70.0, 65.0, "no_go", true},
		{90.0, 85.0, "go", false}, // Not finalized
		{60.0, 70.0, "abstain", false},
	}

	for _, e := range evaluations {
		eval, err := svc.CreateEvaluation(ctx, EvaluationCreateParams{
			TenantID:       tenantID,
			TenderID:       tender.ID,
			EvaluatorID:    uuid.New(),
			TechnicalScore: &e.techScore,
			FinancialScore: &e.finScore,
			Vote:           e.vote,
		})
		require.NoError(t, err)

		if e.isFinal {
			_, err = svc.SubmitEvaluation(ctx, tenantID, eval.ID)
			require.NoError(t, err)
		}
	}

	// Get summary
	summary, err := svc.GetEvaluationSummary(ctx, tenantID, tender.ID)
	require.NoError(t, err)

	assert.Equal(t, 5, summary.TotalEvaluations)
	assert.Equal(t, 3, summary.FinalizedCount)

	// Check vote summary
	assert.Equal(t, 3, summary.VoteSummary["go"])
	assert.Equal(t, 1, summary.VoteSummary["no_go"])
	assert.Equal(t, 1, summary.VoteSummary["abstain"])

	// Check average scores
	assert.NotNil(t, summary.AvgTechnicalScore)
	assert.NotNil(t, summary.AvgFinancialScore)

	// Average tech score should be (80+85+70+90+60)/5 = 77
	assert.InDelta(t, 77.0, *summary.AvgTechnicalScore, 0.1)
}

func TestService_GetEvaluationSummary_Empty(t *testing.T) {
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
		Title:      "Empty Summary Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	// Get summary for tender with no evaluations
	summary, err := svc.GetEvaluationSummary(ctx, tenantID, tender.ID)
	require.NoError(t, err)

	assert.Equal(t, 0, summary.TotalEvaluations)
	assert.Equal(t, 0, summary.FinalizedCount)
	assert.Nil(t, summary.AvgTechnicalScore)
	assert.Nil(t, summary.AvgFinancialScore)
}

func TestService_EvaluationTenantIsolation(t *testing.T) {
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

	// Create tender and evaluation for tenant 1
	tender1, err := svc.Create(ctx, CreateParams{
		TenantID:   tenant1,
		Title:      "Tenant 1 Tender",
		ClientName: "Client 1",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	eval1, err := svc.CreateEvaluation(ctx, EvaluationCreateParams{
		TenantID:    tenant1,
		TenderID:    tender1.ID,
		EvaluatorID: uuid.New(),
	})
	require.NoError(t, err)

	// Create tender and evaluation for tenant 2
	tender2, err := svc.Create(ctx, CreateParams{
		TenantID:   tenant2,
		Title:      "Tenant 2 Tender",
		ClientName: "Client 2",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	eval2, err := svc.CreateEvaluation(ctx, EvaluationCreateParams{
		TenantID:    tenant2,
		TenderID:    tender2.ID,
		EvaluatorID: uuid.New(),
	})
	require.NoError(t, err)

	// Cross-tenant access should fail
	_, err = svc.GetEvaluation(ctx, tenant1, eval2.ID)
	assert.ErrorIs(t, err, ErrEvaluationNotFound)

	_, err = svc.GetEvaluation(ctx, tenant2, eval1.ID)
	assert.ErrorIs(t, err, ErrEvaluationNotFound)

	// List only shows tenant's own evaluations
	t1Evals, err := svc.ListEvaluations(ctx, tenant1, tender1.ID)
	require.NoError(t, err)
	assert.Len(t, t1Evals, 1)
	assert.Equal(t, eval1.ID, t1Evals[0].ID)
}
