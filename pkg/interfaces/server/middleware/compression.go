package middleware

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/yairfalse/tapio/pkg/domain"
)

// CompressionMiddleware handles response compression
type CompressionMiddleware struct {
	level  int
	logger domain.Logger

	// Minimum size for compression (bytes)
	minSize int

	// Content types to compress
	compressibleTypes map[string]bool
}

// NewCompressionMiddleware creates a new compression middleware
func NewCompressionMiddleware(logger domain.Logger) *CompressionMiddleware {
	return &CompressionMiddleware{
		level:   gzip.DefaultCompression,
		logger:  logger,
		minSize: 1024, // 1KB minimum
		compressibleTypes: map[string]bool{
			"text/plain":                    true,
			"text/html":                     true,
			"text/css":                      true,
			"text/javascript":               true,
			"application/json":              true,
			"application/javascript":        true,
			"application/x-javascript":      true,
			"application/xml":               true,
			"application/x-font-ttf":        true,
			"application/x-font-opentype":   true,
			"application/vnd.ms-fontobject": true,
			"image/svg+xml":                 true,
		},
	}
}

// Execute implements the middleware interface
func (m *CompressionMiddleware) Execute(ctx context.Context, request *domain.Request, response *domain.Response, next func() error) error {
	// For domain-level middleware, compression is typically handled at transport layer
	// This is a placeholder that would integrate with the transport
	return next()
}

// Name returns the middleware name
func (m *CompressionMiddleware) Name() string {
	return "compression"
}

// Priority returns the middleware priority
func (m *CompressionMiddleware) Priority() int {
	return 50 // Execute after CORS but before most other middleware
}

// Configure configures the middleware
func (m *CompressionMiddleware) Configure(ctx context.Context, config map[string]interface{}) error {
	if level, ok := config["level"].(int); ok && level >= gzip.NoCompression && level <= gzip.BestCompression {
		m.level = level
	}

	if minSize, ok := config["min_size"].(int); ok && minSize > 0 {
		m.minSize = minSize
	}

	if types, ok := config["compressible_types"].([]string); ok {
		m.compressibleTypes = make(map[string]bool)
		for _, t := range types {
			m.compressibleTypes[t] = true
		}
	}

	if m.logger != nil {
		m.logger.Info(ctx, fmt.Sprintf("compression middleware configured: level=%d, minSize=%d", m.level, m.minSize))
	}

	return nil
}

// HTTPMiddleware returns an HTTP handler middleware for compression
func (m *CompressionMiddleware) HTTPMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if client accepts gzip
			if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
				next.ServeHTTP(w, r)
				return
			}

			// Create gzip response writer
			gw := &gzipResponseWriter{
				ResponseWriter: w,
				gzipWriter:     nil,
				minSize:        m.minSize,
				compressible:   m.compressibleTypes,
			}
			defer gw.Close()

			// Set encoding header
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Del("Content-Length") // Remove content length as it will change

			// Serve with gzip writer
			next.ServeHTTP(gw, r)
		})
	}
}

// gzipResponseWriter wraps http.ResponseWriter to provide gzip compression
type gzipResponseWriter struct {
	http.ResponseWriter
	gzipWriter   *gzip.Writer
	minSize      int
	written      int
	compressible map[string]bool
	statusCode   int
}

// Write implements io.Writer
func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	// Check if we should compress
	if w.gzipWriter == nil {
		w.written += len(b)

		// Check minimum size
		if w.written < w.minSize {
			return w.ResponseWriter.Write(b)
		}

		// Check content type
		contentType := w.Header().Get("Content-Type")
		if contentType == "" {
			contentType = http.DetectContentType(b)
			w.Header().Set("Content-Type", contentType)
		}

		// Extract base content type (remove charset etc)
		if idx := strings.Index(contentType, ";"); idx != -1 {
			contentType = contentType[:idx]
		}
		contentType = strings.TrimSpace(contentType)

		// Check if compressible
		if !w.compressible[contentType] {
			w.Header().Del("Content-Encoding")
			return w.ResponseWriter.Write(b)
		}

		// Initialize gzip writer
		w.gzipWriter = gzip.NewWriter(w.ResponseWriter)
	}

	return w.gzipWriter.Write(b)
}

// WriteHeader implements http.ResponseWriter
func (w *gzipResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

// Close closes the gzip writer
func (w *gzipResponseWriter) Close() error {
	if w.gzipWriter != nil {
		return w.gzipWriter.Close()
	}
	return nil
}

// Flush implements http.Flusher
func (w *gzipResponseWriter) Flush() {
	if w.gzipWriter != nil {
		w.gzipWriter.Flush()
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack implements http.Hijacker
func (w *gzipResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, fmt.Errorf("ResponseWriter does not implement http.Hijacker")
}

// Push implements http.Pusher
func (w *gzipResponseWriter) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

// DecompressionMiddleware handles request decompression
type DecompressionMiddleware struct {
	logger domain.Logger
}

// NewDecompressionMiddleware creates a new decompression middleware
func NewDecompressionMiddleware(logger domain.Logger) *DecompressionMiddleware {
	return &DecompressionMiddleware{
		logger: logger,
	}
}

// HTTPMiddleware returns an HTTP handler middleware for decompression
func (m *DecompressionMiddleware) HTTPMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check content encoding
			encoding := r.Header.Get("Content-Encoding")

			switch encoding {
			case "gzip":
				reader, err := gzip.NewReader(r.Body)
				if err != nil {
					if m.logger != nil {
						m.logger.Error(r.Context(), fmt.Sprintf("failed to create gzip reader: %v", err))
					}
					http.Error(w, "Invalid gzip encoding", http.StatusBadRequest)
					return
				}
				defer reader.Close()

				// Replace request body
				r.Body = io.NopCloser(reader)
				r.Header.Del("Content-Encoding")
				r.Header.Del("Content-Length")

			case "deflate":
				// Could add deflate support here
				http.Error(w, "Deflate encoding not supported", http.StatusUnsupportedMediaType)
				return

			case "":
				// No encoding, pass through

			default:
				http.Error(w, fmt.Sprintf("Unsupported encoding: %s", encoding), http.StatusUnsupportedMediaType)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
