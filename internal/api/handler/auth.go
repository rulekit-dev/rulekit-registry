package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rulekit-dev/rulekit-registry/internal/jwtutil"
	"github.com/rulekit-dev/rulekit-registry/internal/mailer"
	"github.com/rulekit-dev/rulekit-registry/internal/model"
	"github.com/rulekit-dev/rulekit-registry/internal/store"
)

const (
	otpTTL          = 10 * time.Minute
	otpLength       = 6
	refreshTokenTTL = jwtutil.RefreshTokenTTL
)

type AuthHandler struct {
	store     store.Store
	mailer    mailer.Mailer
	jwtSecret []byte
}

func NewAuthHandler(s store.Store, m mailer.Mailer, jwtSecret []byte) *AuthHandler {
	return &AuthHandler{store: s, mailer: m, jwtSecret: jwtSecret}
}

// POST /v1/auth/login
// Body: { "email": "alice@example.com" }
// Always returns 200 to avoid email enumeration.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}
	body.Email = strings.ToLower(strings.TrimSpace(body.Email))
	if body.Email == "" {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "email is required")
		return
	}

	user, err := h.store.GetUserByEmail(r.Context(), body.Email)
	if errors.Is(err, store.ErrNotFound) {
		// Auto-provision the user on first login.
		user = &model.User{
			ID:        uuid.NewString(),
			Email:     body.Email,
			CreatedAt: time.Now().UTC(),
		}
		user.LastLoginAt = user.CreatedAt
		if createErr := h.store.CreateUser(r.Context(), user); createErr != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to create user")
			return
		}
	} else if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to look up user")
		return
	}

	code, err := generateOTP(otpLength)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to generate OTP")
		return
	}

	otp := &model.OTPCode{
		ID:        uuid.NewString(),
		UserID:    user.ID,
		CodeHash:  HashString(code),
		ExpiresAt: time.Now().Add(otpTTL).UTC(),
	}
	if err := h.store.CreateOTPCode(r.Context(), otp); err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to store OTP")
		return
	}

	if err := h.mailer.SendOTP(r.Context(), user.Email, code); err != nil {
		// Non-fatal: the code is in the DB; log and continue.
		_ = err
	}

	WriteJSON(w, http.StatusOK, map[string]string{"message": "OTP sent to email"})
}

// POST /v1/auth/verify
// Body: { "email": "alice@example.com", "code": "123456" }
func (h *AuthHandler) Verify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}
	body.Email = strings.ToLower(strings.TrimSpace(body.Email))
	body.Code = strings.TrimSpace(body.Code)
	if body.Email == "" || body.Code == "" {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "email and code are required")
		return
	}

	user, err := h.store.GetUserByEmail(r.Context(), body.Email)
	if errors.Is(err, store.ErrNotFound) {
		WriteError(w, http.StatusUnauthorized, "INVALID_CODE", "invalid or expired code")
		return
	}
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to look up user")
		return
	}

	otp, err := h.store.GetUnusedOTPCode(r.Context(), user.ID)
	if errors.Is(err, store.ErrNotFound) {
		WriteError(w, http.StatusUnauthorized, "INVALID_CODE", "invalid or expired code")
		return
	}
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to look up OTP")
		return
	}

	if otp.CodeHash != HashString(body.Code) {
		WriteError(w, http.StatusUnauthorized, "INVALID_CODE", "invalid or expired code")
		return
	}

	if err := h.store.MarkOTPUsed(r.Context(), otp.ID); err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to consume OTP")
		return
	}
	if err := h.store.UpdateUserLastLogin(r.Context(), user.ID); err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to update last login")
		return
	}

	roles, err := h.store.ListUserRoles(r.Context(), user.ID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to load roles")
		return
	}

	accessToken, err := jwtutil.SignAccessToken(h.jwtSecret, user, roles)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to issue access token")
		return
	}

	refreshToken, refreshHash, err := generateRefreshToken()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to generate refresh token")
		return
	}

	exp := time.Now().Add(refreshTokenTTL).UTC()
	rt := &model.APIToken{
		ID:        uuid.NewString(),
		UserID:    user.ID,
		Name:      "refresh",
		TokenHash: refreshHash,
		Namespace: "*",
		Role:      0, // refresh tokens carry no role; roles come from JWT
		CreatedAt: time.Now().UTC(),
		ExpiresAt: &exp,
	}
	if err := h.store.CreateAPIToken(r.Context(), rt); err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to store refresh token")
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"token_type":    "Bearer",
		"expires_in":    int(jwtutil.AccessTokenTTL.Seconds()),
	})
}

// POST /v1/auth/refresh
// Body: { "refresh_token": "<token>" }
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}
	if body.RefreshToken == "" {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "refresh_token is required")
		return
	}

	tokenHash := HashString(body.RefreshToken)
	rt, err := h.store.GetAPITokenByHash(r.Context(), tokenHash)
	if errors.Is(err, store.ErrNotFound) {
		WriteError(w, http.StatusUnauthorized, "INVALID_TOKEN", "invalid or expired refresh token")
		return
	}
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to look up token")
		return
	}

	if rt.RevokedAt != nil {
		WriteError(w, http.StatusUnauthorized, "INVALID_TOKEN", "refresh token has been revoked")
		return
	}
	if rt.ExpiresAt != nil && time.Now().After(*rt.ExpiresAt) {
		WriteError(w, http.StatusUnauthorized, "INVALID_TOKEN", "refresh token has expired")
		return
	}

	user, err := h.store.GetUserByID(r.Context(), rt.UserID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to load user")
		return
	}

	roles, err := h.store.ListUserRoles(r.Context(), user.ID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to load roles")
		return
	}

	accessToken, err := jwtutil.SignAccessToken(h.jwtSecret, user, roles)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to issue access token")
		return
	}

	// Rotate the refresh token.
	if err := h.store.RevokeAPIToken(r.Context(), rt.ID); err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to rotate refresh token")
		return
	}

	newRefreshToken, newRefreshHash, err := generateRefreshToken()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to generate refresh token")
		return
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
	if err := h.store.CreateAPIToken(r.Context(), newRT); err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to store refresh token")
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"access_token":  accessToken,
		"refresh_token": newRefreshToken,
		"token_type":    "Bearer",
		"expires_in":    int(jwtutil.AccessTokenTTL.Seconds()),
	})
}

// POST /v1/auth/logout
// Body: { "refresh_token": "<token>" }
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}
	if body.RefreshToken == "" {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "refresh_token is required")
		return
	}

	tokenHash := HashString(body.RefreshToken)
	rt, err := h.store.GetAPITokenByHash(r.Context(), tokenHash)
	if errors.Is(err, store.ErrNotFound) {
		// Already gone; treat as success.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to look up token")
		return
	}

	if err := h.store.RevokeAPIToken(r.Context(), rt.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to revoke token")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ClaimsContext attaches claims to a context (used by middleware).
func ClaimsContext(ctx context.Context, claims *jwtutil.Claims) context.Context {
	return context.WithValue(ctx, ClaimsKey, claims)
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

func HashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
