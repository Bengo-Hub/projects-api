// Package treasury is projects-api's S2S client for treasury-api, the owner of budgets and
// of every booked cost and revenue. Projects never store money figures of their own: a
// project's budget is a treasury budget of type "project", and its costs are ledger lines
// tagged with the project id.
package treasury

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrNotConfigured is returned when the treasury URL or service key is missing.
var ErrNotConfigured = errors.New("treasury: not configured")

// Error is a non-2xx answer from treasury, passed through to the caller.
type Error struct {
	Status int
	Body   json.RawMessage
}

func (e *Error) Error() string { return fmt.Sprintf("treasury: status %d", e.Status) }

// Client calls treasury-api with the shared INTERNAL_SERVICE_KEY.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func NewClient(baseURL, apiKey string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, http: &http.Client{Timeout: 10 * time.Second}}
}

// Enabled reports whether S2S calls can be made.
func (c *Client) Enabled() bool { return c != nil && c.baseURL != "" && c.apiKey != "" }

// Money is a decimal amount as treasury encodes it (a JSON string or number).
type Money float64

func (m *Money) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		*m = 0
		return nil
	}
	var f float64
	if _, err := fmt.Sscanf(s, "%g", &f); err != nil {
		return fmt.Errorf("treasury money %q: %w", s, err)
	}
	*m = Money(f)
	return nil
}

// MonthAmount is one month of a series.
type MonthAmount struct {
	Month  string `json:"month"`
	Amount Money  `json:"amount"`
}

// CategoryCost is cost on one chart account.
type CategoryCost struct {
	AccountID   string `json:"account_id"`
	AccountCode string `json:"account_code"`
	AccountName string `json:"account_name"`
	Amount      Money  `json:"amount"`
}

// ProjectFinancials mirrors treasury's budgets.ProjectFinancials.
type ProjectFinancials struct {
	ProjectID      uuid.UUID      `json:"project_id"`
	Currency       string         `json:"currency"`
	BudgetID       *uuid.UUID     `json:"budget_id,omitempty"`
	BudgetStatus   string         `json:"budget_status,omitempty"`
	BudgetCost     Money          `json:"budget_cost"`
	BudgetRevenue  Money          `json:"budget_revenue"`
	ActualCost     Money          `json:"actual_cost"`
	ActualRevenue  Money          `json:"actual_revenue"`
	Committed      Money          `json:"committed"`
	Margin         Money          `json:"margin"`
	MarginPct      float64        `json:"margin_pct"`
	BudgetUsedPct  float64        `json:"budget_used_pct"`
	CostByCategory []CategoryCost `json:"cost_by_category"`
	MonthlyCost    []MonthAmount  `json:"monthly_cost"`
	MonthlyRevenue []MonthAmount  `json:"monthly_revenue"`
	PlannedByMonth []MonthAmount  `json:"planned_by_month,omitempty"`
}

func (c *Client) do(ctx context.Context, method, path string, userID *uuid.UUID, body any, out any) error {
	if !c.Enabled() {
		return ErrNotConfigured
	}
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if userID != nil {
		req.Header.Set("X-User-ID", userID.String())
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("treasury: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &Error{Status: resp.StatusCode, Body: raw}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func s2s(tenantID uuid.UUID, rest string) string {
	return "/api/v1/s2s/" + tenantID.String() + rest
}

// ProjectsFinancials fetches financials for up to 200 projects in one call.
func (c *Client) ProjectsFinancials(ctx context.Context, tenantID uuid.UUID, projectIDs []uuid.UUID) (map[uuid.UUID]ProjectFinancials, error) {
	out := map[uuid.UUID]ProjectFinancials{}
	if len(projectIDs) == 0 {
		return out, nil
	}
	ids := make([]string, len(projectIDs))
	for i, id := range projectIDs {
		ids[i] = id.String()
	}
	var resp struct {
		Data []ProjectFinancials `json:"data"`
	}
	path := s2s(tenantID, "/budgets/projects/financials?ids="+url.QueryEscape(strings.Join(ids, ",")))
	if err := c.do(ctx, http.MethodGet, path, nil, nil, &resp); err != nil {
		return nil, err
	}
	for _, f := range resp.Data {
		out[f.ProjectID] = f
	}
	return out, nil
}

// CostCenter is one of the tenant's treasury cost centres.
type CostCenter struct {
	ID       uuid.UUID `json:"id"`
	Code     string    `json:"code"`
	Name     string    `json:"name"`
	IsActive bool      `json:"is_active"`
}

// CostCenters lists the tenant's active cost centres (the project cost-centre picker).
func (c *Client) CostCenters(ctx context.Context, tenantID uuid.UUID) ([]CostCenter, error) {
	var resp struct {
		CostCenters []CostCenter `json:"cost_centers"`
	}
	if err := c.do(ctx, http.MethodGet, s2s(tenantID, "/cost-centers?active_only=true"), nil, nil, &resp); err != nil {
		return nil, err
	}
	return resp.CostCenters, nil
}

// ProjectBudgets lists the treasury budgets of one project (all versions, newest first). The raw
// treasury JSON is returned so the UI sees treasury's budget model unchanged.
func (c *Client) ProjectBudgets(ctx context.Context, tenantID, projectID uuid.UUID) (json.RawMessage, error) {
	var raw json.RawMessage
	path := s2s(tenantID, "/budgets?budget_type=project&project_id="+projectID.String()+"&limit=50")
	if err := c.do(ctx, http.MethodGet, path, nil, nil, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// CreateBudget creates a budget acting as userID; body is treasury's BudgetInput.
func (c *Client) CreateBudget(ctx context.Context, tenantID, userID uuid.UUID, body map[string]any) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodPost, s2s(tenantID, "/budgets"), &userID, body, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// UpdateBudget replaces a draft budget acting as userID.
func (c *Client) UpdateBudget(ctx context.Context, tenantID, userID, budgetID uuid.UUID, body map[string]any) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodPut, s2s(tenantID, "/budgets/"+budgetID.String()), &userID, body, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// SubmitBudget sends a budget for approval acting as userID.
func (c *Client) SubmitBudget(ctx context.Context, tenantID, userID, budgetID uuid.UUID) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodPost, s2s(tenantID, "/budgets/"+budgetID.String()+"/submit"), &userID, nil, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// GetBudget reads one budget (used to confirm it belongs to the project before changing it).
func (c *Client) GetBudget(ctx context.Context, tenantID, budgetID uuid.UUID) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, s2s(tenantID, "/budgets/"+budgetID.String()), nil, nil, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}
