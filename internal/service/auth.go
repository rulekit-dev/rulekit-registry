package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rulekit-dev/rulekit-registry/internal/datastore"
	"github.com/rulekit-dev/rulekit-registry/internal/jwtutil"
	"github.com/rulekit-dev/rulekit-registry/internal/mailer"
	"github.com/rulekit-dev/rulekit-registry/internal/model"
)

const (
	otpTTL          = 10 * time.Minute
	otpLength       = 6
	refreshTokenTTL = jwtutil.RefreshTokenTTL
)

type AuthService struct {
	db        datastore.Datastore
	mailer    mailer.Mailer
	jwtSecret []byte
}

func NewAuthService(db datastore.Datastore, m mailer.Mailer, jwtSecret []byte) *AuthService {
	return &AuthService{db: db, mailer: m, jwtSecret: jwtSecret}
}

// Login looks up or auto-provisions the user and sends an OTP to their email.
// Always returns without error to avoid email enumeration.
func (s *AuthService) Login(ctx context.Context, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))

	user, err := s.db.GetUserByEmail(ctx, email)
	if errors.Is(err, datastore.ErrNotFound) {
		user = &model.User{
			ID:        uuid.NewString(),
			Email:     email,
			CreatedAt: time.Now().UTC(),
		}
		user.LastLoginAt = user.CreatedAt
		if createErr := s.db.CreateUser(ctx, user); createErr != nil {
			return fmt.Errorf("create user: %w", createErr)
		}
	} else if err != nil {
		return fmt.Errorf("look up user: %w", err)
	}

	code, err := generateOTP(otpLength)
	if err != nil {
		return fmt.Errorf("generate OTP: %w", err)
	}

	otp := &model.OTPCode{
		ID:        uuid.NewString(),
		UserID:    user.ID,
		CodeHash:  HashString(code),
		ExpiresAt: time.Now().Add(otpTTL).UTC(),
	}
	if err := s.db.CreateOTPCode(ctx, otp); err != nil {
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
	if errors.Is(err, datastore.ErrNotFound) {
		return nil, ErrInvalidCode
	}
	if err != nil {
		return nil, fmt.Errorf("look up user: %w", err)
	}

	otp, err := s.db.GetUnusedOTPCode(ctx, user.ID)
	if errors.Is(err, datastore.ErrNotFound) {
		return nil, ErrInvalidCode
	}
	if err != nil {
		return nil, fmt.Errorf("look up OTP: %w", err)
	}

	if otp.CodeHash != HashString(code) {
		return nil, ErrInvalidCode
	}

	if err := s.db.MarkOTPUsed(ctx, otp.ID); err != nil {
		return nil, fmt.Errorf("consume OTP: %w", err)
	}
	if err := s.db.UpdateUserLastLogin(ctx, user.ID); err != nil {
		return nil, fmt.Errorf("update last login: %w", err)
	}

	roles, err := s.db.ListUserRoles(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("load roles: %w", err)
	}

	accessToken, err := jwtutil.SignAccessToken(s.jwtSecret, user, roles)
	if err != nil {
		return nil, fmt.Errorf("issue access token: %w", err)
	}

	refreshToken, refreshHash, err := generateRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}

	exp := time.Now().Add(refreshTokenTTL).UTC()
	rt := &model.APIToken{
		ID:        uuid.NewString(),
		UserID:    user.ID,
		Name:      "refresh",
		TokenHash: refreshHash,
		Namespace: "*",
		Role:      0,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: &exp,
	}
	if err := s.db.CreateAPIToken(ctx, rt); err != nil {
		return nil, fmt.Errorf("store refresh token: %w", err)
	}

	return &TokenPair{AccessToken: accessToken, RefreshToken: refreshToken}, nil
}

// Refresh validates a refresh token, rotates it, and returns a new token pair.
func (s *AuthService) Refresh(ctx context.Context, rawRefreshToken string) (*TokenPair, error) {
	tokenHash := HashString(rawRefreshToken)
	rt, err := s.db.GetAPITokenByHash(ctx, tokenHash)
	if errors.Is(err, datastore.ErrNotFound) {
		return nil, ErrInvalidToken
	}
	if err != nil {
		return nil, fmt.Errorf("look up token: %w", err)
	}

	if rt.RevokedAt != nil {
		return nil, ErrInvalidToken
	}
	if rt.ExpiresAt != nil && time.Now().After(*rt.ExpiresAt) {
		return nil, ErrInvalidToken
	}

	user, err := s.db.GetUserByID(ctx, rt.UserID)
	if err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}

	roles, err := s.db.ListUserRoles(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("load roles: %w", err)
	}

	accessToken, err := jwtutil.SignAccessToken(s.jwtSecret, user, roles)
	if err != nil {
		return nil, fmt.Errorf("issue access token: %w", err)
	}

	if err := s.db.RevokeAPIToken(ctx, rt.ID); err != nil {
		return nil, fmt.Errorf("rotate refresh token: %w", err)
	}

	newRefreshToken, newRefreshHash, err := generateRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}

	exp := time.Now().Add(refreshTokenTTL).UTC()
	newRT := &model.APIToken{
		ID:        uuid.NewString(),
		UserID:    user.ID,
		Name:      "refresh",
		TokenHash: newRefreshHash,
		Namespace: "*",
		Role:      0,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: &exp,
	}
	if err := s.db.CreateAPIToken(ctx, newRT); err != nil {
		return nil, fmt.Errorf("store refresh token: %w", err)
	}

	return &TokenPair{AccessToken: accessToken, RefreshToken: newRefreshToken}, nil
}

// Logout revokes the refresh token identified by rawRefreshToken.
// Returns nil if the token is already gone.
func (s *AuthService) Logout(ctx context.Context, rawRefreshToken string) error {
	tokenHash := HashString(rawRefreshToken)
	rt, err := s.db.GetAPITokenByHash(ctx, tokenHash)
	if errors.Is(err, datastore.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("look up token: %w", err)
	}
	if err := s.db.RevokeAPIToken(ctx, rt.ID); err != nil && !errors.Is(err, datastore.ErrNotFound) {
		return fmt.Errorf("revoke token: %w", err)
	}
	return nil
}

var (
	ErrInvalidCode  = errors.New("invalid or expired code")
	ErrInvalidToken = errors.New("invalid or expired token")
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
