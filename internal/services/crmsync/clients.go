package crmsync

import (
	"context"
	"fmt"
	"maps"

	"entgo.io/ent/dialect/sql"
	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/predicate"
	"github.com/bengobox/projects-service/internal/ent/projectmember"
	"github.com/bengobox/projects-service/internal/modules/rbac"
)

// Client portal rows (sprint 14) copy a CRM contact too: project_members with role client keep
// crm_contact_id, a contact_name snapshot and a masked email_hint (portal package keys).
const (
	metaClientContactName = "contact_name"
	metaClientEmailHint   = "email_hint"
)

// clientRowsOf matches the client rows naming the contact, in any tenant.
func clientRowsOf(contactID uuid.UUID) predicate.ProjectMember {
	return predicate.ProjectMember(func(s *sql.Selector) {
		s.Where(sql.And(
			sql.EQ(s.C(projectmember.FieldRoleCode), rbac.RoleClient),
			sql.P(func(b *sql.Builder) {
				b.WriteString("lower(").Ident(s.C(projectmember.FieldMetadata)).WriteString(" ->> '" + metaContactID + "') = ").Arg(contactID.String())
			}),
		))
	})
}

// clientsNamed reports whether any client row naming the contact still holds a name or hint.
func (c *ContactEventsConsumer) clientsNamed(ctx context.Context, contactID uuid.UUID) (bool, error) {
	rows, err := c.db.ProjectMember.Query().Where(clientRowsOf(contactID)).All(ctx)
	if err != nil {
		return false, err
	}
	for _, m := range rows {
		if m.Metadata[metaClientContactName] != nil || m.Metadata[metaClientEmailHint] != nil {
			return true, nil
		}
	}
	return false, nil
}

// unlinkClients applies a CRM delete to client rows: an invite nobody accepted yet is removed
// (the person is no longer in the CRM to be invited), and an active client keeps portal access
// but loses the contact id and the copied name and hint. It answers how many rows changed.
func (c *ContactEventsConsumer) unlinkClients(ctx context.Context, contactID uuid.UUID) (int, error) {
	return c.rewriteClients(ctx, contactID, func(tx *ent.Tx, m *ent.ProjectMember) (bool, error) {
		if m.UserID == uuid.Nil {
			return true, tx.ProjectMember.DeleteOne(m).Exec(ctx)
		}
		meta := make(map[string]any, len(m.Metadata))
		maps.Copy(meta, m.Metadata)
		delete(meta, metaContactID)
		delete(meta, metaClientContactName)
		delete(meta, metaClientEmailHint)
		return true, tx.ProjectMember.UpdateOneID(m.ID).SetMetadata(meta).Exec(ctx)
	})
}

// blankClients applies a CRM erasure: the copied name and email hint go, the link stays.
func (c *ContactEventsConsumer) blankClients(ctx context.Context, contactID uuid.UUID) (int, error) {
	return c.rewriteClients(ctx, contactID, func(tx *ent.Tx, m *ent.ProjectMember) (bool, error) {
		if m.Metadata[metaClientContactName] == nil && m.Metadata[metaClientEmailHint] == nil {
			return false, nil
		}
		meta := make(map[string]any, len(m.Metadata))
		maps.Copy(meta, m.Metadata)
		delete(meta, metaClientContactName)
		delete(meta, metaClientEmailHint)
		return true, tx.ProjectMember.UpdateOneID(m.ID).SetMetadata(meta).Exec(ctx)
	})
}

func (c *ContactEventsConsumer) rewriteClients(ctx context.Context, contactID uuid.UUID,
	edit func(tx *ent.Tx, m *ent.ProjectMember) (bool, error)) (int, error) {
	tx, err := c.db.Tx(ctx)
	if err != nil {
		return 0, err
	}
	rows, err := tx.ProjectMember.Query().Where(clientRowsOf(contactID)).All(ctx)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	n := 0
	for _, m := range rows {
		changed, err := edit(tx, m)
		if err != nil {
			_ = tx.Rollback()
			return 0, fmt.Errorf("client row %s: %w", m.ID, err)
		}
		if changed {
			n++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}
