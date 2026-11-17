package financials

import (
	"strconv"
	"strings"

	"github.com/Bengo-Hub/httpware/money"
)

// Commercial is the project's contract side: what the client pays against what the work costs.
// Cost is treasury's booked cost plus ERP labour (approved hours at each person's hourly rate);
// payroll is not booked against projects in treasury, so the two never overlap.
//
//   - fixed: margin at completion = contract value - (EAC + labour to date); unbilled = contract
//     value - invoiced. EAC comes from treasury's figures, so labour to date is added to it.
//   - time_and_materials and retainer: revenue follows the work, so margin to date = invoiced -
//     cost to date.
//   - non_billable (or unset): internal work, no margin.
//
// MarginToDate (invoiced - cost to date) is filled for every billable type; the client rollup
// sums it.
type Commercial struct {
	BillingType        string   `json:"billing_type,omitempty"`
	ContractValue      *float64 `json:"contract_value,omitempty"`
	Invoiced           float64  `json:"invoiced"`
	LabourCost         float64  `json:"labour_cost"`
	CostToDate         float64  `json:"cost_to_date"`
	MarginToDate       *float64 `json:"margin_to_date,omitempty"`
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
func commercialFrom(meta map[string]any, invoiced, actualCost, eac, labour float64) Commercial {
	cost := actualCost + labour
	c := Commercial{
		BillingType:    metaString(meta, "billing_type"),
		Invoiced:       money.Round2(invoiced),
		LabourCost:     money.Round2(labour),
		CostToDate:     money.Round2(cost),
		ClientID:       metaString(meta, "crm_contact_id"),
		ClientName:     metaString(meta, "crm_contact_name"),
		CostCenterID:   metaString(meta, "cost_center_id"),
		CostCenterName: metaString(meta, "cost_center_name"),
	}
	if v, ok := metaNumber(meta, "contract_value"); ok {
		c.ContractValue = &v
	}
	switch c.BillingType {
	case "fixed", "time_and_materials", "retainer":
		toDate := money.Round2(invoiced - cost)
		c.MarginToDate = &toDate
	}
	switch c.BillingType {
	case "fixed":
		if c.ContractValue == nil {
			return c
		}
		cv := *c.ContractValue
		unbilled := money.Round2(max0(cv - invoiced))
		margin := money.Round2(cv - (eac + labour))
		c.Unbilled, c.ProjectedMargin, c.MarginBasis = &unbilled, &margin, "at_completion"
		if cv > 0 {
			pct := money.Round2(margin / cv * 100)
			c.ProjectedMarginPct = &pct
		}
	case "time_and_materials", "retainer":
		margin := *c.MarginToDate
		c.ProjectedMargin, c.MarginBasis = &margin, "to_date"
		if invoiced > 0 {
			pct := money.Round2(margin / invoiced * 100)
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
