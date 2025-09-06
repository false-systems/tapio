package processsignals

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/observers/base"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Config holds configuration for runtime observer
type Config struct {
	// Buffer size for event channel
	BufferSize int

	// Enable eBPF monitoring
	EnableEBPF bool

	// Ring buffer config for high-volume signal processing
	EnableRingBuffer bool
	RingBufferSize   int           // Must be power of 2
	BatchSize        int           // Events to process at once
	BatchTimeout     time.Duration // Max time to wait for batch

	// Filter config for noise reduction
	EnableFilters    bool
	FilterConfigPath string

	// Logger
	Logger *zap.Logger
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		BufferSize:       10000,
		EnableEBPF:       true,
		EnableRingBuffer: true,
		RingBufferSize:   8192, // Power of 2
		BatchSize:        32,
		BatchTimeout:     10 * time.Millisecond,
		EnableFilters:    true,
	}
}

// Observer implements runtime signal monitoring via eBPF
type Observer struct {
	*base.BaseObserver        // Provides Statistics() and Health()
	*base.EventChannelManager // Handles event channel with drop counting
	*base.LifecycleManager    // Manages goroutines and graceful shutdown

	name   string
	config *Config
	logger *zap.Logger

	// eBPF components (platform-specific)
	ebpfState interface{}

	// Signal correlation engine
	signalTracker *SignalTracker

	// OTEL instrumentation
	tracer trace.Tracer

	// Core metrics (mandatory)
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
	droppedEvents   metric.Int64Counter
	bufferUsage     metric.Float64Gauge

	// Runtime-specific metrics
	ebpfLoadsTotal     metric.Int64Counter
	ebpfLoadErrors     metric.Int64Counter
	ebpfAttachTotal    metric.Int64Counter
	ebpfAttachErrors   metric.Int64Counter
	k8sExtractionTotal metric.Int64Counter
	k8sExtractionHits  metric.Int64Counter
	signalsByType      metric.Int64Counter
	deathsCorrelated   metric.Int64Counter
	processExecs       metric.Int64Counter
	processExits       metric.Int64Counter
	oomKills           metric.Int64Counter
}

// NewObserver creates a new runtime signals observer
func NewObserver(name string, config *Config) (*Observer, error) {
	if config == nil {
		config = DefaultConfig()
	}

	if config.Logger == nil {
		logger, err := zap.NewProduction()
		if err != nil {
			return nil, fmt.Errorf("failed to create logger: %w", err)
		}
		config.Logger = logger
	}

	// Initialize OTEL components
	tracer := otel.Tracer(name)
	meter := otel.Meter(name)

	// Create core metrics
	eventsProcessed, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription(fmt.Sprintf("Total events processed by %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create events counter", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription(fmt.Sprintf("Total errors in %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	processingTime, err := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_duration_ms", name),
		metric.WithDescription(fmt.Sprintf("Processing duration for %s in milliseconds", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	droppedEvents, err := meter.Int64Counter(
		fmt.Sprintf("%s_dropped_events_total", name),
		metric.WithDescription(fmt.Sprintf("Total events dropped by %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create dropped events counter", zap.Error(err))
	}

	bufferUsage, err := meter.Float64Gauge(
		fmt.Sprintf("%s_buffer_usage_ratio", name),
		metric.WithDescription(fmt.Sprintf("Buffer usage ratio for %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create buffer usage gauge", zap.Error(err))
	}

	// Create runtime-specific metrics
	ebpfLoadsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_ebpf_loads_total", name),
		metric.WithDescription(fmt.Sprintf("Total eBPF loads attempted by %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create ebpf_loads counter", zap.Error(err))
	}

	ebpfLoadErrors, err := meter.Int64Counter(
		fmt.Sprintf("%s_ebpf_load_errors_total", name),
		metric.WithDescription(fmt.Sprintf("Total eBPF load errors in %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create ebpf_load_errors counter", zap.Error(err))
	}

	ebpfAttachTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_ebpf_attach_total", name),
		metric.WithDescription(fmt.Sprintf("Total eBPF attach attempts by %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create ebpf_attach counter", zap.Error(err))
	}

	ebpfAttachErrors, err := meter.Int64Counter(
		fmt.Sprintf("%s_ebpf_attach_errors_total", name),
		metric.WithDescription(fmt.Sprintf("Total eBPF attach errors in %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create ebpf_attach_errors counter", zap.Error(err))
	}

	k8sExtractionTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_k8s_extraction_attempts_total", name),
		metric.WithDescription(fmt.Sprintf("Total K8s metadata extraction attempts by %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create k8s_extraction counter", zap.Error(err))
	}

	k8sExtractionHits, err := meter.Int64Counter(
		fmt.Sprintf("%s_k8s_extraction_hits_total", name),
		metric.WithDescription(fmt.Sprintf("Total K8s metadata extraction hits by %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create k8s_extraction_hits counter", zap.Error(err))
	}

	signalsByType, err := meter.Int64Counter(
		fmt.Sprintf("%s_signals_by_type_total", name),
		metric.WithDescription(fmt.Sprintf("Total signals by type observed by %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create signals_by_type counter", zap.Error(err))
	}

	deathsCorrelated, err := meter.Int64Counter(
		fmt.Sprintf("%s_deaths_correlated_total", name),
		metric.WithDescription(fmt.Sprintf("Total process deaths correlated with signals by %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create deaths_correlated counter", zap.Error(err))
	}

	processExecs, err := meter.Int64Counter(
		fmt.Sprintf("%s_process_execs_total", name),
		metric.WithDescription(fmt.Sprintf("Total process executions observed by %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create process_execs counter", zap.Error(err))
	}

	processExits, err := meter.Int64Counter(
		fmt.Sprintf("%s_process_exits_total", name),
		metric.WithDescription(fmt.Sprintf("Total process exits observed by %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create process_exits counter", zap.Error(err))
	}

	oomKills, err := meter.Int64Counter(
		fmt.Sprintf("%s_oom_kills_total", name),
		metric.WithDescription(fmt.Sprintf("Total OOM kills observed by %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create oom_kills counter", zap.Error(err))
	}

	// Initialize base components with ring buffer and filter support
	baseConfig := base.BaseObserverConfig{
		Name:               name,
		HealthCheckTimeout: 5 * time.Minute,
		ErrorRateThreshold: 0.1,
		EnableRingBuffer:   config.EnableRingBuffer,
		RingBufferSize:     config.RingBufferSize,
		BatchSize:          config.BatchSize,
		BatchTimeout:       config.BatchTimeout,
		EnableFilters:      config.EnableFilters,
		FilterConfigPath:   config.FilterConfigPath,
		Logger:             config.Logger.Named("base"),
	}

	// Initialize signal correlation engine
	signalTracker := NewSignalTracker(config.Logger.Named("signal_tracker"))

	o := &Observer{
		BaseObserver:        base.NewBaseObserverWithConfig(baseConfig),
		EventChannelManager: base.NewEventChannelManager(config.BufferSize, name, config.Logger),
		LifecycleManager:    base.NewLifecycleManager(context.Background(), config.Logger),

		name:          name,
		config:        config,
		logger:        config.Logger.Named(name),
		signalTracker: signalTracker,

		// OTEL
		tracer:          tracer,
		eventsProcessed: eventsProcessed,
		errorsTotal:     errorsTotal,
		processingTime:  processingTime,
		droppedEvents:   droppedEvents,
		bufferUsage:     bufferUsage,

		// Runtime-specific
		ebpfLoadsTotal:     ebpfLoadsTotal,
		ebpfLoadErrors:     ebpfLoadErrors,
		ebpfAttachTotal:    ebpfAttachTotal,
		ebpfAttachErrors:   ebpfAttachErrors,
		k8sExtractionTotal: k8sExtractionTotal,
		k8sExtractionHits:  k8sExtractionHits,
		signalsByType:      signalsByType,
		deathsCorrelated:   deathsCorrelated,
		processExecs:       processExecs,
		processExits:       processExits,
		oomKills:           oomKills,
	}

	o.logger.Info("Runtime signals observer created", zap.String("name", name))
	return o, nil
}

// Name returns observer name
func (o *Observer) Name() string {
	return o.name
}

// Start starts the runtime signal monitoring
func (o *Observer) Start(ctx context.Context) error {
	o.logger.Info("Starting runtime signals observer")

	// Mark as healthy
	o.BaseObserver.SetHealthy(true)

	// Initialize platform-specific monitoring
	if o.config.EnableEBPF {
		if err := o.initializeEBPF(ctx); err != nil {
			o.logger.Warn("Failed to initialize eBPF, falling back to limited mode", zap.Error(err))
			// Continue without eBPF - we can still provide some value
		}
	}

	// Start event processing
	o.LifecycleManager.Start("process-events", func() {
		o.processEvents(ctx)
	})

	o.logger.Info("Runtime signals observer started")
	return nil
}

// Stop stops the observer
func (o *Observer) Stop() error {
	o.logger.Info("Stopping runtime signals observer")

	// Stop lifecycle manager
	if err := o.LifecycleManager.Stop(5 * time.Second); err != nil {
		o.logger.Error("Failed to stop lifecycle manager", zap.Error(err))
	}

	// Cleanup eBPF if initialized
	if o.ebpfState != nil {
		o.cleanupEBPF()
	}

	// Close event channel
	o.EventChannelManager.Close()

	// Mark as unhealthy
	o.BaseObserver.SetHealthy(false)

	o.logger.Info("Runtime signals observer stopped")
	return nil
}

// Events returns the event channel
func (o *Observer) Events() <-chan *domain.CollectorEvent {
	return o.EventChannelManager.GetChannel()
}

// IsHealthy returns health status
func (o *Observer) IsHealthy() bool {
	health := o.BaseObserver.Health()
	return health.Status == domain.HealthHealthy
}

// Statistics returns observer statistics
func (o *Observer) Statistics() interface{} {
	stats := o.BaseObserver.Statistics()

	// Add runtime-specific stats
	if stats.CustomMetrics == nil {
		stats.CustomMetrics = make(map[string]string)
	}

	if o.signalTracker != nil {
		trackedPIDs, totalSignals, deathsCached := o.signalTracker.GetStats()
		stats.CustomMetrics["tracked_pids"] = fmt.Sprintf("%d", trackedPIDs)
		stats.CustomMetrics["total_signals"] = fmt.Sprintf("%d", totalSignals)
		stats.CustomMetrics["deaths_cached"] = fmt.Sprintf("%d", deathsCached)
	}

	stats.CustomMetrics["ebpf_enabled"] = fmt.Sprintf("%v", o.ebpfState != nil)

	return stats
}

// Health returns health status
func (o *Observer) Health() *domain.HealthStatus {
	health := o.BaseObserver.Health()
	health.Component = o.name

	// Add error count from statistics
	stats := o.BaseObserver.Statistics()
	if stats != nil {
		health.ErrorCount = stats.ErrorCount
	}

	return health
}
