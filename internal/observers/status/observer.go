package status

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/internal/observers/base"
	"github.com/yairfalse/tapio/internal/observers/common"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Observer monitors L7 status codes and error patterns
type Observer struct {
	*base.BaseObserver
	*base.EventChannelManager
	*base.LifecycleManager

	name   string
	config *Config
	logger *zap.Logger

	// eBPF state (platform-specific)
	ebpfState interface{}

	// Hash decoder for service/endpoint names
	hashDecoder *HashDecoder

	// Status aggregator for error patterns
	aggregator *StatusAggregator

	// OTEL instrumentation
	tracer          trace.Tracer
	eventsProcessed metric.Int64Counter
	eventsDropped   metric.Int64Counter
	processingTime  metric.Float64Histogram
	httpErrors      metric.Int64Counter
	grpcErrors      metric.Int64Counter
	timeouts        metric.Int64Counter
	latency         metric.Float64Histogram
	errorRate       metric.Float64ObservableGauge

	// Error rate tracking
	mu         sync.RWMutex
	errorRates map[uint32]float64
}

// Config defines status observer configuration
type Config struct {
	Enabled         bool          `yaml:"enabled"`
	BufferSize      int           `yaml:"buffer_size"`
	SampleRate      float64       `yaml:"sample_rate"`
	MaxEventsPerSec int           `yaml:"max_events_per_sec"`
	FlushInterval   time.Duration `yaml:"flush_interval"`
	EnableL7Parse   bool          `yaml:"enable_l7_parse"`
	HTTPPorts       []int         `yaml:"http_ports"`
	GRPCPorts       []int         `yaml:"grpc_ports"`
	Logger          *zap.Logger
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		Enabled:         true,
		BufferSize:      10000,
		SampleRate:      1.0,
		MaxEventsPerSec: 10000,
		FlushInterval:   30 * time.Second,
		EnableL7Parse:   true,
		HTTPPorts:       []int{80, 8080, 8000, 3000},
		GRPCPorts:       []int{50051, 9090},
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.BufferSize <= 0 {
		return fmt.Errorf("buffer_size must be positive")
	}
	if c.SampleRate < 0 || c.SampleRate > 1 {
		return fmt.Errorf("sample_rate must be between 0 and 1")
	}
	if c.FlushInterval <= 0 {
		return fmt.Errorf("flush_interval must be positive")
	}
	return nil
}

// NewObserver creates a new status observer
func NewObserver(name string, config *Config) (*Observer, error) {
	if config == nil {
		config = DefaultConfig()
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Set up logger
	logger := config.Logger
	if logger == nil {
		logger = zap.NewNop()
	}
	logger = logger.Named("status")

	// Initialize base components
	ctx := context.Background()

	o := &Observer{
		BaseObserver:        base.NewBaseObserver(name, 30*time.Second),
		EventChannelManager: base.NewEventChannelManager(config.BufferSize, name, logger),
		LifecycleManager:    base.NewLifecycleManager(ctx, logger),
		name:                name,
		config:              config,
		logger:              logger,
		hashDecoder:         NewHashDecoder(),
		aggregator:          NewStatusAggregator(config.FlushInterval),
		errorRates:          make(map[uint32]float64),
	}

	// Set up OTEL instrumentation
	if err := o.setupOTEL(); err != nil {
		return nil, fmt.Errorf("setting up OTEL: %w", err)
	}

	o.logger.Info("Status observer created", zap.String("name", name))
	return o, nil
}

// setupOTEL sets up OpenTelemetry instrumentation
func (o *Observer) setupOTEL() error {
	// Get tracer
	o.tracer = otel.Tracer("tapio.observers.status")

	// Get meter
	meter := otel.GetMeterProvider().Meter("tapio.observers.status")

	var err error

	// Events processed counter
	o.eventsProcessed, err = meter.Int64Counter(
		"status_events_processed_total",
		metric.WithDescription("Total status events processed"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return err
	}

	// Events dropped counter
	o.eventsDropped, err = meter.Int64Counter(
		"status_events_dropped_total",
		metric.WithDescription("Total status events dropped"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return err
	}

	// Processing time histogram
	o.processingTime, err = meter.Float64Histogram(
		"status_processing_duration_ms",
		metric.WithDescription("Status event processing duration"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return err
	}

	// HTTP errors counter
	o.httpErrors, err = meter.Int64Counter(
		"status_http_errors_total",
		metric.WithDescription("Count of HTTP errors by status code"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return err
	}

	// gRPC errors counter
	o.grpcErrors, err = meter.Int64Counter(
		"status_grpc_errors_total",
		metric.WithDescription("Count of gRPC errors by status code"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return err
	}

	// Timeouts counter
	o.timeouts, err = meter.Int64Counter(
		"status_timeouts_total",
		metric.WithDescription("Count of connection timeouts"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return err
	}

	// Latency histogram
	o.latency, err = meter.Float64Histogram(
		"status_latency_ms",
		metric.WithDescription("L7 request latency"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return err
	}

	// Error rate gauge
	o.errorRate, err = meter.Float64ObservableGauge(
		"status_error_rate",
		metric.WithDescription("L7 error rate by service"),
		metric.WithUnit("ratio"),
		metric.WithFloat64Callback(o.observeErrorRate),
	)

	return err
}

// observeErrorRate callback for error rate metric
func (o *Observer) observeErrorRate(ctx context.Context, observer metric.Float64Observer) error {
	o.mu.RLock()
	defer o.mu.RUnlock()

	for serviceHash, rate := range o.errorRates {
		serviceName := o.hashDecoder.GetService(serviceHash)
		if serviceName != "" {
			observer.Observe(rate, metric.WithAttributes(
				attribute.String("service", serviceName),
			))
		}
	}

	return nil
}

// Name returns the observer name
func (o *Observer) Name() string {
	return o.name
}

// Start starts the observer
func (o *Observer) Start(ctx context.Context) error {
	o.logger.Info("Starting status observer")

	ctx, span := o.tracer.Start(ctx, "status.start")
	defer span.End()

	// Start eBPF if available (Linux only)
	if err := o.startEBPF(); err != nil {
		o.logger.Warn("Failed to start eBPF, running in limited mode", zap.Error(err))
		// Continue without eBPF - can still provide value through other means
	}

	// Start lifecycle manager
	o.LifecycleManager.Start("pattern-detector", func() {
		o.detectFailurePatterns(ctx)
	})

	o.SetHealthy(true)
	o.logger.Info("Status observer started")

	return nil
}

// Stop stops the observer
func (o *Observer) Stop() error {
	o.logger.Info("Stopping status observer")

	// Stop eBPF if running
	if o.ebpfState != nil {
		if err := o.stopEBPF(); err != nil {
			o.logger.Warn("Error stopping eBPF", zap.Error(err))
		}
	}

	// Stop lifecycle manager
	o.LifecycleManager.Stop(5 * time.Second)

	// Close event channel
	o.EventChannelManager.Close()

	o.SetHealthy(false)
	o.logger.Info("Status observer stopped")

	return nil
}

// Events returns the event channel
func (o *Observer) Events() <-chan *domain.CollectorEvent {
	return o.EventChannelManager.GetChannel()
}

// Statistics returns observer statistics
func (o *Observer) Statistics() *domain.CollectorStats {
	return o.BaseObserver.Statistics()
}

// IsHealthy returns observer health status
func (o *Observer) IsHealthy() bool {
	return o.BaseObserver.IsHealthy()
}

// GetEvents returns the event channel - for common.Observer compatibility
// Deprecated: Use Events() instead
func (o *Observer) GetEvents() <-chan common.ObserverEvent {
	// This would require converting domain.CollectorEvent to common.ObserverEvent
	// For now, return nil as we're using the new architecture
	o.logger.Warn("GetEvents() called on modern observer - use Events() instead")
	return nil
}

// updateErrorRates updates error rate metrics
func (o *Observer) updateErrorRates(aggregates map[uint32]*AggregatedStatus) {
	o.mu.Lock()
	defer o.mu.Unlock()

	for serviceHash, agg := range aggregates {
		if agg.TotalCount > 0 {
			o.errorRates[serviceHash] = float64(agg.ErrorCount) / float64(agg.TotalCount)
		}
	}
}

// detectFailurePatterns detects known failure patterns
func (o *Observer) detectFailurePatterns(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	recentEvents := make([]*StatusEvent, 0, 100)

	for {
		select {
		case <-ctx.Done():
			return
		case <-o.LifecycleManager.StopChannel():
			return
		case <-ticker.C:
			// Check for known patterns
			for _, pattern := range KnownPatterns {
				if pattern.Detector(recentEvents) {
					o.logger.Warn("Failure pattern detected",
						zap.String("pattern", pattern.Name),
						zap.String("description", pattern.Description),
						zap.String("severity", pattern.Severity),
					)

					// Create alert event
					alertEvent := &domain.CollectorEvent{
						EventID:   fmt.Sprintf("pattern-%s-%d", pattern.Name, time.Now().Unix()),
						Timestamp: time.Now(),
						Type:      domain.EventTypeNetworkConnection, // Using network connection type for alerts
						Source:    o.name,
						Severity:  domain.EventSeverityError,
						Metadata: domain.EventMetadata{
							Labels: map[string]string{
								"pattern":     pattern.Name,
								"description": pattern.Description,
								"severity":    pattern.Severity,
							},
						},
					}

					o.EventChannelManager.SendEvent(alertEvent)
				}
			}

			// Keep only recent events (last 5 minutes)
			cutoff := time.Now().Add(-5 * time.Minute).UnixNano()
			filtered := recentEvents[:0]
			for _, e := range recentEvents {
				if e.Timestamp > uint64(cutoff) {
					filtered = append(filtered, e)
				}
			}
			recentEvents = filtered
		}
	}
}
