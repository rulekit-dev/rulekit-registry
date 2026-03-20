package httptransport

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/rulekit-dev/rulekit-registry/internal/datastore"
	"github.com/rulekit-dev/rulekit-registry/internal/jwtutil"
	"github.com/rulekit-dev/rulekit-registry/internal/model"
	"github.com/rulekit-dev/rulekit-registry/internal/transport/http/handler"
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

// authMiddleware handles the legacy single-API-key mode (AuthMode=none).
func authMiddleware(apiKey string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(token), []byte(apiKey)) != 1 {
			handler.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid or missing API key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// jwtAuthMiddleware validates a JWT access token and attaches claims to context.
func jwtAuthMiddleware(secret []byte, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" {
			handler.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing Authorization header")
			return
		}
		claims, err := jwtutil.ParseAccessToken(secret, token)
		if errors.Is(err, jwtutil.ErrTokenExpired) {
			handler.WriteError(w, http.StatusUnauthorized, "TOKEN_EXPIRED", "access token has expired")
			return
		}
		if err != nil {
			handler.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid access token")
			return
		}
		ctx := handler.ClaimsContext(r.Context(), claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// apiTokenMiddleware tries JWT first, falls back to opaque DB token lookup.
func apiTokenMiddleware(db datastore.Datastore, secret []byte, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if raw == "" {
			handler.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing Authorization header")
			return
		}

		// Try JWT first.
		claims, err := jwtutil.ParseAccessToken(secret, raw)
		if err == nil {
			ctx := handler.ClaimsContext(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		if errors.Is(err, jwtutil.ErrTokenExpired) {
			handler.WriteError(w, http.StatusUnauthorized, "TOKEN_EXPIRED", "access token has expired")
			return
		}

		// Fall back to opaque API token lookup.
		sum := sha256.Sum256([]byte(raw))
		tokenHash := hex.EncodeToString(sum[:])
		t, dbErr := db.GetAPITokenByHash(r.Context(), tokenHash)
		if errors.Is(dbErr, datastore.ErrNotFound) {
			handler.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid or missing token")
			return
		}
		if dbErr != nil {
			handler.WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to validate token")
			return
		}
		if t.RevokedAt != nil {
			handler.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "token has been revoked")
			return
		}
		if t.ExpiresAt != nil && time.Now().After(*t.ExpiresAt) {
			handler.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "token has expired")
			return
		}

		// Synthesise claims from the token so handler RBAC checks are uniform.
		synth := &jwtutil.Claims{
			Email: "",
			Roles: []jwtutil.RoleClaim{{Namespace: t.Namespace, RoleMask: t.Role}},
		}
		synth.Subject = t.UserID
		ctx := handler.ClaimsContext(r.Context(), synth)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireRole checks the caller has at least the given role in the target namespace.
func requireRole(required model.Role, next http.Handler) http.Handler {
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
	return requireRole(model.RoleAdmin, next)
}

// corsMiddleware adds CORS headers based on a comma-separated allowlist.
// Pass "*" to allow all origins.
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
