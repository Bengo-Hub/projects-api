-- Modify "tasks" table
ALTER TABLE "tasks" ADD COLUMN "parent_id" uuid NULL, ADD COLUMN "wbs_code" character varying NULL;
-- Create "budgets" table
CREATE TABLE "budgets" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "total_amount" double precision NOT NULL DEFAULT 0, "spent_amount" double precision NOT NULL DEFAULT 0, "currency" character varying NOT NULL DEFAULT 'KES', "status" character varying NOT NULL DEFAULT 'draft', "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "project_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "budgets_projects_project_budget" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create "expenses" table
CREATE TABLE "expenses" ("id" uuid NOT NULL, "project_id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "description" character varying NOT NULL, "amount" double precision NOT NULL, "currency" character varying NOT NULL DEFAULT 'KES', "category" character varying NULL, "incurred_by" uuid NOT NULL, "incurred_at" timestamptz NOT NULL, "receipt_url" character varying NULL, "status" character varying NOT NULL DEFAULT 'pending', "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "budget_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "expenses_budgets_expenses" FOREIGN KEY ("budget_id") REFERENCES "budgets" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create "tenders" table
CREATE TABLE "tenders" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "number" character varying NOT NULL, "title" character varying NOT NULL, "client_name" character varying NOT NULL, "source" character varying NULL, "status" character varying NOT NULL DEFAULT 'draft', "priority" character varying NOT NULL DEFAULT 'medium', "estimated_value" double precision NULL, "currency" character varying NOT NULL DEFAULT 'KES', "deadline" timestamptz NULL, "description" text NULL, "submission_type" character varying NOT NULL DEFAULT 'physical', "submitted_at" timestamptz NULL, "metadata" jsonb NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "created_by" uuid NOT NULL, PRIMARY KEY ("id"));
-- Create index "tender_tenant_id_deadline" to table: "tenders"
CREATE INDEX "tender_tenant_id_deadline" ON "tenders" ("tenant_id", "deadline");
-- Create index "tender_tenant_id_status" to table: "tenders"
CREATE INDEX "tender_tenant_id_status" ON "tenders" ("tenant_id", "status");
-- Create index "tenders_number_key" to table: "tenders"
CREATE UNIQUE INDEX "tenders_number_key" ON "tenders" ("number");
-- Create "tender_committees" table
CREATE TABLE "tender_committees" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "name" character varying NOT NULL, "created_at" timestamptz NOT NULL, "tender_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "tender_committees_tenders_committees" FOREIGN KEY ("tender_id") REFERENCES "tenders" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create "tender_committee_members" table
CREATE TABLE "tender_committee_members" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "user_id" uuid NOT NULL, "role" character varying NOT NULL DEFAULT 'member', "joined_at" timestamptz NOT NULL, "committee_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "tender_committee_members_tender_committees_members" FOREIGN KEY ("committee_id") REFERENCES "tender_committees" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create "tender_documents" table
CREATE TABLE "tender_documents" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "file_url" character varying NOT NULL, "file_name" character varying NOT NULL, "file_size" bigint NULL, "mime_type" character varying NULL, "uploaded_by" uuid NOT NULL, "uploaded_at" timestamptz NOT NULL, "tender_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "tender_documents_tenders_documents" FOREIGN KEY ("tender_id") REFERENCES "tenders" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create "tender_evaluations" table
CREATE TABLE "tender_evaluations" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "evaluator_id" uuid NOT NULL, "score" double precision NOT NULL, "notes" text NULL, "criteria" character varying NULL, "evaluated_at" timestamptz NOT NULL, "tender_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "tender_evaluations_tenders_evaluations" FOREIGN KEY ("tender_id") REFERENCES "tenders" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create "tender_meetings" table
CREATE TABLE "tender_meetings" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "title" character varying NOT NULL, "scheduled_at" timestamptz NOT NULL, "platform" character varying NOT NULL DEFAULT 'physical', "meeting_url" character varying NULL, "notes" text NULL, "created_by" uuid NOT NULL, "created_at" timestamptz NOT NULL, "tender_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "tender_meetings_tenders_meetings" FOREIGN KEY ("tender_id") REFERENCES "tenders" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create "time_logs" table
CREATE TABLE "time_logs" ("id" uuid NOT NULL, "task_id" uuid NULL, "tenant_id" uuid NOT NULL, "user_id" uuid NOT NULL, "hours" double precision NOT NULL, "description" text NULL, "logged_date" timestamptz NOT NULL, "is_billable" boolean NOT NULL DEFAULT false, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "project_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "time_logs_projects_time_logs" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "timelog_tenant_id_project_id" to table: "time_logs"
CREATE INDEX "timelog_tenant_id_project_id" ON "time_logs" ("tenant_id", "project_id");
-- Create index "timelog_tenant_id_user_id" to table: "time_logs"
CREATE INDEX "timelog_tenant_id_user_id" ON "time_logs" ("tenant_id", "user_id");
