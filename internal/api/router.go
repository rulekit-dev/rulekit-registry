package api

import (
	"encoding/json"
	"net/http"
)

// NewRouter builds the HTTP router. If apiKey is non-empty, all /v1/* routes
// require an Authorization: Bearer <apiKey> header.
func NewRouter(h *Handler, apiKey string) http.Handler {
	mux := http.NewServeMux()

	v1 := http.NewServeMux()
	v1.HandleFunc("GET /v1/rulesets", h.ListRulesets)
	v1.HandleFunc("POST /v1/rulesets", h.CreateRuleset)
	v1.HandleFunc("GET /v1/rulesets/{key}", h.GetRuleset)
	v1.HandleFunc("DELETE /v1/rulesets/{key}", h.DeleteRuleset)
	v1.HandleFunc("GET /v1/rulesets/{key}/draft", h.GetDraft)
	v1.HandleFunc("PUT /v1/rulesets/{key}/draft", h.UpsertDraft)
	v1.HandleFunc("DELETE /v1/rulesets/{key}/draft", h.DeleteDraft)
	v1.HandleFunc("POST /v1/rulesets/{key}/publish", h.Publish)
	v1.HandleFunc("GET /v1/rulesets/{key}/versions", h.ListVersions)

	// "latest" routes must be registered before {version} so the more-specific pattern wins.
	v1.HandleFunc("GET /v1/rulesets/{key}/versions/latest", h.GetLatestVersion)
	v1.HandleFunc("GET /v1/rulesets/{key}/versions/latest/bundle", h.GetLatestBundle)

	v1.HandleFunc("GET /v1/rulesets/{key}/versions/{version}", h.GetVersion)
	v1.HandleFunc("GET /v1/rulesets/{key}/versions/{version}/bundle", h.GetVersionBundle)

	var v1Handler http.Handler = v1
	if apiKey != "" {
		v1Handler = authMiddleware(apiKey, v1)
	}
	mux.Handle("/v1/", v1Handler)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	return loggingMiddleware(recoveryMiddleware(mux))
}
