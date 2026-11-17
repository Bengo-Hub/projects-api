package financials

import (
	"context"
	"sort"

	"github.com/Bengo-Hub/httpware/money"
	"github.com/google/uuid"
)

// maxClientRollupProjects bounds one client rollup. Each page of 100 projects costs one project
// query, one task query, one treasury batch and one ERP batch, so the cap keeps the call at 20
// pages; Truncated says when a tenant has more.
const maxClientRollupProjects = 2000

// ClientProfit is the money side of every billable project of one client, in one currency.
// Margin is to date: invoiced revenue against treasury cost plus ERP labour, the figure that is
// comparable across fixed, time and materials and retainer work.
type ClientProfit struct {
	ClientID      string   `json:"client_id"`
	ClientName    string   `json:"client_name"`
	Currency      string   `json:"currency"`
	Projects      int      `json:"projects"`
	ContractValue float64  `json:"contract_value"`
	Invoiced      float64  `json:"invoiced"`
	Unbilled      float64  `json:"unbilled"`
	LabourCost    float64  `json:"labour_cost"`
	CostToDate    float64  `json:"cost_to_date"`
	MarginToDate  float64  `json:"margin_to_date"`
	MarginPct     *float64 `json:"margin_pct,omitempty"`
}

// ClientRollup is the per-client profitability report.
type ClientRollup struct {
	Clients []ClientProfit `json:"clients"`
	// Truncated means the tenant has more projects than one rollup reads.
	Truncated bool `json:"truncated"`
	// MoneyUnavailable means treasury did not answer for some projects, so their money is missing.
	MoneyUnavailable bool `json:"money_unavailable"`
}

// Clients rolls the portfolio up by client (the project's CRM contact). Non-billable projects are
// left out; billable projects without a client are grouped under an empty client id.
func (s *Service) Clients(ctx context.Context, tenantID uuid.UUID, status string, visibleTo *uuid.UUID) (*ClientRollup, error) {
	type key struct{ client, currency string }
	groups := map[key]*ClientProfit{}
	out := &ClientRollup{Clients: []ClientProfit{}}
	for offset := 0; offset < maxClientRollupProjects; offset += maxPortfolioPage {
		page, total, err := s.Portfolio(ctx, tenantID, PortfolioFilter{Status: status, Limit: maxPortfolioPage, Offset: offset, VisibleTo: visibleTo})
		if err != nil {
			return nil, err
		}
		for _, pf := range page {
			if pf.MoneyError != "" {
				out.MoneyUnavailable = true
			}
			c := pf.Commercial
			if c.MarginToDate == nil {
				continue
			}
			k := key{c.ClientID, pf.Currency}
			g := groups[k]
			if g == nil {
				g = &ClientProfit{ClientID: c.ClientID, ClientName: c.ClientName, Currency: pf.Currency}
				groups[k] = g
			}
			if g.ClientName == "" {
				g.ClientName = c.ClientName
			}
			g.Projects++
			if c.BillingType == "fixed" && c.ContractValue != nil {
				g.ContractValue += *c.ContractValue
			}
			if c.Unbilled != nil {
				g.Unbilled += *c.Unbilled
			}
			g.Invoiced += c.Invoiced
			g.LabourCost += c.LabourCost
			g.CostToDate += c.CostToDate
		}
		out.Truncated = total > maxClientRollupProjects
		if offset+len(page) >= total || len(page) == 0 {
			break
		}
	}
	for _, g := range groups {
		g.ContractValue, g.Unbilled = money.Round2(g.ContractValue), money.Round2(g.Unbilled)
		g.Invoiced, g.LabourCost, g.CostToDate = money.Round2(g.Invoiced), money.Round2(g.LabourCost), money.Round2(g.CostToDate)
		g.MarginToDate = money.Round2(g.Invoiced - g.CostToDate)
		if g.Invoiced > 0 {
			pct := money.Round2(g.MarginToDate / g.Invoiced * 100)
			g.MarginPct = &pct
		}
		out.Clients = append(out.Clients, *g)
	}
	// Most profitable first; ties by name so the order is stable.
	sort.Slice(out.Clients, func(i, j int) bool {
		a, b := out.Clients[i], out.Clients[j]
		if a.MarginToDate != b.MarginToDate {
			return a.MarginToDate > b.MarginToDate
		}
		return a.ClientName < b.ClientName
	})
	return out, nil
}
