// Package realtime pushes small change hints for a project to connected browsers over SSE.
// Services publish an Event after their transaction commits; the shared-events Broadcaster relays
// it to every replica (core NATS, subject _rt.projects.work.<tenant>.project:<id>), and each
// replica's FanoutHub hands it to its own streams for that project. Payloads are hints only: the
// client refetches through the normal, permission-checked routes, and refetches everything on
// reconnect because a pod that was reconnecting to NATS misses messages.
package realtime

import (
	"encoding/json"

	eventslib "github.com/Bengo-Hub/shared-events"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// Event types, sent as the SSE "event:" field.
const (
	TaskCreated     = "task.created"
	TaskUpdated     = "task.updated"
	TaskDeleted     = "task.deleted"
	TaskMoved       = "task.moved" // status (board column) changed
	CommentCreated  = "comment.created"
	AttachmentAdded = "attachment.added"
)

// Namespace and Topic make the relay subject _rt.projects.work.<tenant>.<scope>.
const (
	Namespace = "projects"
	Topic     = "work"
)

// Event is the SSE payload.
type Event struct {
	Type      string `json:"type"`
	ProjectID string `json:"project_id"`
	TaskID    string `json:"task_id,omitempty"`
	// ID is the comment or attachment the event is about.
	ID     string `json:"id,omitempty"`
	Status string `json:"status,omitempty"`
	// ActorID lets the client that made the change skip its own echo.
	ActorID string `json:"actor_id,omitempty"`
}

// Publisher is what the domain services depend on, so none of them imports NATS.
type Publisher interface {
	Publish(tenantID uuid.UUID, ev Event)
}

// Emit publishes through p when it is set; services call it after their commit, so a nil
// publisher (tests, a service built without the hub) is a no-op.
func Emit(p Publisher, tenantID uuid.UUID, ev Event) {
	if p == nil || tenantID == uuid.Nil || ev.Type == "" || ev.ProjectID == "" {
		return
	}
	p.Publish(tenantID, ev)
}

// Scope is the FanoutHub scope of one project's stream.
func Scope(projectID string) string { return "project:" + projectID }

// Hub is this replica's registry of project streams on top of the shared FanoutHub.
type Hub struct {
	fan *eventslib.FanoutHub
	log *zap.Logger
}

// NewHub builds the hub. nc may be nil (local development, tests): delivery is then this pod only.
func NewHub(log *zap.Logger, nc *nats.Conn) *Hub {
	if log == nil {
		log = zap.NewNop()
	}
	relay := eventslib.NewBroadcaster(log, nc, Namespace)
	fan, err := eventslib.NewFanoutHub(relay, Topic, 64)
	if err != nil {
		log.Warn("realtime relay subscription failed; delivery is local to this pod", zap.Error(err))
	}
	return &Hub{fan: fan, log: log.Named("realtime")}
}

// Publish sends the event to every stream of its project, on every pod. Nil-safe.
func (h *Hub) Publish(tenantID uuid.UUID, ev Event) {
	if h == nil || h.fan == nil {
		return
	}
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	h.fan.Publish(tenantID.String(), Scope(ev.ProjectID), b)
}

// Subscribe registers one stream for a project of the tenant.
func (h *Hub) Subscribe(tenantID, projectID uuid.UUID) *eventslib.Sub {
	return h.fan.Subscribe(tenantID.String(), Scope(projectID.String()))
}

// Unsubscribe removes a stream.
func (h *Hub) Unsubscribe(s *eventslib.Sub) { h.fan.Unsubscribe(s) }

// Decode parses a relayed payload.
func Decode(raw []byte) (Event, error) {
	var ev Event
	err := json.Unmarshal(raw, &ev)
	return ev, err
}
