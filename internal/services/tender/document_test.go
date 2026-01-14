package tender

import (
	"context"
	"testing"
	"time"

	"github.com/bengobox/projects-service/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestService_CreateDocument(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := zap.NewNop()
	svc := NewService(logger, client)
	ctx := context.Background()

	tenantID := uuid.New()
	createdBy := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	// Create a tender first
	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Document Test Tender",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	tests := []struct {
		name    string
		params  DocumentCreateParams
		wantErr bool
	}{
		{
			name: "create document with required fields",
			params: DocumentCreateParams{
				TenantID:   tenantID,
				TenderID:   tender.ID,
				Name:       "Technical Proposal",
				FileURL:    "https://storage.example.com/docs/proposal.pdf",
				FileName:   "proposal.pdf",
				FileSize:   1024 * 1024,
				UploadedBy: createdBy,
			},
			wantErr: false,
		},
		{
			name: "create document with all optional fields",
			params: DocumentCreateParams{
				TenantID:     tenantID,
				TenderID:     tender.ID,
				Name:         "Financial Proposal",
				Description:  "Detailed cost breakdown",
				DocumentType: "financial",
				FileURL:      "https://storage.example.com/docs/financial.pdf",
				FileName:     "financial.pdf",
				FileSize:     2 * 1024 * 1024,
				MimeType:     "application/pdf",
				UploadedBy:   createdBy,
				Metadata:     map[string]any{"pages": 25, "version": "1.0"},
			},
			wantErr: false,
		},
		{
			name: "create document for non-existent tender",
			params: DocumentCreateParams{
				TenantID:   tenantID,
				TenderID:   uuid.New(),
				Name:       "Orphan Document",
				FileURL:    "https://storage.example.com/docs/orphan.pdf",
				FileName:   "orphan.pdf",
				FileSize:   1024,
				UploadedBy: createdBy,
			},
			wantErr: true,
		},
		{
			name: "create document with wrong tenant",
			params: DocumentCreateParams{
				TenantID:   uuid.New(),
				TenderID:   tender.ID,
				Name:       "Wrong Tenant Doc",
				FileURL:    "https://storage.example.com/docs/wrong.pdf",
				FileName:   "wrong.pdf",
				FileSize:   1024,
				UploadedBy: createdBy,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := svc.CreateDocument(ctx, tt.params)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, doc)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, doc)
				assert.Equal(t, tt.params.Name, doc.Name)
				assert.Equal(t, tt.params.FileURL, doc.FileURL)
				assert.Equal(t, tt.params.TenderID, doc.TenderID)
				assert.Equal(t, 1, doc.Version)
				assert.True(t, doc.IsLatest)
			}
		})
	}
}

func TestService_GetDocument(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := zap.NewNop()
	svc := NewService(logger, client)
	ctx := context.Background()

	tenantID := uuid.New()
	otherTenantID := uuid.New()
	createdBy := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Get Document Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	doc, err := svc.CreateDocument(ctx, DocumentCreateParams{
		TenantID:   tenantID,
		TenderID:   tender.ID,
		Name:       "Test Document",
		FileURL:    "https://storage.example.com/docs/test.pdf",
		FileName:   "test.pdf",
		FileSize:   1024,
		UploadedBy: createdBy,
	})
	require.NoError(t, err)

	tests := []struct {
		name       string
		tenantID   uuid.UUID
		documentID uuid.UUID
		wantErr    bool
		errType    error
	}{
		{
			name:       "get existing document",
			tenantID:   tenantID,
			documentID: doc.ID,
			wantErr:    false,
		},
		{
			name:       "get non-existent document",
			tenantID:   tenantID,
			documentID: uuid.New(),
			wantErr:    true,
			errType:    ErrDocumentNotFound,
		},
		{
			name:       "get document from wrong tenant",
			tenantID:   otherTenantID,
			documentID: doc.ID,
			wantErr:    true,
			errType:    ErrDocumentNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.GetDocument(ctx, tt.tenantID, tt.documentID)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errType != nil {
					assert.ErrorIs(t, err, tt.errType)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, doc.ID, result.ID)
				assert.Equal(t, doc.Name, result.Name)
			}
		})
	}
}

func TestService_ListDocuments(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := zap.NewNop()
	svc := NewService(logger, client)
	ctx := context.Background()

	tenantID := uuid.New()
	createdBy := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "List Documents Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	// Create multiple documents with different types
	documentTypes := []string{"technical", "financial", "legal", "technical", "other"}
	for i, docType := range documentTypes {
		_, err := svc.CreateDocument(ctx, DocumentCreateParams{
			TenantID:     tenantID,
			TenderID:     tender.ID,
			Name:         "Document " + string(rune('A'+i)),
			DocumentType: docType,
			FileURL:      "https://storage.example.com/docs/" + string(rune('A'+i)) + ".pdf",
			FileName:     string(rune('A'+i)) + ".pdf",
			FileSize:     int64(1024 * (i + 1)),
			UploadedBy:   createdBy,
		})
		require.NoError(t, err)
	}

	tests := []struct {
		name      string
		params    DocumentListParams
		wantCount int
		wantTotal int
	}{
		{
			name: "list all documents",
			params: DocumentListParams{
				TenantID: tenantID,
				TenderID: tender.ID,
				Limit:    10,
			},
			wantCount: 5,
			wantTotal: 5,
		},
		{
			name: "list with pagination",
			params: DocumentListParams{
				TenantID: tenantID,
				TenderID: tender.ID,
				Limit:    2,
				Offset:   0,
			},
			wantCount: 2,
			wantTotal: 5,
		},
		{
			name: "filter by document type",
			params: DocumentListParams{
				TenantID:     tenantID,
				TenderID:     tender.ID,
				DocumentType: "technical",
				Limit:        10,
			},
			wantCount: 2,
			wantTotal: 2,
		},
		{
			name: "filter latest only",
			params: DocumentListParams{
				TenantID:   tenantID,
				TenderID:   tender.ID,
				LatestOnly: true,
				Limit:      10,
			},
			wantCount: 5,
			wantTotal: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs, total, err := svc.ListDocuments(ctx, tt.params)
			require.NoError(t, err)
			assert.Len(t, docs, tt.wantCount)
			assert.Equal(t, tt.wantTotal, total)
		})
	}
}

func TestService_UpdateDocument(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := zap.NewNop()
	svc := NewService(logger, client)
	ctx := context.Background()

	tenantID := uuid.New()
	createdBy := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Update Document Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	doc, err := svc.CreateDocument(ctx, DocumentCreateParams{
		TenantID:   tenantID,
		TenderID:   tender.ID,
		Name:       "Original Name",
		FileURL:    "https://storage.example.com/docs/original.pdf",
		FileName:   "original.pdf",
		FileSize:   1024,
		UploadedBy: createdBy,
	})
	require.NoError(t, err)

	newName := "Updated Document Name"
	newDescription := "Updated description"
	newType := "technical"

	tests := []struct {
		name       string
		tenantID   uuid.UUID
		documentID uuid.UUID
		params     DocumentUpdateParams
		wantErr    bool
		validate   func(t *testing.T, doc interface{})
	}{
		{
			name:       "update name",
			tenantID:   tenantID,
			documentID: doc.ID,
			params:     DocumentUpdateParams{Name: &newName},
			wantErr:    false,
			validate: func(t *testing.T, d interface{}) {
				doc := d.(interface{ GetName() string })
				assert.Equal(t, newName, doc.GetName())
			},
		},
		{
			name:       "update multiple fields",
			tenantID:   tenantID,
			documentID: doc.ID,
			params: DocumentUpdateParams{
				Description:  &newDescription,
				DocumentType: &newType,
			},
			wantErr: false,
		},
		{
			name:       "update non-existent document",
			tenantID:   tenantID,
			documentID: uuid.New(),
			params:     DocumentUpdateParams{Name: &newName},
			wantErr:    true,
		},
		{
			name:       "update document from wrong tenant",
			tenantID:   uuid.New(),
			documentID: doc.ID,
			params:     DocumentUpdateParams{Name: &newName},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.UpdateDocument(ctx, tt.tenantID, tt.documentID, tt.params)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				if tt.validate != nil {
					tt.validate(t, result)
				}
			}
		})
	}
}

func TestService_DeleteDocument(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := zap.NewNop()
	svc := NewService(logger, client)
	ctx := context.Background()

	tenantID := uuid.New()
	createdBy := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Delete Document Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	doc, err := svc.CreateDocument(ctx, DocumentCreateParams{
		TenantID:   tenantID,
		TenderID:   tender.ID,
		Name:       "To Delete",
		FileURL:    "https://storage.example.com/docs/delete.pdf",
		FileName:   "delete.pdf",
		FileSize:   1024,
		UploadedBy: createdBy,
	})
	require.NoError(t, err)

	tests := []struct {
		name       string
		tenantID   uuid.UUID
		documentID uuid.UUID
		wantErr    bool
	}{
		{
			name:       "delete existing document",
			tenantID:   tenantID,
			documentID: doc.ID,
			wantErr:    false,
		},
		{
			name:       "delete already deleted document",
			tenantID:   tenantID,
			documentID: doc.ID,
			wantErr:    true,
		},
		{
			name:       "delete non-existent document",
			tenantID:   tenantID,
			documentID: uuid.New(),
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.DeleteDocument(ctx, tt.tenantID, tt.documentID)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// Verify deletion
				_, err := svc.GetDocument(ctx, tt.tenantID, tt.documentID)
				assert.ErrorIs(t, err, ErrDocumentNotFound)
			}
		})
	}
}

func TestService_CreateDocumentVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := zap.NewNop()
	svc := NewService(logger, client)
	ctx := context.Background()

	tenantID := uuid.New()
	createdBy := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	tender, err := svc.Create(ctx, CreateParams{
		TenantID:   tenantID,
		Title:      "Document Version Test",
		ClientName: "Test Client",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	// Create initial document (version 1)
	v1, err := svc.CreateDocument(ctx, DocumentCreateParams{
		TenantID:     tenantID,
		TenderID:     tender.ID,
		Name:         "Versioned Document",
		DocumentType: "technical",
		FileURL:      "https://storage.example.com/docs/v1.pdf",
		FileName:     "document_v1.pdf",
		FileSize:     1024,
		UploadedBy:   createdBy,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, v1.Version)
	assert.True(t, v1.IsLatest)

	// Create version 2
	v2, err := svc.CreateDocumentVersion(ctx, tenantID, v1.ID, DocumentCreateParams{
		TenantID:   tenantID,
		Name:       "Versioned Document v2",
		FileURL:    "https://storage.example.com/docs/v2.pdf",
		FileName:   "document_v2.pdf",
		FileSize:   2048,
		UploadedBy: createdBy,
	})
	require.NoError(t, err)
	assert.Equal(t, 2, v2.Version)
	assert.True(t, v2.IsLatest)
	assert.Equal(t, v1.ID, v2.PreviousVersionID)
	assert.Equal(t, v1.DocumentType, v2.DocumentType) // Inherits type from previous

	// Verify v1 is no longer latest
	v1Updated, err := svc.GetDocument(ctx, tenantID, v1.ID)
	require.NoError(t, err)
	assert.False(t, v1Updated.IsLatest)

	// Create version 3
	v3, err := svc.CreateDocumentVersion(ctx, tenantID, v2.ID, DocumentCreateParams{
		TenantID:     tenantID,
		Name:         "Versioned Document v3",
		DocumentType: "revised", // Override document type
		FileURL:      "https://storage.example.com/docs/v3.pdf",
		FileName:     "document_v3.pdf",
		FileSize:     3072,
		UploadedBy:   createdBy,
	})
	require.NoError(t, err)
	assert.Equal(t, 3, v3.Version)
	assert.True(t, v3.IsLatest)
	assert.Equal(t, v2.ID, v3.PreviousVersionID)
	assert.Equal(t, "revised", v3.DocumentType) // Overridden type

	// List should show all versions
	allDocs, total, err := svc.ListDocuments(ctx, DocumentListParams{
		TenantID: tenantID,
		TenderID: tender.ID,
		Limit:    10,
	})
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	assert.Len(t, allDocs, 3)

	// Latest only filter should show 1
	latestDocs, latestTotal, err := svc.ListDocuments(ctx, DocumentListParams{
		TenantID:   tenantID,
		TenderID:   tender.ID,
		LatestOnly: true,
		Limit:      10,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, latestTotal)
	assert.Len(t, latestDocs, 1)
	assert.Equal(t, v3.ID, latestDocs[0].ID)
}

func TestService_DocumentTenantIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := zap.NewNop()
	svc := NewService(logger, client)
	ctx := context.Background()

	tenant1 := uuid.New()
	tenant2 := uuid.New()
	createdBy := uuid.New()
	deadline := time.Now().Add(30 * 24 * time.Hour)

	// Create tender and document for tenant 1
	tender1, err := svc.Create(ctx, CreateParams{
		TenantID:   tenant1,
		Title:      "Tenant 1 Tender",
		ClientName: "Client 1",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	doc1, err := svc.CreateDocument(ctx, DocumentCreateParams{
		TenantID:   tenant1,
		TenderID:   tender1.ID,
		Name:       "Tenant 1 Document",
		FileURL:    "https://storage.example.com/docs/t1.pdf",
		FileName:   "t1.pdf",
		FileSize:   1024,
		UploadedBy: createdBy,
	})
	require.NoError(t, err)

	// Create tender and document for tenant 2
	tender2, err := svc.Create(ctx, CreateParams{
		TenantID:   tenant2,
		Title:      "Tenant 2 Tender",
		ClientName: "Client 2",
		Deadline:   deadline,
		CreatedBy:  createdBy,
	})
	require.NoError(t, err)

	doc2, err := svc.CreateDocument(ctx, DocumentCreateParams{
		TenantID:   tenant2,
		TenderID:   tender2.ID,
		Name:       "Tenant 2 Document",
		FileURL:    "https://storage.example.com/docs/t2.pdf",
		FileName:   "t2.pdf",
		FileSize:   1024,
		UploadedBy: createdBy,
	})
	require.NoError(t, err)

	// Tenant 1 cannot access tenant 2's document
	_, err = svc.GetDocument(ctx, tenant1, doc2.ID)
	assert.ErrorIs(t, err, ErrDocumentNotFound)

	// Tenant 2 cannot access tenant 1's document
	_, err = svc.GetDocument(ctx, tenant2, doc1.ID)
	assert.ErrorIs(t, err, ErrDocumentNotFound)

	// List documents only shows tenant's own documents
	t1Docs, _, err := svc.ListDocuments(ctx, DocumentListParams{
		TenantID: tenant1,
		TenderID: tender1.ID,
		Limit:    10,
	})
	require.NoError(t, err)
	assert.Len(t, t1Docs, 1)
	assert.Equal(t, doc1.ID, t1Docs[0].ID)

	t2Docs, _, err := svc.ListDocuments(ctx, DocumentListParams{
		TenantID: tenant2,
		TenderID: tender2.ID,
		Limit:    10,
	})
	require.NoError(t, err)
	assert.Len(t, t2Docs, 1)
	assert.Equal(t, doc2.ID, t2Docs[0].ID)
}
