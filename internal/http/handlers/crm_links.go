package handlers

import (
	"context"
	"net/http"
	"time"

	"entgo.io/ent/dialect/sql"
	sharedcache "github.com/Bengo-Hub/cache"
	httpware "github.com/Bengo-Hub/httpware"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/predicate"
	"github.com/bengobox/projects-service/internal/ent/project"
)

// TenantLookup resolves a tenant slug to auth-api's tenant; *TenantResolver implements it.
type TenantLookup interface {
	GetTenantDetails(ctx context.Context, slug string) (sharedcache.TenantDetails, error)
}

// TenantResolver resolves tenant details through the Redis cache, then auth-api. Projects keeps no
// tenant table of its own.
type TenantResolver struct {
	aside   *sharedcache.Aside
	authURL string
}

// NewTenantResolver builds a resolver. aside may be nil (no cache, always asks auth-api).
func NewTenantResolver(aside *sharedcache.Aside, authURL string) *TenantResolver {
	return &TenantResolver{aside: aside, authURL: authURL}
}

// GetTenantDetails returns the tenant for a slug.
func (t *TenantResolver) GetTenantDetails(ctx context.Context, slug string) (sharedcache.TenantDetails, error) {
	return sharedcache.GetTenantDetails(ctx, t.aside, t.authURL, slug, 0)
}

// CRMLinksHandler tells marketflow whether projects still links a CRM contact, so marketflow can
// decide between deleting an erased person's contact and stripping it to a minimal record.
//
// Projects keeps no tenant table, so it cannot check a tenant UUID: a UUID is accepted as given,
// and a slug is resolved through auth-api (an unknown one answers 404). The contact id is matched
// in every tenant: CRM contact ids are random UUIDs minted by marketflow, so a match anywhere is
// this person, and a project held under a mismatched tenant id can only answer "referenced", which
// keeps the contact.
type CRMLinksHandler struct {
	referenced func(ctx context.Context, contactID uuid.UUID) (bool, error)
	tenants    TenantLookup
	key        string
	log        *zap.Logger
}

// NewCRMLinksHandler builds the handler over the projects table. An empty key makes the handler
// refuse every call.
func NewCRMLinksHandler(db *ent.Client, tenants TenantLookup, key string, log *zap.Logger) *CRMLinksHandler {
	if log == nil {
		log = zap.NewNop()
	}
	return &CRMLinksHandler{
		referenced: func(ctx context.Context, contactID uuid.UUID) (bool, error) {
			return ProjectReferencesContact(ctx, db, contactID)
		},
		tenants: tenants,
		key:     key,
		log:     log,
	}
}

// ProjectReferencesContact reports whether any project, in any tenant, keeps the CRM contact id in
// its metadata (the crm_contact_id key). It is the only place projects stores a CRM contact id:
// tenders keep a client name only, and task, member, comment and attachment metadata never hold
// one. The id is matched case-insensitively because the metadata keeps the text as the caller sent it.
func ProjectReferencesContact(ctx context.Context, db *ent.Client, contactID uuid.UUID) (bool, error) {
	return db.Project.Query().Where(metadataContact(contactID)).Exist(ctx)
}

// metadataContact is a JSON-path predicate on the project metadata: lower(metadata ->> 'crm_contact_id') = $1.
// The value goes through b.Arg so the driver numbers the placeholder (Postgres rejects a raw "?").
func metadataContact(contactID uuid.UUID) predicate.Project {
	return predicate.Project(func(s *sql.Selector) {
		s.Where(sql.P(func(b *sql.Builder) {
			b.WriteString("lower(").Ident(s.C(project.FieldMetadata)).WriteString(" ->> 'crm_contact_id') = ").Arg(contactID.String())
		}))
	})
}

// tenantID resolves the tenant segment: a UUID is used as given, a slug is looked up. An unknown
// slug or a failed lookup is false, so the caller answers 404 and marketflow keeps the contact.
func (h *CRMLinksHandler) tenantID(ctx context.Context, raw string) (uuid.UUID, bool) {
	if id, err := uuid.Parse(raw); err == nil {
		return id, id != uuid.Nil
	}
	if raw == "" || h.tenants == nil {
		return uuid.Nil, false
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	d, err := h.tenants.GetTenantDetails(lookupCtx, raw)
	if err != nil {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(d.ID)
	return id, err == nil && id != uuid.Nil
}

// Referenced answers whether any project, in any tenant, carries the contact id. The answer is the
// flag only: no project names, ids or client details.
// GET /api/v1/s2s/{tenant}/crm-links/{contactID} (X-API-Key) -> {"referenced": bool}
func (h *CRMLinksHandler) Referenced(w http.ResponseWriter, r *http.Request) {
	if !httpware.ValidServiceKey(r, h.key) {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	contactID, err := uuid.Parse(chi.URLParam(r, "contactID"))
	if err != nil || contactID == uuid.Nil {
		respondError(w, http.StatusBadRequest, "contact id must be a UUID")
		return
	}
	tenantID, ok := h.tenantID(r.Context(), chi.URLParam(r, "tenant"))
	if !ok {
		respondError(w, http.StatusNotFound, "tenant not found")
		return
	}
	found, err := h.referenced(r.Context(), contactID)
	if err != nil {
		// Ids only in the log, never the person's details.
		h.log.Error("crm links check failed", zap.String("tenant_id", tenantID.String()),
			zap.String("contact_id", contactID.String()), zap.Error(err))
		respondError(w, http.StatusInternalServerError, "failed to check links")
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"referenced": found})
}
