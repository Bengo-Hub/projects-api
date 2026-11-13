package main

import (
	"database/sql"
	"log"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"

	"github.com/bengobox/projects-service/internal/config"
	"github.com/bengobox/projects-service/internal/ent"
)

func main() {
	_ = godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// Bypass PgBouncer for direct connection during seed.
	dbURL := cfg.Postgres.URL
	if cfg.Postgres.MigrateURL != "" {
		dbURL = cfg.Postgres.MigrateURL
	}

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	drv := entsql.OpenDB(dialect.Postgres, db)
	client := ent.NewClient(ent.Driver(drv))
	defer client.Close()

	_ = client // use client for seeding once Ent schema is finalized

	// Seed default roles
	defaultRoles := []struct {
		code        string
		name        string
		description string
		permissions []string
	}{
		// QA and acceptance codes (projects.qa.view, run, manage) are also carried in the auth-api
		// token; tenant admins pass without them.
		{"admin", "Admin", "Full access to all projects and settings", []string{"projects:read", "projects:write", "projects:delete", "projects:manage", "projects.qa.view", "projects.qa.run", "projects.qa.manage"}},
		{"member", "Member", "Can create and manage assigned projects", []string{"projects:read", "projects:write", "projects.qa.view", "projects.qa.run"}},
		{"viewer", "Viewer", "Read-only access to projects", []string{"projects:read", "projects.qa.view"}},
	}

	for _, r := range defaultRoles {
		log.Printf("Would seed role: %s - %s", r.code, r.name)
	}

	log.Println("seed completed")
}
