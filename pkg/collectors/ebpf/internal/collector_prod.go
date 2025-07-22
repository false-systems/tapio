package internal

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
	"github.com/yairfalse/tapio/pkg/domain"
)

// ProductionCollector is a production-hardened eBPF collector implementation
type ProductionCollector struct {
	// Configuration
	config core.Config

	// State management
	started atomic.Bool
	stopped atomic.Bool

	// Event processing
	eventChan chan domain.UnifiedEvent
	processor core.EventProcessor

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Statistics
	stats struct {
		eventsCollected atomic.Uint64
		eventsDropped   atomic.Uint64
		bytesProcessed  atomic.Uint64
		errorCount      atomic.Uint64
	}

	// Health tracking
	lastEventTime atomic.Value // time.Time
	startTime     time.Time

	// Platform-specific implementation
	impl platformImpl

	// Production hardening components
	rateLimiter *RateLimiter
}

// NewProductionCollector creates a new production-hardened eBPF collector
func NewProductionCollector(config core.Config) (core.Collector, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	c := &ProductionCollector{
		config:    config,
		eventChan: make(chan domain.UnifiedEvent, config.EventBufferSize),
		startTime: time.Now(),
		processor: newEventProcessor(),
	}

	// Initialize rate limiter
	if config.MaxEventsPerSecond > 0 {
		c.rateLimiter = NewRateLimiterSimple(int64(config.MaxEventsPerSecond))
	} else {
		c.rateLimiter = NewRateLimiterSimple(10000) // Default 10k/sec
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
func (c *ProductionCollector) Start(ctx context.Context) error {
	if !c.config.Enabled {
		return fmt.Errorf("collector is disabled")
	}

	if c.started.Load() {
		return ErrAlreadyStarted
	}

	// Create cancellable context
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Start platform implementation
	if err := c.impl.start(c.ctx); err != nil {
		return fmt.Errorf("failed to start platform implementation: %w", err)
	}

	// Mark as started
	c.started.Store(true)

	// Start event processing
	c.wg.Add(1)
	go c.processEvents()

	return nil
}

// Stop gracefully stops the collector
func (c *ProductionCollector) Stop() error {
	if !c.started.Load() {
		return ErrNotStarted
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

	// Close event channel
	close(c.eventChan)

	return nil
}

// Events returns the event channel
func (c *ProductionCollector) Events() <-chan domain.UnifiedEvent {
	return c.eventChan
}

// Health returns the current health status
func (c *ProductionCollector) Health() core.Health {
	status := core.HealthStatusHealthy
	message := "eBPF collector is healthy"

	if !c.started.Load() {
		status = core.HealthStatusUnknown
		message = "Collector not started"
	} else if c.stopped.Load() {
		status = core.HealthStatusUnhealthy
		message = "Collector stopped"
	} else if c.stats.errorCount.Load() > 100 {
		status = core.HealthStatusDegraded
		message = fmt.Sprintf("High error count: %d", c.stats.errorCount.Load())
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
		ErrorCount:      c.stats.errorCount.Load(),
		Metrics:         c.getHealthMetrics(),
	}
}

// Statistics returns runtime statistics
func (c *ProductionCollector) Statistics() core.Statistics {
	uptime := time.Since(c.startTime)

	return core.Statistics{
		StartTime:       c.startTime,
		EventsCollected: c.stats.eventsCollected.Load(),
		EventsDropped:   c.stats.eventsDropped.Load(),
		BytesProcessed:  c.stats.bytesProcessed.Load(),
		ProgramsLoaded:  c.impl.programsLoaded(),
		MapsCreated:     c.impl.mapsCreated(),
		Custom: map[string]interface{}{
			"uptime_seconds":    uptime.Seconds(),
			"events_per_second": c.getEventsPerSecond(),
			"bytes_per_second":  c.getBytesPerSecond(),
		},
	}
}

// Configure updates the collector configuration
func (c *ProductionCollector) Configure(config core.Config) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	c.config = config
	return nil
}

// processEvents processes raw events from the platform implementation
func (c *ProductionCollector) processEvents() {
	defer c.wg.Done()

	for {
		select {
		case <-c.ctx.Done():
			return

		case rawEvent, ok := <-c.impl.events():
			if !ok {
				return
			}

			// Apply rate limiting
			if !c.rateLimiter.Allow(c.ctx) {
				c.stats.eventsDropped.Add(1)
				continue
			}

			// Process the raw event
			processedEvent, err := c.processor.ProcessEvent(c.ctx, rawEvent)
			if err != nil {
				c.stats.errorCount.Add(1)
				continue
			}

			// Try to send event
			select {
			case c.eventChan <- processedEvent:
				// Event sent successfully
				c.stats.eventsCollected.Add(1)
				c.stats.bytesProcessed.Add(uint64(len(rawEvent.Data)))
				c.lastEventTime.Store(time.Now())

			case <-time.After(10 * time.Millisecond):
				// Timeout - buffer full
				c.stats.eventsDropped.Add(1)
			}
		}
	}
}

// Helper methods

func (c *ProductionCollector) getEventsPerSecond() float64 {
	uptime := time.Since(c.startTime).Seconds()
	if uptime == 0 {
		return 0
	}
	return float64(c.stats.eventsCollected.Load()) / uptime
}

func (c *ProductionCollector) getBytesPerSecond() float64 {
	uptime := time.Since(c.startTime).Seconds()
	if uptime == 0 {
		return 0
	}
	return float64(c.stats.bytesProcessed.Load()) / uptime
}

// getHealthMetrics returns comprehensive health metrics
func (c *ProductionCollector) getHealthMetrics() map[string]float64 {
	metrics := map[string]float64{
		"programs_loaded":   float64(c.impl.programsLoaded()),
		"maps_created":      float64(c.impl.mapsCreated()),
		"events_per_second": c.getEventsPerSecond(),
		"bytes_per_second":  c.getBytesPerSecond(),
	}

	// Add rate limiter metrics
	if c.rateLimiter != nil {
		rlMetrics := c.rateLimiter.GetMetrics()
		metrics["rate_limit_allowed"] = float64(rlMetrics.allowed)
		metrics["rate_limit_rejected"] = float64(rlMetrics.limited)
		metrics["rate_limit_utilization"] = rlMetrics.utilizationPct
	}

	return metrics
}
