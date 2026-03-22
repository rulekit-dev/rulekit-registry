package fs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rulekit-dev/rulekit-registry/internal/port"
)

type FSBlobStore struct {
	baseDir string
}

func New(baseDir string) (*FSBlobStore, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("blobstore/fs: create base dir: %w", err)
	}
	return &FSBlobStore{baseDir: baseDir}, nil
}

func (b *FSBlobStore) dslPath(workspace, key string, version int) string {
	return filepath.Join(b.baseDir, workspace, key, fmt.Sprintf("v%d", version), "dsl.json")
}

func (b *FSBlobStore) bundlePath(workspace, key string, version int) string {
	return filepath.Join(b.baseDir, workspace, key, fmt.Sprintf("v%d", version), "bundle.zip")
}

func (b *FSBlobStore) put(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("blobstore/fs: mkdir: %w", err)
	}
	// Write to a temp file then rename for atomic replacement.
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("blobstore/fs: create temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("blobstore/fs: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("blobstore/fs: close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("blobstore/fs: rename: %w", err)
	}
	return nil
}

func (b *FSBlobStore) delete(path string) error {
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("blobstore/fs: delete: %w", err)
	}
	return nil
}

func (b *FSBlobStore) get(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, port.ErrBlobNotFound
		}
		return nil, fmt.Errorf("blobstore/fs: read: %w", err)
	}
	return data, nil
}

func (b *FSBlobStore) PutDSL(ctx context.Context, workspace, key string, version int, data []byte) error {
	return b.put(b.dslPath(workspace, key, version), data)
}

func (b *FSBlobStore) GetDSL(ctx context.Context, workspace, key string, version int) ([]byte, error) {
	return b.get(b.dslPath(workspace, key, version))
}

func (b *FSBlobStore) DeleteDSL(ctx context.Context, workspace, key string, version int) error {
	return b.delete(b.dslPath(workspace, key, version))
}

func (b *FSBlobStore) PutBundle(ctx context.Context, workspace, key string, version int, data []byte) error {
	return b.put(b.bundlePath(workspace, key, version), data)
}

func (b *FSBlobStore) GetBundle(ctx context.Context, workspace, key string, version int) ([]byte, error) {
	return b.get(b.bundlePath(workspace, key, version))
}

func (b *FSBlobStore) DeleteBundle(ctx context.Context, workspace, key string, version int) error {
	return b.delete(b.bundlePath(workspace, key, version))
}

func (b *FSBlobStore) Close() error { return nil }
