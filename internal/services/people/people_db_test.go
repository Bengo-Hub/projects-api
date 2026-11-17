package people

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	entuser "github.com/bengobox/projects-service/internal/ent/projectsuser"
	"github.com/bengobox/projects-service/internal/modules/rbac"
	"github.com/bengobox/projects-service/internal/testutil"
)

// The week page's person picker lists the tenant's signed-in staff: never portal clients, never
// inactive users, never another tenant; q matches name or email in any case; pages hold.
func TestSearchListsStaffOnly(t *testing.T) {
	client := testutil.Client(t)
	ctx := context.Background()
	roles := rbac.NewService(client, nil, zap.NewNop())
	if err := roles.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	tenant, other := uuid.New(), uuid.New()
	signIn := func(tenantID uuid.UUID, name, email string, global ...string) uuid.UUID {
		id := uuid.New()
		if err := roles.EnsureUserFromToken(ctx, rbac.Identity{TenantID: tenantID, AuthUserID: id, Email: email, Name: name, GlobalRoles: global}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	amina := signIn(tenant, "Amina Otieno", "amina@acme.test", "staff")
	brian := signIn(tenant, "Brian Kamau", "brian@acme.test", "manager")
	signIn(tenant, "Joan Client", "joan@client.test", "client")
	gone := signIn(tenant, "Old Hand", "old@acme.test", "staff")
	signIn(other, "Amina Elsewhere", "amina@other.test", "staff")
	client.ProjectsUser.Update().Where(entuser.AuthServiceUserID(gone)).SetStatus("inactive").ExecX(ctx)

	list, total, err := Search(ctx, client.ProjectsUser, tenant, "", 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(list) != 2 || list[0].ID != amina || list[1].ID != brian {
		t.Fatalf("staff = %+v (total %d), want Amina then Brian only", list, total)
	}
	if found, n, _ := Search(ctx, client.ProjectsUser, tenant, "BRIAN@", 20, 0); n != 1 || found[0].ID != brian {
		t.Fatalf("email search = %+v %d", found, n)
	}
	if _, n, _ := Search(ctx, client.ProjectsUser, tenant, "joan", 20, 0); n != 0 {
		t.Fatal("a portal client must never be listed")
	}
	if page, n, _ := Search(ctx, client.ProjectsUser, tenant, "", 1, 1); n != 2 || len(page) != 1 || page[0].ID != brian {
		t.Fatalf("second page = %+v (total %d)", page, n)
	}
}
