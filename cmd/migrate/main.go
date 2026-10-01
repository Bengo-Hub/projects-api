package main

import (
	"context"
	"database/sql"
	"log"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"

	"github.com/bengobox/projects-service/internal/config"
	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/migrate"
	"github.com/bengobox/projects-service/internal/platform/database"
)

// migrationLockKey serializes migrations across pods: every replica's entrypoint runs this
// binary and concurrent schema runs can race on the same DDL. Stable and unique per service
// ("PRJM", projects migrate).
const migrationLockKey int64 = 0x5052_4A4D

func main() {
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// Prefer direct PostgreSQL URL to bypass PgBouncer during migrations.
	dbURL := cfg.Postgres.URL
	if cfg.Postgres.MigrateURL != "" {
		dbURL = cfg.Postgres.MigrateURL
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	// One connection: the advisory lock and every migration statement share one session.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrationLockKey); err != nil {
		log.Fatalf("acquire migration lock: %v", err)
	}
	unlock := func() {
		if _, err := db.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", migrationLockKey); err != nil {
			log.Printf("release migration lock: %v", err)
		}
	}

	if err := database.DropRetiredTables(ctx, db); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	drv := entsql.OpenDB(dialect.Postgres, db)
	client := ent.NewClient(ent.Driver(drv))
	defer client.Close()

	migrateErr := client.Schema.Create(ctx, schema.WithDir(migrate.Dir))
	unlock()
	if migrateErr != nil {
		log.Fatalf("migrate: %v", migrateErr)
	}
	log.Println("migrations completed")
}
