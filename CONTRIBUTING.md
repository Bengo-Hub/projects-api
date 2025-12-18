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

### Naming Conventions

- **Go Packages**: Use lowercase, single-word names (e.g., `tender`, `project`). Avoid underscores or mixedCaps.
- **Go Symbols**: Use `PascalCase` for exported symbols and `camelCase` for unexported symbols.
- **Database**: Use `snake_case` for table names (plural) and column names.
- **API Endpoints**: Use `kebab-case` and plural nouns (e.g., `/api/v1/tender-opportunities`).
- **Environment Variables**: Use `UPPER_SNAKE_CASE` (e.g., `DATABASE_URL`).

### General Guidelines

- Follow idiomatic Go patterns and clean architecture boundaries.
- Keep module interfaces small; prefer dependency injection over globals.
- Use table-driven tests; leverage Testcontainers for DB/Redis integration tests.
- Ensure migrations are reversible and reviewed together with schema changes.
- Always sync user creation with auth-service for SSO compatibility.

## Commit Style

We follow the [Conventional Commits](https://www.conventionalcommits.org/) specification:

- `feat`: A new feature
- `fix`: A bug fix
- `docs`: Documentation only changes
- `style`: Changes that do not affect the meaning of the code (formatting, etc.)
- `refactor`: A code change that neither fixes a bug nor adds a feature
- `perf`: A code change that improves performance
- `test`: Adding missing tests or correcting existing tests
- `build`: Changes that affect the build system or external dependencies
- `ci`: Changes to our CI configuration files and scripts
- `chore`: Other changes that don't modify src or test files
- `revert`: Reverts a previous commit

**Example**: `feat(tender): add committee evaluation scoring matrix`

## Branching Strategy

- `main`: Production-ready code. All PRs must eventually merge here.
- `develop`: Integration branch for upcoming releases.
- **Feature Branches**: `feat/short-description` or `feature/short-description`.
- **Bugfix Branches**: `fix/short-description` or `bugfix/short-description`.
- **Hotfix Branches**: `hotfix/short-description` (branched from `main`).
- **Release Branches**: `release/vX.Y.Z`.

## Issue Reporting

- Provide reproduction steps, expected vs actual behaviour, service logs.
- Tag severity (`bug`, `enhancement`, `question`, `security`).
- For security concerns, follow the guidance in `SECURITY.md`.

## Communication

- Slack channel: `#bengobox-projects`.
- Weekly triage: Wednesdays 14:00 EAT.
- Architecture decisions recorded as ADRs in `docs/`.

Thanks for helping build a world-class project management platform!

