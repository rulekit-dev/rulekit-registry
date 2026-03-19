package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/rulekit/rulekit-registry/internal/blobstore"
	"github.com/rulekit/rulekit-registry/internal/dsl"
	"github.com/rulekit/rulekit-registry/internal/model"
	"github.com/rulekit/rulekit-registry/internal/store"
)

type Handler struct {
	store store.Store
	blobs blobstore.BlobStore
	pubMu sync.Map // "namespace\x00key" -> *sync.Mutex
}

func NewHandler(s store.Store, b blobstore.BlobStore) *Handler {
	return &Handler{store: s, blobs: b}
}

func (h *Handler) getPublishMu(namespace, key string) *sync.Mutex {
	v, _ := h.pubMu.LoadOrStore(namespace+"\x00"+key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

var identPattern = regexp.MustCompile(`^[a-z0-9_-]+$`)

func validKey(s string) bool {
	return len(s) > 0 && len(s) <= 128 && identPattern.MatchString(s)
}

func validNamespace(s string) bool {
	return len(s) > 0 && len(s) <= 128 && identPattern.MatchString(s)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func namespaceParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	ns := r.URL.Query().Get("namespace")
	if ns == "" {
		return "default", true
	}
	if !validNamespace(ns) {
		writeError(w, http.StatusBadRequest, "INVALID_NAMESPACE",
			"namespace must be non-empty, at most 128 characters, and match [a-z0-9_-]")
		return "", false
	}
	return ns, true
}

func pageParams(r *http.Request) (limit, offset int) {
	limit = 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 200 {
		limit = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v >= 0 {
		offset = v
	}
	return limit, offset
}

func (h *Handler) ListRulesets(w http.ResponseWriter, r *http.Request) {
	ns, ok := namespaceParam(w, r)
	if !ok {
		return
	}
	limit, offset := pageParams(r)
	rulesets, err := h.store.ListRulesets(r.Context(), ns, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to list rulesets")
		return
	}
	if rulesets == nil {
		rulesets = []*model.Ruleset{}
	}
	writeJSON(w, http.StatusOK, rulesets)
}

func (h *Handler) CreateRuleset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key         string `json:"key"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Namespace   string `json:"namespace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}
	if body.Namespace == "" {
		body.Namespace = "default"
	}
	if !validNamespace(body.Namespace) {
		writeError(w, http.StatusBadRequest, "INVALID_NAMESPACE",
			"namespace must be non-empty, at most 128 characters, and match [a-z0-9_-]")
		return
	}
	if !validKey(body.Key) {
		writeError(w, http.StatusBadRequest, "INVALID_KEY",
			"key must be non-empty, at most 128 characters, and match [a-z0-9_-]")
		return
	}

	now := time.Now().UTC()
	rs := &model.Ruleset{
		Namespace:   body.Namespace,
		Key:         body.Key,
		Name:        body.Name,
		Description: body.Description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := h.store.CreateRuleset(r.Context(), rs); err != nil {
		if errors.Is(err, store.ErrAlreadyExists) {
			writeError(w, http.StatusConflict, "ALREADY_EXISTS", "ruleset already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to create ruleset")
		return
	}

	writeJSON(w, http.StatusCreated, rs)
}

func (h *Handler) GetRuleset(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	ns, ok := namespaceParam(w, r)
	if !ok {
		return
	}

	rs, err := h.store.GetRuleset(r.Context(), ns, key)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "ruleset not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to get ruleset")
		return
	}

	writeJSON(w, http.StatusOK, rs)
}

func (h *Handler) DeleteRuleset(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	ns, ok := namespaceParam(w, r)
	if !ok {
		return
	}

	if err := h.store.DeleteRuleset(r.Context(), ns, key); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "ruleset not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to delete ruleset")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetDraft(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	ns, ok := namespaceParam(w, r)
	if !ok {
		return
	}

	draft, err := h.store.GetDraft(r.Context(), ns, key)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "draft not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to get draft")
		return
	}

	writeJSON(w, http.StatusOK, draft)
}

func (h *Handler) UpsertDraft(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	ns, ok := namespaceParam(w, r)
	if !ok {
		return
	}

	if _, err := h.store.GetRuleset(r.Context(), ns, key); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "ruleset not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to get ruleset")
		return
	}

	var body struct {
		DSL json.RawMessage `json:"dsl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}
	if len(body.DSL) == 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "dsl field is required")
		return
	}

	_, err := dsl.ParseAndValidate(body.DSL)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_DSL", err.Error())
		return
	}

	deterministicDSL, err := dsl.MarshalDeterministic(json.RawMessage(body.DSL))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to serialize DSL")
		return
	}

	draft := &model.Draft{
		Namespace:  ns,
		RulesetKey: key,
		DSL:        json.RawMessage(deterministicDSL),
		UpdatedAt:  time.Now().UTC(),
	}

	if err := h.store.UpsertDraft(r.Context(), draft); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to upsert draft")
		return
	}

	writeJSON(w, http.StatusOK, draft)
}

func (h *Handler) DeleteDraft(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	ns, ok := namespaceParam(w, r)
	if !ok {
		return
	}

	if err := h.store.DeleteDraft(r.Context(), ns, key); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "draft not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to delete draft")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	ns, ok := namespaceParam(w, r)
	if !ok {
		return
	}

	if _, err := h.store.GetRuleset(r.Context(), ns, key); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "ruleset not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to get ruleset")
		return
	}

	draft, err := h.store.GetDraft(r.Context(), ns, key)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "no draft to publish")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to get draft")
		return
	}

	_, err = dsl.ParseAndValidate(draft.DSL)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_DSL", err.Error())
		return
	}

	dslBytes, err := dsl.MarshalDeterministic(json.RawMessage(draft.DSL))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to serialize DSL")
		return
	}

	checksum := dsl.Checksum(dslBytes)

	if latest, err := h.store.GetLatestVersion(r.Context(), ns, key); err == nil {
		if latest.Checksum == checksum {
			writeError(w, http.StatusConflict, "NO_CHANGES", "draft is identical to the latest published version")
			return
		}
	}

	mu := h.getPublishMu(ns, key)
	mu.Lock()
	defer mu.Unlock()

	versionNum, err := h.store.NextVersionNumber(r.Context(), ns, key)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to get next version number")
		return
	}

	manifest := model.VersionManifest{
		Namespace:  ns,
		RulesetKey: key,
		Version:    versionNum,
		Checksum:   checksum,
		CreatedAt:  time.Now().UTC(),
	}

	bundleBytes, err := buildBundle(manifest, dslBytes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to build bundle")
		return
	}

	if err := h.blobs.PutDSL(r.Context(), ns, key, versionNum, dslBytes); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to store DSL blob")
		return
	}

	if err := h.blobs.PutBundle(r.Context(), ns, key, versionNum, bundleBytes); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to store bundle blob")
		return
	}

	// DSL is intentionally left nil in the DB; the blob store is the source of truth.
	v := &model.Version{
		Namespace:  ns,
		RulesetKey: key,
		Version:    versionNum,
		Checksum:   checksum,
		CreatedAt:  manifest.CreatedAt,
	}

	if err := h.store.CreateVersion(r.Context(), v); err != nil {
		if errors.Is(err, store.ErrVersionImmutable) {
			writeError(w, http.StatusConflict, "VERSION_IMMUTABLE", "version already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to create version")
		return
	}

	v.DSL = json.RawMessage(dslBytes)

	writeJSON(w, http.StatusCreated, v)
}

func (h *Handler) ListVersions(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	ns, ok := namespaceParam(w, r)
	if !ok {
		return
	}
	limit, offset := pageParams(r)
	versions, err := h.store.ListVersions(r.Context(), ns, key, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to list versions")
		return
	}
	if versions == nil {
		versions = []*model.Version{}
	}
	writeJSON(w, http.StatusOK, versions)
}

func (h *Handler) GetVersion(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	ns, ok := namespaceParam(w, r)
	if !ok {
		return
	}
	versionStr := r.PathValue("version")

	versionNum, err := strconv.Atoi(versionStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "version must be an integer")
		return
	}

	v, err := h.store.GetVersion(r.Context(), ns, key, versionNum)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "version not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to get version")
		return
	}

	dslData, err := h.blobs.GetDSL(r.Context(), ns, key, v.Version)
	if err != nil {
		if errors.Is(err, blobstore.ErrNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "DSL blob not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to get DSL blob")
		return
	}
	v.DSL = json.RawMessage(dslData)

	writeJSON(w, http.StatusOK, v)
}

func (h *Handler) GetLatestVersion(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	ns, ok := namespaceParam(w, r)
	if !ok {
		return
	}

	v, err := h.store.GetLatestVersion(r.Context(), ns, key)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "no versions found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to get latest version")
		return
	}

	dslData, err := h.blobs.GetDSL(r.Context(), ns, key, v.Version)
	if err != nil {
		if errors.Is(err, blobstore.ErrNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "DSL blob not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to get DSL blob")
		return
	}
	v.DSL = json.RawMessage(dslData)

	writeJSON(w, http.StatusOK, v)
}

func (h *Handler) GetVersionBundle(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	ns, ok := namespaceParam(w, r)
	if !ok {
		return
	}
	versionStr := r.PathValue("version")

	versionNum, err := strconv.Atoi(versionStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "version must be an integer")
		return
	}

	_, err = h.store.GetVersion(r.Context(), ns, key, versionNum)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "version not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to get version")
		return
	}

	bundleBytes, err := h.blobs.GetBundle(r.Context(), ns, key, versionNum)
	if err != nil {
		if errors.Is(err, blobstore.ErrNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "bundle not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to get bundle")
		return
	}

	filename := fmt.Sprintf("ruleset-%s-v%d.zip", key, versionNum)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	w.Write(bundleBytes) //nolint:errcheck
}

func (h *Handler) GetLatestBundle(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	ns, ok := namespaceParam(w, r)
	if !ok {
		return
	}

	v, err := h.store.GetLatestVersion(r.Context(), ns, key)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "no versions found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to get latest version")
		return
	}

	bundleBytes, err := h.blobs.GetBundle(r.Context(), ns, key, v.Version)
	if err != nil {
		if errors.Is(err, blobstore.ErrNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "bundle not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to get bundle")
		return
	}

	filename := fmt.Sprintf("ruleset-%s-v%d.zip", key, v.Version)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	w.Write(bundleBytes) //nolint:errcheck
}

func buildBundle(manifest model.VersionManifest, dslBytes []byte) ([]byte, error) {
	manifestBytes, err := dsl.MarshalDeterministic(manifest)
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	mf, err := zw.Create("manifest.json")
	if err != nil {
		return nil, fmt.Errorf("create manifest entry: %w", err)
	}
	if _, err := mf.Write(manifestBytes); err != nil {
		return nil, fmt.Errorf("write manifest: %w", err)
	}

	df, err := zw.Create("dsl.json")
	if err != nil {
		return nil, fmt.Errorf("create dsl entry: %w", err)
	}
	if _, err := df.Write(dslBytes); err != nil {
		return nil, fmt.Errorf("write dsl: %w", err)
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("finalize zip: %w", err)
	}

	return buf.Bytes(), nil
}
