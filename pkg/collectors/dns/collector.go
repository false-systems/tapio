package dns

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/base"
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
	*base.BaseCollector       // Provides Statistics() and Health()
	*base.EventChannelManager // Handles event channels
	*base.LifecycleManager    // Manages goroutines

	// Core DNS configuration
	config Config
	logger *zap.Logger

	// DNS-specific state
	stats *DNSStats

	// eBPF components (platform-specific via interface{})
	ebpfState interface{}

	// Mock mode
	mockMode bool

	// Error handling
	consecutiveErrors int
	errorLogInterval  time.Time

	// Fault tolerance
	circuitBreaker *CircuitBreaker

	// Container tracking
	containerCache map[uint64]string // cgroup_id -> container_id
	cacheMutex     sync.RWMutex      // Separate mutex for cache

	// DNS-specific OpenTelemetry metrics
	tracer             trace.Tracer
	bufferUsage        metric.Int64Gauge
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

	// Initialize base components
	baseConfig := base.BaseCollectorConfig{
		Name:               name,
		HealthCheckTimeout: 30 * time.Second,
		ErrorRateThreshold: 0.05,
		Logger:             logger,
	}

	baseCollector := base.NewBaseCollectorWithConfig(baseConfig)
	eventManager := base.NewEventChannelManager(cfg.BufferSize, name, logger)
	lifecycle := base.NewLifecycleManager(context.Background(), logger)

	// Initialize DNS-specific OTEL metrics
	tracer := otel.Tracer(name)
	meter := otel.Meter(name)

	bufferUsage, err := meter.Int64Gauge(
		fmt.Sprintf("%s_buffer_usage", name),
		metric.WithDescription("DNS collector buffer usage"),
	)
	if err != nil {
		logger.Warn("Failed to create buffer usage gauge", zap.Error(err))
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
		BaseCollector:       baseCollector,
		EventChannelManager: eventManager,
		LifecycleManager:    lifecycle,
		config:              cfg,
		logger:              logger.Named(name),
		stats:               &DNSStats{},
		circuitBreaker:      circuitBreaker,
		containerCache:      make(map[uint64]string),
		tracer:              tracer,
		bufferUsage:         bufferUsage,
		dnsLatency:          dnsLatency,
		dnsFailures:         dnsFailures,
		circuitBreakerHits:  circuitBreakerHits,
	}

	collector.logger.Info("DNS collector created",
		zap.String("name", name),
		zap.Int("buffer_size", cfg.BufferSize),
		zap.Bool("enable_ebpf", cfg.EnableEBPF),
	)

	return collector, nil
}

// Start starts the DNS collector
func (c *Collector) Start(ctx context.Context) error {
	c.logger.Info("Starting DNS collector")

	// Start eBPF monitoring (platform-specific)
	if c.config.EnableEBPF {
		if err := c.startEBPF(); err != nil {
			return fmt.Errorf("failed to start eBPF: %w", err)
		}

		// Start event processing using lifecycle manager
		c.LifecycleManager.Start("dns-event-processor", func() {
			c.logger.Info("DNS event processor cleanup")
		})

		// Start event processing goroutine
		go func() {
			c.readEBPFEvents()
		}()
	} else {
		c.logger.Info("eBPF disabled, DNS collector running in limited mode")
	}

	c.BaseCollector.SetHealthy(true)
	c.logger.Info("DNS collector started")
	return nil
}

// Stop stops the DNS collector
func (c *Collector) Stop() error {
	c.logger.Info("Stopping DNS collector")

	// Stop lifecycle manager with timeout
	c.LifecycleManager.Stop(30 * time.Second)

	// Stop eBPF (platform-specific)
	c.stopEBPF()

	c.BaseCollector.SetHealthy(false)
	c.logger.Info("DNS collector stopped")
	return nil
}

// Events returns the channel for collector events (required by collectors.Collector interface)
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.EventChannelManager.GetChannel()
}

// Name returns the collector name (required by collectors.Collector interface)
func (c *Collector) Name() string {
	return c.BaseCollector.GetName()
}

// Platform-specific methods implemented in collector_ebpf.go (Linux) and collector_fallback.go (others)
// These are the methods that each platform must implement - they are defined here as
// declarations and implemented in the platform-specific files
