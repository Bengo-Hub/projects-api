package router

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"go.uber.org/zap"

	authclient "github.com/Bengo-Hub/shared-auth-client"
	handlers "github.com/bengobox/projects-service/internal/http/handlers"
	sharedmw "github.com/bengobox/projects-service/internal/shared/middleware"
)

// Handlers holds all HTTP handlers for dependency injection.
type Handlers struct {
	Health           *handlers.HealthHandler
	User             *handlers.UserHandler
	Project          *handlers.ProjectHandler
	Task             *handlers.TaskHandler
	Tender           *handlers.TenderHandler
	TenderDocument   *handlers.TenderDocumentHandler
	TenderCommittee  *handlers.TenderCommitteeHandler
	TenderEvaluation *handlers.TenderEvaluationHandler
	TenderMeeting    *handlers.TenderMeetingHandler
	TenderSection    *handlers.TenderSectionHandler
	TenderSubmission *handlers.TenderSubmissionHandler
	Docs             *handlers.DocsHandler
}

// Config holds router configuration.
type Config struct {
	Log            *zap.Logger
	Handlers       Handlers
	AuthMiddleware *authclient.AuthMiddleware
}

// New creates the main HTTP router with all API versions mounted.
func New(cfg Config) http.Handler {
	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(sharedmw.RequestID)
	r.Use(sharedmw.Tenant)
	r.Use(sharedmw.Logging(cfg.Log))
	r.Use(sharedmw.Recover(cfg.Log))
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "X-Tenant-ID", "X-Request-ID"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health endpoints (no auth required)
	r.Get("/healthz", cfg.Handlers.Health.Liveness)
	r.Get("/readyz", cfg.Handlers.Health.Readiness)
	r.Get("/metrics", cfg.Handlers.Health.Metrics)

	// API documentation (no auth required)
	if cfg.Handlers.Docs != nil {
		r.Get("/docs/openapi.yaml", cfg.Handlers.Docs.OpenAPISpec)
		r.Get("/docs/swagger", cfg.Handlers.Docs.SwaggerUI)
	}

	// Mount API versions
	r.Route("/api", func(api chi.Router) {
		api.Mount("/v1", newV1Router(cfg))
		// Future: api.Mount("/v2", newV2Router(cfg))
	})

	return r
}

// newV1Router creates the v1 API router.
func newV1Router(cfg Config) http.Handler {
	r := chi.NewRouter()

	// Apply auth middleware to all v1 routes
	if cfg.AuthMiddleware != nil {
		r.Use(cfg.AuthMiddleware.RequireAuth)
	}

	r.Route("/{tenantID}", func(tenant chi.Router) {
		// User management routes
		cfg.Handlers.User.RegisterRoutes(tenant)

		// Project routes
		tenant.Route("/projects", func(projects chi.Router) {
			cfg.Handlers.Project.RegisterRoutes(projects)

			// Task routes (nested under projects)
			projects.Route("/{projectID}/tasks", func(tasks chi.Router) {
				cfg.Handlers.Task.RegisterRoutes(tasks)
			})
		})

		// Tender routes
		if cfg.Handlers.Tender != nil {
			tenant.Route("/tenders", func(tenders chi.Router) {
				cfg.Handlers.Tender.RegisterRoutes(tenders)

				// Tender document routes (nested under tenders)
				if cfg.Handlers.TenderDocument != nil {
					tenders.Route("/{tenderID}/documents", func(docs chi.Router) {
						cfg.Handlers.TenderDocument.RegisterRoutes(docs)
					})
				}

				// Tender committee routes (nested under tenders)
				if cfg.Handlers.TenderCommittee != nil {
					tenders.Route("/{tenderID}/committees", func(committees chi.Router) {
						cfg.Handlers.TenderCommittee.RegisterRoutes(committees)
					})
				}

				// Tender evaluation routes (nested under tenders)
				if cfg.Handlers.TenderEvaluation != nil {
					tenders.Route("/{tenderID}/evaluations", func(evaluations chi.Router) {
						cfg.Handlers.TenderEvaluation.RegisterRoutes(evaluations)
					})
				}

				// Tender meeting routes (nested under tenders)
				if cfg.Handlers.TenderMeeting != nil {
					tenders.Route("/{tenderID}/meetings", func(meetings chi.Router) {
						cfg.Handlers.TenderMeeting.RegisterRoutes(meetings)
					})
				}

				// Tender section routes (nested under tenders)
				if cfg.Handlers.TenderSection != nil {
					tenders.Route("/{tenderID}/sections", func(sections chi.Router) {
						cfg.Handlers.TenderSection.RegisterRoutes(sections)
					})
				}

				// Tender submission routes (nested under tenders)
				if cfg.Handlers.TenderSubmission != nil {
					tenders.Route("/{tenderID}/submissions", func(submissions chi.Router) {
						cfg.Handlers.TenderSubmission.RegisterRoutes(submissions)
					})
				}
			})
		}
	})

	return r
}

// newV2Router creates the v2 API router.
// Uncomment and implement when v2 is needed.
// func newV2Router(cfg Config) http.Handler {
// 	r := chi.NewRouter()
//
// 	if cfg.AuthMiddleware != nil {
// 		r.Use(cfg.AuthMiddleware.RequireAuth)
// 	}
//
// 	r.Route("/{tenantID}", func(tenant chi.Router) {
// 		// V2 routes with breaking changes
// 		cfg.Handlers.User.RegisterRoutesV2(tenant)
// 		cfg.Handlers.Project.RegisterRoutesV2(tenant)
// 	})
//
// 	return r
// }
