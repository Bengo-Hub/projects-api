package router

import handlers "github.com/bengobox/projects-service/internal/http/handlers"

// QARoutes carries the QA and acceptance handlers. Either may be nil, which leaves its routes out.
type QARoutes struct {
	// Tenant serves /api/v1/{tenantID}/qa/... for the tenant's own team.
	Tenant *handlers.QAHandler
	// Invitee serves /api/v1/qa/access/... and /api/v1/qa/invitee/... for invited emails.
	Invitee *handlers.QAInviteeHandler
}

// PlatformRoutes carries the platform owner handlers under /api/v1/platform. Nil leaves them out.
type PlatformRoutes struct {
	// FieldKeys serves the "Security keys" page: field key health, backup and rotation.
	FieldKeys *handlers.FieldKeyHandler
}
