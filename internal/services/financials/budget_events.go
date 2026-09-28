package financials

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	eventslib "github.com/Bengo-Hub/shared-events"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
)

// BudgetEventsConsumer keeps Project.budget, the project's budget at completion (BAC), in step
// with treasury: when a project budget is approved (a first version or a revision), its planned
// spend becomes the project's budget. Treasury owns the budget; this is a read-model copy for
// lists and quick views, the financials endpoint always reads treasury itself.
type BudgetEventsConsumer struct {
	client *ent.Client
	log    *zap.Logger
}

const (
	budgetApprovedSubject = "treasury.budget.approved"
	budgetApprovedDurable = "projects-budget-approved"
)

func NewBudgetEventsConsumer(client *ent.Client, log *zap.Logger) *BudgetEventsConsumer {
	return &BudgetEventsConsumer{client: client, log: log.Named("financials.budget_events")}
}

// Start binds to the stream treasury publishes budget events on (whatever it is named) and
// consumes until ctx ends. Treasury owns that stream, so this waits for it rather than creating
// it: a subscription against a missing stream would fail for good.
func (c *BudgetEventsConsumer) Start(ctx context.Context, nc *nats.Conn) error {
	if nc == nil {
		c.log.Warn("NATS not available; budget events consumer disabled")
		return nil
	}
	js, err := nc.JetStream()
	if err != nil {
		return err
	}
	stream := ""
	for backoff := 5 * time.Second; stream == ""; backoff = min(backoff*2, 5*time.Minute) {
		if name, err := js.StreamNameBySubject(budgetApprovedSubject); err == nil && name != "" {
			stream = name
			break
		}
		c.log.Info("treasury budget stream not found yet; retrying", zap.Duration("in", backoff))
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
	}
	eventslib.SubscribeQueueWithRebind(c.log, js, stream, budgetApprovedSubject, budgetApprovedDurable, c.handle,
		nats.Durable(budgetApprovedDurable), nats.AckExplicit(), nats.AckWait(30*time.Second), nats.MaxDeliver(5), nats.DeliverAll())
	<-ctx.Done()
	return nil
}

type budgetApproved struct {
	TenantID    string `json:"tenant_id"`
	BudgetType  string `json:"budget_type"`
	ProjectID   string `json:"project_id"`
	PlannedCost string `json:"planned_cost"`
	Currency    string `json:"currency"`
}

// decodeBudgetApproved returns the project and its new budget, ok=false when the event is not
// about a project budget with a planned cost.
func decodeBudgetApproved(data []byte) (tenantID, projectID uuid.UUID, bac float64, currency string, ok bool) {
	var env struct {
		Payload budgetApproved `json:"payload"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return
	}
	p := env.Payload
	if p.BudgetType != "project" {
		return
	}
	var err error
	if tenantID, err = uuid.Parse(p.TenantID); err != nil {
		return
	}
	if projectID, err = uuid.Parse(p.ProjectID); err != nil {
		return
	}
	if bac, err = strconv.ParseFloat(p.PlannedCost, 64); err != nil || bac < 0 {
		return
	}
	return tenantID, projectID, bac, p.Currency, true
}

func (c *BudgetEventsConsumer) handle(msg *nats.Msg) {
	tenantID, projectID, bac, currency, ok := decodeBudgetApproved(msg.Data)
	if !ok {
		_ = msg.Ack()
		return
	}
	if err := c.apply(context.Background(), tenantID, projectID, bac, currency); err != nil {
		c.log.Warn("project budget not updated; will retry", zap.String("project", projectID.String()), zap.Error(err))
		_ = msg.Nak()
		return
	}
	_ = msg.Ack()
}

// apply sets the project's budget (and currency when given). Scoped by tenant; a project that
// is not there any more is simply skipped.
func (c *BudgetEventsConsumer) apply(ctx context.Context, tenantID, projectID uuid.UUID, bac float64, currency string) error {
	u := c.client.Project.Update().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).SetBudget(bac)
	if currency != "" {
		u = u.SetCurrency(currency)
	}
	_, err := u.Save(ctx)
	return err
}
