package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"

	"github.com/rulekit-dev/rulekit-registry/internal/jwtutil"
)

var identPattern = regexp.MustCompile(`^[a-z0-9_-]+$`)

func ValidKey(s string) bool {
	return len(s) > 0 && len(s) <= 128 && identPattern.MatchString(s)
}

func ValidNamespace(s string) bool {
	return len(s) > 0 && len(s) <= 128 && identPattern.MatchString(s)
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func NamespaceParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	ns := r.URL.Query().Get("namespace")
	if ns == "" {
		return "default", true
	}
	if !ValidNamespace(ns) {
		WriteError(w, http.StatusBadRequest, "INVALID_NAMESPACE",
			"namespace must be non-empty, at most 128 characters, and match [a-z0-9_-]")
		return "", false
	}
	return ns, true
}

func PageParams(r *http.Request) (limit, offset int) {
	limit = 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 200 {
		limit = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v >= 0 {
		offset = v
	}
	return limit, offset
}

// contextKey is used to store auth claims in request context.
type contextKey int

const ClaimsKey contextKey = iota

func ClaimsFromContext(ctx context.Context) *jwtutil.Claims {
	v, _ := ctx.Value(ClaimsKey).(*jwtutil.Claims)
	return v
}
