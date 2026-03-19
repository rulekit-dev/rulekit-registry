package fs_test

import (
	"testing"

	"github.com/rulekit-dev/rulekit-registry/internal/blobstore"
	"github.com/rulekit-dev/rulekit-registry/internal/blobstore/fs"
	"github.com/rulekit-dev/rulekit-registry/internal/blobstore/testhelper"
)

func TestFSBlobStore(t *testing.T) {
	testhelper.RunSuite(t, func(t *testing.T) blobstore.BlobStore {
		b, err := fs.New(t.TempDir())
		if err != nil {
			t.Fatalf("fs.New: %v", err)
		}
		t.Cleanup(func() { b.Close() })
		return b
	})
}
