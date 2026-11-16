// Package attachments stores files on tasks and projects in the shared object storage
// (httpware/objectstore, private attachments bucket). The browser uploads straight to the bucket:
// RequestUpload hands out a presigned PUT for a key namespaced by tenant, project and task, and
// Confirm checks the stored file (size and the type sniffed from its bytes) before the attachment
// row is written. Rows keep the object key; every read signs a short-lived download link.
package attachments

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Bengo-Hub/httpware/objectstore"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	entattachment "github.com/bengobox/projects-service/internal/ent/attachment"
	enttask "github.com/bengobox/projects-service/internal/ent/task"
	"github.com/bengobox/projects-service/internal/platform/realtime"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// ErrNotFound is a missing attachment, project or task.
var ErrNotFound = errors.New("not found")

// ErrNotOwner refuses deleting someone else's attachment without comments.manage.
var ErrNotOwner = errors.New("you can only delete your own attachments")

// ErrUnavailable means object storage is not configured for direct uploads (local development).
var ErrUnavailable = errors.New("file uploads are not available right now")

// ValidationError is a refused input, answered 400.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

// Store is the slice of objectstore.Store the service uses.
type Store interface {
	Key(owner objectstore.Owner, mediaType string, parts ...string) (string, error)
	UploadURL(ctx context.Context, key, contentType string, ttl time.Duration) (*objectstore.DirectUpload, error)
	VerifyUpload(ctx context.Context, key string) (objectstore.ObjectInfo, error)
	URL(ref string) string
	Delete(ctx context.Context, ref string) error
}

// Limits are the server-side upload rules (ATTACHMENTS_* in config).
type Limits struct {
	MaxBytes     int64
	AllowedTypes []string
	UploadTTL    time.Duration
}

// Allowed reports whether a content type is on the allow-list (parameters such as charset are
// ignored).
func (l Limits) Allowed(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	for _, a := range l.AllowedTypes {
		if strings.EqualFold(strings.TrimSpace(a), ct) {
			return true
		}
	}
	return false
}

// Service handles attachments.
type Service struct {
	client *ent.Client
	store  Store
	limits Limits
	live   realtime.Publisher
	log    *zap.Logger
}

// NewService creates the attachments service. store may be nil: uploads then answer
// ErrUnavailable and lists carry no links.
func NewService(client *ent.Client, store Store, limits Limits, log *zap.Logger) *Service {
	return &Service{client: client, store: store, limits: limits, log: log.Named("attachments.svc")}
}

// WithRealtime sets where attachment.added hints go after a commit.
func (s *Service) WithRealtime(p realtime.Publisher) *Service {
	s.live = p
	return s
}

// UploadRequest asks for a direct upload link. TaskID attaches the file to a task of the project;
// without it the file belongs to the project.
type UploadRequest struct {
	FileName    string     `json:"file_name"`
	ContentType string     `json:"content_type"`
	Size        int64      `json:"size"`
	TaskID      *uuid.UUID `json:"task_id"`
}

// UploadTicket is the presigned PUT and the key to confirm afterwards.
type UploadTicket struct {
	Key          string                    `json:"key"`
	Upload       *objectstore.DirectUpload `json:"upload"`
	MaxBytes     int64                     `json:"max_bytes"`
	AllowedTypes []string                  `json:"allowed_types"`
}

var extRe = regexp.MustCompile(`^\.[a-z0-9]{1,10}$`)

// scopeSegment is the key segment under the project: the task id, or "project".
func scopeSegment(taskID *uuid.UUID) string {
	if taskID == nil || *taskID == uuid.Nil {
		return "project"
	}
	return taskID.String()
}

// prefix is the key prefix of the project's (or task's) files:
// projects/<tenant>/attachments/<project>/<task|project>/.
func (s *Service) prefix(tenantID, projectID uuid.UUID, taskID *uuid.UUID) (string, error) {
	k, err := s.store.Key(objectstore.Owner{TenantID: tenantID.String()}, objectstore.TypeAttachments,
		projectID.String(), scopeSegment(taskID))
	if err != nil {
		return "", err
	}
	return k + "/", nil
}

// checkScope confirms the project, and the task when given, belong to the tenant.
func (s *Service) checkScope(ctx context.Context, tenantID, projectID uuid.UUID, taskID *uuid.UUID) error {
	if taskID == nil || *taskID == uuid.Nil {
		return nil
	}
	ok, err := s.client.Task.Query().Where(enttask.ID(*taskID), enttask.TenantID(tenantID), enttask.ProjectID(projectID)).Exist(ctx)
	if err != nil {
		return fmt.Errorf("check task: %w", err)
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

func storageErr(err error) error {
	switch {
	case errors.Is(err, objectstore.ErrRemoteOnly), errors.Is(err, objectstore.ErrNotConfigured):
		return ErrUnavailable
	case objectstore.IsUploadError(err):
		return ValidationError(err.Error())
	}
	return err
}

// RequestUpload checks the declared size and type against the limits and returns a presigned PUT
// for a fresh key. The declared values are only a first check: Confirm checks the stored bytes.
func (s *Service) RequestUpload(ctx context.Context, tenantID, projectID uuid.UUID, req UploadRequest) (UploadTicket, error) {
	if s.store == nil {
		return UploadTicket{}, ErrUnavailable
	}
	name := strings.TrimSpace(req.FileName)
	if name == "" || len([]rune(name)) > 255 {
		return UploadTicket{}, ValidationError("file_name is required (at most 255 characters)")
	}
	if req.Size <= 0 {
		return UploadTicket{}, ValidationError("size is required")
	}
	if req.Size > s.limits.MaxBytes {
		return UploadTicket{}, ValidationError(fmt.Sprintf("the file must be at most %d bytes", s.limits.MaxBytes))
	}
	if !s.limits.Allowed(req.ContentType) {
		return UploadTicket{}, ValidationError("this file type is not accepted")
	}
	if err := s.checkScope(ctx, tenantID, projectID, req.TaskID); err != nil {
		return UploadTicket{}, err
	}
	prefix, err := s.prefix(tenantID, projectID, req.TaskID)
	if err != nil {
		return UploadTicket{}, err
	}
	ext := strings.ToLower(filepath.Ext(name))
	if !extRe.MatchString(ext) {
		ext = ""
	}
	key := prefix + uuid.NewString() + ext
	up, err := s.store.UploadURL(ctx, key, req.ContentType, s.limits.UploadTTL)
	if err != nil {
		return UploadTicket{}, storageErr(err)
	}
	return UploadTicket{Key: key, Upload: up, MaxBytes: s.limits.MaxBytes, AllowedTypes: s.limits.AllowedTypes}, nil
}

// ConfirmInput reports a finished upload.
type ConfirmInput struct {
	Key      string     `json:"key"`
	FileName string     `json:"file_name"`
	TaskID   *uuid.UUID `json:"task_id"`
}

// Item is an attachment with a signed download link.
type Item struct {
	*ent.Attachment
	URL string `json:"url"`
}

func (s *Service) item(a *ent.Attachment) Item {
	it := Item{Attachment: a}
	if s.store != nil {
		it.URL = s.store.URL(a.FileURL)
	}
	return it
}

// Confirm checks an uploaded file and records it: the key must be one RequestUpload hands out for
// this project and task, and the stored file must pass the size and sniffed type rules (a file
// that does not is deleted). Confirming the same key again returns the existing attachment.
func (s *Service) Confirm(ctx context.Context, tenantID, projectID, uploader uuid.UUID, in ConfirmInput) (Item, error) {
	if s.store == nil {
		return Item{}, ErrUnavailable
	}
	name := strings.TrimSpace(in.FileName)
	if name == "" || len([]rune(name)) > 255 {
		return Item{}, ValidationError("file_name is required (at most 255 characters)")
	}
	if err := s.checkScope(ctx, tenantID, projectID, in.TaskID); err != nil {
		return Item{}, err
	}
	prefix, err := s.prefix(tenantID, projectID, in.TaskID)
	if err != nil {
		return Item{}, err
	}
	rest := strings.TrimPrefix(in.Key, prefix)
	if !strings.HasPrefix(in.Key, prefix) || rest == "" || strings.Contains(rest, "/") {
		return Item{}, ValidationError("the key does not belong to this project or task")
	}
	if existing, err := s.client.Attachment.Query().
		Where(entattachment.TenantID(tenantID), entattachment.FileURL(in.Key)).Only(ctx); err == nil {
		return s.item(existing), nil
	} else if !ent.IsNotFound(err) {
		return Item{}, fmt.Errorf("check attachment: %w", err)
	}
	info, err := s.store.VerifyUpload(ctx, in.Key)
	if err != nil {
		return Item{}, storageErr(err)
	}
	if info.Size > s.limits.MaxBytes || !s.limits.Allowed(info.ContentType) {
		if derr := s.store.Delete(ctx, in.Key); derr != nil {
			s.log.Warn("rejected upload not deleted", zap.String("key", in.Key), zap.Error(derr))
		}
		return Item{}, ValidationError("the file is too large or of a type that is not accepted")
	}
	var out *ent.Attachment
	err = txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		c := tx.Attachment.Create().
			SetTenantID(tenantID).SetProjectID(projectID).SetFileURL(in.Key).SetFileName(name).
			SetFileSize(info.Size).SetMimeType(info.ContentType).SetUploadedBy(uploader).
			SetMetadata(map[string]any{"storage": "objectstore"})
		if in.TaskID != nil && *in.TaskID != uuid.Nil {
			c = c.SetTaskID(*in.TaskID)
		}
		saved, err := c.Save(ctx)
		if err != nil {
			return fmt.Errorf("save attachment: %w", err)
		}
		out = saved
		e := activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(projectID), EntityType: "attachment", EntityID: out.ID,
			Action: activity.ActionCreated, Changes: map[string]any{"file_name": name, "file_size": info.Size},
		}
		if in.TaskID != nil && *in.TaskID != uuid.Nil {
			e.TaskID = activity.Ref(*in.TaskID)
		}
		return activity.Record(ctx, tx, e)
	})
	if err != nil {
		return Item{}, err
	}
	ev := realtime.Event{Type: realtime.AttachmentAdded, ProjectID: projectID.String(), ID: out.ID.String()}
	if in.TaskID != nil && *in.TaskID != uuid.Nil {
		ev.TaskID = in.TaskID.String()
	}
	if actor := activity.ActorFrom(ctx); actor != uuid.Nil {
		ev.ActorID = actor.String()
	}
	realtime.Emit(s.live, tenantID, ev)
	return s.item(out), nil
}

// List returns one page of the project's attachments (newest first) with download links; taskID
// limits it to one task's files.
func (s *Service) List(ctx context.Context, tenantID, projectID uuid.UUID, taskID *uuid.UUID, limit, offset int) ([]Item, int, error) {
	if limit <= 0 {
		limit = 20
	}
	q := s.client.Attachment.Query().Where(entattachment.TenantID(tenantID), entattachment.ProjectID(projectID))
	if taskID != nil {
		q = q.Where(entattachment.TaskID(*taskID))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count attachments: %w", err)
	}
	rows, err := q.Order(ent.Desc(entattachment.FieldUploadedAt), ent.Desc(entattachment.FieldID)).
		Offset(offset).Limit(limit).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list attachments: %w", err)
	}
	out := make([]Item, len(rows))
	for i, a := range rows {
		out[i] = s.item(a)
	}
	return out, total, nil
}

// Delete removes an attachment of the project; ownerOnly (when set) must be its uploader. The
// stored file is deleted after the commit, best effort.
func (s *Service) Delete(ctx context.Context, tenantID, projectID, id uuid.UUID, ownerOnly *uuid.UUID) error {
	var gone *ent.Attachment
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		a, err := tx.Attachment.Query().
			Where(entattachment.ID(id), entattachment.TenantID(tenantID), entattachment.ProjectID(projectID)).Only(ctx)
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("fetch attachment: %w", err)
		}
		if ownerOnly != nil && a.UploadedBy != *ownerOnly {
			return ErrNotOwner
		}
		if err := tx.Attachment.DeleteOne(a).Exec(ctx); err != nil {
			return fmt.Errorf("delete attachment: %w", err)
		}
		gone = a
		e := activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(projectID), EntityType: "attachment", EntityID: a.ID,
			Action: activity.ActionDeleted, Changes: map[string]any{"file_name": a.FileName},
		}
		if a.TaskID != uuid.Nil {
			e.TaskID = activity.Ref(a.TaskID)
		}
		return activity.Record(ctx, tx, e)
	})
	if err != nil {
		return err
	}
	if s.store != nil && strings.HasPrefix(gone.FileURL, "projects/") {
		if err := s.store.Delete(ctx, gone.FileURL); err != nil {
			s.log.Warn("deleted attachment's file not removed from storage", zap.String("key", gone.FileURL), zap.Error(err))
		}
	}
	return nil
}
