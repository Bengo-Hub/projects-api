-- Modify "tasks" table
ALTER TABLE "tasks" ADD COLUMN "start_date" timestamptz NULL, ADD COLUMN "estimated_hours" double precision NULL, ADD COLUMN "progress_pct" bigint NOT NULL DEFAULT 0;
-- Retired: treasury owns budgets and project costs, ERP owns timesheets (empty in production).
DROP TABLE IF EXISTS "time_logs" CASCADE;
DROP TABLE IF EXISTS "expenses" CASCADE;
DROP TABLE IF EXISTS "budgets" CASCADE;
