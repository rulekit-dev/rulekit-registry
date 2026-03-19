package sqlite_test

import (
	"testing"

	"github.com/rulekit-dev/rulekit-registry/internal/store"
	"github.com/rulekit-dev/rulekit-registry/internal/store/sqlite"
	"github.com/rulekit-dev/rulekit-registry/internal/store/testhelper"
)

func TestSQLite(t *testing.T) {
	testhelper.RunSuite(t, func(t *testing.T) store.Store {
		s, err := sqlite.New(t.TempDir())
		if err != nil {
			t.Fatalf("sqlite.New: %v", err)
		}
		t.Cleanup(func() { s.Close() })
		return s
	})
}
