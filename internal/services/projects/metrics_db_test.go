package projects

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	entactivity "github.com/bengobox/projects-service/internal/ent/activity"
	entoutbox "github.com/bengobox/projects-service/internal/ent/outboxevent"
	"github.com/bengobox/projects-service/internal/services/activity"
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

	m, err := NewService(client, nil, zap.NewNop()).GetProjectMetrics(ctx, tenant, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.Total != 4 || m.ByStatus["active"] != 2 || m.ByStatus["completed"] != 1 || m.ByStatus["on_hold"] != 1 {
		t.Fatalf("metrics = %+v, want 4 total (2 active, 1 completed, 1 on hold), other tenant excluded", m)
	}
}

// A user limited to their own projects lists and counts only the ones they own or belong to;
// a member who left no longer sees the project.
func TestMembershipScoping(t *testing.T) {
	client := testutil.Client(t)
	ctx := context.Background()
	tenant, user := uuid.New(), uuid.New()
	owned := client.Project.Create().SetTenantID(tenant).SetName("owned").SetOwnerID(user).SaveX(ctx)
	joined := client.Project.Create().SetTenantID(tenant).SetName("joined").SetOwnerID(uuid.New()).SaveX(ctx)
	left := client.Project.Create().SetTenantID(tenant).SetName("left").SetOwnerID(uuid.New()).SaveX(ctx)
	client.Project.Create().SetTenantID(tenant).SetName("other").SetOwnerID(uuid.New()).SaveX(ctx)
	client.ProjectMember.Create().SetTenantID(tenant).SetProjectID(joined.ID).SetUserID(user).SetRoleCode("member").SaveX(ctx)
	client.ProjectMember.Create().SetTenantID(tenant).SetProjectID(left.ID).SetUserID(user).SetRoleCode("member").
		SetLeftAt(left.CreatedAt).SaveX(ctx)

	svc := NewService(client, nil, zap.NewNop())
	items, total, err := svc.ListProjects(ctx, tenant, ListProjectsFilter{VisibleTo: &user})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("visible projects = %d (total %d), want owned and joined only", len(items), total)
	}
	for _, p := range items {
		if p.ID != owned.ID && p.ID != joined.ID {
			t.Errorf("unexpected project %s", p.Name)
		}
	}
	m, err := svc.GetProjectMetrics(ctx, tenant, &user)
	if err != nil {
		t.Fatal(err)
	}
	if m.Total != 2 {
		t.Errorf("scoped metrics total = %d, want 2", m.Total)
	}
	_, all, err := svc.ListProjects(ctx, tenant, ListProjectsFilter{})
	if err != nil || all != 4 {
		t.Errorf("unscoped total = %d (%v), want 4", all, err)
	}
}

// Create and update record the activity entry and the outbox event in the same transaction as the
// change, with the actor from the request context.
func TestProjectWritesRecordActivityAndEvents(t *testing.T) {
	client := testutil.Client(t)
	tenant, actor := uuid.New(), uuid.New()
	ctx := activity.WithActor(context.Background(), actor)
	svc := NewService(client, nil, zap.NewNop())

	p, err := svc.CreateProject(ctx, tenant, CreateProjectInput{Name: "Bridge", OwnerID: actor})
	if err != nil {
		t.Fatal(err)
	}
	status := "completed"
	if _, err := svc.UpdateProject(ctx, tenant, p.ID, UpdateProjectInput{Status: &status}); err != nil {
		t.Fatal(err)
	}
	rows := client.Activity.Query().Where(entactivity.TenantID(tenant), entactivity.ProjectID(p.ID)).AllX(ctx)
	types := map[string]bool{}
	for _, a := range rows {
		types[a.ActivityType] = true
		if a.UserID != actor {
			t.Errorf("%s recorded actor %s, want %s", a.ActivityType, a.UserID, actor)
		}
	}
	if !types["project.created"] || !types["project.updated"] {
		t.Fatalf("activity types = %v, want project.created and project.updated", types)
	}
	events := client.OutboxEvent.Query().Where(entoutbox.TenantID(tenant), entoutbox.AggregateID(p.ID)).AllX(ctx)
	got := map[string]bool{}
	for _, e := range events {
		got[e.EventType] = true
		if e.Status != "PENDING" {
			t.Errorf("event %s status %s, want PENDING", e.EventType, e.Status)
		}
	}
	for _, want := range []string{EventCreated, EventUpdated, EventClosed} {
		if !got[want] {
			t.Errorf("missing outbox event project.%s (have %v)", want, got)
		}
	}

	// A failed update writes neither the change nor its activity or event.
	missing := uuid.New()
	if _, err := svc.UpdateProject(ctx, tenant, missing, UpdateProjectInput{Status: &status}); err != ErrNotFound {
		t.Fatalf("update of a missing project = %v, want ErrNotFound", err)
	}
	if n := client.Activity.Query().Where(entactivity.ProjectID(missing)).CountX(ctx); n != 0 {
		t.Errorf("activity written for a failed update: %d", n)
	}
}
