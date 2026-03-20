package port

import (
	"context"
	"errors"
)

var ErrBlobNotFound = errors.New("blob not found")

type BlobStore interface {
	PutDSL(ctx context.Context, namespace, key string, version int, data []byte) error
	GetDSL(ctx context.Context, namespace, key string, version int) ([]byte, error)
	DeleteDSL(ctx context.Context, namespace, key string, version int) error

	PutBundle(ctx context.Context, namespace, key string, version int, data []byte) error
	GetBundle(ctx context.Context, namespace, key string, version int) ([]byte, error)
	DeleteBundle(ctx context.Context, namespace, key string, version int) error

	Close() error
}
