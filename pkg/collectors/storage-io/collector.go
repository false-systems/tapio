package storageio

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// Collector implements eBPF-based storage I/O monitoring using BaseCollector
// Focuses on VFS layer monitoring to detect storage performance issues
type Collector struct {
	*base.BaseCollector      // Embed for stats/health
	*base.EventChannelManager // Embed for events
	*base.LifecycleManager    // Embed for lifecycle
	
	// Core configuration
	config *Config
	logger *zap.Logger

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

	// Storage-specific metrics (beyond BaseCollector)
	slowIOOperations    metric.Int64Counter
	ioLatencyHistogram  metric.Float64Histogram
	k8sVolumeOperations metric.Int64Counter
	blockingIOEvents    metric.Int64Counter
	vfsOperations       metric.Int64Counter
}

// NewCollector creates a new storage-io collector with BaseCollector integration
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

	// Create BaseCollector with 5-minute health check timeout
	baseConfig := base.BaseCollectorConfig{
		Name:               name,
		HealthCheckTimeout: 5 * time.Minute,
		ErrorRateThreshold: 0.05, // 5% error rate threshold for storage collector
	}
	baseCollector := base.NewBaseCollectorWithConfig(baseConfig)

	// Get the meter from BaseCollector for consistency
	meter := baseCollector.GetMeter()

	// Create storage-specific metrics using the same meter
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

	// Detect runtime environment
	runtime, err := DetectRuntimeEnvironment()
	if err != nil {
		logger.Warn("Failed to detect runtime environment, using defaults", zap.Error(err))
		runtime = &RuntimeEnvironment{
			IsKubernetes: false,
			VolumePathPatterns: make(map[string]string),
		}
	}

	// Override configured paths with detected paths if available
	if runtime.IsKubernetes && len(runtime.GetMonitoredPaths()) > 0 {
		config.MonitoredK8sPaths = runtime.GetMonitoredPaths()
	}

	// Create lifecycle manager first as it's needed for context
	lifecycleManager := base.NewLifecycleManager(context.Background(), logger.Named(name))
	
	// Create event channel manager
	eventManager := base.NewEventChannelManager(config.BufferSize, name, logger.Named(name))
	
	c := &Collector{
		BaseCollector:       baseCollector,
		EventChannelManager: eventManager,
		LifecycleManager:    lifecycleManager,
		config:              config,
		logger:              logger.Named(name),
		runtime:             runtime,
		mountCache:          make(map[string]*MountInfo),
		containerCache:      make(map[uint64]*ContainerInfo),
		slowIOCache:         make(map[string]*SlowIOEvent),
		slowIOOperations:    slowIOOperations,
		ioLatencyHistogram:  ioLatencyHistogram,
		k8sVolumeOperations: k8sVolumeOperations,
		blockingIOEvents:    blockingIOEvents,
		vfsOperations:       vfsOperations,
	}

	c.logger.Info("Storage I/O collector created",
		zap.String("name", c.GetName()),
		zap.Int("buffer_size", config.BufferSize),
		zap.Int("slow_io_threshold_ms", config.SlowIOThresholdMs),
		zap.Bool("enable_vfs_read", config.EnableVFSRead),
		zap.Bool("enable_vfs_write", config.EnableVFSWrite),
		zap.Bool("enable_block_io", config.EnableBlockIO),
		zap.Bool("kubernetes_detected", runtime.IsKubernetes),
		zap.String("container_runtime", runtime.ContainerRuntime),
		zap.String("kubelet_dir", runtime.KubeletRootDir),
		zap.Strings("monitored_paths", config.MonitoredK8sPaths),
	)

	return c, nil
}

// Name returns collector name (required by Collector interface)
func (c *Collector) Name() string {
	return c.GetName()
}

// Start starts the storage I/O monitoring
func (c *Collector) Start(ctx context.Context) error {
	ctx, span := c.StartSpan(ctx, "storage-io.collector.start")
	defer span.End()

	// Start eBPF monitoring
	if err := c.startEBPF(); err != nil {
		c.RecordErrorWithContext(ctx, err)
		return fmt.Errorf("failed to start eBPF: %w", err)
	}

	// Platform-specific event processing and metrics will be started by startEBPF

	// Start mount discovery goroutine using lifecycle manager
	c.LifecycleManager.Start("mount-discovery", c.discoverMounts)

	c.SetHealthy(true)
	c.logger.Info("Storage I/O collector started successfully")

	return nil
}

// Stop stops the collector
func (c *Collector) Stop() error {
	c.logger.Info("Stopping storage I/O collector")

	// Stop lifecycle manager with 5 second timeout
	if err := c.LifecycleManager.Stop(5 * time.Second); err != nil {
		c.logger.Warn("Lifecycle manager stop timeout", zap.Error(err))
	}

	c.stopEBPF()
	c.EventChannelManager.Close()
	c.SetHealthy(false)

	c.logger.Info("Storage I/O collector stopped successfully")
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.GetChannel()
}

// IsHealthy returns health status (delegates to BaseCollector)
func (c *Collector) IsHealthy() bool {
	return c.BaseCollector.IsHealthy()
}

// discoverMounts periodically discovers Kubernetes volume mounts
func (c *Collector) discoverMounts() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.StopChannel():
			return
		case <-ticker.C:
			if err := c.updateMountCache(); err != nil {
				c.RecordError(err)
				c.logger.Warn("Failed to update mount cache", zap.Error(err))
			}
		}
	}
}

// updateMountCache updates the mount point cache
func (c *Collector) updateMountCache() error {
	// This would normally scan /proc/mounts and correlate with K8s volumes
	// For now, just a placeholder
	c.logger.Debug("Updating mount cache")
	return nil
}

// GetMountInfo returns mount information for a given path
func (c *Collector) GetMountInfo(path string) *MountInfo {
	c.mountCacheMu.RLock()
	defer c.mountCacheMu.RUnlock()
	return c.mountCache[path]
}

// AddContainerInfo adds container information to the cache
func (c *Collector) AddContainerInfo(cgroupID uint64, info *ContainerInfo) {
	c.containerCacheMu.Lock()
	defer c.containerCacheMu.Unlock()
	c.containerCache[cgroupID] = info
}

// GetContainerInfo retrieves container information from the cache
func (c *Collector) GetContainerInfo(cgroupID uint64) *ContainerInfo {
	c.containerCacheMu.RLock()
	defer c.containerCacheMu.RUnlock()
	return c.containerCache[cgroupID]
}

// RecordSlowIO records a slow I/O operation
func (c *Collector) RecordSlowIO(event *SlowIOEvent) {
	c.slowIOCacheMu.Lock()
	defer c.slowIOCacheMu.Unlock()
	
	key := fmt.Sprintf("%s-%d", event.Path, event.PID)
	c.slowIOCache[key] = event
	
	// Clean up old entries if cache gets too large
	if len(c.slowIOCache) > 1000 {
		// Remove oldest entries
		for k := range c.slowIOCache {
			delete(c.slowIOCache, k)
			if len(c.slowIOCache) <= 800 {
				break
			}
		}
	}
}