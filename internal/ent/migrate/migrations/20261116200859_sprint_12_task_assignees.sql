-- Create index "task_tenant_id_project_id_due_date" to table: "tasks"
CREATE INDEX "task_tenant_id_project_id_due_date" ON "tasks" ("tenant_id", "project_id", "due_date");
-- Create "task_assignees" table
CREATE TABLE "task_assignees" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "user_id" uuid NOT NULL, "is_primary" boolean NOT NULL DEFAULT false, "created_at" timestamptz NOT NULL, "task_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "task_assignees_tasks_assignees" FOREIGN KEY ("task_id") REFERENCES "tasks" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "taskassignee_task_id_user_id" to table: "task_assignees"
CREATE UNIQUE INDEX "taskassignee_task_id_user_id" ON "task_assignees" ("task_id", "user_id");
-- Create index "taskassignee_tenant_id_user_id" to table: "task_assignees"
CREATE INDEX "taskassignee_tenant_id_user_id" ON "task_assignees" ("tenant_id", "user_id");
-- Each task's assignee_id becomes its primary assignee, then the column goes (dropping it also
-- drops its (tenant_id, assignee_id) index). In production the migrator runs the same move in one
-- transaction after Schema.Create (migrate.MoveTaskAssignees), only while the column exists.
INSERT INTO "task_assignees" ("id", "tenant_id", "task_id", "user_id", "is_primary", "created_at")
SELECT gen_random_uuid(), t."tenant_id", t."id", t."assignee_id", true, now()
FROM "tasks" t
WHERE t."assignee_id" IS NOT NULL
ON CONFLICT ("task_id", "user_id") DO UPDATE SET "is_primary" = true;
ALTER TABLE "tasks" DROP COLUMN IF EXISTS "assignee_id";
