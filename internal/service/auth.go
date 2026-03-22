package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rulekit-dev/rulekit-registry/internal/domain"
	"github.com/rulekit-dev/rulekit-registry/internal/port"
	"github.com/rulekit-dev/rulekit-registry/internal/util"
)

const (
	otpTTL          = 10 * time.Minute
	otpLength       = 6
	refreshTokenTTL = util.RefreshTokenTTL
)

type AuthService struct {
	db            port.Datastore
	mailer        port.Mailer
	jwtSecret     []byte
	adminPassword string
}

func NewAuthService(db port.Datastore, m port.Mailer, jwtSecret []byte, adminPassword string) *AuthService {
	return &AuthService{db: db, mailer: m, jwtSecret: jwtSecret, adminPassword: adminPassword}
}

// AdminLogin verifies the admin password and returns a long-lived JWT.
// Admin is a virtual identity — no DB record is created or required.
func (s *AuthService) AdminLogin(ctx context.Context, password string) (*TokenPair, error) {
	if password == "" || password != s.adminPassword {
		return nil, ErrInvalidPassword
	}
	token, err := util.SignAdminToken(s.jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("issue admin token: %w", err)
	}
	return &TokenPair{AccessToken: token}, nil
}

// Login looks up or auto-provisions the user and sends an OTP to their email.
func (s *AuthService) Login(ctx context.Context, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))

	user, err := s.db.GetUserByEmail(ctx, email)
	if errors.Is(err, port.ErrNotFound) {
		user = &domain.User{
			ID:        uuid.NewString(),
			Email:     email,
			CreatedAt: time.Now().UTC(),
		}
		user.LastLoginAt = user.CreatedAt
		if createErr := s.db.CreateUser(ctx, user); createErr != nil {
			slog.ErrorContext(ctx, "login: create user", "email", email, "error", createErr)
			return fmt.Errorf("create user: %w", createErr)
		}
	} else if err != nil {
		slog.ErrorContext(ctx, "login: look up user", "email", email, "error", err)
		return fmt.Errorf("look up user: %w", err)
	}

	code, err := generateOTP(otpLength)
	if err != nil {
		slog.ErrorContext(ctx, "login: generate OTP", "email", email, "error", err)
		return fmt.Errorf("generate OTP: %w", err)
	}

	otp := &domain.OTPCode{
		ID:        uuid.NewString(),
		UserID:    user.ID,
		CodeHash:  HashString(code),
		ExpiresAt: time.Now().Add(otpTTL).UTC(),
	}
	if err := s.db.CreateOTPCode(ctx, otp); err != nil {
		slog.ErrorContext(ctx, "login: store OTP", "email", email, "error", err)
		return fmt.Errorf("store OTP: %w", err)
	}

	// Non-fatal: the code is in the DB even if email delivery fails.
	_ = s.mailer.SendOTP(ctx, user.Email, code)
	return nil
}

type TokenPair struct {
	AccessToken  string
	RefreshToken string
}

// Verify validates an OTP code and returns a JWT access token + refresh token.
func (s *AuthService) Verify(ctx context.Context, email, code string) (*TokenPair, error) {
	email = strings.ToLower(strings.TrimSpace(email))

	user, err := s.db.GetUserByEmail(ctx, email)
	if errors.Is(err, port.ErrNotFound) {
		return nil, ErrInvalidCode
	}
	if err != nil {
		slog.ErrorContext(ctx, "verify: look up user", "email", email, "error", err)
		return nil, fmt.Errorf("look up user: %w", err)
	}

	otp, err := s.db.GetUnusedOTPCode(ctx, user.ID)
	if errors.Is(err, port.ErrNotFound) {
		return nil, ErrInvalidCode
	}
	if err != nil {
		slog.ErrorContext(ctx, "verify: look up OTP", "user_id", user.ID, "error", err)
		return nil, fmt.Errorf("look up OTP: %w", err)
	}

	if otp.CodeHash != HashString(code) {
		return nil, ErrInvalidCode
	}

	if err := s.db.MarkOTPUsed(ctx, otp.ID); err != nil {
		slog.ErrorContext(ctx, "verify: consume OTP", "user_id", user.ID, "error", err)
		return nil, fmt.Errorf("consume OTP: %w", err)
	}
	if err := s.db.UpdateUserLastLogin(ctx, user.ID); err != nil {
		slog.ErrorContext(ctx, "verify: update last login", "user_id", user.ID, "error", err)
		return nil, fmt.Errorf("update last login: %w", err)
	}

	roles, err := s.db.ListUserRoles(ctx, user.ID)
	if err != nil {
		slog.ErrorContext(ctx, "verify: load roles", "user_id", user.ID, "error", err)
		return nil, fmt.Errorf("load roles: %w", err)
	}

	accessToken, err := util.SignAccessToken(s.jwtSecret, user, roles)
	if err != nil {
		slog.ErrorContext(ctx, "verify: issue access token", "user_id", user.ID, "error", err)
		return nil, fmt.Errorf("issue access token: %w", err)
	}

	rawRefresh, refreshHash, err := generateRefreshToken()
	if err != nil {
		slog.ErrorContext(ctx, "verify: generate refresh token", "user_id", user.ID, "error", err)
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}

	exp := time.Now().Add(refreshTokenTTL).UTC()
	rt := &domain.RefreshToken{
		ID:        uuid.NewString(),
		UserID:    user.ID,
		TokenHash: refreshHash,
		ExpiresAt: exp,
	}
	if err := s.db.CreateRefreshToken(ctx, rt); err != nil {
		slog.ErrorContext(ctx, "verify: store refresh token", "user_id", user.ID, "error", err)
		return nil, fmt.Errorf("store refresh token: %w", err)
	}

	return &TokenPair{AccessToken: accessToken, RefreshToken: rawRefresh}, nil
}

// Refresh validates a refresh token, rotates it, and returns a new token pair.
func (s *AuthService) Refresh(ctx context.Context, rawRefreshToken string) (*TokenPair, error) {
	tokenHash := HashString(rawRefreshToken)
	rt, err := s.db.GetRefreshTokenByHash(ctx, tokenHash)
	if errors.Is(err, port.ErrNotFound) {
		return nil, ErrInvalidToken
	}
	if err != nil {
		slog.ErrorContext(ctx, "refresh: look up token", "error", err)
		return nil, fmt.Errorf("look up token: %w", err)
	}

	if rt.RevokedAt != nil {
		return nil, ErrInvalidToken
	}
	if time.Now().After(rt.ExpiresAt) {
		return nil, ErrInvalidToken
	}

	user, err := s.db.GetUserByID(ctx, rt.UserID)
	if err != nil {
		slog.ErrorContext(ctx, "refresh: load user", "user_id", rt.UserID, "error", err)
		return nil, fmt.Errorf("load user: %w", err)
	}

	roles, err := s.db.ListUserRoles(ctx, user.ID)
	if err != nil {
		slog.ErrorContext(ctx, "refresh: load roles", "user_id", user.ID, "error", err)
		return nil, fmt.Errorf("load roles: %w", err)
	}

	accessToken, err := util.SignAccessToken(s.jwtSecret, user, roles)
	if err != nil {
		slog.ErrorContext(ctx, "refresh: issue access token", "user_id", user.ID, "error", err)
		return nil, fmt.Errorf("issue access token: %w", err)
	}

	if err := s.db.RevokeRefreshToken(ctx, rt.ID); err != nil {
		slog.ErrorContext(ctx, "refresh: rotate refresh token", "user_id", user.ID, "error", err)
		return nil, fmt.Errorf("rotate refresh token: %w", err)
	}

	newRawRefresh, newRefreshHash, err := generateRefreshToken()
	if err != nil {
		slog.ErrorContext(ctx, "refresh: generate new refresh token", "user_id", user.ID, "error", err)
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}

	exp := time.Now().Add(refreshTokenTTL).UTC()
	newRT := &domain.RefreshToken{
		ID:        uuid.NewString(),
		UserID:    user.ID,
		TokenHash: newRefreshHash,
		ExpiresAt: exp,
	}
	if err := s.db.CreateRefreshToken(ctx, newRT); err != nil {
		slog.ErrorContext(ctx, "refresh: store new refresh token", "user_id", user.ID, "error", err)
		return nil, fmt.Errorf("store refresh token: %w", err)
	}

	return &TokenPair{AccessToken: accessToken, RefreshToken: newRawRefresh}, nil
}

// Logout revokes the refresh token. Returns nil if already gone.
func (s *AuthService) Logout(ctx context.Context, rawRefreshToken string) error {
	tokenHash := HashString(rawRefreshToken)
	rt, err := s.db.GetRefreshTokenByHash(ctx, tokenHash)
	if errors.Is(err, port.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("look up token: %w", err)
	}
	if err := s.db.RevokeRefreshToken(ctx, rt.ID); err != nil && !errors.Is(err, port.ErrNotFound) {
		return fmt.Errorf("revoke token: %w", err)
	}
	return nil
}

var (
	ErrInvalidCode     = errors.New("invalid or expired code")
	ErrInvalidToken    = errors.New("invalid or expired token")
	ErrInvalidPassword = errors.New("invalid password")
)

func HashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func generateOTP(length int) (string, error) {
	const digits = "0123456789"
	code := make([]byte, length)
	for i := range code {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(digits))))
		if err != nil {
			return "", fmt.Errorf("generate otp: %w", err)
		}
		code[i] = digits[n.Int64()]
	}
	return string(code), nil
}

func generateRefreshToken() (token, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate refresh token: %w", err)
	}
	token = hex.EncodeToString(b)
	hash = HashString(token)
	return token, hash, nil
}
