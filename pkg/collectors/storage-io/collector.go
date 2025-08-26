//go:build linux

package storageio

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Collector implements eBPF-based storage I/O monitoring for Kubernetes
// Focuses on VFS layer monitoring to detect storage performance issues
type Collector struct {
	// Core configuration
	name   string
	config *Config
	logger *zap.Logger

	// Event processing
	events chan *domain.CollectorEvent
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Health and lifecycle
	healthy   bool
	startTime time.Time
	mu        sync.RWMutex

	// eBPF components (platform-specific)
	ebpfState interface{}

	// K8s mount point tracking
	mountCache   map[string]*MountInfo
	mountCacheMu sync.RWMutex

	// Container correlation cache
	containerCache   map[uint64]*ContainerInfo
	containerCacheMu sync.RWMutex

	// Performance tracking
	slowIOCache   map[string]*SlowIOEvent
	slowIOCacheMu sync.RWMutex

	// OpenTelemetry instrumentation - MANDATORY pattern from CLAUDE.md
	tracer          trace.Tracer
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
	droppedEvents   metric.Int64Counter
	bufferUsage     metric.Int64Gauge

	// storage-io specific metrics
	slowIOOperations    metric.Int64Counter
	ioLatencyHistogram  metric.Float64Histogram
	k8sVolumeOperations metric.Int64Counter
	blockingIOEvents    metric.Int64Counter
	vfsOperations       metric.Int64Counter
}

// NewCollector creates a new storage-io collector with full OTEL instrumentation
func NewCollector(name string, config *Config) (*Collector, error) {
	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Initialize production logger
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize OpenTelemetry components following CLAUDE.md standards
	tracer := otel.Tracer(name)
	meter := otel.Meter(name)

	// Create metrics with descriptive names and descriptions
	eventsProcessed, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription(fmt.Sprintf("Total storage I/O events processed by %s collector", name)),
	)
	if err != nil {
		logger.Warn("Failed to create events counter", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription(fmt.Sprintf("Total errors encountered by %s collector", name)),
	)
	if err != nil {
		logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	processingTime, err := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_duration_ms", name),
		metric.WithDescription(fmt.Sprintf("Storage I/O event processing duration in milliseconds for %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	droppedEvents, err := meter.Int64Counter(
		fmt.Sprintf("%s_dropped_events_total", name),
		metric.WithDescription(fmt.Sprintf("Total storage I/O events dropped due to buffer overflow by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create dropped events counter", zap.Error(err))
	}

	bufferUsage, err := meter.Int64Gauge(
		fmt.Sprintf("%s_buffer_usage", name),
		metric.WithDescription(fmt.Sprintf("Current event buffer usage for %s collector", name)),
	)
	if err != nil {
		logger.Warn("Failed to create buffer usage gauge", zap.Error(err))
	}

	slowIOOperations, err := meter.Int64Counter(
		fmt.Sprintf("%s_slow_io_operations_total", name),
		metric.WithDescription(fmt.Sprintf("Total slow I/O operations (>%dms) detected by %s", config.SlowIOThresholdMs, name)),
	)
	if err != nil {
		logger.Warn("Failed to create slow IO operations counter", zap.Error(err))
	}

	ioLatencyHistogram, err := meter.Float64Histogram(
		fmt.Sprintf("%s_io_latency_ms", name),
		metric.WithDescription(fmt.Sprintf("I/O operation latency distribution in milliseconds for %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create IO latency histogram", zap.Error(err))
	}

	k8sVolumeOperations, err := meter.Int64Counter(
		fmt.Sprintf("%s_k8s_volume_operations_total", name),
		metric.WithDescription(fmt.Sprintf("Total Kubernetes volume operations monitored by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create K8s volume operations counter", zap.Error(err))
	}

	blockingIOEvents, err := meter.Int64Counter(
		fmt.Sprintf("%s_blocking_io_events_total", name),
		metric.WithDescription(fmt.Sprintf("Total blocking I/O events detected by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create blocking IO events counter", zap.Error(err))
	}

	vfsOperations, err := meter.Int64Counter(
		fmt.Sprintf("%s_vfs_operations_total", name),
		metric.WithDescription(fmt.Sprintf("Total VFS operations monitored by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create VFS operations counter", zap.Error(err))
	}

	return &Collector{
		name:                name,
		config:              config,
		logger:              logger.Named(name),
		events:              make(chan *domain.CollectorEvent, config.BufferSize),
		mountCache:          make(map[string]*MountInfo),
		containerCache:      make(map[uint64]*ContainerInfo),
		slowIOCache:         make(map[string]*SlowIOEvent),
		tracer:              tracer,
		eventsProcessed:     eventsProcessed,
		errorsTotal:         errorsTotal,
		processingTime:      processingTime,
		droppedEvents:       droppedEvents,
		bufferUsage:         bufferUsage,
		slowIOOperations:    slowIOOperations,
		ioLatencyHistogram:  ioLatencyHistogram,
		k8sVolumeOperations: k8sVolumeOperations,
		blockingIOEvents:    blockingIOEvents,
		vfsOperations:       vfsOperations,
	}, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.name
}

// Start begins eBPF storage I/O monitoring
func (c *Collector) Start(ctx context.Context) error {
	// Create span for startup
	ctx, span := c.tracer.Start(ctx, "storage-io.start")
	defer span.End()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ctx != nil {
		span.SetAttributes(attribute.String("error", "collector_already_started"))
		return fmt.Errorf("collector already started")
	}

	c.ctx, c.cancel = context.WithCancel(ctx)
	c.startTime = time.Now()

	// Initialize K8s mount point discovery
	if err := c.initializeK8sMountPoints(); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "k8s_mount_init_failed"),
			))
		}
		span.SetAttributes(attribute.String("error", err.Error()))
		return fmt.Errorf("failed to initialize K8s mount points: %w", err)
	}

	// Initialize eBPF monitoring
	if err := c.startEBPF(); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_init_failed"),
			))
		}
		span.SetAttributes(attribute.String("error", err.Error()))
		return fmt.Errorf("failed to start eBPF monitoring: %w", err)
	}

	// Start event processing goroutines
	c.wg.Add(1)
	go c.processStorageEvents()

	// Start mount point refresh goroutine
	c.wg.Add(1)
	go c.refreshMountPointsLoop()

	// Start health monitoring
	c.wg.Add(1)
	go c.healthMonitorLoop()

	// Start slow I/O tracking
	c.wg.Add(1)
	go c.slowIOTrackingLoop()

	c.healthy = true
	c.logger.Info("Storage-io collector started",
		zap.String("name", c.name),
		zap.Int("buffer_size", c.config.BufferSize),
		zap.Int("slow_io_threshold_ms", c.config.SlowIOThresholdMs),
		zap.Strings("monitored_k8s_paths", c.config.MonitoredK8sPaths),
		zap.Bool("enable_cgroup_correlation", c.config.EnableCgroupCorrelation),
	)

	span.SetAttributes(
		attribute.String("collector", c.name),
		attribute.Int("buffer_size", c.config.BufferSize),
		attribute.Int("slow_io_threshold_ms", c.config.SlowIOThresholdMs),
		attribute.StringSlice("monitored_k8s_paths", c.config.MonitoredK8sPaths),
	)

	return nil
}

// Stop gracefully shuts down the collector
func (c *Collector) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}

	// Wait for goroutines to finish
	c.wg.Wait()

	// Stop eBPF monitoring
	c.stopEBPF()

	// Close events channel
	close(c.events)
	c.healthy = false

	c.logger.Info("Storage-io collector stopped")
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy && c.ebpfState != nil
}

// initializeK8sMountPoints discovers and caches Kubernetes-relevant mount points
func (c *Collector) initializeK8sMountPoints() error {
	c.logger.Info("Initializing Kubernetes mount point discovery")

	// Discover K8s mount points
	mounts, err := discoverK8sMountPoints(c.config.MonitoredK8sPaths)
	if err != nil {
		return fmt.Errorf("failed to discover K8s mount points: %w", err)
	}

	c.mountCacheMu.Lock()
	defer c.mountCacheMu.Unlock()

	for _, mount := range mounts {
		c.mountCache[mount.Path] = mount
		c.logger.Debug("Cached K8s mount point",
			zap.String("path", mount.Path),
			zap.String("type", mount.Type),
			zap.String("k8s_volume_type", mount.K8sVolumeType),
		)
	}

	c.logger.Info("K8s mount point discovery completed",
		zap.Int("mount_points_cached", len(c.mountCache)),
	)

	return nil
}

// refreshMountPointsLoop periodically refreshes the mount point cache
func (c *Collector) refreshMountPointsLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.MountRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if err := c.initializeK8sMountPoints(); err != nil {
				c.logger.Warn("Failed to refresh mount points", zap.Error(err))
				if c.errorsTotal != nil {
					c.errorsTotal.Add(c.ctx, 1, metric.WithAttributes(
						attribute.String("error_type", "mount_refresh_failed"),
					))
				}
			}
		}
	}
}

// healthMonitorLoop monitors collector health and updates metrics
func (c *Collector) healthMonitorLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.updateHealthMetrics()
		}
	}
}

// updateHealthMetrics updates health and performance metrics
func (c *Collector) updateHealthMetrics() {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Update buffer utilization
	if c.bufferUsage != nil {
		bufferUsage := int64(len(c.events))
		c.bufferUsage.Record(context.Background(), bufferUsage, metric.WithAttributes(
			attribute.String("collector", c.name),
		))
	}
}

// slowIOTrackingLoop manages slow I/O event tracking and cleanup
func (c *Collector) slowIOTrackingLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.cleanupSlowIOCache()
		}
	}
}

// cleanupSlowIOCache removes old entries from the slow I/O cache
func (c *Collector) cleanupSlowIOCache() {
	c.slowIOCacheMu.Lock()
	defer c.slowIOCacheMu.Unlock()

	now := time.Now()
	cutoff := now.Add(-5 * time.Minute)

	for key, event := range c.slowIOCache {
		if event.Timestamp.Before(cutoff) {
			delete(c.slowIOCache, key)
		}
	}
}

// processStorageEvent processes a single storage I/O event from eBPF
func (c *Collector) processStorageEvent(rawEvent *StorageIOEvent) error {
	ctx, span := c.tracer.Start(c.ctx, "storage-io.process_event")
	defer span.End()

	start := time.Now()

	// Apply filtering
	if !c.shouldProcessEvent(rawEvent) {
		span.SetAttributes(attribute.String("skipped", "filtered"))
		return nil
	}

	// Enrich with K8s context
	enrichedEvent, err := c.enrichWithK8sContext(rawEvent)
	if err != nil {
		span.SetAttributes(attribute.String("error", "k8s_enrichment_failed"))
		c.logger.Warn("Failed to enrich event with K8s context", zap.Error(err))
		// Continue processing without K8s enrichment
		enrichedEvent = rawEvent
	}

	// Convert to domain.CollectorEvent
	collectorEvent, err := c.convertToCollectorEvent(enrichedEvent)
	if err != nil {
		span.SetAttributes(attribute.String("error", "conversion_failed"))
		return fmt.Errorf("failed to convert to collector event: %w", err)
	}

	// Set span attributes for debugging
	span.SetAttributes(
		attribute.String("operation", enrichedEvent.Operation),
		attribute.String("path", enrichedEvent.Path),
		attribute.Int64("size", enrichedEvent.Size),
		attribute.Float64("duration_ms", float64(enrichedEvent.Duration.Nanoseconds())/1e6),
		attribute.Bool("slow_io", enrichedEvent.Duration > time.Duration(c.config.SlowIOThresholdMs)*time.Millisecond),
	)

	// Send event
	select {
	case c.events <- collectorEvent:
		// Record success metrics
		if c.eventsProcessed != nil {
			c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("operation", enrichedEvent.Operation),
				attribute.String("k8s_volume_type", enrichedEvent.K8sVolumeType),
			))
		}

		// Record VFS operation metrics
		if c.vfsOperations != nil {
			c.vfsOperations.Add(ctx, 1, metric.WithAttributes(
				attribute.String("vfs_layer", enrichedEvent.VFSLayer),
				attribute.String("operation", enrichedEvent.Operation),
			))
		}

		// Record K8s volume operation metrics
		if enrichedEvent.K8sVolumeType != "" && c.k8sVolumeOperations != nil {
			c.k8sVolumeOperations.Add(ctx, 1, metric.WithAttributes(
				attribute.String("volume_type", enrichedEvent.K8sVolumeType),
				attribute.String("operation", enrichedEvent.Operation),
			))
		}

		// Record I/O latency
		latencyMs := float64(enrichedEvent.Duration.Nanoseconds()) / 1e6
		if c.ioLatencyHistogram != nil {
			c.ioLatencyHistogram.Record(ctx, latencyMs, metric.WithAttributes(
				attribute.String("operation", enrichedEvent.Operation),
				attribute.String("device", enrichedEvent.Device),
			))
		}

		// Track slow I/O operations
		if enrichedEvent.Duration > time.Duration(c.config.SlowIOThresholdMs)*time.Millisecond {
			if c.slowIOOperations != nil {
				c.slowIOOperations.Add(ctx, 1, metric.WithAttributes(
					attribute.String("operation", enrichedEvent.Operation),
					attribute.String("path", enrichedEvent.Path),
				))
			}
			c.trackSlowIOEvent(enrichedEvent)
		}

		// Track blocking I/O events
		if enrichedEvent.BlockedIO && c.blockingIOEvents != nil {
			c.blockingIOEvents.Add(ctx, 1, metric.WithAttributes(
				attribute.String("operation", enrichedEvent.Operation),
				attribute.String("path", enrichedEvent.Path),
			))
		}

		// Record processing time
		processingDuration := time.Since(start).Seconds() * 1000 // Convert to milliseconds
		if c.processingTime != nil {
			c.processingTime.Record(ctx, processingDuration, metric.WithAttributes(
				attribute.String("operation", enrichedEvent.Operation),
			))
		}

	case <-c.ctx.Done():
		return nil

	default:
		// Buffer full - drop event
		if c.droppedEvents != nil {
			c.droppedEvents.Add(ctx, 1, metric.WithAttributes(
				attribute.String("reason", "buffer_full"),
			))
		}
		span.SetAttributes(attribute.String("dropped", "buffer_full"))
		c.logger.Warn("Event buffer full, dropping storage I/O event",
			zap.String("operation", enrichedEvent.Operation),
			zap.String("path", enrichedEvent.Path))
	}

	return nil
}

// shouldProcessEvent determines if an event should be processed based on filtering rules
func (c *Collector) shouldProcessEvent(event *StorageIOEvent) bool {
	// Always process slow I/O events
	if event.Duration > time.Duration(c.config.SlowIOThresholdMs)*time.Millisecond {
		return true
	}

	// Check if path matches K8s monitoring patterns
	c.mountCacheMu.RLock()
	defer c.mountCacheMu.RUnlock()

	for mountPath := range c.mountCache {
		if matchesPath(event.Path, mountPath) {
			return true
		}
	}

	// Apply sampling for non-K8s paths if configured
	if c.config.SamplingRate > 0 && c.config.SamplingRate < 1.0 {
		// Simple hash-based sampling
		return hashPath(event.Path)%100 < int(c.config.SamplingRate*100)
	}

	return c.config.SamplingRate >= 1.0
}

// convertToCollectorEvent converts internal event to domain.CollectorEvent
func (c *Collector) convertToCollectorEvent(event *StorageIOEvent) (*domain.CollectorEvent, error) {
	eventID := uuid.New().String()

	// Create StorageIOData
	storageIOData := &domain.StorageIOData{
		Operation:    event.Operation,
		Path:         event.Path,
		Size:         event.Size,
		Offset:       event.Offset,
		Duration:     event.Duration,
		SlowIO:       event.Duration > time.Duration(c.config.SlowIOThresholdMs)*time.Millisecond,
		BlockedIO:    event.BlockedIO,
		Device:       event.Device,
		Inode:        event.Inode,
		FileSystem:   event.FileSystem,
		MountPoint:   event.MountPoint,
		K8sPath:      event.K8sPath,
		VolumeType:   event.K8sVolumeType,
		PVCName:      event.PVCName,
		StorageClass: event.StorageClass,
		ErrorCode:    event.ErrorCode,
		ErrorMessage: event.ErrorMessage,
		VFSLayer:     event.VFSLayer,
		Flags:        event.Flags,
		Mode:         event.Mode,
		CPUTime:      event.CPUTime,
		QueueTime:    event.QueueTime,
		BlockTime:    event.BlockTime,
		LatencyMS:    float64(event.Duration.Nanoseconds()) / 1e6,
	}

	// Use generic storage IO type for all events
	eventType := domain.EventTypeStorageIO

	// Determine severity based on duration
	var severity domain.EventSeverity
	if event.Duration > time.Duration(c.config.SlowIOThresholdMs*10)*time.Millisecond {
		severity = domain.EventSeverityCritical
	} else if event.Duration > time.Duration(c.config.SlowIOThresholdMs*5)*time.Millisecond {
		severity = domain.EventSeverityError
	} else if event.Duration > time.Duration(c.config.SlowIOThresholdMs)*time.Millisecond {
		severity = domain.EventSeverityWarning
	} else {
		severity = domain.EventSeverityInfo
	}

	// Create CollectorEvent
	collectorEvent := &domain.CollectorEvent{
		EventID:   eventID,
		Timestamp: event.Timestamp,
		Type:      eventType,
		Source:    c.name,
		Severity:  severity,
		EventData: domain.EventDataContainer{
			StorageIO: storageIOData,
		},
		Metadata: domain.EventMetadata{
			// Kubernetes context
			PodUID:      event.PodUID,
			ContainerID: event.ContainerID,

			// Process context
			PID:      event.PID,
			PPID:     event.PPID,
			UID:      event.UID,
			GID:      event.GID,
			Command:  event.Command,
			CgroupID: event.CgroupID,

			// Priority based on slow I/O
			Priority: c.determinePriority(event),

			// Tags for categorization
			Tags: c.generateTags(event),

			// Correlation hints
			CorrelationHints: []string{
				fmt.Sprintf("cgroup:%d", event.CgroupID),
				fmt.Sprintf("mount:%s", event.MountPoint),
				fmt.Sprintf("device:%s", event.Device),
			},
		},
	}

	return collectorEvent, nil
}

// determinePriority determines event priority based on characteristics
func (c *Collector) determinePriority(event *StorageIOEvent) domain.EventPriority {
	// Critical: Very slow I/O or errors
	if event.Duration > time.Duration(c.config.SlowIOThresholdMs*10)*time.Millisecond || event.ErrorCode != 0 {
		return domain.PriorityCritical
	}

	// High: Slow I/O or blocked operations
	if event.SlowIO || event.BlockedIO {
		return domain.PriorityHigh
	}

	// Normal: Kubernetes critical paths
	if isK8sCriticalPath(event.Path) {
		return domain.PriorityNormal
	}

	// Low: Everything else
	return domain.PriorityLow
}

// generateTags generates tags for the event
func (c *Collector) generateTags(event *StorageIOEvent) []string {
	tags := []string{"storage-io"}

	// Add operation tag
	tags = append(tags, event.Operation)

	// Add slow-io tag if applicable
	if event.SlowIO {
		tags = append(tags, "slow-io")
	}

	// Add blocked-io tag if applicable
	if event.BlockedIO {
		tags = append(tags, "blocked-io")
	}

	// Add kubernetes tag if it's a K8s path
	if isK8sCriticalPath(event.Path) {
		tags = append(tags, "kubernetes")
	}

	// Add volume type tag if available
	if event.K8sVolumeType != "" {
		tags = append(tags, fmt.Sprintf("volume:%s", event.K8sVolumeType))
	}

	return tags
}

// Helper methods for event processing

func (c *Collector) trackSlowIOEvent(event *StorageIOEvent) {
	c.slowIOCacheMu.Lock()
	defer c.slowIOCacheMu.Unlock()

	key := fmt.Sprintf("%s:%s:%d", event.Operation, event.Path, event.PID)
	c.slowIOCache[key] = &SlowIOEvent{
		Operation: event.Operation,
		Path:      event.Path,
		PID:       event.PID,
		Duration:  event.Duration,
		Timestamp: event.Timestamp,
	}
}

// enrichWithK8sContext enriches the storage event with Kubernetes context
func (c *Collector) enrichWithK8sContext(event *StorageIOEvent) (*StorageIOEvent, error) {
	// Look up mount point information
	c.mountCacheMu.RLock()
	mountInfo := c.findMatchingMount(event.Path)
	c.mountCacheMu.RUnlock()

	if mountInfo != nil {
		event.K8sPath = mountInfo.Path
		event.K8sVolumeType = mountInfo.K8sVolumeType
		event.PodUID = mountInfo.PodUID
		event.MountPoint = mountInfo.Path
	}

	// Look up container information if cgroup correlation is enabled
	if c.config.EnableCgroupCorrelation && event.CgroupID != 0 {
		c.containerCacheMu.RLock()
		containerInfo, exists := c.containerCache[event.CgroupID]
		c.containerCacheMu.RUnlock()

		if exists {
			event.ContainerID = containerInfo.ContainerID
			if event.PodUID == "" {
				event.PodUID = containerInfo.PodUID
			}
		}
	}

	return event, nil
}

// findMatchingMount finds the mount point that best matches the given path
func (c *Collector) findMatchingMount(path string) *MountInfo {
	var bestMatch *MountInfo
	maxMatchLength := 0

	for mountPath, mountInfo := range c.mountCache {
		if len(path) >= len(mountPath) && path[:len(mountPath)] == mountPath {
			if len(mountPath) > maxMatchLength {
				bestMatch = mountInfo
				maxMatchLength = len(mountPath)
			}
		}
	}

	return bestMatch
}

// Utility functions

// matchesPath checks if a path matches a pattern (supports prefix matching)
func matchesPath(path, pattern string) bool {
	if len(path) < len(pattern) {
		return false
	}
	return path[:len(pattern)] == pattern
}

// hashPath creates a simple hash for path-based sampling
func hashPath(path string) int {
	hash := 0
	for _, c := range path {
		hash = hash*31 + int(c)
	}
	if hash < 0 {
		hash = -hash
	}
	return hash
}

// Platform-specific functions (implemented in platform-specific files)

// startEBPF starts the eBPF monitoring (implemented in collector_linux.go)
func (c *Collector) startEBPF() error {
	return c.startEBPFImpl()
}

// stopEBPF stops the eBPF monitoring (implemented in collector_linux.go)
func (c *Collector) stopEBPF() {
	c.stopEBPFImpl()
}

// processStorageEvents processes storage events from eBPF (implemented in collector_linux.go)
func (c *Collector) processStorageEvents() {
	c.processStorageEventsImpl()
}

// discoverK8sMountPoints discovers Kubernetes mount points (implemented in mount_discovery.go)
func discoverK8sMountPoints(monitoredPaths []string) ([]*MountInfo, error) {
	return discoverK8sMountPointsImpl(monitoredPaths)
}
