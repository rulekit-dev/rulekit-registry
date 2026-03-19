package blobstore

import (
	"context"
	"errors"
)

var ErrNotFound = errors.New("blob not found")

type BlobStore interface {
	PutDSL(ctx context.Context, namespace, key string, version int, data []byte) error
	GetDSL(ctx context.Context, namespace, key string, version int) ([]byte, error)

	PutBundle(ctx context.Context, namespace, key string, version int, data []byte) error
	GetBundle(ctx context.Context, namespace, key string, version int) ([]byte, error)

	Close() error
}
