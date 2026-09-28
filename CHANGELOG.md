# Changelog

All notable changes to the Projects Service will be documented in this file.

This project follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added (2026-09-28)
- Tender workflow (sprint 1, US-1.6 to US-1.13): `POST /tenders/{id}/decision`, `GET /tenders/{id}/evaluation-summary`, `POST /tenders/{id}/status` (validated transitions, `status_history`), `POST /tenders/{id}/outcome`, tender sections with review (`/tenders/{id}/sections/...`), versioned final document (`/final-document`) and `/ready`, submissions (`POST /tenders/{id}/submit`, `GET /tenders/{id}/submissions`). New statuses: preparing, under_review, shortlisted, interview, no_go.
- `project.tender.*` events through the outbox.
- Attachments: file links on projects and tasks (`/projects/{id}/attachments`, `/projects/{id}/tasks/{taskId}/attachments`).
- The activity feed is written on task, comment, attachment, milestone, member and project changes.
- OpenAPI spec and Swagger UI at `/v1/docs/`.
- Migration `20260928101531_tender_sections_submissions_decision`.

### Fixed (2026-09-28)
- Task comment routes answered 404 (shadowed by the tasks subrouter).
- Task and tender lists ignored `limit`; they now accept `limit` (and still `page_size`).
- Deleting a tender with committees, documents or evaluations failed on foreign keys.
- Task comments and tender sub-resources were not checked against the tenant or project.
- `PUT /tenders/{id}` could set any status; it now follows the transition rules.

### Added
- **Service Bootstrap:** Complete Go service scaffolding with HTTP server, configuration, logging, health endpoints
- **Auth-Service SSO Integration:** Integrated `shared/auth-client` v0.1.0 library for production-ready JWT validation using JWKS from auth-service. All protected `/v1/{tenantID}` routes require valid Bearer tokens
- **User Management:** User creation and synchronization with auth-service SSO
- **RBAC Service:** Role-Based Access Control with default roles (admin, member, viewer) and permissions
- **Infrastructure:** PostgreSQL connection pool, Redis caching, NATS event bus integration, Prometheus metrics, structured logging with zap
- **Documentation:** README, plan.md, CHANGELOG, SECURITY, SUPPORT, CONTRIBUTING, CODE_OF_CONDUCT
- **DevOps:** Dockerfile, build.sh, Makefile, example.env configuration

### Changed
- Service now uses Go workspace (`go.work`) for local development; production deployments consume `shared/auth-client` as a private Go module

### Pending
- Ent schema implementation for projects, tasks, and user roles
- Database persistence for RBAC roles and permissions
- Complete user management APIs
- CI/CD automation
- Project management features

