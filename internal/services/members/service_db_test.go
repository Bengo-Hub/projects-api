package members

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/testutil"
)

// The member list carries each member's name and email from projects_users (empty for someone
// who never signed in), pages, and searches name or email case-insensitively with wildcards
// taken literally, never matching a user of another tenant or project.
func TestListMembersNamesAndSearch(t *testing.T) {
	client := testutil.Client(t)
	ctx := context.Background()
	tenant, other := uuid.New(), uuid.New()
	ana, ben, cy := uuid.New(), uuid.New(), uuid.New()
	p := client.Project.Create().SetTenantID(tenant).SetName("Bridge").SetOwnerID(ana).SaveX(ctx)
	elsewhere := client.Project.Create().SetTenantID(tenant).SetName("Road").SetOwnerID(ana).SaveX(ctx)
	for _, u := range []uuid.UUID{ana, ben, cy} {
		client.ProjectMember.Create().SetTenantID(tenant).SetProjectID(p.ID).SetUserID(u).SetRoleCode("member").SaveX(ctx)
	}
	client.ProjectMember.Create().SetTenantID(tenant).SetProjectID(elsewhere.ID).SetUserID(uuid.New()).SetRoleCode("member").SaveX(ctx)
	client.ProjectsUser.Create().SetTenantID(tenant).SetAuthServiceUserID(ana).SetEmail("ana@acme.test").SetName("Ana Wanjiru").SaveX(ctx)
	client.ProjectsUser.Create().SetTenantID(tenant).SetAuthServiceUserID(ben).SetEmail("ben_o@acme.test").SetName("Ben Otieno").SaveX(ctx)
	// The same person in another tenant must never leak a name into this one.
	client.ProjectsUser.Create().SetTenantID(other).SetAuthServiceUserID(cy).SetEmail("cy@other.test").SetName("Cy Elsewhere").SaveX(ctx)
	svc := NewService(client, nil, zap.NewNop())

	all, total, err := svc.ListMembers(ctx, tenant, p.ID, ListMembersFilter{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(all) != 3 {
		t.Fatalf("members = %d of %d, want 3", len(all), total)
	}
	byID := map[uuid.UUID]Member{}
	for _, m := range all {
		byID[m.UserID] = m
	}
	if byID[ana].Name != "Ana Wanjiru" || byID[ana].Email != "ana@acme.test" || byID[ben].Name != "Ben Otieno" {
		t.Errorf("names = %+v", byID)
	}
	if byID[cy].Name != "" || byID[cy].Email != "" {
		t.Errorf("cy has no row in this tenant, got %q %q", byID[cy].Name, byID[cy].Email)
	}

	for term, want := range map[string]int{"ANA": 1, "acme.test": 2, "otieno": 1, "elsewhere": 0, "_": 1, "%": 0} {
		got, n, err := svc.ListMembers(ctx, tenant, p.ID, ListMembersFilter{Search: term, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		if n != want || len(got) != want {
			t.Errorf("search %q: %d rows, total %d, want %d", term, len(got), n, want)
		}
	}

	page, total, err := svc.ListMembers(ctx, tenant, p.ID, ListMembersFilter{Limit: 2, Offset: 2})
	if err != nil || total != 3 || len(page) != 1 {
		t.Errorf("second page: %d rows of %d, %v", len(page), total, err)
	}
}
