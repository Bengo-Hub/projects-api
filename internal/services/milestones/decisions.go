package milestones

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/people"
)

// Decision is a client's latest verdict on a milestone, read back from the activity entry Decide
// writes. The team's milestone routes and the portal's both return it as last_decision.
type Decision struct {
	// Decision is the outcome: approved or changes_requested.
	Decision      string `json:"decision"`
	Comment       string `json:"comment"`
	DecidedByName string `json:"decided_by_name"`
	DecidedAt     string `json:"decided_at"`
}

// Reviewed is a milestone with the client's latest decision on it (nil when no client decided).
type Reviewed struct {
	*ent.Milestone
	LastDecision *Decision `json:"last_decision"`
}

// decisionOutcome turns the decision a client sent (approve, request_changes) into the status it
// set, the word every last_decision carries.
func decisionOutcome(sent string) string {
	switch sent {
	case DecisionApprove:
		return StatusApproved
	case DecisionRequestChanges:
		return StatusChangesRequested
	}
	return sent
}

// latestDecisionsSQL keeps the newest client decision per milestone of one project. The project's
// rows come from the (tenant_id, project_id, occurred_at) index; DISTINCT ON picks one per
// milestone however many decisions the project collected. $4 narrows it to some milestones (NULL
// means every milestone of the project).
const latestDecisionsSQL = `
SELECT DISTINCT ON (payload->>'entity_id')
       payload->>'entity_id', user_id,
       COALESCE(payload->'changes'->>'` + ChangeClientDecision + `', ''),
       COALESCE(payload->'changes'->>'comment', ''),
       COALESCE(payload->'changes'->>'decided_by', ''),
       COALESCE(payload->'changes'->>'decided_at', '')
  FROM activities
 WHERE tenant_id = $1 AND project_id = $2 AND activity_type = $3
   AND payload->'changes'->>'` + ChangeClientDecision + `' IS NOT NULL
   AND ($4::text[] IS NULL OR payload->>'entity_id' = ANY($4::text[]))
 ORDER BY payload->>'entity_id', occurred_at DESC`

// LatestDecisions returns the newest client decision of each milestone of the project, keyed by
// milestone id, in one query for the whole list plus one batched name lookup. ids narrows it to
// those milestones; none means every milestone of the project.
func LatestDecisions(ctx context.Context, client *ent.Client, tenantID, projectID uuid.UUID, ids ...uuid.UUID) (map[uuid.UUID]*Decision, error) {
	var only []string
	if len(ids) > 0 {
		only = make([]string, len(ids))
		for i, id := range ids {
			only[i] = id.String()
		}
	}
	rows, err := client.QueryContext(ctx, latestDecisionsSQL, tenantID, projectID, "milestone."+activity.ActionUpdated, only)
	if err != nil {
		return nil, fmt.Errorf("load decisions: %w", err)
	}
	defer rows.Close()
	type row struct {
		milestone, decider uuid.UUID
		d                  Decision
	}
	var found []row
	var deciders []uuid.UUID
	for rows.Next() {
		var entity, decision, comment, decidedBy, decidedAt string
		var actor uuid.UUID
		if err := rows.Scan(&entity, &actor, &decision, &comment, &decidedBy, &decidedAt); err != nil {
			return nil, fmt.Errorf("read decision: %w", err)
		}
		id, err := uuid.Parse(entity)
		if err != nil {
			continue
		}
		// decided_by is the client Decide was called for; the entry's actor is the fallback.
		decider := actor
		if v, err := uuid.Parse(decidedBy); err == nil {
			decider = v
		}
		found = append(found, row{milestone: id, decider: decider, d: Decision{
			Decision: decisionOutcome(decision), Comment: comment, DecidedAt: decidedAt,
		}})
		deciders = append(deciders, decider)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load decisions: %w", err)
	}
	who, err := people.Lookup(ctx, client.ProjectsUser, tenantID, deciders)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]*Decision, len(found))
	for _, f := range found {
		d := f.d
		d.DecidedByName = who[f.decider].Label()
		out[f.milestone] = &d
	}
	return out, nil
}

// ListReviewed is ListMilestones with each milestone's latest client decision.
func (s *Service) ListReviewed(ctx context.Context, tenantID, projectID uuid.UUID) ([]Reviewed, error) {
	items, err := s.ListMilestones(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	decisions, err := LatestDecisions(ctx, s.client, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]Reviewed, len(items))
	for i, m := range items {
		out[i] = Reviewed{Milestone: m, LastDecision: decisions[m.ID]}
	}
	return out, nil
}

// GetReviewed is GetMilestone with the milestone's latest client decision.
func (s *Service) GetReviewed(ctx context.Context, tenantID, projectID, id uuid.UUID) (Reviewed, error) {
	m, err := s.GetMilestone(ctx, tenantID, projectID, id)
	if err != nil {
		return Reviewed{}, err
	}
	decisions, err := LatestDecisions(ctx, s.client, tenantID, projectID, id)
	if err != nil {
		return Reviewed{}, err
	}
	return Reviewed{Milestone: m, LastDecision: decisions[id]}, nil
}
