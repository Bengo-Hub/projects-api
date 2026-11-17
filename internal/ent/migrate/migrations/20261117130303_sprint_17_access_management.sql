-- Modify "projects_users" table
ALTER TABLE "projects_users" ADD COLUMN "deactivated_at" timestamptz NULL, ADD COLUMN "deactivated_by" uuid NULL;
-- Create "rbac_audit_logs" table
CREATE TABLE "rbac_audit_logs" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "actor_user_id" uuid NOT NULL, "actor_email" character varying NULL, "action" character varying NOT NULL, "resource_type" character varying NOT NULL, "resource_id" character varying NOT NULL, "changes" jsonb NULL, "ip_address" character varying NULL, "created_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "rbacauditlog_tenant_id_created_at" to table: "rbac_audit_logs"
CREATE INDEX "rbacauditlog_tenant_id_created_at" ON "rbac_audit_logs" ("tenant_id", "created_at");
