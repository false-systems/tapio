package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/internal/observers/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Observer monitors CPU scheduling delays and resource contention
type Observer struct {
	*base.BaseObserver
	*base.EventChannelManager
	*base.LifecycleManager

	config *Config
	logger *zap.Logger
	name   string

	// eBPF state (platform-specific)
	ebpfState interface{}

	// Pattern detection
	patternDetector *PatternDetector

	// OTEL instrumentation
	tracer trace.Tracer
	meter  metric.Meter

	// Core metrics
	schedDelayHist     metric.Float64Histogram
	throttleTimeHist   metric.Float64Histogram
	schedulerEvents    metric.Int64Counter
	noiseScore         metric.Float64Gauge
	waitRatio          metric.Float64Gauge
	throttlePercentage metric.Float64Gauge
	coreMigrations     metric.Int64Counter
}

// Config holds observer configuration
type Config struct {
	// Detection thresholds
	SchedDelayThresholdMs  int
	ThrottleThresholdMs    int
	MigrationThreshold     int
	NoiseNeighborThreshold float64

	// eBPF configuration
	RingBufferSize   int
	EventChannelSize int

	// Feature flags
	EnableStackTraces    bool
	EnablePatternDetect  bool
	EnableNoiseDetection bool

	// Performance tuning
	SamplingRate int // 1 = all events, 100 = 1 in 100
}

// NewDefaultConfig returns default configuration
func NewDefaultConfig() *Config {
	return &Config{
		SchedDelayThresholdMs:  10,  // 10ms scheduling delay
		ThrottleThresholdMs:    100, // 100ms throttle
		MigrationThreshold:     10,  // 10 migrations per second
		NoiseNeighborThreshold: 0.8, // 80% CPU monopolization
		RingBufferSize:         8 * 1024 * 1024,
		EventChannelSize:       10000,
		EnableStackTraces:      false, // Disabled by default for performance
		EnablePatternDetect:    true,
		EnableNoiseDetection:   true,
		SamplingRate:           1, // Sample all events
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.SchedDelayThresholdMs < 0 {
		return fmt.Errorf("scheduling delay threshold must be non-negative")
	}
	if c.RingBufferSize < 4096 {
		return fmt.Errorf("ring buffer size too small")
	}
	if c.SamplingRate < 1 {
		return fmt.Errorf("sampling rate must be at least 1")
	}
	return nil
}

// NewObserver creates a new scheduler observer
func NewObserver(name string, config *Config, logger *zap.Logger) (*Observer, error) {
	if config == nil {
		config = NewDefaultConfig()
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	if logger == nil {
		var err error
		logger, err = zap.NewProduction()
		if err != nil {
			return nil, fmt.Errorf("failed to create logger: %w", err)
		}
	}

	tracer := otel.Tracer(name)
	meter := otel.Meter(name)

	// Create metrics
	schedDelayBuckets := []float64{0.001, 0.010, 0.050, 0.100, 0.500, 1.000, 5.000}

	schedDelayHist, err := meter.Float64Histogram(
		fmt.Sprintf("%s_sched_delay_seconds", name),
		metric.WithDescription("CPU scheduling delay distribution"),
		metric.WithExplicitBucketBoundaries(schedDelayBuckets...),
	)
	if err != nil {
		logger.Warn("Failed to create scheduling delay histogram", zap.Error(err))
	}

	throttleTimeHist, err := meter.Float64Histogram(
		fmt.Sprintf("%s_throttle_duration_seconds", name),
		metric.WithDescription("CFS throttle duration distribution"),
	)
	if err != nil {
		logger.Warn("Failed to create throttle time histogram", zap.Error(err))
	}

	schedulerEvents, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_total", name),
		metric.WithDescription("Total scheduler events detected"),
	)
	if err != nil {
		logger.Warn("Failed to create scheduler events counter", zap.Error(err))
	}

	noiseScore, err := meter.Float64Gauge(
		fmt.Sprintf("%s_noise_score", name),
		metric.WithDescription("Noisy neighbor score per container"),
	)
	if err != nil {
		logger.Warn("Failed to create noise score gauge", zap.Error(err))
	}

	waitRatio, err := meter.Float64Gauge(
		fmt.Sprintf("%s_wait_ratio", name),
		metric.WithDescription("Wait time to run time ratio"),
	)
	if err != nil {
		logger.Warn("Failed to create wait ratio gauge", zap.Error(err))
	}

	throttlePercentage, err := meter.Float64Gauge(
		fmt.Sprintf("%s_throttle_percentage", name),
		metric.WithDescription("Percentage of time throttled"),
	)
	if err != nil {
		logger.Warn("Failed to create throttle percentage gauge", zap.Error(err))
	}

	coreMigrations, err := meter.Int64Counter(
		fmt.Sprintf("%s_core_migrations_total", name),
		metric.WithDescription("Total CPU core migrations"),
	)
	if err != nil {
		logger.Warn("Failed to create core migrations counter", zap.Error(err))
	}

	// Create pattern detector if enabled
	var patternDetector *PatternDetector
	if config.EnablePatternDetect {
		patternDetector = NewPatternDetector(logger)
	}

	o := &Observer{
		BaseObserver:        base.NewBaseObserver(name, 5*time.Minute),
		EventChannelManager: base.NewEventChannelManager(config.EventChannelSize, name, logger),
		LifecycleManager:    base.NewLifecycleManager(context.Background(), logger),
		config:              config,
		logger:              logger.Named(name),
		name:                name,
		patternDetector:     patternDetector,
		tracer:              tracer,
		meter:               meter,
		schedDelayHist:      schedDelayHist,
		throttleTimeHist:    throttleTimeHist,
		schedulerEvents:     schedulerEvents,
		noiseScore:          noiseScore,
		waitRatio:           waitRatio,
		throttlePercentage:  throttlePercentage,
		coreMigrations:      coreMigrations,
	}

	return o, nil
}

// Name returns the observer name
func (o *Observer) Name() string {
	return o.name
}

// Start begins monitoring scheduler events
func (o *Observer) Start(ctx context.Context) error {
	o.logger.Info("Starting scheduler observer",
		zap.Int("schedDelayThresholdMs", o.config.SchedDelayThresholdMs),
		zap.Int("throttleThresholdMs", o.config.ThrottleThresholdMs),
		zap.Bool("stackTraces", o.config.EnableStackTraces),
		zap.Bool("patternDetection", o.config.EnablePatternDetect),
	)

	// Start eBPF monitoring (platform-specific)
	if err := o.startEBPF(); err != nil {
		return fmt.Errorf("failed to start eBPF: %w", err)
	}

	// Start event processor
	o.LifecycleManager.Start("event-processor", func() {
		o.processEvents()
	})

	// Start pattern detection if enabled
	if o.config.EnablePatternDetect && o.patternDetector != nil {
		o.LifecycleManager.Start("pattern-detector", func() {
			o.runPatternDetection()
		})
	}

	o.BaseObserver.SetHealthy(true)
	o.logger.Info("Scheduler observer started successfully")
	return nil
}

// Stop stops the observer
func (o *Observer) Stop() error {
	o.logger.Info("Stopping scheduler observer")

	// Stop eBPF
	o.stopEBPF()

	// Stop goroutines
	if err := o.LifecycleManager.Stop(5 * time.Second); err != nil {
		o.logger.Warn("Timeout during shutdown", zap.Error(err))
	}

	// Close event channel
	o.EventChannelManager.Close()

	o.BaseObserver.SetHealthy(false)
	o.logger.Info("Scheduler observer stopped")
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

// Health returns health status
func (o *Observer) Health() *domain.HealthStatus {
	return o.BaseObserver.Health()
}

// runPatternDetection runs periodic pattern analysis
func (o *Observer) runPatternDetection() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-o.LifecycleManager.Context().Done():
			return
		case <-ticker.C:
			if o.patternDetector != nil {
				patterns := o.patternDetector.DetectPatterns()
				for _, pattern := range patterns {
					o.logger.Info("Pattern detected",
						zap.String("type", pattern.Type),
						zap.String("description", pattern.Description),
						zap.Float64("confidence", pattern.Confidence),
					)

					// Create event for significant patterns
					if pattern.Confidence > 0.8 {
						event := &domain.CollectorEvent{
							EventID:   fmt.Sprintf("scheduler-pattern-%d", time.Now().UnixNano()),
							Timestamp: time.Now(),
							Type:      domain.EventTypeScheduler,
							Source:    o.name,
							Severity:  domain.EventSeverityWarning,
							EventData: domain.EventDataContainer{
								Custom: map[string]string{
									"pattern_type":       pattern.Type,
									"pattern_desc":       pattern.Description,
									"pattern_confidence": fmt.Sprintf("%.2f", pattern.Confidence),
								},
							},
							Metadata: domain.EventMetadata{
								Labels: map[string]string{
									"pattern": "true",
									"type":    pattern.Type,
								},
							},
						}

						o.EventChannelManager.SendEvent(event)
					}
				}
			}
		}
	}
}

// handleSchedulerEvent processes a scheduler event
func (o *Observer) handleSchedulerEvent(eventType string, data interface{}) {
	ctx := o.LifecycleManager.Context()

	// Update metrics
	if o.schedulerEvents != nil {
		o.schedulerEvents.Add(ctx, 1, metric.WithAttributes(
			attribute.String("event_type", eventType)))
	}

	// Record with BaseObserver
	o.BaseObserver.RecordEvent()
}
