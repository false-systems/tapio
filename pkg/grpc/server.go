package grpc

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/yairfalse/tapio/pkg/events"
)

// Server implements the CollectorService gRPC server with high-performance
// streaming, backpressure handling, and connection management.
type Server struct {
	UnimplementedCollectorServiceServer

	// Configuration
	config ServerConfig

	// Connection management
	connMgr *ConnectionManager

	// Event processing
	eventProcessor EventProcessor

	// Flow control
	flowController *FlowController

	// Metrics and monitoring
	metrics *ServerMetrics

	// Lifecycle management
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Server state
	started atomic.Bool
	stopped atomic.Bool
}

// ServerConfig contains server configuration options
type ServerConfig struct {
	// Server address and port
	Address string
	Port    int

	// TLS configuration
	TLSEnabled bool
	CertFile   string
	KeyFile    string

	// Performance settings
	MaxConcurrentStreams uint32
	MaxEventBufferSize   int
	MaxBatchSize         uint32
	DefaultEventsPerSec  uint32

	// Connection settings
	MaxConnectionAge  time.Duration
	KeepAliveTime     time.Duration
	KeepAliveTimeout  time.Duration
	MaxConnectionIdle time.Duration

	// Backpressure settings
	BackpressureThreshold float64 // 0.0 to 1.0
	MaxMemoryUsage        uint64  // bytes
	FlowControlWindow     uint32

	// Quality settings
	RequireAcks bool
	AckTimeout  time.Duration
	MaxRetries  int
}

// DefaultServerConfig returns a production-ready server configuration
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		Address:               "0.0.0.0",
		Port:                  9090,
		TLSEnabled:            true,
		MaxConcurrentStreams:  1000,
		MaxEventBufferSize:    1024 * 1024, // 1MB
		MaxBatchSize:          1000,
		DefaultEventsPerSec:   10000,
		MaxConnectionAge:      30 * time.Minute,
		KeepAliveTime:         30 * time.Second,
		KeepAliveTimeout:      5 * time.Second,
		MaxConnectionIdle:     15 * time.Minute,
		BackpressureThreshold: 0.8,
		MaxMemoryUsage:        50 * 1024 * 1024, // 50MB
		FlowControlWindow:     64 * 1024,        // 64KB
		RequireAcks:           true,
		AckTimeout:            5 * time.Second,
		MaxRetries:            3,
	}
}

// EventProcessor handles event processing logic
type EventProcessor interface {
	ProcessEvents(ctx context.Context, events []*events.UnifiedEvent) error
	ProcessEventBatch(ctx context.Context, batch *EventBatch) (*EventAck, error)
	GetProcessingStats() ProcessingStats
}

// ProcessingStats tracks event processing metrics
type ProcessingStats struct {
	EventsProcessed   uint64
	EventsFailed      uint64
	BatchesProcessed  uint64
	AvgProcessingTime time.Duration
	LastProcessedAt   time.Time
	ErrorRate         float64
}

// NewServer creates a new gRPC server with the specified configuration
func NewServer(config ServerConfig, processor EventProcessor) *Server {
	ctx, cancel := context.WithCancel(context.Background())

	server := &Server{
		config:         config,
		ctx:            ctx,
		cancel:         cancel,
		connMgr:        NewConnectionManager(config),
		eventProcessor: processor,
		flowController: NewFlowController(config),
		metrics:        NewServerMetrics(),
	}

	return server
}

// Start starts the gRPC server
func (s *Server) Start() error {
	if !s.started.CompareAndSwap(false, true) {
		return fmt.Errorf("server already started")
	}

	// Create gRPC server with optimized settings
	grpcServer := grpc.NewServer(
		grpc.MaxConcurrentStreams(s.config.MaxConcurrentStreams),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionAge:      s.config.MaxConnectionAge,
			MaxConnectionAgeGrace: 5 * time.Second,
			Time:                  s.config.KeepAliveTime,
			Timeout:               s.config.KeepAliveTimeout,
			MaxConnectionIdle:     s.config.MaxConnectionIdle,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	)

	// Register service
	RegisterCollectorServiceServer(grpcServer, s)

	// Start background tasks
	s.wg.Add(3)
	go s.monitorConnections()
	go s.monitorMemoryUsage()
	go s.updateFlowControl()

	s.metrics.ServerStarted()

	return nil
}

// Stop gracefully stops the server
func (s *Server) Stop() error {
	if !s.stopped.CompareAndSwap(false, true) {
		return fmt.Errorf("server already stopped")
	}

	s.cancel()
	s.wg.Wait()

	s.metrics.ServerStopped()

	return nil
}

// StreamEvents handles bidirectional streaming for real-time event delivery
func (s *Server) StreamEvents(stream CollectorService_StreamEventsServer) error {
	ctx := stream.Context()

	// Create connection state
	conn := s.connMgr.NewConnection(ctx)
	defer s.connMgr.CloseConnection(conn.ID)

	s.metrics.ConnectionEstablished()
	defer s.metrics.ConnectionClosed()

	// Setup bidirectional communication
	errChan := make(chan error, 2)

	// Handle incoming requests
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		errChan <- s.handleIncomingRequests(ctx, stream, conn)
	}()

	// Handle outgoing responses
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		errChan <- s.handleOutgoingResponses(ctx, stream, conn)
	}()

	// Wait for completion or error
	select {
	case err := <-errChan:
		if err != nil && err != io.EOF {
			s.metrics.ConnectionError()
			return status.Errorf(codes.Internal, "stream error: %v", err)
		}
		return nil
	case <-ctx.Done():
		return status.Errorf(codes.Canceled, "stream canceled")
	case <-s.ctx.Done():
		return status.Errorf(codes.Unavailable, "server shutting down")
	}
}

// handleIncomingRequests processes requests from collectors
func (s *Server) handleIncomingRequests(ctx context.Context, stream CollectorService_StreamEventsServer, conn *Connection) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.ctx.Done():
			return s.ctx.Err()
		default:
		}

		req, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("failed to receive request: %w", err)
		}

		s.metrics.RequestReceived()

		// Process request based on payload type
		switch payload := req.Payload.(type) {
		case *StreamRequest_Events:
			if err := s.handleEventBatch(ctx, conn, req.Sequence, payload.Events); err != nil {
				s.sendError(stream, req.Sequence, err)
				continue
			}

		case *StreamRequest_FlowControl:
			s.handleFlowControlMessage(conn, payload.FlowControl)

		case *StreamRequest_Status:
			conn.UpdateStatus(payload.Status)

		case *StreamRequest_Ack:
			conn.ProcessAck(payload.Ack)

		default:
			s.sendError(stream, req.Sequence, fmt.Errorf("unknown request payload type"))
		}
	}
}

// handleOutgoingResponses sends responses to collectors
func (s *Server) handleOutgoingResponses(ctx context.Context, stream CollectorService_StreamEventsServer, conn *Connection) error {
	ticker := time.NewTicker(100 * time.Millisecond) // 10 FPS for flow control updates
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.ctx.Done():
			return s.ctx.Err()
		case response := <-conn.ResponseChan:
			if err := stream.Send(response); err != nil {
				return fmt.Errorf("failed to send response: %w", err)
			}
			s.metrics.ResponseSent()

		case <-ticker.C:
			// Send periodic flow control updates
			if directive := s.flowController.GetDirective(conn); directive != nil {
				response := &StreamResponse{
					Sequence:  conn.NextSequence(),
					Timestamp: timestamppb.Now(),
					Payload: &StreamResponse_FlowControl{
						FlowControl: directive,
					},
				}

				if err := stream.Send(response); err != nil {
					return fmt.Errorf("failed to send flow control: %w", err)
				}
			}
		}
	}
}

// handleEventBatch processes a batch of events
func (s *Server) handleEventBatch(ctx context.Context, conn *Connection, sequence uint64, batch *EventBatch) error {
	start := time.Now()

	// Check backpressure
	if s.flowController.ShouldThrottle(conn) {
		s.metrics.EventsThrottled(uint64(len(batch.Events)))
		return s.sendThrottleResponse(conn, sequence, "backpressure detected")
	}

	// Validate batch
	if len(batch.Events) == 0 {
		return fmt.Errorf("empty event batch")
	}

	if len(batch.Events) > int(s.config.MaxBatchSize) {
		return fmt.Errorf("batch size %d exceeds maximum %d", len(batch.Events), s.config.MaxBatchSize)
	}

	// Process events
	ack, err := s.eventProcessor.ProcessEventBatch(ctx, batch)
	if err != nil {
		s.metrics.EventProcessingFailed(uint64(len(batch.Events)))
		return fmt.Errorf("failed to process events: %w", err)
	}

	// Update metrics
	processingTime := time.Since(start)
	s.metrics.EventsProcessed(uint64(len(batch.Events)), processingTime)
	conn.UpdateStats(uint64(len(batch.Events)), processingTime)

	// Send acknowledgment
	response := &StreamResponse{
		Sequence:  conn.NextSequence(),
		Timestamp: timestamppb.Now(),
		Payload: &StreamResponse_EventAck{
			EventAck: ack,
		},
	}

	select {
	case conn.ResponseChan <- response:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return fmt.Errorf("response channel full")
	}
}

// sendError sends an error response
func (s *Server) sendError(stream CollectorService_StreamEventsServer, sequence uint64, err error) {
	response := &StreamResponse{
		Sequence:  sequence,
		Timestamp: timestamppb.Now(),
		Payload: &StreamResponse_Error{
			Error: &ErrorResponse{
				Code:      ErrorCode_ERROR_INTERNAL_ERROR,
				Message:   err.Error(),
				Timestamp: timestamppb.Now(),
				Retryable: false,
			},
		},
	}

	if sendErr := stream.Send(response); sendErr != nil {
		// Log error but don't return it to avoid cascade failures
		s.metrics.ResponseSendFailed()
	}
}

// sendThrottleResponse sends a throttle response
func (s *Server) sendThrottleResponse(conn *Connection, sequence uint64, reason string) error {
	response := &StreamResponse{
		Sequence:  sequence,
		Timestamp: timestamppb.Now(),
		Payload: &StreamResponse_Error{
			Error: &ErrorResponse{
				Code:       ErrorCode_ERROR_RATE_LIMITED,
				Message:    fmt.Sprintf("rate limited: %s", reason),
				Timestamp:  timestamppb.Now(),
				Retryable:  true,
				RetryAfter: durationpb.New(time.Second),
			},
		},
	}

	select {
	case conn.ResponseChan <- response:
		return nil
	default:
		return fmt.Errorf("response channel full")
	}
}

// handleFlowControlMessage processes flow control requests from collectors
func (s *Server) handleFlowControlMessage(conn *Connection, msg *FlowControlMessage) {
	conn.SetRequestedRate(msg.RequestedRate)
	conn.SetBufferUtilization(msg.BufferUtilization)
	conn.SetMemoryPressure(msg.MemoryPressure)

	s.flowController.UpdateCollectorState(conn)
}

// SendEventBatch handles non-streaming batch requests
func (s *Server) SendEventBatch(ctx context.Context, req *EventBatchRequest) (*EventBatchResponse, error) {
	start := time.Now()

	// Validate request
	if req.Batch == nil {
		return nil, status.Errorf(codes.InvalidArgument, "batch is required")
	}

	if len(req.Batch.Events) == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "batch cannot be empty")
	}

	// Process batch
	ack, err := s.eventProcessor.ProcessEventBatch(ctx, req.Batch)
	if err != nil {
		s.metrics.EventProcessingFailed(uint64(len(req.Batch.Events)))
		return nil, status.Errorf(codes.Internal, "failed to process batch: %v", err)
	}

	// Update metrics
	processingTime := time.Since(start)
	s.metrics.EventsProcessed(uint64(len(req.Batch.Events)), processingTime)

	response := &EventBatchResponse{
		Success: true,
		Message: "batch processed successfully",
		Ack:     ack,
	}

	// Add flow control if needed
	if directive := s.flowController.GetGlobalDirective(); directive != nil {
		response.FlowControl = directive
	}

	return response, nil
}

// RegisterCollector handles collector registration
func (s *Server) RegisterCollector(ctx context.Context, req *CollectorRegistration) (*CollectorConfig, error) {
	// Validate registration
	if req.CollectorId == "" {
		return nil, status.Errorf(codes.InvalidArgument, "collector_id is required")
	}

	// Register with connection manager
	config := s.connMgr.RegisterCollector(req)

	s.metrics.CollectorRegistered(req.CollectorType)

	return config, nil
}

// Heartbeat handles heartbeat streams
func (s *Server) Heartbeat(stream CollectorService_HeartbeatServer) error {
	ctx := stream.Context()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.ctx.Done():
			return s.ctx.Err()
		default:
		}

		req, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return status.Errorf(codes.Internal, "heartbeat receive error: %v", err)
		}

		// Process heartbeat
		conn := s.connMgr.GetConnection(req.CollectorId)
		if conn != nil {
			conn.UpdateHeartbeat(req.Timestamp.AsTime())
			if req.Status != nil {
				conn.UpdateStatus(req.Status)
			}
		}

		// Send response
		response := &HeartbeatResponse{
			Timestamp:    timestamppb.Now(),
			ServerStatus: s.getServerStatus(),
		}

		if err := stream.Send(response); err != nil {
			return status.Errorf(codes.Internal, "heartbeat send error: %v", err)
		}
	}
}

// GetServerInfo returns server information
func (s *Server) GetServerInfo(ctx context.Context, req *ServerInfoRequest) (*ServerInfoResponse, error) {
	response := &ServerInfoResponse{
		ServerVersion: "1.0.0",
		SupportedFeatures: []string{
			"streaming",
			"batching",
			"compression",
			"flow_control",
			"load_balancing",
		},
		Capabilities: &ServerCapabilities{
			MaxEventsPerSecond:      165000, // Target throughput
			MaxConcurrentCollectors: s.config.MaxConcurrentStreams,
			MaxBatchSize:            s.config.MaxBatchSize,
			SupportsStreaming:       true,
			SupportsCompression:     true,
			SupportsLoadBalancing:   true,
		},
		SupportedCompression: []CompressionType{
			CompressionType_COMPRESSION_GZIP,
			CompressionType_COMPRESSION_ZSTD,
			CompressionType_COMPRESSION_LZ4,
			CompressionType_COMPRESSION_SNAPPY,
		},
		RateLimits: &RateLimitInfo{
			GlobalLimit:       165000,
			PerCollectorLimit: s.config.DefaultEventsPerSec,
			BurstLimit:        s.config.DefaultEventsPerSec * 2,
			Window:            durationpb.New(time.Minute),
		},
	}

	return response, nil
}

// Background monitoring and management functions

func (s *Server) monitorConnections() {
	defer s.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.connMgr.CleanupIdleConnections()
			s.metrics.UpdateConnectionStats(s.connMgr.GetStats())
		}
	}
}

func (s *Server) monitorMemoryUsage() {
	defer s.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			memUsage := s.getMemoryUsage()
			s.metrics.UpdateMemoryUsage(memUsage)

			if memUsage > s.config.MaxMemoryUsage {
				s.flowController.TriggerBackpressure("high_memory_usage")
			}
		}
	}
}

func (s *Server) updateFlowControl() {
	defer s.wg.Done()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.flowController.UpdateGlobalState(s.metrics)
		}
	}
}

func (s *Server) getServerStatus() *ServerStatus {
	stats := s.metrics.GetStats()

	return &ServerStatus{
		Load:                float32(stats.Load),
		AvailableCapacity:   stats.AvailableCapacity,
		ConnectedCollectors: uint32(stats.ActiveConnections),
		ConfigVersion:       "1.0",
		RateLimits: &RateLimitInfo{
			GlobalLimit:       165000,
			PerCollectorLimit: s.config.DefaultEventsPerSec,
		},
	}
}

func (s *Server) getMemoryUsage() uint64 {
	// Implementation would use runtime.ReadMemStats or similar
	// For now, return a placeholder
	return 0
}
