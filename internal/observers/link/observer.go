package link

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/internal/observers/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Observer tracks network link failures (lean negative observer)
type Observer struct {
	*base.BaseObserver        // Provides Statistics() and Health()
	*base.EventChannelManager // Handles event channels
	*base.LifecycleManager    // Manages goroutines

	name   string
	config *Config
	logger *zap.Logger

	// eBPF state (platform-specific)
	ebpfState interface{}

	// Simple failure tracking
	mu       sync.RWMutex
	failures map[string]*FailureStats // Key: "src_ip:dst_ip"

	// OpenTelemetry instrumentation
	tracer          trace.Tracer
	linkFailures    metric.Int64Counter
	synTimeouts     metric.Int64Counter
	connectionRSTs  metric.Int64Counter
	eventsProcessed metric.Int64Counter
	eventsDropped   metric.Int64Counter
	processingTime  metric.Float64Histogram
}

// NewObserver creates a new link observer
func NewObserver(name string, config *Config, logger *zap.Logger) (*Observer, error) {
	if config == nil {
		config = DefaultConfig()
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	if logger == nil {
		logger = zap.NewNop()
	}
	logger = logger.Named("link")

	ctx := context.Background()

	o := &Observer{
		BaseObserver:        base.NewBaseObserver(name, 30*time.Second),
		EventChannelManager: base.NewEventChannelManager(config.BufferSize, name, logger),
		LifecycleManager:    base.NewLifecycleManager(ctx, logger),
		name:                name,
		config:              config,
		logger:              logger,
		failures:            make(map[string]*FailureStats),
	}

	if err := o.setupOTEL(); err != nil {
		return nil, fmt.Errorf("setting up OTEL: %w", err)
	}

	o.logger.Info("Link observer created", zap.String("name", name))
	return o, nil
}

// setupOTEL sets up OpenTelemetry instrumentation
func (o *Observer) setupOTEL() error {
	o.tracer = otel.Tracer("tapio.observers.link")
	meter := otel.GetMeterProvider().Meter("tapio.observers.link")

	var err error

	o.linkFailures, err = meter.Int64Counter(
		"link_failures_total",
		metric.WithDescription("Total link failures detected"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return err
	}

	o.synTimeouts, err = meter.Int64Counter(
		"link_syn_timeouts_total",
		metric.WithDescription("TCP SYN timeouts"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return err
	}

	o.connectionRSTs, err = meter.Int64Counter(
		"link_connection_resets_total",
		metric.WithDescription("TCP connection resets"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return err
	}

	o.eventsProcessed, err = meter.Int64Counter(
		"link_events_processed_total",
		metric.WithDescription("Total events processed"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return err
	}

	o.eventsDropped, err = meter.Int64Counter(
		"link_events_dropped_total",
		metric.WithDescription("Total events dropped"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return err
	}

	o.processingTime, err = meter.Float64Histogram(
		"link_processing_duration_ms",
		metric.WithDescription("Event processing duration"),
		metric.WithUnit("ms"),
	)

	return err
}

// Name returns the observer name
func (o *Observer) Name() string {
	return o.name
}

// Start starts the observer
func (o *Observer) Start(ctx context.Context) error {
	o.logger.Info("Starting link observer")

	ctx, span := o.tracer.Start(ctx, "link.start")
	defer span.End()

	// Start eBPF if available (Linux only)
	if err := o.startEBPF(); err != nil {
		o.logger.Warn("Failed to start eBPF, running in limited mode", zap.Error(err))
		// Continue without eBPF
	}

	// Start aggregation worker
	o.LifecycleManager.Start("failure-aggregator", func() {
		o.aggregateFailures(ctx)
	})

	o.SetHealthy(true)
	o.logger.Info("Link observer started")

	return nil
}

// Stop stops the observer
func (o *Observer) Stop() error {
	o.logger.Info("Stopping link observer")

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
	o.logger.Info("Link observer stopped")

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

// Health returns detailed health status
func (o *Observer) Health() *domain.HealthStatus {
	return o.BaseObserver.Health()
}

// IsHealthy returns observer health status
func (o *Observer) IsHealthy() bool {
	return o.BaseObserver.IsHealthy()
}

// trackFailure records a failure (lean correlation)
func (o *Observer) trackFailure(srcIP, dstIP string, eventType uint8) {
	key := fmt.Sprintf("%s:%s", srcIP, dstIP)

	o.mu.Lock()
	defer o.mu.Unlock()

	stats, exists := o.failures[key]
	if !exists {
		stats = &FailureStats{}
		o.failures[key] = stats
	}

	// Simple increment based on type
	switch eventType {
	case EventSYNTimeout:
		stats.SYNTimeouts++
	case EventConnectionRST:
		stats.ConnectionRSTs++
	case EventARPTimeout:
		stats.ARPTimeouts++
	}
	stats.LastSeen = time.Now()

	// Clean old entries (simple TTL)
	if len(o.failures) > 1000 {
		cutoff := time.Now().Add(-5 * time.Minute)
		for k, v := range o.failures {
			if v.LastSeen.Before(cutoff) {
				delete(o.failures, k)
			}
		}
	}
}

// aggregateFailures periodically checks for failure patterns
func (o *Observer) aggregateFailures(ctx context.Context) {
	ticker := time.NewTicker(o.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-o.LifecycleManager.StopChannel():
			return
		case <-ticker.C:
			o.checkFailurePatterns()
		}
	}
}

// checkFailurePatterns detects problematic links (simple detection)
func (o *Observer) checkFailurePatterns() {
	o.mu.RLock()
	defer o.mu.RUnlock()

	for key, stats := range o.failures {
		// Simple threshold detection
		if stats.SYNTimeouts > 5 {
			o.logger.Warn("Multiple SYN timeouts detected",
				zap.String("link", key),
				zap.Uint64("count", stats.SYNTimeouts),
			)
		}
		if stats.ConnectionRSTs > 10 {
			o.logger.Warn("High connection reset rate",
				zap.String("link", key),
				zap.Uint64("count", stats.ConnectionRSTs),
			)
		}
	}
}

// formatIP converts IP from uint32 to string
func formatIP(ip uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		byte(ip), byte(ip>>8), byte(ip>>16), byte(ip>>24))
}
