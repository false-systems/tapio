package grpc

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

// Server implements unified gRPC+REST server following Polar Signals pattern
type Server struct {
	config Config
	logger *zap.Logger

	grpcServer   *grpc.Server
	httpMux      *runtime.ServeMux
	healthServer *health.Server

	listener net.Listener

	// Service implementations
	eventService     *EventServiceImpl
	collectorService *CollectorServiceImpl
	tapioService     *TapioServiceComplete
}

// Config holds server configuration
type Config struct {
	Address            string
	EnableReflection   bool
	EnableHealth       bool
	MaxBatchSize       int
	MaxEventsPerSecond int
	ClusterID          string
	NodeID             string
}

// NewServer creates a new unified server
func NewServer(config Config, logger *zap.Logger) (*Server, error) {
	// Create gRPC server
	grpcServer := grpc.NewServer()

	// Create health server
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)

	// Enable reflection for development
	if config.EnableReflection {
		reflection.Register(grpcServer)
	}

	// Create HTTP mux for REST gateway
	httpMux := runtime.NewServeMux()

	return &Server{
		config:       config,
		logger:       logger,
		grpcServer:   grpcServer,
		httpMux:      httpMux,
		healthServer: healthServer,
	}, nil
}

// SetEventService sets the event service implementation
func (s *Server) SetEventService(service *EventServiceImpl) {
	s.eventService = service
	pb.RegisterEventServiceServer(s.grpcServer, service)
}

// SetCollectorService sets the collector service implementation
func (s *Server) SetCollectorService(service *CollectorServiceImpl) {
	s.collectorService = service
	pb.RegisterCollectorServiceServer(s.grpcServer, service)
}

// SetTapioService sets the tapio service implementation
func (s *Server) SetTapioService(service *TapioServiceComplete) {
	s.tapioService = service
	pb.RegisterTapioServiceServer(s.grpcServer, service)
}

// RegisterServices registers all gRPC services and their REST gateways
func (s *Server) RegisterServices() error {
	ctx := context.Background()
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}

	// Register EventService REST gateway
	if s.eventService != nil {
		if err := pb.RegisterEventServiceHandlerFromEndpoint(ctx, s.httpMux, s.config.Address, opts); err != nil {
			return fmt.Errorf("failed to register EventService REST gateway: %w", err)
		}
		s.healthServer.SetServingStatus("tapio.v1.EventService", grpc_health_v1.HealthCheckResponse_SERVING)
	}

	// Register CollectorService REST gateway (if it has REST endpoints)
	if s.collectorService != nil {
		if err := pb.RegisterCollectorServiceHandlerFromEndpoint(ctx, s.httpMux, s.config.Address, opts); err != nil {
			// CollectorService might not have REST endpoints, log but don't fail
			s.logger.Debug("CollectorService has no REST endpoints", zap.Error(err))
		}
		s.healthServer.SetServingStatus("tapio.v1.CollectorService", grpc_health_v1.HealthCheckResponse_SERVING)
	}

	// Register TapioService REST gateway
	if s.tapioService != nil {
		if err := pb.RegisterTapioServiceHandlerFromEndpoint(ctx, s.httpMux, s.config.Address, opts); err != nil {
			return fmt.Errorf("failed to register TapioService REST gateway: %w", err)
		}
		s.healthServer.SetServingStatus("tapio.v1.TapioService", grpc_health_v1.HealthCheckResponse_SERVING)
	}

	// Register CorrelationService REST gateway (when implemented)
	// if err := pb.RegisterCorrelationServiceHandlerFromEndpoint(ctx, s.httpMux, s.config.Address, opts); err != nil {
	//     return fmt.Errorf("failed to register CorrelationService REST gateway: %w", err)
	// }

	// Set overall health status
	s.healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	return nil
}

// Start starts the unified server
func (s *Server) Start() error {
	listener, err := net.Listen("tcp", s.config.Address)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	s.listener = listener

	s.logger.Info("Starting unified gRPC+REST server",
		zap.String("address", listener.Addr().String()),
		zap.Bool("reflection", s.config.EnableReflection),
		zap.Bool("health", s.config.EnableHealth),
	)

	// HTTP server that checks headers to multiplex between gRPC and REST
	httpServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
				s.grpcServer.ServeHTTP(w, r)
			} else {
				s.httpMux.ServeHTTP(w, r)
			}
		}),
	}

	return httpServer.Serve(s.listener)
}

// Stop gracefully stops the server
func (s *Server) Stop() {
	s.healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	s.grpcServer.GracefulStop()
	s.logger.Info("Server stopped")
}
