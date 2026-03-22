package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/rulekit-dev/rulekit-registry/internal/service"
)

type WorkspaceHandler struct {
	svc service.WorkspaceUseCase
}

func NewWorkspaceHandler(svc service.WorkspaceUseCase) *WorkspaceHandler {
	return &WorkspaceHandler{svc: svc}
}

func (h *WorkspaceHandler) CreateWorkspace(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}
	if !ValidWorkspace(body.Name) {
		WriteError(w, http.StatusBadRequest, "INVALID_WORKSPACE",
			"workspace name must be non-empty, at most 128 characters, and match [a-z0-9_-]")
		return
	}

	ws, err := h.svc.CreateWorkspace(r.Context(), body.Name, body.Description)
	if err != nil {
		if errors.Is(err, service.ErrAlreadyExists) {
			WriteError(w, http.StatusConflict, "ALREADY_EXISTS", "workspace already exists")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to create workspace")
		return
	}
	WriteJSON(w, http.StatusCreated, ws)
}

func (h *WorkspaceHandler) GetWorkspace(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("workspace")
	ws, err := h.svc.GetWorkspace(r.Context(), name)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "workspace not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to get workspace")
		return
	}
	WriteJSON(w, http.StatusOK, ws)
}

func (h *WorkspaceHandler) ListWorkspaces(w http.ResponseWriter, r *http.Request) {
	limit, offset := PageParams(r)
	workspaces, err := h.svc.ListWorkspaces(r.Context(), limit, offset)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to list workspaces")
		return
	}
	WriteJSON(w, http.StatusOK, workspaces)
}

func (h *WorkspaceHandler) DeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("workspace")
	if err := h.svc.DeleteWorkspace(r.Context(), name); err != nil {
		if errors.Is(err, service.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "workspace not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to delete workspace")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
