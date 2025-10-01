// Package base provides common functionality for all Tapio observers
// This reduces code duplication and ensures consistent observability
package base

import (
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// BaseObserver provides common statistics and health tracking for all observers
// Embed this in your observer to get Statistics() and Health() methods automatically
type BaseObserver struct {
	// Basic info
	name      string
	startTime time.Time

	// Statistics tracking (atomic for thread safety)
	eventsProcessed atomic.Int64
	eventsDropped   atomic.Int64
	eventsFiltered  atomic.Int64 // New: tracks filtered events
	errorCount      atomic.Int64

	// Atomic values for complex types
	lastEventTime atomic.Value // stores time.Time
	lastError     atomic.Value // stores error

	// Health tracking
	isHealthy          atomic.Bool
	healthCheckTimeout time.Duration
	errorRateThreshold float64 // Configurable error rate threshold

	// OTEL instrumentation
	tracer trace.Tracer
	meter  metric.Meter

	// Standard OTEL metrics
	eventsProcessedCounter metric.Int64Counter
	eventsDroppedCounter   metric.Int64Counter
	eventsFilteredCounter  metric.Int64Counter // New metric
	errorCounter           metric.Int64Counter
	processingDuration     metric.Float64Histogram
	eventSizeHistogram     metric.Int64Histogram
	healthStatus           metric.Int64Gauge

	// Ring buffer support (optional)
	ringBuffer    *RingBuffer
	useRingBuffer bool

	// Filter support (optional)
	filterManager *FilterManager
	useFilters    bool
	logger        *zap.Logger // Need logger for filter manager

	// Multi-output support (NEW)
	outputTargets OutputTargets
	otelEmitter   *OTELEmitter
	stdoutEmitter *StdoutEmitter
	// natsEmitter *NATSEmitter // Future: NATS support
}

// BaseObserverConfig holds configuration for BaseObserver
type BaseObserverConfig struct {
	Name               string
	HealthCheckTimeout time.Duration
	ErrorRateThreshold float64 // Default 0.1 (10%)

	// Ring buffer configuration (optional)
	EnableRingBuffer bool
	RingBufferSize   int           // Must be power of 2
	BatchSize        int           // Events to process at once
	BatchTimeout     time.Duration // Max time to wait for batch

	// Filter configuration (optional)
	EnableFilters    bool
	FilterConfigPath string // Path to filter config file (YAML)

	// Multi-output configuration (NEW)
	OutputTargets OutputTargets
	OTELConfig    *OTELOutputConfig
	StdoutConfig  *StdoutEmitterConfig

	// Logger
	Logger *zap.Logger
}

// NewBaseObserver creates a new base observer with the given name
// healthCheckTimeout determines how long without events before marking degraded
func NewBaseObserver(name string, healthCheckTimeout time.Duration) *BaseObserver {
	return NewBaseObserverWithConfig(BaseObserverConfig{
		Name:               name,
		HealthCheckTimeout: healthCheckTimeout,
		ErrorRateThreshold: 0.1, // Default 10%
	})
}

// NewBaseObserverWithConfig creates a new base observer with full configuration
func NewBaseObserverWithConfig(config BaseObserverConfig) *BaseObserver {
	if config.ErrorRateThreshold == 0 {
		config.ErrorRateThreshold = 0.1 // Default 10%
	}

	bc := &BaseObserver{
		name:               config.Name,
		startTime:          time.Now(),
		healthCheckTimeout: config.HealthCheckTimeout,
		errorRateThreshold: config.ErrorRateThreshold,
		tracer:             otel.Tracer(config.Name),
		meter:              otel.Meter(config.Name),
		useRingBuffer:      config.EnableRingBuffer,
		useFilters:         config.EnableFilters,
		logger:             config.Logger,
	}
	bc.isHealthy.Store(true)
	bc.lastEventTime.Store(time.Now())

	// Initialize ring buffer if enabled
	if config.EnableRingBuffer {
		rbConfig := RingBufferConfig{
			Size:          config.RingBufferSize,
			BatchSize:     config.BatchSize,
			BatchTimeout:  config.BatchTimeout,
			CollectorName: config.Name,
			Logger:        config.Logger,
		}

		// Set defaults if not specified
		if rbConfig.Size == 0 {
			rbConfig.Size = 8192
		}
		if rbConfig.BatchSize == 0 {
			rbConfig.BatchSize = 32
		}
		if rbConfig.BatchTimeout == 0 {
			rbConfig.BatchTimeout = 10 * time.Millisecond
		}

		ringBuffer, err := NewRingBuffer(rbConfig)
		if err != nil {
			// Log ring buffer creation failure but continue - fall back to channel-only mode
			if config.Logger != nil {
				config.Logger.Warn("Failed to create ring buffer, falling back to channel-only mode",
					zap.String("observer", config.Name),
					zap.Error(err))
			}
		} else {
			bc.ringBuffer = ringBuffer
		}
	}

	// Initialize filter manager if enabled
	if config.EnableFilters {
		bc.filterManager = NewFilterManager(config.Name, config.Logger)

		// Start watching config file if provided
		if config.FilterConfigPath != "" {
			if err := bc.filterManager.WatchConfigFile(config.FilterConfigPath); err != nil {
				if config.Logger != nil {
					config.Logger.Warn("Failed to watch filter config file",
						zap.String("observer", config.Name),
						zap.String("path", config.FilterConfigPath),
						zap.Error(err))
				}
			}
		}
	}

	// Initialize output targets (NEW)
	bc.outputTargets = config.OutputTargets
	if bc.outputTargets.HasAnyOutput() {
		bc.initializeOutputs(config)
	}

	// Initialize OTEL metrics
	bc.initializeMetrics()

	return bc
}
