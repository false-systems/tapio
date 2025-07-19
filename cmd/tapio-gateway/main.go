package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/rs/cors"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
)

var (
	grpcServerEndpoint = flag.String("grpc-server-endpoint", "localhost:50051", "gRPC server endpoint")
	httpPort           = flag.Int("http-port", 8080, "HTTP port for REST API")
	swaggerDir         = flag.String("swagger-dir", "./proto/gen/openapiv2", "Swagger UI files directory")
	enableCORS         = flag.Bool("enable-cors", true, "Enable CORS support")
	enableSwagger      = flag.Bool("enable-swagger", true, "Enable Swagger UI")
)

func main() {
	flag.Parse()

	// Initialize logger
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	// Create context that listens for interrupt signal
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown gracefully
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		logger.Info("Shutting down gateway...")
		cancel()
	}()

	// Start the gateway
	if err := runGateway(ctx, logger); err != nil {
		logger.Fatal("Failed to run gateway", zap.Error(err))
	}
}

func runGateway(ctx context.Context, logger *zap.Logger) error {
	// Create gRPC-Gateway mux with custom options
	gwmux := runtime.NewServeMux(
		// Convert gRPC metadata to HTTP headers
		runtime.WithMetadata(func(ctx context.Context, req *http.Request) metadata.MD {
			md := metadata.New(map[string]string{
				"x-forwarded-host": req.Host,
				"x-request-id":     req.Header.Get("X-Request-ID"),
			})
			return md
		}),
		// Custom error handler
		runtime.WithErrorHandler(customErrorHandler),
		// Incoming header matcher
		runtime.WithIncomingHeaderMatcher(customHeaderMatcher),
		// Marshal using proto names
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			MarshalOptions: {
				UseProtoNames:   true,
				EmitUnpopulated: true,
			},
			UnmarshalOptions: {
				DiscardUnknown: true,
			},
		}),
	)

	// Register gRPC service handlers
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	
	// Register TapioService
	if err := pb.RegisterTapioServiceHandlerFromEndpoint(ctx, gwmux, *grpcServerEndpoint, opts); err != nil {
		return fmt.Errorf("failed to register TapioService: %w", err)
	}
	
	// Register CollectorService
	if err := pb.RegisterCollectorServiceHandlerFromEndpoint(ctx, gwmux, *grpcServerEndpoint, opts); err != nil {
		return fmt.Errorf("failed to register CollectorService: %w", err)
	}
	
	// Register EventService
	if err := pb.RegisterEventServiceHandlerFromEndpoint(ctx, gwmux, *grpcServerEndpoint, opts); err != nil {
		return fmt.Errorf("failed to register EventService: %w", err)
	}
	
	// Register CorrelationService
	if err := pb.RegisterCorrelationServiceHandlerFromEndpoint(ctx, gwmux, *grpcServerEndpoint, opts); err != nil {
		return fmt.Errorf("failed to register CorrelationService: %w", err)
	}
	
	// Register ObservabilityService
	if err := pb.RegisterObservabilityServiceHandlerFromEndpoint(ctx, gwmux, *grpcServerEndpoint, opts); err != nil {
		return fmt.Errorf("failed to register ObservabilityService: %w", err)
	}

	// Create HTTP mux
	mux := http.NewServeMux()
	
	// Mount gRPC-Gateway
	mux.Handle("/v1/", gwmux)
	
	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"healthy"}`))
	})
	
	// Swagger UI
	if *enableSwagger {
		mux.HandleFunc("/swagger.json", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, fmt.Sprintf("%s/tapio.swagger.json", *swaggerDir))
		})
		
		// Serve Swagger UI (you'll need to add swagger-ui files)
		mux.HandleFunc("/swagger/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(swaggerUIHTML))
		})
	}

	// Wrap with middleware
	handler := loggingMiddleware(logger)(mux)
	handler = authMiddleware(handler)
	handler = rateLimitMiddleware(handler)
	
	// Configure CORS
	if *enableCORS {
		c := cors.New(cors.Options{
			AllowedOrigins:   []string{"*"}, // Configure properly for production
			AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
			AllowedHeaders:   []string{"*"},
			ExposedHeaders:   []string{"X-Request-ID", "X-Trace-ID"},
			AllowCredentials: true,
			MaxAge:           3600,
		})
		handler = c.Handler(handler)
	}

	// Create HTTP server
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", *httpPort),
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server
	logger.Info("Starting gRPC-Gateway", 
		zap.String("grpc_endpoint", *grpcServerEndpoint),
		zap.Int("http_port", *httpPort),
		zap.Bool("cors_enabled", *enableCORS),
		zap.Bool("swagger_enabled", *enableSwagger),
	)

	// Run server
	errChan := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Wait for context cancellation or error
	select {
	case <-ctx.Done():
		// Graceful shutdown
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errChan:
		return err
	}
}

// Middleware functions

func loggingMiddleware(logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			
			// Wrap response writer to capture status
			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			
			// Process request
			next.ServeHTTP(wrapped, r)
			
			// Log request
			logger.Info("HTTP request",
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Int("status", wrapped.statusCode),
				zap.Duration("duration", time.Since(start)),
				zap.String("remote_addr", r.RemoteAddr),
			)
		})
	}
}

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for health and swagger endpoints
		if r.URL.Path == "/health" || r.URL.Path == "/swagger.json" || r.URL.Path == "/swagger/" {
			next.ServeHTTP(w, r)
			return
		}
		
		// Check for API key or Bearer token
		apiKey := r.Header.Get("X-API-Key")
		authHeader := r.Header.Get("Authorization")
		
		if apiKey == "" && authHeader == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		
		// TODO: Implement actual authentication logic
		// For now, just pass through
		next.ServeHTTP(w, r)
	})
}

func rateLimitMiddleware(next http.Handler) http.Handler {
	// TODO: Implement rate limiting
	// For now, just pass through
	return next
}

// Custom error handler for gRPC-Gateway
func customErrorHandler(ctx context.Context, mux *runtime.ServeMux, marshaler runtime.Marshaler, w http.ResponseWriter, r *http.Request, err error) {
	// Convert gRPC error to HTTP status code
	// TODO: Implement proper error mapping
	runtime.DefaultHTTPErrorHandler(ctx, mux, marshaler, w, r, err)
}

// Custom header matcher
func customHeaderMatcher(key string) (string, bool) {
	// Forward these headers to gRPC metadata
	switch key {
	case "X-Request-Id", "X-Trace-Id", "X-Api-Key":
		return key, true
	default:
		return runtime.DefaultHeaderMatcher(key)
	}
}

// Response writer wrapper to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// Minimal Swagger UI HTML
const swaggerUIHTML = `
<!DOCTYPE html>
<html>
<head>
    <title>Tapio API Documentation</title>
    <link rel="stylesheet" type="text/css" href="https://cdnjs.cloudflare.com/ajax/libs/swagger-ui/4.18.3/swagger-ui.css">
</head>
<body>
    <div id="swagger-ui"></div>
    <script src="https://cdnjs.cloudflare.com/ajax/libs/swagger-ui/4.18.3/swagger-ui-bundle.js"></script>
    <script src="https://cdnjs.cloudflare.com/ajax/libs/swagger-ui/4.18.3/swagger-ui-standalone-preset.js"></script>
    <script>
        window.onload = function() {
            const ui = SwaggerUIBundle({
                url: "/swagger.json",
                dom_id: '#swagger-ui',
                deepLinking: true,
                presets: [
                    SwaggerUIBundle.presets.apis,
                    SwaggerUIStandalonePreset
                ],
                plugins: [
                    SwaggerUIBundle.plugins.DownloadUrl
                ],
                layout: "StandaloneLayout"
            });
            window.ui = ui;
        }
    </script>
</body>
</html>
`