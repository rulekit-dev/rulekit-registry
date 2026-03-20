package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/rulekit-dev/rulekit-registry/internal/jwtutil"
	"github.com/rulekit-dev/rulekit-registry/internal/service"
)

type AuthHandler struct {
	svc service.AuthUseCase
}

func NewAuthHandler(svc service.AuthUseCase) *AuthHandler {
	return &AuthHandler{svc: svc}
}

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

	if err := h.svc.Login(r.Context(), body.Email); err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to process login")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{"message": "OTP sent to email"})
}

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

	pair, err := h.svc.Verify(r.Context(), body.Email, body.Code)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCode) {
			WriteError(w, http.StatusUnauthorized, "INVALID_CODE", "invalid or expired code")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to verify code")
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"access_token":  pair.AccessToken,
		"refresh_token": pair.RefreshToken,
		"token_type":    "Bearer",
		"expires_in":    int(jwtutil.AccessTokenTTL.Seconds()),
	})
}

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

	pair, err := h.svc.Refresh(r.Context(), body.RefreshToken)
	if err != nil {
		if errors.Is(err, service.ErrInvalidToken) {
			WriteError(w, http.StatusUnauthorized, "INVALID_TOKEN", "invalid or expired refresh token")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to refresh token")
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"access_token":  pair.AccessToken,
		"refresh_token": pair.RefreshToken,
		"token_type":    "Bearer",
		"expires_in":    int(jwtutil.AccessTokenTTL.Seconds()),
	})
}

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

	if err := h.svc.Logout(r.Context(), body.RefreshToken); err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to revoke token")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ClaimsContext attaches claims to a context (used by middleware).
func ClaimsContext(ctx context.Context, claims *jwtutil.Claims) context.Context {
	return context.WithValue(ctx, ClaimsKey, claims)
}
