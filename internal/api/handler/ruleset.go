package handler

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/rulekit-dev/rulekit-registry/internal/blobstore"
	"github.com/rulekit-dev/rulekit-registry/internal/dsl"
	"github.com/rulekit-dev/rulekit-registry/internal/model"
	"github.com/rulekit-dev/rulekit-registry/internal/store"
)

type RulesetHandler struct {
	store store.Store
	blobs blobstore.BlobStore
	pubMu sync.Map // "namespace\x00key" -> *sync.Mutex
}

func NewRulesetHandler(s store.Store, b blobstore.BlobStore) *RulesetHandler {
	return &RulesetHandler{store: s, blobs: b}
}

func (h *RulesetHandler) getPublishMu(namespace, key string) *sync.Mutex {
	v, _ := h.pubMu.LoadOrStore(namespace+"\x00"+key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func (h *RulesetHandler) ListRulesets(w http.ResponseWriter, r *http.Request) {
	ns, ok := NamespaceParam(w, r)
	if !ok {
		return
	}
	limit, offset := PageParams(r)
	rulesets, err := h.store.ListRulesets(r.Context(), ns, limit, offset)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to list rulesets")
		return
	}
	if rulesets == nil {
		rulesets = []*model.Ruleset{}
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

	rs, err := h.store.GetRuleset(r.Context(), ns, key)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
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

	if err := h.store.DeleteRuleset(r.Context(), ns, key); err != nil {
		if errors.Is(err, store.ErrNotFound) {
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

	draft, err := h.store.GetDraft(r.Context(), ns, key)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
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

	if _, err := h.store.GetRuleset(r.Context(), ns, key); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "ruleset not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to get ruleset")
		return
	}

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

	_, err := dsl.ParseAndValidate(body.DSL)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "INVALID_DSL", err.Error())
		return
	}

	deterministicDSL, err := dsl.MarshalDeterministic(json.RawMessage(body.DSL))
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to serialize DSL")
		return
	}

	draft := &model.Draft{
		Namespace:  ns,
		RulesetKey: key,
		DSL:        json.RawMessage(deterministicDSL),
		UpdatedAt:  time.Now().UTC(),
	}

	if err := h.store.UpsertDraft(r.Context(), draft); err != nil {
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

	if err := h.store.DeleteDraft(r.Context(), ns, key); err != nil {
		if errors.Is(err, store.ErrNotFound) {
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

	if _, err := h.store.GetRuleset(r.Context(), ns, key); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "ruleset not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to get ruleset")
		return
	}

	draft, err := h.store.GetDraft(r.Context(), ns, key)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "no draft to publish")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to get draft")
		return
	}

	_, err = dsl.ParseAndValidate(draft.DSL)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "INVALID_DSL", err.Error())
		return
	}

	dslBytes, err := dsl.MarshalDeterministic(json.RawMessage(draft.DSL))
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to serialize DSL")
		return
	}

	checksum := dsl.Checksum(dslBytes)

	if latest, err := h.store.GetLatestVersion(r.Context(), ns, key); err == nil {
		if latest.Checksum == checksum {
			WriteError(w, http.StatusConflict, "NO_CHANGES", "draft is identical to the latest published version")
			return
		}
	}

	mu := h.getPublishMu(ns, key)
	mu.Lock()
	defer mu.Unlock()

	versionNum, err := h.store.NextVersionNumber(r.Context(), ns, key)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to get next version number")
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
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to build bundle")
		return
	}

	if err := h.blobs.PutDSL(r.Context(), ns, key, versionNum, dslBytes); err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to store DSL blob")
		return
	}

	if err := h.blobs.PutBundle(r.Context(), ns, key, versionNum, bundleBytes); err != nil {
		h.blobs.DeleteDSL(r.Context(), ns, key, versionNum) //nolint:errcheck
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to store bundle blob")
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
		h.blobs.DeleteDSL(r.Context(), ns, key, versionNum)    //nolint:errcheck
		h.blobs.DeleteBundle(r.Context(), ns, key, versionNum) //nolint:errcheck
		if errors.Is(err, store.ErrVersionImmutable) {
			WriteError(w, http.StatusConflict, "VERSION_IMMUTABLE", "version already exists")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to create version")
		return
	}

	v.DSL = json.RawMessage(dslBytes)

	WriteJSON(w, http.StatusCreated, v)
}

func (h *RulesetHandler) ListVersions(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	ns, ok := NamespaceParam(w, r)
	if !ok {
		return
	}
	limit, offset := PageParams(r)
	versions, err := h.store.ListVersions(r.Context(), ns, key, limit, offset)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to list versions")
		return
	}
	if versions == nil {
		versions = []*model.Version{}
	}
	WriteJSON(w, http.StatusOK, versions)
}

func (h *RulesetHandler) GetVersion(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	ns, ok := NamespaceParam(w, r)
	if !ok {
		return
	}
	versionStr := r.PathValue("version")

	versionNum, err := strconv.Atoi(versionStr)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "version must be an integer")
		return
	}

	v, err := h.store.GetVersion(r.Context(), ns, key, versionNum)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "version not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to get version")
		return
	}

	dslData, err := h.blobs.GetDSL(r.Context(), ns, key, v.Version)
	if err != nil {
		if errors.Is(err, blobstore.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "DSL blob not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to get DSL blob")
		return
	}
	v.DSL = json.RawMessage(dslData)

	WriteJSON(w, http.StatusOK, v)
}

func (h *RulesetHandler) GetLatestVersion(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	ns, ok := NamespaceParam(w, r)
	if !ok {
		return
	}

	v, err := h.store.GetLatestVersion(r.Context(), ns, key)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "no versions found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to get latest version")
		return
	}

	dslData, err := h.blobs.GetDSL(r.Context(), ns, key, v.Version)
	if err != nil {
		if errors.Is(err, blobstore.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "DSL blob not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to get DSL blob")
		return
	}
	v.DSL = json.RawMessage(dslData)

	WriteJSON(w, http.StatusOK, v)
}

func (h *RulesetHandler) GetVersionBundle(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	ns, ok := NamespaceParam(w, r)
	if !ok {
		return
	}
	versionStr := r.PathValue("version")

	versionNum, err := strconv.Atoi(versionStr)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "version must be an integer")
		return
	}

	_, err = h.store.GetVersion(r.Context(), ns, key, versionNum)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "version not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to get version")
		return
	}

	bundleBytes, err := h.blobs.GetBundle(r.Context(), ns, key, versionNum)
	if err != nil {
		if errors.Is(err, blobstore.ErrNotFound) {
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

	v, err := h.store.GetLatestVersion(r.Context(), ns, key)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "no versions found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to get latest version")
		return
	}

	bundleBytes, err := h.blobs.GetBundle(r.Context(), ns, key, v.Version)
	if err != nil {
		if errors.Is(err, blobstore.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "bundle not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to get bundle")
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
