-- Create "field_keys" table: field encryption data keys, sealed under FIELD_ENCRYPTION_KEY.
CREATE TABLE "field_keys" ("id" character varying(16) NOT NULL, "key_enc" character varying(512) NULL, "fingerprint" character varying(16) NOT NULL, "root_fingerprint" character varying(16) NOT NULL, "status" character varying NOT NULL DEFAULT 'current', "created_by" uuid NULL, "created_at" timestamptz NOT NULL, "reseal_heartbeat_at" timestamptz NULL, "reseal_done_at" timestamptz NULL, "resealed" bigint NOT NULL DEFAULT 0, "reseal_failed" bigint NOT NULL DEFAULT 0, "retired_at" timestamptz NULL, PRIMARY KEY ("id"));
-- Create index "fieldkey_one_current" to table: "field_keys"
CREATE UNIQUE INDEX "fieldkey_one_current" ON "field_keys" ("status") WHERE (status = 'current');
-- Create "field_key_audits" table: backups, rotations and retirements of the field keys.
CREATE TABLE "field_key_audits" ("id" uuid NOT NULL, "action" character varying(32) NOT NULL, "actor_id" uuid NULL, "actor_email" character varying(254) NULL, "key_id" character varying(16) NULL, "fingerprint" character varying(16) NULL, "root_fingerprint" character varying(16) NULL, "detail" character varying(500) NULL, "created_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "fieldkeyaudit_action_created_at" to table: "field_key_audits"
CREATE INDEX "fieldkeyaudit_action_created_at" ON "field_key_audits" ("action", "created_at");
