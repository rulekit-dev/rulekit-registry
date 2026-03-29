package mock

import (
	"context"
	"sync"

	"github.com/rulekit-dev/rulekit-registry/internal/port"
)

// BlobStore is an in-memory implementation of port.BlobStore for testing.
type BlobStore struct {
	mu   sync.Mutex
	dsls map[string][]byte // "ws\x00key\x00version"
	zips map[string][]byte
}

func NewBlobStore() *BlobStore {
	return &BlobStore{
		dsls: make(map[string][]byte),
		zips: make(map[string][]byte),
	}
}

func blobKey(workspace, rkey string, version int) string {
	return workspace + "\x00" + rkey + "\x00" + string(rune(version))
}

func (b *BlobStore) PutDSL(_ context.Context, workspace, rkey string, version int, data []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	b.dsls[blobKey(workspace, rkey, version)] = cp
	return nil
}

func (b *BlobStore) GetDSL(_ context.Context, workspace, rkey string, version int) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, ok := b.dsls[blobKey(workspace, rkey, version)]
	if !ok {
		return nil, port.ErrBlobNotFound
	}
	return data, nil
}

func (b *BlobStore) DeleteDSL(_ context.Context, workspace, rkey string, version int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.dsls, blobKey(workspace, rkey, version))
	return nil
}

func (b *BlobStore) PutBundle(_ context.Context, workspace, rkey string, version int, data []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	b.zips[blobKey(workspace, rkey, version)] = cp
	return nil
}

func (b *BlobStore) GetBundle(_ context.Context, workspace, rkey string, version int) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, ok := b.zips[blobKey(workspace, rkey, version)]
	if !ok {
		return nil, port.ErrBlobNotFound
	}
	return data, nil
}

func (b *BlobStore) DeleteBundle(_ context.Context, workspace, rkey string, version int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.zips, blobKey(workspace, rkey, version))
	return nil
}

func (b *BlobStore) Close() error { return nil }
