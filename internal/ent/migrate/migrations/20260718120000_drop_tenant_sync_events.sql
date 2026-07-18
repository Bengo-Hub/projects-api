-- Drop the dead "tenant_sync_events" table: the TenantSyncEvent ent schema was removed
-- (no consumer ever populated it — projects-api is publish-only; tenant sync scaffolding
-- was dead code).
DROP TABLE IF EXISTS "tenant_sync_events";
