// Package sequence hands out per-tenant document numbers. projects-api had no sequence helper, so
// this is a counter row per (tenant, kind) advanced by one upsert inside the caller's transaction:
// the row lock is held until that transaction ends, so two concurrent allocations for the same
// tenant queue on it and never share a number, and a rolled back transaction gives its number
// back with it. A deleted document never frees its number (unlike the old COUNT+1).
package sequence

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
)

// KindTender numbers tenders.
const KindTender = "tender"

// Next returns the next number of kind for the tenant, starting at 1.
func Next(ctx context.Context, tx *ent.Tx, tenantID uuid.UUID, kind string) (int64, error) {
	rows, err := tx.QueryContext(ctx, `
		INSERT INTO document_sequences (id, tenant_id, kind, last_value, updated_at)
		VALUES ($1, $2, $3, 1, now())
		ON CONFLICT (tenant_id, kind) DO UPDATE
			SET last_value = document_sequences.last_value + 1, updated_at = now()
		RETURNING last_value`, uuid.New(), tenantID, kind)
	if err != nil {
		return 0, fmt.Errorf("next %s number: %w", kind, err)
	}
	defer rows.Close()
	var v int64
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, fmt.Errorf("next %s number: %w", kind, err)
		}
		return 0, fmt.Errorf("next %s number: no row returned", kind)
	}
	if err := rows.Scan(&v); err != nil {
		return 0, fmt.Errorf("next %s number: %w", kind, err)
	}
	return v, rows.Err()
}
