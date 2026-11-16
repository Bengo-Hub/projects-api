package migrate

import (
	"context"
	"database/sql"
	"fmt"
)

// Schema.Create with the versioned dir diffs the live schema against ent; it never runs the .sql
// files and never drops tables or indexes on its own. The data moves and drops it cannot do live
// here, called by cmd/migrate under the migration advisory lock. Every step is idempotent.

// legacyRBACTables are the first RBAC tables: roles keyed by a text code with a JSON permission
// list, permissions keyed per tenant, and user_roles. Nothing ever wrote to them (no route
// assigned a role and the seed only logged), so they are dropped and ent creates the reshaped
// roles, permissions, role_permissions and user_role_assignments tables.
var legacyRBACTables = []string{"user_roles", "role_permissions", "permissions", "roles"}

// ReshapeRBACTables drops the legacy RBAC tables while roles still has its old "code" primary key.
// Run it before Schema.Create. Once the reshaped roles table exists (it has no "code" column) this
// does nothing, so the new tables and their rows are never touched again.
func ReshapeRBACTables(ctx context.Context, db *sql.DB) error {
	var legacy bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'roles' AND column_name = 'code')`).Scan(&legacy); err != nil {
		return fmt.Errorf("inspect roles table: %w", err)
	}
	if !legacy {
		return nil
	}
	for _, t := range legacyRBACTables {
		if _, err := db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %q CASCADE", t)); err != nil {
			return fmt.Errorf("drop legacy rbac table %s: %w", t, err)
		}
	}
	return nil
}

// legacyIndexes were removed from the ent schema. tenders_number_key made tender numbers unique
// across all tenants, so a second tenant's first tender (TND-2026-0001) could not be saved; the
// (tenant_id, number) unique index replaces it.
var legacyIndexes = []string{"tenders_number_key"}

// DropLegacyIndexes drops indexes that no longer exist in the ent schema. Run it after
// Schema.Create, so the replacement index exists before the old one goes.
func DropLegacyIndexes(ctx context.Context, db *sql.DB) error {
	for _, name := range legacyIndexes {
		if _, err := db.ExecContext(ctx, fmt.Sprintf("DROP INDEX IF EXISTS %q", name)); err != nil {
			return fmt.Errorf("drop legacy index %s: %w", name, err)
		}
	}
	return nil
}

// BackfillTenderSequences starts each tenant's tender counter at the highest number it already
// used (the digits after the last dash of TND-{year}-{seq}), so the counter never hands out a
// number a tender holds. A tenant that already has a counter row keeps it. Run it after
// Schema.Create.
func BackfillTenderSequences(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO document_sequences (id, tenant_id, kind, last_value, updated_at)
		SELECT gen_random_uuid(), tenant_id, 'tender', MAX(CAST(substring(number FROM '([0-9]+)$') AS bigint)), now()
		FROM tenders
		WHERE number ~ '[0-9]+$'
		GROUP BY tenant_id
		ON CONFLICT (tenant_id, kind) DO NOTHING`)
	if err != nil {
		return fmt.Errorf("backfill tender sequences: %w", err)
	}
	return nil
}
