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

// Client thread notices: a client's post goes to the project's leads, a teammate's to the
// project's signed-in clients.
const (
	EventClientThreadPosted  = "client_thread.posted"
	EventClientThreadReplied = "client_thread.replied"
)

// Client member metadata keys shared with the portal package (portal.MetaCRMContactID and
// portal.MetaContactName are these), so a notice can read an invited client's CRM contact.
const (
	MetaCRMContactID = "crm_contact_id"
	MetaContactName  = "contact_name"
)

// leadRoles are the project member roles told about a client's decision, beside the owner.
var leadRoles = []string{"owner", "manager"}

// Invited is a client still at the invite stage: no account yet, so a notice reaches them through
// their CRM contact.
type Invited struct {
	MemberID  uuid.UUID
	ContactID uuid.UUID
	Name      string // the name snapshot taken at invite time
}

// InvitedClients returns the project's clients still at the invite stage (live client rows with no
// user yet). A row without a readable contact id is left out. members is client.ProjectMember or
// tx.ProjectMember.
func InvitedClients(ctx context.Context, members *ent.ProjectMemberClient, tenantID, projectID uuid.UUID) ([]Invited, error) {
	rows, err := members.Query().
		Where(entmember.TenantID(tenantID), entmember.ProjectID(projectID), entmember.RoleCode(rbac.RoleClient),
			entmember.LeftAtIsNil(), entmember.UserID(uuid.Nil)).
		Select(entmember.FieldID, entmember.FieldMetadata).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load invited clients: %w", err)
	}
	out := make([]Invited, 0, len(rows))
	for _, m := range rows {
		raw, _ := m.Metadata[MetaCRMContactID].(string)
		contact, err := uuid.Parse(raw)
		if err != nil || contact == uuid.Nil {
			continue
		}
		name, _ := m.Metadata[MetaContactName].(string)
		out = append(out, Invited{MemberID: m.ID, ContactID: contact, Name: name})
	}
	return out, nil
}

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

// orgPath prefixes p with /{org} when ctx carries the slug.
func orgPath(ctx context.Context, p string) string {
	if org := OrgSlug(ctx); org != "" {
		return "/" + org + p
	}
	return p
}

// ClientThreadPath is the team's client thread of a project: /{org}/projects/{id}/client-thread.
func ClientThreadPath(ctx context.Context, projectID uuid.UUID) string {
	return orgPath(ctx, "/projects/"+projectID.String()+"/client-thread")
}

// PortalThreadPath is the client thread in the portal, the project page's Messages tab:
// /{org}/portal/projects/{id}?tab=messages.
func PortalThreadPath(ctx context.Context, projectID uuid.UUID) string {
	return PortalPath(ctx, projectID, uuid.Nil) + "?tab=messages"
}

// ChangeRequestPath is the team's link to a change request:
// /{org}/projects/{id}/change-requests?request={id}.
func ChangeRequestPath(ctx context.Context, projectID, requestID uuid.UUID) string {
	return orgPath(ctx, "/projects/"+projectID.String()+"/change-requests?request="+requestID.String())
}

// ProjectLeadsNotClients is ProjectLeads without anyone who is also a client of the project, so a
// notice meant for the team never reaches a client.
func ProjectLeadsNotClients(ctx context.Context, tx *ent.Tx, tenantID, projectID uuid.UUID) ([]uuid.UUID, error) {
	leads, err := ProjectLeads(ctx, tx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	clients, err := ProjectClients(ctx, tx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	isClient := make(map[uuid.UUID]bool, len(clients))
	for _, c := range clients {
		isClient[c] = true
	}
	out := make([]uuid.UUID, 0, len(leads))
	for _, id := range leads {
		if !isClient[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

// MilestonePath is the team's link to a milestone: /{org}/projects/{id}/milestones?milestone={id}.
func MilestonePath(ctx context.Context, projectID, milestoneID uuid.UUID) string {
	p := "/projects/" + projectID.String() + "/milestones?milestone=" + milestoneID.String()
	if org := OrgSlug(ctx); org != "" {
		p = "/" + org + p
	}
	return p
}
