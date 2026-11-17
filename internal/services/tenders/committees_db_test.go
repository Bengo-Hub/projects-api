package tenders

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/testutil"
)

// The committee list names every member from projects_users (one lookup for the whole list), keeps
// the existing member fields, never borrows a name from another tenant and leaves the name empty
// for someone who never signed in.
func TestListCommitteesNamesMembers(t *testing.T) {
	client := testutil.Client(t)
	ctx := context.Background()
	svc := NewService(client, nil, zap.NewNop())
	tenant, other := uuid.New(), uuid.New()
	ana, ben, cy := uuid.New(), uuid.New(), uuid.New()
	client.ProjectsUser.Create().SetTenantID(tenant).SetAuthServiceUserID(ana).SetEmail("ana@acme.test").SetName("Ana Wanjiru").SaveX(ctx)
	client.ProjectsUser.Create().SetTenantID(tenant).SetAuthServiceUserID(ben).SetEmail("ben@acme.test").SaveX(ctx)
	// cy has a local row in another tenant only: no name may leak into this one.
	client.ProjectsUser.Create().SetTenantID(other).SetAuthServiceUserID(cy).SetEmail("cy@other.test").SetName("Cy Elsewhere").SaveX(ctx)

	tender, err := svc.CreateTender(ctx, tenant, CreateTenderInput{Title: "Road", ClientName: "County"})
	if err != nil {
		t.Fatal(err)
	}
	panel, err := svc.CreateCommittee(ctx, tenant, tender.ID, CreateCommitteeInput{Name: "Evaluation panel"})
	if err != nil {
		t.Fatal(err)
	}
	finance, err := svc.CreateCommittee(ctx, tenant, tender.ID, CreateCommitteeInput{Name: "Finance"})
	if err != nil {
		t.Fatal(err)
	}
	for _, add := range []struct {
		committee uuid.UUID
		user      uuid.UUID
		role      string
	}{{panel.ID, ana, "chair"}, {panel.ID, cy, "member"}, {finance.ID, ana, "member"}, {finance.ID, ben, "secretary"}} {
		if _, err := svc.AddCommitteeMember(ctx, tenant, tender.ID, add.committee, AddCommitteeMemberInput{UserID: add.user, Role: add.role}); err != nil {
			t.Fatal(err)
		}
	}

	list, err := svc.ListCommittees(ctx, tenant, tender.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("committees = %d, want 2", len(list))
	}
	got := map[string]map[uuid.UUID][2]string{}
	for _, c := range list {
		got[c.Name] = map[uuid.UUID][2]string{}
		for _, m := range c.Edges.Members {
			got[c.Name][m.UserID] = [2]string{m.UserName, m.Email}
		}
	}
	if got["Evaluation panel"][ana] != [2]string{"Ana Wanjiru", "ana@acme.test"} {
		t.Errorf("ana on the panel: %v", got["Evaluation panel"][ana])
	}
	if got["Evaluation panel"][cy] != [2]string{"", ""} {
		t.Errorf("another tenant's name leaked: %v", got["Evaluation panel"][cy])
	}
	if got["Finance"][ana] != [2]string{"Ana Wanjiru", "ana@acme.test"} || got["Finance"][ben] != [2]string{"", "ben@acme.test"} {
		t.Errorf("finance members: %v", got["Finance"])
	}

	// The JSON keeps the committee and member fields the UI already reads and adds the names.
	raw, err := json.Marshal(map[string]any{"data": list})
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Data []struct {
			ID       uuid.UUID `json:"id"`
			TenderID uuid.UUID `json:"tender_id"`
			Name     string    `json:"name"`
			Edges    struct {
				Members []map[string]any `json:"members"`
			} `json:"edges"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	for _, c := range body.Data {
		if c.ID == uuid.Nil || c.TenderID != tender.ID || c.Name == "" || len(c.Edges.Members) != 2 {
			t.Fatalf("committee JSON: %s", raw)
		}
		for _, m := range c.Edges.Members {
			for _, k := range []string{"id", "committee_id", "user_id", "role", "joined_at", "user_name", "email"} {
				if _, ok := m[k]; !ok {
					t.Fatalf("member JSON has no %q: %s", k, raw)
				}
			}
		}
	}
}
