package network

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/internal/observers/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Observer implements network monitoring (positive observer - tracks normal operations)
type Observer struct {
	*base.BaseObserver        // Embed for stats/health
	*base.EventChannelManager // Embed for events
	*base.LifecycleManager    // Embed for lifecycle

	// Network-specific fields
	config   *Config
	logger   *zap.Logger
	name     string
	l7Parser *L7Parser

	// eBPF state (platform-specific)
	ebpfState interface{}

	// OpenTelemetry instrumentation
	tracer             trace.Tracer
	connectionsTotal   metric.Int64Counter
	bytesTransferred   metric.Int64Counter
	packetsProcessed   metric.Int64Counter
	httpRequests       metric.Int64Counter
	dnsQueries         metric.Int64Counter
	connectionDuration metric.Float64Histogram
	requestLatency     metric.Float64Histogram
	eventsProcessed    metric.Int64Counter
	errorsTotal        metric.Int64Counter
}

// NewObserver creates a new network observer
func NewObserver(name string, config *Config, logger *zap.Logger) (*Observer, error) {
	config, logger, err := initializeObserverDeps(config, logger)
	if err != nil {
		return nil, err
	}

	metrics := createObserverMetrics(name, logger)

	return &Observer{
		BaseObserver:        base.NewBaseObserver(name, 5*time.Minute),
		EventChannelManager: base.NewEventChannelManager(config.BufferSize, name, logger),
		LifecycleManager:    base.NewLifecycleManager(context.Background(), logger),
		config:              config,
		logger:              logger.Named(name),
		name:                name,
		l7Parser:            NewL7Parser(logger, config),
		tracer:              otel.Tracer(name),
		connectionsTotal:    metrics.connectionsTotal,
		bytesTransferred:    metrics.bytesTransferred,
		packetsProcessed:    metrics.packetsProcessed,
		httpRequests:        metrics.httpRequests,
		dnsQueries:          metrics.dnsQueries,
		connectionDuration:  metrics.connectionDuration,
		requestLatency:      metrics.requestLatency,
		eventsProcessed:     metrics.eventsProcessed,
		errorsTotal:         metrics.errorsTotal,
	}, nil
}

// initializeObserverDeps initializes observer dependencies
func initializeObserverDeps(config *Config, logger *zap.Logger) (*Config, *zap.Logger, error) {
	if config == nil {
		config = DefaultConfig()
	}

	if logger == nil {
		var err error
		logger, err = zap.NewProduction()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create logger: %w", err)
		}
	}

	return config, logger, nil
}

// observerMetrics holds all metrics for the observer
type observerMetrics struct {
	connectionsTotal   metric.Int64Counter
	bytesTransferred   metric.Int64Counter
	packetsProcessed   metric.Int64Counter
	httpRequests       metric.Int64Counter
	dnsQueries         metric.Int64Counter
	connectionDuration metric.Float64Histogram
	requestLatency     metric.Float64Histogram
	eventsProcessed    metric.Int64Counter
	errorsTotal        metric.Int64Counter
}

// createObserverMetrics creates all metrics for the observer
func createObserverMetrics(name string, logger *zap.Logger) *observerMetrics {
	meter := otel.Meter(name)
	m := &observerMetrics{}

	m.connectionsTotal, _ = meter.Int64Counter(
		fmt.Sprintf("%s_connections_total", name),
		metric.WithDescription("Total network connections observed"),
	)

	m.bytesTransferred, _ = meter.Int64Counter(
		fmt.Sprintf("%s_bytes_transferred_total", name),
		metric.WithDescription("Total bytes transferred"),
	)

	m.packetsProcessed, _ = meter.Int64Counter(
		fmt.Sprintf("%s_packets_processed_total", name),
		metric.WithDescription("Total packets processed"),
	)

	m.httpRequests, _ = meter.Int64Counter(
		fmt.Sprintf("%s_http_requests_total", name),
		metric.WithDescription("Total HTTP requests observed"),
	)

	m.dnsQueries, _ = meter.Int64Counter(
		fmt.Sprintf("%s_dns_queries_total", name),
		metric.WithDescription("Total DNS queries observed"),
	)

	m.connectionDuration, _ = meter.Float64Histogram(
		fmt.Sprintf("%s_connection_duration_ms", name),
		metric.WithDescription("Connection duration in milliseconds"),
	)

	m.requestLatency, _ = meter.Float64Histogram(
		fmt.Sprintf("%s_request_latency_ms", name),
		metric.WithDescription("Request latency in milliseconds"),
	)

	m.eventsProcessed, _ = meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription("Total events processed"),
	)

	m.errorsTotal, _ = meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription("Total errors in observer"),
	)

	return m
}

// Name returns the observer name
func (o *Observer) Name() string {
	return o.name
}

// Start starts the observer
func (o *Observer) Start(ctx context.Context) error {
	o.logger.Info("Starting network observer",
		zap.Bool("ipv4", o.config.EnableIPv4),
		zap.Bool("ipv6", o.config.EnableIPv6),
		zap.Bool("tcp", o.config.EnableTCP),
		zap.Bool("udp", o.config.EnableUDP),
		zap.Bool("http", o.config.EnableHTTP),
		zap.Bool("dns", o.config.EnableDNS),
		zap.Bool("l7Parse", o.config.EnableL7Parse),
	)

	// Start eBPF monitoring (platform-specific)
	if err := o.startEBPF(); err != nil {
		return fmt.Errorf("failed to start eBPF: %w", err)
	}

	// Start background tasks
	if o.config.EnableL7Parse {
		o.LifecycleManager.Start("l7-parser", func() {
			o.runL7Parser()
		})
	}

	o.LifecycleManager.Start("event-processor", func() {
		// Event processing handled by eBPF goroutines
	})

	o.LifecycleManager.Start("metrics-flush", func() {
		o.flushMetrics()
	})

	o.BaseObserver.SetHealthy(true)
	o.logger.Info("Network observer started successfully")
	return nil
}

// Stop stops the observer
func (o *Observer) Stop() error {
	o.logger.Info("Stopping network observer")

	// Stop eBPF monitoring
	o.stopEBPF()

	// Stop background tasks
	if err := o.LifecycleManager.Stop(5 * time.Second); err != nil {
		o.logger.Warn("Timeout during shutdown", zap.Error(err))
	}

	// Close event channel
	o.EventChannelManager.Close()

	o.BaseObserver.SetHealthy(false)
	o.logger.Info("Network observer stopped")
	return nil
}

// Events returns the events channel
func (o *Observer) Events() <-chan *domain.CollectorEvent {
	return o.EventChannelManager.GetChannel()
}

// Statistics returns observer statistics
func (o *Observer) Statistics() *domain.CollectorStats {
	return o.BaseObserver.Statistics()
}

// Health returns health status
func (o *Observer) Health() *domain.HealthStatus {
	return o.BaseObserver.Health()
}

// runL7Parser runs the L7 protocol parser
func (o *Observer) runL7Parser() {
	ticker := time.NewTicker(o.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-o.LifecycleManager.Context().Done():
			return
		case <-ticker.C:
			o.l7Parser.Flush()
		}
	}
}

// flushMetrics periodically flushes metrics
func (o *Observer) flushMetrics() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-o.LifecycleManager.Context().Done():
			return
		case <-ticker.C:
			// Metrics are automatically exported by OTEL
			o.logger.Debug("Metrics flush cycle",
				zap.Int64("events_processed", o.BaseObserver.GetEventCount()),
				zap.Int64("events_dropped", o.BaseObserver.GetDroppedCount()))
		}
	}
}

// SendEvent sends an event through the observer
func (o *Observer) SendEvent(event *domain.CollectorEvent) {
	if o.EventChannelManager.SendEvent(event) {
		o.BaseObserver.RecordEvent()
		if o.eventsProcessed != nil {
			o.eventsProcessed.Add(o.LifecycleManager.Context(), 1)
		}
	} else {
		o.BaseObserver.RecordDrop()
		o.logger.Debug("Event dropped due to full channel")
	}
}
