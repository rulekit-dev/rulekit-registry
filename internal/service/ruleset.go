package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rulekit-dev/rulekit-registry/internal/domain"
	"github.com/rulekit-dev/rulekit-registry/internal/domain/dsl"
	"github.com/rulekit-dev/rulekit-registry/internal/port"
)

type RulesetService struct {
	db    port.Datastore
	blobs port.BlobStore
	pubMu sync.Map // "namespace\x00key" -> *sync.Mutex
}

func NewRulesetService(db port.Datastore, blobs port.BlobStore) *RulesetService {
	return &RulesetService{db: db, blobs: blobs}
}

func (s *RulesetService) ListRulesets(ctx context.Context, namespace string, limit, offset int) ([]*domain.Ruleset, error) {
	rulesets, err := s.db.ListRulesets(ctx, namespace, limit, offset)
	if err != nil {
		return nil, err
	}
	if rulesets == nil {
		rulesets = []*domain.Ruleset{}
	}
	return rulesets, nil
}

func (s *RulesetService) CreateRuleset(ctx context.Context, namespace, key, name, description string) (*domain.Ruleset, error) {
	now := time.Now().UTC()
	rs := &domain.Ruleset{
		Namespace:   namespace,
		Key:         key,
		Name:        name,
		Description: description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.db.CreateRuleset(ctx, rs); err != nil {
		return nil, mapErr(err)
	}
	return rs, nil
}

func (s *RulesetService) GetRuleset(ctx context.Context, namespace, key string) (*domain.Ruleset, error) {
	rs, err := s.db.GetRuleset(ctx, namespace, key)
	return rs, mapErr(err)
}

func (s *RulesetService) DeleteRuleset(ctx context.Context, namespace, key string) error {
	return mapErr(s.db.DeleteRuleset(ctx, namespace, key))
}

func (s *RulesetService) GetDraft(ctx context.Context, namespace, key string) (*domain.Draft, error) {
	d, err := s.db.GetDraft(ctx, namespace, key)
	return d, mapErr(err)
}

func (s *RulesetService) UpsertDraft(ctx context.Context, namespace, key string, rawDSL json.RawMessage) (*domain.Draft, error) {
	if _, err := s.db.GetRuleset(ctx, namespace, key); err != nil {
		return nil, mapErr(err)
	}

	if _, err := dsl.ParseAndValidate(rawDSL); err != nil {
		return nil, &ValidationError{Msg: err.Error()}
	}

	deterministicDSL, err := dsl.MarshalDeterministic(rawDSL)
	if err != nil {
		return nil, fmt.Errorf("serialize DSL: %w", err)
	}

	draft := &domain.Draft{
		Namespace:  namespace,
		RulesetKey: key,
		DSL:        json.RawMessage(deterministicDSL),
		UpdatedAt:  time.Now().UTC(),
	}
	if err := s.db.UpsertDraft(ctx, draft); err != nil {
		return nil, err
	}
	return draft, nil
}

func (s *RulesetService) DeleteDraft(ctx context.Context, namespace, key string) error {
	return mapErr(s.db.DeleteDraft(ctx, namespace, key))
}

func (s *RulesetService) Publish(ctx context.Context, namespace, key string) (*domain.Version, error) {
	if _, err := s.db.GetRuleset(ctx, namespace, key); err != nil {
		return nil, mapErr(err)
	}

	draft, err := s.db.GetDraft(ctx, namespace, key)
	if err != nil {
		return nil, mapErr(err)
	}

	if _, err := dsl.ParseAndValidate(draft.DSL); err != nil {
		return nil, &ValidationError{Msg: err.Error()}
	}

	dslBytes, err := dsl.MarshalDeterministic(json.RawMessage(draft.DSL))
	if err != nil {
		return nil, fmt.Errorf("serialize DSL: %w", err)
	}

	checksum := dsl.Checksum(dslBytes)

	if latest, err := s.db.GetLatestVersion(ctx, namespace, key); err == nil {
		if latest.Checksum == checksum {
			return nil, ErrNoChanges
		}
	}

	mu := s.publishMu(namespace, key)
	mu.Lock()
	defer mu.Unlock()

	versionNum, err := s.db.NextVersionNumber(ctx, namespace, key)
	if err != nil {
		return nil, fmt.Errorf("next version number: %w", err)
	}

	manifest := domain.VersionManifest{
		Namespace:  namespace,
		RulesetKey: key,
		Version:    versionNum,
		Checksum:   checksum,
		CreatedAt:  time.Now().UTC(),
	}

	bundleBytes, err := buildBundle(manifest, dslBytes)
	if err != nil {
		return nil, fmt.Errorf("build bundle: %w", err)
	}

	if err := s.blobs.PutDSL(ctx, namespace, key, versionNum, dslBytes); err != nil {
		return nil, fmt.Errorf("store DSL blob: %w", err)
	}

	if err := s.blobs.PutBundle(ctx, namespace, key, versionNum, bundleBytes); err != nil {
		s.blobs.DeleteDSL(ctx, namespace, key, versionNum) //nolint:errcheck
		return nil, fmt.Errorf("store bundle blob: %w", err)
	}

	v := &domain.Version{
		Namespace:  namespace,
		RulesetKey: key,
		Version:    versionNum,
		Checksum:   checksum,
		CreatedAt:  manifest.CreatedAt,
	}

	if err := s.db.CreateVersion(ctx, v); err != nil {
		s.blobs.DeleteDSL(ctx, namespace, key, versionNum)    //nolint:errcheck
		s.blobs.DeleteBundle(ctx, namespace, key, versionNum) //nolint:errcheck
		return nil, mapErr(err)
	}

	v.DSL = json.RawMessage(dslBytes)
	return v, nil
}

func (s *RulesetService) ListVersions(ctx context.Context, namespace, key string, limit, offset int) ([]*domain.Version, error) {
	versions, err := s.db.ListVersions(ctx, namespace, key, limit, offset)
	if err != nil {
		return nil, err
	}
	if versions == nil {
		versions = []*domain.Version{}
	}
	return versions, nil
}

func (s *RulesetService) GetVersion(ctx context.Context, namespace, key string, versionNum int) (*domain.Version, error) {
	v, err := s.db.GetVersion(ctx, namespace, key, versionNum)
	if err != nil {
		return nil, mapErr(err)
	}
	dslData, err := s.blobs.GetDSL(ctx, namespace, key, v.Version)
	if err != nil {
		return nil, mapErr(err)
	}
	v.DSL = json.RawMessage(dslData)
	return v, nil
}

func (s *RulesetService) GetLatestVersion(ctx context.Context, namespace, key string) (*domain.Version, error) {
	v, err := s.db.GetLatestVersion(ctx, namespace, key)
	if err != nil {
		return nil, mapErr(err)
	}
	dslData, err := s.blobs.GetDSL(ctx, namespace, key, v.Version)
	if err != nil {
		return nil, mapErr(err)
	}
	v.DSL = json.RawMessage(dslData)
	return v, nil
}

func (s *RulesetService) GetVersionBundle(ctx context.Context, namespace, key string, versionNum int) ([]byte, error) {
	if _, err := s.db.GetVersion(ctx, namespace, key, versionNum); err != nil {
		return nil, mapErr(err)
	}
	data, err := s.blobs.GetBundle(ctx, namespace, key, versionNum)
	return data, mapErr(err)
}

func (s *RulesetService) GetLatestBundle(ctx context.Context, namespace, key string) (int, []byte, error) {
	v, err := s.db.GetLatestVersion(ctx, namespace, key)
	if err != nil {
		return 0, nil, mapErr(err)
	}
	data, err := s.blobs.GetBundle(ctx, namespace, key, v.Version)
	return v.Version, data, mapErr(err)
}

func (s *RulesetService) publishMu(namespace, key string) *sync.Mutex {
	v, _ := s.pubMu.LoadOrStore(namespace+"\x00"+key, &sync.Mutex{})
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
