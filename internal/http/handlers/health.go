package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// HealthHandler exposes liveness/readiness endpoints for the projects service.
type HealthHandler struct {
	log    *zap.Logger
	db     *ent.Client
	cache  *redis.Client
	events *nats.Conn
}

func NewHealthHandler(log *zap.Logger, db *ent.Client, cache *redis.Client, events *nats.Conn) *HealthHandler {
	return &HealthHandler{log: log, db: db, cache: cache, events: events}
}

type livenessResponse struct {
	Status  string `json:"status" example:"ok"`
	Service string `json:"service" example:"projects-service"`
}

type readinessResponse struct {
	Status       string            `json:"status" example:"OK"`
	Dependencies map[string]string `json:"dependencies"`
}

// Liveness reports whether the API process is running.
func (h *HealthHandler) Liveness(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, livenessResponse{
		Status:  "ok",
		Service: "projects-service",
	})
}

// Readiness checks downstream infrastructure dependencies.
func (h *HealthHandler) Readiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	issues := map[string]string{}

	if h.db != nil {
		// Use a simple query to check database connectivity
		if _, err := h.db.Role.Query().Limit(1).All(ctx); err != nil {
			issues["postgres"] = err.Error()
		}
	}

	if h.cache != nil {
		if err := h.cache.Ping(ctx).Err(); err != nil {
			issues["redis"] = err.Error()
		}
	}

	if h.events != nil && !h.events.IsConnected() {
		issues["nats"] = "not connected"
	}

	status := http.StatusOK
	if len(issues) > 0 {
		status = http.StatusServiceUnavailable
	}

	respondJSON(w, status, readinessResponse{
		Status:       http.StatusText(status),
		Dependencies: issues,
	})
}

// Metrics exposes Prometheus metrics.
func (h *HealthHandler) Metrics(w http.ResponseWriter, r *http.Request) {
	promhttp.Handler().ServeHTTP(w, r)
}

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

