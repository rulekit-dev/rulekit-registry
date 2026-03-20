package httpadapter

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/rulekit-dev/rulekit-registry/internal/adapter/http/handler"
	"github.com/rulekit-dev/rulekit-registry/internal/domain"
	"github.com/rulekit-dev/rulekit-registry/internal/port"
	"github.com/rulekit-dev/rulekit-registry/internal/util"
)

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

// apiTokenMiddleware tries JWT first, then falls back to opaque API key lookup.
func apiTokenMiddleware(db storePort, secret []byte, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if raw == "" {
			handler.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing Authorization header")
			return
		}

		// Try JWT first.
		claims, err := util.ParseAccessToken(secret, raw)
		if err == nil {
			ctx := handler.ClaimsContext(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		if errors.Is(err, util.ErrTokenExpired) {
			handler.WriteError(w, http.StatusUnauthorized, "TOKEN_EXPIRED", "access token has expired")
			return
		}

		// Fall back to opaque API key lookup.
		sum := sha256.Sum256([]byte(raw))
		keyHash := hex.EncodeToString(sum[:])
		k, dbErr := db.GetAPIKeyByHash(r.Context(), keyHash)
		if errors.Is(dbErr, port.ErrNotFound) {
			handler.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid or missing token")
			return
		}
		if dbErr != nil {
			handler.WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to validate token")
			return
		}
		if k.RevokedAt != nil {
			handler.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "token has been revoked")
			return
		}
		if k.ExpiresAt != nil && time.Now().After(*k.ExpiresAt) {
			handler.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "token has expired")
			return
		}

		// Synthesise claims from the API key so downstream RBAC checks are uniform.
		synth := &util.Claims{
			Roles: []util.RoleClaim{{Namespace: k.Namespace, RoleMask: k.Role}},
		}
		ctx := handler.ClaimsContext(r.Context(), synth)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireRole checks the caller has at least the given role in the target namespace.
func requireRole(required domain.Role, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := handler.ClaimsFromContext(r.Context())
		if claims == nil {
			handler.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
			return
		}

		ns := r.URL.Query().Get("namespace")
		if ns == "" {
			ns = "default"
		}

		mask := claims.RoleForNamespace(ns)
		if !mask.Has(required) {
			handler.WriteError(w, http.StatusForbidden, "FORBIDDEN", "insufficient permissions")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requireAdmin(next http.Handler) http.Handler {
	return requireRole(domain.RoleAdmin, next)
}

// corsMiddleware adds CORS headers based on a comma-separated allowlist.
func corsMiddleware(origins string, next http.Handler) http.Handler {
	allowed := make(map[string]bool)
	wildcard := false
	for _, o := range strings.Split(origins, ",") {
		o = strings.TrimSpace(o)
		if o == "*" {
			wildcard = true
			break
		}
		if o != "" {
			allowed[o] = true
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (wildcard || allowed[origin]) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Max-Age", "86400")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered", "panic", rec, "stack", string(debug.Stack()))
				http.Error(w, `{"error":{"code":"INTERNAL","message":"internal server error"}}`, http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
