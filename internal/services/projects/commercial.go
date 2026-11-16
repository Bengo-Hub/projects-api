package projects

import (
	"fmt"
	"regexp"
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
//	hourly_rate       price of a billable hour in the project's currency (decimal string, two
//	                  places at most, above zero); time_and_materials projects bill logged hours
const (
	MetaBillingType    = "billing_type"
	MetaContractValue  = "contract_value"
	MetaHourlyRate     = "hourly_rate"
	MetaCRMContactID   = "crm_contact_id"
	MetaCRMContactName = "crm_contact_name"
	MetaCostCenterID   = "cost_center_id"
	MetaCostCenterName = "cost_center_name"
)

// metaStatuses is the board's status columns (tasks.MetaStatuses). Only the statuses route,
// which validates them, writes it, so a project create or update drops it from its metadata.
const metaStatuses = "statuses"

var billingTypes = map[string]bool{"fixed": true, "time_and_materials": true, "non_billable": true}

// BillingTimeAndMaterials is the billing type whose logged hours are billable.
const BillingTimeAndMaterials = "time_and_materials"

// BillsHours reports whether time logged on a project with this metadata is billable by default:
// only time and materials work is billed by the hour.
func BillsHours(meta map[string]any) bool {
	s, _ := meta[MetaBillingType].(string)
	return s == BillingTimeAndMaterials
}

// hourlyRatePattern is a positive decimal with at most two places, the precision treasury invoices.
var hourlyRatePattern = regexp.MustCompile(`^\d{1,12}(\.\d{1,2})?$`)

// normalizeHourlyRate validates hourly_rate in place: a number or decimal string above zero with
// at most two places, stored as a string so no float rounding reaches an invoice. Empty clears it.
func normalizeHourlyRate(meta map[string]any) error {
	v, ok := meta[MetaHourlyRate]
	if !ok || v == nil {
		return nil
	}
	var s string
	switch n := v.(type) {
	case string:
		s = strings.TrimSpace(n)
	case float64:
		s = strconv.FormatFloat(n, 'f', -1, 64)
	case int:
		s = strconv.Itoa(n)
	default:
		return ErrValidation("hourly_rate must be a decimal string such as \"2500.00\"")
	}
	if s == "" {
		meta[MetaHourlyRate] = nil
		return nil
	}
	if !hourlyRatePattern.MatchString(s) || strings.Trim(s, "0.") == "" {
		return ErrValidation("hourly_rate must be above zero with at most two decimal places")
	}
	meta[MetaHourlyRate] = s
	return nil
}

// normalizeCommercial validates the commercial keys of a metadata map in place: billing type
// from the allowed set, a non-negative contract value stored as a number, and UUID ids. An empty
// id also clears its display name. The board statuses key is dropped. Other keys pass through
// untouched.
func normalizeCommercial(meta map[string]any) error {
	if meta == nil {
		return nil
	}
	delete(meta, metaStatuses)
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
	if err := normalizeHourlyRate(meta); err != nil {
		return err
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
		parsed, err := uuid.Parse(strings.TrimSpace(s))
		if err != nil {
			return ErrValidation(fmt.Sprintf("%s must be a UUID", idKey))
		}
		// Store the canonical lower-case hyphenated form: uuid.Parse also accepts upper case,
		// 32 hex digits, {...} and urn:uuid: forms, and the crm-links check matches the
		// canonical form only.
		meta[idKey] = parsed.String()
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
