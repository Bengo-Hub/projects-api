package router

import (
	"net/http"
	"strconv"
	"time"

	ratelimit "github.com/Bengo-Hub/shared-ratelimit"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"

	httpware "github.com/Bengo-Hub/httpware"
	authclient "github.com/Bengo-Hub/shared-auth-client"

	"github.com/bengobox/projects-service/internal/http/authz"
	handlers "github.com/bengobox/projects-service/internal/http/handlers"
)

// Handlers are the route groups of the tenant API.
type Handlers struct {
	Health      *handlers.HealthHandler
	RBAC        *handlers.RBACHandler
	Projects    *handlers.ProjectHandler
	Tasks       *handlers.TaskHandler
	Milestones  *handlers.MilestoneHandler
	Members     *handlers.MemberHandler
	Comments    *handlers.CommentHandler
	Attachments *handlers.AttachmentHandler
	Stream      *handlers.StreamHandler
	Activity    *handlers.ActivityHandler
	Tenders     *handlers.TenderHandler
	Financials  *handlers.FinancialsHandler
	Time        *handlers.TimeHandler
	Portal      *handlers.PortalHandler
	Billing     *handlers.BillingHandler
	Planning    *handlers.PlanningHandler
}

// Deps is everything the router needs.
type Deps struct {
	Log      *zap.Logger
	Service  ServiceInfo
	Handlers Handlers
	// Authenticate validates the bearer token or API key (auth-client RequireAuth) and puts the
	// claims in the context. Nil leaves every authenticated route answering 401.
	Authenticate   func(http.Handler) http.Handler
	AllowedOrigins []string
	Limiter        *ratelimit.Limiter
	// Access is the Layer 3 resolver (local RBAC, JIT) and Members the membership checker.
	Access  authz.RBAC
	Members authz.Members
	// Tenants resolves a slug in the path of a service call.
	Tenants      TenantLookup
	TenantExport http.Handler
	CRMLinks     http.Handler
	QA           QARoutes
	Platform     PlatformRoutes
}

// ServiceInfo is what GET / reports.
type ServiceInfo struct {
	Name    string `json:"service"`
	Version string `json:"version"`
}

// New builds the HTTP router. Middleware order on the tenant API: per-IP rate limit, auth (JWT
// or API key), service entitlement, subscription (mutations only), tenant guard, access (JIT
// provisioning and local permissions), outlet context, then each route's permission gate.
func New(d Deps) http.Handler {
	log := d.Log
	r := chi.NewRouter()

	// Never chi RealIP: it trusts client-sent True-Client-IP/X-Forwarded-For.
	r.Use(ratelimit.TrustedRealIP)
	r.Use(httpware.RequestID)
	r.Use(httpware.Tenant)
	r.Use(httpware.Logging(log))
	r.Use(httpware.Recover(log))
	r.Use(httpware.BypassForStreaming(middleware.Timeout(30 * time.Second)))
	cors := httpware.StandardCORS(d.AllowedOrigins, "X-QA-Access")
	// The client portal reads X-Client-Invites (email_not_verified) from another origin.
	cors.ExposedHeaders = append(cors.ExposedHeaders, "X-Client-Invites")
	r.Use(httpware.CORS(cors))
	// Per-IP abuse limit; after CORS so 429s carry CORS headers.
	if d.Limiter != nil {
		r.Use(d.Limiter.Middleware(ratelimit.IPKey, 300, time.Minute))
	}

	authenticate := d.Authenticate
	if authenticate == nil {
		authenticate = func(http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				authz.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized", "code": "unauthorized"})
			})
		}
	}

	r.Get("/healthz", d.Handlers.Health.Liveness)
	r.Get("/readyz", d.Handlers.Health.Readiness)
	r.Get("/metrics", d.Handlers.Health.Metrics)
	// No Swagger UI is mounted, so the root answers what this is.
	r.Get("/", func(w http.ResponseWriter, _ *http.Request) {
		authz.WriteJSON(w, http.StatusOK, map[string]any{
			"service": d.Service.Name, "version": d.Service.Version, "status": "ok",
			"health": "/healthz", "ready": "/readyz", "api": "/api/v1/{tenant}",
		})
	})

	// Tenant export for auth-api's SSO tenant backups; the handler checks INTERNAL_SERVICE_KEY
	// itself (httpware/tenantexport, Options.InternalKey).
	if d.TenantExport != nil {
		r.Get("/api/v1/s2s/tenant-export", d.TenantExport.ServeHTTP)
	}
	// marketflow's "does projects still link this CRM contact" check. Outside the authenticated
	// group: the handler checks the service key itself and takes the tenant from the path.
	if d.CRMLinks != nil {
		r.Get("/api/v1/s2s/{tenant}/crm-links/{contactID}", d.CRMLinks.ServeHTTP)
	}

	// QA invitees: people a session owner invited by email, who may hold no account in the tenant.
	// Outside the tenant group; each handler proves the invite itself, and only the SSO hand-over
	// route needs a signed-in user.
	if d.QA.Invitee != nil {
		r.Group(func(g chi.Router) {
			if d.Limiter != nil {
				g.Use(d.Limiter.MiddlewareWith(ratelimit.IPKey, ratelimit.Options{Name: "qa-invitee", Limit: 60, Window: time.Minute}))
			}
			d.QA.Invitee.RegisterRoutes(g, authenticate)
		})
	}

	r.Route("/api/v1", func(api chi.Router) {
		api.Use(authenticate)
		// Layer 1.5: the whole projects module (reads included) is closed to a tenant whose plan
		// has no projects entitlement at all.
		api.Use(authclient.RequireServiceAccess("projects"))
		// Layer 2: subscription and the project_management feature, mutations only. Only
		// claims.IsGatingExempt() (platform owner, demo, service-charge, sub-exempt) skips it; a
		// tenant superuser does not.
		api.Use(requireSubscriptionForMutations)

		// Platform owner pages (not tenant scoped). A static segment, so chi matches it before
		// /{tenantID}; each handler checks IsPlatformOwner itself.
		if d.Platform.FieldKeys != nil {
			api.Route("/platform", func(p chi.Router) {
				d.Platform.FieldKeys.RegisterPlatformRoutes(p)
			})
		}

		api.Route("/{tenantID}", func(tenant chi.Router) {
			tenant.Use(tenantGuard(d.Tenants))
			tenant.Use(authz.Resolve(d.Access, d.Members, log))
			tenant.Use(outletContext)
			h := d.Handlers
			// Plan features (reads included): task_tracking for tasks, the board, My Work, the
			// calendar and the live stream; gantt_chart for the Gantt chart on top of it;
			// team_collaboration for comments, mentions and attachments.
			tracking, collaboration := requireFeature("task_tracking"), requireFeature("team_collaboration")
			h.RBAC.RegisterRoutes(tenant)
			h.Projects.RegisterRoutes(tenant)
			gantt := requireFeature("gantt_chart")
			h.Tasks.RegisterRoutes(tenant, tracking, gantt)
			h.Milestones.RegisterRoutes(tenant)
			h.Members.RegisterRoutes(tenant)
			h.Comments.RegisterRoutes(tenant, collaboration)
			if h.Attachments != nil {
				h.Attachments.RegisterRoutes(tenant, collaboration)
			}
			if h.Stream != nil {
				h.Stream.RegisterRoutes(tenant, tracking)
			}
			h.Activity.RegisterRoutes(tenant)
			// Tenders: the project_management feature (mutations, layer 2); the conversion budget retry
			// also needs budget_tracking, like the financials budget proxy.
			h.Tenders.RegisterRoutes(tenant, requireFeature("budget_tracking"))
			if h.Financials != nil {
				// Project financials, portfolio and project budgets: the budget_tracking feature.
				h.Financials.RegisterRoutes(tenant, requireFeature("budget_tracking"))
			}
			if h.Time != nil {
				// Time tracking: the time_tracking feature, and ERP's attendance feature because the
				// hours go to ERP timesheets. erp-api lets the service key through its own gate, so
				// the tenant's ERP entitlement is checked here on the user's token.
				h.Time.RegisterRoutes(tenant, requireFeature("time_tracking"), requireFeature("attendance"))
			}
			if h.Portal != nil {
				// Client portal (sprint 14): the client_portal feature, which every Projects tier has.
				h.Portal.RegisterRoutes(tenant, requireFeature("client_portal"))
			}
			if h.Billing != nil {
				// Billing (sprint 15): draft invoices in treasury, the milestone_billing feature (tier 3).
				h.Billing.RegisterRoutes(tenant, requireFeature("milestone_billing"))
			}
			if h.Planning != nil {
				// Planning depth (sprint 16): workload on workload_planning, baselines on gantt_chart,
				// status rules and automations on project_automation; risks, change requests and
				// templates on the module alone (project_management for mutations).
				h.Planning.RegisterRoutes(tenant, handlers.PlanningGates{
					Tracking: tracking, Gantt: gantt,
					Workload: requireFeature("workload_planning"), Automation: requireFeature("project_automation"),
				})
			}
			if d.QA.Tenant != nil {
				// QA and acceptance; each route is gated on a projects.qa.* permission.
				d.QA.Tenant.RegisterRoutes(tenant)
			}
		})
	})

	return r
}

// outletContext stores an optional X-Outlet-ID in the context; absent is not an error.
func outletContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if outletID := httpware.OutletHeader(r); outletID != "" {
			r = r.WithContext(httpware.WithOutletID(r.Context(), outletID))
		}
		next.ServeHTTP(w, r)
	})
}

// requireSubscriptionForMutations lets reads through and requires an active subscription (with
// the fleet's 7-day post-expiry grace, warned through X-Sub-Grace-Days-Left) and the
// project_management feature for every mutation. Both refusals carry upgrade:true.
func requireSubscriptionForMutations(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		claims, ok := authclient.ClaimsFromContext(r.Context())
		if !ok || claims == nil || claims.IsGatingExempt() {
			next.ServeHTTP(w, r)
			return
		}
		if !claims.IsSubscriptionActive() {
			left, inGrace := claims.GraceDaysLeft(7)
			if !inGrace {
				authz.WriteJSON(w, http.StatusForbidden, map[string]any{
					"error": "Your subscription is not active. Please renew to continue.", "code": "subscription_inactive", "upgrade": true,
				})
				return
			}
			w.Header().Set("X-Sub-Grace-Days-Left", strconv.Itoa(left))
		}
		if !claims.HasFeature("project_management") {
			authz.WriteJSON(w, http.StatusForbidden, map[string]any{
				"error": "feature_not_available", "code": "feature_not_available", "required_feature": "project_management", "upgrade": true,
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}
