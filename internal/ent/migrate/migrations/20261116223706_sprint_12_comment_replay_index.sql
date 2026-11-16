-- A comment replayed with the same Idempotency-Key is found by tenant, author and age.
-- Create index "comment_tenant_id_user_id_created_at" to table: "comments"
CREATE INDEX "comment_tenant_id_user_id_created_at" ON "comments" ("tenant_id", "user_id", "created_at");
