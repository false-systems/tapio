package dns

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/observers/base"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Config for DNS observer focused on operational monitoring
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

// Observer implements DNS monitoring with cross-platform support
type Observer struct {
	*base.BaseObserver        // Provides Statistics() and Health()
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

// NewObserver creates a new DNS observer
func NewObserver(name string, cfg Config) (*Observer, error) {
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize base components
	ctx := context.Background()
	baseObserver := base.NewBaseObserver("dns", 30*time.Second)
	eventManager := base.NewEventChannelManager(cfg.BufferSize, name, logger)
	lifecycle := base.NewLifecycleManager(ctx, logger)

	// Initialize DNS-specific OTEL metrics
	tracer := otel.Tracer(name)
	meter := otel.Meter(name)

	bufferUsage, err := meter.Int64Gauge(
		fmt.Sprintf("%s_buffer_usage", name),
		metric.WithDescription("DNS observer buffer usage"),
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

	observer := &Observer{
		BaseObserver:        baseObserver,
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

	observer.logger.Info("DNS observer created",
		zap.String("name", name),
		zap.Int("buffer_size", cfg.BufferSize),
		zap.Bool("enable_ebpf", cfg.EnableEBPF),
	)

	return observer, nil
}

// Start starts the DNS observer
func (c *Observer) Start(ctx context.Context) error {
	c.logger.Info("Starting DNS observer")

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
		c.logger.Info("eBPF disabled, DNS observer running in limited mode")
	}

	c.BaseObserver.SetHealthy(true)
	c.logger.Info("DNS observer started")
	return nil
}

// Stop stops the DNS observer
func (c *Observer) Stop() error {
	c.logger.Info("Stopping DNS observer")

	// Stop lifecycle manager with timeout
	c.LifecycleManager.Stop(30 * time.Second)

	// Stop eBPF (platform-specific)
	c.stopEBPF()

	c.BaseObserver.SetHealthy(false)
	c.logger.Info("DNS observer stopped")
	return nil
}

// Events returns the channel for observer events (required by observers.Observer interface)
func (c *Observer) Events() <-chan *domain.CollectorEvent {
	return c.EventChannelManager.GetChannel()
}

// Name returns the observer name
func (c *Observer) Name() string {
	return c.config.Name
}

// Statistics delegates to base observer
func (c *Observer) Statistics() *domain.CollectorStats {
	return c.BaseObserver.Statistics()
}

// Health delegates to base observer
func (c *Observer) Health() *domain.HealthStatus {
	return c.BaseObserver.Health()
}

// Platform-specific methods implemented in observer_ebpf.go (Linux) and observer_fallback.go (others)
// These are the methods that each platform must implement - they are defined here as
// declarations and implemented in the platform-specific files
