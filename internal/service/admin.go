package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/rulekit-dev/rulekit-registry/internal/datastore"
	"github.com/rulekit-dev/rulekit-registry/internal/model"
)

type AdminService struct {
	db datastore.Datastore
}

func NewAdminService(db datastore.Datastore) *AdminService {
	return &AdminService{db: db}
}

func (s *AdminService) ListUsers(ctx context.Context, limit, offset int) ([]*model.User, error) {
	users, err := s.db.ListUsers(ctx, limit, offset)
	if err != nil {
		return nil, err
	}
	if users == nil {
		users = []*model.User{}
	}
	return users, nil
}

func (s *AdminService) DeleteUser(ctx context.Context, userID string) error {
	return s.db.DeleteUser(ctx, userID)
}

func (s *AdminService) ListUserRoles(ctx context.Context, userID string) ([]*model.UserRole, error) {
	roles, err := s.db.ListUserRoles(ctx, userID)
	if err != nil {
		return nil, err
	}
	if roles == nil {
		roles = []*model.UserRole{}
	}
	return roles, nil
}

func (s *AdminService) UpsertUserRole(ctx context.Context, userID, namespace string, roleMask model.Role) (*model.UserRole, error) {
	if _, err := s.db.GetUserByID(ctx, userID); err != nil {
		return nil, err
	}
	ur := &model.UserRole{UserID: userID, Namespace: namespace, RoleMask: roleMask}
	if err := s.db.UpsertUserRole(ctx, ur); err != nil {
		return nil, err
	}
	return ur, nil
}

func (s *AdminService) DeleteUserRole(ctx context.Context, userID, namespace string) error {
	return s.db.DeleteUserRole(ctx, userID, namespace)
}

type CreatedToken struct {
	model.APIToken
	RawToken string
}

func (s *AdminService) CreateAPIToken(ctx context.Context, userID, name, namespace string, role model.Role, expiresInDays int) (*CreatedToken, error) {
	if _, err := s.db.GetUserByID(ctx, userID); err != nil {
		return nil, err
	}

	raw, tokenHash, err := generateAPITokenValue()
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}

	t := &model.APIToken{
		ID:        uuid.NewString(),
		UserID:    userID,
		Name:      name,
		TokenHash: tokenHash,
		Namespace: namespace,
		Role:      role,
		CreatedAt: time.Now().UTC(),
	}
	if expiresInDays > 0 {
		exp := time.Now().Add(time.Duration(expiresInDays) * 24 * time.Hour).UTC()
		t.ExpiresAt = &exp
	}

	if err := s.db.CreateAPIToken(ctx, t); err != nil {
		return nil, err
	}
	return &CreatedToken{APIToken: *t, RawToken: raw}, nil
}

func (s *AdminService) ListAPITokens(ctx context.Context, userID string) ([]*model.APIToken, error) {
	tokens, err := s.db.ListAPITokens(ctx, userID)
	if err != nil {
		return nil, err
	}
	if tokens == nil {
		tokens = []*model.APIToken{}
	}
	return tokens, nil
}

func (s *AdminService) RevokeAPIToken(ctx context.Context, tokenID string) error {
	return s.db.RevokeAPIToken(ctx, tokenID)
}

func generateAPITokenValue() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	raw = "rk_" + hex.EncodeToString(b)
	hash = HashString(raw)
	return raw, hash, nil
}
