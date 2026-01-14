package tender

import (
	"context"
	"fmt"
	"time"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/tender"
	"github.com/bengobox/projects-service/internal/ent/tendercommittee"
	"github.com/bengobox/projects-service/internal/ent/tendercommitteemember"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// CommitteeCreateParams holds parameters for creating a tender committee.
type CommitteeCreateParams struct {
	TenantID      uuid.UUID
	TenderID      uuid.UUID
	Name          string
	CommitteeType string
	ChairID       *uuid.UUID
	Mandate       string
	Metadata      map[string]any
}

// CommitteeUpdateParams holds parameters for updating a committee.
type CommitteeUpdateParams struct {
	Name          *string
	CommitteeType *string
	ChairID       *uuid.UUID
	Mandate       *string
	Status        *string
	Metadata      map[string]any
}

// MemberCreateParams holds parameters for adding a committee member.
type MemberCreateParams struct {
	TenantID    uuid.UUID
	CommitteeID uuid.UUID
	UserID      uuid.UUID
	Role        string
	Expertise   string
	AddedBy     uuid.UUID
	Metadata    map[string]any
}

// MemberUpdateParams holds parameters for updating a committee member.
type MemberUpdateParams struct {
	Role      *string
	Expertise *string
	IsActive  *bool
	Metadata  map[string]any
}

// CreateCommittee creates a new tender committee.
func (s *Service) CreateCommittee(ctx context.Context, params CommitteeCreateParams) (*ent.TenderCommittee, error) {
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

	builder := s.db.TenderCommittee.Create().
		SetTenantID(params.TenantID).
		SetTenderID(params.TenderID).
		SetName(params.Name)

	if params.CommitteeType != "" {
		builder.SetCommitteeType(params.CommitteeType)
	}
	if params.ChairID != nil {
		builder.SetChairID(*params.ChairID)
	}
	if params.Mandate != "" {
		builder.SetMandate(params.Mandate)
	}
	if params.Metadata != nil {
		builder.SetMetadata(params.Metadata)
	}

	committee, err := builder.Save(ctx)
	if err != nil {
		s.logger.Error("failed to create tender committee",
			zap.String("tender_id", params.TenderID.String()),
			zap.String("name", params.Name),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to create tender committee: %w", err)
	}

	s.logger.Info("tender committee created",
		zap.String("committee_id", committee.ID.String()),
		zap.String("tender_id", params.TenderID.String()),
	)

	return committee, nil
}

// GetCommittee retrieves a committee by ID.
func (s *Service) GetCommittee(ctx context.Context, tenantID, committeeID uuid.UUID) (*ent.TenderCommittee, error) {
	committee, err := s.db.TenderCommittee.Query().
		Where(
			tendercommittee.ID(committeeID),
			tendercommittee.TenantID(tenantID),
		).
		WithMembers().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrCommitteeNotFound
		}
		return nil, fmt.Errorf("failed to get tender committee: %w", err)
	}
	return committee, nil
}

// ListCommittees retrieves committees for a tender.
func (s *Service) ListCommittees(ctx context.Context, tenantID, tenderID uuid.UUID) ([]*ent.TenderCommittee, error) {
	committees, err := s.db.TenderCommittee.Query().
		Where(
			tendercommittee.TenantID(tenantID),
			tendercommittee.TenderID(tenderID),
		).
		WithMembers().
		Order(ent.Asc(tendercommittee.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list committees: %w", err)
	}
	return committees, nil
}

// UpdateCommittee updates a tender committee.
func (s *Service) UpdateCommittee(ctx context.Context, tenantID, committeeID uuid.UUID, params CommitteeUpdateParams) (*ent.TenderCommittee, error) {
	_, err := s.GetCommittee(ctx, tenantID, committeeID)
	if err != nil {
		return nil, err
	}

	builder := s.db.TenderCommittee.UpdateOneID(committeeID)

	if params.Name != nil {
		builder.SetName(*params.Name)
	}
	if params.CommitteeType != nil {
		builder.SetCommitteeType(*params.CommitteeType)
	}
	if params.ChairID != nil {
		builder.SetChairID(*params.ChairID)
	}
	if params.Mandate != nil {
		builder.SetMandate(*params.Mandate)
	}
	if params.Status != nil {
		builder.SetStatus(*params.Status)
		if *params.Status == "dissolved" {
			builder.SetDissolvedAt(time.Now())
		}
	}
	if params.Metadata != nil {
		builder.SetMetadata(params.Metadata)
	}

	committee, err := builder.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to update committee: %w", err)
	}

	return committee, nil
}

// DissolveCommittee marks a committee as dissolved.
func (s *Service) DissolveCommittee(ctx context.Context, tenantID, committeeID uuid.UUID) (*ent.TenderCommittee, error) {
	status := "dissolved"
	return s.UpdateCommittee(ctx, tenantID, committeeID, CommitteeUpdateParams{
		Status: &status,
	})
}

// DeleteCommittee removes a committee.
func (s *Service) DeleteCommittee(ctx context.Context, tenantID, committeeID uuid.UUID) error {
	_, err := s.GetCommittee(ctx, tenantID, committeeID)
	if err != nil {
		return err
	}

	err = s.db.TenderCommittee.DeleteOneID(committeeID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete committee: %w", err)
	}

	s.logger.Info("tender committee deleted",
		zap.String("committee_id", committeeID.String()),
	)

	return nil
}

// AddMember adds a member to a committee.
func (s *Service) AddMember(ctx context.Context, params MemberCreateParams) (*ent.TenderCommitteeMember, error) {
	// Verify committee exists
	_, err := s.GetCommittee(ctx, params.TenantID, params.CommitteeID)
	if err != nil {
		return nil, err
	}

	// Check if user is already a member
	exists, err := s.db.TenderCommitteeMember.Query().
		Where(
			tendercommitteemember.CommitteeID(params.CommitteeID),
			tendercommitteemember.UserID(params.UserID),
		).
		Exist(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing membership: %w", err)
	}
	if exists {
		return nil, ErrDuplicateMember
	}

	builder := s.db.TenderCommitteeMember.Create().
		SetTenantID(params.TenantID).
		SetCommitteeID(params.CommitteeID).
		SetUserID(params.UserID).
		SetAddedBy(params.AddedBy)

	if params.Role != "" {
		builder.SetRole(params.Role)
	}
	if params.Expertise != "" {
		builder.SetExpertise(params.Expertise)
	}
	if params.Metadata != nil {
		builder.SetMetadata(params.Metadata)
	}

	member, err := builder.Save(ctx)
	if err != nil {
		s.logger.Error("failed to add committee member",
			zap.String("committee_id", params.CommitteeID.String()),
			zap.String("user_id", params.UserID.String()),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to add committee member: %w", err)
	}

	s.logger.Info("committee member added",
		zap.String("member_id", member.ID.String()),
		zap.String("committee_id", params.CommitteeID.String()),
	)

	return member, nil
}

// GetMember retrieves a committee member.
func (s *Service) GetMember(ctx context.Context, tenantID, memberID uuid.UUID) (*ent.TenderCommitteeMember, error) {
	member, err := s.db.TenderCommitteeMember.Query().
		Where(
			tendercommitteemember.ID(memberID),
			tendercommitteemember.TenantID(tenantID),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrMemberNotFound
		}
		return nil, fmt.Errorf("failed to get committee member: %w", err)
	}
	return member, nil
}

// UpdateMember updates a committee member.
func (s *Service) UpdateMember(ctx context.Context, tenantID, memberID uuid.UUID, params MemberUpdateParams) (*ent.TenderCommitteeMember, error) {
	_, err := s.GetMember(ctx, tenantID, memberID)
	if err != nil {
		return nil, err
	}

	builder := s.db.TenderCommitteeMember.UpdateOneID(memberID)

	if params.Role != nil {
		builder.SetRole(*params.Role)
	}
	if params.Expertise != nil {
		builder.SetExpertise(*params.Expertise)
	}
	if params.IsActive != nil {
		builder.SetIsActive(*params.IsActive)
		if !*params.IsActive {
			builder.SetLeftAt(time.Now())
		}
	}
	if params.Metadata != nil {
		builder.SetMetadata(params.Metadata)
	}

	member, err := builder.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to update committee member: %w", err)
	}

	return member, nil
}

// RemoveMember removes a member from a committee.
func (s *Service) RemoveMember(ctx context.Context, tenantID, memberID uuid.UUID) error {
	_, err := s.GetMember(ctx, tenantID, memberID)
	if err != nil {
		return err
	}

	err = s.db.TenderCommitteeMember.DeleteOneID(memberID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to remove committee member: %w", err)
	}

	s.logger.Info("committee member removed",
		zap.String("member_id", memberID.String()),
	)

	return nil
}
