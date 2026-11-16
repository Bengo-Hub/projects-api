package attachments

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Bengo-Hub/httpware/objectstore"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/testutil"
)

// fakeStore stands in for object storage: Key builds the real layout, and VerifyUpload answers
// what the test put in objects.
type fakeStore struct {
	objects map[string]objectstore.ObjectInfo
	deleted []string
}

func (f *fakeStore) Key(owner objectstore.Owner, mediaType string, parts ...string) (string, error) {
	return strings.Join(append([]string{"projects", owner.TenantID, mediaType}, parts...), "/"), nil
}

func (f *fakeStore) UploadURL(_ context.Context, key, ct string, ttl time.Duration) (*objectstore.DirectUpload, error) {
	return &objectstore.DirectUpload{URL: "https://bucket.test/" + key, Method: "PUT", Headers: map[string]string{"Content-Type": ct}, ExpiresAt: time.Now().Add(ttl)}, nil
}

func (f *fakeStore) VerifyUpload(_ context.Context, key string) (objectstore.ObjectInfo, error) {
	info, ok := f.objects[key]
	if !ok {
		return objectstore.ObjectInfo{}, errors.New("the upload did not arrive")
	}
	return info, nil
}

func (f *fakeStore) URL(ref string) string { return "https://signed.test/" + ref }

func (f *fakeStore) Delete(_ context.Context, ref string) error {
	f.deleted = append(f.deleted, ref)
	return nil
}

func TestUploadConfirmListDelete(t *testing.T) {
	client := testutil.Client(t)
	ctx := context.Background()
	tenant, owner, other := uuid.New(), uuid.New(), uuid.New()
	p := client.Project.Create().SetTenantID(tenant).SetName("P").SetOwnerID(owner).SaveX(ctx)
	q := client.Project.Create().SetTenantID(tenant).SetName("Q").SetOwnerID(owner).SaveX(ctx)
	task := client.Task.Create().SetTenantID(tenant).SetProjectID(p.ID).SetTitle("t").SaveX(ctx)
	store := &fakeStore{objects: map[string]objectstore.ObjectInfo{}}
	svc := NewService(client, store, Limits{MaxBytes: 1000, AllowedTypes: []string{"application/pdf"}, UploadTTL: time.Minute}, zap.NewNop())

	if _, err := svc.RequestUpload(ctx, tenant, p.ID, UploadRequest{FileName: "a.pdf", ContentType: "application/pdf", Size: 5000}); err == nil {
		t.Error("an oversized upload must be refused")
	}
	if _, err := svc.RequestUpload(ctx, tenant, p.ID, UploadRequest{FileName: "a.exe", ContentType: "application/x-msdownload", Size: 10}); err == nil {
		t.Error("a type off the allow-list must be refused")
	}
	ticket, err := svc.RequestUpload(ctx, tenant, p.ID, UploadRequest{FileName: "Plan.PDF", ContentType: "application/pdf", Size: 500, TaskID: &task.ID})
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := "projects/" + tenant.String() + "/attachments/" + p.ID.String() + "/" + task.ID.String() + "/"
	if !strings.HasPrefix(ticket.Key, wantPrefix) || !strings.HasSuffix(ticket.Key, ".pdf") {
		t.Fatalf("key %q not namespaced by tenant, project and task", ticket.Key)
	}
	// The key cannot be confirmed on another project.
	if _, err := svc.Confirm(ctx, tenant, q.ID, owner, ConfirmInput{Key: ticket.Key, FileName: "Plan.pdf"}); err == nil {
		t.Error("confirming another project's key must be refused")
	}
	// The stored bytes decide: a sniffed type off the list is deleted and refused.
	store.objects[ticket.Key] = objectstore.ObjectInfo{Size: 400, ContentType: "text/html"}
	if _, err := svc.Confirm(ctx, tenant, p.ID, owner, ConfirmInput{Key: ticket.Key, FileName: "Plan.pdf", TaskID: &task.ID}); err == nil || len(store.deleted) != 1 {
		t.Errorf("a disguised file must be refused and deleted: %v %v", err, store.deleted)
	}
	store.objects[ticket.Key] = objectstore.ObjectInfo{Size: 400, ContentType: "application/pdf"}
	item, err := svc.Confirm(ctx, tenant, p.ID, owner, ConfirmInput{Key: ticket.Key, FileName: "Plan.pdf", TaskID: &task.ID})
	if err != nil || item.URL == "" || item.FileSize != 400 || item.MimeType != "application/pdf" {
		t.Fatalf("confirm: %+v %v", item, err)
	}
	if again, err := svc.Confirm(ctx, tenant, p.ID, owner, ConfirmInput{Key: ticket.Key, FileName: "Plan.pdf", TaskID: &task.ID}); err != nil || again.ID != item.ID {
		t.Errorf("confirming twice must return the same attachment: %v", err)
	}
	for _, taskID := range []*uuid.UUID{nil, &task.ID} {
		items, total, err := svc.List(ctx, tenant, p.ID, taskID, 20, 0)
		if err != nil || total != 1 || len(items) != 1 || items[0].URL != "https://signed.test/"+ticket.Key {
			t.Errorf("list (task %v): %d %v", taskID, total, err)
		}
	}
	if err := svc.Delete(ctx, tenant, p.ID, item.ID, &other); !errors.Is(err, ErrNotOwner) {
		t.Errorf("deleting someone else's file: %v, want ErrNotOwner", err)
	}
	if err := svc.Delete(ctx, tenant, p.ID, item.ID, &owner); err != nil {
		t.Fatal(err)
	}
	if store.deleted[len(store.deleted)-1] != ticket.Key {
		t.Errorf("stored file not deleted: %v", store.deleted)
	}
}

// Without object storage the upload routes answer ErrUnavailable.
func TestUnavailableWithoutStore(t *testing.T) {
	svc := NewService(nil, nil, Limits{}, zap.NewNop())
	if _, err := svc.RequestUpload(context.Background(), uuid.New(), uuid.New(), UploadRequest{}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("got %v, want ErrUnavailable", err)
	}
}
