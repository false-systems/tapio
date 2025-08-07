package api

import (
	"context"
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// RequestIDKey is the context key for request ID
type RequestIDKey struct{}

// limitRequestSize limits the size of incoming requests
func (s *Server) limitRequestSize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, s.config.MaxRequestSize)
		next.ServeHTTP(w, r)
	})
}

// tracingMiddleware adds request ID and starts tracing span
func (s *Server) tracingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Generate or extract request ID
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = fmt.Sprintf("req-%d", time.Now().UnixNano())
		}

		// Add to context
		ctx := context.WithValue(r.Context(), RequestIDKey{}, requestID)

		// Start span
		ctx, span := s.instrumentation.StartSpan(ctx, "http.request",
			trace.WithAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.url", r.URL.String()),
				attribute.String("http.request_id", requestID),
			),
		)
		defer span.End()

		// Add to response header
		w.Header().Set("X-Request-ID", requestID)

		// Wrap response writer to capture status
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// Continue
		next.ServeHTTP(wrapped, r.WithContext(ctx))

		// Record response attributes
		span.SetAttributes(
			attribute.Int("http.status_code", wrapped.statusCode),
		)
	})
}

// loggingMiddleware logs all requests
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Get request ID
		requestID, _ := r.Context().Value(RequestIDKey{}).(string)

		// Wrap response writer
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// Log request start
		s.logger.Debug("Request started",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("remote_addr", r.RemoteAddr),
			zap.String("request_id", requestID),
		)

		// Continue
		next.ServeHTTP(wrapped, r)

		// Log request completion
		duration := time.Since(start)
		s.logger.Info("Request completed",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.Int("status", wrapped.statusCode),
			zap.Duration("duration", duration),
			zap.String("request_id", requestID),
		)

		// Record metrics
		s.instrumentation.RecordHTTPRequest(r.Context(), r.Method, r.URL.Path, wrapped.statusCode, duration)
	})
}

// corsMiddleware handles CORS
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Check if origin is allowed
		allowed := false
		for _, allowedOrigin := range s.config.AllowedOrigins {
			if allowedOrigin == "*" || allowedOrigin == origin {
				allowed = true
				break
			}
		}

		if allowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID")
			w.Header().Set("Access-Control-Max-Age", "86400")
		}

		// Handle preflight
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// recoveryMiddleware recovers from panics
func (s *Server) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				// Get request ID
				requestID, _ := r.Context().Value(RequestIDKey{}).(string)

				// Log panic
				s.logger.Error("Panic recovered",
					zap.Any("error", err),
					zap.String("stack", string(debug.Stack())),
					zap.String("method", r.Method),
					zap.String("path", r.URL.Path),
					zap.String("request_id", requestID),
				)

				// Return error response
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintf(w, `{"error":"Internal server error","request_id":"%s"}`, requestID)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.written {
		rw.statusCode = code
		rw.ResponseWriter.WriteHeader(code)
		rw.written = true
	}
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

// getRequestID gets request ID from context
func getRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(RequestIDKey{}).(string); ok {
		return id
	}
	return ""
}

// isHealthEndpoint checks if path is a health endpoint
func isHealthEndpoint(path string) bool {
	return strings.HasPrefix(path, "/health") || strings.HasPrefix(path, "/ready")
}
