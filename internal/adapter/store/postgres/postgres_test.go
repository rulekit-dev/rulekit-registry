package postgres_test

import (
	"os"
	"testing"

	"github.com/rulekit-dev/rulekit-registry/internal/adapter/store/postgres"
	"github.com/rulekit-dev/rulekit-registry/internal/adapter/store/testhelper"
	"github.com/rulekit-dev/rulekit-registry/internal/port"
)

func TestPostgres(t *testing.T) {
	dsn := os.Getenv("RULEKIT_DATABASE_URL")
	if dsn == "" {
		t.Skip("RULEKIT_DATABASE_URL not set; skipping PostgreSQL tests")
	}

	// Open and migrate once; each subtest gets the same store.
	// Data isolation is guaranteed by unique workspace in the shared suite.
	shared, err := postgres.New(dsn)
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { shared.Close() })

	testhelper.RunSuite(t, func(t *testing.T) port.Datastore {
		return shared
	})
}
