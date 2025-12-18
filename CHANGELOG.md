# Changelog

All notable changes to the Projects Service will be documented in this file.

This project follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

