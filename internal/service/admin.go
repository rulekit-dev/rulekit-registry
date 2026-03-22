package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/rulekit-dev/rulekit-registry/internal/domain"
	"github.com/rulekit-dev/rulekit-registry/internal/port"
)

type AdminService struct {
	db port.Datastore
}

func NewAdminService(db port.Datastore) *AdminService {
	return &AdminService{db: db}
}

func (s *AdminService) ListUsers(ctx context.Context, limit, offset int) ([]*domain.User, error) {
	users, err := s.db.ListUsers(ctx, limit, offset)
	if err != nil {
		slog.ErrorContext(ctx, "list users", "error", err)
		return nil, err
	}
	if users == nil {
		users = []*domain.User{}
	}
	return users, nil
}

func (s *AdminService) DeleteUser(ctx context.Context, userID string) error {
	if err := s.db.DeleteUser(ctx, userID); err != nil {
		slog.ErrorContext(ctx, "delete user", "user_id", userID, "error", err)
		return mapErr(err)
	}
	return nil
}

func (s *AdminService) ListUserRoles(ctx context.Context, userID string) ([]*domain.UserRole, error) {
	roles, err := s.db.ListUserRoles(ctx, userID)
	if err != nil {
		slog.ErrorContext(ctx, "list user roles", "user_id", userID, "error", err)
		return nil, err
	}
	if roles == nil {
		roles = []*domain.UserRole{}
	}
	return roles, nil
}

func (s *AdminService) UpsertUserRole(ctx context.Context, userID, workspace string, roleMask domain.Role) (*domain.UserRole, error) {
	if _, err := s.db.GetUserByID(ctx, userID); err != nil {
		slog.ErrorContext(ctx, "upsert user role: get user", "user_id", userID, "error", err)
		return nil, mapErr(err)
	}
	ur := &domain.UserRole{UserID: userID, Workspace: workspace, RoleMask: roleMask}
	if err := s.db.UpsertUserRole(ctx, ur); err != nil {
		slog.ErrorContext(ctx, "upsert user role", "user_id", userID, "workspace", workspace, "error", err)
		return nil, err
	}
	return ur, nil
}

func (s *AdminService) DeleteUserRole(ctx context.Context, userID, workspace string) error {
	if err := s.db.DeleteUserRole(ctx, userID, workspace); err != nil {
		slog.ErrorContext(ctx, "delete user role", "user_id", userID, "workspace", workspace, "error", err)
		return mapErr(err)
	}
	return nil
}

// CreatedKey wraps the newly created APIKey and includes the raw token shown once.
type CreatedKey struct {
	domain.APIKey
	RawKey string
}

func (s *AdminService) CreateAPIKey(ctx context.Context, name, workspace string, role domain.Role, expiresInDays int) (*CreatedKey, error) {
	raw, keyHash, err := generateAPIKeyValue()
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	k := &domain.APIKey{
		ID:        uuid.NewString(),
		Name:      name,
		KeyHash:   keyHash,
		Workspace: workspace,
		Role:      role,
		CreatedAt: time.Now().UTC(),
	}
	if expiresInDays > 0 {
		exp := time.Now().Add(time.Duration(expiresInDays) * 24 * time.Hour).UTC()
		k.ExpiresAt = &exp
	}

	if err := s.db.CreateAPIKey(ctx, k); err != nil {
		slog.ErrorContext(ctx, "create api key", "name", name, "workspace", workspace, "error", err)
		return nil, err
	}
	return &CreatedKey{APIKey: *k, RawKey: raw}, nil
}

func (s *AdminService) ListAPIKeys(ctx context.Context, limit, offset int) ([]*domain.APIKey, error) {
	keys, err := s.db.ListAPIKeys(ctx, limit, offset)
	if err != nil {
		slog.ErrorContext(ctx, "list api keys", "error", err)
		return nil, err
	}
	if keys == nil {
		keys = []*domain.APIKey{}
	}
	return keys, nil
}

func (s *AdminService) RevokeAPIKey(ctx context.Context, keyID string) error {
	if err := s.db.RevokeAPIKey(ctx, keyID); err != nil {
		slog.ErrorContext(ctx, "revoke api key", "key_id", keyID, "error", err)
		return mapErr(err)
	}
	return nil
}

func generateAPIKeyValue() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	raw = "rk_" + hex.EncodeToString(b)
	hash = HashString(raw)
	return raw, hash, nil
}
