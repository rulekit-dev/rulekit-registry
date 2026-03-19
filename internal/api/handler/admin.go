package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/rulekit-dev/rulekit-registry/internal/model"
	"github.com/rulekit-dev/rulekit-registry/internal/store"
)

type AdminHandler struct {
	store store.Store
}

func NewAdminHandler(s store.Store) *AdminHandler {
	return &AdminHandler{store: s}
}

// GET /v1/admin/users
func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	limit, offset := PageParams(r)
	users, err := h.store.ListUsers(r.Context(), limit, offset)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to list users")
		return
	}
	if users == nil {
		users = []*model.User{}
	}
	WriteJSON(w, http.StatusOK, users)
}

// DELETE /v1/admin/users/{userID}
func (h *AdminHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userID")
	if err := h.store.DeleteUser(r.Context(), userID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "user not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to delete user")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /v1/admin/users/{userID}/roles
func (h *AdminHandler) ListUserRoles(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userID")
	roles, err := h.store.ListUserRoles(r.Context(), userID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to list roles")
		return
	}
	if roles == nil {
		roles = []*model.UserRole{}
	}
	WriteJSON(w, http.StatusOK, roles)
}

// PUT /v1/admin/users/{userID}/roles/{namespace}
// Body: { "role_mask": 3 }
func (h *AdminHandler) UpsertUserRole(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userID")
	namespace := r.PathValue("namespace")

	if _, err := h.store.GetUserByID(r.Context(), userID); errors.Is(err, store.ErrNotFound) {
		WriteError(w, http.StatusNotFound, "NOT_FOUND", "user not found")
		return
	} else if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to look up user")
		return
	}

	var body struct {
		RoleMask model.Role `json:"role_mask"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}

	ur := &model.UserRole{UserID: userID, Namespace: namespace, RoleMask: body.RoleMask}
	if err := h.store.UpsertUserRole(r.Context(), ur); err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to upsert role")
		return
	}
	WriteJSON(w, http.StatusOK, ur)
}

// DELETE /v1/admin/users/{userID}/roles/{namespace}
func (h *AdminHandler) DeleteUserRole(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userID")
	namespace := r.PathValue("namespace")

	if err := h.store.DeleteUserRole(r.Context(), userID, namespace); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "role not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to delete role")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /v1/admin/tokens
// Body: { "user_id": "...", "name": "ci-pipeline", "namespace": "payments", "role": 2, "expires_in_days": 90 }
func (h *AdminHandler) CreateAPIToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID        string     `json:"user_id"`
		Name          string     `json:"name"`
		Namespace     string     `json:"namespace"`
		Role          model.Role `json:"role"`
		ExpiresInDays int        `json:"expires_in_days"` // 0 = never
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}
	if body.UserID == "" || body.Name == "" || body.Namespace == "" {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "user_id, name, and namespace are required")
		return
	}

	if _, err := h.store.GetUserByID(r.Context(), body.UserID); errors.Is(err, store.ErrNotFound) {
		WriteError(w, http.StatusNotFound, "NOT_FOUND", "user not found")
		return
	} else if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to look up user")
		return
	}

	raw, tokenHash, err := generateAPITokenValue()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to generate token")
		return
	}

	t := &model.APIToken{
		ID:        uuid.NewString(),
		UserID:    body.UserID,
		Name:      body.Name,
		TokenHash: tokenHash,
		Namespace: body.Namespace,
		Role:      body.Role,
		CreatedAt: time.Now().UTC(),
	}
	if body.ExpiresInDays > 0 {
		exp := time.Now().Add(time.Duration(body.ExpiresInDays) * 24 * time.Hour).UTC()
		t.ExpiresAt = &exp
	}

	if err := h.store.CreateAPIToken(r.Context(), t); err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to create token")
		return
	}

	// Return the raw token only on creation — it is never retrievable again.
	WriteJSON(w, http.StatusCreated, map[string]any{
		"id":         t.ID,
		"token":      raw,
		"name":       t.Name,
		"namespace":  t.Namespace,
		"role":       t.Role,
		"created_at": t.CreatedAt,
		"expires_at": t.ExpiresAt,
	})
}

// GET /v1/admin/tokens?user_id=...
func (h *AdminHandler) ListAPITokens(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "user_id query param is required")
		return
	}
	tokens, err := h.store.ListAPITokens(r.Context(), userID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to list tokens")
		return
	}
	if tokens == nil {
		tokens = []*model.APIToken{}
	}
	WriteJSON(w, http.StatusOK, tokens)
}

// DELETE /v1/admin/tokens/{tokenID}
func (h *AdminHandler) RevokeAPIToken(w http.ResponseWriter, r *http.Request) {
	tokenID := r.PathValue("tokenID")
	if err := h.store.RevokeAPIToken(r.Context(), tokenID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "token not found or already revoked")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to revoke token")
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
