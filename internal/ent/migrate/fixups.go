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

// MoveTaskAssignees copies every tasks.assignee_id into task_assignees as the task's primary
// assignee, then drops the column, so who a task is assigned to has one source. Run it after
// Schema.Create: that creates task_assignees and, without WithDropColumn, leaves assignee_id in
// place for this step to read. One transaction; once the column is gone this does nothing.
func MoveTaskAssignees(ctx context.Context, db *sql.DB) error {
	var present bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'tasks' AND column_name = 'assignee_id')`).Scan(&present); err != nil {
		return fmt.Errorf("inspect tasks.assignee_id: %w", err)
	}
	if !present {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin assignee move: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO task_assignees (id, tenant_id, task_id, user_id, is_primary, created_at)
		SELECT gen_random_uuid(), t.tenant_id, t.id, t.assignee_id, true, now()
		FROM tasks t
		WHERE t.assignee_id IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM task_assignees a WHERE a.task_id = t.id AND a.is_primary)
		ON CONFLICT (task_id, user_id) DO UPDATE SET is_primary = true`); err != nil {
		return fmt.Errorf("copy task assignees: %w", err)
	}
	// Dropping the column also drops its (tenant_id, assignee_id) index.
	if _, err := tx.ExecContext(ctx, `ALTER TABLE tasks DROP COLUMN IF EXISTS assignee_id`); err != nil {
		return fmt.Errorf("drop tasks.assignee_id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit assignee move: %w", err)
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
