// Package testhelper provides a shared acceptance test suite for datastore.Datastore
// implementations.
package testhelper

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rulekit-dev/rulekit-registry/internal/model"
	"github.com/rulekit-dev/rulekit-registry/internal/datastore"
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
func RunSuite(t *testing.T, newStore func(t *testing.T) datastore.Datastore) {
	t.Helper()

	t.Run("TestCreateAndGetRuleset", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()

		now := time.Now().UTC().Truncate(time.Second)
		r := &model.Ruleset{
			Namespace:   "acme",
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
		if got.Namespace != r.Namespace {
			t.Errorf("Namespace: got %q, want %q", got.Namespace, r.Namespace)
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
		if !errors.Is(err, datastore.ErrNotFound) {
			t.Errorf("GetRuleset non-existent: got %v, want ErrNotFound", err)
		}
	})

	t.Run("TestListRulesets", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()

		now := time.Now().UTC().Truncate(time.Second)
		ns := "org-list"

		keys := []string{"ruleset-b", "ruleset-a", "ruleset-c"}
		for _, k := range keys {
			if err := s.CreateRuleset(ctx, &model.Ruleset{
				Namespace: ns,
				Key:       k,
				Name:      "Name " + k,
				CreatedAt: now,
				UpdatedAt: now,
			}); err != nil {
				t.Fatalf("CreateRuleset %q: %v", k, err)
			}
		}

		// Also create a ruleset in a different namespace to verify isolation.
		if err := s.CreateRuleset(ctx, &model.Ruleset{
			Namespace: "other-ns",
			Key:       "other-ruleset",
			Name:      "Other",
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateRuleset other-ns: %v", err)
		}

		list, err := s.ListRulesets(ctx, ns, 50, 0)
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

		// Different namespace should be isolated.
		otherList, err := s.ListRulesets(ctx, "other-ns", 50, 0)
		if err != nil {
			t.Fatalf("ListRulesets other-ns: %v", err)
		}
		if len(otherList) != 1 {
			t.Errorf("other-ns count: got %d, want 1", len(otherList))
		}

		// Namespace with no rulesets returns empty slice (not error).
		emptyList, err := s.ListRulesets(ctx, "empty-ns", 50, 0)
		if err != nil {
			t.Fatalf("ListRulesets empty-ns: %v", err)
		}
		if len(emptyList) != 0 {
			t.Errorf("empty-ns count: got %d, want 0", len(emptyList))
		}
	})

	t.Run("TestDraftUpsert", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()

		now := time.Now().UTC().Truncate(time.Second)
		ns := "draft-ns"
		key := "pricing-rules"

		// Create the parent ruleset first.
		if err := s.CreateRuleset(ctx, &model.Ruleset{
			Namespace: ns, Key: key, Name: "Pricing Rules",
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateRuleset: %v", err)
		}

		dsl1 := json.RawMessage(`{"dsl_version":"v1","strategy":"first_match","schema":{},"rules":[]}`)
		d := &model.Draft{
			Namespace:  ns,
			RulesetKey: key,
			DSL:        dsl1,
			UpdatedAt:  now,
		}

		// Insert draft.
		if err := s.UpsertDraft(ctx, d); err != nil {
			t.Fatalf("UpsertDraft (insert): %v", err)
		}

		got, err := s.GetDraft(ctx, ns, key)
		if err != nil {
			t.Fatalf("GetDraft: %v", err)
		}
		if !jsonEqual(got.DSL, dsl1) {
			t.Errorf("DSL after insert: got %s, want %s", got.DSL, dsl1)
		}
		if got.Namespace != ns {
			t.Errorf("Namespace: got %q, want %q", got.Namespace, ns)
		}
		if got.RulesetKey != key {
			t.Errorf("RulesetKey: got %q, want %q", got.RulesetKey, key)
		}

		// Update draft.
		dsl2 := json.RawMessage(`{"dsl_version":"v1","strategy":"first_match","schema":{},"rules":[{"id":"r1"}]}`)
		later := now.Add(time.Minute)
		d2 := &model.Draft{
			Namespace:  ns,
			RulesetKey: key,
			DSL:        dsl2,
			UpdatedAt:  later,
		}
		if err := s.UpsertDraft(ctx, d2); err != nil {
			t.Fatalf("UpsertDraft (update): %v", err)
		}

		got2, err := s.GetDraft(ctx, ns, key)
		if err != nil {
			t.Fatalf("GetDraft after update: %v", err)
		}
		if !jsonEqual(got2.DSL, dsl2) {
			t.Errorf("DSL after update: got %s, want %s", got2.DSL, dsl2)
		}

		// Get draft for non-existent key returns ErrNotFound.
		_, err = s.GetDraft(ctx, ns, "nonexistent-key")
		if !errors.Is(err, datastore.ErrNotFound) {
			t.Errorf("GetDraft non-existent: got %v, want ErrNotFound", err)
		}
	})

	t.Run("TestPublishVersions", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()

		now := time.Now().UTC().Truncate(time.Second)
		ns := "publish-ns"
		key := "tax-rules"

		if err := s.CreateRuleset(ctx, &model.Ruleset{
			Namespace: ns, Key: key, Name: "Tax Rules",
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateRuleset: %v", err)
		}

		// NextVersionNumber returns 1 for a new ruleset.
		n, err := s.NextVersionNumber(ctx, ns, key)
		if err != nil {
			t.Fatalf("NextVersionNumber (first): %v", err)
		}
		if n != 1 {
			t.Errorf("NextVersionNumber (first): got %d, want 1", n)
		}

		v1 := &model.Version{
			Namespace:  ns,
			RulesetKey: key,
			Version:    1,
			Checksum:   "sha256:abc123",
			CreatedAt:  now,
		}
		if err := s.CreateVersion(ctx, v1); err != nil {
			t.Fatalf("CreateVersion v1: %v", err)
		}

		// NextVersionNumber now returns 2.
		n2, err := s.NextVersionNumber(ctx, ns, key)
		if err != nil {
			t.Fatalf("NextVersionNumber (second): %v", err)
		}
		if n2 != 2 {
			t.Errorf("NextVersionNumber (second): got %d, want 2", n2)
		}

		v2 := &model.Version{
			Namespace:  ns,
			RulesetKey: key,
			Version:    2,
			Checksum:   "sha256:def456",
			CreatedAt:  now.Add(time.Minute),
		}
		if err := s.CreateVersion(ctx, v2); err != nil {
			t.Fatalf("CreateVersion v2: %v", err)
		}

		// ListVersions returns [v1, v2] in ascending order.
		versions, err := s.ListVersions(ctx, ns, key, 50, 0)
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
		latest, err := s.GetLatestVersion(ctx, ns, key)
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
		gotV1, err := s.GetVersion(ctx, ns, key, 1)
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
		ns := "immutable-ns"
		key := "immutable-rules"

		if err := s.CreateRuleset(ctx, &model.Ruleset{
			Namespace: ns, Key: key, Name: "Immutable Rules",
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateRuleset: %v", err)
		}

		v1 := &model.Version{
			Namespace:  ns,
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
		if !errors.Is(err, datastore.ErrVersionImmutable) {
			t.Errorf("CreateVersion (duplicate): got %v, want ErrVersionImmutable", err)
		}
	})

	t.Run("TestNamespaceIsolation", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()

		now := time.Now().UTC().Truncate(time.Second)
		key := "shared-key"

		// Create the same key in two different namespaces — both should succeed.
		for _, ns := range []string{"ns-alpha", "ns-beta"} {
			if err := s.CreateRuleset(ctx, &model.Ruleset{
				Namespace: ns, Key: key, Name: "Name in " + ns,
				CreatedAt: now, UpdatedAt: now,
			}); err != nil {
				t.Fatalf("CreateRuleset ns=%q: %v", ns, err)
			}
		}

		// Each namespace returns its own record.
		for _, ns := range []string{"ns-alpha", "ns-beta"} {
			r, err := s.GetRuleset(ctx, ns, key)
			if err != nil {
				t.Fatalf("GetRuleset ns=%q: %v", ns, err)
			}
			if r.Namespace != ns {
				t.Errorf("GetRuleset ns=%q: Namespace=%q", ns, r.Namespace)
			}
			wantName := "Name in " + ns
			if r.Name != wantName {
				t.Errorf("GetRuleset ns=%q: Name=%q, want %q", ns, r.Name, wantName)
			}
		}

		// List results are namespace-scoped.
		for _, ns := range []string{"ns-alpha", "ns-beta"} {
			list, err := s.ListRulesets(ctx, ns, 50, 0)
			if err != nil {
				t.Fatalf("ListRulesets ns=%q: %v", ns, err)
			}
			if len(list) != 1 {
				t.Errorf("ListRulesets ns=%q: got %d results, want 1", ns, len(list))
			}
		}

		// Versions are also namespace-scoped.
		dsl := json.RawMessage(`{"dsl_version":"v1","strategy":"first_match","schema":{},"rules":[]}`)
		for _, ns := range []string{"ns-alpha", "ns-beta"} {
			if err := s.CreateVersion(ctx, &model.Version{
				Namespace:  ns,
				RulesetKey: key,
				Version:    1,
				Checksum:   "sha256:" + ns,
				DSL:        dsl,
				CreatedAt:  now,
			}); err != nil {
				t.Fatalf("CreateVersion ns=%q: %v", ns, err)
			}
		}

		// Each namespace has exactly one version.
		for _, ns := range []string{"ns-alpha", "ns-beta"} {
			versions, err := s.ListVersions(ctx, ns, key, 50, 0)
			if err != nil {
				t.Fatalf("ListVersions ns=%q: %v", ns, err)
			}
			if len(versions) != 1 {
				t.Errorf("ListVersions ns=%q: got %d, want 1", ns, len(versions))
			}
			if versions[0].Checksum != "sha256:"+ns {
				t.Errorf("ListVersions ns=%q: Checksum=%q, want %q",
					ns, versions[0].Checksum, "sha256:"+ns)
			}
		}
	})
}
