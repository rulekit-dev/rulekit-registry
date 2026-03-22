package httpadapter_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	fsblobstore "github.com/rulekit-dev/rulekit-registry/internal/adapter/blob/fs"
	httpadapter "github.com/rulekit-dev/rulekit-registry/internal/adapter/http"
	"github.com/rulekit-dev/rulekit-registry/internal/adapter/http/handler"
	"github.com/rulekit-dev/rulekit-registry/internal/adapter/mailer"
	sqlitestore "github.com/rulekit-dev/rulekit-registry/internal/adapter/store/sqlite"
	"github.com/rulekit-dev/rulekit-registry/internal/config"
	"github.com/rulekit-dev/rulekit-registry/internal/domain"
	"github.com/rulekit-dev/rulekit-registry/internal/service"
	"github.com/rulekit-dev/rulekit-registry/internal/util"
)

const testJWTSecret = "test-secret-for-tests"
const testAdminPassword = "test-admin-pass"

// newTestServer returns a test HTTP server and a pre-signed admin token for auth.
func newTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()

	db, err := sqlitestore.New(dir)
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}

	blobs, err := fsblobstore.New(dir + "/blobs")
	if err != nil {
		t.Fatalf("fsblobstore.New: %v", err)
	}

	t.Cleanup(func() {
		db.Close()
		blobs.Close()
	})

	cfg := &config.Config{
		JWTSecret:     testJWTSecret,
		AdminPassword: testAdminPassword,
	}

	rulesetSvc := service.NewRulesetService(db, blobs)
	authSvc := service.NewAuthService(db, mailer.NewStdout(), []byte(testJWTSecret), testAdminPassword)
	adminSvc := service.NewAdminService(db)
	workspaceSvc := service.NewWorkspaceService(db)

	h := handler.NewRulesetHandler(rulesetSvc)
	authHandler := handler.NewAuthHandler(authSvc)
	adminHandler := handler.NewAdminHandler(adminSvc)
	workspaceHandler := handler.NewWorkspaceHandler(workspaceSvc)

	token, err := util.SignAdminToken([]byte(testJWTSecret))
	if err != nil {
		t.Fatalf("SignAdminToken: %v", err)
	}

	srv := httptest.NewServer(httpadapter.NewRouter(h, authHandler, adminHandler, workspaceHandler, db, cfg, time.Now()))
	return srv, token
}

func mustJSON(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return bytes.NewBuffer(b)
}

func decode(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func authGet(t *testing.T, token, url string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

func authPost(t *testing.T, token, url string, body *bytes.Buffer) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func authDo(t *testing.T, token string, req *http.Request) *http.Response {
	t.Helper()
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL, err)
	}
	return resp
}

var validDSL = map[string]any{
	"dsl_version": "v1",
	"strategy":    "first_match",
	"schema": map[string]any{
		"age": map[string]any{"type": "number"},
	},
	"rules": []any{
		map[string]any{
			"id":   "r1",
			"name": "adult",
			"when": []any{
				map[string]any{"field": "age", "op": "gte", "value": 18},
			},
			"then": map[string]any{"result": "adult"},
		},
	},
}

// --- Healthz ---

func TestHealthz(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}
}

// --- Rulesets ---

func TestCreateAndGetRuleset(t *testing.T) {
	srv, token := newTestServer(t)
	defer srv.Close()

	resp := authPost(t, token, srv.URL+"/v1/rulesets", mustJSON(t, map[string]string{"key": "my-rules", "name": "My Rules"}))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status: got %d, want 201", resp.StatusCode)
	}

	resp2 := authGet(t, token, srv.URL+"/v1/rulesets/my-rules")
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("get status: got %d, want 200", resp2.StatusCode)
	}

	var rs domain.Ruleset
	decode(t, resp2, &rs)
	if rs.Key != "my-rules" {
		t.Errorf("Key: got %q, want %q", rs.Key, "my-rules")
	}
}

func TestCreateRulesetDuplicate(t *testing.T) {
	srv, token := newTestServer(t)
	defer srv.Close()

	authPost(t, token, srv.URL+"/v1/rulesets", mustJSON(t, map[string]string{"key": "dup-rules", "name": "Dup"})).Body.Close() //nolint:errcheck

	resp := authPost(t, token, srv.URL+"/v1/rulesets", mustJSON(t, map[string]string{"key": "dup-rules", "name": "Dup"}))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status: got %d, want 409", resp.StatusCode)
	}
}

func TestCreateRulesetInvalidKey(t *testing.T) {
	srv, token := newTestServer(t)
	defer srv.Close()

	resp := authPost(t, token, srv.URL+"/v1/rulesets", mustJSON(t, map[string]string{"key": "INVALID KEY!", "name": "Bad"}))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", resp.StatusCode)
	}
}

func TestCreateRulesetInvalidWorkspace(t *testing.T) {
	srv, token := newTestServer(t)
	defer srv.Close()

	resp := authPost(t, token, srv.URL+"/v1/rulesets", mustJSON(t, map[string]string{"key": "rules", "name": "Bad", "workspace": "INVALID workspace!"}))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", resp.StatusCode)
	}
}

func TestGetRulesetNotFound(t *testing.T) {
	srv, token := newTestServer(t)
	defer srv.Close()

	resp := authGet(t, token, srv.URL+"/v1/rulesets/nonexistent")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", resp.StatusCode)
	}
}

func TestListRulesets(t *testing.T) {
	srv, token := newTestServer(t)
	defer srv.Close()

	for _, key := range []string{"rules-b", "rules-a", "rules-c"} {
		authPost(t, token, srv.URL+"/v1/rulesets", mustJSON(t, map[string]string{"key": key, "name": key})).Body.Close()
	}

	resp := authGet(t, token, srv.URL+"/v1/rulesets")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}

	var list []*domain.Ruleset
	decode(t, resp, &list)
	if len(list) != 3 {
		t.Fatalf("count: got %d, want 3", len(list))
	}
	if list[0].Key != "rules-a" || list[1].Key != "rules-b" || list[2].Key != "rules-c" {
		t.Errorf("order: got %v", []string{list[0].Key, list[1].Key, list[2].Key})
	}
}

func TestListRulesetsPagination(t *testing.T) {
	srv, token := newTestServer(t)
	defer srv.Close()

	for _, key := range []string{"rules-a", "rules-b", "rules-c"} {
		authPost(t, token, srv.URL+"/v1/rulesets", mustJSON(t, map[string]string{"key": key, "name": key})).Body.Close()
	}

	resp := authGet(t, token, srv.URL+"/v1/rulesets?limit=2&offset=1")
	defer resp.Body.Close()

	var list []*domain.Ruleset
	decode(t, resp, &list)
	if len(list) != 2 {
		t.Fatalf("count: got %d, want 2", len(list))
	}
	if list[0].Key != "rules-b" {
		t.Errorf("first item: got %q, want %q", list[0].Key, "rules-b")
	}
}

func TestDeleteRuleset(t *testing.T) {
	srv, token := newTestServer(t)
	defer srv.Close()

	authPost(t, token, srv.URL+"/v1/rulesets", mustJSON(t, map[string]string{"key": "to-delete", "name": "Delete Me"})).Body.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/v1/rulesets/to-delete", nil)
	resp2 := authDo(t, token, req)
	resp2.Body.Close()

	if resp2.StatusCode != http.StatusNoContent {
		t.Errorf("delete status: got %d, want 204", resp2.StatusCode)
	}

	resp3 := authGet(t, token, srv.URL+"/v1/rulesets/to-delete")
	defer resp3.Body.Close()

	if resp3.StatusCode != http.StatusNotFound {
		t.Errorf("get after delete: got %d, want 404", resp3.StatusCode)
	}
}

func TestDeleteRulesetNotFound(t *testing.T) {
	srv, token := newTestServer(t)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/v1/rulesets/nonexistent", nil)
	resp := authDo(t, token, req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", resp.StatusCode)
	}
}

// --- Draft ---

func createRuleset(t *testing.T, srv *httptest.Server, token, key string) {
	t.Helper()
	resp := authPost(t, token, srv.URL+"/v1/rulesets", mustJSON(t, map[string]string{"key": key, "name": key}))
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("createRuleset %s: status %d", key, resp.StatusCode)
	}
}

func upsertDraft(t *testing.T, srv *httptest.Server, token, key string, dslBody any) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut,
		srv.URL+"/v1/rulesets/"+key+"/draft",
		mustJSON(t, map[string]any{"dsl": dslBody}))
	req.Header.Set("Content-Type", "application/json")
	return authDo(t, token, req)
}

func TestUpsertAndGetDraft(t *testing.T) {
	srv, token := newTestServer(t)
	defer srv.Close()
	createRuleset(t, srv, token, "draft-rules")

	resp := upsertDraft(t, srv, token, "draft-rules", validDSL)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upsert status: got %d, want 200", resp.StatusCode)
	}

	resp2 := authGet(t, token, srv.URL+"/v1/rulesets/draft-rules/draft")
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Errorf("get draft status: got %d, want 200", resp2.StatusCode)
	}
}

func TestUpsertDraftInvalidDSL(t *testing.T) {
	srv, token := newTestServer(t)
	defer srv.Close()
	createRuleset(t, srv, token, "bad-draft-rules")

	resp := upsertDraft(t, srv, token, "bad-draft-rules", map[string]any{"dsl_version": "v99"})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", resp.StatusCode)
	}
}

func TestGetDraftNotFound(t *testing.T) {
	srv, token := newTestServer(t)
	defer srv.Close()
	createRuleset(t, srv, token, "nodraft-rules")

	resp := authGet(t, token, srv.URL+"/v1/rulesets/nodraft-rules/draft")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", resp.StatusCode)
	}
}

func TestDeleteDraft(t *testing.T) {
	srv, token := newTestServer(t)
	defer srv.Close()
	createRuleset(t, srv, token, "del-draft-rules")

	upsertDraft(t, srv, token, "del-draft-rules", validDSL).Body.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/v1/rulesets/del-draft-rules/draft", nil)
	resp2 := authDo(t, token, req)
	resp2.Body.Close()

	if resp2.StatusCode != http.StatusNoContent {
		t.Errorf("delete status: got %d, want 204", resp2.StatusCode)
	}

	resp3 := authGet(t, token, srv.URL+"/v1/rulesets/del-draft-rules/draft")
	defer resp3.Body.Close()

	if resp3.StatusCode != http.StatusNotFound {
		t.Errorf("get after delete: got %d, want 404", resp3.StatusCode)
	}
}

// --- Publish ---

func publish(t *testing.T, srv *httptest.Server, token, key string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/rulesets/"+key+"/publish", nil)
	return authDo(t, token, req)
}

func TestPublishFlow(t *testing.T) {
	srv, token := newTestServer(t)
	defer srv.Close()
	createRuleset(t, srv, token, "pub-rules")

	// Publish without draft → 404
	resp := publish(t, srv, token, "pub-rules")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("publish without draft: got %d, want 404", resp.StatusCode)
	}

	// Upsert draft
	upsertDraft(t, srv, token, "pub-rules", validDSL).Body.Close()

	// Publish → 201
	resp2 := publish(t, srv, token, "pub-rules")
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusCreated {
		t.Fatalf("publish: got %d, want 201", resp2.StatusCode)
	}

	var v domain.Version
	decode(t, resp2, &v)
	if v.Version != 1 {
		t.Errorf("version: got %d, want 1", v.Version)
	}
	if v.DSL == nil {
		t.Error("DSL in response is nil")
	}
}

func TestPublishNoChanges(t *testing.T) {
	srv, token := newTestServer(t)
	defer srv.Close()
	createRuleset(t, srv, token, "noop-rules")

	upsertDraft(t, srv, token, "noop-rules", validDSL).Body.Close()
	publish(t, srv, token, "noop-rules").Body.Close()

	// Re-upsert same DSL and publish again → 409 NO_CHANGES
	upsertDraft(t, srv, token, "noop-rules", validDSL).Body.Close()

	resp := publish(t, srv, token, "noop-rules")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("no-op publish: got %d, want 409", resp.StatusCode)
	}
}

func TestPublishIncrementsVersion(t *testing.T) {
	srv, token := newTestServer(t)
	defer srv.Close()
	createRuleset(t, srv, token, "versioned-rules")

	upsertDraft(t, srv, token, "versioned-rules", validDSL).Body.Close()
	publish(t, srv, token, "versioned-rules").Body.Close()

	dsl2 := map[string]any{
		"dsl_version": "v1",
		"strategy":    "all_matches",
		"schema": map[string]any{
			"score": map[string]any{"type": "number"},
		},
		"rules": []any{
			map[string]any{
				"id":   "r2",
				"name": "high-score",
				"when": []any{
					map[string]any{"field": "score", "op": "gt", "value": 90},
				},
				"then": map[string]any{"tier": "gold"},
			},
		},
	}
	upsertDraft(t, srv, token, "versioned-rules", dsl2).Body.Close()
	resp := publish(t, srv, token, "versioned-rules")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("second publish: got %d, want 201", resp.StatusCode)
	}
	var v domain.Version
	decode(t, resp, &v)
	if v.Version != 2 {
		t.Errorf("version: got %d, want 2", v.Version)
	}
}

// --- Versions ---

func TestGetVersion(t *testing.T) {
	srv, token := newTestServer(t)
	defer srv.Close()
	createRuleset(t, srv, token, "ver-rules")
	upsertDraft(t, srv, token, "ver-rules", validDSL).Body.Close()
	publish(t, srv, token, "ver-rules").Body.Close()

	resp := authGet(t, token, srv.URL+"/v1/rulesets/ver-rules/versions/1")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	var v domain.Version
	decode(t, resp, &v)
	if v.Version != 1 {
		t.Errorf("version: got %d, want 1", v.Version)
	}
	if v.DSL == nil {
		t.Error("DSL is nil")
	}
}

func TestGetLatestVersion(t *testing.T) {
	srv, token := newTestServer(t)
	defer srv.Close()
	createRuleset(t, srv, token, "latest-rules")
	upsertDraft(t, srv, token, "latest-rules", validDSL).Body.Close()
	publish(t, srv, token, "latest-rules").Body.Close()

	resp := authGet(t, token, srv.URL+"/v1/rulesets/latest-rules/versions/latest")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	var v domain.Version
	decode(t, resp, &v)
	if v.Version != 1 {
		t.Errorf("version: got %d, want 1", v.Version)
	}
}

func TestListVersions(t *testing.T) {
	srv, token := newTestServer(t)
	defer srv.Close()
	createRuleset(t, srv, token, "list-ver-rules")

	dsl2 := map[string]any{
		"dsl_version": "v1",
		"strategy":    "all_matches",
		"schema":      map[string]any{"x": map[string]any{"type": "number"}},
		"rules": []any{
			map[string]any{
				"id": "r1", "name": "x", "when": []any{
					map[string]any{"field": "x", "op": "eq", "value": 1},
				}, "then": map[string]any{"ok": true},
			},
		},
	}

	upsertDraft(t, srv, token, "list-ver-rules", validDSL).Body.Close()
	publish(t, srv, token, "list-ver-rules").Body.Close()
	upsertDraft(t, srv, token, "list-ver-rules", dsl2).Body.Close()
	publish(t, srv, token, "list-ver-rules").Body.Close()

	resp := authGet(t, token, srv.URL+"/v1/rulesets/list-ver-rules/versions")
	defer resp.Body.Close()

	var versions []*domain.Version
	decode(t, resp, &versions)
	if len(versions) != 2 {
		t.Fatalf("count: got %d, want 2", len(versions))
	}
	if versions[0].Version != 1 || versions[1].Version != 2 {
		t.Errorf("order: got %d, %d", versions[0].Version, versions[1].Version)
	}
}

// --- Bundle ---

func TestGetVersionBundle(t *testing.T) {
	srv, token := newTestServer(t)
	defer srv.Close()
	createRuleset(t, srv, token, "bundle-rules")
	upsertDraft(t, srv, token, "bundle-rules", validDSL).Body.Close()
	publish(t, srv, token, "bundle-rules").Body.Close()

	resp := authGet(t, token, srv.URL+"/v1/rulesets/bundle-rules/versions/1/bundle")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type: got %q, want application/zip", ct)
	}
}

func TestGetLatestBundle(t *testing.T) {
	srv, token := newTestServer(t)
	defer srv.Close()
	createRuleset(t, srv, token, "latest-bundle-rules")
	upsertDraft(t, srv, token, "latest-bundle-rules", validDSL).Body.Close()
	publish(t, srv, token, "latest-bundle-rules").Body.Close()

	resp := authGet(t, token, srv.URL+"/v1/rulesets/latest-bundle-rules/versions/latest/bundle")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type: got %q, want application/zip", ct)
	}
}

// --- Auth ---

func TestAuthRequired(t *testing.T) {
	srv, token := newTestServer(t)
	defer srv.Close()

	// No token → 401
	resp, err := http.Get(srv.URL + "/v1/rulesets")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: got %d, want 401", resp.StatusCode)
	}

	// Invalid token → 401
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/rulesets", nil)
	req.Header.Set("Authorization", "Bearer not-a-valid-jwt")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET with bad token: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("bad token: got %d, want 401", resp2.StatusCode)
	}

	// Valid admin JWT → 200
	resp3 := authGet(t, token, srv.URL+"/v1/rulesets")
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Errorf("valid token: got %d, want 200", resp3.StatusCode)
	}
}

func TestHealthzSkipsAuth(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz without token: got %d, want 200", resp.StatusCode)
	}
}

// --- Workspace isolation ---

func TestWorkspaceIsolation(t *testing.T) {
	srv, token := newTestServer(t)
	defer srv.Close()

	for _, workspace := range []string{"team-a", "team-b"} {
		resp := authPost(t, token, srv.URL+"/v1/rulesets",
			mustJSON(t, map[string]string{"key": "shared", "name": "Shared", "workspace": workspace}))
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("POST ws=%s: status %d", workspace, resp.StatusCode)
		}
	}

	for _, workspace := range []string{"team-a", "team-b"} {
		resp := authGet(t, token, srv.URL+"/v1/rulesets?workspace="+workspace)
		defer resp.Body.Close()
		var list []*domain.Ruleset
		decode(t, resp, &list)
		if len(list) != 1 {
			t.Errorf("ws=%s: got %d rulesets, want 1", workspace, len(list))
		}
	}
}

func TestInvalidWorkspaceQueryParam(t *testing.T) {
	srv, token := newTestServer(t)
	defer srv.Close()

	resp := authGet(t, token, srv.URL+"/v1/rulesets?workspace=INVALID!")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", resp.StatusCode)
	}
}
