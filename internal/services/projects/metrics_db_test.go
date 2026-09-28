package projects

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/testutil"
)

func TestGetProjectMetrics(t *testing.T) {
	client := testutil.Client(t)
	ctx := context.Background()
	tenant, other := uuid.New(), uuid.New()
	for _, st := range []string{"active", "active", "completed", "on_hold"} {
		client.Project.Create().SetTenantID(tenant).SetName("P").SetStatus(st).SetOwnerID(uuid.New()).SaveX(ctx)
	}
	client.Project.Create().SetTenantID(other).SetName("X").SetStatus("active").SetOwnerID(uuid.New()).SaveX(ctx)

	m, err := NewService(client, nil, zap.NewNop()).GetProjectMetrics(ctx, tenant)
	if err != nil {
		t.Fatal(err)
	}
	if m.Total != 4 || m.ByStatus["active"] != 2 || m.ByStatus["completed"] != 1 || m.ByStatus["on_hold"] != 1 {
		t.Fatalf("metrics = %+v, want 4 total (2 active, 1 completed, 1 on hold), other tenant excluded", m)
	}
}
