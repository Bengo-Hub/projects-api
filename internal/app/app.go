package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	authclient "github.com/Bengo-Hub/shared-auth-client"
	eventslib "github.com/Bengo-Hub/shared-events"

	"github.com/bengobox/projects-service/internal/config"
	"github.com/bengobox/projects-service/internal/ent"
	handlers "github.com/bengobox/projects-service/internal/http/handlers"
	router "github.com/bengobox/projects-service/internal/http/router"
	"github.com/bengobox/projects-service/internal/modules/outbox"
	"github.com/bengobox/projects-service/internal/platform/cache"
	"github.com/bengobox/projects-service/internal/platform/database"
	"github.com/bengobox/projects-service/internal/platform/events"
	"github.com/bengobox/projects-service/internal/services/project"
	"github.com/bengobox/projects-service/internal/services/rbac"
	"github.com/bengobox/projects-service/internal/services/task"
	"github.com/bengobox/projects-service/internal/services/tender"
	"github.com/bengobox/projects-service/internal/services/usersync"
	"github.com/bengobox/projects-service/internal/shared/logger"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type App struct {
	cfg             *config.Config
	log             *zap.Logger
	httpServer      *http.Server
	db              *ent.Client
	cache           *redis.Client
	events          *nats.Conn
	outboxPublisher *eventslib.Publisher
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

	dbClient, err := database.NewClient(ctx, cfg.Postgres)
	if err != nil {
		return nil, fmt.Errorf("postgres init: %w", err)
	}

	// Run database migrations
	if err := database.RunMigrations(ctx, dbClient); err != nil {
		return nil, fmt.Errorf("migrations: %w", err)
	}

	redisClient := cache.NewClient(cfg.Redis)

	natsConn, err := events.Connect(cfg.Events)
	if err != nil {
		log.Warn("event bus connection failed", zap.Error(err))
	}

	healthHandler := handlers.NewHealthHandler(log, dbClient, redisClient, natsConn)

	// Initialize user management services
	rbacService := rbac.NewService(log, dbClient)
	syncService := usersync.NewService(cfg.Auth.ServiceURL, cfg.Auth.APIKey, log)
	userHandler := handlers.NewUserHandler(log, rbacService, syncService)

	// Initialize project service and handler
	projectService := project.NewService(log, dbClient)
	projectHandler := handlers.NewProjectHandler(log, projectService)

	// Initialize task service and handler
	taskService := task.NewService(log, dbClient)
	taskHandler := handlers.NewTaskHandler(log, taskService)

	// Initialize tender service and handlers
	// We'll create the event-aware service after outbox initialization
	tenderService := tender.NewService(log, dbClient)

	// Initialize docs handler
	docsHandler := handlers.NewDocsHandler()

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

	// Initialize outbox publisher and event-aware tender service
	var outboxPublisher *eventslib.Publisher
	var eventAwareTenderService *tender.EventAwareService
	var sqlDB *sql.DB

	if natsConn != nil && dbClient != nil {
		js, err := natsConn.JetStream()
		if err != nil {
			log.Warn("failed to get jetstream context, outbox publisher disabled", zap.Error(err))
		} else {
			// Get underlying sql.DB for outbox repository
			sqlDB, err = sql.Open("pgx", cfg.Postgres.URL)
			if err == nil {
				outboxRepo := outbox.NewRepository(sqlDB)
				pubCfg := eventslib.DefaultPublisherConfig(js, outboxRepo, log)
				outboxPublisher = eventslib.NewPublisher(pubCfg)
				log.Info("outbox publisher initialized")

				// Create event publisher for tender service
				eventPublisher := tender.NewOutboxEventPublisher(outboxRepo)
				eventAwareTenderService = tender.NewEventAwareService(log, dbClient, sqlDB, eventPublisher)
				log.Info("event-aware tender service initialized")
			} else {
				log.Warn("failed to create sql.DB for outbox, publisher disabled", zap.Error(err))
			}
		}
	}

	// Initialize tender handlers with event-aware service if available, fallback to basic service
	var tenderHandler *handlers.TenderHandler
	var tenderDocumentHandler *handlers.TenderDocumentHandler
	var tenderCommitteeHandler *handlers.TenderCommitteeHandler
	var tenderEvaluationHandler *handlers.TenderEvaluationHandler
	var tenderMeetingHandler *handlers.TenderMeetingHandler
	var tenderSectionHandler *handlers.TenderSectionHandler
	var tenderSubmissionHandler *handlers.TenderSubmissionHandler

	if eventAwareTenderService != nil {
		// Use event-aware service (publishes domain events)
		tenderHandler = handlers.NewTenderHandler(log, eventAwareTenderService)
		tenderDocumentHandler = handlers.NewTenderDocumentHandler(log, eventAwareTenderService)
		tenderCommitteeHandler = handlers.NewTenderCommitteeHandler(log, eventAwareTenderService)
		tenderEvaluationHandler = handlers.NewTenderEvaluationHandler(log, eventAwareTenderService)
		tenderMeetingHandler = handlers.NewTenderMeetingHandler(log, eventAwareTenderService)
		tenderSectionHandler = handlers.NewTenderSectionHandler(log, eventAwareTenderService)
		tenderSubmissionHandler = handlers.NewTenderSubmissionHandler(log, eventAwareTenderService)
	} else {
		// Fallback to basic service (no events)
		log.Warn("using basic tender service without event publishing")
		tenderHandler = handlers.NewTenderHandler(log, tenderService)
		tenderDocumentHandler = handlers.NewTenderDocumentHandler(log, tenderService)
		tenderCommitteeHandler = handlers.NewTenderCommitteeHandler(log, tenderService)
		tenderEvaluationHandler = handlers.NewTenderEvaluationHandler(log, tenderService)
		tenderMeetingHandler = handlers.NewTenderMeetingHandler(log, tenderService)
		tenderSectionHandler = handlers.NewTenderSectionHandler(log, tenderService)
		tenderSubmissionHandler = handlers.NewTenderSubmissionHandler(log, tenderService)
	}

	chiRouter := router.New(router.Config{
		Log: log,
		Handlers: router.Handlers{
			Health:           healthHandler,
			User:             userHandler,
			Project:          projectHandler,
			Task:             taskHandler,
			Tender:           tenderHandler,
			TenderDocument:   tenderDocumentHandler,
			TenderCommittee:  tenderCommitteeHandler,
			TenderEvaluation: tenderEvaluationHandler,
			TenderMeeting:    tenderMeetingHandler,
			TenderSection:    tenderSectionHandler,
			TenderSubmission: tenderSubmissionHandler,
			Docs:             docsHandler,
		},
		AuthMiddleware: authMiddleware,
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
		cfg:             cfg,
		log:             log,
		httpServer:      httpServer,
		db:              dbClient,
		cache:           redisClient,
		events:          natsConn,
		outboxPublisher: outboxPublisher,
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
