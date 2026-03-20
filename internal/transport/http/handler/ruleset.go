package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/rulekit-dev/rulekit-registry/internal/blobstore"
	"github.com/rulekit-dev/rulekit-registry/internal/datastore"
	"github.com/rulekit-dev/rulekit-registry/internal/service"
)

type RulesetHandler struct {
	svc *service.RulesetService
}

func NewRulesetHandler(svc *service.RulesetService) *RulesetHandler {
	return &RulesetHandler{svc: svc}
}

func (h *RulesetHandler) ListRulesets(w http.ResponseWriter, r *http.Request) {
	ns, ok := NamespaceParam(w, r)
	if !ok {
		return
	}
	limit, offset := PageParams(r)
	rulesets, err := h.svc.ListRulesets(r.Context(), ns, limit, offset)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to list rulesets")
		return
	}
	WriteJSON(w, http.StatusOK, rulesets)
}

func (h *RulesetHandler) CreateRuleset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key         string `json:"key"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Namespace   string `json:"namespace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}
	if body.Namespace == "" {
		body.Namespace = "default"
	}
	if !ValidNamespace(body.Namespace) {
		WriteError(w, http.StatusBadRequest, "INVALID_NAMESPACE",
			"namespace must be non-empty, at most 128 characters, and match [a-z0-9_-]")
		return
	}
	if !ValidKey(body.Key) {
		WriteError(w, http.StatusBadRequest, "INVALID_KEY",
			"key must be non-empty, at most 128 characters, and match [a-z0-9_-]")
		return
	}

	rs, err := h.svc.CreateRuleset(r.Context(), body.Namespace, body.Key, body.Name, body.Description)
	if err != nil {
		if errors.Is(err, datastore.ErrAlreadyExists) {
			WriteError(w, http.StatusConflict, "ALREADY_EXISTS", "ruleset already exists")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to create ruleset")
		return
	}
	WriteJSON(w, http.StatusCreated, rs)
}

func (h *RulesetHandler) GetRuleset(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	ns, ok := NamespaceParam(w, r)
	if !ok {
		return
	}

	rs, err := h.svc.GetRuleset(r.Context(), ns, key)
	if err != nil {
		if errors.Is(err, datastore.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "ruleset not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to get ruleset")
		return
	}
	WriteJSON(w, http.StatusOK, rs)
}

func (h *RulesetHandler) DeleteRuleset(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	ns, ok := NamespaceParam(w, r)
	if !ok {
		return
	}

	if err := h.svc.DeleteRuleset(r.Context(), ns, key); err != nil {
		if errors.Is(err, datastore.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "ruleset not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to delete ruleset")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *RulesetHandler) GetDraft(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	ns, ok := NamespaceParam(w, r)
	if !ok {
		return
	}

	draft, err := h.svc.GetDraft(r.Context(), ns, key)
	if err != nil {
		if errors.Is(err, datastore.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "draft not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to get draft")
		return
	}
	WriteJSON(w, http.StatusOK, draft)
}

const maxDraftBodyBytes = 1 << 20 // 1 MiB

func (h *RulesetHandler) UpsertDraft(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	ns, ok := NamespaceParam(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxDraftBodyBytes)

	var body struct {
		DSL json.RawMessage `json:"dsl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			WriteError(w, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE",
				"request body exceeds 1 MiB limit")
			return
		}
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}
	if len(body.DSL) == 0 {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "dsl field is required")
		return
	}

	draft, err := h.svc.UpsertDraft(r.Context(), ns, key, body.DSL)
	if err != nil {
		if errors.Is(err, datastore.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "ruleset not found")
			return
		}
		var ve *service.ValidationError
		if errors.As(err, &ve) {
			WriteError(w, http.StatusBadRequest, "INVALID_DSL", ve.Msg)
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to upsert draft")
		return
	}
	WriteJSON(w, http.StatusOK, draft)
}

func (h *RulesetHandler) DeleteDraft(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	ns, ok := NamespaceParam(w, r)
	if !ok {
		return
	}

	if err := h.svc.DeleteDraft(r.Context(), ns, key); err != nil {
		if errors.Is(err, datastore.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "draft not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to delete draft")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *RulesetHandler) Publish(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	ns, ok := NamespaceParam(w, r)
	if !ok {
		return
	}

	v, err := h.svc.Publish(r.Context(), ns, key)
	if err != nil {
		if errors.Is(err, datastore.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "ruleset or draft not found")
			return
		}
		if errors.Is(err, service.ErrNoChanges) {
			WriteError(w, http.StatusConflict, "NO_CHANGES", "draft is identical to the latest published version")
			return
		}
		if errors.Is(err, datastore.ErrVersionImmutable) {
			WriteError(w, http.StatusConflict, "VERSION_IMMUTABLE", "version already exists")
			return
		}
		var ve *service.ValidationError
		if errors.As(err, &ve) {
			WriteError(w, http.StatusBadRequest, "INVALID_DSL", ve.Msg)
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to publish")
		return
	}
	WriteJSON(w, http.StatusCreated, v)
}

func (h *RulesetHandler) ListVersions(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	ns, ok := NamespaceParam(w, r)
	if !ok {
		return
	}
	limit, offset := PageParams(r)
	versions, err := h.svc.ListVersions(r.Context(), ns, key, limit, offset)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to list versions")
		return
	}
	WriteJSON(w, http.StatusOK, versions)
}

func (h *RulesetHandler) GetVersion(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	ns, ok := NamespaceParam(w, r)
	if !ok {
		return
	}
	versionNum, err := strconv.Atoi(r.PathValue("version"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "version must be an integer")
		return
	}

	v, err := h.svc.GetVersion(r.Context(), ns, key, versionNum)
	if err != nil {
		if errors.Is(err, datastore.ErrNotFound) || errors.Is(err, blobstore.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "version not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to get version")
		return
	}
	WriteJSON(w, http.StatusOK, v)
}

func (h *RulesetHandler) GetLatestVersion(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	ns, ok := NamespaceParam(w, r)
	if !ok {
		return
	}

	v, err := h.svc.GetLatestVersion(r.Context(), ns, key)
	if err != nil {
		if errors.Is(err, datastore.ErrNotFound) || errors.Is(err, blobstore.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "no versions found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to get latest version")
		return
	}
	WriteJSON(w, http.StatusOK, v)
}

func (h *RulesetHandler) GetVersionBundle(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	ns, ok := NamespaceParam(w, r)
	if !ok {
		return
	}
	versionNum, err := strconv.Atoi(r.PathValue("version"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "version must be an integer")
		return
	}

	bundleBytes, err := h.svc.GetVersionBundle(r.Context(), ns, key, versionNum)
	if err != nil {
		if errors.Is(err, datastore.ErrNotFound) || errors.Is(err, blobstore.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "bundle not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to get bundle")
		return
	}

	filename := fmt.Sprintf("ruleset-%s-v%d.zip", key, versionNum)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	w.Write(bundleBytes) //nolint:errcheck
}

func (h *RulesetHandler) GetLatestBundle(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	ns, ok := NamespaceParam(w, r)
	if !ok {
		return
	}

	versionNum, bundleBytes, err := h.svc.GetLatestBundle(r.Context(), ns, key)
	if err != nil {
		if errors.Is(err, datastore.ErrNotFound) || errors.Is(err, blobstore.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "no versions found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to get bundle")
		return
	}

	filename := fmt.Sprintf("ruleset-%s-v%d.zip", key, versionNum)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	w.Write(bundleBytes) //nolint:errcheck
}
