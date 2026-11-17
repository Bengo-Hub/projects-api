-- Create index "task_recurring_pending" to table: "tasks"
CREATE INDEX "task_recurring_pending" ON "tasks" ("due_date") WHERE (((metadata -> 'repeat'::text) IS NOT NULL) AND ((metadata #>> '{recurrence,spawned}'::text[]) IS NULL));
-- Create "project_baselines" table
CREATE TABLE "project_baselines" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "project_id" uuid NOT NULL, "name" character varying NOT NULL, "note" character varying NULL, "snapshot" jsonb NOT NULL, "task_count" bigint NOT NULL DEFAULT 0, "created_by" uuid NULL, "created_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "projectbaseline_tenant_id_project_id_created_at" to table: "project_baselines"
CREATE INDEX "projectbaseline_tenant_id_project_id_created_at" ON "project_baselines" ("tenant_id", "project_id", "created_at");
-- Create "project_change_requests" table
CREATE TABLE "project_change_requests" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "project_id" uuid NOT NULL, "title" character varying NOT NULL, "description" character varying NULL, "kind" character varying NOT NULL DEFAULT 'scope', "status" character varying NOT NULL DEFAULT 'pending', "requested_by" uuid NOT NULL, "decided_by" uuid NULL, "decided_at" timestamptz NULL, "decision_comment" character varying NULL, "metadata" jsonb NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "projectchangerequest_tenant_id_project_id_status_created_at" to table: "project_change_requests"
CREATE INDEX "projectchangerequest_tenant_id_project_id_status_created_at" ON "project_change_requests" ("tenant_id", "project_id", "status", "created_at");
-- Create index "projectchangerequest_tenant_id_status_created_at" to table: "project_change_requests"
CREATE INDEX "projectchangerequest_tenant_id_status_created_at" ON "project_change_requests" ("tenant_id", "status", "created_at");
-- Create "project_risks" table
CREATE TABLE "project_risks" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "project_id" uuid NOT NULL, "title" character varying NOT NULL, "description" character varying NULL, "category" character varying NULL, "likelihood" bigint NOT NULL, "impact" bigint NOT NULL, "score" bigint NOT NULL, "owner_id" uuid NULL, "mitigation" character varying NULL, "status" character varying NOT NULL DEFAULT 'open', "due_date" timestamptz NULL, "created_by" uuid NULL, "metadata" jsonb NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "projectrisk_tenant_id_project_id_score" to table: "project_risks"
CREATE INDEX "projectrisk_tenant_id_project_id_score" ON "project_risks" ("tenant_id", "project_id", "score");
-- Create index "projectrisk_tenant_id_project_id_status" to table: "project_risks"
CREATE INDEX "projectrisk_tenant_id_project_id_status" ON "project_risks" ("tenant_id", "project_id", "status");
-- Create "project_templates" table
CREATE TABLE "project_templates" ("id" uuid NOT NULL, "tenant_id" uuid NULL, "system_key" character varying NULL, "name" character varying NOT NULL, "description" character varying NULL, "category" character varying NULL, "definition" jsonb NOT NULL, "task_count" bigint NOT NULL DEFAULT 0, "source_project_id" uuid NULL, "created_by" uuid NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "projecttemplate_system_key" to table: "project_templates"
CREATE UNIQUE INDEX "projecttemplate_system_key" ON "project_templates" ("system_key");
-- Create index "projecttemplate_tenant_id_name" to table: "project_templates"
CREATE INDEX "projecttemplate_tenant_id_name" ON "project_templates" ("tenant_id", "name");
-- Create "tenant_settings" table
CREATE TABLE "tenant_settings" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "settings" jsonb NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "tenantsetting_tenant_id" to table: "tenant_settings"
CREATE UNIQUE INDEX "tenantsetting_tenant_id" ON "tenant_settings" ("tenant_id");
