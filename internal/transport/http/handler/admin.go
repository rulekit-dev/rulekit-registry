package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/rulekit-dev/rulekit-registry/internal/datastore"
	"github.com/rulekit-dev/rulekit-registry/internal/model"
	"github.com/rulekit-dev/rulekit-registry/internal/service"
)

type AdminHandler struct {
	svc *service.AdminService
}

func NewAdminHandler(svc *service.AdminService) *AdminHandler {
	return &AdminHandler{svc: svc}
}

// GET /v1/admin/users
func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	limit, offset := PageParams(r)
	users, err := h.svc.ListUsers(r.Context(), limit, offset)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to list users")
		return
	}
	WriteJSON(w, http.StatusOK, users)
}

// DELETE /v1/admin/users/{userID}
func (h *AdminHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userID")
	if err := h.svc.DeleteUser(r.Context(), userID); err != nil {
		if errors.Is(err, datastore.ErrNotFound) {
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
	roles, err := h.svc.ListUserRoles(r.Context(), userID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to list roles")
		return
	}
	WriteJSON(w, http.StatusOK, roles)
}

// PUT /v1/admin/users/{userID}/roles/{namespace}
// Body: { "role_mask": 3 }
func (h *AdminHandler) UpsertUserRole(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userID")
	namespace := r.PathValue("namespace")

	var body struct {
		RoleMask model.Role `json:"role_mask"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}

	ur, err := h.svc.UpsertUserRole(r.Context(), userID, namespace, body.RoleMask)
	if err != nil {
		if errors.Is(err, datastore.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "user not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to upsert role")
		return
	}
	WriteJSON(w, http.StatusOK, ur)
}

// DELETE /v1/admin/users/{userID}/roles/{namespace}
func (h *AdminHandler) DeleteUserRole(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userID")
	namespace := r.PathValue("namespace")

	if err := h.svc.DeleteUserRole(r.Context(), userID, namespace); err != nil {
		if errors.Is(err, datastore.ErrNotFound) {
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
		ExpiresInDays int        `json:"expires_in_days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}
	if body.UserID == "" || body.Name == "" || body.Namespace == "" {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "user_id, name, and namespace are required")
		return
	}

	created, err := h.svc.CreateAPIToken(r.Context(), body.UserID, body.Name, body.Namespace, body.Role, body.ExpiresInDays)
	if err != nil {
		if errors.Is(err, datastore.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "user not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to create token")
		return
	}

	// Return the raw token only on creation — it is never retrievable again.
	WriteJSON(w, http.StatusCreated, map[string]any{
		"id":         created.ID,
		"token":      created.RawToken,
		"name":       created.Name,
		"namespace":  created.Namespace,
		"role":       created.Role,
		"created_at": created.CreatedAt,
		"expires_at": created.ExpiresAt,
	})
}

// GET /v1/admin/tokens?user_id=...
func (h *AdminHandler) ListAPITokens(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "user_id query param is required")
		return
	}
	tokens, err := h.svc.ListAPITokens(r.Context(), userID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to list tokens")
		return
	}
	WriteJSON(w, http.StatusOK, tokens)
}

// DELETE /v1/admin/tokens/{tokenID}
func (h *AdminHandler) RevokeAPIToken(w http.ResponseWriter, r *http.Request) {
	tokenID := r.PathValue("tokenID")
	if err := h.svc.RevokeAPIToken(r.Context(), tokenID); err != nil {
		if errors.Is(err, datastore.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "token not found or already revoked")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to revoke token")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
