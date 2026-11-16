package handlers

import (
	"fmt"
	"net/http"
	"time"

	httpware "github.com/Bengo-Hub/httpware"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/http/authz"
	"github.com/bengobox/projects-service/internal/modules/rbac"
	"github.com/bengobox/projects-service/internal/platform/realtime"
)

// streamHeartbeat keeps an idle stream open through ingress and proxy idle timeouts.
const streamHeartbeat = 15 * time.Second

// StreamHandler serves a project's live updates over SSE from the realtime hub, which relays
// changes made on any pod (shared-events Broadcaster).
type StreamHandler struct {
	log *zap.Logger
	hub *realtime.Hub
}

// NewStreamHandler creates the stream handler.
func NewStreamHandler(log *zap.Logger, hub *realtime.Hub) *StreamHandler {
	return &StreamHandler{log: log.Named("stream.handler"), hub: hub}
}

// RegisterRoutes registers GET /projects/{projectID}/stream behind tracking (task_tracking) and
// projects.tasks.view. The client reads it with fetch (Authorization header, Accept:
// text/event-stream, so the 30 second request timeout and the rate limiter skip it), not with
// EventSource, which cannot send the header.
func (h *StreamHandler) RegisterRoutes(r chi.Router, tracking func(http.Handler) http.Handler) {
	r.With(tracking, authz.Require(rbac.PermTasksView)).Get("/projects/{projectID}/stream", h.Stream)
}

// Stream sends "event: connected", then one event per change in the project (task.created,
// task.updated, task.deleted, task.moved, comment.created, attachment.added) with the JSON hint
// as data, and a comment line every 15 seconds. Hints carry ids only; the client refetches
// through the normal routes, and refetches everything after a reconnect.
func (h *StreamHandler) Stream(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	if h.hub == nil {
		respondError(w, http.StatusServiceUnavailable, "live updates are not available")
		return
	}
	httpware.StreamHeaders(w)
	// The server WriteTimeout would cut the stream; the heartbeat keeps it alive instead.
	httpware.ExtendWriteDeadline(w)
	rc := http.NewResponseController(w)

	sub := h.hub.Subscribe(tenantID, projectID)
	defer h.hub.Unsubscribe(sub)

	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, "retry: 5000\nevent: connected\ndata: {}\n\n"); err != nil || rc.Flush() != nil {
		return
	}
	heartbeat := time.NewTicker(streamHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil || rc.Flush() != nil {
				return
			}
		case raw, ok := <-sub.C:
			if !ok {
				return
			}
			ev, err := realtime.Decode(raw)
			if err != nil || ev.Type == "" {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, raw); err != nil || rc.Flush() != nil {
				return
			}
		}
	}
}
