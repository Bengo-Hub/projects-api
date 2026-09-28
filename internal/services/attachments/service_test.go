package attachments

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/testutil"
)

func TestValidate(t *testing.T) {
	in := CreateInput{FileURL: " https://drive.example.com/files/Bill%20of%20Quantities.xlsx "}
	if err := in.Validate(); err != nil || in.FileName != "Bill of Quantities.xlsx" {
		t.Fatalf("name from url: %q, %v", in.FileName, err)
	}
	in = CreateInput{FileURL: "https://drive.example.com/", FileName: ""}
	if err := in.Validate(); err != nil || in.FileName != "drive.example.com" {
		t.Fatalf("name from host: %q, %v", in.FileName, err)
	}
	for _, bad := range []string{"", "javascript:alert(1)", "ftp://x/y", "drive.example.com/x", "https://"} {
		in := CreateInput{FileURL: bad}
		if err := in.Validate(); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
	if err := (&CreateInput{FileURL: "https://a.b/c", FileSize: -1}).Validate(); err == nil {
		t.Error("negative size should be refused")
	}
}

func TestAttachments_DB(t *testing.T) {
	client := testutil.PGClient(t)
	ctx := context.Background()
	svc := NewService(client, zap.NewNop())
	tenant, other := uuid.New(), uuid.New()
	p := client.Project.Create().SetTenantID(tenant).SetName("P").SetOwnerID(uuid.New()).SaveX(ctx)
	task := client.Task.Create().SetTenantID(tenant).SetProjectID(p.ID).SetTitle("T").SaveX(ctx)

	onProject, err := svc.Create(ctx, tenant, p.ID, nil, CreateInput{FileURL: "https://x.io/charter.pdf"})
	if err != nil {
		t.Fatal(err)
	}
	onTask, err := svc.Create(ctx, tenant, p.ID, &task.ID, CreateInput{FileURL: "https://x.io/boq.xlsx", FileName: "BoQ"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, other, p.ID, nil, CreateInput{FileURL: "https://x.io/a"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other tenant's project: %v", err)
	}
	wrongProject := uuid.New()
	if _, err := svc.Create(ctx, tenant, wrongProject, &task.ID, CreateInput{FileURL: "https://x.io/a"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("task under another project: %v", err)
	}

	proj, _ := svc.ListByProject(ctx, tenant, p.ID)
	if len(proj) != 1 || proj[0].ID != onProject.ID {
		t.Fatalf("project list = %v", proj)
	}
	tl, _ := svc.ListByTask(ctx, tenant, p.ID, task.ID)
	if len(tl) != 1 || tl[0].ID != onTask.ID || tl[0].FileName != "BoQ" {
		t.Fatalf("task list = %v", tl)
	}
	if _, err := svc.Delete(ctx, other, p.ID, onTask.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete by another tenant: %v", err)
	}
	if a, err := svc.Delete(ctx, tenant, p.ID, onTask.ID); err != nil || a.FileName != "BoQ" {
		t.Fatalf("delete: %v, %v", a, err)
	}
}
