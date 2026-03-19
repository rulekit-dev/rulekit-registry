package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/rulekit/rulekit-registry/internal/jwtutil"
	"github.com/rulekit/rulekit-registry/internal/model"
	"github.com/rulekit/rulekit-registry/internal/store"
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
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid or missing API key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// jwtAuthMiddleware validates a JWT access token and attaches claims to context.
// Used in AuthMode=jwt. Does not enforce role — role checks happen per-handler.
func jwtAuthMiddleware(secret []byte, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing Authorization header")
			return
		}
		claims, err := jwtutil.ParseAccessToken(secret, token)
		if errors.Is(err, jwtutil.ErrTokenExpired) {
			writeError(w, http.StatusUnauthorized, "TOKEN_EXPIRED", "access token has expired")
			return
		}
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid access token")
			return
		}
		ctx := context.WithValue(r.Context(), claimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// apiTokenMiddleware validates a long-lived API token (CLI/CI/SDK) and synthesises
// a Claims value so downstream handlers stay uniform.
// It is chained before jwtAuthMiddleware — if the bearer token parses as a JWT, JWT
// wins; if not, we fall through to DB lookup.
func apiTokenMiddleware(st store.Store, secret []byte, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if raw == "" {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing Authorization header")
			return
		}

		// Try JWT first.
		claims, err := jwtutil.ParseAccessToken(secret, raw)
		if err == nil {
			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		if errors.Is(err, jwtutil.ErrTokenExpired) {
			writeError(w, http.StatusUnauthorized, "TOKEN_EXPIRED", "access token has expired")
			return
		}

		// Fall back to opaque API token lookup.
		sum := sha256.Sum256([]byte(raw))
		tokenHash := hex.EncodeToString(sum[:])
		t, dbErr := st.GetAPITokenByHash(r.Context(), tokenHash)
		if errors.Is(dbErr, store.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid or missing token")
			return
		}
		if dbErr != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to validate token")
			return
		}
		if t.RevokedAt != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "token has been revoked")
			return
		}
		if t.ExpiresAt != nil && time.Now().After(*t.ExpiresAt) {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "token has expired")
			return
		}

		// Synthesise claims from the token so handler RBAC checks are uniform.
		synth := &jwtutil.Claims{
			Email: "",
			Roles: []jwtutil.RoleClaim{{Namespace: t.Namespace, RoleMask: t.Role}},
		}
		synth.Subject = t.UserID
		ctx := context.WithValue(r.Context(), claimsKey, synth)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireRole returns a middleware that checks the caller has at least the given
// role in the target namespace. The namespace is read from the "namespace" query
// param (defaulting to "default"). A global role (namespace="*") satisfies any check.
func requireRole(required model.Role, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromContext(r.Context())
		if claims == nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
			return
		}

		ns := r.URL.Query().Get("namespace")
		if ns == "" {
			ns = "default"
		}

		mask := claims.RoleForNamespace(ns)
		if !mask.Has(required) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "insufficient permissions")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAdmin is a convenience wrapper for RoleAdmin.
func requireAdmin(next http.Handler) http.Handler {
	return requireRole(model.RoleAdmin, next)
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
