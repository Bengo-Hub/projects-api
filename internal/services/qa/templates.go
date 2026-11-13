package qa

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/predicate"
	"github.com/bengobox/projects-service/internal/ent/qacriterion"
	"github.com/bengobox/projects-service/internal/ent/qatemplate"
	"github.com/bengobox/projects-service/internal/qamodel"
)

// serviceTagRe is the shape of a catalogue service tag ("pos", "hadia", "erp").
var serviceTagRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// TemplateInput creates a template. Platform makes it a platform template (no tenant), which only
// the platform owner may do.
type TemplateInput struct {
	Name       string            `json:"name"`
	Kind       string            `json:"kind"`
	ServiceTag string            `json:"service_tag"`
	Status     string            `json:"status"`
	PassRule   *qamodel.PassRule `json:"pass_rule"`
	Metadata   map[string]any    `json:"metadata"`
	Platform   bool              `json:"platform"`
}

// TemplatePatch changes a template; nil fields stay as they are.
type TemplatePatch struct {
	Name        *string           `json:"name"`
	ServiceTag  *string           `json:"service_tag"`
	Status      *string           `json:"status"`
	PassRule    *qamodel.PassRule `json:"pass_rule"`
	Metadata    map[string]any    `json:"metadata"`
	ClearReview bool              `json:"clear_review"`
}

// TemplateFilter narrows the template list.
type TemplateFilter struct {
	ServiceTag string
	Status     string
	Limit      int
	Offset     int
}

func validStatus(s string) bool {
	return s == qamodel.TemplateDraft || s == qamodel.TemplateActive || s == qamodel.TemplateArchived
}

func normalizeRule(r *qamodel.PassRule) (qamodel.PassRule, error) {
	if r == nil {
		return qamodel.DefaultPassRule(), nil
	}
	n, err := r.Normalize()
	if err != nil {
		return qamodel.PassRule{}, invalid("pass_rule: %v", err)
	}
	return n, nil
}

// CreateTemplate adds a tenant template, or a platform template for the platform owner.
func (s *Service) CreateTemplate(ctx context.Context, actor Actor, in TemplateInput) (*ent.QATemplate, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 200 {
		return nil, invalid("name is required (at most 200 characters)")
	}
	if in.Kind == "" {
		in.Kind = qamodel.KindSoftware
	}
	if !qamodel.ValidKind(in.Kind) {
		return nil, invalid("kind must be software, project or general")
	}
	if in.Status == "" {
		in.Status = qamodel.TemplateDraft
	}
	if !validStatus(in.Status) {
		return nil, invalid("status must be draft, active or archived")
	}
	if in.ServiceTag != "" && !serviceTagRe.MatchString(in.ServiceTag) {
		return nil, invalid("service_tag must be a catalogue service tag")
	}
	if in.Platform && !actor.PlatformOwner {
		return nil, ErrForbidden
	}
	rule, err := normalizeRule(in.PassRule)
	if err != nil {
		return nil, err
	}
	c := s.client.QATemplate.Create().
		SetName(in.Name).SetKind(in.Kind).SetStatus(in.Status).
		SetPassRule(rule).SetServiceTag(in.ServiceTag)
	if !in.Platform {
		c = c.SetTenantID(actor.TenantID)
	}
	if in.Metadata != nil {
		c = c.SetMetadata(in.Metadata)
	}
	if actor.UserID != uuid.Nil {
		c = c.SetCreatedBy(actor.UserID)
	}
	return c.Save(ctx)
}

// readable: the tenant's own templates, and platform templates (only active ones unless the
// caller is the platform owner).
func readable(actor Actor) predicate.QATemplate {
	if actor.PlatformOwner {
		return qatemplate.Or(qatemplate.TenantID(actor.TenantID), qatemplate.TenantIDIsNil())
	}
	return qatemplate.Or(
		qatemplate.TenantID(actor.TenantID),
		qatemplate.And(qatemplate.TenantIDIsNil(), qatemplate.Status(qamodel.TemplateActive)),
	)
}

// ListTemplates lists the templates the actor can use, newest first.
func (s *Service) ListTemplates(ctx context.Context, actor Actor, f TemplateFilter) ([]*ent.QATemplate, int, error) {
	q := s.client.QATemplate.Query().Where(readable(actor))
	if f.ServiceTag != "" {
		q = q.Where(qatemplate.ServiceTag(f.ServiceTag))
	}
	if f.Status != "" {
		q = q.Where(qatemplate.Status(f.Status))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count templates: %w", err)
	}
	if f.Limit <= 0 {
		f.Limit = 20
	}
	items, err := q.Order(ent.Desc(qatemplate.FieldCreatedAt)).Offset(f.Offset).Limit(f.Limit).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list templates: %w", err)
	}
	return items, total, nil
}

// GetTemplate returns a template the actor can read, with its criteria in order.
func (s *Service) GetTemplate(ctx context.Context, actor Actor, id uuid.UUID) (*ent.QATemplate, error) {
	t, err := s.client.QATemplate.Query().
		Where(qatemplate.ID(id), readable(actor)).
		WithCriteria(func(q *ent.QACriterionQuery) {
			q.Order(ent.Asc(qacriterion.FieldSortOrder), ent.Asc(qacriterion.FieldCreatedAt))
		}).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	return t, err
}

// editable loads a template the actor may change: its own tenant's, or a platform template for
// the platform owner.
func (s *Service) editable(ctx context.Context, actor Actor, id uuid.UUID) (*ent.QATemplate, error) {
	t, err := s.client.QATemplate.Get(ctx, id)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	switch {
	case t.TenantID == nil && actor.PlatformOwner:
		return t, nil
	case t.TenantID == nil:
		return nil, ErrForbidden
	case *t.TenantID == actor.TenantID:
		return t, nil
	default:
		return nil, ErrNotFound
	}
}

// UpdateTemplate changes a template's name, tag, status, pass rule or metadata, or clears its
// review flag once the owner has checked the criteria against the changed catalogue.
func (s *Service) UpdateTemplate(ctx context.Context, actor Actor, id uuid.UUID, p TemplatePatch) (*ent.QATemplate, error) {
	if _, err := s.editable(ctx, actor, id); err != nil {
		return nil, err
	}
	u := s.client.QATemplate.UpdateOneID(id)
	if p.Name != nil {
		n := strings.TrimSpace(*p.Name)
		if n == "" || len(n) > 200 {
			return nil, invalid("name is required (at most 200 characters)")
		}
		u = u.SetName(n)
	}
	if p.ServiceTag != nil {
		if *p.ServiceTag != "" && !serviceTagRe.MatchString(*p.ServiceTag) {
			return nil, invalid("service_tag must be a catalogue service tag")
		}
		u = u.SetServiceTag(*p.ServiceTag)
	}
	if p.Status != nil {
		if !validStatus(*p.Status) {
			return nil, invalid("status must be draft, active or archived")
		}
		u = u.SetStatus(*p.Status)
	}
	if p.PassRule != nil {
		rule, err := normalizeRule(p.PassRule)
		if err != nil {
			return nil, err
		}
		u = u.SetPassRule(rule).AddVersion(1)
	}
	if p.Metadata != nil {
		u = u.SetMetadata(p.Metadata)
	}
	if p.ClearReview {
		u = u.SetNeedsReview(false)
	}
	return u.Save(ctx)
}

// MarkFeatureChanged flags every template with a criterion on the feature code for review. It is
// idempotent: a repeated event sets the same flag again.
func (s *Service) MarkFeatureChanged(ctx context.Context, featureCode string) (int, error) {
	if featureCode == "" {
		return 0, nil
	}
	rows, err := s.client.QACriterion.Query().
		Where(qacriterion.SubjectKind(qamodel.SubjectFeature), qacriterion.SubjectRef(featureCode)).
		Select(qacriterion.FieldTemplateID).All(ctx)
	if err != nil {
		return 0, fmt.Errorf("find templates for feature: %w", err)
	}
	if len(rows) == 0 {
		return 0, nil
	}
	tids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		tids = append(tids, r.TemplateID)
	}
	return s.client.QATemplate.Update().Where(qatemplate.IDIn(tids...)).SetNeedsReview(true).Save(ctx)
}
