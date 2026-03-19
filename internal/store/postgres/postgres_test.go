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
	testhelper.RunSuite(t, func(t *testing.T) store.Store {
		s, err := postgres.New(dsn)
		if err != nil {
			t.Fatalf("postgres.New: %v", err)
		}
		t.Cleanup(func() { s.Close() })
		return s
	})
}
