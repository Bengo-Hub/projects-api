# Security Policy

The Projects Service holds sensitive project and user data. Please follow these guidelines to keep it secure.

## Supported Versions

| Version | Supported |
|---------|-----------|
| `main` branch | ✅ |
| Tagged releases (future) | ✅ |
| Older branches/forks | ❌ |

## Reporting Vulnerabilities

1. Email `security@bengobox.com` with a detailed description (do not open a public issue).
2. Include reproduction steps, impact assessment, and suggested mitigations if possible.
3. Encrypt communications when feasible; public PGP keys are available on request.

We will acknowledge reports within 48 hours and coordinate remediation and disclosure.

## Secure Development Practices

- Never commit credentials or production data.
- Use parameterised queries; Ent handles this by default.
- Validate input rigorously, especially for bulk upload endpoints.
- Enforce least privilege in database roles and service accounts.
- Run `govulncheck` / dependency scanners regularly.
- Always validate JWT tokens via auth-service JWKS endpoint.

## Infrastructure Considerations

- Enable point-in-time recovery for PostgreSQL.
- Protect message brokers (NATS/Kafka) with TLS and authentication.
- Monitor audit logs for suspicious access attempts or unauthorized operations.
- Enforce RBAC at both API and database levels.

Thank you for helping keep the BengoBox platform secure.

