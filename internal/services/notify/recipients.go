package notify

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	entmember "github.com/bengobox/projects-service/internal/ent/projectmember"
	"github.com/bengobox/projects-service/internal/modules/rbac"
)

// Client portal notices (sprint 14).
const (
	EventClientInvited             = "client.invited"
	EventMilestoneReadyForReview   = "milestone.ready_for_review"
	EventMilestoneApproved         = "milestone.approved"
	EventMilestoneChangesRequested = "milestone.changes_requested"
)

// leadRoles are the project member roles told about a client's decision, beside the owner.
var leadRoles = []string{"owner", "manager"}

// ProjectClients returns the auth user ids of the project's signed-in clients: active client
// rows that have been matched to an account. A client still at the invite stage has no account
// yet and is reached by the invite alone.
func ProjectClients(ctx context.Context, tx *ent.Tx, tenantID, projectID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.ProjectMember.Query().
		Where(entmember.TenantID(tenantID), entmember.ProjectID(projectID), entmember.RoleCode(rbac.RoleClient),
			entmember.LeftAtIsNil(), entmember.UserIDNEQ(uuid.Nil)).
		Select(entmember.FieldUserID).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load project clients: %w", err)
	}
	out := make([]uuid.UUID, len(rows))
	for i, m := range rows {
		out[i] = m.UserID
	}
	return out, nil
}

// ProjectLeads returns the project's owner and its active owner and manager members: who hears
// about a client's milestone decision.
func ProjectLeads(ctx context.Context, tx *ent.Tx, tenantID, projectID uuid.UUID) ([]uuid.UUID, error) {
	p, err := tx.Project.Query().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).
		Select(entproject.FieldOwnerID).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("load project owner: %w", err)
	}
	rows, err := tx.ProjectMember.Query().
		Where(entmember.TenantID(tenantID), entmember.ProjectID(projectID), entmember.RoleCodeIn(leadRoles...),
			entmember.LeftAtIsNil()).
		Select(entmember.FieldUserID).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load project leads: %w", err)
	}
	out := []uuid.UUID{p.OwnerID}
	for _, m := range rows {
		out = append(out, m.UserID)
	}
	return out, nil
}

// PortalPath builds a client portal link: /{org}/portal, plus /projects/{id} for a project and
// ?milestone={id} for a milestone, the org taken from ctx.
func PortalPath(ctx context.Context, projectID, milestoneID uuid.UUID) string {
	p := "/portal"
	if org := OrgSlug(ctx); org != "" {
		p = "/" + org + p
	}
	if projectID != uuid.Nil {
		p += "/projects/" + projectID.String()
	}
	if milestoneID != uuid.Nil {
		p += "?milestone=" + milestoneID.String()
	}
	return p
}

// MilestonePath is the team's link to a milestone: /{org}/projects/{id}/milestones?milestone={id}.
func MilestonePath(ctx context.Context, projectID, milestoneID uuid.UUID) string {
	p := "/projects/" + projectID.String() + "/milestones?milestone=" + milestoneID.String()
	if org := OrgSlug(ctx); org != "" {
		p = "/" + org + p
	}
	return p
}
