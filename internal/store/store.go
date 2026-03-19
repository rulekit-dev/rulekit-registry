// Package store defines the storage abstraction for rulekit-registry.
package store

import (
	"context"
	"errors"

	"github.com/rulekit/rulekit-registry/internal/model"
)

// Sentinel errors returned by Store implementations.
var (
	ErrNotFound         = errors.New("not found")
	ErrAlreadyExists    = errors.New("already exists")
	ErrVersionImmutable = errors.New("version already published and is immutable")
)

type Store interface {
	// Ruleset operations
	ListRulesets(ctx context.Context, namespace string, limit, offset int) ([]*model.Ruleset, error)
	CreateRuleset(ctx context.Context, r *model.Ruleset) error
	GetRuleset(ctx context.Context, namespace, key string) (*model.Ruleset, error)
	DeleteRuleset(ctx context.Context, namespace, key string) error

	// Draft operations
	GetDraft(ctx context.Context, namespace, key string) (*model.Draft, error)
	UpsertDraft(ctx context.Context, d *model.Draft) error
	DeleteDraft(ctx context.Context, namespace, key string) error

	// Version operations
	ListVersions(ctx context.Context, namespace, key string, limit, offset int) ([]*model.Version, error)
	GetVersion(ctx context.Context, namespace, key string, version int) (*model.Version, error)
	GetLatestVersion(ctx context.Context, namespace, key string) (*model.Version, error)
	CreateVersion(ctx context.Context, v *model.Version) error
	NextVersionNumber(ctx context.Context, namespace, key string) (int, error)

	Close() error
}
