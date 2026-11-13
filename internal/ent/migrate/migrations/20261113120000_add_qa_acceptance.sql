-- Create "qa_templates" table
CREATE TABLE "qa_templates" ("id" uuid NOT NULL, "tenant_id" uuid NULL, "name" character varying(200) NOT NULL, "kind" character varying NOT NULL DEFAULT 'software', "service_tag" character varying(64) NULL, "version" bigint NOT NULL DEFAULT 1, "status" character varying NOT NULL DEFAULT 'draft', "needs_review" boolean NOT NULL DEFAULT false, "pass_rule" jsonb NULL, "metadata" jsonb NULL, "created_by" uuid NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "qatemplate_tenant_id_created_at" to table: "qa_templates"
CREATE INDEX "qatemplate_tenant_id_created_at" ON "qa_templates" ("tenant_id", "created_at");
-- Create index "qatemplate_service_tag" to table: "qa_templates"
CREATE INDEX "qatemplate_service_tag" ON "qa_templates" ("service_tag");
-- Create "qa_criteria" table
CREATE TABLE "qa_criteria" ("id" uuid NOT NULL, "tenant_id" uuid NULL, "subject_kind" character varying NOT NULL DEFAULT 'none', "subject_ref" character varying(200) NULL, "area_code" character varying(32) NULL, "title" character varying(300) NOT NULL, "requirement_ref" character varying(100) NULL, "preconditions" text NULL, "steps" text NULL, "expected_result" text NULL, "weight" character varying NOT NULL DEFAULT 'must', "automated_ref" character varying(300) NULL, "sort_order" bigint NOT NULL DEFAULT 0, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "template_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "qa_criteria_qa_templates_criteria" FOREIGN KEY ("template_id") REFERENCES "qa_templates" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "qacriterion_template_id_sort_order" to table: "qa_criteria"
CREATE INDEX "qacriterion_template_id_sort_order" ON "qa_criteria" ("template_id", "sort_order");
-- Create index "qacriterion_subject_kind_subject_ref" to table: "qa_criteria"
CREATE INDEX "qacriterion_subject_kind_subject_ref" ON "qa_criteria" ("subject_kind", "subject_ref");
-- Create "qa_sessions" table
CREATE TABLE "qa_sessions" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "template_id" uuid NULL, "template_version" bigint NOT NULL DEFAULT 1, "title" character varying(200) NOT NULL, "subject_kind" character varying NOT NULL DEFAULT 'general', "subject_ref" character varying(100) NULL, "criteria_snapshot" jsonb NULL, "pass_rule" jsonb NULL, "status" character varying NOT NULL DEFAULT 'draft', "share_code" character varying NOT NULL, "owner_id" uuid NOT NULL, "opened_at" timestamptz NULL, "closed_at" timestamptz NULL, "document_hash" character varying(64) NULL, "metadata" jsonb NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "qa_sessions_share_code_key" to table: "qa_sessions"
CREATE UNIQUE INDEX "qa_sessions_share_code_key" ON "qa_sessions" ("share_code");
-- Create index "qasession_tenant_id_created_at" to table: "qa_sessions"
CREATE INDEX "qasession_tenant_id_created_at" ON "qa_sessions" ("tenant_id", "created_at");
-- Create index "qasession_tenant_id_status" to table: "qa_sessions"
CREATE INDEX "qasession_tenant_id_status" ON "qa_sessions" ("tenant_id", "status");
-- Create "qa_invites" table
CREATE TABLE "qa_invites" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "session_id" uuid NOT NULL, "email_hash" character varying(64) NOT NULL, "email_hint" character varying(200) NULL, "email_enc" character varying(1000) NULL, "name" character varying(200) NULL, "role" character varying NOT NULL DEFAULT 'tester', "status" character varying NOT NULL DEFAULT 'invited', "last_seen_at" timestamptz NULL, "invited_by" uuid NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "qainvite_session_id_email_hash" to table: "qa_invites"
CREATE UNIQUE INDEX "qainvite_session_id_email_hash" ON "qa_invites" ("session_id", "email_hash");
-- Create "qa_results" table
CREATE TABLE "qa_results" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "session_id" uuid NOT NULL, "criterion_id" uuid NOT NULL, "tester_key" character varying(64) NOT NULL, "tester_hint" character varying(200) NULL, "tester_user_id" uuid NULL, "invite_id" uuid NULL, "result" character varying NOT NULL, "severity" bigint NULL, "device" character varying(200) NULL, "notes" text NULL, "evidence_keys" jsonb NULL, "defect_id" uuid NULL, "is_current" boolean NOT NULL DEFAULT true, "created_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "qaresult_session_id_criterion_id_tester_key" to table: "qa_results"
CREATE UNIQUE INDEX "qaresult_session_id_criterion_id_tester_key" ON "qa_results" ("session_id", "criterion_id", "tester_key") WHERE is_current;
-- Create index "qaresult_session_id_created_at" to table: "qa_results"
CREATE INDEX "qaresult_session_id_created_at" ON "qa_results" ("session_id", "created_at");
-- Create "qa_defects" table
CREATE TABLE "qa_defects" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "session_id" uuid NOT NULL, "criterion_id" uuid NOT NULL, "first_result_id" uuid NOT NULL, "severity" bigint NOT NULL, "status" character varying NOT NULL DEFAULT 'open', "ticket_id" character varying(64) NULL, "ticket_number" character varying(64) NULL, "last_error" character varying(500) NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "qadefect_session_id_criterion_id" to table: "qa_defects"
CREATE UNIQUE INDEX "qadefect_session_id_criterion_id" ON "qa_defects" ("session_id", "criterion_id");
