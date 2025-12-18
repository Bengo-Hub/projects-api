# Contributing Guide

We welcome contributions to the Projects Service. Please review this guide before submitting changes.

## Environment Setup

1. Install Go 1.24+, Docker, and make.
2. Provision PostgreSQL and Redis (see `docker-compose.yml` or local setup).
3. Copy sample environment variables (`config/example.env` to `.env`).
4. Run `go generate ./internal/ent` whenever schema files change.

## Workflow

1. Branch from `main`.
2. Implement changes with clear, self-contained commits.
3. Run `go fmt ./...`, `golangci-lint run`, and `go test ./...`.
4. Update docs (`plan.md`, READMEs) as needed.
5. Open a pull request describing the changes, rationale, and testing.

## Coding Standards

- Follow idiomatic Go patterns and clean architecture boundaries.
- Keep module interfaces small; prefer dependency injection over globals.
- Use table-driven tests; leverage Testcontainers for DB/Redis integration tests.
- Ensure migrations are reversible and reviewed together with schema changes.
- Always sync user creation with auth-service for SSO compatibility.

## Commit Style

- Use descriptive messages (`projects: add user management`, `rbac: implement permission checks`).
- Reference task/issue IDs where applicable.
- Avoid large mixed commits; keep concerns separated.

## Issue Reporting

- Provide reproduction steps, expected vs actual behaviour, service logs.
- Tag severity (`bug`, `enhancement`, `question`, `security`).
- For security concerns, follow the guidance in `SECURITY.md`.

## Communication

- Slack channel: `#bengobox-projects`.
- Weekly triage: Wednesdays 14:00 EAT.
- Architecture decisions recorded as ADRs in `docs/`.

Thanks for helping build a world-class project management platform!

