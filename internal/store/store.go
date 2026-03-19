// Package store defines the storage abstraction for rulekit-registry.
package store

import (
	"context"
	"errors"

	"github.com/rulekit-dev/rulekit-registry/internal/model"
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

	// User operations
	CreateUser(ctx context.Context, u *model.User) error
	GetUserByEmail(ctx context.Context, email string) (*model.User, error)
	GetUserByID(ctx context.Context, id string) (*model.User, error)
	UpdateUserLastLogin(ctx context.Context, userID string) error
	ListUsers(ctx context.Context, limit, offset int) ([]*model.User, error)
	DeleteUser(ctx context.Context, userID string) error

	// OTP operations
	CreateOTPCode(ctx context.Context, otp *model.OTPCode) error
	GetUnusedOTPCode(ctx context.Context, userID string) (*model.OTPCode, error)
	MarkOTPUsed(ctx context.Context, otpID string) error
	DeleteExpiredOTPs(ctx context.Context) error

	// API token operations
	CreateAPIToken(ctx context.Context, t *model.APIToken) error
	GetAPITokenByHash(ctx context.Context, tokenHash string) (*model.APIToken, error)
	ListAPITokens(ctx context.Context, userID string) ([]*model.APIToken, error)
	RevokeAPIToken(ctx context.Context, tokenID string) error

	// User role operations
	UpsertUserRole(ctx context.Context, ur *model.UserRole) error
	GetUserRole(ctx context.Context, userID, namespace string) (*model.UserRole, error)
	ListUserRoles(ctx context.Context, userID string) ([]*model.UserRole, error)
	DeleteUserRole(ctx context.Context, userID, namespace string) error

	Close() error
}
