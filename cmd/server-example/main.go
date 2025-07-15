package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/yairfalse/tapio/pkg/server"
	"github.com/yairfalse/tapio/pkg/server/config"
	"github.com/yairfalse/tapio/pkg/server/domain"
	"github.com/yairfalse/tapio/pkg/server/logging"
	"github.com/yairfalse/tapio/pkg/server/managers"
	"github.com/yairfalse/tapio/pkg/server/middleware"
	"github.com/yairfalse/tapio/pkg/server/transports"
)

func main() {
	// Create context
	ctx := context.Background()
	
	// Load configuration
	cfg, err := config.LoadConfiguration(ctx)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}
	
	// Create logger
	logger, err := logging.NewZapLogger(&cfg.Logging)
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Sync()
	
	logger.Info(ctx, "Starting Tapio Server", 
		"version", cfg.Server.Version,
		"environment", cfg.Server.Environment,
	)
	
	// Create metrics collector
	metricsCollector := managers.NewMetricsCollector(logger)
	
	// Create event publisher (using a simple implementation)
	eventPublisher := NewSimpleEventPublisher(logger)
	
	// Create connection manager
	connectionManager := managers.NewConnectionManager(
		cfg.Server.MaxConnections,
		metricsCollector,
		eventPublisher,
		logger,
	)
	
	// Create health checker
	healthChecker := managers.NewHealthChecker(logger)
	
	// Register additional health checks
	healthChecker.RegisterCheck("database", func(ctx context.Context) (*domain.HealthCheck, error) {
		// Simulate database health check
		return &domain.HealthCheck{
			Name:      "database",
			Status:    domain.HealthStatusPass,
			Message:   "Database is healthy",
			Timestamp: time.Now(),
			Duration:  10 * time.Millisecond,
		}, nil
	})
	
	// Create server builder
	serverBuilder := server.NewServerBuilder().
		WithConfig(cfg).
		WithLogger(logger).
		WithConnectionManager(connectionManager).
		WithHealthChecker(healthChecker).
		WithMetricsCollector(metricsCollector).
		WithEventPublisher(eventPublisher)
	
	// Build server
	srv, err := serverBuilder.Build()
	if err != nil {
		logger.Error(ctx, "Failed to build server", "error", err)
		os.Exit(1)
	}
	
	// Create and register HTTP transport
	httpConfig := &domain.EndpointConfig{
		Name:     "http",
		Protocol: "http",
		Address:  "0.0.0.0",
		Port:     8080,
		Path:     "/",
		Enabled:  true,
		Timeout:  30 * time.Second,
	}
	
	// Get request handler from server
	requestHandler := &serverRequestHandler{server: srv}
	
	httpTransport := transports.NewHTTPTransport(httpConfig, requestHandler, logger)
	
	// Setup middleware
	setupMiddleware(srv, cfg, logger, metricsCollector, eventPublisher)
	
	// Start metrics server on separate port
	go startMetricsServer(metricsCollector, logger)
	
	// Start HTTP transport
	if err := httpTransport.Start(ctx); err != nil {
		logger.Error(ctx, "Failed to start HTTP transport", "error", err)
		os.Exit(1)
	}
	
	// Start server
	if err := srv.Start(ctx); err != nil {
		logger.Error(ctx, "Failed to start server", "error", err)
		os.Exit(1)
	}
	
	logger.Info(ctx, "Server started successfully",
		"http_address", fmt.Sprintf("%s:%d", httpConfig.Address, httpConfig.Port),
		"metrics_address", ":9090",
	)
	
	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	
	logger.Info(ctx, "Shutdown signal received, stopping server...")
	
	// Create shutdown context with timeout
	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	
	// Stop HTTP transport
	if err := httpTransport.Stop(shutdownCtx); err != nil {
		logger.Error(ctx, "Error stopping HTTP transport", "error", err)
	}
	
	// Stop server
	if err := srv.Stop(shutdownCtx); err != nil {
		logger.Error(ctx, "Error stopping server", "error", err)
	}
	
	logger.Info(ctx, "Server stopped gracefully")
}

// serverRequestHandler adapts server to domain.RequestHandler interface
type serverRequestHandler struct {
	server *server.Server
}

func (h *serverRequestHandler) HandleRequest(ctx context.Context, request *domain.Request) (*domain.Response, error) {
	return h.server.HandleRequest(ctx, request)
}

func (h *serverRequestHandler) ValidateRequest(ctx context.Context, request *domain.Request) error {
	// Basic validation
	if request == nil {
		return domain.ErrInvalidRequest("request cannot be nil")
	}
	if request.ID == "" {
		return domain.ErrInvalidRequest("request ID cannot be empty")
	}
	return nil
}

func (h *serverRequestHandler) RouteRequest(ctx context.Context, request *domain.Request) (string, error) {
	// Simple routing based on request type
	switch request.Type {
	case domain.RequestTypeHealth:
		return "health", nil
	case domain.RequestTypeMetrics:
		return "metrics", nil
	case domain.RequestTypeEvent:
		return "event", nil
	case domain.RequestTypeQuery:
		return "query", nil
	case domain.RequestTypeCommand:
		return "command", nil
	case domain.RequestTypeStream:
		return "stream", nil
	default:
		return "", domain.ErrInvalidRequest(fmt.Sprintf("unknown request type: %s", request.Type))
	}
}

// setupMiddleware configures server middleware
func setupMiddleware(
	srv *server.Server,
	cfg *domain.Configuration,
	logger domain.Logger,
	metricsCollector domain.MetricsCollector,
	eventPublisher domain.EventPublisher,
) {
	// Recovery middleware (highest priority)
	recoveryMiddleware := middleware.NewRecoveryMiddleware(logger, eventPublisher, metricsCollector)
	
	// CORS middleware
	corsMiddleware := middleware.NewCORSMiddleware(&cfg.Security.CORS, logger)
	
	// Compression middleware
	compressionMiddleware := middleware.NewCompressionMiddleware(logger)
	
	// Note: In a real implementation, we would register these with the server's middleware manager
	// For now, they're created but would need to be integrated into the request pipeline
	
	_ = recoveryMiddleware
	_ = corsMiddleware
	_ = compressionMiddleware
}

// startMetricsServer starts the Prometheus metrics server
func startMetricsServer(collector *managers.MetricsCollector, logger domain.Logger) {
	// Create HTTP mux
	mux := http.NewServeMux()
	
	// Register Prometheus handler
	mux.Handle("/metrics", promhttp.HandlerFor(
		collector.GetRegistry(),
		promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		},
	))
	
	// Start server
	server := &http.Server{
		Addr:    ":9090",
		Handler: mux,
	}
	
	logger.Info(context.Background(), "Metrics server started", "address", ":9090")
	
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error(context.Background(), "Metrics server error", "error", err)
	}
}

// SimpleEventPublisher is a basic event publisher implementation
type SimpleEventPublisher struct {
	logger domain.Logger
	handlers map[domain.EventType][]domain.EventHandler
	mu       sync.RWMutex
}

func NewSimpleEventPublisher(logger domain.Logger) *SimpleEventPublisher {
	return &SimpleEventPublisher{
		logger:   logger,
		handlers: make(map[domain.EventType][]domain.EventHandler),
	}
}

func (p *SimpleEventPublisher) PublishEvent(ctx context.Context, event *domain.Event) error {
	if event == nil {
		return domain.ErrInvalidRequest("event cannot be nil")
	}
	
	// Log event
	if p.logger != nil {
		p.logger.Info(ctx, "Event published",
			"event_id", event.ID,
			"event_type", string(event.Type),
			"severity", string(event.Severity),
			"message", event.Message,
		)
	}
	
	// Get handlers
	p.mu.RLock()
	handlers := p.handlers[event.Type]
	p.mu.RUnlock()
	
	// Execute handlers
	for _, handler := range handlers {
		go func(h domain.EventHandler) {
			if err := h.HandleEvent(ctx, event); err != nil {
				p.logger.Error(ctx, "Event handler error", "error", err)
			}
		}(handler)
	}
	
	return nil
}

func (p *SimpleEventPublisher) PublishEvents(ctx context.Context, events []*domain.Event) error {
	for _, event := range events {
		if err := p.PublishEvent(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func (p *SimpleEventPublisher) Subscribe(ctx context.Context, eventType domain.EventType, handler domain.EventHandler) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	p.handlers[eventType] = append(p.handlers[eventType], handler)
	return nil
}

func (p *SimpleEventPublisher) Unsubscribe(ctx context.Context, eventType domain.EventType, handler domain.EventHandler) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	// In a real implementation, we would properly track and remove handlers
	// For now, just clear all handlers for the event type
	delete(p.handlers, eventType)
	return nil
}