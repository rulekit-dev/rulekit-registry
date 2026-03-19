package api

import (
	"encoding/json"
	"net/http"
)

func NewRouter(h *Handler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /v1/rulesets", h.ListRulesets)
	mux.HandleFunc("POST /v1/rulesets", h.CreateRuleset)
	mux.HandleFunc("GET /v1/rulesets/{key}", h.GetRuleset)
	mux.HandleFunc("GET /v1/rulesets/{key}/draft", h.GetDraft)
	mux.HandleFunc("PUT /v1/rulesets/{key}/draft", h.UpsertDraft)
	mux.HandleFunc("POST /v1/rulesets/{key}/publish", h.Publish)
	mux.HandleFunc("GET /v1/rulesets/{key}/versions", h.ListVersions)

	// "latest" routes must be registered before {version} so the more-specific pattern wins.
	mux.HandleFunc("GET /v1/rulesets/{key}/versions/latest", h.GetLatestVersion)
	mux.HandleFunc("GET /v1/rulesets/{key}/versions/latest/bundle", h.GetLatestBundle)

	mux.HandleFunc("GET /v1/rulesets/{key}/versions/{version}", h.GetVersion)
	mux.HandleFunc("GET /v1/rulesets/{key}/versions/{version}/bundle", h.GetVersionBundle)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	return loggingMiddleware(recoveryMiddleware(mux))
}
