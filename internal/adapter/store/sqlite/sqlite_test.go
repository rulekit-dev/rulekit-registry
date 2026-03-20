package sqlite_test

import (
	"testing"

	"github.com/rulekit-dev/rulekit-registry/internal/adapter/store/sqlite"
	"github.com/rulekit-dev/rulekit-registry/internal/adapter/store/testhelper"
	"github.com/rulekit-dev/rulekit-registry/internal/port"
)

func TestSQLite(t *testing.T) {
	testhelper.RunSuite(t, func(t *testing.T) port.Datastore {
		s, err := sqlite.New(t.TempDir())
		if err != nil {
			t.Fatalf("sqlite.New: %v", err)
		}
		t.Cleanup(func() { s.Close() })
		return s
	})
}
