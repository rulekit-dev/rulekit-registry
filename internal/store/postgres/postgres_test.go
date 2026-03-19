package postgres_test

import (
	"os"
	"testing"

	"github.com/rulekit-dev/rulekit-registry/internal/store"
	"github.com/rulekit-dev/rulekit-registry/internal/store/postgres"
	"github.com/rulekit-dev/rulekit-registry/internal/store/testhelper"
)

func TestPostgres(t *testing.T) {
	dsn := os.Getenv("RULEKIT_DATABASE_URL")
	if dsn == "" {
		t.Skip("RULEKIT_DATABASE_URL not set; skipping PostgreSQL tests")
	}

	// Open and migrate once; each subtest gets the same store.
	// Data isolation is guaranteed by unique namespaces in the shared suite.
	shared, err := postgres.New(dsn)
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { shared.Close() })

	testhelper.RunSuite(t, func(t *testing.T) store.Store {
		return shared
	})
}
