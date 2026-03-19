// Package testhelper provides a shared acceptance test suite for blobstore.BlobStore
// implementations.
package testhelper

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/rulekit-dev/rulekit-registry/internal/blobstore"
)

// RunSuite runs the full acceptance test suite against the BlobStore returned by
// newStore. newStore is called once per sub-test so each test gets a clean instance.
func RunSuite(t *testing.T, newStore func(t *testing.T) blobstore.BlobStore) {
	t.Helper()

	t.Run("PutAndGetDSL", func(t *testing.T) {
		t.Parallel()
		b := newStore(t)
		ctx := context.Background()

		data := []byte(`{"dsl_version":"v1"}`)
		if err := b.PutDSL(ctx, "default", "my-ruleset", 1, data); err != nil {
			t.Fatalf("PutDSL: %v", err)
		}

		got, err := b.GetDSL(ctx, "default", "my-ruleset", 1)
		if err != nil {
			t.Fatalf("GetDSL: %v", err)
		}
		if !bytes.Equal(got, data) {
			t.Errorf("GetDSL: got %q, want %q", got, data)
		}
	})

	t.Run("PutAndGetBundle", func(t *testing.T) {
		t.Parallel()
		b := newStore(t)
		ctx := context.Background()

		data := []byte("PK\x03\x04bundle-zip-content")
		if err := b.PutBundle(ctx, "default", "my-ruleset", 1, data); err != nil {
			t.Fatalf("PutBundle: %v", err)
		}

		got, err := b.GetBundle(ctx, "default", "my-ruleset", 1)
		if err != nil {
			t.Fatalf("GetBundle: %v", err)
		}
		if !bytes.Equal(got, data) {
			t.Errorf("GetBundle: got %q, want %q", got, data)
		}
	})

	t.Run("GetDSLNotFound", func(t *testing.T) {
		t.Parallel()
		b := newStore(t)
		ctx := context.Background()

		_, err := b.GetDSL(ctx, "default", "nonexistent", 1)
		if !errors.Is(err, blobstore.ErrNotFound) {
			t.Errorf("GetDSL nonexistent: got %v, want ErrNotFound", err)
		}
	})

	t.Run("GetBundleNotFound", func(t *testing.T) {
		t.Parallel()
		b := newStore(t)
		ctx := context.Background()

		_, err := b.GetBundle(ctx, "default", "nonexistent", 1)
		if !errors.Is(err, blobstore.ErrNotFound) {
			t.Errorf("GetBundle nonexistent: got %v, want ErrNotFound", err)
		}
	})

	t.Run("MultipleVersionsIsolated", func(t *testing.T) {
		t.Parallel()
		b := newStore(t)
		ctx := context.Background()

		v1 := []byte(`{"version":1}`)
		v2 := []byte(`{"version":2}`)

		if err := b.PutDSL(ctx, "default", "ruleset", 1, v1); err != nil {
			t.Fatalf("PutDSL v1: %v", err)
		}
		if err := b.PutDSL(ctx, "default", "ruleset", 2, v2); err != nil {
			t.Fatalf("PutDSL v2: %v", err)
		}

		got1, err := b.GetDSL(ctx, "default", "ruleset", 1)
		if err != nil {
			t.Fatalf("GetDSL v1: %v", err)
		}
		if !bytes.Equal(got1, v1) {
			t.Errorf("GetDSL v1: got %q, want %q", got1, v1)
		}

		got2, err := b.GetDSL(ctx, "default", "ruleset", 2)
		if err != nil {
			t.Fatalf("GetDSL v2: %v", err)
		}
		if !bytes.Equal(got2, v2) {
			t.Errorf("GetDSL v2: got %q, want %q", got2, v2)
		}
	})

	t.Run("NamespaceIsolation", func(t *testing.T) {
		t.Parallel()
		b := newStore(t)
		ctx := context.Background()

		nsA := []byte(`{"ns":"a"}`)
		nsB := []byte(`{"ns":"b"}`)

		if err := b.PutDSL(ctx, "ns-a", "ruleset", 1, nsA); err != nil {
			t.Fatalf("PutDSL ns-a: %v", err)
		}
		if err := b.PutDSL(ctx, "ns-b", "ruleset", 1, nsB); err != nil {
			t.Fatalf("PutDSL ns-b: %v", err)
		}

		gotA, err := b.GetDSL(ctx, "ns-a", "ruleset", 1)
		if err != nil {
			t.Fatalf("GetDSL ns-a: %v", err)
		}
		if !bytes.Equal(gotA, nsA) {
			t.Errorf("GetDSL ns-a: got %q, want %q", gotA, nsA)
		}

		gotB, err := b.GetDSL(ctx, "ns-b", "ruleset", 1)
		if err != nil {
			t.Fatalf("GetDSL ns-b: %v", err)
		}
		if !bytes.Equal(gotB, nsB) {
			t.Errorf("GetDSL ns-b: got %q, want %q", gotB, nsB)
		}
	})

	t.Run("OverwriteDSL", func(t *testing.T) {
		t.Parallel()
		b := newStore(t)
		ctx := context.Background()

		original := []byte(`{"original":true}`)
		updated := []byte(`{"updated":true}`)

		if err := b.PutDSL(ctx, "default", "ruleset", 1, original); err != nil {
			t.Fatalf("PutDSL original: %v", err)
		}
		if err := b.PutDSL(ctx, "default", "ruleset", 1, updated); err != nil {
			t.Fatalf("PutDSL updated: %v", err)
		}

		got, err := b.GetDSL(ctx, "default", "ruleset", 1)
		if err != nil {
			t.Fatalf("GetDSL after overwrite: %v", err)
		}
		if !bytes.Equal(got, updated) {
			t.Errorf("GetDSL after overwrite: got %q, want %q", got, updated)
		}
	})
}
