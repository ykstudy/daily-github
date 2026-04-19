package mcpserver

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"daily-github/internal/readiness"
)

func bearerTokenMiddleware(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			provided := strings.TrimSpace(r.Header.Get("Authorization"))
			provided = strings.TrimSpace(strings.TrimPrefix(provided, "Bearer"))
			if len(provided) != len(token) || subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func readinessMiddleware(checker *readiness.Checker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if checker != nil && !checker.IsReady() {
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{
					"error":     "mcp server not ready",
					"readiness": checker.Snapshot(),
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
