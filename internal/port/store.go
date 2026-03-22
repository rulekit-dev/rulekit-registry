package port

import (
	"context"
	"errors"

	"github.com/rulekit-dev/rulekit-registry/internal/domain"
)

var (
	ErrNotFound         = errors.New("not found")
	ErrAlreadyExists    = errors.New("already exists")
	ErrVersionImmutable = errors.New("version already published and is immutable")
)

type Datastore interface {
	// Workspace operations
	CreateWorkspace(ctx context.Context, ws *domain.Workspace) error
	GetWorkspace(ctx context.Context, name string) (*domain.Workspace, error)
	ListWorkspaces(ctx context.Context, limit, offset int) ([]*domain.Workspace, error)
	DeleteWorkspace(ctx context.Context, name string) error

	// Ruleset operations
	ListRulesets(ctx context.Context, workspace string, limit, offset int) ([]*domain.Ruleset, error)
	CreateRuleset(ctx context.Context, r *domain.Ruleset) error
	GetRuleset(ctx context.Context, workspace, key string) (*domain.Ruleset, error)
	DeleteRuleset(ctx context.Context, workspace, key string) error

	// Draft operations
	GetDraft(ctx context.Context, workspace, key string) (*domain.Draft, error)
	UpsertDraft(ctx context.Context, d *domain.Draft) error
	DeleteDraft(ctx context.Context, workspace, key string) error

	// Version operations
	ListVersions(ctx context.Context, workspace, key string, limit, offset int) ([]*domain.Version, error)
	GetVersion(ctx context.Context, workspace, key string, version int) (*domain.Version, error)
	GetLatestVersion(ctx context.Context, workspace, key string) (*domain.Version, error)
	CreateVersion(ctx context.Context, v *domain.Version) error
	NextVersionNumber(ctx context.Context, workspace, key string) (int, error)

	// User operations
	CreateUser(ctx context.Context, u *domain.User) error
	GetUserByEmail(ctx context.Context, email string) (*domain.User, error)
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
	UpdateUserLastLogin(ctx context.Context, userID string) error
	ListUsers(ctx context.Context, limit, offset int) ([]*domain.User, error)
	DeleteUser(ctx context.Context, userID string) error

	// OTP operations
	CreateOTPCode(ctx context.Context, otp *domain.OTPCode) error
	GetUnusedOTPCode(ctx context.Context, userID string) (*domain.OTPCode, error)
	MarkOTPUsed(ctx context.Context, otpID string) error
	DeleteExpiredOTPs(ctx context.Context) error

	// Refresh token operations
	CreateRefreshToken(ctx context.Context, t *domain.RefreshToken) error
	GetRefreshTokenByHash(ctx context.Context, tokenHash string) (*domain.RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, tokenID string) error

	// API key operations
	CreateAPIKey(ctx context.Context, k *domain.APIKey) error
	GetAPIKeyByHash(ctx context.Context, keyHash string) (*domain.APIKey, error)
	ListAPIKeys(ctx context.Context, limit, offset int) ([]*domain.APIKey, error)
	RevokeAPIKey(ctx context.Context, keyID string) error

	// User role operations
	UpsertUserRole(ctx context.Context, ur *domain.UserRole) error
	GetUserRole(ctx context.Context, userID, workspace string) (*domain.UserRole, error)
	ListUserRoles(ctx context.Context, userID string) ([]*domain.UserRole, error)
	DeleteUserRole(ctx context.Context, userID, workspace string) error

	Ping(ctx context.Context) error
	Close() error
}
