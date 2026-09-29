package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	httpware "github.com/Bengo-Hub/httpware"
	"github.com/bengobox/projects-service/internal/platform/marketflow"
	"github.com/bengobox/projects-service/internal/platform/treasury"
)

// contactSearcher and costCenterLister are the S2S reads behind the project form's client and
// cost-centre pickers (marketflow is the CRM; treasury owns cost centres).
type contactSearcher interface {
	SearchContacts(ctx context.Context, tenantID uuid.UUID, q string, limit int) ([]marketflow.Contact, error)
}

type costCenterLister interface {
	CostCenters(ctx context.Context, tenantID uuid.UUID) ([]treasury.CostCenter, error)
}

// WithLookups enables the project form lookups. Either may be nil.
func (h *ProjectHandler) WithLookups(contacts contactSearcher, costCenters costCenterLister) *ProjectHandler {
	h.contacts, h.costCenters = contacts, costCenters
	return h
}

func (h *ProjectHandler) registerLookups(r chi.Router) {
	r.Get("/lookups/contacts", h.LookupContacts)
	r.Get("/lookups/cost-centers", h.LookupCostCenters)
}

type lookupOption struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
}

// LookupContacts searches the tenant's CRM contacts (?q= name, email or phone; ?limit= up to 50)
// for the project client picker.
func (h *ProjectHandler) LookupContacts(w http.ResponseWriter, r *http.Request) {
	tenantID, err := uuid.Parse(httpware.GetTenantID(r.Context()))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	if h.contacts == nil {
		respondJSON(w, http.StatusOK, map[string]any{"data": []lookupOption{}})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := h.contacts.SearchContacts(r.Context(), tenantID, strings.TrimSpace(r.URL.Query().Get("q")), limit)
	if err != nil {
		h.log.Warn("crm contact lookup failed", zap.Error(err))
		respondError(w, http.StatusBadGateway, "the CRM is not reachable right now")
		return
	}
	out := make([]lookupOption, 0, len(rows))
	for _, c := range rows {
		detail := c.Email
		if detail == "" {
			detail = c.Phone
		}
		out = append(out, lookupOption{ID: c.ID.String(), Name: c.Name(), Detail: detail})
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}

// LookupCostCenters lists the tenant's active treasury cost centres for the project form.
func (h *ProjectHandler) LookupCostCenters(w http.ResponseWriter, r *http.Request) {
	tenantID, err := uuid.Parse(httpware.GetTenantID(r.Context()))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	if h.costCenters == nil {
		respondJSON(w, http.StatusOK, map[string]any{"data": []lookupOption{}})
		return
	}
	rows, err := h.costCenters.CostCenters(r.Context(), tenantID)
	if err != nil {
		h.log.Warn("cost centre lookup failed", zap.Error(err))
		respondError(w, http.StatusBadGateway, "treasury is not reachable right now")
		return
	}
	out := make([]lookupOption, 0, len(rows))
	for _, c := range rows {
		out = append(out, lookupOption{ID: c.ID.String(), Name: c.Name, Detail: c.Code})
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}
