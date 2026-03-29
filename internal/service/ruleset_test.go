package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rulekit-dev/rulekit-registry/internal/domain"
	"github.com/rulekit-dev/rulekit-registry/internal/mock"
	"github.com/rulekit-dev/rulekit-registry/internal/service"
)

var validDSLJSON = json.RawMessage(`{
	"dsl_version": "v1",
	"entry": "node-1",
	"nodes": [{
		"id": "node-1",
		"strategy": "first_match",
		"schema": {"age": {"type": "number"}},
		"rules": [{
			"id": "r1",
			"name": "adult",
			"when": [{"field": "age", "op": "gte", "value": 18}],
			"then": {"result": "adult"}
		}]
	}]
}`)

func setup(t *testing.T) (*service.RulesetService, *mock.Datastore, *mock.BlobStore) {
	t.Helper()
	db := mock.NewDatastore()
	blobs := mock.NewBlobStore()
	svc := service.NewRulesetService(db, blobs)
	return svc, db, blobs
}

func seedRuleset(t *testing.T, db *mock.Datastore, workspace, key string) {
	t.Helper()
	err := db.CreateRuleset(context.Background(), &domain.Ruleset{
		Workspace: workspace, Key: key, Name: key,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("seed ruleset: %v", err)
	}
}

func TestCreateRuleset(t *testing.T) {
	svc, _, _ := setup(t)
	ctx := context.Background()

	rs, err := svc.CreateRuleset(ctx, "default", "my-rules", "My Rules", "desc")
	if err != nil {
		t.Fatalf("CreateRuleset: %v", err)
	}
	if rs.Key != "my-rules" {
		t.Errorf("Key: got %q, want %q", rs.Key, "my-rules")
	}
}

func TestCreateRulesetDuplicate(t *testing.T) {
	svc, _, _ := setup(t)
	ctx := context.Background()

	svc.CreateRuleset(ctx, "default", "dup", "Dup", "") //nolint:errcheck
	_, err := svc.CreateRuleset(ctx, "default", "dup", "Dup", "")
	if !errors.Is(err, service.ErrAlreadyExists) {
		t.Errorf("expected ErrAlreadyExists, got %v", err)
	}
}

func TestGetRulesetNotFound(t *testing.T) {
	svc, _, _ := setup(t)
	_, err := svc.GetRuleset(context.Background(), "default", "nonexistent")
	if !errors.Is(err, service.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteRuleset(t *testing.T) {
	svc, _, _ := setup(t)
	ctx := context.Background()

	svc.CreateRuleset(ctx, "default", "to-del", "Del", "") //nolint:errcheck
	if err := svc.DeleteRuleset(ctx, "default", "to-del"); err != nil {
		t.Fatalf("DeleteRuleset: %v", err)
	}
	_, err := svc.GetRuleset(ctx, "default", "to-del")
	if !errors.Is(err, service.ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestDeleteRulesetNotFound(t *testing.T) {
	svc, _, _ := setup(t)
	err := svc.DeleteRuleset(context.Background(), "default", "nonexistent")
	if !errors.Is(err, service.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUpsertAndGetDraft(t *testing.T) {
	svc, db, _ := setup(t)
	ctx := context.Background()
	seedRuleset(t, db, "default", "draft-rules")

	draft, err := svc.UpsertDraft(ctx, "default", "draft-rules", validDSLJSON)
	if err != nil {
		t.Fatalf("UpsertDraft: %v", err)
	}
	if draft.RulesetKey != "draft-rules" {
		t.Errorf("RulesetKey: got %q, want %q", draft.RulesetKey, "draft-rules")
	}

	got, err := svc.GetDraft(ctx, "default", "draft-rules")
	if err != nil {
		t.Fatalf("GetDraft: %v", err)
	}
	if got.DSL == nil {
		t.Error("DSL is nil")
	}
}

func TestUpsertDraftInvalidDSL(t *testing.T) {
	svc, db, _ := setup(t)
	ctx := context.Background()
	seedRuleset(t, db, "default", "bad-draft")

	_, err := svc.UpsertDraft(ctx, "default", "bad-draft", json.RawMessage(`{"dsl_version":"v99"}`))
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	var ve *service.ValidationError
	if !errors.As(err, &ve) {
		t.Errorf("expected ValidationError, got %T: %v", err, err)
	}
}

func TestUpsertDraftRulesetNotFound(t *testing.T) {
	svc, _, _ := setup(t)
	_, err := svc.UpsertDraft(context.Background(), "default", "nonexistent", validDSLJSON)
	if !errors.Is(err, service.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestGetDraftNotFound(t *testing.T) {
	svc, db, _ := setup(t)
	seedRuleset(t, db, "default", "no-draft")
	_, err := svc.GetDraft(context.Background(), "default", "no-draft")
	if !errors.Is(err, service.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteDraft(t *testing.T) {
	svc, db, _ := setup(t)
	ctx := context.Background()
	seedRuleset(t, db, "default", "del-draft")

	svc.UpsertDraft(ctx, "default", "del-draft", validDSLJSON) //nolint:errcheck
	if err := svc.DeleteDraft(ctx, "default", "del-draft"); err != nil {
		t.Fatalf("DeleteDraft: %v", err)
	}
	_, err := svc.GetDraft(ctx, "default", "del-draft")
	if !errors.Is(err, service.ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestPublish(t *testing.T) {
	svc, db, _ := setup(t)
	ctx := context.Background()
	seedRuleset(t, db, "default", "pub-rules")

	svc.UpsertDraft(ctx, "default", "pub-rules", validDSLJSON) //nolint:errcheck

	v, err := svc.Publish(ctx, "default", "pub-rules")
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if v.Version != 1 {
		t.Errorf("version: got %d, want 1", v.Version)
	}
	if v.DSL == nil {
		t.Error("DSL is nil in response")
	}
	if v.Checksum == "" {
		t.Error("Checksum is empty")
	}
}

func TestPublishWithoutDraft(t *testing.T) {
	svc, db, _ := setup(t)
	ctx := context.Background()
	seedRuleset(t, db, "default", "no-draft-pub")

	_, err := svc.Publish(ctx, "default", "no-draft-pub")
	if !errors.Is(err, service.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestPublishNoChanges(t *testing.T) {
	svc, db, _ := setup(t)
	ctx := context.Background()
	seedRuleset(t, db, "default", "noop-rules")

	svc.UpsertDraft(ctx, "default", "noop-rules", validDSLJSON) //nolint:errcheck
	svc.Publish(ctx, "default", "noop-rules")                   //nolint:errcheck

	// Re-upsert same DSL and publish again
	svc.UpsertDraft(ctx, "default", "noop-rules", validDSLJSON) //nolint:errcheck
	_, err := svc.Publish(ctx, "default", "noop-rules")
	if !errors.Is(err, service.ErrNoChanges) {
		t.Errorf("expected ErrNoChanges, got %v", err)
	}
}

func TestPublishIncrementsVersion(t *testing.T) {
	svc, db, _ := setup(t)
	ctx := context.Background()
	seedRuleset(t, db, "default", "ver-rules")

	svc.UpsertDraft(ctx, "default", "ver-rules", validDSLJSON) //nolint:errcheck
	svc.Publish(ctx, "default", "ver-rules")                   //nolint:errcheck

	dsl2 := json.RawMessage(`{
		"dsl_version": "v1",
		"entry": "node-1",
		"nodes": [{
			"id": "node-1",
			"strategy": "all_matches",
			"schema": {"score": {"type": "number"}},
			"rules": [{
				"id": "r2", "name": "high",
				"when": [{"field": "score", "op": "gt", "value": 90}],
				"then": {"tier": "gold"}
			}]
		}]
	}`)
	svc.UpsertDraft(ctx, "default", "ver-rules", dsl2) //nolint:errcheck

	v, err := svc.Publish(ctx, "default", "ver-rules")
	if err != nil {
		t.Fatalf("second Publish: %v", err)
	}
	if v.Version != 2 {
		t.Errorf("version: got %d, want 2", v.Version)
	}
}

func TestGetVersion(t *testing.T) {
	svc, db, _ := setup(t)
	ctx := context.Background()
	seedRuleset(t, db, "default", "get-ver")

	svc.UpsertDraft(ctx, "default", "get-ver", validDSLJSON) //nolint:errcheck
	svc.Publish(ctx, "default", "get-ver")                   //nolint:errcheck

	v, err := svc.GetVersion(ctx, "default", "get-ver", 1)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if v.Version != 1 {
		t.Errorf("version: got %d, want 1", v.Version)
	}
	if v.DSL == nil {
		t.Error("DSL is nil")
	}
}

func TestGetVersionNotFound(t *testing.T) {
	svc, _, _ := setup(t)
	_, err := svc.GetVersion(context.Background(), "default", "nonexistent", 1)
	if !errors.Is(err, service.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestGetLatestVersion(t *testing.T) {
	svc, db, _ := setup(t)
	ctx := context.Background()
	seedRuleset(t, db, "default", "latest-ver")

	svc.UpsertDraft(ctx, "default", "latest-ver", validDSLJSON) //nolint:errcheck
	svc.Publish(ctx, "default", "latest-ver")                   //nolint:errcheck

	v, err := svc.GetLatestVersion(ctx, "default", "latest-ver")
	if err != nil {
		t.Fatalf("GetLatestVersion: %v", err)
	}
	if v.Version != 1 {
		t.Errorf("version: got %d, want 1", v.Version)
	}
}

func TestGetVersionBundle(t *testing.T) {
	svc, db, _ := setup(t)
	ctx := context.Background()
	seedRuleset(t, db, "default", "bundle-rules")

	svc.UpsertDraft(ctx, "default", "bundle-rules", validDSLJSON) //nolint:errcheck
	svc.Publish(ctx, "default", "bundle-rules")                   //nolint:errcheck

	data, err := svc.GetVersionBundle(ctx, "default", "bundle-rules", 1)
	if err != nil {
		t.Fatalf("GetVersionBundle: %v", err)
	}
	if len(data) == 0 {
		t.Error("bundle data is empty")
	}
}

func TestGetLatestBundle(t *testing.T) {
	svc, db, _ := setup(t)
	ctx := context.Background()
	seedRuleset(t, db, "default", "latest-bundle")

	svc.UpsertDraft(ctx, "default", "latest-bundle", validDSLJSON) //nolint:errcheck
	svc.Publish(ctx, "default", "latest-bundle")                   //nolint:errcheck

	ver, data, err := svc.GetLatestBundle(ctx, "default", "latest-bundle")
	if err != nil {
		t.Fatalf("GetLatestBundle: %v", err)
	}
	if ver != 1 {
		t.Errorf("version: got %d, want 1", ver)
	}
	if len(data) == 0 {
		t.Error("bundle data is empty")
	}
}

func TestListRulesetsEmpty(t *testing.T) {
	svc, _, _ := setup(t)
	list, err := svc.ListRulesets(context.Background(), "default", 50, 0)
	if err != nil {
		t.Fatalf("ListRulesets: %v", err)
	}
	if list == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(list) != 0 {
		t.Errorf("count: got %d, want 0", len(list))
	}
}

func TestListVersionsEmpty(t *testing.T) {
	svc, _, _ := setup(t)
	list, err := svc.ListVersions(context.Background(), "default", "nonexistent", 50, 0)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if list == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(list) != 0 {
		t.Errorf("count: got %d, want 0", len(list))
	}
}

func TestWorkspaceIsolation(t *testing.T) {
	svc, _, _ := setup(t)
	ctx := context.Background()

	svc.CreateRuleset(ctx, "ws-a", "shared", "Shared", "") //nolint:errcheck
	svc.CreateRuleset(ctx, "ws-b", "shared", "Shared", "") //nolint:errcheck

	listA, _ := svc.ListRulesets(ctx, "ws-a", 50, 0)
	listB, _ := svc.ListRulesets(ctx, "ws-b", 50, 0)

	if len(listA) != 1 {
		t.Errorf("ws-a: got %d rulesets, want 1", len(listA))
	}
	if len(listB) != 1 {
		t.Errorf("ws-b: got %d rulesets, want 1", len(listB))
	}
}
