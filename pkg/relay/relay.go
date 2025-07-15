package relay

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/api"
	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/yairfalse/tapio/pkg/output"
	"github.com/yairfalse/tapio/pkg/resilience"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Relay implements the core relay service
// Zero-config, high-performance event aggregation and routing
type Relay struct {
	api.UnimplementedCollectorServiceServer // gRPC server for collectors
	
	// Core components
	processor   EventProcessor
	router      RoutingPolicy
	aggregator  AggregationStrategy
	buffer      BufferManager
	resilience  ResilienceManager
	
	// Export pipelines
	otelExporter   *OTELExporter
	engineClient   api.CollectorServiceClient
	exportManager  *ExportManager
	
	// Observability
	logger *zap.Logger
	tracer trace.Tracer
	stats  atomic.Value // RelayStats
	
	// Configuration
	config *Config
	
	// Lifecycle
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	running    atomic.Bool
	
	// Performance optimization
	eventPool  *sync.Pool
	batchPool  *sync.Pool
}

// Config holds relay configuration
type Config struct {
	// Server settings
	GRPCPort int
	
	// Engine connection
	EngineEndpoint string
	
	// OTEL export
	OTELEnabled  bool
	OTELEndpoint string
	
	// Performance tuning
	BufferSize          int
	BatchSize           int
	FlushInterval       time.Duration
	AggregationWindow   time.Duration
	
	// Resilience
	CircuitBreakerThreshold float64
	RetryAttempts           int
	RetryDelay              time.Duration
}

// DefaultConfig returns production-ready configuration
func DefaultConfig() *Config {
	return &Config{
		GRPCPort:                9095, // Different from engine (9090)
		EngineEndpoint:          "localhost:9090",
		OTELEnabled:             true,
		OTELEndpoint:            "localhost:4317",
		BufferSize:              100000, // Handle burst traffic
		BatchSize:               1000,
		FlushInterval:           1 * time.Second,
		AggregationWindow:       5 * time.Second,
		CircuitBreakerThreshold: 0.5,
		RetryAttempts:           3,
		RetryDelay:              100 * time.Millisecond,
	}
}

// NewRelay creates a new relay instance
func NewRelay(config *Config, logger *zap.Logger) (*Relay, error) {
	if config == nil {
		config = DefaultConfig()
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	relay := &Relay{
		config: config,
		logger: logger,
		tracer: otel.Tracer("tapio-relay"),
		ctx:    ctx,
		cancel: cancel,
		
		// Object pools for zero-allocation
		eventPool: &sync.Pool{
			New: func() interface{} {
				return &api.Event{}
			},
		},
		batchPool: &sync.Pool{
			New: func() interface{} {
				return make([]*api.Event, 0, config.BatchSize)
			},
		},
	}
	
	// Initialize components
	relay.buffer = NewRingBuffer(config.BufferSize)
	relay.resilience = resilience.NewManager(resilience.Config{
		CircuitBreakerThreshold: config.CircuitBreakerThreshold,
		RetryAttempts:          config.RetryAttempts,
		RetryDelay:            config.RetryDelay,
	})
	relay.aggregator = NewTimeWindowAggregator(config.AggregationWindow)
	relay.router = NewSmartRouter()
	
	// Initialize stats
	relay.stats.Store(&RelayStats{})
	
	return relay, nil
}

// Start begins relay operations
func (r *Relay) Start(ctx context.Context) error {
	if !r.running.CompareAndSwap(false, true) {
		return fmt.Errorf("relay already running")
	}
	
	r.logger.Info("Starting Tapio Relay",
		zap.Int("grpc_port", r.config.GRPCPort),
		zap.String("engine", r.config.EngineEndpoint),
		zap.Bool("otel_enabled", r.config.OTELEnabled),
	)
	
	// Connect to engine
	if err := r.connectToEngine(); err != nil {
		return fmt.Errorf("failed to connect to engine: %w", err)
	}
	
	// Initialize OTEL export if enabled
	if r.config.OTELEnabled {
		otelExporter, err := NewOTELExporter(r.config.OTELEndpoint)
		if err != nil {
			return fmt.Errorf("failed to create OTEL exporter: %w", err)
		}
		r.otelExporter = otelExporter
	}
	
	// Start gRPC server for collectors
	if err := r.startGRPCServer(); err != nil {
		return fmt.Errorf("failed to start gRPC server: %w", err)
	}
	
	// Start background workers
	r.wg.Add(3)
	go r.processLoop()
	go r.aggregationLoop()
	go r.statsLoop()
	
	r.logger.Info("Tapio Relay started successfully")
	return nil
}

// Stop gracefully shuts down the relay
func (r *Relay) Stop() error {
	if !r.running.CompareAndSwap(true, false) {
		return fmt.Errorf("relay not running")
	}
	
	r.logger.Info("Stopping Tapio Relay")
	
	// Cancel context to stop workers
	r.cancel()
	
	// Wait for workers with timeout
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	
	select {
	case <-done:
		r.logger.Info("All workers stopped")
	case <-time.After(10 * time.Second):
		r.logger.Warn("Timeout waiting for workers")
	}
	
	// Flush any remaining data
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := r.flush(ctx); err != nil {
		r.logger.Error("Error during final flush", zap.Error(err))
	}
	
	// Close connections
	if r.otelExporter != nil {
		r.otelExporter.Close()
	}
	
	r.logger.Info("Tapio Relay stopped")
	return nil
}

// StreamEvents implements the gRPC collector service
func (r *Relay) StreamEvents(stream api.CollectorService_StreamEventsServer) error {
	ctx := stream.Context()
	collectorID := "unknown" // Extract from metadata in real implementation
	
	r.logger.Info("New collector connected", zap.String("collector", collectorID))
	
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			event, err := stream.Recv()
			if err != nil {
				r.logger.Error("Error receiving event", zap.Error(err))
				return err
			}
			
			// Add to buffer with backpressure
			if err := r.addEvent(event); err != nil {
				r.logger.Warn("Event dropped due to backpressure",
					zap.String("type", event.Type),
					zap.Error(err),
				)
				r.updateStats(func(s *RelayStats) {
					s.EventsDropped++
				})
				
				// Send flow control response
				if err := stream.Send(&api.EventResponse{
					Status:  api.EventResponse_BACKPRESSURE,
					Message: "Relay buffer full, apply backpressure",
				}); err != nil {
					return err
				}
			} else {
				// Acknowledge successful receipt
				if err := stream.Send(&api.EventResponse{
					Status:  api.EventResponse_OK,
					EventId: event.Id,
				}); err != nil {
					return err
				}
			}
		}
	}
}

// addEvent adds an event to the buffer
func (r *Relay) addEvent(event *api.Event) error {
	// Update stats
	r.updateStats(func(s *RelayStats) {
		s.EventsReceived++
		s.LastEventTime = time.Now()
	})
	
	// Check circuit breaker
	if !r.resilience.IsHealthy("buffer_add") {
		return fmt.Errorf("circuit breaker open")
	}
	
	// Add to buffer
	if err := r.buffer.Add(event); err != nil {
		r.resilience.RecordFailure("buffer_add", err)
		return err
	}
	
	r.resilience.RecordSuccess("buffer_add")
	return nil
}

// processLoop continuously processes buffered events
func (r *Relay) processLoop() {
	defer r.wg.Done()
	
	ticker := time.NewTicker(r.config.FlushInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			r.processBatch()
		}
	}
}

// processBatch processes a batch of events
func (r *Relay) processBatch() {
	// Drain events from buffer
	events := r.buffer.Drain(r.config.BatchSize)
	if len(events) == 0 {
		return
	}
	
	ctx, span := r.tracer.Start(r.ctx, "relay.processBatch",
		trace.WithAttributes(
			trace.Int("batch_size", len(events)),
		),
	)
	defer span.End()
	
	// Route events to destinations
	eventsByDest := make(map[DestinationType][]*api.Event)
	for _, event := range events {
		destinations := r.router.Route(event)
		for _, dest := range destinations {
			eventsByDest[dest.Type] = append(eventsByDest[dest.Type], event)
		}
	}
	
	// Send to each destination
	var wg sync.WaitGroup
	for destType, destEvents := range eventsByDest {
		wg.Add(1)
		go func(dt DestinationType, events []*api.Event) {
			defer wg.Done()
			
			switch dt {
			case DestinationEngine:
				r.sendToEngine(ctx, events)
			case DestinationOTEL:
				r.sendToOTEL(ctx, events)
			}
		}(destType, destEvents)
	}
	
	wg.Wait()
	
	// Update stats
	r.updateStats(func(s *RelayStats) {
		s.EventsProcessed += int64(len(events))
		s.BatchesReceived++
	})
}

// sendToEngine forwards events to the correlation engine
func (r *Relay) sendToEngine(ctx context.Context, events []*api.Event) {
	if r.engineClient == nil {
		r.logger.Error("Engine client not initialized")
		return
	}
	
	// Create stream to engine
	stream, err := r.engineClient.StreamEvents(ctx)
	if err != nil {
		r.logger.Error("Failed to create engine stream", zap.Error(err))
		r.updateStats(func(s *RelayStats) {
			s.ExportsFailed++
		})
		return
	}
	defer stream.CloseSend()
	
	// Send events
	for _, event := range events {
		if err := stream.Send(event); err != nil {
			r.logger.Error("Failed to send event to engine", zap.Error(err))
			r.updateStats(func(s *RelayStats) {
				s.ExportsFailed++
			})
			return
		}
	}
	
	r.updateStats(func(s *RelayStats) {
		s.ExportsSuccess++
	})
}

// sendToOTEL exports events as OTEL traces
func (r *Relay) sendToOTEL(ctx context.Context, events []*api.Event) {
	if r.otelExporter == nil {
		return
	}
	
	// Convert events to OTEL spans
	spans := r.otelExporter.ConvertEventsToSpans(ctx, events)
	
	// Export spans
	if err := r.otelExporter.Export(ctx, spans); err != nil {
		r.logger.Error("Failed to export OTEL spans", zap.Error(err))
		r.updateStats(func(s *RelayStats) {
			s.ExportsFailed++
		})
		return
	}
	
	r.updateStats(func(s *RelayStats) {
		s.ExportsSuccess++
	})
}

// aggregationLoop handles event aggregation
func (r *Relay) aggregationLoop() {
	defer r.wg.Done()
	
	ticker := time.NewTicker(r.config.AggregationWindow)
	defer ticker.Stop()
	
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			// Aggregation logic here
			// For now, just update stats
			r.updateStats(func(s *RelayStats) {
				s.BufferUtilization = float64(r.buffer.Size()) / float64(r.config.BufferSize)
			})
		}
	}
}

// statsLoop updates statistics
func (r *Relay) statsLoop() {
	defer r.wg.Done()
	
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	
	startTime := time.Now()
	
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			r.updateStats(func(s *RelayStats) {
				s.UptimeSeconds = int64(time.Since(startTime).Seconds())
			})
			
			// Log stats periodically
			stats := r.GetStats()
			r.logger.Info("Relay statistics",
				zap.Int64("events_received", stats.EventsReceived),
				zap.Int64("events_processed", stats.EventsProcessed),
				zap.Int64("events_dropped", stats.EventsDropped),
				zap.Float64("buffer_utilization", stats.BufferUtilization),
			)
		}
	}
}

// GetStats returns current relay statistics
func (r *Relay) GetStats() RelayStats {
	stats := r.stats.Load().(*RelayStats)
	return *stats
}

// updateStats safely updates statistics
func (r *Relay) updateStats(fn func(*RelayStats)) {
	stats := r.stats.Load().(*RelayStats)
	newStats := *stats
	fn(&newStats)
	r.stats.Store(&newStats)
}

// connectToEngine establishes connection to correlation engine
func (r *Relay) connectToEngine() error {
	conn, err := grpc.Dial(r.config.EngineEndpoint,
		grpc.WithInsecure(),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(16 * 1024 * 1024), // 16MB
			grpc.MaxCallSendMsgSize(16 * 1024 * 1024),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to connect to engine: %w", err)
	}
	
	r.engineClient = api.NewCollectorServiceClient(conn)
	return nil
}

// startGRPCServer starts the gRPC server for collectors
func (r *Relay) startGRPCServer() error {
	// Implementation would start gRPC server
	// Simplified for brevity
	return nil
}

// flush processes any remaining events
func (r *Relay) flush(ctx context.Context) error {
	events := r.buffer.Drain(r.config.BufferSize)
	if len(events) == 0 {
		return nil
	}
	
	// Process remaining events
	r.sendToEngine(ctx, events)
	if r.otelExporter != nil {
		r.sendToOTEL(ctx, events)
	}
	
	return nil
}