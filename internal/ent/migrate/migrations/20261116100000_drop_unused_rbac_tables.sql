-- The first RBAC tables (roles keyed by a text code, per-tenant permissions, user_roles) were
-- never written to. They are dropped so the reshaped roles, permissions, role_permissions and
-- user_role_assignments tables can be created in the next migration. In production the migrator
-- runs the same drop (migrate.ReshapeRBACTables) before Schema.Create, only while roles.code exists.
DROP TABLE IF EXISTS "user_roles" CASCADE;
DROP TABLE IF EXISTS "role_permissions" CASCADE;
DROP TABLE IF EXISTS "permissions" CASCADE;
DROP TABLE IF EXISTS "roles" CASCADE;
