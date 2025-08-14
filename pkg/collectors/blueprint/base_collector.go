package blueprint

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/yairfalse/tapio/pkg/collectors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// BaseCollector provides the foundational implementation for all Tapio collectors
// It implements all required interfaces and provides extension points for specific collectors
type BaseCollector struct {
	// Core configuration
	name   string
	config BaseConfig
	logger *zap.Logger

	// Lifecycle management
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.RWMutex

	// Event processing
	events        chan collectors.RawEvent
	eventBuffer   []collectors.RawEvent
	bufferMu      sync.Mutex
	lastEventTime time.Time

	// State tracking
	healthy   bool
	startTime time.Time
	stats     atomic.Value // *CollectorStats

	// OTEL instrumentation (REQUIRED for all collectors)
	tracer                 trace.Tracer
	meter                  metric.Meter
	eventsProcessedCtr     metric.Int64Counter
	eventsDroppedCtr       metric.Int64Counter
	errorsCtr              metric.Int64Counter
	collectorHealthGauge   metric.Int64Gauge
	processingLatencyHist  metric.Float64Histogram
	bufferUtilizationGauge metric.Float64Gauge
	resourceUsageGauge     metric.Float64Gauge

	// Resource management
	resources   []func() error
	resourcesMu sync.Mutex

	// Container integration support
	podTraceMap map[string]string
	podTraceMu  sync.RWMutex

	// Validation
	validator *validator.Validate

	// Extension points for specific collectors
	eventGatherer     EventGatherer
	metadataExtractor K8sMetadataExtractor
	eventEnricher     EventEnricher
	securityValidator SecurityValidator

	// Performance monitoring
	resourceMonitor *resourceMonitor
}

// BaseConfig provides common configuration for all collectors
type BaseConfig struct {
	Name           string         `json:"name" yaml:"name" validate:"required,min=1,max=50"`
	BufferSize     int            `json:"buffer_size" yaml:"buffer_size" validate:"min=100,max=100000"`
	Workers        int            `json:"workers" yaml:"workers" validate:"min=1,max=20"`
	PollInterval   time.Duration  `json:"poll_interval" yaml:"poll_interval" validate:"min=100ms,max=1h"`
	EnableMetrics  bool           `json:"enable_metrics" yaml:"enable_metrics"`
	EnableTracing  bool           `json:"enable_tracing" yaml:"enable_tracing"`
	ResourceLimits ResourceLimits `json:"resource_limits" yaml:"resource_limits"`
	Security       SecurityConfig `json:"security" yaml:"security"`
	K8sIntegration K8sConfig      `json:"k8s_integration" yaml:"k8s_integration"`
}

// SecurityConfig defines security-related configuration
type SecurityConfig struct {
	FilterSensitiveData  bool     `json:"filter_sensitive_data" yaml:"filter_sensitive_data"`
	AllowedSources       []string `json:"allowed_sources" yaml:"allowed_sources"`
	RequiredCapabilities []string `json:"required_capabilities" yaml:"required_capabilities"`
	ValidatePermissions  bool     `json:"validate_permissions" yaml:"validate_permissions"`
}

// K8sConfig defines Kubernetes integration configuration
type K8sConfig struct {
	Enabled            bool   `json:"enabled" yaml:"enabled"`
	EnrichEvents       bool   `json:"enrich_events" yaml:"enrich_events"`
	ResolveOwnerRefs   bool   `json:"resolve_owner_refs" yaml:"resolve_owner_refs"`
	IncludeLabels      bool   `json:"include_labels" yaml:"include_labels"`
	IncludeAnnotations bool   `json:"include_annotations" yaml:"include_annotations"`
	KubeconfigPath     string `json:"kubeconfig_path" yaml:"kubeconfig_path"`
}

// CollectorStats tracks collector performance metrics
type CollectorStats struct {
	EventsCollected   int64     `json:"events_collected"`
	EventsDropped     int64     `json:"events_dropped"`
	ErrorCount        int64     `json:"error_count"`
	LastEventTime     time.Time `json:"last_event_time"`
	StartTime         time.Time `json:"start_time"`
	UptimeSeconds     float64   `json:"uptime_seconds"`
	MemoryUsageMB     float64   `json:"memory_usage_mb"`
	CPUUsagePercent   float64   `json:"cpu_usage_percent"`
	GoroutineCount    int       `json:"goroutine_count"`
	BufferUtilization float64   `json:"buffer_utilization"`
	ResourceHealth    bool      `json:"resource_health"`
}

// EventGatherer interface for collector-specific event gathering
type EventGatherer interface {
	// GatherEvents collects raw events from the source
	GatherEvents(ctx context.Context) ([]interface{}, error)

	// Initialize sets up the event gathering mechanism
	Initialize(ctx context.Context) error

	// Cleanup releases resources
	Cleanup() error
}

// DefaultBaseConfig returns default configuration
func DefaultBaseConfig() BaseConfig {
	return BaseConfig{
		BufferSize:    10000,
		Workers:       2,
		PollInterval:  time.Second,
		EnableMetrics: true,
		EnableTracing: true,
		ResourceLimits: ResourceLimits{
			MaxMemoryMB:   100,
			MaxCPUPercent: 10,
			MaxOpenFiles:  1000,
			MaxGoroutines: 100,
		},
		Security: SecurityConfig{
			FilterSensitiveData:  true,
			ValidatePermissions:  true,
			RequiredCapabilities: []string{},
		},
		K8sIntegration: K8sConfig{
			Enabled:            true,
			EnrichEvents:       true,
			ResolveOwnerRefs:   true,
			IncludeLabels:      true,
			IncludeAnnotations: false,
		},
	}
}

// Validate validates the configuration
func (c BaseConfig) Validate() error {
	validator := validator.New()
	return validator.Struct(c)
}

// NewBaseCollector creates a new base collector
func NewBaseCollector(name string, config BaseConfig, gatherer EventGatherer) (*BaseCollector, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}
	logger = logger.Named(name)

	// Initialize OTEL components
	tracer := otel.Tracer(fmt.Sprintf("%s-collector", name))
	meter := otel.Meter(fmt.Sprintf("%s-collector", name))

	// Create required metrics
	eventsProcessed, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription("Total number of events processed by collector"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create events processed counter: %w", err)
	}

	eventsDropped, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_dropped_total", name),
		metric.WithDescription("Total number of events dropped due to buffer full or errors"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create events dropped counter: %w", err)
	}

	errors, err := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription("Total number of errors encountered by collector"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create errors counter: %w", err)
	}

	collectorHealth, err := meter.Int64Gauge(
		fmt.Sprintf("%s_healthy", name),
		metric.WithDescription("Collector health status (1=healthy, 0=unhealthy)"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create health gauge: %w", err)
	}

	processingLatency, err := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_duration_seconds", name),
		metric.WithDescription("Time spent processing events"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create processing latency histogram: %w", err)
	}

	bufferUtilization, err := meter.Float64Gauge(
		fmt.Sprintf("%s_buffer_utilization_ratio", name),
		metric.WithDescription("Event buffer utilization ratio (0.0-1.0)"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create buffer utilization gauge: %w", err)
	}

	resourceUsage, err := meter.Float64Gauge(
		fmt.Sprintf("%s_resource_usage_ratio", name),
		metric.WithDescription("Resource usage ratio relative to limits (0.0-1.0)"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource usage gauge: %w", err)
	}

	// Initialize base collector
	collector := &BaseCollector{
		name:        name,
		config:      config,
		logger:      logger,
		events:      make(chan collectors.RawEvent, config.BufferSize),
		healthy:     true,
		startTime:   time.Now(),
		podTraceMap: make(map[string]string),
		validator:   validator.New(),

		// OTEL components
		tracer:                 tracer,
		meter:                  meter,
		eventsProcessedCtr:     eventsProcessed,
		eventsDroppedCtr:       eventsDropped,
		errorsCtr:              errors,
		collectorHealthGauge:   collectorHealth,
		processingLatencyHist:  processingLatency,
		bufferUtilizationGauge: bufferUtilization,
		resourceUsageGauge:     resourceUsage,

		// Extension points
		eventGatherer: gatherer,
	}

	// Initialize stats
	initialStats := &CollectorStats{
		StartTime:         collector.startTime,
		LastEventTime:     collector.startTime,
		EventsCollected:   0,
		EventsDropped:     0,
		ErrorCount:        0,
		UptimeSeconds:     0,
		MemoryUsageMB:     0,
		CPUUsagePercent:   0,
		GoroutineCount:    runtime.NumGoroutine(),
		BufferUtilization: 0,
		ResourceHealth:    true,
	}
	collector.stats.Store(initialStats)

	// Initialize resource monitor
	collector.resourceMonitor = newResourceMonitor(config.ResourceLimits, logger.Named("resource-monitor"))

	// Record initial health status
	collector.recordHealthStatus()

	logger.Info("Base collector initialized",
		zap.String("name", name),
		zap.Int("buffer_size", config.BufferSize),
		zap.Int("workers", config.Workers),
		zap.Duration("poll_interval", config.PollInterval),
	)

	return collector, nil
}

// Name returns collector name
func (c *BaseCollector) Name() string {
	return c.name
}

// Start begins the collector lifecycle
func (c *BaseCollector) Start(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "collector.start")
	defer span.End()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Prevent double start
	if c.ctx != nil {
		err := fmt.Errorf("collector already started")
		span.RecordError(err)
		return err
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	c.logger.Info("Starting collector", zap.String("name", c.name))

	// Initialize event gatherer
	if c.eventGatherer != nil {
		if err := c.eventGatherer.Initialize(c.ctx); err != nil {
			c.recordError("gatherer_init", err)
			span.RecordError(err)
			return fmt.Errorf("failed to initialize event gatherer: %w", err)
		}
		c.addResource(c.eventGatherer.Cleanup)
	}

	// Initialize extensions
	if err := c.initializeExtensions(c.ctx); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to initialize extensions: %w", err)
	}

	// Start worker goroutines
	for i := 0; i < c.config.Workers; i++ {
		c.wg.Add(1)
		go c.worker(i)
	}

	// Start monitoring goroutines
	c.wg.Add(1)
	go c.healthMonitor()

	c.wg.Add(1)
	go c.bufferMonitor()

	c.wg.Add(1)
	go c.resourceMonitor.start(c.ctx)

	span.SetAttributes(attribute.Bool("started", true))
	c.logger.Info("Collector started successfully", zap.String("name", c.name))
	return nil
}

// Stop gracefully shuts down the collector
func (c *BaseCollector) Stop() error {
	_, span := c.tracer.Start(context.Background(), "collector.stop")
	defer span.End()

	c.logger.Info("Stopping collector", zap.String("name", c.name))

	c.mu.Lock()
	defer c.mu.Unlock()

	// Signal shutdown
	if c.cancel != nil {
		c.cancel()
	}

	// Wait for workers to finish
	c.wg.Wait()

	// Cleanup resources
	c.cleanupResources()

	// Close events channel
	if c.events != nil {
		close(c.events)
	}

	// Update health status
	c.healthy = false
	c.recordHealthStatus()

	// Log final statistics
	stats := c.getStats()
	c.logger.Info("Collector stopped",
		zap.String("name", c.name),
		zap.Int64("events_processed", stats.EventsCollected),
		zap.Int64("events_dropped", stats.EventsDropped),
		zap.Int64("errors", stats.ErrorCount),
		zap.Float64("uptime_seconds", stats.UptimeSeconds),
	)

	span.SetAttributes(attribute.Bool("stopped", true))
	return nil
}

// Events returns the event channel
func (c *BaseCollector) Events() <-chan collectors.RawEvent {
	return c.events
}

// IsHealthy returns basic health status
func (c *BaseCollector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}

// Health returns detailed health information
func (c *BaseCollector) Health() (bool, map[string]interface{}) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := c.getStats()
	resourceUsage := c.resourceMonitor.getResourceUsage()

	health := map[string]interface{}{
		"healthy":            c.healthy,
		"uptime_seconds":     stats.UptimeSeconds,
		"events_collected":   stats.EventsCollected,
		"events_dropped":     stats.EventsDropped,
		"error_count":        stats.ErrorCount,
		"last_event_time":    stats.LastEventTime,
		"buffer_utilization": stats.BufferUtilization,
		"memory_usage_mb":    resourceUsage.MemoryMB,
		"cpu_usage_percent":  resourceUsage.CPUPercent,
		"goroutine_count":    runtime.NumGoroutine(),
		"resource_health":    c.resourceMonitor.isWithinLimits(resourceUsage),
		"extensions": map[string]interface{}{
			"gatherer_active":    c.eventGatherer != nil,
			"metadata_extractor": c.metadataExtractor != nil,
			"enricher_active":    c.eventEnricher != nil,
			"security_active":    c.securityValidator != nil,
		},
	}

	return c.healthy, health
}

// Statistics returns performance metrics
func (c *BaseCollector) Statistics() map[string]interface{} {
	stats := c.getStats()
	resourceUsage := c.resourceMonitor.getResourceUsage()

	return map[string]interface{}{
		"events_collected":   stats.EventsCollected,
		"events_dropped":     stats.EventsDropped,
		"error_count":        stats.ErrorCount,
		"uptime_seconds":     stats.UptimeSeconds,
		"last_event_time":    stats.LastEventTime,
		"buffer_utilization": stats.BufferUtilization,
		"resource_usage": map[string]interface{}{
			"memory_mb":        resourceUsage.MemoryMB,
			"cpu_percent":      resourceUsage.CPUPercent,
			"goroutines":       resourceUsage.Goroutines,
			"open_files":       resourceUsage.OpenFiles,
			"network_bytes_rx": resourceUsage.NetworkBytesRx,
			"network_bytes_tx": resourceUsage.NetworkBytesTx,
		},
		"performance": map[string]interface{}{
			"events_per_second":      c.calculateEventsPerSecond(stats),
			"error_rate":             c.calculateErrorRate(stats),
			"avg_processing_time_ms": c.calculateAvgProcessingTime(stats),
		},
	}
}

// UpdateConfig dynamically updates configuration
func (c *BaseCollector) UpdateConfig(newConfig interface{}) error {
	baseConfig, ok := newConfig.(BaseConfig)
	if !ok {
		return fmt.Errorf("invalid configuration type")
	}

	if err := baseConfig.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.config = baseConfig
	c.logger.Info("Configuration updated",
		zap.String("name", c.name),
		zap.Int("buffer_size", baseConfig.BufferSize),
		zap.Int("workers", baseConfig.Workers),
	)

	return nil
}

// GetConfig returns current configuration
func (c *BaseCollector) GetConfig() interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.config
}

// Worker processes events periodically
func (c *BaseCollector) worker(id int) {
	defer c.wg.Done()

	workerLogger := c.logger.With(zap.Int("worker_id", id))
	ticker := time.NewTicker(c.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			workerLogger.Debug("Worker stopping due to context cancellation")
			return
		case <-ticker.C:
			c.collectAndProcessEvents(workerLogger)
		}
	}
}

// collectAndProcessEvents gathers and processes events
func (c *BaseCollector) collectAndProcessEvents(logger *zap.Logger) {
	start := time.Now()
	ctx, span := c.tracer.Start(c.ctx, "collector.process_events")
	defer span.End()

	// Gather events if gatherer is available
	if c.eventGatherer == nil {
		return
	}

	rawEvents, err := c.eventGatherer.GatherEvents(ctx)
	if err != nil {
		c.recordError("gather_events", err)
		span.RecordError(err)
		logger.Error("Failed to gather events", zap.Error(err))
		return
	}

	// Process each event
	eventCount := 0
	for _, rawData := range rawEvents {
		event, err := c.createEvent(ctx, rawData)
		if err != nil {
			c.recordError("create_event", err)
			span.RecordError(err)
			continue
		}

		// Send event to channel
		select {
		case c.events <- event:
			c.recordEventProcessed()
			eventCount++
		case <-c.ctx.Done():
			return
		default:
			// Buffer full, drop event
			c.recordEventDropped("buffer_full")
			span.AddEvent("event_dropped", trace.WithAttributes(
				attribute.String("reason", "buffer_full"),
			))
		}
	}

	// Record processing metrics
	duration := time.Since(start).Seconds()
	c.processingLatencyHist.Record(ctx, duration, metric.WithAttributes(
		attribute.Int("events_processed", eventCount),
	))

	span.SetAttributes(
		attribute.Int("events_processed", eventCount),
		attribute.Float64("processing_duration_seconds", duration),
	)
}

// createEvent creates a RawEvent from raw data
func (c *BaseCollector) createEvent(ctx context.Context, rawData interface{}) (collectors.RawEvent, error) {
	ctx, span := c.tracer.Start(ctx, "collector.create_event")
	defer span.End()

	// Serialize raw data
	jsonData, err := json.Marshal(rawData)
	if err != nil {
		span.RecordError(err)
		return collectors.RawEvent{}, fmt.Errorf("failed to marshal event data: %w", err)
	}

	// Create base metadata
	metadata := map[string]string{
		"collector":   c.name,
		"timestamp":   time.Now().Format(time.RFC3339Nano),
		"source_node": os.Getenv("NODE_NAME"),
		"version":     "1.0",
	}

	// Extract K8s metadata if extractor is available
	if c.metadataExtractor != nil {
		if k8sData := c.metadataExtractor.ExtractPodMetadata(rawData); k8sData != nil {
			metadata["k8s_namespace"] = k8sData.Namespace
			metadata["k8s_name"] = k8sData.Name
			metadata["k8s_kind"] = k8sData.Kind
			metadata["k8s_uid"] = k8sData.UID

			if c.config.K8sIntegration.IncludeLabels && len(k8sData.Labels) > 0 {
				labelsJSON, _ := json.Marshal(k8sData.Labels)
				metadata["k8s_labels"] = string(labelsJSON)
			}

			if c.config.K8sIntegration.IncludeAnnotations && len(k8sData.Annotations) > 0 {
				annotationsJSON, _ := json.Marshal(k8sData.Annotations)
				metadata["k8s_annotations"] = string(annotationsJSON)
			}
		}
	}

	// Create event
	event := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      c.name,
		Data:      jsonData,
		Metadata:  metadata,
		TraceID:   span.SpanContext().TraceID().String(),
		SpanID:    span.SpanContext().SpanID().String(),
	}

	// Enrich event if enricher is available
	if c.eventEnricher != nil {
		if err := c.eventEnricher.EnrichEvent(ctx, &event); err != nil {
			span.RecordError(err)
			c.logger.Warn("Failed to enrich event", zap.Error(err))
		}
	}

	// Validate event security if validator is available
	if c.securityValidator != nil && c.config.Security.FilterSensitiveData {
		if err := c.securityValidator.ValidateEvent(&event); err != nil {
			span.RecordError(err)
			return collectors.RawEvent{}, fmt.Errorf("security validation failed: %w", err)
		}
		c.securityValidator.FilterSensitiveData(&event)
	}

	span.SetAttributes(
		attribute.String("event_type", c.name),
		attribute.Int("data_size_bytes", len(jsonData)),
	)

	return event, nil
}

// Helper methods for metrics and monitoring

func (c *BaseCollector) recordEventProcessed() {
	c.eventsProcessedCtr.Add(context.Background(), 1)
	c.updateLastEventTime()
	c.updateStats(func(stats *CollectorStats) {
		stats.EventsCollected++
	})
}

func (c *BaseCollector) recordEventDropped(reason string) {
	c.eventsDroppedCtr.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String("reason", reason),
	))
	c.updateStats(func(stats *CollectorStats) {
		stats.EventsDropped++
	})
}

func (c *BaseCollector) recordError(operation string, err error) {
	c.errorsCtr.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String("operation", operation),
		attribute.String("error_type", fmt.Sprintf("%T", err)),
	))
	c.updateStats(func(stats *CollectorStats) {
		stats.ErrorCount++
	})
	c.logger.Error("Collector error",
		zap.String("operation", operation),
		zap.Error(err),
	)
}

func (c *BaseCollector) updateLastEventTime() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastEventTime = time.Now()
}

func (c *BaseCollector) updateStats(updateFunc func(*CollectorStats)) {
	currentStats := c.getStats()
	newStats := *currentStats
	updateFunc(&newStats)
	newStats.UptimeSeconds = time.Since(c.startTime).Seconds()
	c.stats.Store(&newStats)
}

func (c *BaseCollector) getStats() *CollectorStats {
	return c.stats.Load().(*CollectorStats)
}

func (c *BaseCollector) recordHealthStatus() {
	healthValue := int64(0)
	if c.healthy {
		healthValue = 1
	}
	c.collectorHealthGauge.Record(context.Background(), healthValue)
}

// Resource management methods

func (c *BaseCollector) addResource(cleanup func() error) {
	c.resourcesMu.Lock()
	defer c.resourcesMu.Unlock()
	c.resources = append(c.resources, cleanup)
}

func (c *BaseCollector) cleanupResources() {
	c.resourcesMu.Lock()
	defer c.resourcesMu.Unlock()

	for i := len(c.resources) - 1; i >= 0; i-- {
		if err := c.resources[i](); err != nil {
			c.logger.Warn("Resource cleanup failed", zap.Error(err))
		}
	}
	c.resources = nil
}

func (c *BaseCollector) initializeExtensions(ctx context.Context) error {
	// Initialize K8s metadata extractor if enabled
	if c.config.K8sIntegration.Enabled {
		// Implementation would initialize K8s client and metadata extractor
		// This is a placeholder for the actual implementation
	}

	// Initialize security validator if enabled
	if c.config.Security.ValidatePermissions {
		// Implementation would initialize security validator
		// This is a placeholder for the actual implementation
	}

	return nil
}

// Performance calculation methods

func (c *BaseCollector) calculateEventsPerSecond(stats *CollectorStats) float64 {
	if stats.UptimeSeconds == 0 {
		return 0
	}
	return float64(stats.EventsCollected) / stats.UptimeSeconds
}

func (c *BaseCollector) calculateErrorRate(stats *CollectorStats) float64 {
	total := stats.EventsCollected + stats.EventsDropped
	if total == 0 {
		return 0
	}
	return float64(stats.ErrorCount) / float64(total)
}

func (c *BaseCollector) calculateAvgProcessingTime(stats *CollectorStats) float64 {
	// This would be calculated from histogram data in a real implementation
	return 0.0 // Placeholder
}

// Monitor goroutines

func (c *BaseCollector) healthMonitor() {
	defer c.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.updateHealthStatus()
		}
	}
}

func (c *BaseCollector) bufferMonitor() {
	defer c.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			utilization := float64(len(c.events)) / float64(cap(c.events))
			c.bufferUtilizationGauge.Record(context.Background(), utilization)

			c.updateStats(func(stats *CollectorStats) {
				stats.BufferUtilization = utilization
			})
		}
	}
}

func (c *BaseCollector) updateHealthStatus() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	stats := c.getStats()
	resourceUsage := c.resourceMonitor.getResourceUsage()

	// Health checks
	healthy := true

	// Check if we've seen events recently (if we should have)
	if !c.lastEventTime.IsZero() && now.Sub(c.lastEventTime) > 5*time.Minute {
		healthy = false
		c.logger.Warn("No recent events detected")
	}

	// Check error rate
	if stats.ErrorCount > 0 {
		total := stats.EventsCollected + stats.EventsDropped
		if total > 0 && float64(stats.ErrorCount)/float64(total) > 0.1 {
			healthy = false
			c.logger.Warn("High error rate detected", zap.Float64("error_rate", float64(stats.ErrorCount)/float64(total)))
		}
	}

	// Check buffer utilization
	if stats.BufferUtilization > 0.9 {
		healthy = false
		c.logger.Warn("Buffer nearly full", zap.Float64("utilization", stats.BufferUtilization))
	}

	// Check resource limits
	if !c.resourceMonitor.isWithinLimits(resourceUsage) {
		healthy = false
		c.logger.Warn("Resource limits exceeded",
			zap.Float64("memory_mb", resourceUsage.MemoryMB),
			zap.Float64("cpu_percent", resourceUsage.CPUPercent))
	}

	c.healthy = healthy
	c.recordHealthStatus()

	// Record resource usage metric
	memoryRatio := resourceUsage.MemoryMB / float64(c.config.ResourceLimits.MaxMemoryMB)
	c.resourceUsageGauge.Record(context.Background(), memoryRatio, metric.WithAttributes(
		attribute.String("resource_type", "memory"),
	))

	cpuRatio := resourceUsage.CPUPercent / float64(c.config.ResourceLimits.MaxCPUPercent)
	c.resourceUsageGauge.Record(context.Background(), cpuRatio, metric.WithAttributes(
		attribute.String("resource_type", "cpu"),
	))
}
