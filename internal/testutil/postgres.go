package testutil

import (
	"context"
	"fmt"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/bengobox/projects-service/internal/ent"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// PostgresContainer holds the container instance and connection details.
type PostgresContainer struct {
	Container testcontainers.Container
	URI       string
	Host      string
	Port      string
	Database  string
	Username  string
	Password  string
}

// PostgresConfig holds configuration for the test container.
type PostgresConfig struct {
	Database string
	Username string
	Password string
}

// DefaultPostgresConfig returns sensible defaults for testing.
func DefaultPostgresConfig() PostgresConfig {
	return PostgresConfig{
		Database: "projects_test",
		Username: "test",
		Password: "test",
	}
}

// SetupPostgres creates a PostgreSQL container for testing.
// It returns the container details and a cleanup function.
func SetupPostgres(ctx context.Context, t *testing.T, cfg PostgresConfig) (*PostgresContainer, func()) {
	t.Helper()

	container, err := postgres.Run(ctx,
		"postgres:15-alpine",
		postgres.WithDatabase(cfg.Database),
		postgres.WithUsername(cfg.Username),
		postgres.WithPassword(cfg.Password),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("failed to get container host: %v", err)
	}

	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		t.Fatalf("failed to get container port: %v", err)
	}

	uri := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		cfg.Username, cfg.Password, host, port.Port(), cfg.Database)

	cleanup := func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("failed to terminate postgres container: %v", err)
		}
	}

	return &PostgresContainer{
		Container: container,
		URI:       uri,
		Host:      host,
		Port:      port.Port(),
		Database:  cfg.Database,
		Username:  cfg.Username,
		Password:  cfg.Password,
	}, cleanup
}

// SetupEntClient creates an Ent client connected to the test database.
// It also runs migrations to ensure the schema is up to date.
func SetupEntClient(ctx context.Context, t *testing.T, pg *PostgresContainer) *ent.Client {
	t.Helper()

	client, err := ent.Open(dialect.Postgres, pg.URI)
	if err != nil {
		t.Fatalf("failed to connect to postgres: %v", err)
	}

	// Run migrations
	if err := client.Schema.Create(ctx); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	return client
}

// SetupTestDB is a convenience function that sets up both the container and client.
// Returns the Ent client and a cleanup function.
func SetupTestDB(t *testing.T) (*ent.Client, func()) {
	t.Helper()

	ctx := context.Background()
	cfg := DefaultPostgresConfig()

	pg, cleanupPg := SetupPostgres(ctx, t, cfg)

	client := SetupEntClient(ctx, t, pg)

	cleanup := func() {
		client.Close()
		cleanupPg()
	}

	return client, cleanup
}
