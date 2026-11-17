package tenders

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Bengo-Hub/httpware/objectstore"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	entattachment "github.com/bengobox/projects-service/internal/ent/attachment"
	enttenderdoc "github.com/bengobox/projects-service/internal/ent/tenderdocument"
	enttendersection "github.com/bengobox/projects-service/internal/ent/tendersection"
	enttendersubmission "github.com/bengobox/projects-service/internal/ent/tendersubmission"
)

// Tender documents live in the shared object storage (httpware/objectstore, the private
// attachments bucket), the same way as project attachments: the browser asks for a presigned PUT
// (RequestUpload), uploads straight to the bucket, then confirms (ConfirmUpload), which checks the
// stored bytes before the row is written. Keys are
// projects/<tenant>/attachments/tenders/<tender>/<uuid><ext>, and every read signs a short-lived
// link. A document can also be a link to a file kept elsewhere (an https URL), as before.

// ErrUnavailable means object storage is not configured for direct uploads (local development).
var ErrUnavailable = errors.New("file uploads are not available right now")

// Store is the slice of objectstore.Store the tender documents use (the attachments service uses
// the same one).
type Store interface {
	Key(owner objectstore.Owner, mediaType string, parts ...string) (string, error)
	UploadURL(ctx context.Context, key, contentType string, ttl time.Duration) (*objectstore.DirectUpload, error)
	VerifyUpload(ctx context.Context, key string) (objectstore.ObjectInfo, error)
	URL(ref string) string
	Delete(ctx context.Context, ref string) error
}

// Limits are the server-side upload rules (ATTACHMENTS_* in config, shared with attachments).
type Limits struct {
	MaxBytes     int64
	AllowedTypes []string
	UploadTTL    time.Duration
}

func (l Limits) allowed(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	for _, a := range l.AllowedTypes {
		if strings.EqualFold(strings.TrimSpace(a), ct) {
			return true
		}
	}
	return false
}

// WithStorage sets where tender documents are stored (nil store: uploads answer ErrUnavailable and
// stored documents carry no signed link).
func (s *Service) WithStorage(store Store, limits Limits) *Service {
	s.store, s.limits = store, limits
	return s
}

// storedKey reports whether a document's file_url is an object storage key of this service (and
// not a link to a file kept elsewhere).
func storedKey(fileURL string) bool { return strings.HasPrefix(fileURL, "projects/") }

// prefix is the key prefix of a tender's files: projects/<tenant>/attachments/tenders/<tender>/.
func (s *Service) prefix(tenantID, tenderID uuid.UUID) (string, error) {
	k, err := s.store.Key(objectstore.Owner{TenantID: tenantID.String()}, objectstore.TypeAttachments, "tenders", tenderID.String())
	if err != nil {
		return "", err
	}
	return k + "/", nil
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

// UploadRequest asks for a direct upload link for a tender document.
type UploadRequest struct {
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

// UploadTicket is the presigned PUT and the key to confirm afterwards.
type UploadTicket struct {
	Key          string                    `json:"key"`
	Upload       *objectstore.DirectUpload `json:"upload"`
	MaxBytes     int64                     `json:"max_bytes"`
	AllowedTypes []string                  `json:"allowed_types"`
}

var extRe = regexp.MustCompile(`^\.[a-z0-9]{1,10}$`)

func checkFileName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 255 {
		return "", ErrValidation("file_name is required (at most 255 characters)")
	}
	return name, nil
}

// RequestUpload checks the declared size and type and returns a presigned PUT for a fresh key
// under the tender. ConfirmUpload checks the stored bytes.
func (s *Service) RequestUpload(ctx context.Context, tenantID, tenderID uuid.UUID, req UploadRequest) (UploadTicket, error) {
	if s.store == nil {
		return UploadTicket{}, ErrUnavailable
	}
	name, err := checkFileName(req.FileName)
	if err != nil {
		return UploadTicket{}, err
	}
	if req.Size <= 0 {
		return UploadTicket{}, ErrValidation("size is required")
	}
	if req.Size > s.limits.MaxBytes {
		return UploadTicket{}, ErrValidation(fmt.Sprintf("the file must be at most %d bytes", s.limits.MaxBytes))
	}
	if !s.limits.allowed(req.ContentType) {
		return UploadTicket{}, ErrValidation("this file type is not accepted")
	}
	if err := s.tenderExists(ctx, tenantID, tenderID); err != nil {
		return UploadTicket{}, err
	}
	prefix, err := s.prefix(tenantID, tenderID)
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

func (s *Service) tenderExists(ctx context.Context, tenantID, tenderID uuid.UUID) error {
	ok, err := s.client.Tender.Query().Where(tenderOf(tenantID, tenderID)...).Exist(ctx)
	if err != nil {
		return fmt.Errorf("fetch tender: %w", err)
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// AddDocumentInput adds a document: Key (after RequestUpload and the PUT) for a stored file, or
// FileURL for a link to a file kept elsewhere (https only).
type AddDocumentInput struct {
	Key        string    `json:"key"`
	FileURL    string    `json:"file_url"`
	FileName   string    `json:"file_name"`
	FileSize   *int64    `json:"file_size"`
	MimeType   string    `json:"mime_type"`
	UploadedBy uuid.UUID `json:"-"`
}

// Document is a tender document with its download link (signed for a stored file).
type Document struct {
	*ent.TenderDocument
	URL    string `json:"url"`
	Stored bool   `json:"stored"`
}

func (s *Service) document(d *ent.TenderDocument) Document {
	out := Document{TenderDocument: d, URL: d.FileURL, Stored: storedKey(d.FileURL)}
	if out.Stored {
		out.URL = ""
		if s.store != nil {
			out.URL = s.store.URL(d.FileURL)
		}
	}
	return out
}

// AddDocument records a tender document: a confirmed upload (Key) or an https link (FileURL).
// Confirming the same key again returns the existing document.
func (s *Service) AddDocument(ctx context.Context, tenantID, tenderID uuid.UUID, in AddDocumentInput) (Document, error) {
	name, err := checkFileName(in.FileName)
	if err != nil {
		return Document{}, err
	}
	in.FileName = name
	if strings.TrimSpace(in.Key) != "" {
		return s.confirmUpload(ctx, tenantID, tenderID, in)
	}
	u, err := url.Parse(strings.TrimSpace(in.FileURL))
	if err != nil || u.Scheme != "https" || u.Host == "" || len(in.FileURL) > 2000 {
		return Document{}, ErrValidation("upload the file, or give an https link to it")
	}
	in.FileURL = u.String()
	return s.saveDocument(ctx, tenantID, tenderID, in)
}

func (s *Service) confirmUpload(ctx context.Context, tenantID, tenderID uuid.UUID, in AddDocumentInput) (Document, error) {
	if s.store == nil {
		return Document{}, ErrUnavailable
	}
	if err := s.tenderExists(ctx, tenantID, tenderID); err != nil {
		return Document{}, err
	}
	prefix, err := s.prefix(tenantID, tenderID)
	if err != nil {
		return Document{}, err
	}
	rest := strings.TrimPrefix(in.Key, prefix)
	if !strings.HasPrefix(in.Key, prefix) || rest == "" || strings.Contains(rest, "/") {
		return Document{}, ErrValidation("the key does not belong to this tender")
	}
	if existing, err := s.client.TenderDocument.Query().
		Where(enttenderdoc.TenantID(tenantID), enttenderdoc.TenderID(tenderID), enttenderdoc.FileURL(in.Key)).Only(ctx); err == nil {
		return s.document(existing), nil
	} else if !ent.IsNotFound(err) {
		return Document{}, fmt.Errorf("check document: %w", err)
	}
	info, err := s.store.VerifyUpload(ctx, in.Key)
	if err != nil {
		return Document{}, storageErr(err)
	}
	if info.Size > s.limits.MaxBytes || !s.limits.allowed(info.ContentType) {
		if derr := s.store.Delete(ctx, in.Key); derr != nil {
			s.log.Warn("rejected tender upload not deleted", zap.String("key", in.Key), zap.Error(derr))
		}
		return Document{}, ErrValidation("the file is too large or of a type that is not accepted")
	}
	size := info.Size
	in.FileURL, in.FileSize, in.MimeType = in.Key, &size, info.ContentType
	return s.saveDocument(ctx, tenantID, tenderID, in)
}

func (s *Service) saveDocument(ctx context.Context, tenantID, tenderID uuid.UUID, in AddDocumentInput) (Document, error) {
	var doc *ent.TenderDocument
	err := s.onTender(ctx, tenantID, tenderID, func(tx *ent.Tx) (map[string]any, error) {
		c := tx.TenderDocument.Create().SetTenderID(tenderID).SetTenantID(tenantID).
			SetFileURL(in.FileURL).SetFileName(in.FileName).SetUploadedBy(in.UploadedBy)
		if in.FileSize != nil {
			c = c.SetFileSize(*in.FileSize)
		}
		if in.MimeType != "" {
			c = c.SetMimeType(in.MimeType)
		}
		var err error
		if doc, err = c.Save(ctx); err != nil {
			return nil, fmt.Errorf("add document: %w", err)
		}
		return map[string]any{"document_added": doc.FileName}, nil
	})
	if err != nil {
		return Document{}, err
	}
	return s.document(doc), nil
}

// ListDocuments lists a tender's documents, newest first, with their links.
func (s *Service) ListDocuments(ctx context.Context, tenantID, tenderID uuid.UUID) ([]Document, error) {
	items, err := s.client.TenderDocument.Query().
		Where(enttenderdoc.TenderID(tenderID), enttenderdoc.TenantID(tenantID)).
		Order(ent.Desc(enttenderdoc.FieldUploadedAt)).
		Limit(subListLimit).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	out := make([]Document, len(items))
	for i, d := range items {
		out[i] = s.document(d)
	}
	return out, nil
}

// documentOf checks a document belongs to the tender (inside tx).
func documentOf(ctx context.Context, tx *ent.Tx, tenantID, tenderID, docID uuid.UUID) error {
	ok, err := tx.TenderDocument.Query().Where(enttenderdoc.ID(docID), enttenderdoc.TenderID(tenderID), enttenderdoc.TenantID(tenantID)).Exist(ctx)
	if err != nil {
		return fmt.Errorf("fetch document: %w", err)
	}
	if !ok {
		return ErrValidation("the document is not one of this tender's documents")
	}
	return nil
}

// DeleteDocument removes a document from the tender. A document a submission records (the bid as
// sent or its proof) stays: 409 document_in_use. A section that pointed at it loses the link. The
// stored file is deleted after the commit unless a project attachment still links it (a converted
// tender's documents are linked, not copied).
func (s *Service) DeleteDocument(ctx context.Context, tenantID, tenderID, id uuid.UUID) error {
	var gone *ent.TenderDocument
	err := s.onTender(ctx, tenantID, tenderID, func(tx *ent.Tx) (map[string]any, error) {
		doc, err := tx.TenderDocument.Query().Where(enttenderdoc.ID(id), enttenderdoc.TenderID(tenderID),
			enttenderdoc.TenantID(tenantID)).Only(ctx)
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("fetch document: %w", err)
		}
		used, err := tx.TenderSubmission.Query().Where(enttendersubmission.TenantID(tenantID), enttendersubmission.TenderID(tenderID),
			enttendersubmission.Or(enttendersubmission.DocumentID(id), enttendersubmission.ProofDocumentID(id))).Exist(ctx)
		if err != nil {
			return nil, fmt.Errorf("check submissions: %w", err)
		}
		if used {
			return nil, stateErr("document_in_use", "a submission records this document, so it stays")
		}
		if _, err := tx.TenderSection.Update().Where(enttendersection.TenantID(tenantID), enttendersection.TenderID(tenderID),
			enttendersection.DocumentID(id)).ClearDocumentID().Save(ctx); err != nil {
			return nil, fmt.Errorf("unlink sections: %w", err)
		}
		if err := tx.TenderDocument.DeleteOne(doc).Exec(ctx); err != nil {
			return nil, fmt.Errorf("delete document: %w", err)
		}
		gone = doc
		return map[string]any{"document_removed": doc.FileName}, nil
	})
	if err != nil {
		return err
	}
	s.removeStored(ctx, tenantID, []string{gone.FileURL})
	return nil
}

// removeStored deletes stored files after a commit, best effort, skipping links and any key a
// project attachment still uses (one query for all of them).
func (s *Service) removeStored(ctx context.Context, tenantID uuid.UUID, refs []string) {
	if s.store == nil {
		return
	}
	var keys []string
	for _, r := range refs {
		if storedKey(r) {
			keys = append(keys, r)
		}
	}
	if len(keys) == 0 {
		return
	}
	linked, err := s.client.Attachment.Query().Where(entattachment.TenantID(tenantID), entattachment.FileURLIn(keys...)).
		Select(entattachment.FieldFileURL).Strings(ctx)
	if err != nil {
		s.log.Warn("tender files kept: attachment check failed", zap.Error(err))
		return
	}
	keep := map[string]bool{}
	for _, k := range linked {
		keep[k] = true
	}
	for _, k := range keys {
		if keep[k] {
			continue
		}
		if err := s.store.Delete(context.WithoutCancel(ctx), k); err != nil {
			s.log.Warn("deleted tender document's file not removed from storage", zap.String("key", k), zap.Error(err))
		}
	}
}
