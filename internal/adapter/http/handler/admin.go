package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/rulekit-dev/rulekit-registry/internal/domain"
	"github.com/rulekit-dev/rulekit-registry/internal/service"
)

type AdminHandler struct {
	svc service.AdminUseCase
}

func NewAdminHandler(svc service.AdminUseCase) *AdminHandler {
	return &AdminHandler{svc: svc}
}

func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	limit, offset := PageParams(r)
	users, err := h.svc.ListUsers(r.Context(), limit, offset)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to list users")
		return
	}
	WriteJSON(w, http.StatusOK, users)
}

func (h *AdminHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userID")
	if err := h.svc.DeleteUser(r.Context(), userID); err != nil {
		if errors.Is(err, service.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "user not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to delete user")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *AdminHandler) ListUserRoles(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userID")
	roles, err := h.svc.ListUserRoles(r.Context(), userID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to list roles")
		return
	}
	WriteJSON(w, http.StatusOK, roles)
}

func (h *AdminHandler) UpsertUserRole(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userID")
	workspace := r.PathValue("workspace")

	var body struct {
		RoleMask domain.Role `json:"role_mask"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}

	ur, err := h.svc.UpsertUserRole(r.Context(), userID, workspace, body.RoleMask)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "user not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to upsert role")
		return
	}
	WriteJSON(w, http.StatusOK, ur)
}

func (h *AdminHandler) DeleteUserRole(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userID")
	workspace := r.PathValue("workspace")

	if err := h.svc.DeleteUserRole(r.Context(), userID, workspace); err != nil {
		if errors.Is(err, service.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "role not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to delete role")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /v1/admin/keys
// Body: { "name": "ci-pipeline", "workspace": "payments", "role": 2, "expires_in_days": 90 }
func (h *AdminHandler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name          string      `json:"name"`
		Workspace     string      `json:"workspace"`
		Role          domain.Role `json:"role"`
		ExpiresInDays int         `json:"expires_in_days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}
	if body.Name == "" || body.Workspace == "" {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "name and workspace are required")
		return
	}

	created, err := h.svc.CreateAPIKey(r.Context(), body.Name, body.Workspace, body.Role, body.ExpiresInDays)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to create key")
		return
	}

	// Return the raw key only on creation — it is never retrievable again.
	WriteJSON(w, http.StatusCreated, map[string]any{
		"id":         created.ID,
		"key":        created.RawKey,
		"name":       created.Name,
		"workspace":  created.Workspace,
		"role":       created.Role,
		"created_at": created.CreatedAt,
		"expires_at": created.ExpiresAt,
	})
}

// GET /v1/admin/keys
func (h *AdminHandler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	limit, offset := PageParams(r)
	keys, err := h.svc.ListAPIKeys(r.Context(), limit, offset)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to list keys")
		return
	}
	WriteJSON(w, http.StatusOK, keys)
}

// DELETE /v1/admin/keys/{keyID}
func (h *AdminHandler) RevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	keyID := r.PathValue("keyID")
	if err := h.svc.RevokeAPIKey(r.Context(), keyID); err != nil {
		if errors.Is(err, service.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "key not found or already revoked")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to revoke key")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
