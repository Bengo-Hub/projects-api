# Projects Service

The Projects Service provides project management capabilities for the Codevertex ecosystem, including project creation, task management, team collaboration, and resource allocation. It integrates with auth-service for SSO authentication and user management.

## Key Features

- Multi-tenant project management with organization-aware access control
- User management with RBAC (Role-Based Access Control) and permissions
- SSO integration with auth-service for centralized authentication
- Real-time collaboration features via event-driven architecture
- RESTful API with OpenAPI/Swagger documentation

## Tech Stack

- Go 1.24+, PostgreSQL, Redis, NATS
- HTTP transport via `chi` router
- JWT validation via `shared/auth-client`
- Observability: zap logging, Prometheus metrics, OpenTelemetry traces

## Getting Started

```shell
cp config/example.env .env
go mod download
docker compose up -d postgres redis nats
go run ./cmd/api
```

APIs default to `http://localhost:4005`. Configure via `PROJECTS_HTTP_PORT`.

## Project Structure

```
cmd/
  api/         # HTTP entrypoint
internal/
  app/         # Bootstrap and lifecycle
  config/      # Environment configuration loader
  http/        # Chi handlers and routes
  platform/    # Infrastructure adapters (database, cache, events)
  services/    # Domain services (rbac, usersync)
  shared/      # Logger and middleware
```

## User Management & RBAC

The service includes comprehensive user management with:
- User creation and synchronization with auth-service SSO
- Role-Based Access Control (RBAC) with permissions
- Tenant-aware access control
- User role assignment and management

### Default Roles

- **admin**: Full access to all projects and user management
- **member**: Can create and manage projects
- **viewer**: Read-only access to projects

## API Documentation

- Swagger UI: `http://localhost:4005/v1/docs/` (when implemented)
- All API endpoints are under `/api/v1/{tenantID}/`

## Environment Variables

All configuration keys prefixed with `PROJECTS_`. See [`config/example.env`](config/example.env) for details.

### Auth Service SSO Integration

- `PROJECTS_AUTH_SERVICE_URL`: Auth service URL (default: `https://auth.codevertex.local:4101`)
- `PROJECTS_AUTH_JWKS_URL`: JWKS endpoint for JWT validation
- `PROJECTS_AUTH_SERVICE_API_KEY`: API key for user sync operations

## Documentation

- [`plan.md`](plan.md) - Service architecture and roadmap
- [`CHANGELOG.md`](CHANGELOG.md) - Version history
- [`docs/`](docs/) - Additional documentation

## Community & Governance

- [`CONTRIBUTING.md`](CONTRIBUTING.md)
- [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md)
- [`SECURITY.md`](SECURITY.md)
- [`SUPPORT.md`](SUPPORT.md)

