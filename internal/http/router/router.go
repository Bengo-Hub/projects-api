package router

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"go.uber.org/zap"

	httpware "github.com/Bengo-Hub/httpware"
	authclient "github.com/Bengo-Hub/shared-auth-client"
	handlers "github.com/bengobox/projects-service/internal/http/handlers"
)

func New(
	log *zap.Logger,
	health *handlers.HealthHandler,
	userHandler *handlers.UserHandler,
	projectHandler *handlers.ProjectHandler,
	taskHandler *handlers.TaskHandler,
	milestoneHandler *handlers.MilestoneHandler,
	memberHandler *handlers.MemberHandler,
	commentHandler *handlers.CommentHandler,
	activityHandler *handlers.ActivityHandler,
	tenderHandler *handlers.TenderHandler,
	authMiddleware *authclient.AuthMiddleware,
	allowedOrigins []string,
) http.Handler {
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
		AllowedHeaders:   []string{"Authorization", "Content-Type", "X-Tenant-ID", "X-Tenant-Slug", "X-Request-ID", "X-Outlet-ID", "X-API-Key"},
		ExposedHeaders:   []string{"Link", "X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset", "Retry-After"},
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
		// Optional outlet context — extracts X-Outlet-ID if present
		api.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if outletID := httpware.OutletHeader(r); outletID != "" {
					r = r.WithContext(httpware.WithOutletID(r.Context(), outletID))
				}
				next.ServeHTTP(w, r)
			})
		})

		// Apply auth middleware to all v1 routes
		if authMiddleware != nil {
			api.Use(authMiddleware.RequireAuth)
			// Layer 2: Subscription + feature enforcement — mutations only (GET/HEAD/OPTIONS
			// pass through). Exemption funnels through claims.IsGatingExempt() (platform owner /
			// demo / service-charge / sub-exempt), uniform with the rest of the fleet — note a
			// tenant superuser is NOT exempt. A mutating tenant must have an active subscription
			// AND the project_management feature (the projects module's base entitlement).
			api.Use(func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
						next.ServeHTTP(w, r)
						return
					}
					claims, ok := authclient.ClaimsFromContext(r.Context())
					if !ok {
						next.ServeHTTP(w, r)
						return
					}
					if claims.IsGatingExempt() {
						next.ServeHTTP(w, r)
						return
					}
					if !claims.IsSubscriptionActive() {
						// Uniform fleet-wide 7-day post-expiry grace: an EXPIRED tenant may still
						// mutate within the window (warned via X-Sub-Grace-Days-Left); beyond it → 403.
						if left, inGrace := claims.GraceDaysLeft(7); inGrace {
							w.Header().Set("X-Sub-Grace-Days-Left", strconv.Itoa(left))
						} else {
							w.Header().Set("Content-Type", "application/json")
							w.WriteHeader(http.StatusForbidden)
							_, _ = w.Write([]byte(`{"error":"Your subscription is not active. Please renew to continue.","code":"subscription_inactive","upgrade":true}`))
							return
						}
					}
					if !claims.HasFeature("project_management") {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusForbidden)
						_, _ = w.Write([]byte(`{"error":"feature_not_available","code":"feature_not_available","required_feature":"project_management","upgrade":true}`))
						return
					}
					next.ServeHTTP(w, r)
				})
			})
		}

		api.Route("/{tenantID}", func(tenant chi.Router) {
			userHandler.RegisterRoutes(tenant)
			projectHandler.RegisterRoutes(tenant)
			taskHandler.RegisterRoutes(tenant)
			milestoneHandler.RegisterRoutes(tenant)
			memberHandler.RegisterRoutes(tenant)
			commentHandler.RegisterRoutes(tenant)
			activityHandler.RegisterRoutes(tenant)
			tenderHandler.RegisterRoutes(tenant)
		})
	})

	return r
}
