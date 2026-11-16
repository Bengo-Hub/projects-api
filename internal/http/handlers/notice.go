package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/services/notify"
)

// noticeCtx is the request context with the tenant slug the notification links start with
// (/{org}/projects/...): the {tenantID} path segment when it is a slug, else the token's slug when
// the token belongs to the request's tenant.
func noticeCtx(r *http.Request) context.Context {
	slug := strings.TrimSpace(chi.URLParam(r, "tenantID"))
	if _, err := uuid.Parse(slug); err == nil || slug == "" {
		slug = ""
		if a := accessOf(r); a != nil && a.Claims != nil && strings.EqualFold(a.Claims.TenantID, a.TenantID.String()) {
			slug = a.Claims.TenantSlug
		}
	}
	return notify.WithOrgSlug(r.Context(), slug)
}
