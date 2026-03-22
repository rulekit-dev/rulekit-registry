package service

import (
	"context"
	"encoding/json"

	"github.com/rulekit-dev/rulekit-registry/internal/domain"
)

// WorkspaceUseCase is the inbound port for workspace management.
type WorkspaceUseCase interface {
	CreateWorkspace(ctx context.Context, name, description string) (*domain.Workspace, error)
	GetWorkspace(ctx context.Context, name string) (*domain.Workspace, error)
	ListWorkspaces(ctx context.Context, limit, offset int) ([]*domain.Workspace, error)
	DeleteWorkspace(ctx context.Context, name string) error
}

// RulesetUseCase is the inbound port for all ruleset operations.
type RulesetUseCase interface {
	ListRulesets(ctx context.Context, workspace string, limit, offset int) ([]*domain.Ruleset, error)
	CreateRuleset(ctx context.Context, workspace, key, name, description string) (*domain.Ruleset, error)
	GetRuleset(ctx context.Context, workspace, key string) (*domain.Ruleset, error)
	DeleteRuleset(ctx context.Context, workspace, key string) error

	GetDraft(ctx context.Context, workspace, key string) (*domain.Draft, error)
	UpsertDraft(ctx context.Context, workspace, key string, rawDSL json.RawMessage) (*domain.Draft, error)
	DeleteDraft(ctx context.Context, workspace, key string) error

	Publish(ctx context.Context, workspace, key string) (*domain.Version, error)
	ListVersions(ctx context.Context, workspace, key string, limit, offset int) ([]*domain.Version, error)
	GetVersion(ctx context.Context, workspace, key string, versionNum int) (*domain.Version, error)
	GetLatestVersion(ctx context.Context, workspace, key string) (*domain.Version, error)
	GetVersionBundle(ctx context.Context, workspace, key string, versionNum int) ([]byte, error)
	GetLatestBundle(ctx context.Context, workspace, key string) (int, []byte, error)
}

// AuthUseCase is the inbound port for authentication operations.
type AuthUseCase interface {
	AdminLogin(ctx context.Context, password string) (*TokenPair, error)
	Login(ctx context.Context, email string) error
	Verify(ctx context.Context, email, code string) (*TokenPair, error)
	Refresh(ctx context.Context, rawRefreshToken string) (*TokenPair, error)
	Logout(ctx context.Context, rawRefreshToken string) error
}

// AdminUseCase is the inbound port for admin operations.
type AdminUseCase interface {
	ListUsers(ctx context.Context, limit, offset int) ([]*domain.User, error)
	DeleteUser(ctx context.Context, userID string) error

	ListUserRoles(ctx context.Context, userID string) ([]*domain.UserRole, error)
	UpsertUserRole(ctx context.Context, userID, workspace string, roleMask domain.Role) (*domain.UserRole, error)
	DeleteUserRole(ctx context.Context, userID, workspace string) error

	CreateAPIKey(ctx context.Context, name, workspace string, role domain.Role, expiresInDays int) (*CreatedKey, error)
	ListAPIKeys(ctx context.Context, limit, offset int) ([]*domain.APIKey, error)
	RevokeAPIKey(ctx context.Context, keyID string) error
}
