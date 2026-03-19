package api

import (
	"encoding/json"
	"net/http"

	"github.com/rulekit-dev/rulekit-registry/internal/api/handler"
	"github.com/rulekit-dev/rulekit-registry/internal/config"
	"github.com/rulekit-dev/rulekit-registry/internal/model"
	"github.com/rulekit-dev/rulekit-registry/internal/store"
)

func NewRouter(h *handler.RulesetHandler, auth *handler.AuthHandler, admin *handler.AdminHandler, st store.Store, cfg *config.Config) http.Handler {
	mux := http.NewServeMux()

	if cfg.AuthMode == config.AuthModeJWT {
		registerJWTRoutes(mux, h, auth, admin, st, cfg)
	} else {
		registerLegacyRoutes(mux, h, cfg.APIKey)
	}

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
	})

	var root http.Handler = mux
	if cfg.CORSOrigins != "" {
		root = corsMiddleware(cfg.CORSOrigins, root)
	}
	return loggingMiddleware(recoveryMiddleware(root))
}

func registerLegacyRoutes(mux *http.ServeMux, h *handler.RulesetHandler, apiKey string) {
	v1 := http.NewServeMux()
	registerRulesetRoutes(v1, h)

	var v1Handler http.Handler = v1
	if apiKey != "" {
		v1Handler = authMiddleware(apiKey, v1)
	}
	mux.Handle("/v1/", v1Handler)
}

func registerJWTRoutes(mux *http.ServeMux, h *handler.RulesetHandler, auth *handler.AuthHandler, admin *handler.AdminHandler, st store.Store, cfg *config.Config) {
	secret := []byte(cfg.JWTSecret)

	// Public auth endpoints — no token required.
	mux.HandleFunc("POST /v1/auth/login", auth.Login)
	mux.HandleFunc("POST /v1/auth/verify", auth.Verify)
	mux.HandleFunc("POST /v1/auth/refresh", auth.Refresh)

	// Logout requires a valid token to identify the session.
	mux.Handle("POST /v1/auth/logout", apiTokenMiddleware(st, secret, http.HandlerFunc(auth.Logout)))

	// Ruleset API: viewer+ for reads, editor+ for writes.
	v1 := http.NewServeMux()
	v1.Handle("GET /v1/rulesets", requireRole(model.RoleViewer, http.HandlerFunc(h.ListRulesets)))
	v1.Handle("POST /v1/rulesets", requireRole(model.RoleEditor, http.HandlerFunc(h.CreateRuleset)))
	v1.Handle("GET /v1/rulesets/{key}", requireRole(model.RoleViewer, http.HandlerFunc(h.GetRuleset)))
	v1.Handle("DELETE /v1/rulesets/{key}", requireRole(model.RoleEditor, http.HandlerFunc(h.DeleteRuleset)))
	v1.Handle("GET /v1/rulesets/{key}/draft", requireRole(model.RoleEditor, http.HandlerFunc(h.GetDraft)))
	v1.Handle("PUT /v1/rulesets/{key}/draft", requireRole(model.RoleEditor, http.HandlerFunc(h.UpsertDraft)))
	v1.Handle("DELETE /v1/rulesets/{key}/draft", requireRole(model.RoleEditor, http.HandlerFunc(h.DeleteDraft)))
	v1.Handle("POST /v1/rulesets/{key}/publish", requireRole(model.RoleEditor, http.HandlerFunc(h.Publish)))
	v1.Handle("GET /v1/rulesets/{key}/versions", requireRole(model.RoleViewer, http.HandlerFunc(h.ListVersions)))
	v1.Handle("GET /v1/rulesets/{key}/versions/latest", requireRole(model.RoleViewer, http.HandlerFunc(h.GetLatestVersion)))
	v1.Handle("GET /v1/rulesets/{key}/versions/latest/bundle", requireRole(model.RoleViewer, http.HandlerFunc(h.GetLatestBundle)))
	v1.Handle("GET /v1/rulesets/{key}/versions/{version}", requireRole(model.RoleViewer, http.HandlerFunc(h.GetVersion)))
	v1.Handle("GET /v1/rulesets/{key}/versions/{version}/bundle", requireRole(model.RoleViewer, http.HandlerFunc(h.GetVersionBundle)))

	// Admin API: admin role required.
	v1.Handle("GET /v1/admin/users", requireAdmin(http.HandlerFunc(admin.ListUsers)))
	v1.Handle("DELETE /v1/admin/users/{userID}", requireAdmin(http.HandlerFunc(admin.DeleteUser)))
	v1.Handle("GET /v1/admin/users/{userID}/roles", requireAdmin(http.HandlerFunc(admin.ListUserRoles)))
	v1.Handle("PUT /v1/admin/users/{userID}/roles/{namespace}", requireAdmin(http.HandlerFunc(admin.UpsertUserRole)))
	v1.Handle("DELETE /v1/admin/users/{userID}/roles/{namespace}", requireAdmin(http.HandlerFunc(admin.DeleteUserRole)))
	v1.Handle("POST /v1/admin/tokens", requireAdmin(http.HandlerFunc(admin.CreateAPIToken)))
	v1.Handle("GET /v1/admin/tokens", requireAdmin(http.HandlerFunc(admin.ListAPITokens)))
	v1.Handle("DELETE /v1/admin/tokens/{tokenID}", requireAdmin(http.HandlerFunc(admin.RevokeAPIToken)))

	mux.Handle("/v1/", apiTokenMiddleware(st, secret, v1))
}

func registerRulesetRoutes(mux *http.ServeMux, h *handler.RulesetHandler) {
	mux.HandleFunc("GET /v1/rulesets", h.ListRulesets)
	mux.HandleFunc("POST /v1/rulesets", h.CreateRuleset)
	mux.HandleFunc("GET /v1/rulesets/{key}", h.GetRuleset)
	mux.HandleFunc("DELETE /v1/rulesets/{key}", h.DeleteRuleset)
	mux.HandleFunc("GET /v1/rulesets/{key}/draft", h.GetDraft)
	mux.HandleFunc("PUT /v1/rulesets/{key}/draft", h.UpsertDraft)
	mux.HandleFunc("DELETE /v1/rulesets/{key}/draft", h.DeleteDraft)
	mux.HandleFunc("POST /v1/rulesets/{key}/publish", h.Publish)
	mux.HandleFunc("GET /v1/rulesets/{key}/versions", h.ListVersions)
	// "latest" routes must be registered before {version} so the more-specific pattern wins.
	mux.HandleFunc("GET /v1/rulesets/{key}/versions/latest", h.GetLatestVersion)
	mux.HandleFunc("GET /v1/rulesets/{key}/versions/latest/bundle", h.GetLatestBundle)
	mux.HandleFunc("GET /v1/rulesets/{key}/versions/{version}", h.GetVersion)
	mux.HandleFunc("GET /v1/rulesets/{key}/versions/{version}/bundle", h.GetVersionBundle)
}
