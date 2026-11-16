package tasks

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/testutil"
)

// The recursive CTE walks only from the new target: a chain a -> b -> c refuses c -> a, a cycle
// already in the data (b -> a) terminates, another tenant's edges are never followed, and
// unrelated tasks link freely.
func TestAddDependencyCycleCheck(t *testing.T) {
	client := testutil.Client(t)
	ctx := context.Background()
	tenant := uuid.New()
	p := client.Project.Create().SetTenantID(tenant).SetName("P").SetOwnerID(uuid.New()).SaveX(ctx)
	task := func() uuid.UUID {
		return client.Task.Create().SetTenantID(tenant).SetProjectID(p.ID).SetTitle("t").SaveX(ctx).ID
	}
	a, b, c, d := task(), task(), task(), task()
	svc := NewService(client, nil, zap.NewNop())
	for _, e := range [][2]uuid.UUID{{a, b}, {b, c}} {
		if err := svc.AddDependency(ctx, tenant, p.ID, e[0], e[1], ""); err != nil {
			t.Fatalf("add %v: %v", e, err)
		}
	}
	if err := svc.AddDependency(ctx, tenant, p.ID, c, a, ""); err != ErrCircularDependency {
		t.Errorf("c -> a closes a -> b -> c -> a: got %v", err)
	}
	if err := svc.AddDependency(ctx, tenant, p.ID, a, a, ""); err != ErrCircularDependency {
		t.Errorf("self dependency: got %v", err)
	}
	if err := svc.AddDependency(ctx, tenant, p.ID, d, c, ""); err != nil {
		t.Errorf("d -> c is fine: %v", err)
	}
	// A cycle written straight to the table (older data) must not make the walk loop.
	client.TaskDependency.Create().SetTaskID(b).SetDependsOnTaskID(a).SetDependencyType("FS").SaveX(ctx)
	found, err := func() (bool, error) {
		tx, err := client.Tx(ctx)
		if err != nil {
			return false, err
		}
		defer func() { _ = tx.Rollback() }()
		return reachesInDB(ctx, tx, tenant, a, d)
	}()
	if err != nil || found {
		t.Errorf("a does not reach d despite the b <-> a cycle: found=%v err=%v", found, err)
	}
	// A task of another tenant is not found.
	other := uuid.New()
	op := client.Project.Create().SetTenantID(other).SetName("O").SetOwnerID(uuid.New()).SaveX(ctx)
	ot := client.Task.Create().SetTenantID(other).SetProjectID(op.ID).SetTitle("o").SaveX(ctx).ID
	if err := svc.AddDependency(ctx, tenant, p.ID, a, ot, ""); err != ErrNotFound {
		t.Errorf("cross-tenant dependency: got %v, want ErrNotFound", err)
	}
}

func TestDateAcceptsPlainAndRFC3339(t *testing.T) {
	for in, want := range map[string]string{
		`"2026-10-01"`:           "2026-10-01",
		`"2026-10-01T09:30:00Z"`: "2026-10-01",
		`"2026-10-01T09:30:00"`:  "2026-10-01",
	} {
		var d Date
		if err := d.UnmarshalJSON([]byte(in)); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got := d.Format("2006-01-02"); got != want {
			t.Errorf("%s = %s, want %s", in, got, want)
		}
	}
	var d Date
	if err := d.UnmarshalJSON([]byte(`"01/10/2026"`)); err == nil {
		t.Error("an unknown format must be refused")
	}
	if err := d.UnmarshalJSON([]byte(`""`)); err != nil || !d.IsZero() {
		t.Error("empty is no date")
	}
}
