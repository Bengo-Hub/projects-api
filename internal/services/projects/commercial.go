package projects

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// Commercial terms live in Project.metadata (read, never filtered or indexed):
//
//	billing_type      fixed | time_and_materials | non_billable
//	contract_value    agreed price for fixed-price work (number, >= 0)
//	crm_contact_id    the client in marketflow (CRM contact UUID)
//	crm_contact_name  the client's display name, kept so lists need no CRM call
//	cost_center_id    treasury cost centre the project's spend belongs to
//	cost_center_name  its display name
const (
	MetaBillingType    = "billing_type"
	MetaContractValue  = "contract_value"
	MetaCRMContactID   = "crm_contact_id"
	MetaCRMContactName = "crm_contact_name"
	MetaCostCenterID   = "cost_center_id"
	MetaCostCenterName = "cost_center_name"
)

var billingTypes = map[string]bool{"fixed": true, "time_and_materials": true, "non_billable": true}

// normalizeCommercial validates the commercial keys of a metadata map in place: billing type
// from the allowed set, a non-negative contract value stored as a number, and UUID ids. An empty
// id also clears its display name. Other keys pass through untouched.
func normalizeCommercial(meta map[string]any) error {
	if meta == nil {
		return nil
	}
	if v, ok := meta[MetaBillingType]; ok && v != nil {
		s, _ := v.(string)
		if s == "" {
			delete(meta, MetaBillingType)
		} else if !billingTypes[s] {
			return ErrValidation("billing_type must be fixed, time_and_materials or non_billable")
		}
	}
	if v, ok := meta[MetaContractValue]; ok && v != nil {
		n, err := toNumber(v)
		if err != nil || n < 0 {
			return ErrValidation("contract_value must be a number of zero or more")
		}
		meta[MetaContractValue] = n
	}
	for idKey, nameKey := range map[string]string{MetaCRMContactID: MetaCRMContactName, MetaCostCenterID: MetaCostCenterName} {
		v, ok := meta[idKey]
		if !ok || v == nil {
			continue
		}
		s, _ := v.(string)
		if s == "" {
			meta[idKey], meta[nameKey] = nil, nil
			continue
		}
		if _, err := uuid.Parse(s); err != nil {
			return ErrValidation(fmt.Sprintf("%s must be a UUID", idKey))
		}
	}
	return nil
}

func toNumber(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case int:
		return float64(n), nil
	case string:
		return strconv.ParseFloat(strings.TrimSpace(n), 64)
	}
	return 0, fmt.Errorf("not a number")
}

// mergeMetadata applies a partial metadata update: keys set to null are removed, the rest
// replace or add, and keys the update does not mention are kept.
func mergeMetadata(current, update map[string]any) map[string]any {
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
