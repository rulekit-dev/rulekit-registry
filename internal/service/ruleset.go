package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/rulekit-dev/rulekit-registry/internal/domain"
	"github.com/rulekit-dev/rulekit-registry/internal/domain/dsl"
	"github.com/rulekit-dev/rulekit-registry/internal/port"
)

type RulesetService struct {
	db    port.Datastore
	blobs port.BlobStore
	pubMu sync.Map // "workspace\x00key" -> *sync.Mutex
}

func NewRulesetService(db port.Datastore, blobs port.BlobStore) *RulesetService {
	return &RulesetService{db: db, blobs: blobs}
}

func (s *RulesetService) ListRulesets(ctx context.Context, workspace string, limit, offset int) ([]*domain.Ruleset, error) {
	rulesets, err := s.db.ListRulesets(ctx, workspace, limit, offset)
	if err != nil {
		slog.ErrorContext(ctx, "list rulesets", "workspace", workspace, "error", err)
		return nil, err
	}
	if rulesets == nil {
		rulesets = []*domain.Ruleset{}
	}
	return rulesets, nil
}

func (s *RulesetService) CreateRuleset(ctx context.Context, workspace, key, name, description string) (*domain.Ruleset, error) {
	now := time.Now().UTC()
	rs := &domain.Ruleset{
		Workspace:   workspace,
		Key:         key,
		Name:        name,
		Description: description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.db.CreateRuleset(ctx, rs); err != nil {
		slog.ErrorContext(ctx, "create ruleset", "workspace", workspace, "key", key, "error", err)
		return nil, mapErr(err)
	}
	return rs, nil
}

func (s *RulesetService) GetRuleset(ctx context.Context, workspace, key string) (*domain.Ruleset, error) {
	rs, err := s.db.GetRuleset(ctx, workspace, key)
	if err != nil {
		slog.ErrorContext(ctx, "get ruleset", "workspace", workspace, "key", key, "error", err)
	}
	return rs, mapErr(err)
}

func (s *RulesetService) DeleteRuleset(ctx context.Context, workspace, key string) error {
	if err := s.db.DeleteRuleset(ctx, workspace, key); err != nil {
		slog.ErrorContext(ctx, "delete ruleset", "workspace", workspace, "key", key, "error", err)
		return mapErr(err)
	}
	return nil
}

func (s *RulesetService) GetDraft(ctx context.Context, workspace, key string) (*domain.Draft, error) {
	d, err := s.db.GetDraft(ctx, workspace, key)
	if err != nil {
		slog.ErrorContext(ctx, "get draft", "workspace", workspace, "key", key, "error", err)
	}
	return d, mapErr(err)
}

func (s *RulesetService) UpsertDraft(ctx context.Context, workspace, key string, rawDSL json.RawMessage) (*domain.Draft, error) {
	if _, err := s.db.GetRuleset(ctx, workspace, key); err != nil {
		slog.ErrorContext(ctx, "upsert draft: get ruleset", "workspace", workspace, "key", key, "error", err)
		return nil, mapErr(err)
	}

	if _, err := dsl.ParseAndValidateDraft(rawDSL); err != nil {
		return nil, &ValidationError{Msg: err.Error()}
	}

	deterministicDSL, err := dsl.MarshalDeterministic(rawDSL)
	if err != nil {
		slog.ErrorContext(ctx, "upsert draft: serialize DSL", "workspace", workspace, "key", key, "error", err)
		return nil, fmt.Errorf("serialize DSL: %w", err)
	}

	draft := &domain.Draft{
		Workspace:  workspace,
		RulesetKey: key,
		DSL:        json.RawMessage(deterministicDSL),
		UpdatedAt:  time.Now().UTC(),
	}
	if err := s.db.UpsertDraft(ctx, draft); err != nil {
		slog.ErrorContext(ctx, "upsert draft: store", "workspace", workspace, "key", key, "error", err)
		return nil, err
	}
	return draft, nil
}

func (s *RulesetService) DeleteDraft(ctx context.Context, workspace, key string) error {
	if err := s.db.DeleteDraft(ctx, workspace, key); err != nil {
		slog.ErrorContext(ctx, "delete draft", "workspace", workspace, "key", key, "error", err)
		return mapErr(err)
	}
	return nil
}

func (s *RulesetService) Publish(ctx context.Context, workspace, key string) (*domain.Version, error) {
	if _, err := s.db.GetRuleset(ctx, workspace, key); err != nil {
		slog.ErrorContext(ctx, "publish: get ruleset", "workspace", workspace, "key", key, "error", err)
		return nil, mapErr(err)
	}

	draft, err := s.db.GetDraft(ctx, workspace, key)
	if err != nil {
		slog.ErrorContext(ctx, "publish: get draft", "workspace", workspace, "key", key, "error", err)
		return nil, mapErr(err)
	}

	if _, err := dsl.ParseAndValidate(draft.DSL); err != nil {
		return nil, &ValidationError{Msg: err.Error()}
	}

	dslBytes, err := dsl.MarshalDeterministic(json.RawMessage(draft.DSL))
	if err != nil {
		slog.ErrorContext(ctx, "publish: serialize DSL", "workspace", workspace, "key", key, "error", err)
		return nil, fmt.Errorf("serialize DSL: %w", err)
	}

	checksum := dsl.Checksum(dslBytes)

	if latest, err := s.db.GetLatestVersion(ctx, workspace, key); err == nil {
		if latest.Checksum == checksum {
			return nil, ErrNoChanges
		}
	}

	mu := s.publishMu(workspace, key)
	mu.Lock()
	defer mu.Unlock()

	versionNum, err := s.db.NextVersionNumber(ctx, workspace, key)
	if err != nil {
		slog.ErrorContext(ctx, "publish: next version number", "workspace", workspace, "key", key, "error", err)
		return nil, fmt.Errorf("next version number: %w", err)
	}

	manifest := domain.VersionManifest{
		Workspace:  workspace,
		RulesetKey: key,
		Version:    versionNum,
		Checksum:   checksum,
		CreatedAt:  time.Now().UTC(),
	}

	bundleBytes, err := buildBundle(manifest, dslBytes)
	if err != nil {
		slog.ErrorContext(ctx, "publish: build bundle", "workspace", workspace, "key", key, "error", err)
		return nil, fmt.Errorf("build bundle: %w", err)
	}

	if err := s.blobs.PutDSL(ctx, workspace, key, versionNum, dslBytes); err != nil {
		slog.ErrorContext(ctx, "publish: store DSL blob", "workspace", workspace, "key", key, "version", versionNum, "error", err)
		return nil, fmt.Errorf("store DSL blob: %w", err)
	}

	if err := s.blobs.PutBundle(ctx, workspace, key, versionNum, bundleBytes); err != nil {
		slog.ErrorContext(ctx, "publish: store bundle blob", "workspace", workspace, "key", key, "version", versionNum, "error", err)
		s.blobs.DeleteDSL(ctx, workspace, key, versionNum) //nolint:errcheck
		return nil, fmt.Errorf("store bundle blob: %w", err)
	}

	v := &domain.Version{
		Workspace:  workspace,
		RulesetKey: key,
		Version:    versionNum,
		Checksum:   checksum,
		CreatedAt:  manifest.CreatedAt,
	}

	if err := s.db.CreateVersion(ctx, v); err != nil {
		slog.ErrorContext(ctx, "publish: create version record", "workspace", workspace, "key", key, "version", versionNum, "error", err)
		s.blobs.DeleteDSL(ctx, workspace, key, versionNum)    //nolint:errcheck
		s.blobs.DeleteBundle(ctx, workspace, key, versionNum) //nolint:errcheck
		return nil, mapErr(err)
	}

	v.DSL = json.RawMessage(dslBytes)
	return v, nil
}

func (s *RulesetService) ListVersions(ctx context.Context, workspace, key string, limit, offset int) ([]*domain.Version, error) {
	versions, err := s.db.ListVersions(ctx, workspace, key, limit, offset)
	if err != nil {
		slog.ErrorContext(ctx, "list versions", "workspace", workspace, "key", key, "error", err)
		return nil, err
	}
	if versions == nil {
		versions = []*domain.Version{}
	}
	return versions, nil
}

func (s *RulesetService) GetVersion(ctx context.Context, workspace, key string, versionNum int) (*domain.Version, error) {
	v, err := s.db.GetVersion(ctx, workspace, key, versionNum)
	if err != nil {
		slog.ErrorContext(ctx, "get version", "workspace", workspace, "key", key, "version", versionNum, "error", err)
		return nil, mapErr(err)
	}
	dslData, err := s.blobs.GetDSL(ctx, workspace, key, v.Version)
	if err != nil {
		slog.ErrorContext(ctx, "get version: fetch DSL blob", "workspace", workspace, "key", key, "version", versionNum, "error", err)
		return nil, mapErr(err)
	}
	v.DSL = json.RawMessage(dslData)
	return v, nil
}

func (s *RulesetService) GetLatestVersion(ctx context.Context, workspace, key string) (*domain.Version, error) {
	v, err := s.db.GetLatestVersion(ctx, workspace, key)
	if err != nil {
		slog.ErrorContext(ctx, "get latest version", "workspace", workspace, "key", key, "error", err)
		return nil, mapErr(err)
	}
	dslData, err := s.blobs.GetDSL(ctx, workspace, key, v.Version)
	if err != nil {
		slog.ErrorContext(ctx, "get latest version: fetch DSL blob", "workspace", workspace, "key", key, "version", v.Version, "error", err)
		return nil, mapErr(err)
	}
	v.DSL = json.RawMessage(dslData)
	return v, nil
}

func (s *RulesetService) GetVersionBundle(ctx context.Context, workspace, key string, versionNum int) ([]byte, error) {
	if _, err := s.db.GetVersion(ctx, workspace, key, versionNum); err != nil {
		slog.ErrorContext(ctx, "get version bundle: get version", "workspace", workspace, "key", key, "version", versionNum, "error", err)
		return nil, mapErr(err)
	}
	data, err := s.blobs.GetBundle(ctx, workspace, key, versionNum)
	if err != nil {
		slog.ErrorContext(ctx, "get version bundle: fetch blob", "workspace", workspace, "key", key, "version", versionNum, "error", err)
	}
	return data, mapErr(err)
}

func (s *RulesetService) GetLatestBundle(ctx context.Context, workspace, key string) (int, []byte, error) {
	v, err := s.db.GetLatestVersion(ctx, workspace, key)
	if err != nil {
		slog.ErrorContext(ctx, "get latest bundle: get version", "workspace", workspace, "key", key, "error", err)
		return 0, nil, mapErr(err)
	}
	data, err := s.blobs.GetBundle(ctx, workspace, key, v.Version)
	if err != nil {
		slog.ErrorContext(ctx, "get latest bundle: fetch blob", "workspace", workspace, "key", key, "version", v.Version, "error", err)
	}
	return v.Version, data, mapErr(err)
}

func (s *RulesetService) publishMu(workspace, key string) *sync.Mutex {
	v, _ := s.pubMu.LoadOrStore(workspace+"\x00"+key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func buildBundle(manifest domain.VersionManifest, dslBytes []byte) ([]byte, error) {
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

// Sentinel errors returned by RulesetService.

var ErrNoChanges = errors.New("draft is identical to the latest published version")

// ValidationError wraps a DSL validation message so callers can distinguish it
// from infrastructure errors.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }
