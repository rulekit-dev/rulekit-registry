// Package testhelper provides a shared acceptance test suite for port.Datastore
// implementations.
package testhelper

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rulekit-dev/rulekit-registry/internal/domain"
	"github.com/rulekit-dev/rulekit-registry/internal/port"
)

// jsonEqual reports whether two JSON byte slices are semantically equal,
// regardless of key ordering. Used to compare DSL values across backends that
// may reorder keys (e.g. Postgres JSONB).
func jsonEqual(a, b []byte) bool {
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}
	an, _ := json.Marshal(av)
	bn, _ := json.Marshal(bv)
	return string(an) == string(bn)
}

// RunSuite runs the full acceptance test suite against the store returned by
// newStore. newStore is called once per sub-test so each test gets a clean
// store instance.
func RunSuite(t *testing.T, newStore func(t *testing.T) port.Datastore) {
	t.Helper()

	t.Run("TestCreateAndGetRuleset", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()

		now := time.Now().UTC().Truncate(time.Second)
		r := &domain.Ruleset{
			Workspace:   "acme",
			Key:         "shipping-rules",
			Name:        "Shipping Rules",
			Description: "Rules for shipping cost calculation",
			CreatedAt:   now,
			UpdatedAt:   now,
		}

		if err := s.CreateRuleset(ctx, r); err != nil {
			t.Fatalf("CreateRuleset: %v", err)
		}

		got, err := s.GetRuleset(ctx, "acme", "shipping-rules")
		if err != nil {
			t.Fatalf("GetRuleset: %v", err)
		}
		if got.Workspace != r.Workspace {
			t.Errorf("Workspace: got %q, want %q", got.Workspace, r.Workspace)
		}
		if got.Key != r.Key {
			t.Errorf("Key: got %q, want %q", got.Key, r.Key)
		}
		if got.Name != r.Name {
			t.Errorf("Name: got %q, want %q", got.Name, r.Name)
		}
		if got.Description != r.Description {
			t.Errorf("Description: got %q, want %q", got.Description, r.Description)
		}
		if !got.CreatedAt.Equal(r.CreatedAt) {
			t.Errorf("CreatedAt: got %v, want %v", got.CreatedAt, r.CreatedAt)
		}
		if !got.UpdatedAt.Equal(r.UpdatedAt) {
			t.Errorf("UpdatedAt: got %v, want %v", got.UpdatedAt, r.UpdatedAt)
		}

		// Non-existent ruleset should return ErrNotFound.
		_, err = s.GetRuleset(ctx, "acme", "nonexistent")
		if !errors.Is(err, port.ErrNotFound) {
			t.Errorf("GetRuleset non-existent: got %v, want ErrNotFound", err)
		}
	})

	t.Run("TestListRulesets", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()

		now := time.Now().UTC().Truncate(time.Second)
		workspace := "org-list"

		keys := []string{"ruleset-b", "ruleset-a", "ruleset-c"}
		for _, k := range keys {
			if err := s.CreateRuleset(ctx, &domain.Ruleset{
				Workspace: workspace,
				Key:       k,
				Name:      "Name " + k,
				CreatedAt: now,
				UpdatedAt: now,
			}); err != nil {
				t.Fatalf("CreateRuleset %q: %v", k, err)
			}
		}

		// Also create a ruleset in a different workspace to verify isolation.
		if err := s.CreateRuleset(ctx, &domain.Ruleset{
			Workspace: "other-workspace",
			Key:       "other-ruleset",
			Name:      "Other",
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateRuleset other-workspace: %v", err)
		}

		list, err := s.ListRulesets(ctx, workspace, 50, 0)
		if err != nil {
			t.Fatalf("ListRulesets: %v", err)
		}
		if len(list) != 3 {
			t.Fatalf("ListRulesets count: got %d, want 3", len(list))
		}

		// Verify alphabetical order by key.
		wantOrder := []string{"ruleset-a", "ruleset-b", "ruleset-c"}
		for i, r := range list {
			if r.Key != wantOrder[i] {
				t.Errorf("list[%d].Key: got %q, want %q", i, r.Key, wantOrder[i])
			}
		}

		// Different workspace should be isolated.
		otherList, err := s.ListRulesets(ctx, "other-workspace", 50, 0)
		if err != nil {
			t.Fatalf("ListRulesets other-workspace: %v", err)
		}
		if len(otherList) != 1 {
			t.Errorf("other-workspace count: got %d, want 1", len(otherList))
		}

		// Workspace with no rulesets returns empty slice (not error).
		emptyList, err := s.ListRulesets(ctx, "empty-workspace", 50, 0)
		if err != nil {
			t.Fatalf("ListRulesets empty-workspace: %v", err)
		}
		if len(emptyList) != 0 {
			t.Errorf("empty-workspace count: got %d, want 0", len(emptyList))
		}
	})

	t.Run("TestDraftUpsert", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()

		now := time.Now().UTC().Truncate(time.Second)
		workspace := "draft-workspace"
		key := "pricing-rules"

		// Create the parent ruleset first.
		if err := s.CreateRuleset(ctx, &domain.Ruleset{
			Workspace: workspace, Key: key, Name: "Pricing Rules",
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateRuleset: %v", err)
		}

		dsl1 := json.RawMessage(`{"dsl_version":"v1","strategy":"first_match","schema":{},"rules":[]}`)
		d := &domain.Draft{
			Workspace:  workspace,
			RulesetKey: key,
			DSL:        dsl1,
			UpdatedAt:  now,
		}

		// Insert draft.
		if err := s.UpsertDraft(ctx, d); err != nil {
			t.Fatalf("UpsertDraft (insert): %v", err)
		}

		got, err := s.GetDraft(ctx, workspace, key)
		if err != nil {
			t.Fatalf("GetDraft: %v", err)
		}
		if !jsonEqual(got.DSL, dsl1) {
			t.Errorf("DSL after insert: got %s, want %s", got.DSL, dsl1)
		}
		if got.Workspace != workspace {
			t.Errorf("Workspace: got %q, want %q", got.Workspace, workspace)
		}
		if got.RulesetKey != key {
			t.Errorf("RulesetKey: got %q, want %q", got.RulesetKey, key)
		}

		// Update draft.
		dsl2 := json.RawMessage(`{"dsl_version":"v1","strategy":"first_match","schema":{},"rules":[{"id":"r1"}]}`)
		later := now.Add(time.Minute)
		d2 := &domain.Draft{
			Workspace:  workspace,
			RulesetKey: key,
			DSL:        dsl2,
			UpdatedAt:  later,
		}
		if err := s.UpsertDraft(ctx, d2); err != nil {
			t.Fatalf("UpsertDraft (update): %v", err)
		}

		got2, err := s.GetDraft(ctx, workspace, key)
		if err != nil {
			t.Fatalf("GetDraft after update: %v", err)
		}
		if !jsonEqual(got2.DSL, dsl2) {
			t.Errorf("DSL after update: got %s, want %s", got2.DSL, dsl2)
		}

		// Get draft for non-existent key returns ErrNotFound.
		_, err = s.GetDraft(ctx, workspace, "nonexistent-key")
		if !errors.Is(err, port.ErrNotFound) {
			t.Errorf("GetDraft non-existent: got %v, want ErrNotFound", err)
		}
	})

	t.Run("TestPublishVersions", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()

		now := time.Now().UTC().Truncate(time.Second)
		workspace := "publish-workspace"
		key := "tax-rules"

		if err := s.CreateRuleset(ctx, &domain.Ruleset{
			Workspace: workspace, Key: key, Name: "Tax Rules",
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateRuleset: %v", err)
		}

		// NextVersionNumber returns 1 for a new ruleset.
		n, err := s.NextVersionNumber(ctx, workspace, key)
		if err != nil {
			t.Fatalf("NextVersionNumber (first): %v", err)
		}
		if n != 1 {
			t.Errorf("NextVersionNumber (first): got %d, want 1", n)
		}

		v1 := &domain.Version{
			Workspace:  workspace,
			RulesetKey: key,
			Version:    1,
			Checksum:   "sha256:abc123",
			CreatedAt:  now,
		}
		if err := s.CreateVersion(ctx, v1); err != nil {
			t.Fatalf("CreateVersion v1: %v", err)
		}

		// NextVersionNumber now returns 2.
		n2, err := s.NextVersionNumber(ctx, workspace, key)
		if err != nil {
			t.Fatalf("NextVersionNumber (second): %v", err)
		}
		if n2 != 2 {
			t.Errorf("NextVersionNumber (second): got %d, want 2", n2)
		}

		v2 := &domain.Version{
			Workspace:  workspace,
			RulesetKey: key,
			Version:    2,
			Checksum:   "sha256:def456",
			CreatedAt:  now.Add(time.Minute),
		}
		if err := s.CreateVersion(ctx, v2); err != nil {
			t.Fatalf("CreateVersion v2: %v", err)
		}

		// ListVersions returns [v1, v2] in ascending order.
		versions, err := s.ListVersions(ctx, workspace, key, 50, 0)
		if err != nil {
			t.Fatalf("ListVersions: %v", err)
		}
		if len(versions) != 2 {
			t.Fatalf("ListVersions count: got %d, want 2", len(versions))
		}
		if versions[0].Version != 1 {
			t.Errorf("versions[0].Version: got %d, want 1", versions[0].Version)
		}
		if versions[1].Version != 2 {
			t.Errorf("versions[1].Version: got %d, want 2", versions[1].Version)
		}

		// GetLatestVersion returns v2.
		latest, err := s.GetLatestVersion(ctx, workspace, key)
		if err != nil {
			t.Fatalf("GetLatestVersion: %v", err)
		}
		if latest.Version != 2 {
			t.Errorf("GetLatestVersion.Version: got %d, want 2", latest.Version)
		}
		if latest.Checksum != v2.Checksum {
			t.Errorf("GetLatestVersion.Checksum: got %q, want %q", latest.Checksum, v2.Checksum)
		}

		// GetVersion(v1) returns v1.
		gotV1, err := s.GetVersion(ctx, workspace, key, 1)
		if err != nil {
			t.Fatalf("GetVersion(1): %v", err)
		}
		if gotV1.Version != 1 {
			t.Errorf("GetVersion(1).Version: got %d, want 1", gotV1.Version)
		}
		if gotV1.Checksum != v1.Checksum {
			t.Errorf("GetVersion(1).Checksum: got %q, want %q", gotV1.Checksum, v1.Checksum)
		}
	})

	t.Run("TestVersionImmutability", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()

		now := time.Now().UTC().Truncate(time.Second)
		workspace := "immutable-workspace"
		key := "immutable-rules"

		if err := s.CreateRuleset(ctx, &domain.Ruleset{
			Workspace: workspace, Key: key, Name: "Immutable Rules",
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateRuleset: %v", err)
		}

		v1 := &domain.Version{
			Workspace:  workspace,
			RulesetKey: key,
			Version:    1,
			Checksum:   "sha256:aaa",
			CreatedAt:  now,
		}

		// First creation should succeed.
		if err := s.CreateVersion(ctx, v1); err != nil {
			t.Fatalf("CreateVersion (first): %v", err)
		}

		// Second creation of the same version must return ErrVersionImmutable.
		err := s.CreateVersion(ctx, v1)
		if !errors.Is(err, port.ErrVersionImmutable) {
			t.Errorf("CreateVersion (duplicate): got %v, want ErrVersionImmutable", err)
		}
	})

	t.Run("TestWorkspaceIsolation", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()

		now := time.Now().UTC().Truncate(time.Second)
		key := "shared-key"

		// Create the same key in two different workspaces — both should succeed.
		for _, workspace := range []string{"workspace-alpha", "workspace-beta"} {
			if err := s.CreateRuleset(ctx, &domain.Ruleset{
				Workspace: workspace, Key: key, Name: "Name in " + workspace,
				CreatedAt: now, UpdatedAt: now,
			}); err != nil {
				t.Fatalf("CreateRuleset workspace=%q: %v", workspace, err)
			}
		}

		// Each workspace returns its own record.
		for _, workspace := range []string{"workspace-alpha", "workspace-beta"} {
			r, err := s.GetRuleset(ctx, workspace, key)
			if err != nil {
				t.Fatalf("GetRuleset workspace=%q: %v", workspace, err)
			}
			if r.Workspace != workspace {
				t.Errorf("GetRuleset workspace=%q: Workspace=%q", workspace, r.Workspace)
			}
			wantName := "Name in " + workspace
			if r.Name != wantName {
				t.Errorf("GetRuleset workspace=%q: Name=%q, want %q", workspace, r.Name, wantName)
			}
		}

		// List results are workspace-scoped.
		for _, workspace := range []string{"workspace-alpha", "workspace-beta"} {
			list, err := s.ListRulesets(ctx, workspace, 50, 0)
			if err != nil {
				t.Fatalf("ListRulesets workspace=%q: %v", workspace, err)
			}
			if len(list) != 1 {
				t.Errorf("ListRulesets workspace=%q: got %d results, want 1", workspace, len(list))
			}
		}

		// Versions are also workspace-scoped.
		dsl := json.RawMessage(`{"dsl_version":"v1","strategy":"first_match","schema":{},"rules":[]}`)
		for _, workspace := range []string{"workspace-alpha", "workspace-beta"} {
			if err := s.CreateVersion(ctx, &domain.Version{
				Workspace:  workspace,
				RulesetKey: key,
				Version:    1,
				Checksum:   "sha256:" + workspace,
				DSL:        dsl,
				CreatedAt:  now,
			}); err != nil {
				t.Fatalf("CreateVersion workspace=%q: %v", workspace, err)
			}
		}

		// Each workspace has exactly one version.
		for _, workspace := range []string{"workspace-alpha", "workspace-beta"} {
			versions, err := s.ListVersions(ctx, workspace, key, 50, 0)
			if err != nil {
				t.Fatalf("ListVersions workspace=%q: %v", workspace, err)
			}
			if len(versions) != 1 {
				t.Errorf("ListVersions workspace=%q: got %d, want 1", workspace, len(versions))
			}
			if versions[0].Checksum != "sha256:"+workspace {
				t.Errorf("ListVersions workspace=%q: Checksum=%q, want %q",
					workspace, versions[0].Checksum, "sha256:"+workspace)
			}
		}
	})
}
