package comments

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	entcomment "github.com/bengobox/projects-service/internal/ent/comment"
	entoutbox "github.com/bengobox/projects-service/internal/ent/outboxevent"
	"github.com/bengobox/projects-service/internal/services/notify"
	"github.com/bengobox/projects-service/internal/testutil"
)

// A comment replayed with the same Idempotency-Key answers the first comment: one row, one
// mention notice, even when the replays race. The key belongs to its author (another author may
// use the same key), and reusing it on another task is refused. Responses carry the author name.
func TestCreateCommentIdempotent(t *testing.T) {
	client := testutil.Client(t)
	ctx := context.Background()
	tenant, author, ana := uuid.New(), uuid.New(), uuid.New()
	p := client.Project.Create().SetTenantID(tenant).SetName("Bridge").SetOwnerID(author).SaveX(ctx)
	client.ProjectMember.Create().SetTenantID(tenant).SetProjectID(p.ID).SetUserID(ana).SetRoleCode("member").SaveX(ctx)
	client.ProjectsUser.Create().SetTenantID(tenant).SetAuthServiceUserID(author).SetEmail("own@acme.test").SetName("Olive").SaveX(ctx)
	task := client.Task.Create().SetTenantID(tenant).SetProjectID(p.ID).SetTitle("Pour slab").SaveX(ctx)
	other := client.Task.Create().SetTenantID(tenant).SetProjectID(p.ID).SetTitle("Cure").SaveX(ctx)
	svc := NewService(client, nil, zap.NewNop())
	in := CreateCommentInput{UserID: author, Content: `<p>` + mention(ana, "Ana") + ` ready</p>`, IdempotencyKey: "k-1"}

	first, replayed, err := svc.CreateTaskComment(ctx, tenant, p.ID, task.ID, in)
	if err != nil || replayed {
		t.Fatalf("first: replayed=%v %v", replayed, err)
	}
	if first.AuthorName != "Olive" {
		t.Errorf("author_name = %q, want Olive", first.AuthorName)
	}

	again, replayed, err := svc.CreateTaskComment(ctx, tenant, p.ID, task.ID, in)
	if err != nil || !replayed || again.ID != first.ID {
		t.Fatalf("replay: %v replayed=%v %v", again, replayed, err)
	}

	// Four replays of a new key racing on separate connections still make one comment.
	race := in
	race.IdempotencyKey = "k-race"
	var wg sync.WaitGroup
	ids := make([]uuid.UUID, 4)
	fresh := make([]bool, 4)
	errs := make([]error, 4)
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, rep, err := svc.CreateTaskComment(ctx, tenant, p.ID, task.ID, race)
			if c != nil {
				ids[i] = c.ID
			}
			fresh[i], errs[i] = !rep, err
		}(i)
	}
	wg.Wait()
	created := 0
	for i := range ids {
		if errs[i] != nil || ids[i] != ids[0] {
			t.Errorf("racing replay %d: %v %v, want one shared comment", i, ids[i], errs[i])
		}
		if fresh[i] {
			created++
		}
	}
	if created != 1 {
		t.Errorf("racing replays created %d comments, want 1", created)
	}
	if n := client.Comment.Query().Where(entcomment.TaskID(task.ID)).CountX(ctx); n != 2 {
		t.Errorf("comments = %d, want 2", n)
	}
	if n := client.OutboxEvent.Query().Where(entoutbox.EventType(notify.EventTaskMentioned)).CountX(ctx); n != 2 {
		t.Errorf("mention notices = %d, want 2 (one per key)", n)
	}

	if _, _, err := svc.CreateTaskComment(ctx, tenant, p.ID, other.ID, in); !errors.Is(err, ErrKeyReused) {
		t.Errorf("same key on another task: %v, want ErrKeyReused", err)
	}
	if _, _, err := svc.CreateProjectComment(ctx, tenant, p.ID, in); !errors.Is(err, ErrKeyReused) {
		t.Errorf("same key on the project: %v, want ErrKeyReused", err)
	}
	// Another author's key is their own.
	c, replayed, err := svc.CreateTaskComment(ctx, tenant, p.ID, task.ID, CreateCommentInput{UserID: ana, Content: "<p>ok</p>", IdempotencyKey: "k-1"})
	if err != nil || replayed || c.ID == first.ID || c.AuthorName != "" {
		t.Errorf("ana with the same key: %+v replayed=%v %v", c, replayed, err)
	}
	// A client cannot plant a key through metadata.
	planted, _, err := svc.CreateTaskComment(ctx, tenant, p.ID, task.ID, CreateCommentInput{UserID: author, Content: "<p>x</p>", Metadata: map[string]any{metaIdempotencyKey: "k-2"}})
	if err != nil || planted.Metadata[metaIdempotencyKey] != nil {
		t.Fatalf("planted key kept: %v %v", planted.Metadata, err)
	}

	page, total, err := svc.ListCommentsByTask(ctx, tenant, p.ID, task.ID, 50, 0)
	if err != nil || total != 4 || page[0].AuthorName != "Olive" {
		t.Errorf("list: %d %v %v", total, page, err)
	}
}
