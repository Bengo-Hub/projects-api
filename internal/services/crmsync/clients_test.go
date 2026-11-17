package crmsync

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/modules/rbac"
	"github.com/bengobox/projects-service/internal/platform/marketflow"
	"github.com/bengobox/projects-service/internal/testutil"
)

func mkClient(t *testing.T, db *ent.Client, tenant, project, user, contact uuid.UUID) *ent.ProjectMember {
	t.Helper()
	return db.ProjectMember.Create().SetTenantID(tenant).SetProjectID(project).SetUserID(user).SetRoleCode(rbac.RoleClient).
		SetMetadata(map[string]any{"crm_contact_id": contact.String(), "contact_name": "Jane Doe", "email_hint": "j***@x.test", "email_hash": "h"}).
		SaveX(context.Background())
}

// A CRM erasure blanks the portal client's copied name and hint; a CRM delete drops an invite
// nobody accepted and unlinks an active client, who keeps portal access. The crm-links check
// counts client rows.
func TestPortalClientsFollowTheCRM(t *testing.T) {
	db := testutil.Client(t)
	ctx := context.Background()
	tenant, contact, user := uuid.New(), uuid.New(), uuid.New()
	p := mkProject(t, db, tenant, nil)
	invited := mkClient(t, db, tenant, p.ID, uuid.Nil, contact)
	active := mkClient(t, db, tenant, p.ID, user, contact)
	c := NewContactEventsConsumer(db, &fakeCRM{contacts: map[uuid.UUID]marketflow.Contact{contact: {ID: contact, FirstName: "Erased"}}}, zap.NewNop())

	if n, err := c.ContactUpdated(ctx, tenant, contact); err != nil || n != 2 {
		t.Fatalf("erasure: %d %v", n, err)
	}
	for _, id := range []uuid.UUID{invited.ID, active.ID} {
		m := db.ProjectMember.GetX(ctx, id)
		if m.Metadata["contact_name"] != nil || m.Metadata["email_hint"] != nil || m.Metadata["crm_contact_id"] != contact.String() {
			t.Errorf("after erasure: %v", m.Metadata)
		}
	}
	if _, err := c.ContactDeleted(ctx, contact); err != nil {
		t.Fatal(err)
	}
	if exists := db.ProjectMember.Query().CountX(ctx); exists != 1 {
		t.Fatalf("rows after delete = %d, want the active client only", exists)
	}
	m := db.ProjectMember.GetX(ctx, active.ID)
	if m.Metadata["crm_contact_id"] != nil || m.UserID != user {
		t.Errorf("active client after delete: %+v", m)
	}
}
