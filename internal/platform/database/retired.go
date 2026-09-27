package database

import (
	"context"
	"database/sql"
	"fmt"
)

// retiredTables were projects-api's own budget, expense and time-log tables. Treasury owns
// budgets and project costs (projects-api reads them over S2S) and ERP owns timesheets, so the
// tables were removed from the schema. They never held production rows (checked 2026-09-28).
// ent's live migration never drops tables, so they are dropped here, before the diff runs.
var retiredTables = []string{"time_logs", "expenses", "budgets"}

// DropRetiredTables removes tables this service no longer owns. Idempotent (IF EXISTS), so it
// is safe on every start and on a fresh database.
func DropRetiredTables(ctx context.Context, db *sql.DB) error {
	for _, t := range retiredTables {
		if _, err := db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %q CASCADE", t)); err != nil {
			return fmt.Errorf("drop retired table %s: %w", t, err)
		}
	}
	return nil
}
