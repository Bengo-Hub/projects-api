// Package testutil gives database tests an Ent client on a throwaway schema of a local Postgres
// (PROJECTS_TEST_DATABASE_URL, or the local default). Tests skip when Postgres is unreachable.
package testutil

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib" // register pgx

	"github.com/bengobox/projects-service/internal/ent"
)

// PGClient returns a client on a fresh schema with the Ent schema applied; the schema is
// dropped when the test ends.
func PGClient(t *testing.T) *ent.Client {
	t.Helper()
	url := os.Getenv("PROJECTS_TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"
	}
	admin, err := sql.Open("pgx", url)
	if err != nil || admin.Ping() != nil {
		t.Skip("local postgres unavailable")
	}
	schema := "proj_it_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE"); _ = admin.Close() })
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	db, err := sql.Open("pgx", url+sep+"search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return client
}
