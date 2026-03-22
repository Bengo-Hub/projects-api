package router

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"go.uber.org/zap"

	httpware "github.com/Bengo-Hub/httpware"
	authclient "github.com/Bengo-Hub/shared-auth-client"
	handlers "github.com/bengobox/projects-service/internal/http/handlers"
)

func New(log *zap.Logger, health *handlers.HealthHandler, userHandler *handlers.UserHandler, authMiddleware *authclient.AuthMiddleware, allowedOrigins []string) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RealIP)
	r.Use(httpware.RequestID)
	r.Use(httpware.Tenant)
	r.Use(httpware.Logging(log))
	r.Use(httpware.Recover(log))
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "X-Tenant-ID", "X-Tenant-Slug", "X-Request-ID"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Get("/healthz", health.Liveness)
	r.Get("/readyz", health.Readiness)
	r.Get("/metrics", health.Metrics)

	// Redirect root path to Swagger documentation
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/v1/docs/", http.StatusMovedPermanently)
	})

	r.Route("/api/v1", func(api chi.Router) {
		// Apply auth middleware to all v1 routes
		if authMiddleware != nil {
			api.Use(authMiddleware.RequireAuth)
			// Layer 2: Subscription enforcement — reject expired/cancelled tenants
			api.Use(authclient.RequireActiveSubscription())
		}

		api.Route("/{tenantID}", func(tenant chi.Router) {
			// User management routes
			userHandler.RegisterRoutes(tenant)

			tenant.Route("/projects", func(projects chi.Router) {
				// Placeholder endpoints - to be implemented
				projects.Get("/", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNotImplemented)
					w.Write([]byte("Not implemented yet"))
				})
			})
		})
	})

	return r
}
