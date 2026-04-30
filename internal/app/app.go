package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	authclient "github.com/Bengo-Hub/shared-auth-client"
	eventslib "github.com/Bengo-Hub/shared-events"

	"github.com/bengobox/projects-service/internal/config"
	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/migrate"
	handlers "github.com/bengobox/projects-service/internal/http/handlers"
	router "github.com/bengobox/projects-service/internal/http/router"
	"github.com/bengobox/projects-service/internal/modules/outbox"
	"github.com/bengobox/projects-service/internal/platform/cache"
	"github.com/bengobox/projects-service/internal/platform/database"
	"github.com/bengobox/projects-service/internal/platform/events"
	"github.com/bengobox/projects-service/internal/services/rbac"
	"github.com/bengobox/projects-service/internal/services/usersync"
	"github.com/bengobox/projects-service/internal/shared/logger"

	"database/sql"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type App struct {
	cfg            *config.Config
	log            *zap.Logger
	httpServer     *http.Server
	db             *pgxpool.Pool
	cache          *redis.Client
	events         *nats.Conn
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

	dbPool, err := database.NewPool(ctx, cfg.Postgres)
	if err != nil {
		return nil, fmt.Errorf("postgres init: %w", err)
	}

	if cfg.Postgres.RunMigrations {
		migrateURL := cfg.Postgres.URL
		if cfg.Postgres.MigrateURL != "" {
			migrateURL = cfg.Postgres.MigrateURL
		}
		sqlDB, err := sql.Open("pgx", migrateURL)
		if err != nil {
			return nil, fmt.Errorf("sql open for migrations: %w", err)
		}
		defer sqlDB.Close()
		drv := entsql.OpenDB(dialect.Postgres, sqlDB)
		entClient := ent.NewClient(ent.Driver(drv))
		defer entClient.Close()
		if err := entClient.Schema.Create(ctx, schema.WithDir(migrate.Dir)); err != nil {
			return nil, fmt.Errorf("ent migrate: %w", err)
		}
		log.Info("versioned migrations completed")
	}

	redisClient := cache.NewClient(cfg.Redis)

	natsConn, err := events.Connect(cfg.Events)
	if err != nil {
		log.Warn("event bus connection failed", zap.Error(err))
	}

	healthHandler := handlers.NewHealthHandler(log, dbPool, redisClient, natsConn)

	// Initialize user management services
	rbacService := rbac.NewService(log)
	syncService := usersync.NewService(cfg.Auth.ServiceURL, cfg.Auth.APIKey, log)
	userHandler := handlers.NewUserHandler(log, rbacService, syncService)

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

	// Initialize outbox publisher
	var outboxPublisher *eventslib.Publisher
	if natsConn != nil && dbPool != nil {
		js, err := natsConn.JetStream()
		if err != nil {
			log.Warn("failed to get jetstream context, outbox publisher disabled", zap.Error(err))
		} else {
			// Get underlying sql.DB for outbox repository
			sqlDB, err := sql.Open("pgx", cfg.Postgres.URL)
			if err == nil {
				outboxRepo := outbox.NewRepository(sqlDB)
				pubCfg := eventslib.DefaultPublisherConfig(js, outboxRepo, log)
				outboxPublisher = eventslib.NewPublisher(pubCfg)
				log.Info("outbox publisher initialized")
			} else {
				log.Warn("failed to create sql.DB for outbox, publisher disabled", zap.Error(err))
			}
		}
	}

	chiRouter := router.New(log, healthHandler, userHandler, authMiddleware, cfg.HTTP.AllowedOrigins)

	httpServer := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.HTTP.Host, cfg.HTTP.Port),
		Handler:           chiRouter,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
	}

	return &App{
		cfg:            cfg,
		log:            log,
		httpServer:     httpServer,
		db:             dbPool,
		cache:          redisClient,
		events:         natsConn,
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
