package financials

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/testutil"
)

func TestBudgetEventsApply(t *testing.T) {
	client := testutil.Client(t)
	ctx := context.Background()
	tenant := uuid.New()
	p := client.Project.Create().SetTenantID(tenant).SetName("P").SetOwnerID(uuid.New()).SaveX(ctx)
	c := NewBudgetEventsConsumer(client, zap.NewNop())
	if err := c.apply(ctx, tenant, p.ID, 1250.5, "USD"); err != nil {
		t.Fatal(err)
	}
	got := client.Project.GetX(ctx, p.ID)
	if got.Budget != 1250.5 || got.Currency != "USD" {
		t.Fatalf("budget/currency = %v %s", got.Budget, got.Currency)
	}
	// Another tenant's event for the same id changes nothing; a missing project is skipped.
	if err := c.apply(ctx, uuid.New(), p.ID, 9, ""); err != nil {
		t.Fatal(err)
	}
	if err := c.apply(ctx, tenant, uuid.New(), 9, ""); err != nil {
		t.Fatal(err)
	}
	if client.Project.GetX(ctx, p.ID).Budget != 1250.5 {
		t.Fatal("tenant isolation broken")
	}
}

func TestDecodeBudgetApproved(t *testing.T) {
	tenant, project := uuid.New(), uuid.New()
	ok := `{"payload":{"tenant_id":"` + tenant.String() + `","budget_type":"project","project_id":"` + project.String() + `","planned_cost":"1000.00","currency":"KES"}}`
	tid, pid, bac, cur, good := decodeBudgetApproved([]byte(ok))
	if !good || tid != tenant || pid != project || bac != 1000 || cur != "KES" {
		t.Fatalf("decoded %v %v %v %v %v", tid, pid, bac, cur, good)
	}
	for name, body := range map[string]string{
		"operating budget": `{"payload":{"tenant_id":"` + tenant.String() + `","budget_type":"operating","planned_cost":"5"}}`,
		"no project":       `{"payload":{"tenant_id":"` + tenant.String() + `","budget_type":"project","planned_cost":"5"}}`,
		"no cost":          `{"payload":{"tenant_id":"` + tenant.String() + `","budget_type":"project","project_id":"` + project.String() + `"}}`,
		"garbage":          `not json`,
	} {
		if _, _, _, _, good := decodeBudgetApproved([]byte(body)); good {
			t.Errorf("%s: must be ignored", name)
		}
	}
}
