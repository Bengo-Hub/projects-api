package financials

import (
	"strconv"
	"strings"
)

// Commercial is the project's contract side: what the client pays against what the work costs.
//
//   - fixed: margin at completion = contract value - EAC; unbilled = contract value - invoiced.
//   - time_and_materials: revenue follows the work, so margin to date = invoiced - actual cost.
//   - non_billable (or unset): internal work, no margin.
type Commercial struct {
	BillingType        string   `json:"billing_type,omitempty"`
	ContractValue      *float64 `json:"contract_value,omitempty"`
	Invoiced           float64  `json:"invoiced"`
	Unbilled           *float64 `json:"unbilled,omitempty"`
	ProjectedMargin    *float64 `json:"projected_margin,omitempty"`
	ProjectedMarginPct *float64 `json:"projected_margin_pct,omitempty"`
	// MarginBasis says which figure ProjectedMargin is: "at_completion" or "to_date".
	MarginBasis    string `json:"margin_basis,omitempty"`
	ClientID       string `json:"client_id,omitempty"`
	ClientName     string `json:"client_name,omitempty"`
	CostCenterID   string `json:"cost_center_id,omitempty"`
	CostCenterName string `json:"cost_center_name,omitempty"`
}

// commercialFrom reads the project's commercial metadata and prices it against the money and
// earned-value figures. Pure, so it is unit tested directly.
func commercialFrom(meta map[string]any, invoiced, actualCost, eac float64) Commercial {
	c := Commercial{
		BillingType:    metaString(meta, "billing_type"),
		Invoiced:       round2(invoiced),
		ClientID:       metaString(meta, "crm_contact_id"),
		ClientName:     metaString(meta, "crm_contact_name"),
		CostCenterID:   metaString(meta, "cost_center_id"),
		CostCenterName: metaString(meta, "cost_center_name"),
	}
	if v, ok := metaNumber(meta, "contract_value"); ok {
		c.ContractValue = &v
	}
	switch c.BillingType {
	case "fixed":
		if c.ContractValue == nil {
			return c
		}
		cv := *c.ContractValue
		unbilled := round2(max0(cv - invoiced))
		margin := round2(cv - eac)
		c.Unbilled, c.ProjectedMargin, c.MarginBasis = &unbilled, &margin, "at_completion"
		if cv > 0 {
			pct := round2(margin / cv * 100)
			c.ProjectedMarginPct = &pct
		}
	case "time_and_materials":
		margin := round2(invoiced - actualCost)
		c.ProjectedMargin, c.MarginBasis = &margin, "to_date"
		if invoiced > 0 {
			pct := round2(margin / invoiced * 100)
			c.ProjectedMarginPct = &pct
		}
	}
	return c
}

func metaString(meta map[string]any, key string) string {
	s, _ := meta[key].(string)
	return s
}

func metaNumber(meta map[string]any, key string) (float64, bool) {
	switch n := meta[key].(type) {
	case float64:
		return n, true
	case string:
		v, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return v, err == nil
	}
	return 0, false
}

func max0(v float64) float64 {
	if v < 0 {
		return 0
	}
	return v
}
