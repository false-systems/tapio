//go:build linux

package otel

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// Collector implements OTLP receiver for Linux
type Collector struct {
	name   string
	config *Config
	logger *zap.Logger

	// Lifecycle
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	healthy atomic.Bool
	mu      sync.RWMutex

	// Event channel
	events chan *domain.CollectorEvent

	// OTLP servers
	grpcServer   *grpc.Server
	grpcListener net.Listener
	httpListener net.Listener

	// Service dependency tracking
	serviceDeps     map[string]map[string]int64 // service -> service -> count
	serviceDepsLock sync.RWMutex
	lastDepsEmit    time.Time

	// Statistics
	stats *Stats

	// OpenTelemetry instrumentation
	tracer          trace.Tracer
	spansReceived   metric.Int64Counter
	metricsReceived metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
}

// Stats holds collector statistics
type Stats struct {
	SpansReceived   uint64
	MetricsReceived uint64
	EventsEmitted   uint64
	SpansDropped    uint64
	ErrorCount      uint64
	LastEventTime   time.Time
}

// Interface verification
var _ collectors.Collector = (*Collector)(nil)

// NewCollector creates a new OTEL collector for Linux
func NewCollector(name string, config *Config) (*Collector, error) {
	if config == nil {
		config = DefaultConfig()
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize OTEL instrumentation
	tracer := otel.Tracer(name)
	meter := otel.Meter(name)

	// Create metrics
	spansReceived, err := meter.Int64Counter(
		fmt.Sprintf("%s_spans_received_total", name),
		metric.WithDescription("Total OTEL spans received"),
	)
	if err != nil {
		logger.Warn("Failed to create spans counter", zap.Error(err))
	}

	metricsReceived, err := meter.Int64Counter(
		fmt.Sprintf("%s_metrics_received_total", name),
		metric.WithDescription("Total OTEL metrics received"),
	)
	if err != nil {
		logger.Warn("Failed to create metrics counter", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription("Total errors in OTEL collector"),
	)
	if err != nil {
		logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	processingTime, err := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_duration_ms", name),
		metric.WithDescription("OTEL event processing duration in milliseconds"),
	)
	if err != nil {
		logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	return &Collector{
		name:            name,
		config:          config,
		logger:          logger,
		events:          make(chan *domain.CollectorEvent, config.BufferSize),
		serviceDeps:     make(map[string]map[string]int64),
		stats:           &Stats{},
		tracer:          tracer,
		spansReceived:   spansReceived,
		metricsReceived: metricsReceived,
		errorsTotal:     errorsTotal,
		processingTime:  processingTime,
	}, nil
}

// Name returns the collector name
func (c *Collector) Name() string {
	return c.name
}

// Start begins OTLP collection
func (c *Collector) Start(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "otel.collector.start")
	defer span.End()

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Start OTLP gRPC server
	if c.config.GRPCEndpoint != "" {
		var err error
		c.grpcListener, err = net.Listen("tcp", c.config.GRPCEndpoint)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to listen on gRPC endpoint %s: %w", c.config.GRPCEndpoint, err)
		}

		c.grpcServer = grpc.NewServer()
		// OTLP trace service registration will be added when implementing full OTLP protocol

		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			if err := c.grpcServer.Serve(c.grpcListener); err != nil {
				c.logger.Error("gRPC server error", zap.Error(err))
			}
		}()
	}

	// Start service dependency emitter
	if c.config.EnableDependencies {
		c.wg.Add(1)
		go c.emitServiceDependencies()
	}

	// Start event processor (stub for now)
	c.wg.Add(1)
	go c.processEvents()

	c.healthy.Store(true)
	c.lastDepsEmit = time.Now()

	c.logger.Info("OTEL collector started",
		zap.String("name", c.name),
		zap.String("grpc_endpoint", c.config.GRPCEndpoint),
		zap.String("http_endpoint", c.config.HTTPEndpoint),
		zap.Bool("dependencies", c.config.EnableDependencies),
		zap.Float64("sampling_rate", c.config.SamplingRate),
	)

	return nil
}

// Stop gracefully shuts down the collector
func (c *Collector) Stop() error {
	c.logger.Info("Stopping OTEL collector", zap.String("name", c.name))

	if c.cancel != nil {
		c.cancel()
	}

	// Stop gRPC server
	if c.grpcServer != nil {
		c.grpcServer.GracefulStop()
	}

	// Wait for goroutines
	c.wg.Wait()

	// Close event channel
	close(c.events)

	c.healthy.Store(false)

	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	return c.healthy.Load()
}

// processEvents stub processor
func (c *Collector) processEvents() {
	defer c.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			// Emit test span for validation
			// Real OTLP processing will replace this when protocol is implemented
			c.emitTestSpan()
		}
	}
}

// emitTestSpan emits a test span event (temporary for testing)
func (c *Collector) emitTestSpan() {
	now := time.Now()

	event := &domain.CollectorEvent{
		EventID:   fmt.Sprintf("otel-span-%d", now.UnixNano()),
		Timestamp: now,
		Source:    c.name,
		Type:      domain.EventTypeOTELSpan,
		Severity:  domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			OTELSpan: &domain.OTELSpanData{
				TraceID:       "test-trace-" + fmt.Sprintf("%d", now.Unix()),
				SpanID:        "test-span-" + fmt.Sprintf("%d", now.UnixNano()),
				Name:          "GET /api/test",
				ServiceName:   "test-service",
				Kind:          "SERVER",
				StartTime:     now.Add(-100 * time.Millisecond),
				EndTime:       now,
				DurationNanos: 100000000, // 100ms
				StatusCode:    "OK",
				HTTPMethod:    "GET",
				HTTPURL:       "/api/test",
				K8sPodName:    "test-pod-123",
				K8sNamespace:  "default",
			},
		},
		Metadata: domain.EventMetadata{
			TraceID:      "test-trace-" + fmt.Sprintf("%d", now.Unix()),
			SpanID:       "test-span-" + fmt.Sprintf("%d", now.UnixNano()),
			PodName:      "test-pod-123",
			PodNamespace: "default",
		},
	}

	select {
	case c.events <- event:
		atomic.AddUint64(&c.stats.EventsEmitted, 1)
		c.stats.LastEventTime = now
	case <-c.ctx.Done():
		return
	default:
		atomic.AddUint64(&c.stats.SpansDropped, 1)
	}
}

// emitServiceDependencies periodically emits service dependency events
func (c *Collector) emitServiceDependencies() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.ServiceMapInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.emitDependencyEvents()
		}
	}
}

// emitDependencyEvents emits service dependency events
func (c *Collector) emitDependencyEvents() {
	c.serviceDepsLock.RLock()
	defer c.serviceDepsLock.RUnlock()

	now := time.Now()

	for fromService, toServices := range c.serviceDeps {
		for toService, count := range toServices {
			event := &domain.CollectorEvent{
				EventID:   fmt.Sprintf("otel-dep-%s-%s-%d", fromService, toService, now.UnixNano()),
				Timestamp: now,
				Source:    c.name,
				Type:      domain.EventTypeOTELMetric,
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					Custom: map[string]string{
						"metric_type":  "service_dependency",
						"from_service": fromService,
						"to_service":   toService,
						"call_count":   fmt.Sprintf("%d", count),
						"window":       c.config.ServiceMapInterval.String(),
					},
				},
			}

			select {
			case c.events <- event:
				// Successfully sent
			case <-c.ctx.Done():
				return
			default:
				// Buffer full, skip
			}
		}
	}

	// Clear the map for next interval
	c.serviceDepsLock.Lock()
	c.serviceDeps = make(map[string]map[string]int64)
	c.serviceDepsLock.Unlock()
}

// recordServiceDependency records a service dependency from a span
func (c *Collector) recordServiceDependency(fromService, toService string) {
	if fromService == "" || toService == "" || fromService == toService {
		return
	}

	c.serviceDepsLock.Lock()
	defer c.serviceDepsLock.Unlock()

	if c.serviceDeps[fromService] == nil {
		c.serviceDeps[fromService] = make(map[string]int64)
	}
	c.serviceDeps[fromService][toService]++
}

// shouldSample determines if a span should be sampled
func (c *Collector) shouldSample(span *domain.OTELSpanData) bool {
	// Always sample errors
	if c.config.AlwaysSampleErrors && span.StatusCode == "ERROR" {
		return true
	}

	// Simple probabilistic sampling
	// In production, use a proper hash of trace ID for consistent sampling
	return c.config.SamplingRate >= 1.0 || (time.Now().UnixNano()%100) < int64(c.config.SamplingRate*100)
}
