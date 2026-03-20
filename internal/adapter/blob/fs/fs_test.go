package fs_test

import (
	"testing"

	"github.com/rulekit-dev/rulekit-registry/internal/adapter/blob/fs"
	"github.com/rulekit-dev/rulekit-registry/internal/adapter/blob/testhelper"
	"github.com/rulekit-dev/rulekit-registry/internal/port"
)

func TestFSBlobStore(t *testing.T) {
	testhelper.RunSuite(t, func(t *testing.T) port.BlobStore {
		b, err := fs.New(t.TempDir())
		if err != nil {
			t.Fatalf("fs.New: %v", err)
		}
		t.Cleanup(func() { b.Close() })
		return b
	})
}
