package httpadapter

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"time"

	"github.com/rulekit-dev/rulekit-registry/internal/adapter/http/handler"
	"github.com/rulekit-dev/rulekit-registry/internal/config"
	"github.com/rulekit-dev/rulekit-registry/internal/domain"
	"github.com/rulekit-dev/rulekit-registry/internal/version"
)

// storePort is the narrow interface the HTTP adapter requires from the store.
type storePort interface {
	Ping(ctx context.Context) error
	GetAPIKeyByHash(ctx context.Context, hash string) (*domain.APIKey, error)
}

func NewRouter(h *handler.RulesetHandler, auth *handler.AuthHandler, admin *handler.AdminHandler, db storePort, cfg *config.Config, startTime time.Time) http.Handler {
	mux := http.NewServeMux()
	secret := []byte(cfg.JWTSecret)

	// Public auth endpoints — no token required.
	mux.HandleFunc("POST /v1/auth/login", auth.Login)
	mux.HandleFunc("POST /v1/auth/verify", auth.Verify)
	mux.HandleFunc("POST /v1/auth/refresh", auth.Refresh)
	mux.Handle("POST /v1/auth/logout", apiTokenMiddleware(db, secret, http.HandlerFunc(auth.Logout)))

	// Ruleset API: viewer+ for reads, editor+ for writes.
	v1 := http.NewServeMux()
	v1.Handle("GET /v1/rulesets", requireRole(domain.RoleViewer, http.HandlerFunc(h.ListRulesets)))
	v1.Handle("POST /v1/rulesets", requireRole(domain.RoleEditor, http.HandlerFunc(h.CreateRuleset)))
	v1.Handle("GET /v1/rulesets/{key}", requireRole(domain.RoleViewer, http.HandlerFunc(h.GetRuleset)))
	v1.Handle("DELETE /v1/rulesets/{key}", requireRole(domain.RoleEditor, http.HandlerFunc(h.DeleteRuleset)))
	v1.Handle("GET /v1/rulesets/{key}/draft", requireRole(domain.RoleEditor, http.HandlerFunc(h.GetDraft)))
	v1.Handle("PUT /v1/rulesets/{key}/draft", requireRole(domain.RoleEditor, http.HandlerFunc(h.UpsertDraft)))
	v1.Handle("DELETE /v1/rulesets/{key}/draft", requireRole(domain.RoleEditor, http.HandlerFunc(h.DeleteDraft)))
	v1.Handle("POST /v1/rulesets/{key}/publish", requireRole(domain.RoleEditor, http.HandlerFunc(h.Publish)))
	v1.Handle("GET /v1/rulesets/{key}/versions", requireRole(domain.RoleViewer, http.HandlerFunc(h.ListVersions)))
	v1.Handle("GET /v1/rulesets/{key}/versions/latest", requireRole(domain.RoleViewer, http.HandlerFunc(h.GetLatestVersion)))
	v1.Handle("GET /v1/rulesets/{key}/versions/latest/bundle", requireRole(domain.RoleViewer, http.HandlerFunc(h.GetLatestBundle)))
	v1.Handle("GET /v1/rulesets/{key}/versions/{version}", requireRole(domain.RoleViewer, http.HandlerFunc(h.GetVersion)))
	v1.Handle("GET /v1/rulesets/{key}/versions/{version}/bundle", requireRole(domain.RoleViewer, http.HandlerFunc(h.GetVersionBundle)))

	// Admin API: admin role required.
	v1.Handle("GET /v1/admin/users", requireAdmin(http.HandlerFunc(admin.ListUsers)))
	v1.Handle("DELETE /v1/admin/users/{userID}", requireAdmin(http.HandlerFunc(admin.DeleteUser)))
	v1.Handle("GET /v1/admin/users/{userID}/roles", requireAdmin(http.HandlerFunc(admin.ListUserRoles)))
	v1.Handle("PUT /v1/admin/users/{userID}/roles/{namespace}", requireAdmin(http.HandlerFunc(admin.UpsertUserRole)))
	v1.Handle("DELETE /v1/admin/users/{userID}/roles/{namespace}", requireAdmin(http.HandlerFunc(admin.DeleteUserRole)))
	v1.Handle("POST /v1/admin/keys", requireAdmin(http.HandlerFunc(admin.CreateAPIKey)))
	v1.Handle("GET /v1/admin/keys", requireAdmin(http.HandlerFunc(admin.ListAPIKeys)))
	v1.Handle("DELETE /v1/admin/keys/{keyID}", requireAdmin(http.HandlerFunc(admin.RevokeAPIKey)))

	mux.Handle("/v1/", apiTokenMiddleware(db, secret, v1))

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 500*time.Millisecond)
		defer cancel()

		uptime := int64(math.Round(time.Since(startTime).Seconds()))
		w.Header().Set("Content-Type", "application/json")

		if err := db.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"status":         "degraded",
				"version":        version.Version,
				"store":          cfg.Store,
				"uptime_seconds": uptime,
				"error":          "database unreachable",
			})
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"status":         "ok",
			"version":        version.Version,
			"store":          cfg.Store,
			"uptime_seconds": uptime,
		})
	})

	var root http.Handler = mux
	if cfg.CORSOrigins != "" {
		root = corsMiddleware(cfg.CORSOrigins, root)
	}
	return loggingMiddleware(recoveryMiddleware(root))
}
