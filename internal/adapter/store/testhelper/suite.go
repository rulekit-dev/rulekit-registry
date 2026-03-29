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

func RunSuite(t *testing.T, newStore func(t *testing.T) port.Datastore) {
	t.Helper()

	t.Run("CreateAndGetWorkspace", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		ws := &domain.Workspace{Name: "test-ws", Description: "desc", CreatedAt: now}
		if err := s.CreateWorkspace(ctx, ws); err != nil {
			t.Fatalf("CreateWorkspace: %v", err)
		}

		got, err := s.GetWorkspace(ctx, "test-ws")
		if err != nil {
			t.Fatalf("GetWorkspace: %v", err)
		}
		if got.Name != ws.Name || got.Description != ws.Description {
			t.Errorf("got %+v, want %+v", got, ws)
		}
		if !got.CreatedAt.Equal(now) {
			t.Errorf("CreatedAt: got %v, want %v", got.CreatedAt, now)
		}

		if err := s.CreateWorkspace(ctx, ws); !errors.Is(err, port.ErrAlreadyExists) {
			t.Errorf("duplicate: got %v, want ErrAlreadyExists", err)
		}

		_, err = s.GetWorkspace(ctx, "nonexistent")
		if !errors.Is(err, port.ErrNotFound) {
			t.Errorf("not found: got %v, want ErrNotFound", err)
		}
	})

	t.Run("ListWorkspaces", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		for _, name := range []string{"ws-b", "ws-a", "ws-c"} {
			if err := s.CreateWorkspace(ctx, &domain.Workspace{Name: name, CreatedAt: now}); err != nil {
				t.Fatalf("CreateWorkspace %q: %v", name, err)
			}
		}

		list, err := s.ListWorkspaces(ctx, 50, 0)
		if err != nil {
			t.Fatalf("ListWorkspaces: %v", err)
		}
		if len(list) != 3 {
			t.Fatalf("count: got %d, want 3", len(list))
		}
		want := []string{"ws-a", "ws-b", "ws-c"}
		for i, ws := range list {
			if ws.Name != want[i] {
				t.Errorf("list[%d].Name: got %q, want %q", i, ws.Name, want[i])
			}
		}
	})

	t.Run("DeleteWorkspace", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		if err := s.CreateWorkspace(ctx, &domain.Workspace{Name: "del-ws", CreatedAt: now}); err != nil {
			t.Fatalf("CreateWorkspace: %v", err)
		}
		if err := s.DeleteWorkspace(ctx, "del-ws"); err != nil {
			t.Fatalf("DeleteWorkspace: %v", err)
		}
		if err := s.DeleteWorkspace(ctx, "del-ws"); !errors.Is(err, port.ErrNotFound) {
			t.Errorf("delete again: got %v, want ErrNotFound", err)
		}
	})

	t.Run("CreateAndGetRuleset", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		r := &domain.Ruleset{
			Workspace: "acme", Key: "shipping-rules", Name: "Shipping Rules",
			Description: "Rules for shipping cost calculation",
			CreatedAt:   now, UpdatedAt: now,
		}
		if err := s.CreateRuleset(ctx, r); err != nil {
			t.Fatalf("CreateRuleset: %v", err)
		}

		got, err := s.GetRuleset(ctx, "acme", "shipping-rules")
		if err != nil {
			t.Fatalf("GetRuleset: %v", err)
		}
		if got.Workspace != r.Workspace || got.Key != r.Key || got.Name != r.Name || got.Description != r.Description {
			t.Errorf("got %+v, want %+v", got, r)
		}
		if !got.CreatedAt.Equal(r.CreatedAt) || !got.UpdatedAt.Equal(r.UpdatedAt) {
			t.Errorf("timestamps mismatch")
		}

		if err := s.CreateRuleset(ctx, r); !errors.Is(err, port.ErrAlreadyExists) {
			t.Errorf("duplicate: got %v, want ErrAlreadyExists", err)
		}

		_, err = s.GetRuleset(ctx, "acme", "nonexistent")
		if !errors.Is(err, port.ErrNotFound) {
			t.Errorf("not found: got %v, want ErrNotFound", err)
		}
	})

	t.Run("ListRulesets", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		workspace := "org-list"
		for _, k := range []string{"ruleset-b", "ruleset-a", "ruleset-c"} {
			if err := s.CreateRuleset(ctx, &domain.Ruleset{
				Workspace: workspace, Key: k, Name: "Name " + k,
				CreatedAt: now, UpdatedAt: now,
			}); err != nil {
				t.Fatalf("CreateRuleset %q: %v", k, err)
			}
		}

		list, err := s.ListRulesets(ctx, workspace, 50, 0)
		if err != nil {
			t.Fatalf("ListRulesets: %v", err)
		}
		if len(list) != 3 {
			t.Fatalf("count: got %d, want 3", len(list))
		}
		want := []string{"ruleset-a", "ruleset-b", "ruleset-c"}
		for i, r := range list {
			if r.Key != want[i] {
				t.Errorf("list[%d].Key: got %q, want %q", i, r.Key, want[i])
			}
		}
	})

	t.Run("DeleteRuleset", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		if err := s.CreateRuleset(ctx, &domain.Ruleset{
			Workspace: "ws", Key: "del-rs", Name: "Del", CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateRuleset: %v", err)
		}
		if err := s.DeleteRuleset(ctx, "ws", "del-rs"); err != nil {
			t.Fatalf("DeleteRuleset: %v", err)
		}
		if err := s.DeleteRuleset(ctx, "ws", "del-rs"); !errors.Is(err, port.ErrNotFound) {
			t.Errorf("delete again: got %v, want ErrNotFound", err)
		}
	})

	t.Run("DraftUpsert", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		dsl1 := json.RawMessage(`{"dsl_version":"v1","strategy":"first_match","schema":{},"rules":[]}`)
		d := &domain.Draft{Workspace: "ws", RulesetKey: "pricing", DSL: dsl1, UpdatedAt: now}

		if err := s.UpsertDraft(ctx, d); err != nil {
			t.Fatalf("UpsertDraft (insert): %v", err)
		}

		got, err := s.GetDraft(ctx, "ws", "pricing")
		if err != nil {
			t.Fatalf("GetDraft: %v", err)
		}
		if !jsonEqual(got.DSL, dsl1) {
			t.Errorf("DSL: got %s, want %s", got.DSL, dsl1)
		}

		dsl2 := json.RawMessage(`{"dsl_version":"v1","strategy":"first_match","schema":{},"rules":[{"id":"r1"}]}`)
		d2 := &domain.Draft{Workspace: "ws", RulesetKey: "pricing", DSL: dsl2, UpdatedAt: now.Add(time.Minute)}
		if err := s.UpsertDraft(ctx, d2); err != nil {
			t.Fatalf("UpsertDraft (update): %v", err)
		}

		got2, err := s.GetDraft(ctx, "ws", "pricing")
		if err != nil {
			t.Fatalf("GetDraft after update: %v", err)
		}
		if !jsonEqual(got2.DSL, dsl2) {
			t.Errorf("DSL after update: got %s, want %s", got2.DSL, dsl2)
		}

		_, err = s.GetDraft(ctx, "ws", "nonexistent")
		if !errors.Is(err, port.ErrNotFound) {
			t.Errorf("not found: got %v, want ErrNotFound", err)
		}
	})

	t.Run("DeleteDraft", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		d := &domain.Draft{
			Workspace: "ws", RulesetKey: "del-draft",
			DSL: json.RawMessage(`{}`), UpdatedAt: now,
		}
		if err := s.UpsertDraft(ctx, d); err != nil {
			t.Fatalf("UpsertDraft: %v", err)
		}
		if err := s.DeleteDraft(ctx, "ws", "del-draft"); err != nil {
			t.Fatalf("DeleteDraft: %v", err)
		}
		if err := s.DeleteDraft(ctx, "ws", "del-draft"); !errors.Is(err, port.ErrNotFound) {
			t.Errorf("delete again: got %v, want ErrNotFound", err)
		}
	})

	t.Run("PublishVersions", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)
		workspace, key := "pub-ws", "tax-rules"

		n, err := s.NextVersionNumber(ctx, workspace, key)
		if err != nil {
			t.Fatalf("NextVersionNumber: %v", err)
		}
		if n != 1 {
			t.Errorf("first NextVersionNumber: got %d, want 1", n)
		}

		v1 := &domain.Version{Workspace: workspace, RulesetKey: key, Version: 1, Checksum: "sha256:abc", CreatedAt: now}
		if err := s.CreateVersion(ctx, v1); err != nil {
			t.Fatalf("CreateVersion v1: %v", err)
		}

		n2, err := s.NextVersionNumber(ctx, workspace, key)
		if err != nil {
			t.Fatalf("NextVersionNumber: %v", err)
		}
		if n2 != 2 {
			t.Errorf("second NextVersionNumber: got %d, want 2", n2)
		}

		v2 := &domain.Version{Workspace: workspace, RulesetKey: key, Version: 2, Checksum: "sha256:def", CreatedAt: now.Add(time.Minute)}
		if err := s.CreateVersion(ctx, v2); err != nil {
			t.Fatalf("CreateVersion v2: %v", err)
		}

		versions, err := s.ListVersions(ctx, workspace, key, 50, 0)
		if err != nil {
			t.Fatalf("ListVersions: %v", err)
		}
		if len(versions) != 2 {
			t.Fatalf("count: got %d, want 2", len(versions))
		}
		if versions[0].Version != 1 || versions[1].Version != 2 {
			t.Errorf("order: got [%d, %d], want [1, 2]", versions[0].Version, versions[1].Version)
		}

		latest, err := s.GetLatestVersion(ctx, workspace, key)
		if err != nil {
			t.Fatalf("GetLatestVersion: %v", err)
		}
		if latest.Version != 2 || latest.Checksum != v2.Checksum {
			t.Errorf("latest: got v%d %s, want v2 %s", latest.Version, latest.Checksum, v2.Checksum)
		}

		gotV1, err := s.GetVersion(ctx, workspace, key, 1)
		if err != nil {
			t.Fatalf("GetVersion(1): %v", err)
		}
		if gotV1.Checksum != v1.Checksum {
			t.Errorf("GetVersion(1).Checksum: got %q, want %q", gotV1.Checksum, v1.Checksum)
		}

		_, err = s.GetVersion(ctx, workspace, key, 99)
		if !errors.Is(err, port.ErrNotFound) {
			t.Errorf("not found: got %v, want ErrNotFound", err)
		}
	})

	t.Run("VersionImmutability", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		v := &domain.Version{Workspace: "imm-ws", RulesetKey: "imm-rs", Version: 1, Checksum: "sha256:aaa", CreatedAt: now}
		if err := s.CreateVersion(ctx, v); err != nil {
			t.Fatalf("CreateVersion: %v", err)
		}
		if err := s.CreateVersion(ctx, v); !errors.Is(err, port.ErrVersionImmutable) {
			t.Errorf("duplicate: got %v, want ErrVersionImmutable", err)
		}
	})

	t.Run("WorkspaceIsolation", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)
		key := "shared-key"

		for _, ws := range []string{"ws-alpha", "ws-beta"} {
			if err := s.CreateRuleset(ctx, &domain.Ruleset{
				Workspace: ws, Key: key, Name: "Name in " + ws,
				CreatedAt: now, UpdatedAt: now,
			}); err != nil {
				t.Fatalf("CreateRuleset ws=%q: %v", ws, err)
			}
		}

		for _, ws := range []string{"ws-alpha", "ws-beta"} {
			r, err := s.GetRuleset(ctx, ws, key)
			if err != nil {
				t.Fatalf("GetRuleset ws=%q: %v", ws, err)
			}
			if r.Name != "Name in "+ws {
				t.Errorf("Name: got %q, want %q", r.Name, "Name in "+ws)
			}
			list, err := s.ListRulesets(ctx, ws, 50, 0)
			if err != nil {
				t.Fatalf("ListRulesets ws=%q: %v", ws, err)
			}
			if len(list) != 1 {
				t.Errorf("ListRulesets ws=%q: got %d, want 1", ws, len(list))
			}
		}
	})

	t.Run("UserCRUD", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		u := &domain.User{ID: "u1", Email: "a@b.com", CreatedAt: now, LastLoginAt: now}
		if err := s.CreateUser(ctx, u); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		if err := s.CreateUser(ctx, u); !errors.Is(err, port.ErrAlreadyExists) {
			t.Errorf("duplicate: got %v, want ErrAlreadyExists", err)
		}

		got, err := s.GetUserByID(ctx, "u1")
		if err != nil {
			t.Fatalf("GetUserByID: %v", err)
		}
		if got.Email != u.Email {
			t.Errorf("Email: got %q, want %q", got.Email, u.Email)
		}

		got2, err := s.GetUserByEmail(ctx, "a@b.com")
		if err != nil {
			t.Fatalf("GetUserByEmail: %v", err)
		}
		if got2.ID != u.ID {
			t.Errorf("ID: got %q, want %q", got2.ID, u.ID)
		}

		if err := s.UpdateUserLastLogin(ctx, "u1"); err != nil {
			t.Fatalf("UpdateUserLastLogin: %v", err)
		}

		users, err := s.ListUsers(ctx, 50, 0)
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		if len(users) != 1 {
			t.Errorf("ListUsers count: got %d, want 1", len(users))
		}

		if err := s.DeleteUser(ctx, "u1"); err != nil {
			t.Fatalf("DeleteUser: %v", err)
		}
		if err := s.DeleteUser(ctx, "u1"); !errors.Is(err, port.ErrNotFound) {
			t.Errorf("delete again: got %v, want ErrNotFound", err)
		}

		_, err = s.GetUserByID(ctx, "u1")
		if !errors.Is(err, port.ErrNotFound) {
			t.Errorf("after delete: got %v, want ErrNotFound", err)
		}
	})

	t.Run("OTPCodes", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		u := &domain.User{ID: "otp-user", Email: "otp@test.com", CreatedAt: now, LastLoginAt: now}
		if err := s.CreateUser(ctx, u); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}

		otp := &domain.OTPCode{
			ID: "otp1", UserID: "otp-user", CodeHash: "hash123",
			ExpiresAt: now.Add(10 * time.Minute),
		}
		if err := s.CreateOTPCode(ctx, otp); err != nil {
			t.Fatalf("CreateOTPCode: %v", err)
		}

		got, err := s.GetUnusedOTPCode(ctx, "otp-user")
		if err != nil {
			t.Fatalf("GetUnusedOTPCode: %v", err)
		}
		if got.ID != "otp1" || got.CodeHash != "hash123" {
			t.Errorf("got %+v", got)
		}

		if err := s.MarkOTPUsed(ctx, "otp1"); err != nil {
			t.Fatalf("MarkOTPUsed: %v", err)
		}

		_, err = s.GetUnusedOTPCode(ctx, "otp-user")
		if !errors.Is(err, port.ErrNotFound) {
			t.Errorf("after mark used: got %v, want ErrNotFound", err)
		}

		if err := s.DeleteExpiredOTPs(ctx); err != nil {
			t.Fatalf("DeleteExpiredOTPs: %v", err)
		}
	})

	t.Run("RefreshTokens", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		u := &domain.User{ID: "rt-user", Email: "rt@test.com", CreatedAt: now, LastLoginAt: now}
		if err := s.CreateUser(ctx, u); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}

		rt := &domain.RefreshToken{
			ID: "rt1", UserID: "rt-user", TokenHash: "thash1",
			ExpiresAt: now.Add(time.Hour),
		}
		if err := s.CreateRefreshToken(ctx, rt); err != nil {
			t.Fatalf("CreateRefreshToken: %v", err)
		}

		got, err := s.GetRefreshTokenByHash(ctx, "thash1")
		if err != nil {
			t.Fatalf("GetRefreshTokenByHash: %v", err)
		}
		if got.ID != "rt1" || got.RevokedAt != nil {
			t.Errorf("got %+v", got)
		}

		if err := s.RevokeRefreshToken(ctx, "rt1"); err != nil {
			t.Fatalf("RevokeRefreshToken: %v", err)
		}

		got2, err := s.GetRefreshTokenByHash(ctx, "thash1")
		if err != nil {
			t.Fatalf("GetRefreshTokenByHash after revoke: %v", err)
		}
		if got2.RevokedAt == nil {
			t.Error("RevokedAt should be set after revoke")
		}

		if err := s.RevokeRefreshToken(ctx, "rt1"); !errors.Is(err, port.ErrNotFound) {
			t.Errorf("revoke again: got %v, want ErrNotFound", err)
		}

		_, err = s.GetRefreshTokenByHash(ctx, "nonexistent")
		if !errors.Is(err, port.ErrNotFound) {
			t.Errorf("not found: got %v, want ErrNotFound", err)
		}
	})

	t.Run("APIKeys", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		k := &domain.APIKey{
			ID: "ak1", Name: "my-key", KeyHash: "khash1",
			Workspace: "ws", Role: domain.RoleEditor,
			CreatedAt: now,
		}
		if err := s.CreateAPIKey(ctx, k); err != nil {
			t.Fatalf("CreateAPIKey: %v", err)
		}

		got, err := s.GetAPIKeyByHash(ctx, "khash1")
		if err != nil {
			t.Fatalf("GetAPIKeyByHash: %v", err)
		}
		if got.ID != "ak1" || got.Role != domain.RoleEditor || got.RevokedAt != nil {
			t.Errorf("got %+v", got)
		}

		keys, err := s.ListAPIKeys(ctx, 50, 0)
		if err != nil {
			t.Fatalf("ListAPIKeys: %v", err)
		}
		if len(keys) != 1 {
			t.Errorf("ListAPIKeys count: got %d, want 1", len(keys))
		}

		if err := s.RevokeAPIKey(ctx, "ak1"); err != nil {
			t.Fatalf("RevokeAPIKey: %v", err)
		}
		got2, err := s.GetAPIKeyByHash(ctx, "khash1")
		if err != nil {
			t.Fatalf("GetAPIKeyByHash after revoke: %v", err)
		}
		if got2.RevokedAt == nil {
			t.Error("RevokedAt should be set after revoke")
		}

		if err := s.RevokeAPIKey(ctx, "ak1"); !errors.Is(err, port.ErrNotFound) {
			t.Errorf("revoke again: got %v, want ErrNotFound", err)
		}

		_, err = s.GetAPIKeyByHash(ctx, "nonexistent")
		if !errors.Is(err, port.ErrNotFound) {
			t.Errorf("not found: got %v, want ErrNotFound", err)
		}
	})

	t.Run("APIKeyWithExpiry", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)
		exp := now.Add(24 * time.Hour)

		k := &domain.APIKey{
			ID: "ak-exp", Name: "expiring-key", KeyHash: "khash-exp",
			Workspace: "ws", Role: domain.RoleViewer,
			CreatedAt: now, ExpiresAt: &exp,
		}
		if err := s.CreateAPIKey(ctx, k); err != nil {
			t.Fatalf("CreateAPIKey: %v", err)
		}

		got, err := s.GetAPIKeyByHash(ctx, "khash-exp")
		if err != nil {
			t.Fatalf("GetAPIKeyByHash: %v", err)
		}
		if got.ExpiresAt == nil {
			t.Fatal("ExpiresAt should not be nil")
		}
		if !got.ExpiresAt.Equal(exp) {
			t.Errorf("ExpiresAt: got %v, want %v", got.ExpiresAt, exp)
		}
	})

	t.Run("UserRoles", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		u := &domain.User{ID: "ur-user", Email: "ur@test.com", CreatedAt: now, LastLoginAt: now}
		if err := s.CreateUser(ctx, u); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}

		ur := &domain.UserRole{UserID: "ur-user", Workspace: "ws1", RoleMask: domain.RoleEditor}
		if err := s.UpsertUserRole(ctx, ur); err != nil {
			t.Fatalf("UpsertUserRole: %v", err)
		}

		got, err := s.GetUserRole(ctx, "ur-user", "ws1")
		if err != nil {
			t.Fatalf("GetUserRole: %v", err)
		}
		if got.RoleMask != domain.RoleEditor {
			t.Errorf("RoleMask: got %d, want %d", got.RoleMask, domain.RoleEditor)
		}

		ur2 := &domain.UserRole{UserID: "ur-user", Workspace: "ws1", RoleMask: domain.RoleAdmin}
		if err := s.UpsertUserRole(ctx, ur2); err != nil {
			t.Fatalf("UpsertUserRole (update): %v", err)
		}
		got2, err := s.GetUserRole(ctx, "ur-user", "ws1")
		if err != nil {
			t.Fatalf("GetUserRole after update: %v", err)
		}
		if got2.RoleMask != domain.RoleAdmin {
			t.Errorf("RoleMask after update: got %d, want %d", got2.RoleMask, domain.RoleAdmin)
		}

		ur3 := &domain.UserRole{UserID: "ur-user", Workspace: "ws2", RoleMask: domain.RoleViewer}
		if err := s.UpsertUserRole(ctx, ur3); err != nil {
			t.Fatalf("UpsertUserRole ws2: %v", err)
		}

		roles, err := s.ListUserRoles(ctx, "ur-user")
		if err != nil {
			t.Fatalf("ListUserRoles: %v", err)
		}
		if len(roles) != 2 {
			t.Errorf("ListUserRoles count: got %d, want 2", len(roles))
		}

		if err := s.DeleteUserRole(ctx, "ur-user", "ws1"); err != nil {
			t.Fatalf("DeleteUserRole: %v", err)
		}
		if err := s.DeleteUserRole(ctx, "ur-user", "ws1"); !errors.Is(err, port.ErrNotFound) {
			t.Errorf("delete again: got %v, want ErrNotFound", err)
		}

		_, err = s.GetUserRole(ctx, "ur-user", "ws1")
		if !errors.Is(err, port.ErrNotFound) {
			t.Errorf("after delete: got %v, want ErrNotFound", err)
		}
	})

	t.Run("Ping", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		if err := s.Ping(context.Background()); err != nil {
			t.Fatalf("Ping: %v", err)
		}
	})
}
