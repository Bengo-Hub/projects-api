package tender

import (
	"context"
	"fmt"
	"time"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/tender"
	"github.com/bengobox/projects-service/internal/ent/tenderevaluation"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// EvaluationCreateParams holds parameters for creating a tender evaluation.
type EvaluationCreateParams struct {
	TenantID       uuid.UUID
	TenderID       uuid.UUID
	EvaluatorID    uuid.UUID
	CommitteeID    *uuid.UUID
	TechnicalScore *float64
	FinancialScore *float64
	ResourceScore  *float64
	RiskScore      *float64
	Vote           string
	VoteComment    string
	Strengths      []string
	Weaknesses     []string
	Opportunities  []string
	Threats        []string
	Recommendation string
	Metadata       map[string]any
}

// EvaluationUpdateParams holds parameters for updating an evaluation.
type EvaluationUpdateParams struct {
	TechnicalScore *float64
	FinancialScore *float64
	ResourceScore  *float64
	RiskScore      *float64
	Vote           *string
	VoteComment    *string
	Strengths      []string
	Weaknesses     []string
	Opportunities  []string
	Threats        []string
	Recommendation *string
	IsFinal        *bool
	Metadata       map[string]any
}

// CreateEvaluation creates a new tender evaluation.
func (s *Service) CreateEvaluation(ctx context.Context, params EvaluationCreateParams) (*ent.TenderEvaluation, error) {
	// Verify tender exists and belongs to tenant
	_, err := s.db.Tender.Query().
		Where(
			tender.ID(params.TenderID),
			tender.TenantID(params.TenantID),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to verify tender: %w", err)
	}

	// Check if evaluator already has an evaluation for this tender
	exists, err := s.db.TenderEvaluation.Query().
		Where(
			tenderevaluation.TenderID(params.TenderID),
			tenderevaluation.EvaluatorID(params.EvaluatorID),
		).
		Exist(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing evaluation: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("evaluator already has an evaluation for this tender")
	}

	builder := s.db.TenderEvaluation.Create().
		SetTenantID(params.TenantID).
		SetTenderID(params.TenderID).
		SetEvaluatorID(params.EvaluatorID)

	if params.CommitteeID != nil {
		builder.SetCommitteeID(*params.CommitteeID)
	}
	if params.TechnicalScore != nil {
		builder.SetTechnicalScore(*params.TechnicalScore)
	}
	if params.FinancialScore != nil {
		builder.SetFinancialScore(*params.FinancialScore)
	}
	if params.ResourceScore != nil {
		builder.SetResourceScore(*params.ResourceScore)
	}
	if params.RiskScore != nil {
		builder.SetRiskScore(*params.RiskScore)
	}
	if params.Vote != "" {
		builder.SetVote(params.Vote)
	}
	if params.VoteComment != "" {
		builder.SetVoteComment(params.VoteComment)
	}
	if len(params.Strengths) > 0 {
		builder.SetStrengths(params.Strengths)
	}
	if len(params.Weaknesses) > 0 {
		builder.SetWeaknesses(params.Weaknesses)
	}
	if len(params.Opportunities) > 0 {
		builder.SetOpportunities(params.Opportunities)
	}
	if len(params.Threats) > 0 {
		builder.SetThreats(params.Threats)
	}
	if params.Recommendation != "" {
		builder.SetRecommendation(params.Recommendation)
	}
	if params.Metadata != nil {
		builder.SetMetadata(params.Metadata)
	}

	// Calculate overall score if individual scores provided
	overallScore := s.calculateOverallScore(params.TechnicalScore, params.FinancialScore, params.ResourceScore, params.RiskScore)
	if overallScore != nil {
		builder.SetOverallScore(*overallScore)
	}

	eval, err := builder.Save(ctx)
	if err != nil {
		s.logger.Error("failed to create tender evaluation",
			zap.String("tender_id", params.TenderID.String()),
			zap.String("evaluator_id", params.EvaluatorID.String()),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to create tender evaluation: %w", err)
	}

	s.logger.Info("tender evaluation created",
		zap.String("evaluation_id", eval.ID.String()),
		zap.String("tender_id", params.TenderID.String()),
	)

	return eval, nil
}

// GetEvaluation retrieves an evaluation by ID.
func (s *Service) GetEvaluation(ctx context.Context, tenantID, evaluationID uuid.UUID) (*ent.TenderEvaluation, error) {
	eval, err := s.db.TenderEvaluation.Query().
		Where(
			tenderevaluation.ID(evaluationID),
			tenderevaluation.TenantID(tenantID),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrEvaluationNotFound
		}
		return nil, fmt.Errorf("failed to get tender evaluation: %w", err)
	}
	return eval, nil
}

// ListEvaluations retrieves evaluations for a tender.
func (s *Service) ListEvaluations(ctx context.Context, tenantID, tenderID uuid.UUID) ([]*ent.TenderEvaluation, error) {
	evals, err := s.db.TenderEvaluation.Query().
		Where(
			tenderevaluation.TenantID(tenantID),
			tenderevaluation.TenderID(tenderID),
		).
		Order(ent.Desc(tenderevaluation.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list evaluations: %w", err)
	}
	return evals, nil
}

// UpdateEvaluation updates a tender evaluation.
func (s *Service) UpdateEvaluation(ctx context.Context, tenantID, evaluationID uuid.UUID, params EvaluationUpdateParams) (*ent.TenderEvaluation, error) {
	_, err := s.GetEvaluation(ctx, tenantID, evaluationID)
	if err != nil {
		return nil, err
	}

	builder := s.db.TenderEvaluation.UpdateOneID(evaluationID)

	if params.TechnicalScore != nil {
		builder.SetTechnicalScore(*params.TechnicalScore)
	}
	if params.FinancialScore != nil {
		builder.SetFinancialScore(*params.FinancialScore)
	}
	if params.ResourceScore != nil {
		builder.SetResourceScore(*params.ResourceScore)
	}
	if params.RiskScore != nil {
		builder.SetRiskScore(*params.RiskScore)
	}
	if params.Vote != nil {
		builder.SetVote(*params.Vote)
	}
	if params.VoteComment != nil {
		builder.SetVoteComment(*params.VoteComment)
	}
	if params.Strengths != nil {
		builder.SetStrengths(params.Strengths)
	}
	if params.Weaknesses != nil {
		builder.SetWeaknesses(params.Weaknesses)
	}
	if params.Opportunities != nil {
		builder.SetOpportunities(params.Opportunities)
	}
	if params.Threats != nil {
		builder.SetThreats(params.Threats)
	}
	if params.Recommendation != nil {
		builder.SetRecommendation(*params.Recommendation)
	}
	if params.IsFinal != nil {
		builder.SetIsFinal(*params.IsFinal)
		if *params.IsFinal {
			builder.SetSubmittedAt(time.Now())
		}
	}
	if params.Metadata != nil {
		builder.SetMetadata(params.Metadata)
	}

	// Recalculate overall score
	overallScore := s.calculateOverallScore(params.TechnicalScore, params.FinancialScore, params.ResourceScore, params.RiskScore)
	if overallScore != nil {
		builder.SetOverallScore(*overallScore)
	}

	eval, err := builder.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to update tender evaluation: %w", err)
	}

	return eval, nil
}

// SubmitEvaluation marks an evaluation as final/submitted.
func (s *Service) SubmitEvaluation(ctx context.Context, tenantID, evaluationID uuid.UUID) (*ent.TenderEvaluation, error) {
	isFinal := true
	return s.UpdateEvaluation(ctx, tenantID, evaluationID, EvaluationUpdateParams{
		IsFinal: &isFinal,
	})
}

// DeleteEvaluation removes an evaluation.
func (s *Service) DeleteEvaluation(ctx context.Context, tenantID, evaluationID uuid.UUID) error {
	_, err := s.GetEvaluation(ctx, tenantID, evaluationID)
	if err != nil {
		return err
	}

	err = s.db.TenderEvaluation.DeleteOneID(evaluationID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete evaluation: %w", err)
	}

	s.logger.Info("tender evaluation deleted",
		zap.String("evaluation_id", evaluationID.String()),
	)

	return nil
}

// GetEvaluationSummary returns aggregated evaluation data for a tender.
func (s *Service) GetEvaluationSummary(ctx context.Context, tenantID, tenderID uuid.UUID) (*EvaluationSummary, error) {
	evals, err := s.ListEvaluations(ctx, tenantID, tenderID)
	if err != nil {
		return nil, err
	}

	summary := &EvaluationSummary{
		TotalEvaluations: len(evals),
		VoteSummary:      make(map[string]int),
	}

	var techSum, finSum, resSum, riskSum, overallSum float64
	var techCount, finCount, resCount, riskCount, overallCount int

	for _, eval := range evals {
		if eval.IsFinal {
			summary.FinalizedCount++
		}

		if eval.Vote != "" {
			summary.VoteSummary[eval.Vote]++
		}

		if eval.TechnicalScore != 0 {
			techSum += eval.TechnicalScore
			techCount++
		}
		if eval.FinancialScore != 0 {
			finSum += eval.FinancialScore
			finCount++
		}
		if eval.ResourceScore != 0 {
			resSum += eval.ResourceScore
			resCount++
		}
		if eval.RiskScore != 0 {
			riskSum += eval.RiskScore
			riskCount++
		}
		if eval.OverallScore != 0 {
			overallSum += eval.OverallScore
			overallCount++
		}
	}

	if techCount > 0 {
		avg := techSum / float64(techCount)
		summary.AvgTechnicalScore = &avg
	}
	if finCount > 0 {
		avg := finSum / float64(finCount)
		summary.AvgFinancialScore = &avg
	}
	if resCount > 0 {
		avg := resSum / float64(resCount)
		summary.AvgResourceScore = &avg
	}
	if riskCount > 0 {
		avg := riskSum / float64(riskCount)
		summary.AvgRiskScore = &avg
	}
	if overallCount > 0 {
		avg := overallSum / float64(overallCount)
		summary.AvgOverallScore = &avg
	}

	return summary, nil
}

// EvaluationSummary holds aggregated evaluation data.
type EvaluationSummary struct {
	TotalEvaluations  int            `json:"total_evaluations"`
	FinalizedCount    int            `json:"finalized_count"`
	VoteSummary       map[string]int `json:"vote_summary"`
	AvgTechnicalScore *float64       `json:"avg_technical_score,omitempty"`
	AvgFinancialScore *float64       `json:"avg_financial_score,omitempty"`
	AvgResourceScore  *float64       `json:"avg_resource_score,omitempty"`
	AvgRiskScore      *float64       `json:"avg_risk_score,omitempty"`
	AvgOverallScore   *float64       `json:"avg_overall_score,omitempty"`
}

// calculateOverallScore calculates a weighted overall score from individual scores.
func (s *Service) calculateOverallScore(tech, fin, res, risk *float64) *float64 {
	// Weights: Technical 40%, Financial 30%, Resource 20%, Risk 10% (inverted)
	var sum float64
	var weightSum float64

	if tech != nil {
		sum += *tech * 0.4
		weightSum += 0.4
	}
	if fin != nil {
		sum += *fin * 0.3
		weightSum += 0.3
	}
	if res != nil {
		sum += *res * 0.2
		weightSum += 0.2
	}
	if risk != nil {
		// Risk is inverted: lower risk score = better
		sum += (100 - *risk) * 0.1
		weightSum += 0.1
	}

	if weightSum == 0 {
		return nil
	}

	overall := sum / weightSum
	return &overall
}
