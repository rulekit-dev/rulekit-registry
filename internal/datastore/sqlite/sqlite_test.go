package sqlite_test

import (
	"testing"

	"github.com/rulekit-dev/rulekit-registry/internal/datastore"
	"github.com/rulekit-dev/rulekit-registry/internal/datastore/sqlite"
	"github.com/rulekit-dev/rulekit-registry/internal/datastore/testhelper"
)

func TestSQLite(t *testing.T) {
	testhelper.RunSuite(t, func(t *testing.T) datastore.Datastore {
		s, err := sqlite.New(t.TempDir())
		if err != nil {
			t.Fatalf("sqlite.New: %v", err)
		}
		t.Cleanup(func() { s.Close() })
		return s
	})
}
