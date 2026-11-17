-- Create "tender_sections" table
CREATE TABLE "tender_sections" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "tender_id" uuid NOT NULL, "title" character varying NOT NULL, "description" text NULL, "position" bigint NOT NULL DEFAULT 0, "assignee_id" uuid NULL, "reviewer_id" uuid NULL, "due_date" timestamptz NULL, "status" character varying NOT NULL DEFAULT 'not_started', "document_id" uuid NULL, "submitted_at" timestamptz NULL, "reviewed_by" uuid NULL, "reviewed_at" timestamptz NULL, "review_comment" text NULL, "created_by" uuid NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "tendersection_tenant_id_tender_id_position" to table: "tender_sections"
CREATE INDEX "tendersection_tenant_id_tender_id_position" ON "tender_sections" ("tenant_id", "tender_id", "position");
-- Create "tender_submissions" table
CREATE TABLE "tender_submissions" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "tender_id" uuid NOT NULL, "method" character varying NOT NULL, "submitted_at" timestamptz NOT NULL, "submitted_by" uuid NOT NULL, "recipient" character varying NULL, "reference" character varying NULL, "courier" character varying NULL, "notes" text NULL, "document_id" uuid NULL, "proof_document_id" uuid NULL, "metadata" jsonb NULL, "created_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "tendersubmission_tenant_id_tender_id_submitted_at" to table: "tender_submissions"
CREATE INDEX "tendersubmission_tenant_id_tender_id_submitted_at" ON "tender_submissions" ("tenant_id", "tender_id", "submitted_at");
