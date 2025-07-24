package internal

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/common"
	"github.com/yairfalse/tapio/pkg/collectors/systemd/core"
	"github.com/yairfalse/tapio/pkg/domain"
)

// collector implements the core.Collector interface
type collector struct {
	// Configuration
	config core.Config

	// State management
	started atomic.Bool
	stopped atomic.Bool

	// Event processing
	eventChan chan domain.UnifiedEvent
	processor core.EventProcessor

	// Performance adapter for high-throughput event handling
	perfAdapter *common.PerformanceAdapter

	// Service watching
	watcher core.ServiceWatcher

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Statistics
	stats struct {
		eventsCollected atomic.Uint64
		eventsDropped   atomic.Uint64
		dbusCallsTotal  atomic.Uint64
		dbusErrors      atomic.Uint64
		reconnectCount  atomic.Uint64
	}

	// Health tracking
	lastEventTime atomic.Value // time.Time
	startTime     time.Time

	// Platform-specific implementation
	impl platformImpl
}

// platformImpl is the platform-specific interface
type platformImpl interface {
	init(config core.Config) error
	start(ctx context.Context) error
	stop() error
	events() <-chan core.RawEvent
	isConnected() bool
	systemdVersion() string
	servicesMonitored() int
	activeServices() int
	failedServices() int
}

// NewCollector creates a new systemd collector
func NewCollector(config core.Config) (core.Collector, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Initialize performance adapter
	perfConfig := common.DefaultPerformanceConfig("systemd")
	if config.EventBufferSize > 0 {
		// Ensure power of 2
		size := uint64(config.EventBufferSize)
		if size&(size-1) != 0 {
			// Round up to next power of 2
			size = 1
			for size < uint64(config.EventBufferSize) {
				size *= 2
			}
		}
		perfConfig.BufferSize = size
	}

	perfAdapter, err := common.NewPerformanceAdapter(perfConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create performance adapter: %w", err)
	}

	c := &collector{
		config:      config,
		eventChan:   make(chan domain.UnifiedEvent, config.EventBufferSize),
		startTime:   time.Now(),
		processor:   newEventProcessor(),
		perfAdapter: perfAdapter,
	}

	// Initialize platform-specific implementation
	impl, err := newPlatformImpl()
	if err != nil {
		return nil, fmt.Errorf("failed to create platform implementation: %w", err)
	}

	if err := impl.init(config); err != nil {
		return nil, fmt.Errorf("failed to initialize platform implementation: %w", err)
	}

	c.impl = impl
	c.lastEventTime.Store(time.Now())

	return c, nil
}

// Start begins event collection
func (c *collector) Start(ctx context.Context) error {
	if !c.config.Enabled {
		return fmt.Errorf("collector is disabled")
	}

	if c.started.Load() {
		return core.ErrAlreadyStarted
	}

	// Create cancellable context
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Start performance adapter
	if err := c.perfAdapter.Start(); err != nil {
		return fmt.Errorf("failed to start performance adapter: %w", err)
	}

	// Start platform implementation
	if err := c.impl.start(c.ctx); err != nil {
		c.perfAdapter.Stop()
		return fmt.Errorf("failed to start platform implementation: %w", err)
	}

	// Mark as started
	c.started.Store(true)

	// Start event processing
	c.wg.Add(1)
	go c.processEvents()

	// Start health monitoring
	c.wg.Add(1)
	go c.monitorHealth()

	return nil
}

// Stop gracefully stops the collector
func (c *collector) Stop() error {
	if !c.started.Load() {
		return core.ErrNotStarted
	}

	if c.stopped.Load() {
		return nil
	}

	// Mark as stopping
	c.stopped.Store(true)

	// Cancel context
	if c.cancel != nil {
		c.cancel()
	}

	// Stop platform implementation
	if err := c.impl.stop(); err != nil {
		return fmt.Errorf("failed to stop platform implementation: %w", err)
	}

	// Wait for goroutines
	c.wg.Wait()

	// Stop performance adapter
	if err := c.perfAdapter.Stop(); err != nil {
		return fmt.Errorf("failed to stop performance adapter: %w", err)
	}

	// Close event channel
	close(c.eventChan)

	return nil
}

// Events returns the event channel
func (c *collector) Events() <-chan domain.UnifiedEvent {
	// Return the performance adapter's output channel for zero-copy operation
	return c.perfAdapter.Events()
}

// Health returns the current health status
func (c *collector) Health() core.Health {
	status := core.HealthStatusHealthy
	message := "systemd collector is healthy"

	if !c.started.Load() {
		status = core.HealthStatusUnknown
		message = "Collector not started"
	} else if c.stopped.Load() {
		status = core.HealthStatusUnhealthy
		message = "Collector stopped"
	} else if !c.impl.isConnected() {
		status = core.HealthStatusUnhealthy
		message = "Not connected to D-Bus"
	} else if c.stats.dbusErrors.Load() > 100 {
		status = core.HealthStatusDegraded
		message = fmt.Sprintf("High D-Bus error count: %d", c.stats.dbusErrors.Load())
	}

	lastEvent := c.lastEventTime.Load().(time.Time)
	if time.Since(lastEvent) > 5*time.Minute && c.started.Load() {
		status = core.HealthStatusDegraded
		message = "No events received in 5 minutes"
	}

	return core.Health{
		Status:          status,
		Message:         message,
		LastEventTime:   lastEvent,
		EventsProcessed: c.stats.eventsCollected.Load(),
		EventsDropped:   c.stats.eventsDropped.Load(),
		ErrorCount:      c.stats.dbusErrors.Load(),
		DBusConnected:   c.impl.isConnected(),
		SystemdVersion:  c.impl.systemdVersion(),
		Metrics: map[string]float64{
			"services_monitored": float64(c.impl.servicesMonitored()),
			"active_services":    float64(c.impl.activeServices()),
			"failed_services":    float64(c.impl.failedServices()),
			"dbus_calls_total":   float64(c.stats.dbusCallsTotal.Load()),
			"dbus_errors":        float64(c.stats.dbusErrors.Load()),
			"reconnect_count":    float64(c.stats.reconnectCount.Load()),
			"events_per_second":  c.getEventsPerSecond(),
		},
	}
}

// Statistics returns runtime statistics
func (c *collector) Statistics() core.Statistics {
	uptime := time.Since(c.startTime)
	perfMetrics := c.perfAdapter.GetMetrics()

	return core.Statistics{
		StartTime:         c.startTime,
		EventsCollected:   c.stats.eventsCollected.Load(),
		EventsDropped:     c.stats.eventsDropped.Load(),
		ServicesMonitored: c.impl.servicesMonitored(),
		ActiveServices:    c.impl.activeServices(),
		FailedServices:    c.impl.failedServices(),
		DBusCallsTotal:    c.stats.dbusCallsTotal.Load(),
		DBusErrors:        c.stats.dbusErrors.Load(),
		ReconnectCount:    c.stats.reconnectCount.Load(),
		Custom: map[string]interface{}{
			"uptime_seconds":     uptime.Seconds(),
			"events_per_second":  c.getEventsPerSecond(),
			"connected":          c.impl.isConnected(),
			"systemd_version":    c.impl.systemdVersion(),
			"buffer_size":        perfMetrics.BufferSize,
			"buffer_capacity":    perfMetrics.BufferCapacity,
			"buffer_utilization": perfMetrics.BufferUtilization,
			"batches_processed":  perfMetrics.BatchesProcessed,
			"pool_allocated":     perfMetrics.PoolAllocated,
			"pool_recycled":      perfMetrics.PoolRecycled,
			"pool_in_use":        perfMetrics.PoolInUse,
		},
	}
}

// Configure updates the collector configuration
func (c *collector) Configure(config core.Config) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	c.config = config
	return nil
}

// processEvents processes raw events from the platform implementation
func (c *collector) processEvents() {
	defer c.wg.Done()

	for {
		select {
		case <-c.ctx.Done():
			return

		case rawEvent, ok := <-c.impl.events():
			if !ok {
				return
			}

			// Process the raw event
			event, err := c.processor.ProcessEvent(c.ctx, rawEvent)
			if err != nil {
				c.stats.dbusErrors.Add(1)
				continue
			}

			// Update stats
			c.lastEventTime.Store(time.Now())

			// Submit to performance adapter for high-throughput processing
			if err := c.perfAdapter.Submit(event); err != nil {
				c.stats.eventsDropped.Add(1)
			} else {
				c.stats.eventsCollected.Add(1)
			}
		}
	}
}

// monitorHealth monitors the connection health
func (c *collector) monitorHealth() {
	defer c.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return

		case <-ticker.C:
			// Check connection health
			if !c.impl.isConnected() {
				c.stats.reconnectCount.Add(1)
				// Platform implementation should handle reconnection
			}
		}
	}
}

// Helper methods

func (c *collector) getEventsPerSecond() float64 {
	uptime := time.Since(c.startTime).Seconds()
	if uptime == 0 {
		return 0
	}
	return float64(c.stats.eventsCollected.Load()) / uptime
}
