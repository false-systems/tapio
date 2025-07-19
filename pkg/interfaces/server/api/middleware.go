package api

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// AuthMiddleware provides optional API key authentication
func AuthMiddleware(apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// If no API key is configured, allow all requests
			if apiKey == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Check for API key in header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				authHeader = r.Header.Get("X-API-Key")
			}

			// Validate API key
			if authHeader == "" {
				writeError(w, http.StatusUnauthorized, nil)
				return
			}

			// Remove "Bearer " prefix if present
			key := strings.TrimPrefix(authHeader, "Bearer ")
			key = strings.TrimSpace(key)

			if key != apiKey {
				writeError(w, http.StatusUnauthorized, nil)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// TimeoutMiddleware adds request timeout
func TimeoutMiddleware(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			r = r.WithContext(ctx)
			next.ServeHTTP(w, r)
		})
	}
}

// RecoveryMiddleware recovers from panics
func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				writeError(w, http.StatusInternalServerError, nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
