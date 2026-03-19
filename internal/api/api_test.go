package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rulekit/rulekit-registry/internal/api"
	fsblobstore "github.com/rulekit/rulekit-registry/internal/blobstore/fs"
	"github.com/rulekit/rulekit-registry/internal/model"
	"github.com/rulekit/rulekit-registry/internal/store/sqlite"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	dir := t.TempDir()

	st, err := sqlite.New(dir)
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}

	blobs, err := fsblobstore.New(dir + "/blobs")
	if err != nil {
		t.Fatalf("fsblobstore.New: %v", err)
	}

	t.Cleanup(func() {
		st.Close()
		blobs.Close()
	})

	h := api.NewHandler(st, blobs)
	return httptest.NewServer(api.NewRouter(h))
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
	srv := newTestServer(t)
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
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/rulesets", "application/json",
		mustJSON(t, map[string]string{"key": "my-rules", "name": "My Rules"}))
	if err != nil {
		t.Fatalf("POST /v1/rulesets: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status: got %d, want 201", resp.StatusCode)
	}

	resp2, err := http.Get(srv.URL + "/v1/rulesets/my-rules")
	if err != nil {
		t.Fatalf("GET /v1/rulesets/my-rules: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("get status: got %d, want 200", resp2.StatusCode)
	}

	var rs model.Ruleset
	decode(t, resp2, &rs)
	if rs.Key != "my-rules" {
		t.Errorf("Key: got %q, want %q", rs.Key, "my-rules")
	}
}

func TestCreateRulesetDuplicate(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	body := mustJSON(t, map[string]string{"key": "dup-rules", "name": "Dup"})
	http.Post(srv.URL+"/v1/rulesets", "application/json", body) //nolint:errcheck

	resp, err := http.Post(srv.URL+"/v1/rulesets", "application/json",
		mustJSON(t, map[string]string{"key": "dup-rules", "name": "Dup"}))
	if err != nil {
		t.Fatalf("POST /v1/rulesets: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status: got %d, want 409", resp.StatusCode)
	}
}

func TestCreateRulesetInvalidKey(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/rulesets", "application/json",
		mustJSON(t, map[string]string{"key": "INVALID KEY!", "name": "Bad"}))
	if err != nil {
		t.Fatalf("POST /v1/rulesets: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", resp.StatusCode)
	}
}

func TestCreateRulesetInvalidNamespace(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/rulesets", "application/json",
		mustJSON(t, map[string]string{"key": "rules", "name": "Bad", "namespace": "INVALID NS!"}))
	if err != nil {
		t.Fatalf("POST /v1/rulesets: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", resp.StatusCode)
	}
}

func TestGetRulesetNotFound(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/rulesets/nonexistent")
	if err != nil {
		t.Fatalf("GET /v1/rulesets/nonexistent: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", resp.StatusCode)
	}
}

func TestListRulesets(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	for _, key := range []string{"rules-b", "rules-a", "rules-c"} {
		resp, err := http.Post(srv.URL+"/v1/rulesets", "application/json",
			mustJSON(t, map[string]string{"key": key, "name": key}))
		if err != nil {
			t.Fatalf("POST /v1/rulesets %s: %v", key, err)
		}
		resp.Body.Close()
	}

	resp, err := http.Get(srv.URL + "/v1/rulesets")
	if err != nil {
		t.Fatalf("GET /v1/rulesets: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}

	var list []*model.Ruleset
	decode(t, resp, &list)
	if len(list) != 3 {
		t.Fatalf("count: got %d, want 3", len(list))
	}
	// alphabetical order
	if list[0].Key != "rules-a" || list[1].Key != "rules-b" || list[2].Key != "rules-c" {
		t.Errorf("order: got %v", []string{list[0].Key, list[1].Key, list[2].Key})
	}
}

func TestListRulesetsPagination(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	for _, key := range []string{"rules-a", "rules-b", "rules-c"} {
		resp, err := http.Post(srv.URL+"/v1/rulesets", "application/json",
			mustJSON(t, map[string]string{"key": key, "name": key}))
		if err != nil {
			t.Fatalf("POST /v1/rulesets %s: %v", key, err)
		}
		resp.Body.Close()
	}

	resp, err := http.Get(srv.URL + "/v1/rulesets?limit=2&offset=1")
	if err != nil {
		t.Fatalf("GET /v1/rulesets?limit=2&offset=1: %v", err)
	}
	defer resp.Body.Close()

	var list []*model.Ruleset
	decode(t, resp, &list)
	if len(list) != 2 {
		t.Fatalf("count: got %d, want 2", len(list))
	}
	if list[0].Key != "rules-b" {
		t.Errorf("first item: got %q, want %q", list[0].Key, "rules-b")
	}
}

func TestDeleteRuleset(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/rulesets", "application/json",
		mustJSON(t, map[string]string{"key": "to-delete", "name": "Delete Me"}))
	if err != nil {
		t.Fatalf("POST /v1/rulesets: %v", err)
	}
	resp.Body.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/v1/rulesets/to-delete", nil)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /v1/rulesets/to-delete: %v", err)
	}
	resp2.Body.Close()

	if resp2.StatusCode != http.StatusNoContent {
		t.Errorf("delete status: got %d, want 204", resp2.StatusCode)
	}

	resp3, err := http.Get(srv.URL + "/v1/rulesets/to-delete")
	if err != nil {
		t.Fatalf("GET after delete: %v", err)
	}
	resp3.Body.Close()

	if resp3.StatusCode != http.StatusNotFound {
		t.Errorf("get after delete: got %d, want 404", resp3.StatusCode)
	}
}

func TestDeleteRulesetNotFound(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/v1/rulesets/nonexistent", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /v1/rulesets/nonexistent: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", resp.StatusCode)
	}
}

// --- Draft ---

func createRuleset(t *testing.T, srv *httptest.Server, key string) {
	t.Helper()
	resp, err := http.Post(srv.URL+"/v1/rulesets", "application/json",
		mustJSON(t, map[string]string{"key": key, "name": key}))
	if err != nil {
		t.Fatalf("createRuleset %s: %v", key, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("createRuleset %s: status %d", key, resp.StatusCode)
	}
}

func upsertDraft(t *testing.T, srv *httptest.Server, key string, dslBody any) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut,
		srv.URL+"/v1/rulesets/"+key+"/draft",
		mustJSON(t, map[string]any{"dsl": dslBody}))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upsertDraft %s: %v", key, err)
	}
	return resp
}

func TestUpsertAndGetDraft(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	createRuleset(t, srv, "draft-rules")

	resp := upsertDraft(t, srv, "draft-rules", validDSL)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upsert status: got %d, want 200", resp.StatusCode)
	}

	resp2, err := http.Get(srv.URL + "/v1/rulesets/draft-rules/draft")
	if err != nil {
		t.Fatalf("GET draft: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Errorf("get draft status: got %d, want 200", resp2.StatusCode)
	}
}

func TestUpsertDraftInvalidDSL(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	createRuleset(t, srv, "bad-draft-rules")

	resp := upsertDraft(t, srv, "bad-draft-rules", map[string]any{"dsl_version": "v99"})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", resp.StatusCode)
	}
}

func TestGetDraftNotFound(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	createRuleset(t, srv, "nodraft-rules")

	resp, err := http.Get(srv.URL + "/v1/rulesets/nodraft-rules/draft")
	if err != nil {
		t.Fatalf("GET draft: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", resp.StatusCode)
	}
}

func TestDeleteDraft(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	createRuleset(t, srv, "del-draft-rules")

	resp := upsertDraft(t, srv, "del-draft-rules", validDSL)
	resp.Body.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/v1/rulesets/del-draft-rules/draft", nil)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE draft: %v", err)
	}
	resp2.Body.Close()

	if resp2.StatusCode != http.StatusNoContent {
		t.Errorf("delete status: got %d, want 204", resp2.StatusCode)
	}

	resp3, err := http.Get(srv.URL + "/v1/rulesets/del-draft-rules/draft")
	if err != nil {
		t.Fatalf("GET after delete: %v", err)
	}
	resp3.Body.Close()

	if resp3.StatusCode != http.StatusNotFound {
		t.Errorf("get after delete: got %d, want 404", resp3.StatusCode)
	}
}

// --- Publish ---

func publish(t *testing.T, srv *httptest.Server, key string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/rulesets/"+key+"/publish", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("publish %s: %v", key, err)
	}
	return resp
}

func TestPublishFlow(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	createRuleset(t, srv, "pub-rules")

	// Publish without draft → 404
	resp := publish(t, srv, "pub-rules")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("publish without draft: got %d, want 404", resp.StatusCode)
	}

	// Upsert draft
	r := upsertDraft(t, srv, "pub-rules", validDSL)
	r.Body.Close()

	// Publish → 201
	resp2 := publish(t, srv, "pub-rules")
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusCreated {
		t.Fatalf("publish: got %d, want 201", resp2.StatusCode)
	}

	var v model.Version
	decode(t, resp2, &v)
	if v.Version != 1 {
		t.Errorf("version: got %d, want 1", v.Version)
	}
	if v.DSL == nil {
		t.Error("DSL in response is nil")
	}
}

func TestPublishNoChanges(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	createRuleset(t, srv, "noop-rules")

	r := upsertDraft(t, srv, "noop-rules", validDSL)
	r.Body.Close()

	r2 := publish(t, srv, "noop-rules")
	r2.Body.Close()
	if r2.StatusCode != http.StatusCreated {
		t.Fatalf("first publish: got %d, want 201", r2.StatusCode)
	}

	// Re-upsert same DSL and publish again → 409 NO_CHANGES
	r3 := upsertDraft(t, srv, "noop-rules", validDSL)
	r3.Body.Close()

	resp := publish(t, srv, "noop-rules")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("no-op publish: got %d, want 409", resp.StatusCode)
	}
}

func TestPublishIncrementsVersion(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	createRuleset(t, srv, "versioned-rules")

	dsl1 := validDSL
	r := upsertDraft(t, srv, "versioned-rules", dsl1)
	r.Body.Close()
	r2 := publish(t, srv, "versioned-rules")
	r2.Body.Close()

	// Modified DSL for second publish
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
	r3 := upsertDraft(t, srv, "versioned-rules", dsl2)
	r3.Body.Close()
	resp := publish(t, srv, "versioned-rules")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("second publish: got %d, want 201", resp.StatusCode)
	}
	var v model.Version
	decode(t, resp, &v)
	if v.Version != 2 {
		t.Errorf("version: got %d, want 2", v.Version)
	}
}

// --- Versions ---

func TestGetVersion(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	createRuleset(t, srv, "ver-rules")
	upsertDraft(t, srv, "ver-rules", validDSL).Body.Close()
	publish(t, srv, "ver-rules").Body.Close()

	resp, err := http.Get(srv.URL + "/v1/rulesets/ver-rules/versions/1")
	if err != nil {
		t.Fatalf("GET versions/1: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	var v model.Version
	decode(t, resp, &v)
	if v.Version != 1 {
		t.Errorf("version: got %d, want 1", v.Version)
	}
	if v.DSL == nil {
		t.Error("DSL is nil")
	}
}

func TestGetLatestVersion(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	createRuleset(t, srv, "latest-rules")
	upsertDraft(t, srv, "latest-rules", validDSL).Body.Close()
	publish(t, srv, "latest-rules").Body.Close()

	resp, err := http.Get(srv.URL + "/v1/rulesets/latest-rules/versions/latest")
	if err != nil {
		t.Fatalf("GET versions/latest: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	var v model.Version
	decode(t, resp, &v)
	if v.Version != 1 {
		t.Errorf("version: got %d, want 1", v.Version)
	}
}

func TestListVersions(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	createRuleset(t, srv, "list-ver-rules")

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

	upsertDraft(t, srv, "list-ver-rules", validDSL).Body.Close()
	publish(t, srv, "list-ver-rules").Body.Close()
	upsertDraft(t, srv, "list-ver-rules", dsl2).Body.Close()
	publish(t, srv, "list-ver-rules").Body.Close()

	resp, err := http.Get(srv.URL + "/v1/rulesets/list-ver-rules/versions")
	if err != nil {
		t.Fatalf("GET versions: %v", err)
	}
	defer resp.Body.Close()

	var versions []*model.Version
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
	srv := newTestServer(t)
	defer srv.Close()
	createRuleset(t, srv, "bundle-rules")
	upsertDraft(t, srv, "bundle-rules", validDSL).Body.Close()
	publish(t, srv, "bundle-rules").Body.Close()

	resp, err := http.Get(srv.URL + "/v1/rulesets/bundle-rules/versions/1/bundle")
	if err != nil {
		t.Fatalf("GET bundle: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type: got %q, want application/zip", ct)
	}
}

func TestGetLatestBundle(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	createRuleset(t, srv, "latest-bundle-rules")
	upsertDraft(t, srv, "latest-bundle-rules", validDSL).Body.Close()
	publish(t, srv, "latest-bundle-rules").Body.Close()

	resp, err := http.Get(srv.URL + "/v1/rulesets/latest-bundle-rules/versions/latest/bundle")
	if err != nil {
		t.Fatalf("GET latest bundle: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type: got %q, want application/zip", ct)
	}
}

// --- Namespace isolation ---

func TestNamespaceIsolation(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	// Create same key in two namespaces
	for _, ns := range []string{"team-a", "team-b"} {
		resp, err := http.Post(srv.URL+"/v1/rulesets", "application/json",
			mustJSON(t, map[string]string{"key": "shared", "name": "Shared", "namespace": ns}))
		if err != nil {
			t.Fatalf("POST ns=%s: %v", ns, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("POST ns=%s: status %d", ns, resp.StatusCode)
		}
	}

	// Each namespace sees only its own ruleset
	for _, ns := range []string{"team-a", "team-b"} {
		resp, err := http.Get(srv.URL + "/v1/rulesets?namespace=" + ns)
		if err != nil {
			t.Fatalf("GET ns=%s: %v", ns, err)
		}
		defer resp.Body.Close()
		var list []*model.Ruleset
		decode(t, resp, &list)
		if len(list) != 1 {
			t.Errorf("ns=%s: got %d rulesets, want 1", ns, len(list))
		}
	}
}

func TestInvalidNamespaceQueryParam(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/rulesets?namespace=INVALID!")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", resp.StatusCode)
	}
}
