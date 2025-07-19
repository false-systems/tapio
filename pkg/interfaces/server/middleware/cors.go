package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/yairfalse/tapio/pkg/server/domain"
)

// CORSMiddleware handles Cross-Origin Resource Sharing
type CORSMiddleware struct {
	config *domain.CORSConfig
	logger domain.Logger
}

// NewCORSMiddleware creates a new CORS middleware
func NewCORSMiddleware(config *domain.CORSConfig, logger domain.Logger) *CORSMiddleware {
	return &CORSMiddleware{
		config: config,
		logger: logger,
	}
}

// Execute implements the middleware interface
func (m *CORSMiddleware) Execute(ctx context.Context, request *domain.Request, response *domain.Response, next func() error) error {
	if !m.config.Enabled {
		return next()
	}

	// For HTTP transport, we need access to the actual HTTP request/response
	// This is typically done through context values
	httpReq, _ := ctx.Value("http_request").(*http.Request)
	httpResp, _ := ctx.Value("http_response").(http.ResponseWriter)

	if httpReq != nil && httpResp != nil {
		origin := httpReq.Header.Get("Origin")

		// Check if origin is allowed
		if m.isOriginAllowed(origin) {
			httpResp.Header().Set("Access-Control-Allow-Origin", origin)
			httpResp.Header().Set("Access-Control-Allow-Credentials", "true")
		} else if len(m.config.AllowedOrigins) == 1 && m.config.AllowedOrigins[0] == "*" {
			httpResp.Header().Set("Access-Control-Allow-Origin", "*")
		}

		// Handle preflight requests
		if httpReq.Method == "OPTIONS" {
			m.handlePreflight(httpResp, httpReq)
			response.Status = domain.ResponseStatusOK
			response.Data = map[string]interface{}{"status": "ok"}
			return nil // Don't call next() for preflight
		}

		// Set allowed headers for actual requests
		if len(m.config.AllowedHeaders) > 0 {
			httpResp.Header().Set("Access-Control-Expose-Headers", strings.Join(m.config.AllowedHeaders, ", "))
		}
	}

	return next()
}

// handlePreflight handles CORS preflight requests
func (m *CORSMiddleware) handlePreflight(w http.ResponseWriter, r *http.Request) {
	// Set allowed methods
	if len(m.config.AllowedMethods) > 0 {
		w.Header().Set("Access-Control-Allow-Methods", strings.Join(m.config.AllowedMethods, ", "))
	}

	// Set allowed headers
	requestedHeaders := r.Header.Get("Access-Control-Request-Headers")
	if requestedHeaders != "" && len(m.config.AllowedHeaders) > 0 {
		w.Header().Set("Access-Control-Allow-Headers", strings.Join(m.config.AllowedHeaders, ", "))
	}

	// Set max age
	w.Header().Set("Access-Control-Max-Age", "86400") // 24 hours

	// Return 204 No Content for preflight
	w.WriteHeader(http.StatusNoContent)
}

// isOriginAllowed checks if an origin is allowed
func (m *CORSMiddleware) isOriginAllowed(origin string) bool {
	if origin == "" {
		return false
	}

	for _, allowed := range m.config.AllowedOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}

		// Support wildcard subdomains
		if strings.HasPrefix(allowed, "*.") {
			domain := strings.TrimPrefix(allowed, "*")
			if strings.HasSuffix(origin, domain) {
				return true
			}
		}
	}

	return false
}

// Name returns the middleware name
func (m *CORSMiddleware) Name() string {
	return "cors"
}

// Priority returns the middleware priority (higher = earlier execution)
func (m *CORSMiddleware) Priority() int {
	return 100 // Execute early
}

// Configure configures the middleware
func (m *CORSMiddleware) Configure(ctx context.Context, config map[string]interface{}) error {
	// Update CORS config from map if needed
	if enabled, ok := config["enabled"].(bool); ok {
		m.config.Enabled = enabled
	}

	if origins, ok := config["allowed_origins"].([]string); ok {
		m.config.AllowedOrigins = origins
	}

	if methods, ok := config["allowed_methods"].([]string); ok {
		m.config.AllowedMethods = methods
	}

	if headers, ok := config["allowed_headers"].([]string); ok {
		m.config.AllowedHeaders = headers
	}

	if m.logger != nil {
		m.logger.Info(ctx, fmt.Sprintf("CORS middleware configured: origins=%v", m.config.AllowedOrigins))
	}

	return nil
}

// HTTPMiddleware returns an HTTP handler middleware
func (m *CORSMiddleware) HTTPMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Check if origin is allowed
			if m.isOriginAllowed(origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			} else if len(m.config.AllowedOrigins) == 1 && m.config.AllowedOrigins[0] == "*" {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			}

			// Handle preflight requests
			if r.Method == "OPTIONS" {
				m.handlePreflight(w, r)
				return
			}

			// Set exposed headers
			if len(m.config.AllowedHeaders) > 0 {
				w.Header().Set("Access-Control-Expose-Headers", strings.Join(m.config.AllowedHeaders, ", "))
			}

			next.ServeHTTP(w, r)
		})
	}
}
