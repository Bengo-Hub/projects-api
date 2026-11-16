package ticketing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// The create call matches ticketing-api's internal contract: tenant in X-Tenant-ID, subject (not
// title), and the defect as the idempotency key with the session as its parent.
func TestCreateTicketContract(t *testing.T) {
	tenant, defect, session := uuid.New(), uuid.New(), uuid.New()
	var gotHeaders http.Header
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/internal/tickets" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		gotHeaders = r.Header.Clone()
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK) // a repeat answers 200 with the ticket already opened
		_, _ = w.Write([]byte(`{"id":"t1","ticket_number":"TKT-7","status":"open"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "svc-key", zap.NewNop())
	tk, err := c.CreateTicket(context.Background(), CreateTicketInput{
		TenantID: tenant, Subject: "[QA S1] Session: Login", Description: "steps", Priority: "urgent",
		Category: "qa", DefectID: defect, SessionID: session,
	})
	if err != nil {
		t.Fatal(err)
	}
	if tk.TicketNumber != "TKT-7" {
		t.Errorf("ticket = %+v", tk)
	}
	if gotHeaders.Get("X-API-Key") != "svc-key" || gotHeaders.Get("X-Tenant-ID") != tenant.String() {
		t.Errorf("headers = %v", gotHeaders)
	}
	if got["subject"] != "[QA S1] Session: Login" || got["source"] != "projects.qa" || got["channel"] != "internal" {
		t.Errorf("body = %v", got)
	}
	for _, gone := range []string{"title", "tenant_id", "session_id"} {
		if _, ok := got[gone]; ok {
			t.Errorf("body still sends %q", gone)
		}
	}
	ref, _ := got["source_ref"].(map[string]any)
	parent, _ := ref["parent"].(map[string]any)
	if ref["type"] != "qa_defect" || ref["id"] != defect.String() || parent["type"] != "qa_session" || parent["id"] != session.String() {
		t.Errorf("source_ref = %v", ref)
	}
}

func TestCreateTicketNeedsDefect(t *testing.T) {
	c := NewClient("http://unused", "k", zap.NewNop())
	if _, err := c.CreateTicket(context.Background(), CreateTicketInput{TenantID: uuid.New()}); err == nil {
		t.Fatal("a ticket without a defect must be refused")
	}
}
