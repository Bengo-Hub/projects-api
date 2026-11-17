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
//	billing_type      fixed | time_and_materials | retainer | non_billable
//	contract_value    agreed price for fixed-price work (number, >= 0)
//	retainer_amount   the monthly retainer fee (decimal string like hourly_rate)
//	retainer_hours    hours the monthly fee covers (number, >= 0)
//	retainer_rollover whether unused retainer hours carry into the next month (bool)
//	tax_code          VAT on the project's invoices: VAT-16, VAT-8, VAT-0 or VAT-EXEMPT (treasury's
//	                  tax catalogue codes). Unset sends no VAT, so a VAT registered firm sets it
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
	MetaRetainerAmount = "retainer_amount"
	MetaRetainerHours  = "retainer_hours"
	MetaRetainerRoll   = "retainer_rollover"
	MetaTaxCode        = "tax_code"
	MetaCRMContactID   = "crm_contact_id"
	MetaCRMContactName = "crm_contact_name"
	MetaCostCenterID   = "cost_center_id"
	MetaCostCenterName = "cost_center_name"
)

// metaStatuses is the board's status columns (tasks.MetaStatuses). Only the statuses route,
// which validates them, writes it, so a project create or update drops it from its metadata.
const metaStatuses = "statuses"

// serverOwnedProjectMeta are the project metadata keys only their own routes write (sprint 16):
// the status rules and the automations (tasks.MetaStatusRules, tasks.MetaAutomations), validated
// there. A project create or update drops them.
var serverOwnedProjectMeta = []string{"status_rules", "automations"}

var billingTypes = map[string]bool{"fixed": true, "time_and_materials": true, "retainer": true, "non_billable": true}

// Billing types that the billing service acts on.
const (
	BillingFixed            = "fixed"
	BillingTimeAndMaterials = "time_and_materials"
	BillingRetainer         = "retainer"
	BillingNonBillable      = "non_billable"
)

// BillsHours reports whether time logged on a project with this metadata is billable by default:
// only time and materials work is billed by the hour.
func BillsHours(meta map[string]any) bool {
	s, _ := meta[MetaBillingType].(string)
	return s == BillingTimeAndMaterials
}

// hourlyRatePattern is a positive decimal with at most two places, the precision treasury invoices.
var hourlyRatePattern = regexp.MustCompile(`^\d{1,12}(\.\d{1,2})?$`)

// normalizeHourlyRate validates hourly_rate in place (see normalizePrice).
func normalizeHourlyRate(meta map[string]any) error { return normalizePrice(meta, MetaHourlyRate) }

// ValidPrice reports whether s is a price treasury can invoice: above zero, at most two places.
func ValidPrice(s string) bool {
	return hourlyRatePattern.MatchString(s) && strings.Trim(s, "0.") != ""
}

// normalizePrice validates a price key in place: a number or decimal string above zero with at
// most two places, stored as a string so no float rounding reaches an invoice. Empty clears it.
func normalizePrice(meta map[string]any, key string) error {
	v, ok := meta[key]
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
		return ErrValidation(key + " must be a decimal string such as \"2500.00\"")
	}
	if s == "" {
		meta[key] = nil
		return nil
	}
	if !ValidPrice(s) {
		return ErrValidation(key + " must be above zero with at most two decimal places")
	}
	meta[key] = s
	return nil
}

// TaxRates maps the tax codes a project may bill under to their VAT percent.
var TaxRates = map[string]string{"VAT-16": "16", "VAT-8": "8", "VAT-0": "0", "VAT-EXEMPT": "0"}

// normalizeTaxCode validates tax_code against TaxRates; empty clears it.
func normalizeTaxCode(meta map[string]any) error {
	v, ok := meta[MetaTaxCode]
	if !ok || v == nil {
		return nil
	}
	s, _ := v.(string)
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		meta[MetaTaxCode] = nil
		return nil
	}
	if _, known := TaxRates[s]; !known {
		return ErrValidation("tax_code must be VAT-16, VAT-8, VAT-0 or VAT-EXEMPT")
	}
	meta[MetaTaxCode] = s
	return nil
}

// normalizeRetainer validates the retainer keys: the monthly fee as a price, the hours as a
// number of zero or more, and the rollover rule as a boolean.
func normalizeRetainer(meta map[string]any) error {
	if err := normalizePrice(meta, MetaRetainerAmount); err != nil {
		return err
	}
	if v, ok := meta[MetaRetainerHours]; ok && v != nil {
		n, err := toNumber(v)
		if err != nil || n < 0 {
			return ErrValidation("retainer_hours must be a number of zero or more")
		}
		meta[MetaRetainerHours] = n
	}
	if v, ok := meta[MetaRetainerRoll]; ok && v != nil {
		if _, isBool := v.(bool); !isBool {
			return ErrValidation("retainer_rollover must be true or false")
		}
	}
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
	for _, k := range serverOwnedProjectMeta {
		delete(meta, k)
	}
	if v, ok := meta[MetaBillingType]; ok && v != nil {
		s, _ := v.(string)
		if s == "" {
			delete(meta, MetaBillingType)
		} else if !billingTypes[s] {
			return ErrValidation("billing_type must be fixed, time_and_materials, retainer or non_billable")
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
	if err := normalizeRetainer(meta); err != nil {
		return err
	}
	if err := normalizeTaxCode(meta); err != nil {
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
