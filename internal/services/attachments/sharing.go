package attachments

import (
	"context"
	"fmt"
	"maps"
	"time"

	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	entattachment "github.com/bengobox/projects-service/internal/ent/attachment"
	"github.com/bengobox/projects-service/internal/ent/predicate"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// MetaClientVisible is the attachment metadata key that shares a file with the project's clients
// in the portal. Only SetClientVisible writes it (the sharing route, projects.portal.manage).
const MetaClientVisible = "client_visible"

// clientVisible matches attachments whose metadata shares them: metadata -> 'client_visible' = true.
func clientVisible() predicate.Attachment {
	return predicate.Attachment(func(s *sql.Selector) {
		s.Where(sqljson.ValueEQ(s.C(entattachment.FieldMetadata), true, sqljson.Path(MetaClientVisible)))
	})
}

// SetClientVisible shares the attachment with the project's clients (visible true) or stops
// sharing it, answering the attachment with its download link. Attachment metadata is written
// nowhere else after the upload, so no row lock is needed.
func (s *Service) SetClientVisible(ctx context.Context, tenantID, projectID, id uuid.UUID, visible bool) (Item, error) {
	var out *ent.Attachment
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		a, err := tx.Attachment.Query().
			Where(entattachment.ID(id), entattachment.TenantID(tenantID), entattachment.ProjectID(projectID)).
			Only(ctx)
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("fetch attachment: %w", err)
		}
		if shared, _ := a.Metadata[MetaClientVisible].(bool); shared == visible {
			out = a
			return nil
		}
		meta := make(map[string]any, len(a.Metadata)+1)
		maps.Copy(meta, a.Metadata)
		if visible {
			meta[MetaClientVisible] = true
		} else {
			delete(meta, MetaClientVisible)
		}
		if out, err = a.Update().SetMetadata(meta).Save(ctx); err != nil {
			return fmt.Errorf("share attachment: %w", err)
		}
		e := activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(projectID), EntityType: "attachment", EntityID: a.ID,
			Action: activity.ActionUpdated, Changes: map[string]any{MetaClientVisible: visible},
		}
		if a.TaskID != uuid.Nil {
			e.TaskID = activity.Ref(a.TaskID)
		}
		return activity.Record(ctx, tx, e)
	})
	if err != nil {
		return Item{}, err
	}
	return s.item(out), nil
}

// SharedFile is what a client sees of a shared file: no storage key, uploader or metadata.
type SharedFile struct {
	ID         uuid.UUID  `json:"id"`
	FileName   string     `json:"file_name"`
	FileSize   int64      `json:"file_size"`
	MimeType   string     `json:"mime_type"`
	UploadedAt time.Time  `json:"uploaded_at"`
	TaskID     *uuid.UUID `json:"task_id"`
	URL        string     `json:"url"`
}

// ListShared returns one page of the project's files shared with its clients, newest first, each
// with a signed download link (15 minutes).
func (s *Service) ListShared(ctx context.Context, tenantID, projectID uuid.UUID, limit, offset int) ([]SharedFile, int, error) {
	if limit <= 0 {
		limit = 20
	}
	q := s.client.Attachment.Query().Where(entattachment.TenantID(tenantID), entattachment.ProjectID(projectID), clientVisible())
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count shared files: %w", err)
	}
	rows, err := q.Order(ent.Desc(entattachment.FieldUploadedAt), ent.Desc(entattachment.FieldID)).
		Offset(offset).Limit(limit).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list shared files: %w", err)
	}
	out := make([]SharedFile, len(rows))
	for i, a := range rows {
		f := SharedFile{ID: a.ID, FileName: a.FileName, FileSize: a.FileSize, MimeType: a.MimeType, UploadedAt: a.UploadedAt}
		if a.TaskID != uuid.Nil {
			f.TaskID = activity.Ref(a.TaskID)
		}
		if s.store != nil {
			f.URL = s.store.URL(a.FileURL)
		}
		out[i] = f
	}
	return out, total, nil
}
