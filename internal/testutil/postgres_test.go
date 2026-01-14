package testutil

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	cfg := DefaultPostgresConfig()

	pg, cleanup := SetupPostgres(ctx, t, cfg)
	defer cleanup()

	// Verify container is running and we have connection details
	assert.NotEmpty(t, pg.URI, "URI should not be empty")
	assert.NotEmpty(t, pg.Host, "Host should not be empty")
	assert.NotEmpty(t, pg.Port, "Port should not be empty")
	assert.Equal(t, "projects_test", pg.Database)
	assert.Equal(t, "test", pg.Username)
}

func TestSetupTestDB(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := SetupTestDB(t)
	defer cleanup()

	// Verify client is connected by running a simple query
	require.NotNil(t, client, "Ent client should not be nil")

	// Test that we can interact with the database
	ctx := context.Background()

	// Query should work without error (even if empty result)
	roles, err := client.Role.Query().All(ctx)
	require.NoError(t, err, "Should be able to query roles table")
	assert.Empty(t, roles, "Roles table should be empty initially")
}
