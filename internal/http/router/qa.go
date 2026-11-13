package router

import handlers "github.com/bengobox/projects-service/internal/http/handlers"

// QARoutes carries the QA and acceptance handlers. Either may be nil, which leaves its routes out.
type QARoutes struct {
	// Tenant serves /api/v1/{tenantID}/qa/... for the tenant's own team.
	Tenant *handlers.QAHandler
	// Invitee serves /api/v1/qa/access/... and /api/v1/qa/invitee/... for invited emails.
	Invitee *handlers.QAInviteeHandler
}
