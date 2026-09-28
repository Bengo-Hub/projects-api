// Package attachments keeps links to files on projects and tasks. The file itself lives in the
// tenant's drive or document store; the platform has no upload storage yet.
package attachments

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	entattachment "github.com/bengobox/projects-service/internal/ent/attachment"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	enttask "github.com/bengobox/projects-service/internal/ent/task"
)

// ErrNotFound is returned when the project, task or attachment is not the tenant's.
var ErrNotFound = errors.New("not found")

// ValidationError is a bad request.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

// listLimit bounds one list read.
const listLimit = 500

// Service manages attachments.
type Service struct {
	client *ent.Client
	log    *zap.Logger
}

// NewService builds the attachments service.
func NewService(client *ent.Client, log *zap.Logger) *Service {
	return &Service{client: client, log: log.Named("attachments.svc")}
}

// CreateInput is a link to a file.
type CreateInput struct {
	FileURL    string    `json:"file_url"`
	FileName   string    `json:"file_name"`
	FileSize   int64     `json:"file_size"`
	MimeType   string    `json:"mime_type"`
	UploadedBy uuid.UUID `json:"-"`
}

// Validate checks the link is http(s) and fills the name from the URL when missing.
func (in *CreateInput) Validate() error {
	in.FileURL = strings.TrimSpace(in.FileURL)
	u, err := url.Parse(in.FileURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return ValidationError("file_url must be an http or https link")
	}
	in.FileName = strings.TrimSpace(in.FileName)
	if in.FileName == "" {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if last := parts[len(parts)-1]; last != "" {
			if dec, err := url.PathUnescape(last); err == nil {
				in.FileName = dec
			} else {
				in.FileName = last
			}
		} else {
			in.FileName = u.Host
		}
	}
	if in.FileSize < 0 {
		return ValidationError("file_size cannot be negative")
	}
	return nil
}

func (s *Service) checkProject(ctx context.Context, tenantID, projectID uuid.UUID) error {
	ok, err := s.client.Project.Query().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).Exist(ctx)
	if err != nil {
		return fmt.Errorf("check project: %w", err)
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

func (s *Service) checkTask(ctx context.Context, tenantID, projectID, taskID uuid.UUID) error {
	ok, err := s.client.Task.Query().
		Where(enttask.ID(taskID), enttask.TenantID(tenantID), enttask.ProjectID(projectID)).Exist(ctx)
	if err != nil {
		return fmt.Errorf("check task: %w", err)
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// Create adds an attachment to the project, or to the task when taskID is set.
func (s *Service) Create(ctx context.Context, tenantID, projectID uuid.UUID, taskID *uuid.UUID, in CreateInput) (*ent.Attachment, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	if taskID != nil {
		if err := s.checkTask(ctx, tenantID, projectID, *taskID); err != nil {
			return nil, err
		}
	} else if err := s.checkProject(ctx, tenantID, projectID); err != nil {
		return nil, err
	}
	c := s.client.Attachment.Create().
		SetTenantID(tenantID).
		SetProjectID(projectID).
		SetNillableTaskID(taskID).
		SetFileURL(in.FileURL).
		SetFileName(in.FileName).
		SetFileSize(in.FileSize).
		SetUploadedBy(in.UploadedBy)
	if in.MimeType != "" {
		c = c.SetMimeType(in.MimeType)
	}
	a, err := c.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create attachment: %w", err)
	}
	return a, nil
}

// ListByProject returns the project's own attachments (not its tasks'), newest first.
func (s *Service) ListByProject(ctx context.Context, tenantID, projectID uuid.UUID) ([]*ent.Attachment, error) {
	items, err := s.client.Attachment.Query().
		Where(entattachment.TenantID(tenantID), entattachment.ProjectID(projectID), entattachment.TaskIDIsNil()).
		Order(ent.Desc(entattachment.FieldUploadedAt)).
		Limit(listLimit).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	return items, nil
}

// ListByTask returns a task's attachments, newest first.
func (s *Service) ListByTask(ctx context.Context, tenantID, projectID, taskID uuid.UUID) ([]*ent.Attachment, error) {
	items, err := s.client.Attachment.Query().
		Where(entattachment.TenantID(tenantID), entattachment.ProjectID(projectID), entattachment.TaskID(taskID)).
		Order(ent.Desc(entattachment.FieldUploadedAt)).
		Limit(listLimit).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list task attachments: %w", err)
	}
	return items, nil
}

// Delete removes an attachment of the project (or of one of its tasks) and returns it.
func (s *Service) Delete(ctx context.Context, tenantID, projectID, id uuid.UUID) (*ent.Attachment, error) {
	a, err := s.client.Attachment.Query().
		Where(entattachment.ID(id), entattachment.TenantID(tenantID), entattachment.ProjectID(projectID)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("fetch attachment: %w", err)
	}
	if err := s.client.Attachment.DeleteOne(a).Exec(ctx); err != nil {
		return nil, fmt.Errorf("delete attachment: %w", err)
	}
	return a, nil
}
