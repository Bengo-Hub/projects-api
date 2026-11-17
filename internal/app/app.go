package app

import (
	"context"
	"errors"
	"fmt"
	ratelimit "github.com/Bengo-Hub/shared-ratelimit"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	authclient "github.com/Bengo-Hub/shared-auth-client"
	eventslib "github.com/Bengo-Hub/shared-events"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/Bengo-Hub/httpware/objectstore"
	"github.com/Bengo-Hub/httpware/tenantexport"
	"github.com/bengobox/projects-service/internal/config"
	"github.com/bengobox/projects-service/internal/ent"
	handlers "github.com/bengobox/projects-service/internal/http/handlers"
	router "github.com/bengobox/projects-service/internal/http/router"
	"github.com/bengobox/projects-service/internal/modules/rbac"
	"github.com/bengobox/projects-service/internal/platform/authotp"
	"github.com/bengobox/projects-service/internal/platform/authusers"
	"github.com/bengobox/projects-service/internal/platform/catalog"
	"github.com/bengobox/projects-service/internal/platform/database"
	"github.com/bengobox/projects-service/internal/platform/erp"
	"github.com/bengobox/projects-service/internal/platform/events"
	"github.com/bengobox/projects-service/internal/platform/marketflow"
	"github.com/bengobox/projects-service/internal/platform/realtime"
	"github.com/bengobox/projects-service/internal/platform/ticketing"
	"github.com/bengobox/projects-service/internal/platform/treasury"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/attachments"
	"github.com/bengobox/projects-service/internal/services/comments"
	"github.com/bengobox/projects-service/internal/services/crmsync"
	"github.com/bengobox/projects-service/internal/services/fieldkeys"
	"github.com/bengobox/projects-service/internal/services/billing"
	"github.com/bengobox/projects-service/internal/services/financials"
	"github.com/bengobox/projects-service/internal/services/members"
	"github.com/bengobox/projects-service/internal/services/membership"
	"github.com/bengobox/projects-service/internal/services/milestones"
	"github.com/bengobox/projects-service/internal/services/portal"
	projectssvc "github.com/bengobox/projects-service/internal/services/projects"
	"github.com/bengobox/projects-service/internal/services/qa"
	"github.com/bengobox/projects-service/internal/services/tasks"
	"github.com/bengobox/projects-service/internal/services/tenders"
	"github.com/bengobox/projects-service/internal/services/timetracking"
	"github.com/bengobox/projects-service/internal/services/usersync"
	"github.com/bengobox/projects-service/internal/shared/fieldcrypt"
	"github.com/bengobox/projects-service/internal/shared/logger"

	"database/sql"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type App struct {
	cfg                *config.Config
	log                *zap.Logger
	httpServer         *http.Server
	db                 *pgxpool.Pool
	cache              *redis.Client
	events             *nats.Conn
	outboxPublisher    *eventslib.Publisher
	authEventsConsumer *usersync.AuthEventsConsumer
	budgetEvents       *financials.BudgetEventsConsumer
	// crmContacts clears a project's copied client name when the CRM contact is erased or deleted.
	crmContacts *crmsync.ContactEventsConsumer
	// qaFeatures flags QA templates for review when a catalogue feature they check changes.
	qaFeatures *qa.FeatureEventsConsumer
	// fieldKeys reloads the field key ring and continues a rotation the platform owner started.
	fieldKeys *fieldkeys.Service
}

// tenantExportHandler serves auth-api's SSO tenant backups (httpware/tenantexport). Nil without an
// INTERNAL_SERVICE_KEY, so the export is never reachable unauthenticated.
func tenantExportHandler(db *sql.DB, internalKey string) http.Handler {
	if internalKey == "" {
		return nil
	}
	return tenantexport.Handler(db, tenantexport.Options{Service: "projects", InternalKey: internalKey})
}

// crmLinksHandler answers marketflow's CRM contact link check (X-API-Key = INTERNAL_SERVICE_KEY).
// Nil without a key, so the route is not mounted; the handler also refuses an empty key itself.
func crmLinksHandler(db *ent.Client, aside *sharedcache.Aside, authURL, internalKey string, log *zap.Logger) http.Handler {
	if internalKey == "" {
		return nil
	}
	h := handlers.NewCRMLinksHandler(db, handlers.NewTenantResolver(aside, authURL), internalKey, log)
	return http.HandlerFunc(h.Referenced)
}

func New(ctx context.Context) (*App, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	log, err := logger.New(cfg.App.Env)
	if err != nil {
		return nil, fmt.Errorf("logger init: %w", err)
	}
	// Shared libraries and the 5xx sanitiser in the handlers log through the global logger.
	zap.ReplaceGlobals(log)

	dbPool, err := database.NewPool(ctx, cfg.Postgres)
	if err != nil {
		return nil, fmt.Errorf("postgres init: %w", err)
	}

	// Schema migrations run once per rollout in projects-migrate (entrypoint, advisory-locked,
	// direct DSN); the optional in-app copy that used to live here is gone.

	redisClient, redisErr := sharedcache.NewRedis(ctx, sharedcache.RedisConfig{
		Addr: cfg.Redis.Addr, Username: cfg.Redis.Username, Password: cfg.Redis.Password,
		DB: cfg.Redis.DB, TLS: cfg.Redis.TLSRequired, DialTimeout: cfg.Redis.DialTimeout,
	})
	if redisErr != nil {
		log.Warn("redis not reachable at startup", zap.Error(redisErr))
	}
	sharedcache.SetLeaseClient(redisClient)

	natsConn, err := events.Connect(cfg.Events)
	if err != nil {
		log.Warn("event bus connection failed", zap.Error(err))
	}
	if natsConn != nil {
		// Drop revoked/rotated API keys from every validator on this pod at once.
		_ = eventslib.NewBroadcaster(log, natsConn, "auth").Subscribe("apikey.changed", func(m eventslib.BroadcastMessage) {
			authclient.InvalidateAPIKeyHash(string(m.Data))
		})
	}
	// The projects stream (project.>) must exist before the outbox can publish: without it every
	// project.* event, milestone emails included, had nowhere to go.
	if natsConn != nil {
		if err := events.EnsureStream(ctx, natsConn, cfg.Events); err != nil {
			log.Warn("projects event stream not ensured", zap.Error(err))
		}
	}

	healthHandler := handlers.NewHealthHandler(log, dbPool, redisClient, natsConn)

	// The one runtime pool: ent, the outbox worker, the tenant purger, the tenant export and the
	// field key re-seal all share it.
	runtimeSQLDB, err := sql.Open("pgx", cfg.Postgres.URL)
	if err != nil {
		return nil, fmt.Errorf("sql open for runtime: %w", err)
	}
	runtimeDrv := entsql.OpenDB(dialect.Postgres, runtimeSQLDB)
	entClient := ent.NewClient(ent.Driver(runtimeDrv))

	// Initialize shared cache client
	cacheClient := sharedcache.New(redisClient, log)

	// Layer 3: local RBAC. projects-migrate seeds the registry and system roles on every rollout;
	// seeding again here keeps a pod that starts against an unmigrated copy consistent.
	rbacService := rbac.NewService(entClient, cacheClient, log)
	if err := rbacService.Seed(ctx); err != nil {
		log.Warn("rbac seed at startup failed", zap.Error(err))
	}
	memberships := membership.New(entClient)

	// Initialize all services. Every mutation writes its activity entry and its outbox event
	// (project.*, project.milestone.reached) in the same transaction as the change.
	// Live project updates (SSE): change hints relayed to every pod by the shared Broadcaster.
	liveHub := realtime.NewHub(log, natsConn)
	projectService := projectssvc.NewService(entClient, cacheClient, log)
	taskService := tasks.NewService(entClient, cacheClient, log).WithRealtime(liveHub)
	milestoneService := milestones.NewService(entClient, cacheClient, log)
	memberService := members.NewService(entClient, cacheClient, log)
	memberService.OnChange(memberships.Forget)
	commentService := comments.NewService(entClient, cacheClient, log).WithRealtime(liveHub)
	// Attachments go straight from the browser to the shared object storage (OBJECT_STORAGE_*).
	// The attachments media type takes this service's limits, which VerifyUpload then enforces.
	objectstore.Register(objectstore.MediaType{
		Name: objectstore.TypeAttachments, MaxBytes: cfg.Attachments.MaxBytes,
		Allowed: cfg.Attachments.AllowedTypes, Scan: objectstore.ScanFailClosed,
	})
	var attachmentStore attachments.Store
	if store, err := objectstore.New("projects", objectstore.FromEnv()); err != nil {
		log.Warn("object storage not configured; attachments are unavailable", zap.Error(err))
	} else if store.Remote() {
		attachmentStore = store
	} else {
		log.Warn("object storage endpoint not set; attachment uploads are unavailable")
	}
	attachmentService := attachments.NewService(entClient, attachmentStore, attachments.Limits{
		MaxBytes: cfg.Attachments.MaxBytes, AllowedTypes: cfg.Attachments.AllowedTypes, UploadTTL: cfg.Attachments.UploadTTL,
	}, log).WithRealtime(liveHub)
	tenderService := tenders.NewService(entClient, cacheClient, log)
	syncService := usersync.NewService(cfg.Auth.ServiceURL, cfg.Auth.APIKey, log)
	authEventsConsumer := usersync.NewAuthEventsConsumer(entClient, log)

	// Initialize handlers
	rbacHandler := handlers.NewRBACHandler(log, rbacService, syncService)
	projectHandler := handlers.NewProjectHandler(log, projectService)
	taskHandler := handlers.NewTaskHandler(log, taskService)
	milestoneHandler := handlers.NewMilestoneHandler(log, milestoneService)
	memberHandler := handlers.NewMemberHandler(log, memberService)
	commentHandler := handlers.NewCommentHandler(log, commentService)
	activityHandler := handlers.NewActivityHandler(log, activity.NewService(entClient))
	tenderHandler := handlers.NewTenderHandler(log, tenderService)
	// Project money lives in treasury (budgets, booked costs); financials joins it with task
	// progress for earned value.
	treasuryClient := treasury.NewClient(cfg.Services.TreasuryURL, cfg.Auth.APIKey)
	erpClient := erp.NewClient(cfg.Services.ERPURL, cfg.Auth.APIKey)
	financialsHandler := handlers.NewFinancialsHandler(log, financials.NewService(entClient, treasuryClient, log).WithERP(erpClient))
	// Billing creates draft invoices in treasury from milestones, ERP hours and retainers.
	billingService := billing.NewService(entClient, treasuryClient, erpClient, redisClient, log)
	billingHandler := handlers.NewBillingHandler(log, billingService)
	// Time tracking logs the signed-in user's hours on their ERP timesheet (ERP owns them).
	timeHandler := handlers.NewTimeHandler(log, timetracking.NewService(entClient, erpClient, log).WithStopGuard(redisClient))
	// Project form pickers: the client from the CRM (marketflow), the cost centre from treasury.
	projectHandler.WithLookups(marketflow.NewClient(cfg.Services.MarketflowURL, cfg.Auth.APIKey), treasuryClient)

	// QA and acceptance: templates, sessions, invitees, results, defects and the report.
	// In production QA_HASH_KEY is required: the fallback to INTERNAL_SERVICE_KEY would let any
	// service holding that fleet-wide key forge invitee access tokens, and a random key would
	// break invites across replicas. The fallback stays for local development only.
	if strings.EqualFold(cfg.App.Env, "production") && strings.TrimSpace(cfg.QA.HashKey) == "" {
		return nil, fmt.Errorf("QA_HASH_KEY is required in production (QA invitee tokens and email hashes)")
	}
	qaKeys, ephemeral := qa.NewKeys(cfg.QA.HashKey, cfg.Auth.APIKey)
	if ephemeral {
		log.Warn("QA_HASH_KEY and INTERNAL_SERVICE_KEY are both empty; QA invites will not survive a restart")
	}
	qaService := qa.NewService(entClient, qaKeys, log).
		WithPublisher(events.NewQAPublisher(entClient, log)).
		WithTickets(ticketing.NewClient(cfg.Services.TicketingURL, cfg.Auth.APIKey, log)).
		WithCatalog(catalog.New(cfg.QA.CatalogEnabled, cfg.Services.SubscriptionsURL, cfg.Auth.APIKey, log)).
		WithOTP(authotp.NewClient(cfg.Auth.ServiceURL, cfg.Auth.APIKey)).
		WithUIBaseURL(cfg.QA.UIBaseURL).
		WithAccessTTL(cfg.QA.AccessTTL).
		WithDirectory(authusers.New(cfg.Auth.ServiceURL, cfg.Auth.APIKey, cacheClient, log))
	// Signer addresses are sealed at rest for the close and decision notices.
	// A weak key is refused in production; elsewhere it only costs the notices.
	// FIELD_ENCRYPTION_KEY is the root: it seals the data keys in field_keys, which seal the values
	// once the platform owner has rotated (Security keys page). Never rotated, the root seals them.
	var fieldRoot *fieldcrypt.Box
	var fieldRing *fieldcrypt.Ring
	if box, err := fieldcrypt.New(cfg.QA.FieldEncryptionKey); err == nil {
		fieldRoot, fieldRing = box, fieldcrypt.NewRing(box)
		qaService.WithSealer(fieldRing)
	} else if errors.Is(err, fieldcrypt.ErrWeakKey) {
		if strings.EqualFold(cfg.App.Env, "production") {
			return nil, fmt.Errorf("FIELD_ENCRYPTION_KEY: %w", err)
		}
		log.Warn("FIELD_ENCRYPTION_KEY is too weak; QA signers will not get close and decision notices", zap.Error(err))
	} else {
		log.Warn("FIELD_ENCRYPTION_KEY not set; QA signers will not get close and decision notices")
	}
	fieldKeys := fieldkeys.New(entClient, runtimeSQLDB, fieldRoot, fieldRing,
		authotp.NewClient(cfg.Auth.ServiceURL, cfg.Auth.APIKey), log)
	if err := fieldKeys.Load(ctx); err != nil {
		// Values under a data key stay closed until a reload works; the ring retries on demand.
		log.Error("field encryption data keys not loaded", zap.Error(err))
	}
	// Client portal (sprint 14): invites resolve the client from marketflow and match by the keyed
	// email hash (the QA keys, QA_HASH_KEY), so no address is stored.
	portalService := portal.NewService(entClient, marketflow.NewClient(cfg.Services.MarketflowURL, cfg.Auth.APIKey), qaKeys, log).
		WithEmailVerifier(authusers.New(cfg.Auth.ServiceURL, cfg.Auth.APIKey, cacheClient, log))
	portalService.OnChange(memberships.Forget)
	var portalFiles *attachments.Service
	if attachmentStore != nil {
		portalFiles = attachmentService
	}
	portalHandler := handlers.NewPortalHandler(log, portalService, milestoneService, taskService, commentService, portalFiles).
		WithInvoices(billingService)
	qaRoutes := router.QARoutes{
		Tenant:  handlers.NewQAHandler(log, qaService),
		Invitee: handlers.NewQAInviteeHandler(log, qaService, cfg.QA.CookieSecure),
	}

	// Initialize auth-service JWT validator
	var authMiddleware *authclient.AuthMiddleware
	authConfig := authclient.DefaultConfig(
		cfg.Auth.JWKSUrl,
		cfg.Auth.Issuer,
		cfg.Auth.Audience,
	)
	authConfig.CacheTTL = cfg.Auth.JWKSCacheTTL
	authConfig.RefreshInterval = cfg.Auth.JWKSRefreshInterval

	validator, err := authclient.NewValidator(authConfig)
	if err != nil {
		return nil, fmt.Errorf("auth validator init: %w", err)
	}

	// Initialize API key validator if enabled
	if cfg.Auth.EnableAPIKeyAuth {
		apiKeyValidator := authclient.NewAPIKeyValidator(cfg.Auth.ServiceURL, nil)
		authMiddleware = authclient.NewAuthMiddlewareWithAPIKey(validator, apiKeyValidator)
	} else {
		authMiddleware = authclient.NewAuthMiddleware(validator)
	}

	// The outbox worker drains outbox_events (written inside each domain transaction) to JetStream,
	// on the same pool as ent.
	var outboxPublisher *eventslib.Publisher
	if natsConn != nil {
		js, err := natsConn.JetStream()
		if err != nil {
			log.Warn("failed to get jetstream context, outbox publisher disabled", zap.Error(err))
		} else {
			outboxRepo := eventslib.NewSQLOutboxRepository(runtimeSQLDB)
			outboxPublisher = eventslib.NewPublisher(eventslib.DefaultPublisherConfig(js, outboxRepo, log))
			log.Info("outbox publisher initialized")
			// tenant.purge (dormancy, a deleted tenant, a declined registration): delete every row this
			// service holds for the tenant, by the shared schema-driven purger.
			purger := &eventslib.TenantPurger{DB: runtimeSQLDB, Log: log.Named("tenant-purge"), Service: "projects"}
			if err := purger.Subscribe(js); err != nil {
				log.Warn("tenant purge consumer not started", zap.Error(err))
			}
		}
	}

	chiRouter := router.New(router.Deps{
		Log:     log,
		Service: router.ServiceInfo{Name: cfg.App.Name, Version: cfg.App.Version},
		Handlers: router.Handlers{
			Health: healthHandler, RBAC: rbacHandler, Projects: projectHandler, Tasks: taskHandler,
			Milestones: milestoneHandler, Members: memberHandler, Comments: commentHandler,
			Attachments: handlers.NewAttachmentHandler(log, attachmentService),
			Stream:      handlers.NewStreamHandler(log, liveHub),
			Activity:    activityHandler, Tenders: tenderHandler, Financials: financialsHandler,
			Time:    timeHandler,
			Portal:  portalHandler,
			Billing: billingHandler,
		},
		Authenticate:   authMiddleware.RequireAuth,
		AllowedOrigins: cfg.HTTP.AllowedOrigins,
		Limiter:        ratelimit.NewLimiter(redisClient, log, "projects"),
		Access:         rbacService,
		Members:        memberships,
		Tenants:        handlers.NewTenantResolver(cacheClient, cfg.Auth.ServiceURL),
		TenantExport:   tenantExportHandler(runtimeSQLDB, cfg.Auth.APIKey),
		CRMLinks:       crmLinksHandler(entClient, cacheClient, cfg.Auth.ServiceURL, cfg.Auth.APIKey, log),
		QA:             qaRoutes,
		Platform:       router.PlatformRoutes{FieldKeys: handlers.NewFieldKeyHandler(fieldKeys, log)},
	})

	httpServer := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.HTTP.Host, cfg.HTTP.Port),
		Handler:           chiRouter,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
	}

	return &App{
		cfg:                cfg,
		log:                log,
		httpServer:         httpServer,
		db:                 dbPool,
		cache:              redisClient,
		events:             natsConn,
		outboxPublisher:    outboxPublisher,
		authEventsConsumer: authEventsConsumer,
		// Project.budget follows the approved treasury project budget (treasury.budget.approved).
		budgetEvents: financials.NewBudgetEventsConsumer(entClient, log),
		crmContacts:  crmsync.NewContactEventsConsumer(entClient, marketflow.NewClient(cfg.Services.MarketflowURL, cfg.Auth.APIKey), log),
		qaFeatures:   qa.NewFeatureEventsConsumer(qaService, log),
		fieldKeys:    fieldKeys,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	// Start outbox publisher worker
	if a.outboxPublisher != nil {
		go func() {
			if err := a.outboxPublisher.Start(ctx); err != nil {
				a.log.Error("outbox publisher failed", zap.Error(err))
			}
		}()
		a.log.Info("outbox publisher started")
	}

	if a.authEventsConsumer != nil && a.events != nil {
		go func() {
			if err := a.authEventsConsumer.Start(ctx, a.events); err != nil {
				a.log.Error("auth.user.deleted consumer failed", zap.Error(err))
			}
		}()
		a.log.Info("auth.user.deleted consumer started")
	}
	if a.budgetEvents != nil && a.events != nil {
		go func() {
			if err := a.budgetEvents.Start(ctx, a.events); err != nil {
				a.log.Error("treasury budget events consumer failed", zap.Error(err))
			}
		}()
	}
	if a.crmContacts != nil && a.events != nil {
		go func() {
			if err := a.crmContacts.Start(ctx, a.events); err != nil {
				a.log.Error("crm contact events consumer failed", zap.Error(err))
			}
		}()
	}
	if a.qaFeatures != nil && a.events != nil {
		go func() {
			if err := a.qaFeatures.Start(ctx, a.events); err != nil {
				a.log.Error("qa feature change consumer failed", zap.Error(err))
			}
		}()
	}

	// Reloads the ring and continues an unfinished re-seal; never starts a rotation by itself.
	if a.fieldKeys != nil {
		go a.fieldKeys.Run(ctx)
	}

	errCh := make(chan error, 1)
	if a.cfg.HTTP.TLSCertFile != "" && a.cfg.HTTP.TLSKeyFile != "" {
		a.log.Info("projects service starting with HTTPS",
			zap.String("addr", a.httpServer.Addr),
			zap.String("cert", a.cfg.HTTP.TLSCertFile),
			zap.String("key", a.cfg.HTTP.TLSKeyFile),
		)
		go func() {
			errCh <- a.httpServer.ListenAndServeTLS(a.cfg.HTTP.TLSCertFile, a.cfg.HTTP.TLSKeyFile)
		}()
	} else {
		a.log.Info("projects service starting with HTTP", zap.String("addr", a.httpServer.Addr))
		go func() {
			errCh <- a.httpServer.ListenAndServe()
		}()
	}

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := a.httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("http shutdown: %w", err)
		}

		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("http server error: %w", err)
	}
}

func (a *App) Close() {
	if a.events != nil {
		if err := a.events.Drain(); err != nil {
			a.log.Warn("nats drain failed", zap.Error(err))
		}
		a.events.Close()
	}

	if a.cache != nil {
		if err := a.cache.Close(); err != nil {
			a.log.Warn("redis close failed", zap.Error(err))
		}
	}

	if a.db != nil {
		a.db.Close()
	}

	_ = a.log.Sync()
}
