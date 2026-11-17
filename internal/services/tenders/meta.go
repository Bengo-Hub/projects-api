package tenders

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/platform/events"
)

// Tender metadata (read with the tender, never filtered or indexed, so JSONB rather than columns):
//
//	crm_contact_id    the client in marketflow (CRM contact UUID); set by the user
//	crm_contact_name  its display name, kept so lists need no CRM call
//	tags              free labels (US-1.1)
//	bid_budget        the cost estimate behind the bid: {lines: [{name, category, planned_amount,
//	                  notes}]}. Not a ledger: it becomes a draft treasury budget when the tender
//	                  is converted, and treasury owns it from there
//	go_decision       the scored go/no-go decision (server owned, decision route)
//	go_decision_history earlier decisions, newest last, at most maxDecisionHistory (server owned)
//	outcome           the award or loss (server owned, outcome route)
//	conversion        the project the tender became and its treasury budget push (server owned)
const (
	MetaCRMContactID   = "crm_contact_id"
	MetaCRMContactName = "crm_contact_name"
	MetaTags           = "tags"
	MetaBidBudget      = "bid_budget"
	MetaGoDecision     = "go_decision"
	MetaGoHistory      = "go_decision_history"
	MetaOutcome        = "outcome"
	MetaConversion     = "conversion"
)

// serverOwned are the metadata keys only their own routes write; a create or edit drops them from
// its input and keeps the stored values.
var serverOwned = []string{MetaGoDecision, MetaGoHistory, MetaOutcome, MetaConversion}

const (
	maxBidBudgetLines = 200
	maxTags           = 20
)

// budgetCategories are treasury's budget line categories.
var budgetCategories = map[string]bool{"expense": true, "revenue": true, "capex": true}

var amountPattern = regexp.MustCompile(`^\d{1,14}(\.\d{1,2})?$`)

// normalizeAmount reads a money amount (number or decimal string, at most two places, zero or
// more) and returns it as a decimal string so no float rounding reaches treasury.
func normalizeAmount(v any) (string, bool) {
	var s string
	switch n := v.(type) {
	case string:
		s = strings.TrimSpace(n)
	case float64:
		s = strconv.FormatFloat(n, 'f', -1, 64)
	case int:
		s = strconv.Itoa(n)
	default:
		return "", false
	}
	if !amountPattern.MatchString(s) {
		return "", false
	}
	return s, true
}

// normalizeBidBudget validates bid_budget in place: up to maxBidBudgetLines lines, each with a
// name, a treasury category (expense when empty) and a planned amount of zero or more.
func normalizeBidBudget(meta map[string]any) error {
	v, ok := meta[MetaBidBudget]
	if !ok || v == nil {
		return nil
	}
	body, ok := v.(map[string]any)
	if !ok {
		return ErrValidation("bid_budget must be an object with lines")
	}
	raw, _ := body["lines"].([]any)
	if len(raw) == 0 {
		delete(meta, MetaBidBudget)
		return nil
	}
	if len(raw) > maxBidBudgetLines {
		return ErrValidation(fmt.Sprintf("bid_budget has at most %d lines", maxBidBudgetLines))
	}
	lines := make([]any, 0, len(raw))
	for i, l := range raw {
		line, ok := l.(map[string]any)
		if !ok {
			return ErrValidation(fmt.Sprintf("bid_budget line %d must be an object", i+1))
		}
		name, _ := line["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" || len([]rune(name)) > 200 {
			return ErrValidation(fmt.Sprintf("bid_budget line %d needs a name (at most 200 characters)", i+1))
		}
		cat, _ := line["category"].(string)
		if cat == "" {
			cat = "expense"
		}
		if !budgetCategories[cat] {
			return ErrValidation(fmt.Sprintf("bid_budget line %d has an unknown category %q", i+1, cat))
		}
		amount, ok := normalizeAmount(line["planned_amount"])
		if !ok {
			return ErrValidation(fmt.Sprintf("bid_budget line %d needs a planned_amount of zero or more with at most two decimal places", i+1))
		}
		out := map[string]any{"name": name, "category": cat, "planned_amount": amount}
		if notes, _ := line["notes"].(string); strings.TrimSpace(notes) != "" {
			out["notes"] = strings.TrimSpace(notes)
		}
		lines = append(lines, out)
	}
	meta[MetaBidBudget] = map[string]any{"lines": lines}
	return nil
}

// HasBidBudget reports whether a tender's metadata carries bid budget lines.
func HasBidBudget(meta map[string]any) bool {
	b, _ := meta[MetaBidBudget].(map[string]any)
	lines, _ := b["lines"].([]any)
	return len(lines) > 0
}

// normalizeInputMeta validates the user-set keys of a create or edit and drops the server-owned
// ones. A crm_contact_id of "" clears the client link and its name.
func normalizeInputMeta(meta map[string]any) error {
	if meta == nil {
		return nil
	}
	for _, k := range serverOwned {
		delete(meta, k)
	}
	if v, ok := meta[MetaCRMContactID]; ok && v != nil {
		s, _ := v.(string)
		if strings.TrimSpace(s) == "" {
			meta[MetaCRMContactID], meta[MetaCRMContactName] = nil, nil
		} else {
			id, err := uuid.Parse(strings.TrimSpace(s))
			if err != nil {
				return ErrValidation("crm_contact_id must be a UUID")
			}
			meta[MetaCRMContactID] = id.String()
		}
	}
	if v, ok := meta[MetaTags]; ok && v != nil {
		list, ok := v.([]any)
		if !ok || len(list) > maxTags {
			return ErrValidation(fmt.Sprintf("tags must be a list of at most %d labels", maxTags))
		}
		for _, t := range list {
			if s, ok := t.(string); !ok || strings.TrimSpace(s) == "" || len([]rune(s)) > 50 {
				return ErrValidation("each tag is a label of 1 to 50 characters")
			}
		}
	}
	return normalizeBidBudget(meta)
}

// mergeMeta applies a partial metadata update: keys set to null are removed, the rest replace or
// add, and keys the update does not mention (the server-owned ones included) are kept.
func mergeMeta(current, update map[string]any) map[string]any {
	out := make(map[string]any, len(current)+len(update))
	for k, v := range current {
		out[k] = v
	}
	for k, v := range update {
		if v == nil {
			delete(out, k)
			continue
		}
		out[k] = v
	}
	return out
}

// copyMeta is a shallow copy of a tender's metadata to edit.
func copyMeta(m map[string]any) map[string]any {
	out := make(map[string]any, len(m)+2)
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Tender event types (subject project.tender.<type>), written to the outbox in the same
// transaction as the change. The aggregate is the tender.
const (
	EventSubmitted       = "tender.submitted"
	EventGoDecision      = "tender.go_decision"
	EventWon             = "tender.won"
	EventLost            = "tender.lost"
	EventConverted       = "tender.converted"
	EventStatusChanged   = "tender.status_changed"
	EventSectionReview   = "tender.section.review_requested"
	EventSectionReviewed = "tender.section.reviewed"
)

// tenderPayload is the common part of every tender event.
func tenderPayload(t *ent.Tender) map[string]any {
	out := map[string]any{
		"tender_id": t.ID.String(), "tenant_id": t.TenantID.String(), "number": t.Number, "title": t.Title,
		"client_name": t.ClientName, "status": t.Status, "currency": t.Currency,
	}
	if t.EstimatedValue != 0 {
		out["estimated_value"] = t.EstimatedValue
	}
	if !t.Deadline.IsZero() {
		out["deadline"] = t.Deadline.UTC().Format(time.RFC3339)
	}
	if id, _ := t.Metadata[MetaCRMContactID].(string); id != "" {
		out["crm_contact_id"] = id
	}
	return out
}

// emit writes a tender event in the caller's transaction.
func emit(ctx context.Context, tx *ent.Tx, t *ent.Tender, eventType string, extra map[string]any) error {
	payload := tenderPayload(t)
	for k, v := range extra {
		payload[k] = v
	}
	return events.Write(ctx, tx, events.AggregateProject, eventType, t.ID, t.TenantID, payload)
}
