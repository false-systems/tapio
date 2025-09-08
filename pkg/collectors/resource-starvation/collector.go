package resourcestarvation

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/yairfalse/tapio/pkg/collectors/base"
	"github.com/yairfalse/tapio/pkg/domain"
)

type Collector struct {
	*base.BaseCollector
	*base.EventChannelManager
	*base.LifecycleManager

	config *Config
	logger *zap.Logger

	// OTEL instrumentation
	tracer trace.Tracer
	meter  metric.Meter

	// Core metrics
	schedDelayHist     metric.Float64Histogram
	throttleTimeHist   metric.Float64Histogram
	starvationEvents   metric.Int64Counter
	noiseScore         metric.Float64Gauge
	waitRatio          metric.Float64Gauge
	throttlePercentage metric.Float64Gauge
}

func NewCollector(config *Config, logger *zap.Logger) (*Collector, error) {
	if config == nil {
		config = NewDefaultConfig()
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	tracer := otel.Tracer("resource-starvation")
	meter := otel.Meter("resource-starvation")

	schedDelayBuckets := []float64{0.001, 0.010, 0.050, 0.100, 0.500, 1.000, 5.000}

	schedDelayHist, err := meter.Float64Histogram(
		"resource_starvation_sched_delay_seconds",
		metric.WithDescription("Scheduling delay distribution"),
		metric.WithExplicitBucketBoundaries(schedDelayBuckets...),
	)
	if err != nil {
		logger.Warn("Failed to create scheduling delay histogram", zap.Error(err))
	}

	throttleTimeHist, err := meter.Float64Histogram(
		"resource_starvation_throttle_duration_seconds",
		metric.WithDescription("CFS throttle duration distribution"),
	)
	if err != nil {
		logger.Warn("Failed to create throttle time histogram", zap.Error(err))
	}

	starvationEvents, err := meter.Int64Counter(
		"resource_starvation_events_total",
		metric.WithDescription("Total starvation events detected"),
	)
	if err != nil {
		logger.Warn("Failed to create starvation events counter", zap.Error(err))
	}

	noiseScore, err := meter.Float64Gauge(
		"resource_starvation_noise_score",
		metric.WithDescription("Noisy neighbor score per container"),
	)
	if err != nil {
		logger.Warn("Failed to create noise score gauge", zap.Error(err))
	}

	waitRatio, err := meter.Float64Gauge(
		"resource_starvation_wait_ratio",
		metric.WithDescription("Wait time to run time ratio"),
	)
	if err != nil {
		logger.Warn("Failed to create wait ratio gauge", zap.Error(err))
	}

	throttlePercentage, err := meter.Float64Gauge(
		"resource_starvation_throttle_percentage",
		metric.WithDescription("Percentage of time throttled"),
	)
	if err != nil {
		logger.Warn("Failed to create throttle percentage gauge", zap.Error(err))
	}

	collector := &Collector{
		BaseCollector:       base.NewBaseCollector(config.Name, 5*time.Minute),
		EventChannelManager: base.NewEventChannelManager(config.EventChannelSize, config.Name, logger),
		LifecycleManager:    base.NewLifecycleManager(context.Background(), logger),
		config:              config,
		logger:              logger,
		tracer:              tracer,
		meter:               meter,
		schedDelayHist:      schedDelayHist,
		throttleTimeHist:    throttleTimeHist,
		starvationEvents:    starvationEvents,
		noiseScore:          noiseScore,
		waitRatio:           waitRatio,
		throttlePercentage:  throttlePercentage,
	}

	collector.SetHealthy(true)
	return collector, nil
}

func (c *Collector) Start(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "collector.start")
	defer span.End()

	c.logger.Info("Starting resource starvation collector", zap.String("config", c.config.String()))

	// Start any background goroutines here using LifecycleManager
	// Example:
	// c.LifecycleManager.Start("event-processor", func() {
	//     c.processEvents()
	// })

	return nil
}

func (c *Collector) Stop() error {
	c.logger.Info("Stopping resource starvation collector")

	// Stop lifecycle manager with 30 second timeout
	if err := c.LifecycleManager.Stop(30 * time.Second); err != nil {
		c.logger.Warn("Error during shutdown", zap.Error(err))
	}

	// Close event channel
	c.EventChannelManager.Close()

	c.SetHealthy(false)
	return nil
}

// Name returns the unique identifier for this collector
func (c *Collector) Name() string {
	return c.config.Name
}

// Events returns a channel of collector events
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.GetChannel()
}

// IsHealthy returns true if the collector is functioning properly
func (c *Collector) IsHealthy() bool {
	return c.BaseCollector.IsHealthy()
}

func (c *Collector) ProcessEvent(ctx context.Context, event *StarvationEvent) error {
	ctx, span := c.tracer.Start(ctx, "collector.process_event")
	defer span.End()

	if event == nil {
		return fmt.Errorf("cannot process nil event")
	}

	span.SetAttributes(
		attribute.String("event.type", EventType(event.EventType).String()),
		attribute.Int("event.victim_pid", int(event.VictimPID)),
		attribute.Float64("event.wait_time_ms", float64(event.WaitTimeNS)/1_000_000),
	)

	waitTimeSeconds := float64(event.WaitTimeNS) / 1_000_000_000

	if c.schedDelayHist != nil {
		c.schedDelayHist.Record(ctx, waitTimeSeconds, metric.WithAttributes(
			attribute.String("event_type", EventType(event.EventType).String()),
		))
	}

	if c.starvationEvents != nil {
		c.starvationEvents.Add(ctx, 1, metric.WithAttributes(
			attribute.String("severity", GetSeverity(event.WaitTimeNS)),
			attribute.String("event_type", EventType(event.EventType).String()),
		))
	}

	if event.ThrottledNS > 0 {
		throttleSeconds := float64(event.ThrottledNS) / 1_000_000_000
		if c.throttleTimeHist != nil {
			c.throttleTimeHist.Record(ctx, throttleSeconds)
		}
	}

	// Convert to domain CollectorEvent and emit
	collectorEvent := c.convertToCollectorEvent(event)
	if c.SendEvent(collectorEvent) {
		c.RecordEvent() // Track successful event
	} else {
		c.RecordDrop() // Track dropped event
		c.logger.Warn("Event dropped, channel full",
			zap.String("event_id", collectorEvent.EventID))
	}

	return nil
}

func (c *Collector) detectStarvationPattern(event *StarvationEvent) string {
	switch {
	case event.ThrottledNS > 0:
		return PatternThrottle
	case event.WaitTimeNS > 1_000_000_000:
		return PatternSustained
	case EventType(event.EventType) == EventCoreMigrate:
		return PatternCacheThrash
	case EventType(event.EventType) == EventNoisyNeighbor:
		return PatternNoisyNeighbor
	default:
		return PatternBurst
	}
}

func (c *Collector) convertToCollectorEvent(event *StarvationEvent) *domain.CollectorEvent {
	eventType := c.mapEventType(EventType(event.EventType))
	severity := c.mapSeverity(GetSeverity(event.WaitTimeNS))

	waitTimeMS := float64(event.WaitTimeNS) / 1_000_000
	runTimeMS := float64(event.RunTimeNS) / 1_000_000
	throttleTimeMS := float64(event.ThrottledNS) / 1_000_000

	// Calculate impact metrics
	waitToRunRatio := float64(0)
	if runTimeMS > 0 {
		waitToRunRatio = waitTimeMS / runTimeMS
	}

	estimatedLatencyMS := waitTimeMS * 0.5 // Rough estimate

	pattern := c.detectStarvationPattern(event)

	return &domain.CollectorEvent{
		EventID:   fmt.Sprintf("starvation-%d-%d", event.VictimPID, event.Timestamp),
		Timestamp: time.Unix(0, int64(event.Timestamp)),
		Type:      eventType,
		Source:    c.config.Name,
		Severity:  severity,

		EventData: domain.EventDataContainer{
			// Use Kernel data for resource starvation events
			Kernel: &domain.KernelData{
				EventType:    EventType(event.EventType).String(),
				PID:          int32(event.VictimPID),
				PPID:         0, // Not available in our event
				UID:          0, // Not available in our event
				GID:          0, // Not available in our event
				Command:      c.bytesToString(event.VictimComm[:]),
				CgroupID:     event.VictimCgroupID,
				ErrorMessage: c.getPatternDescription(pattern),
				// Using Custom field to store resource starvation data

			},
		},

		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"collector":            c.config.Name,
				"event_type":           EventType(event.EventType).String(),
				"severity":             GetSeverity(event.WaitTimeNS),
				"cpu_core":             fmt.Sprintf("%d", event.CPUCore),
				"pattern_type":         pattern,
				"wait_time_ms":         fmt.Sprintf("%.2f", waitTimeMS),
				"run_time_ms":          fmt.Sprintf("%.2f", runTimeMS),
				"throttle_time_ms":     fmt.Sprintf("%.2f", throttleTimeMS),
				"wait_to_run_ratio":    fmt.Sprintf("%.2f", waitToRunRatio),
				"culprit_pid":          fmt.Sprintf("%d", event.CulpritPID),
				"culprit_command":      c.bytesToString(event.CulpritComm[:]),
				"victim_priority":      fmt.Sprintf("%d", event.VictimPrio),
				"victim_policy":        GetSchedulingPolicy(event.VictimPolicy),
				"percent_throttled":    fmt.Sprintf("%.1f", c.calculateThrottlePercentage(event)),
				"estimated_latency_ms": fmt.Sprintf("%.2f", estimatedLatencyMS),
			},
		},
	}
}

// Define resource starvation event types as domain constants
const (
	EventTypeSchedulingDelay    domain.CollectorEventType = "resource.scheduling_delay"
	EventTypeCFSThrottle        domain.CollectorEventType = "resource.cfs_throttle"
	EventTypePriorityInversion  domain.CollectorEventType = "resource.priority_inversion"
	EventTypeCoreMigration      domain.CollectorEventType = "resource.core_migration"
	EventTypeNoisyNeighbor      domain.CollectorEventType = "resource.noisy_neighbor"
	EventTypeResourceStarvation domain.CollectorEventType = "resource.starvation"
)

func (c *Collector) mapEventType(et EventType) domain.CollectorEventType {
	// Map to generic kernel event types
	switch et {
	case EventSchedWait:
		return domain.EventTypeKernelCgroup // Scheduling delays often related to cgroups
	case EventCFSThrottle:
		return domain.EventTypeKernelCgroup // CFS throttling is cgroup-related
	case EventPriorityInvert:
		return domain.EventTypeKernelProcess // Priority inversion is process-related
	case EventCoreMigrate:
		return domain.EventTypeKernelProcess // Core migration is process-related
	case EventNoisyNeighbor:
		return domain.EventTypeKernelCgroup // Noisy neighbor affects cgroups
	default:
		return domain.EventTypeKernelProcess // Default to process event

	}
}

func (c *Collector) mapSeverity(severityStr string) domain.EventSeverity {
	switch severityStr {
	case SeverityMinor:
		return domain.EventSeverityInfo
	case SeverityModerate:
		return domain.EventSeverityWarning
	case SeveritySevere:
		return domain.EventSeverityError
	case SeverityCritical:
		return domain.EventSeverityCritical
	default:
		return domain.EventSeverityInfo
	}
}

func (c *Collector) bytesToString(b []byte) string {
	for i, v := range b {
		if v == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

func (c *Collector) calculateThrottlePercentage(event *StarvationEvent) float64 {
	if event.NrPeriods == 0 {
		return 0
	}
	return float64(event.NrThrottled) / float64(event.NrPeriods) * 100
}

func (c *Collector) getPatternDescription(pattern string) string {
	switch pattern {
	case PatternThrottle:
		return "Container hit CPU limit and was throttled"
	case PatternNoisyNeighbor:
		return "Another process is consuming excessive CPU"
	case PatternBurst:
		return "Burst workload causing periodic starvation"
	case PatternSustained:
		return "Sustained high scheduling delays"
	case PatternPriorityInv:
		return "Low priority task blocking high priority task"
	case PatternCacheThrash:
		return "Frequent CPU migrations destroying cache performance"
	default:
		return "Resource starvation detected"
	}
}
