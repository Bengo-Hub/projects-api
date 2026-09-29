package projects

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/testutil"
)

func TestProjectCommercialMetadata(t *testing.T) {
	client := testutil.Client(t)
	ctx := context.Background()
	svc := NewService(client, nil, zap.NewNop())
	tenant := uuid.New()
	contact, cc := uuid.NewString(), uuid.NewString()

	p, err := svc.CreateProject(ctx, tenant, CreateProjectInput{Name: "Road", OwnerID: uuid.New(), Metadata: map[string]any{
		"billing_type": "fixed", "contract_value": "250000", "crm_contact_id": contact, "crm_contact_name": "Acme",
		"cost_center_id": cc, "cost_center_name": "Nairobi", "site": "Thika",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Metadata["contract_value"] != 250000.0 {
		t.Fatalf("contract_value stored as %#v, want the number 250000", p.Metadata["contract_value"])
	}

	// A partial update keeps keys it does not mention and removes those sent as null.
	up, err := svc.UpdateProject(ctx, tenant, p.ID, UpdateProjectInput{Metadata: map[string]any{"contract_value": 300000.0, "cost_center_id": nil, "cost_center_name": nil}})
	if err != nil {
		t.Fatal(err)
	}
	if up.Metadata["contract_value"] != 300000.0 || up.Metadata["site"] != "Thika" || up.Metadata["crm_contact_name"] != "Acme" {
		t.Errorf("merge lost keys: %#v", up.Metadata)
	}
	if _, ok := up.Metadata["cost_center_id"]; ok {
		t.Errorf("null should remove cost_center_id: %#v", up.Metadata)
	}
	// Clearing an id with "" also clears its display name.
	up, err = svc.UpdateProject(ctx, tenant, p.ID, UpdateProjectInput{Metadata: map[string]any{"crm_contact_id": ""}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := up.Metadata["crm_contact_name"]; ok {
		t.Errorf("clearing the contact keeps its name: %#v", up.Metadata)
	}

	for _, bad := range []map[string]any{
		{"billing_type": "barter"},
		{"contract_value": -1.0},
		{"contract_value": "lots"},
		{"cost_center_id": "not-a-uuid"},
	} {
		_, err := svc.UpdateProject(ctx, tenant, p.ID, UpdateProjectInput{Metadata: bad})
		var verr ValidationError
		if !errors.As(err, &verr) {
			t.Errorf("%v: want a validation error, got %v", bad, err)
		}
	}
}
