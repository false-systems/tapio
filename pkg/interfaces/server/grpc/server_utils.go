package grpc

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
)

// ServerConfig configures the gRPC server
type ServerConfig struct {
	Port                int
	EnableReflection    bool
	EnableHealthCheck   bool
	EnableAuth          bool
	EnableRateLimit     bool
	MaxRequestsPerSec   int
	ReadTimeout         time.Duration
	WriteTimeout        time.Duration
	IdleTimeout         time.Duration
	MaxRecvMessageSize  int
	MaxSendMessageSize  int
	MaxConcurrentStream int
}

// DefaultServerConfig returns default server configuration
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		Port:                8080,
		EnableReflection:    true,
		EnableHealthCheck:   true,
		EnableAuth:          false, // Disabled by default for development
		EnableRateLimit:     true,
		MaxRequestsPerSec:   10000,
		ReadTimeout:         30 * time.Second,
		WriteTimeout:        30 * time.Second,
		IdleTimeout:         120 * time.Second,
		MaxRecvMessageSize:  4 * 1024 * 1024, // 4MB
		MaxSendMessageSize:  4 * 1024 * 1024, // 4MB
		MaxConcurrentStream: 1000,
	}
}

// TapioGRPCServer wraps all Tapio gRPC services
type TapioGRPCServer struct {
	config ServerConfig
	server *grpc.Server
	logger *zap.Logger
	tracer trace.Tracer

	// Service instances
	tapioService         *TapioServer
	collectorService     *CollectorServer
	eventService         *EventServer
	observabilityService *ObservabilityServer
	correlationService   *CorrelationServer

	// Health checker
	healthServer *health.Server

	// Server state
	mu       sync.RWMutex
	running  bool
	listener net.Listener
}

// NewTapioGRPCServer creates a new Tapio gRPC server with all services
func NewTapioGRPCServer(config ServerConfig, logger *zap.Logger) (*TapioGRPCServer, error) {
	// Create tracer
	tracer := otel.Tracer("tapio-grpc-server")

	// Create middleware
	middleware := NewServerMiddleware(logger, tracer)

	// Build server options
	var opts []grpc.ServerOption

	// Add middleware interceptors
	opts = append(opts,
		grpc.UnaryInterceptor(middleware.UnaryInterceptor()),
		grpc.StreamInterceptor(middleware.StreamInterceptor()),
	)

	// Add optional interceptors
	if config.EnableRateLimit {
		opts = append(opts, grpc.UnaryInterceptor(RateLimitInterceptor(config.MaxRequestsPerSec)))
	}

	if config.EnableAuth {
		opts = append(opts, grpc.UnaryInterceptor(AuthInterceptor()))
	}

	// Add validation
	opts = append(opts, grpc.UnaryInterceptor(ValidationInterceptor()))

	// Add metrics
	opts = append(opts, grpc.UnaryInterceptor(MetricsInterceptor()))

	// Configure message sizes
	opts = append(opts,
		grpc.MaxRecvMsgSize(config.MaxRecvMessageSize),
		grpc.MaxSendMsgSize(config.MaxSendMessageSize),
		grpc.MaxConcurrentStreams(uint32(config.MaxConcurrentStream)),
	)

	// Create gRPC server
	server := grpc.NewServer(opts...)

	// Create health server
	healthServer := health.NewServer()

	return &TapioGRPCServer{
		config:       config,
		server:       server,
		logger:       logger,
		tracer:       tracer,
		healthServer: healthServer,
	}, nil
}

// InitializeServices creates and registers all Tapio services
func (s *TapioGRPCServer) InitializeServices() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Info("Initializing Tapio gRPC services")

	// Create service instances with real storage
	s.tapioService = NewTapioServerWithRealStore(s.logger.Named("tapio"), s.tracer)
	s.collectorService = NewCollectorServerWithRealStore(s.logger.Named("collector"), s.tracer)
	s.eventService = NewEventServerWithRealStore(s.logger.Named("event"), s.tracer)
	s.observabilityService = NewObservabilityServerWithRealStore(s.logger.Named("observability"), s.tracer)
	s.correlationService = NewCorrelationServerWithRealStore(s.logger.Named("correlation"), s.tracer)

	// Register services with gRPC server
	pb.RegisterTapioServiceServer(s.server, s.tapioService)
	pb.RegisterCollectorServiceServer(s.server, s.collectorService)
	pb.RegisterEventServiceServer(s.server, s.eventService)
	pb.RegisterObservabilityServiceServer(s.server, s.observabilityService)
	pb.RegisterCorrelationServiceServer(s.server, s.correlationService)

	// Register health service
	if s.config.EnableHealthCheck {
		grpc_health_v1.RegisterHealthServer(s.server, s.healthServer)

		// Set all services as healthy
		s.healthServer.SetServingStatus("tapio.v1.TapioService", grpc_health_v1.HealthCheckResponse_SERVING)
		s.healthServer.SetServingStatus("tapio.v1.CollectorService", grpc_health_v1.HealthCheckResponse_SERVING)
		s.healthServer.SetServingStatus("tapio.v1.EventService", grpc_health_v1.HealthCheckResponse_SERVING)
		s.healthServer.SetServingStatus("tapio.v1.ObservabilityService", grpc_health_v1.HealthCheckResponse_SERVING)
		s.healthServer.SetServingStatus("tapio.v1.CorrelationService", grpc_health_v1.HealthCheckResponse_SERVING)
		s.healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING) // Overall health
	}

	// Enable reflection for development
	if s.config.EnableReflection {
		reflection.Register(s.server)
		s.logger.Info("gRPC reflection enabled")
	}

	s.logger.Info("All Tapio gRPC services initialized successfully")
	return nil
}

// Start starts the gRPC server
func (s *TapioGRPCServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("server is already running")
	}

	// Create listener
	addr := fmt.Sprintf(":%d", s.config.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	s.listener = listener
	s.running = true

	s.logger.Info("Starting Tapio gRPC server",
		zap.String("address", addr),
		zap.Bool("reflection", s.config.EnableReflection),
		zap.Bool("health_check", s.config.EnableHealthCheck),
		zap.Bool("auth", s.config.EnableAuth),
		zap.Bool("rate_limit", s.config.EnableRateLimit),
		zap.Int("max_requests_per_sec", s.config.MaxRequestsPerSec),
	)

	// Start server in goroutine
	go func() {
		if err := s.server.Serve(listener); err != nil {
			s.logger.Error("gRPC server failed", zap.Error(err))
		}
	}()

	s.logger.Info("Tapio gRPC server started successfully", zap.String("address", addr))
	return nil
}

// Stop gracefully stops the gRPC server
func (s *TapioGRPCServer) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	s.logger.Info("Stopping Tapio gRPC server")

	// Set health status to not serving
	if s.config.EnableHealthCheck {
		s.healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	}

	// Graceful shutdown with timeout
	done := make(chan struct{})
	go func() {
		s.server.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info("gRPC server stopped gracefully")
	case <-ctx.Done():
		s.logger.Warn("Graceful shutdown timeout, forcing stop")
		s.server.Stop()
	}

	s.running = false
	return nil
}

// GetServiceStats returns statistics for all services
func (s *TapioGRPCServer) GetServiceStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.running {
		return map[string]interface{}{
			"server": map[string]interface{}{
				"running": false,
			},
		}
	}

	stats := map[string]interface{}{
		"server": map[string]interface{}{
			"running":    true,
			"port":       s.config.Port,
			"reflection": s.config.EnableReflection,
			"health":     s.config.EnableHealthCheck,
			"auth":       s.config.EnableAuth,
			"rate_limit": s.config.EnableRateLimit,
		},
	}

	// Get stats from each service
	if s.tapioService != nil {
		stats["tapio"] = s.tapioService.GetServiceStats()
	}
	if s.collectorService != nil {
		stats["collector"] = s.collectorService.GetServiceStats()
	}
	if s.eventService != nil {
		stats["event"] = s.eventService.GetServiceStats()
	}
	if s.observabilityService != nil {
		stats["observability"] = s.observabilityService.GetServiceStats()
	}
	if s.correlationService != nil {
		stats["correlation"] = s.correlationService.GetServiceStats()
	}

	return stats
}

// HealthCheck checks the health of all services
func (s *TapioGRPCServer) HealthCheck() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.running {
		return fmt.Errorf("server is not running")
	}

	// Check each service
	var errors []string

	if s.tapioService != nil {
		if err := s.tapioService.HealthCheck(); err != nil {
			errors = append(errors, fmt.Sprintf("tapio: %v", err))
		}
	}

	if s.collectorService != nil {
		if err := s.collectorService.HealthCheck(); err != nil {
			errors = append(errors, fmt.Sprintf("collector: %v", err))
		}
	}

	if s.eventService != nil {
		if err := s.eventService.HealthCheck(); err != nil {
			errors = append(errors, fmt.Sprintf("event: %v", err))
		}
	}

	if s.observabilityService != nil {
		if err := s.observabilityService.HealthCheck(); err != nil {
			errors = append(errors, fmt.Sprintf("observability: %v", err))
		}
	}

	if s.correlationService != nil {
		if err := s.correlationService.HealthCheck(); err != nil {
			errors = append(errors, fmt.Sprintf("correlation: %v", err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("service health check failed: %v", errors)
	}

	return nil
}

// GetServices returns references to all service instances
func (s *TapioGRPCServer) GetServices() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"tapio":         s.tapioService,
		"collector":     s.collectorService,
		"event":         s.eventService,
		"observability": s.observabilityService,
		"correlation":   s.correlationService,
	}
}

// ConfigureIntegrations sets up cross-service integrations
func (s *TapioGRPCServer) ConfigureIntegrations() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Info("Configuring service integrations")

	// Configure event ingestion from collectors to event service
	if s.eventService != nil && s.collectorService != nil {
		// Set up collector -> event service integration
		s.eventService.ConfigureEventIngestion(EventIngestionConfig{
			EnableCollectorIngestion: true,
			EnableeBPFIngestion:      true,
			EnableK8sIngestion:       true,
			EnableOTELIngestion:      true,
			MaxEventsPerSecond:       s.config.MaxRequestsPerSec,
			MaxEventsPerBatch:        10000,
			EnableContextEnrichment:  true,
			EnableAIEnrichment:       false,
		})
	}

	// Configure correlation analysis from events
	if s.correlationService != nil && s.eventService != nil {
		s.correlationService.ConfigureCorrelationIngestion(CorrelationIngestionConfig{
			EnableCollectorAnalysis: true,
			EnableeBPFAnalysis:      true,
			EnableK8sAnalysis:       true,
			EnableOTELAnalysis:      true,
			ConfidenceThreshold:     0.7,
			MaxEventsPerAnalysis:    10000,
			AnalysisTimeout:         30 * time.Second,
			EnableRealTimeUpdates:   true,
			EnableRootCause:         true,
			EnablePredictions:       true,
			EnableImpactAssess:      true,
		})

		// Start periodic correlation analysis
		s.correlationService.StartPeriodicAnalysis(5 * time.Minute)
	}

	s.logger.Info("Service integrations configured successfully")
	return nil
}

// Address returns the server listening address
func (s *TapioGRPCServer) Address() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return fmt.Sprintf(":%d", s.config.Port)
}
