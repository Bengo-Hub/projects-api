package qa

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/milestone"
	"github.com/bengobox/projects-service/internal/ent/qacriterion"
	"github.com/bengobox/projects-service/internal/ent/task"
	"github.com/bengobox/projects-service/internal/platform/catalog"
	"github.com/bengobox/projects-service/internal/qamodel"
)

// maxCriteriaPerCall bounds one import.
const maxCriteriaPerCall = 500

// CriterionInput is one criterion to add or replace.
type CriterionInput struct {
	SubjectKind    string `json:"subject_kind"`
	SubjectRef     string `json:"subject_ref"`
	AreaCode       string `json:"area_code"`
	Title          string `json:"title"`
	RequirementRef string `json:"requirement_ref"`
	Preconditions  string `json:"preconditions"`
	Steps          string `json:"steps"`
	ExpectedResult string `json:"expected_result"`
	Weight         string `json:"weight"`
	AutomatedRef   string `json:"automated_ref"`
	SortOrder      *int   `json:"sort_order"`
}

func fieldLen(name, v string, max int) error {
	if len(v) > max {
		return invalid("%s is longer than %d characters", name, max)
	}
	return nil
}

// checkCriterion validates one input against the template it goes on. Milestones and tasks must be
// the template tenant's own; a platform template may only use features or free criteria. Feature
// codes are checked against the catalogue when it can be read.
func (s *Service) checkCriterion(ctx context.Context, t *ent.QATemplate, in *CriterionInput, known map[string]bool) error {
	in.Title = strings.TrimSpace(in.Title)
	in.SubjectRef = strings.TrimSpace(in.SubjectRef)
	if in.SubjectKind == "" {
		in.SubjectKind = qamodel.SubjectNone
	}
	if in.Weight == "" {
		in.Weight = qamodel.WeightMust
	}
	switch {
	case in.Title == "":
		return invalid("every criterion needs a title")
	case !qamodel.ValidSubject(in.SubjectKind):
		return invalid("subject_kind must be feature, milestone, task or none")
	case !qamodel.ValidWeight(in.Weight):
		return invalid("weight must be must, should or could")
	}
	for _, c := range []struct {
		name, v string
		max     int
	}{
		{"title", in.Title, 300}, {"subject_ref", in.SubjectRef, 200}, {"area_code", in.AreaCode, 32},
		{"requirement_ref", in.RequirementRef, 100}, {"automated_ref", in.AutomatedRef, 300},
		{"preconditions", in.Preconditions, 10000}, {"steps", in.Steps, 10000}, {"expected_result", in.ExpectedResult, 10000},
	} {
		if err := fieldLen(c.name, c.v, c.max); err != nil {
			return err
		}
	}
	switch in.SubjectKind {
	case qamodel.SubjectNone:
		in.SubjectRef = ""
	case qamodel.SubjectFeature:
		if in.SubjectRef == "" {
			return invalid("a feature criterion needs the feature code in subject_ref")
		}
		if known != nil && !known[in.SubjectRef] {
			return invalid("feature %q is not in the catalogue", in.SubjectRef)
		}
	case qamodel.SubjectMilestone, qamodel.SubjectTask:
		if t.TenantID == nil {
			return invalid("a platform template can only check features or free criteria")
		}
		id, err := uuid.Parse(in.SubjectRef)
		if err != nil {
			return invalid("subject_ref must be the %s id", in.SubjectKind)
		}
		var ok bool
		if in.SubjectKind == qamodel.SubjectMilestone {
			ok, err = s.client.Milestone.Query().Where(milestone.ID(id), milestone.TenantID(*t.TenantID)).Exist(ctx)
		} else {
			ok, err = s.client.Task.Query().Where(task.ID(id), task.TenantID(*t.TenantID)).Exist(ctx)
		}
		if err != nil {
			return err
		}
		if !ok {
			return invalid("%s %s was not found", in.SubjectKind, id)
		}
		in.SubjectRef = id.String()
	}
	return nil
}

// knownFeatures reads the catalogue codes once per call; nil means "cannot check" (the catalogue
// read is not live yet), and codes are then kept unchecked.
func (s *Service) knownFeatures(ctx context.Context, t *ent.QATemplate, in []CriterionInput) map[string]bool {
	need := false
	for _, c := range in {
		need = need || c.SubjectKind == qamodel.SubjectFeature
	}
	if !need {
		return nil
	}
	tenant := uuid.Nil
	if t.TenantID != nil {
		tenant = *t.TenantID
	}
	feats, err := s.catalog.Features(ctx, tenant, t.ServiceTag)
	if err != nil {
		if !errors.Is(err, catalog.ErrUnavailable) {
			s.log.Warn("feature catalogue not read; feature codes kept unchecked", zap.Error(err))
		}
		return nil
	}
	known := make(map[string]bool, len(feats))
	for _, f := range feats {
		known[f.Code] = true
	}
	return known
}

func setCriterion(c *ent.QACriterionCreate, in CriterionInput, order int) *ent.QACriterionCreate {
	return c.SetSubjectKind(in.SubjectKind).SetSubjectRef(in.SubjectRef).SetAreaCode(in.AreaCode).
		SetTitle(in.Title).SetRequirementRef(in.RequirementRef).SetPreconditions(in.Preconditions).
		SetSteps(in.Steps).SetExpectedResult(in.ExpectedResult).SetWeight(in.Weight).
		SetAutomatedRef(in.AutomatedRef).SetSortOrder(order)
}

// AddCriteria appends criteria to a template in one transaction (also the import path) and bumps
// its version. A criterion without sort_order goes after the last one.
func (s *Service) AddCriteria(ctx context.Context, actor Actor, templateID uuid.UUID, in []CriterionInput) ([]*ent.QACriterion, error) {
	if len(in) == 0 || len(in) > maxCriteriaPerCall {
		return nil, invalid("send between 1 and %d criteria", maxCriteriaPerCall)
	}
	t, err := s.editable(ctx, actor, templateID)
	if err != nil {
		return nil, err
	}
	known := s.knownFeatures(ctx, t, in)
	for i := range in {
		if err := s.checkCriterion(ctx, t, &in[i], known); err != nil {
			return nil, fmt.Errorf("criterion %d: %w", i+1, err)
		}
	}
	next, err := s.client.QACriterion.Query().Where(qacriterion.TemplateID(templateID)).Count(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	builders := make([]*ent.QACriterionCreate, len(in))
	for i, c := range in {
		order := (next + i + 1) * 10
		if c.SortOrder != nil {
			order = *c.SortOrder
		}
		b := setCriterion(tx.QACriterion.Create().SetTemplateID(templateID), c, order)
		if t.TenantID != nil {
			b = b.SetTenantID(*t.TenantID)
		}
		builders[i] = b
	}
	out, err := tx.QACriterion.CreateBulk(builders...).Save(ctx)
	if err != nil {
		rollback(tx)
		return nil, fmt.Errorf("add criteria: %w", err)
	}
	if err := tx.QATemplate.UpdateOneID(templateID).AddVersion(1).Exec(ctx); err != nil {
		rollback(tx)
		return nil, err
	}
	return out, tx.Commit()
}

// UpdateCriterion replaces one criterion's content and bumps the template version.
func (s *Service) UpdateCriterion(ctx context.Context, actor Actor, templateID, criterionID uuid.UUID, in CriterionInput) (*ent.QACriterion, error) {
	t, err := s.editable(ctx, actor, templateID)
	if err != nil {
		return nil, err
	}
	cur, err := s.client.QACriterion.Query().Where(qacriterion.ID(criterionID), qacriterion.TemplateID(templateID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := s.checkCriterion(ctx, t, &in, s.knownFeatures(ctx, t, []CriterionInput{in})); err != nil {
		return nil, err
	}
	order := cur.SortOrder
	if in.SortOrder != nil {
		order = *in.SortOrder
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	c, err := tx.QACriterion.UpdateOneID(criterionID).
		SetSubjectKind(in.SubjectKind).SetSubjectRef(in.SubjectRef).SetAreaCode(in.AreaCode).
		SetTitle(in.Title).SetRequirementRef(in.RequirementRef).SetPreconditions(in.Preconditions).
		SetSteps(in.Steps).SetExpectedResult(in.ExpectedResult).SetWeight(in.Weight).
		SetAutomatedRef(in.AutomatedRef).SetSortOrder(order).Save(ctx)
	if err != nil {
		rollback(tx)
		return nil, err
	}
	if err := tx.QATemplate.UpdateOneID(templateID).AddVersion(1).Exec(ctx); err != nil {
		rollback(tx)
		return nil, err
	}
	return c, tx.Commit()
}

// DeleteCriterion removes a criterion from the template. Sessions already created keep it in
// their snapshot.
func (s *Service) DeleteCriterion(ctx context.Context, actor Actor, templateID, criterionID uuid.UUID) error {
	if _, err := s.editable(ctx, actor, templateID); err != nil {
		return err
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return err
	}
	n, err := tx.QACriterion.Delete().Where(qacriterion.ID(criterionID), qacriterion.TemplateID(templateID)).Exec(ctx)
	if err != nil {
		rollback(tx)
		return err
	}
	if n == 0 {
		rollback(tx)
		return ErrNotFound
	}
	if err := tx.QATemplate.UpdateOneID(templateID).AddVersion(1).Exec(ctx); err != nil {
		rollback(tx)
		return err
	}
	return tx.Commit()
}
