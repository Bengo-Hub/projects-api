-- Create index "activity_tenant_id_project_id_occurred_at" to table: "activities"
CREATE INDEX "activity_tenant_id_project_id_occurred_at" ON "activities" ("tenant_id", "project_id", "occurred_at");
-- Create index "activity_tenant_id_task_id_occurred_at" to table: "activities"
CREATE INDEX "activity_tenant_id_task_id_occurred_at" ON "activities" ("tenant_id", "task_id", "occurred_at");
-- Create index "attachment_tenant_id_project_id" to table: "attachments"
CREATE INDEX "attachment_tenant_id_project_id" ON "attachments" ("tenant_id", "project_id");
-- Create index "attachment_tenant_id_task_id" to table: "attachments"
CREATE INDEX "attachment_tenant_id_task_id" ON "attachments" ("tenant_id", "task_id");
-- Create index "comment_tenant_id_project_id_created_at" to table: "comments"
CREATE INDEX "comment_tenant_id_project_id_created_at" ON "comments" ("tenant_id", "project_id", "created_at");
-- Create index "comment_tenant_id_task_id_created_at" to table: "comments"
CREATE INDEX "comment_tenant_id_task_id_created_at" ON "comments" ("tenant_id", "task_id", "created_at");
-- Create index "milestone_tenant_id_project_id_target_date" to table: "milestones"
CREATE INDEX "milestone_tenant_id_project_id_target_date" ON "milestones" ("tenant_id", "project_id", "target_date");
-- Create index "outboxevent_status_created_at" to table: "outbox_events"
CREATE INDEX "outboxevent_status_created_at" ON "outbox_events" ("status", "created_at");
-- Create index "projectmember_tenant_id_project_id_user_id" to table: "project_members"
CREATE INDEX "projectmember_tenant_id_project_id_user_id" ON "project_members" ("tenant_id", "project_id", "user_id");
-- Create index "project_tenant_id_created_at" to table: "projects"
CREATE INDEX "project_tenant_id_created_at" ON "projects" ("tenant_id", "created_at");
-- Create index "project_tenant_id_status" to table: "projects"
CREATE INDEX "project_tenant_id_status" ON "projects" ("tenant_id", "status");
-- Create index "taskdependency_depends_on_task_id" to table: "task_dependencies"
CREATE INDEX "taskdependency_depends_on_task_id" ON "task_dependencies" ("depends_on_task_id");
-- Create index "taskdependency_task_id_depends_on_task_id" to table: "task_dependencies"
CREATE INDEX "taskdependency_task_id_depends_on_task_id" ON "task_dependencies" ("task_id", "depends_on_task_id");
-- Create index "task_tenant_id_completed_at" to table: "tasks"
CREATE INDEX "task_tenant_id_completed_at" ON "tasks" ("tenant_id", "completed_at");
-- Create index "task_tenant_id_created_at" to table: "tasks"
CREATE INDEX "task_tenant_id_created_at" ON "tasks" ("tenant_id", "created_at");
-- Create index "task_tenant_id_due_date" to table: "tasks"
CREATE INDEX "task_tenant_id_due_date" ON "tasks" ("tenant_id", "due_date");
-- Create index "task_tenant_id_project_id_status" to table: "tasks"
CREATE INDEX "task_tenant_id_project_id_status" ON "tasks" ("tenant_id", "project_id", "status");
-- Create index "tendercommitteemember_committee_id_user_id" to table: "tender_committee_members"
CREATE INDEX "tendercommitteemember_committee_id_user_id" ON "tender_committee_members" ("committee_id", "user_id");
-- Create index "tendercommittee_tender_id" to table: "tender_committees"
CREATE INDEX "tendercommittee_tender_id" ON "tender_committees" ("tender_id");
-- Create index "tenderdocument_tender_id" to table: "tender_documents"
CREATE INDEX "tenderdocument_tender_id" ON "tender_documents" ("tender_id");
-- Create index "tenderevaluation_tender_id" to table: "tender_evaluations"
CREATE INDEX "tenderevaluation_tender_id" ON "tender_evaluations" ("tender_id");
-- Create index "tendermeeting_tender_id" to table: "tender_meetings"
CREATE INDEX "tendermeeting_tender_id" ON "tender_meetings" ("tender_id");
-- Create index "userrole_tenant_id_user_id" to table: "user_roles"
CREATE INDEX "userrole_tenant_id_user_id" ON "user_roles" ("tenant_id", "user_id");
