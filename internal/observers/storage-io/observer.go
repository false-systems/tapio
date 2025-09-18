package storageio

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

// Observer implements eBPF-based storage I/O monitoring
// Focuses on VFS layer monitoring to detect storage performance issues
type Observer struct {
	*base.BaseObserver        // Embed for stats/health
	*base.EventChannelManager // Embed for events
	*base.LifecycleManager    // Embed for lifecycle

	// Core configuration
	config *Config
	logger *zap.Logger
	name   string

	// Runtime environment detection
	runtime *RuntimeEnvironment

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

	// OpenTelemetry instrumentation
	tracer              trace.Tracer
	slowIOOperations    metric.Int64Counter
	ioLatencyHistogram  metric.Float64Histogram
	k8sVolumeOperations metric.Int64Counter
	blockingIOEvents    metric.Int64Counter
	vfsOperations       metric.Int64Counter
	eventsProcessed     metric.Int64Counter
	errorsTotal         metric.Int64Counter
}

// NewObserver creates a new storage-io observer
func NewObserver(name string, config *Config) (*Observer, error) {
	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Initialize production logger
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize OpenTelemetry
	tracer := otel.Tracer(name)
	meter := otel.Meter(name)

	// Create metrics
	slowIOOperations, err := meter.Int64Counter(
		fmt.Sprintf("%s_slow_io_operations_total", name),
		metric.WithDescription(fmt.Sprintf("Total slow I/O operations (>%dms) detected", config.SlowIOThresholdMs)),
	)
	if err != nil {
		logger.Warn("Failed to create slow IO operations counter", zap.Error(err))
	}

	ioLatencyHistogram, err := meter.Float64Histogram(
		fmt.Sprintf("%s_io_latency_ms", name),
		metric.WithDescription("I/O operation latency distribution in milliseconds"),
	)
	if err != nil {
		logger.Warn("Failed to create IO latency histogram", zap.Error(err))
	}

	k8sVolumeOperations, err := meter.Int64Counter(
		fmt.Sprintf("%s_k8s_volume_operations_total", name),
		metric.WithDescription("Total Kubernetes volume operations monitored"),
	)
	if err != nil {
		logger.Warn("Failed to create K8s volume operations counter", zap.Error(err))
	}

	blockingIOEvents, err := meter.Int64Counter(
		fmt.Sprintf("%s_blocking_io_events_total", name),
		metric.WithDescription("Total blocking I/O events detected"),
	)
	if err != nil {
		logger.Warn("Failed to create blocking IO events counter", zap.Error(err))
	}

	vfsOperations, err := meter.Int64Counter(
		fmt.Sprintf("%s_vfs_operations_total", name),
		metric.WithDescription("Total VFS operations monitored"),
	)
	if err != nil {
		logger.Warn("Failed to create VFS operations counter", zap.Error(err))
	}

	eventsProcessed, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription("Total events processed"),
	)
	if err != nil {
		logger.Warn("Failed to create events processed counter", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription("Total errors in observer"),
	)
	if err != nil {
		logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	// Detect runtime environment
	runtime, err := DetectRuntimeEnvironment()
	if err != nil {
		logger.Warn("Failed to detect runtime environment, using defaults", zap.Error(err))
		runtime = &RuntimeEnvironment{
			IsKubernetes:       false,
			VolumePathPatterns: make(map[string]string),
		}
	}

	// Override configured paths with detected paths if available
	if runtime.IsKubernetes && len(runtime.GetMonitoredPaths()) > 0 {
		config.MonitoredK8sPaths = runtime.GetMonitoredPaths()
	}

	o := &Observer{
		BaseObserver:        base.NewBaseObserver(name, 5*time.Minute),
		EventChannelManager: base.NewEventChannelManager(config.BufferSize, name, logger.Named(name)),
		LifecycleManager:    base.NewLifecycleManager(context.Background(), logger.Named(name)),
		config:              config,
		logger:              logger.Named(name),
		name:                name,
		runtime:             runtime,
		mountCache:          make(map[string]*MountInfo),
		containerCache:      make(map[uint64]*ContainerInfo),
		slowIOCache:         make(map[string]*SlowIOEvent),
		tracer:              tracer,
		slowIOOperations:    slowIOOperations,
		ioLatencyHistogram:  ioLatencyHistogram,
		k8sVolumeOperations: k8sVolumeOperations,
		blockingIOEvents:    blockingIOEvents,
		vfsOperations:       vfsOperations,
		eventsProcessed:     eventsProcessed,
		errorsTotal:         errorsTotal,
	}

	return o, nil
}

// Name returns the observer name
func (o *Observer) Name() string {
	return o.name
}

// Start begins monitoring storage I/O
func (o *Observer) Start(ctx context.Context) error {
	o.logger.Info("Starting storage I/O observer",
		zap.Bool("ebpf", o.config.EnableEBPF),
		zap.Bool("k8s", o.config.EnableK8sIntegration),
		zap.Int("slowThresholdMs", o.config.SlowIOThresholdMs),
		zap.Strings("monitoredPaths", o.config.MonitoredK8sPaths),
	)

	// Start eBPF if enabled (platform-specific)
	if o.config.EnableEBPF {
		if err := o.startEBPF(); err != nil {
			return fmt.Errorf("failed to start eBPF: %w", err)
		}
	}

	// Start mount discovery if K8s integration is enabled
	if o.config.EnableK8sIntegration {
		o.LifecycleManager.Start("mount-discovery", func() {
			o.discoverMounts()
		})
	}

	// Start cache cleanup
	o.LifecycleManager.Start("cache-cleanup", func() {
		o.cleanupCaches()
	})

	// Start event processor
	o.LifecycleManager.Start("event-processor", func() {
		o.processEvents()
	})

	o.BaseObserver.SetHealthy(true)
	o.logger.Info("Storage I/O observer started successfully")
	return nil
}

// Stop stops the observer
func (o *Observer) Stop() error {
	o.logger.Info("Stopping storage I/O observer")

	// Stop eBPF if running
	if o.config.EnableEBPF {
		o.stopEBPF()
	}

	// Stop goroutines
	if err := o.LifecycleManager.Stop(5 * time.Second); err != nil {
		o.logger.Warn("Timeout during shutdown", zap.Error(err))
	}

	// Close event channel
	o.EventChannelManager.Close()

	o.BaseObserver.SetHealthy(false)
	o.logger.Info("Storage I/O observer stopped")
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

// discoverMounts discovers and monitors Kubernetes mount points
func (o *Observer) discoverMounts() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-o.LifecycleManager.Context().Done():
			return
		case <-ticker.C:
			mounts, err := DiscoverK8sMounts()
			if err != nil {
				o.logger.Warn("Failed to discover mounts", zap.Error(err))
				continue
			}

			o.mountCacheMu.Lock()
			for _, mount := range mounts {
				o.mountCache[mount.Path] = mount
			}
			o.mountCacheMu.Unlock()
		}
	}
}

// cleanupCaches periodically cleans up stale cache entries
func (o *Observer) cleanupCaches() {
	ticker := time.NewTicker(o.config.CacheCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-o.LifecycleManager.Context().Done():
			return
		case <-ticker.C:
			now := time.Now()

			// Clean slow IO cache
			o.slowIOCacheMu.Lock()
			for path, event := range o.slowIOCache {
				if now.Sub(event.LastSeen) > 10*time.Minute {
					delete(o.slowIOCache, path)
				}
			}
			o.slowIOCacheMu.Unlock()

			// Clean container cache
			o.containerCacheMu.Lock()
			// Simple cleanup - in production would be more sophisticated
			if len(o.containerCache) > 1000 {
				o.containerCache = make(map[uint64]*ContainerInfo)
			}
			o.containerCacheMu.Unlock()
		}
	}
}

// processEvents processes storage I/O events
func (o *Observer) processEvents() {
	// Platform-specific implementation
	o.processEventsImpl()
}
