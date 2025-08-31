package dns

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Config for DNS collector focused on operational monitoring
type Config struct {
	Name                  string               `json:"name"`
	BufferSize            int                  `json:"buffer_size"`
	EnableEBPF            bool                 `json:"enable_ebpf"`
	XDPInterfaces         []string             `json:"xdp_interfaces,omitempty"` // Specific interfaces for XDP
	CircuitBreakerConfig  CircuitBreakerConfig `json:"circuit_breaker_config"`
	ContainerIDExtraction bool                 `json:"container_id_extraction"` // Enable container ID parsing
	ParseAnswers          bool                 `json:"parse_answers"`           // Parse DNS answers for resolved IPs
	Labels                map[string]string    `json:"labels,omitempty"`        // Labels to add to all events
}

// DefaultConfig returns sensible defaults for operational monitoring
func DefaultConfig() Config {
	return Config{
		Name:                  "dns",
		BufferSize:            10000,
		EnableEBPF:            true,
		XDPInterfaces:         nil, // Auto-detect if nil
		CircuitBreakerConfig:  DefaultCircuitBreakerConfig(),
		ContainerIDExtraction: true,
		ParseAnswers:          true,
	}
}

// Collector implements DNS monitoring with cross-platform support
type Collector struct {
	// Core
	name    string
	logger  *zap.Logger
	config  Config
	ctx     context.Context
	cancel  context.CancelFunc
	healthy bool
	mu      sync.RWMutex

	// Statistics
	stats *DNSStats

	// eBPF components (platform-specific via interface{})
	ebpfState interface{}

	// Mock mode
	mockMode bool

	// Error handling
	consecutiveErrors int
	errorLogInterval  time.Time

	// Event processing
	events chan *domain.CollectorEvent

	// Fault tolerance
	circuitBreaker *CircuitBreaker

	// Container tracking
	containerCache map[uint64]string // cgroup_id -> container_id
	cacheMutex     sync.RWMutex      // Separate mutex for cache

	// OpenTelemetry metrics - focused on operational metrics
	tracer             trace.Tracer
	eventsProcessed    metric.Int64Counter
	errorsTotal        metric.Int64Counter
	processingTime     metric.Float64Histogram
	bufferUsage        metric.Int64Gauge
	droppedEvents      metric.Int64Counter
	dnsLatency         metric.Float64Histogram
	dnsFailures        metric.Int64Counter
	circuitBreakerHits metric.Int64Counter
}

// NewCollector creates a new DNS collector
func NewCollector(name string, cfg Config) (*Collector, error) {
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize OTEL components
	tracer := otel.Tracer("dns-collector")
	meter := otel.Meter("dns-collector")

	// Create metrics
	eventsProcessed, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription("Total DNS events processed"),
	)
	if err != nil {
		logger.Warn("Failed to create events processed counter", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription("Total DNS processing errors"),
	)
	if err != nil {
		logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	processingTime, err := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_duration_ms", name),
		metric.WithDescription("DNS event processing duration"),
	)
	if err != nil {
		logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	bufferUsage, err := meter.Int64Gauge(
		fmt.Sprintf("%s_buffer_usage", name),
		metric.WithDescription("DNS collector buffer usage"),
	)
	if err != nil {
		logger.Warn("Failed to create buffer usage gauge", zap.Error(err))
	}

	droppedEvents, err := meter.Int64Counter(
		fmt.Sprintf("%s_dropped_events_total", name),
		metric.WithDescription("Total DNS events dropped"),
	)
	if err != nil {
		logger.Warn("Failed to create dropped events counter", zap.Error(err))
	}

	dnsLatency, err := meter.Float64Histogram(
		fmt.Sprintf("%s_dns_latency_ms", name),
		metric.WithDescription("DNS query latency"),
	)
	if err != nil {
		logger.Warn("Failed to create DNS latency histogram", zap.Error(err))
	}

	dnsFailures, err := meter.Int64Counter(
		fmt.Sprintf("%s_dns_failures_total", name),
		metric.WithDescription("Total DNS query failures"),
	)
	if err != nil {
		logger.Warn("Failed to create DNS failures counter", zap.Error(err))
	}

	circuitBreakerHits, err := meter.Int64Counter(
		fmt.Sprintf("%s_circuit_breaker_hits_total", name),
		metric.WithDescription("Circuit breaker activations"),
	)
	if err != nil {
		logger.Warn("Failed to create circuit breaker hits counter", zap.Error(err))
	}

	// Initialize circuit breaker
	circuitBreaker := NewCircuitBreaker(cfg.CircuitBreakerConfig, logger)

	collector := &Collector{
		name:               name,
		logger:             logger.Named(name),
		config:             cfg,
		healthy:            true,
		stats:              &DNSStats{},
		events:             make(chan *domain.CollectorEvent, cfg.BufferSize),
		circuitBreaker:     circuitBreaker,
		containerCache:     make(map[uint64]string),
		tracer:             tracer,
		eventsProcessed:    eventsProcessed,
		errorsTotal:        errorsTotal,
		processingTime:     processingTime,
		bufferUsage:        bufferUsage,
		droppedEvents:      droppedEvents,
		dnsLatency:         dnsLatency,
		dnsFailures:        dnsFailures,
		circuitBreakerHits: circuitBreakerHits,
	}

	collector.logger.Info("DNS collector created",
		zap.String("name", name),
		zap.Int("buffer_size", cfg.BufferSize),
		zap.Bool("enable_ebpf", cfg.EnableEBPF),
	)

	return collector, nil
}

// Name returns the collector name
func (c *Collector) Name() string {
	return c.name
}

// Start starts the DNS collector
func (c *Collector) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ctx != nil {
		return fmt.Errorf("collector already started")
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Start eBPF monitoring (platform-specific)
	if c.config.EnableEBPF {
		if err := c.startEBPF(); err != nil {
			c.cancel()
			c.ctx = nil
			c.cancel = nil
			return fmt.Errorf("failed to start eBPF: %w", err)
		}

		// Start event processing
		go c.readEBPFEvents()
	} else {
		c.logger.Info("eBPF disabled, DNS collector running in limited mode")
	}

	c.healthy = true
	c.logger.Info("DNS collector started", zap.String("name", c.name))
	return nil
}

// Stop stops the DNS collector
func (c *Collector) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ctx == nil {
		return nil // Already stopped
	}

	c.logger.Info("Stopping DNS collector")

	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}

	// Stop eBPF (platform-specific)
	c.stopEBPF()

	if c.events != nil {
		close(c.events)
		c.events = nil
	}

	c.ctx = nil
	c.healthy = false

	c.logger.Info("DNS collector stopped")
	return nil
}

// Events returns the events channel
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.events
}

// IsHealthy returns the health status
func (c *Collector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}

// Platform-specific methods implemented in collector_ebpf.go (Linux) and collector_fallback.go (others)
// These are the methods that each platform must implement - they are defined here as
// declarations and implemented in the platform-specific files