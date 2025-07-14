package journald

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/logging"
)

// Collector implements the journald collector for OPINIONATED log parsing
// focused on critical system events that matter for Kubernetes debugging
type Collector struct {
	// Configuration
	config collectors.CollectorConfig
	logger *logging.Logger

	// Core components
	reader         *Reader
	parser         *Parser
	oomDetector    *OOMDetector
	containerParser *ContainerEventParser
	filter         *SmartFilter
	enricher       *SemanticEnricher

	// Event channel
	eventChan chan *collectors.Event

	// State management
	started atomic.Bool
	stopped atomic.Bool
	enabled atomic.Bool

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Statistics
	stats struct {
		eventsCollected  atomic.Uint64
		eventsDropped    atomic.Uint64
		eventsFiltered   atomic.Uint64
		oomKillsDetected atomic.Uint64
		containersFailures atomic.Uint64
		errorCount       atomic.Uint64
		bytesProcessed   atomic.Uint64
	}

	// Performance tracking
	lastEventTime   atomic.Value // time.Time
	processingTime  atomic.Int64
}

// NewCollector creates a new journald collector with OPINIONATED parsing
func NewCollector(config collectors.CollectorConfig) (*Collector, error) {
	logger := logging.WithComponent("journald-collector")

	// Extract journald-specific config
	journaldConfig, err := extractJournaldConfig(config.Extra)
	if err != nil {
		return nil, fmt.Errorf("invalid journald configuration: %w", err)
	}

	c := &Collector{
		config:    config,
		logger:    logger,
		eventChan: make(chan *collectors.Event, config.EventBufferSize),
		enabled:   atomic.Bool{},
	}

	// Initialize as enabled based on config
	c.enabled.Store(config.Enabled)

	// Initialize components
	c.reader = NewReader(journaldConfig)
	c.parser = NewParser()
	c.oomDetector = NewOOMDetector()
	c.containerParser = NewContainerEventParser()
	c.filter = NewSmartFilter(journaldConfig)
	c.enricher = NewSemanticEnricher()

	// Set initial last event time
	c.lastEventTime.Store(time.Now())

	return c, nil
}

// Name returns the collector name
func (c *Collector) Name() string {
	return c.config.Name
}

// Type returns the collector type
func (c *Collector) Type() string {
	return "journald"
}

// Start begins collecting journald events
func (c *Collector) Start(ctx context.Context) error {
	if !c.enabled.Load() {
		return fmt.Errorf("journald collector is disabled")
	}

	if c.started.Load() {
		return fmt.Errorf("journald collector already started")
	}

	c.logger.Info("Starting journald collector",
		"filters", c.filter.GetActiveFilters(),
		"enrichment", "enabled",
	)

	// Create cancellable context
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Start reader
	if err := c.reader.Start(c.ctx); err != nil {
		return fmt.Errorf("failed to start journald reader: %w", err)
	}

	// Mark as started
	c.started.Store(true)

	// Start event processing
	c.wg.Add(1)
	go c.processEvents()

	// Start statistics reporting
	c.wg.Add(1)
	go c.reportStatistics()

	c.logger.Info("Journald collector started successfully")
	return nil
}

// Stop halts the collector
func (c *Collector) Stop() error {
	if !c.started.Load() {
		return fmt.Errorf("journald collector not started")
	}

	if c.stopped.Load() {
		return fmt.Errorf("journald collector already stopped")
	}

	c.logger.Info("Stopping journald collector")

	// Mark as stopping
	c.stopped.Store(true)

	// Cancel context
	if c.cancel != nil {
		c.cancel()
	}

	// Stop reader
	if err := c.reader.Stop(); err != nil {
		c.logger.Error("Failed to stop journald reader", "error", err)
	}

	// Wait for goroutines
	c.wg.Wait()

	// Close event channel
	close(c.eventChan)

	c.logger.Info("Journald collector stopped",
		"events_collected", c.stats.eventsCollected.Load(),
		"events_filtered", c.stats.eventsFiltered.Load(),
		"oom_kills_detected", c.stats.oomKillsDetected.Load(),
	)

	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan *collectors.Event {
	return c.eventChan
}

// Health returns the collector health status
func (c *Collector) Health() *collectors.Health {
	status := collectors.HealthStatusHealthy
	message := "Journald collector is healthy"
	
	if !c.started.Load() {
		status = collectors.HealthStatusUnknown
		message = "Collector not started"
	} else if c.stopped.Load() {
		status = collectors.HealthStatusUnhealthy
		message = "Collector stopped"
	} else if !c.reader.IsHealthy() {
		status = collectors.HealthStatusUnhealthy
		message = "Journald reader unhealthy"
	} else if c.stats.errorCount.Load() > 100 {
		status = collectors.HealthStatusDegraded
		message = fmt.Sprintf("High error count: %d", c.stats.errorCount.Load())
	}

	lastEvent := c.lastEventTime.Load().(time.Time)
	if time.Since(lastEvent) > 5*time.Minute && c.started.Load() {
		status = collectors.HealthStatusDegraded
		message = "No events received in 5 minutes"
	}

	return &collectors.Health{
		Status:          status,
		Message:         message,
		LastEventTime:   lastEvent,
		EventsProcessed: c.stats.eventsCollected.Load(),
		EventsDropped:   c.stats.eventsDropped.Load(),
		ErrorCount:      c.stats.errorCount.Load(),
		Metrics: map[string]interface{}{
			"events_filtered":     c.stats.eventsFiltered.Load(),
			"oom_kills_detected":  c.stats.oomKillsDetected.Load(),
			"container_failures":  c.stats.containersFailures.Load(),
			"bytes_processed":     c.stats.bytesProcessed.Load(),
			"avg_processing_time": c.getAverageProcessingTime(),
			"filter_efficiency":   c.getFilterEfficiency(),
		},
	}
}

// GetStats returns collector statistics
func (c *Collector) GetStats() *collectors.Stats {
	eventsCollected := c.stats.eventsCollected.Load()
	eventsFiltered := c.stats.eventsFiltered.Load()
	totalProcessed := eventsCollected + eventsFiltered

	return &collectors.Stats{
		EventsCollected: eventsCollected,
		EventsDropped:   c.stats.eventsDropped.Load(),
		EventsFiltered:  eventsFiltered,
		ErrorCount:      c.stats.errorCount.Load(),
		StartTime:       c.reader.GetStartTime(),
		LastEventTime:   c.lastEventTime.Load().(time.Time),
		Custom: map[string]interface{}{
			"oom_kills_detected":    c.stats.oomKillsDetected.Load(),
			"container_failures":    c.stats.containersFailures.Load(),
			"bytes_processed_mb":    float64(c.stats.bytesProcessed.Load()) / 1024 / 1024,
			"filter_efficiency_pct": c.getFilterEfficiency(),
			"events_per_second":     c.getEventsPerSecond(),
			"critical_events_ratio": c.getCriticalEventsRatio(),
		},
	}
}

// Configure updates the collector configuration
func (c *Collector) Configure(config collectors.CollectorConfig) error {
	c.config = config
	c.enabled.Store(config.Enabled)

	// Update components with new config
	journaldConfig, err := extractJournaldConfig(config.Extra)
	if err != nil {
		return fmt.Errorf("invalid journald configuration: %w", err)
	}

	if err := c.filter.UpdateConfig(journaldConfig); err != nil {
		return fmt.Errorf("failed to update filter config: %w", err)
	}

	c.logger.Info("Updated journald collector configuration",
		"enabled", config.Enabled,
		"buffer_size", config.EventBufferSize,
	)

	return nil
}

// IsEnabled returns whether the collector is enabled
func (c *Collector) IsEnabled() bool {
	return c.enabled.Load()
}

// processEvents is the main event processing loop
func (c *Collector) processEvents() {
	defer c.wg.Done()

	for {
		select {
		case <-c.ctx.Done():
			return

		case entry, ok := <-c.reader.Entries():
			if !ok {
				c.logger.Debug("Journald entries channel closed")
				return
			}

			start := time.Now()
			
			// Update statistics
			c.stats.bytesProcessed.Add(uint64(len(entry.Message)))

			// Apply smart filtering first (95% noise reduction)
			if !c.filter.ShouldProcess(entry) {
				c.stats.eventsFiltered.Add(1)
				continue
			}

			// Parse the entry with OPINIONATED logic
			event := c.parseEntry(entry)
			if event == nil {
				continue
			}

			// Enrich with semantic context
			c.enricher.Enrich(event, entry)

			// Send event
			select {
			case c.eventChan <- event:
				c.stats.eventsCollected.Add(1)
				c.lastEventTime.Store(time.Now())
				
				// Update processing time
				processingNanos := time.Since(start).Nanoseconds()
				c.processingTime.Add(processingNanos)
				
			case <-c.ctx.Done():
				return
			default:
				c.stats.eventsDropped.Add(1)
			}
		}
	}
}

// parseEntry performs OPINIONATED parsing focused on critical events
func (c *Collector) parseEntry(entry *JournalEntry) *collectors.Event {
	// Check for OOM kill
	if oomEvent := c.oomDetector.Detect(entry); oomEvent != nil {
		c.stats.oomKillsDetected.Add(1)
		return oomEvent
	}

	// Check for container runtime failures
	if containerEvent := c.containerParser.Parse(entry); containerEvent != nil {
		c.stats.containersFailures.Add(1)
		return containerEvent
	}

	// Parse general critical events
	event := c.parser.ParseCritical(entry)
	if event == nil {
		return nil
	}

	// Set base event properties
	event.ID = fmt.Sprintf("journald_%s_%d", entry.SystemdUnit, entry.RealtimeTimestamp)
	event.Timestamp = time.Unix(0, entry.RealtimeTimestamp*1000)
	event.Source = collectors.EventSource{
		Collector: c.Name(),
		Component: "journald",
		Node:      entry.Hostname,
	}

	return event
}

// reportStatistics periodically logs collector statistics
func (c *Collector) reportStatistics() {
	defer c.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.logger.Info("Journald collector statistics",
				"events_collected", c.stats.eventsCollected.Load(),
				"events_filtered", c.stats.eventsFiltered.Load(),
				"filter_efficiency", fmt.Sprintf("%.2f%%", c.getFilterEfficiency()),
				"oom_kills", c.stats.oomKillsDetected.Load(),
				"container_failures", c.stats.containersFailures.Load(),
				"critical_events_ratio", fmt.Sprintf("%.2f%%", c.getCriticalEventsRatio()),
			)
		}
	}
}

// Helper methods

func (c *Collector) getFilterEfficiency() float64 {
	total := c.stats.eventsCollected.Load() + c.stats.eventsFiltered.Load()
	if total == 0 {
		return 0
	}
	return (float64(c.stats.eventsFiltered.Load()) / float64(total)) * 100
}

func (c *Collector) getCriticalEventsRatio() float64 {
	collected := c.stats.eventsCollected.Load()
	if collected == 0 {
		return 0
	}
	critical := c.stats.oomKillsDetected.Load() + c.stats.containersFailures.Load()
	return (float64(critical) / float64(collected)) * 100
}

func (c *Collector) getAverageProcessingTime() time.Duration {
	events := c.stats.eventsCollected.Load()
	if events == 0 {
		return 0
	}
	totalNanos := c.processingTime.Load()
	return time.Duration(totalNanos / int64(events))
}

func (c *Collector) getEventsPerSecond() float64 {
	runtime := time.Since(c.reader.GetStartTime()).Seconds()
	if runtime == 0 {
		return 0
	}
	return float64(c.stats.eventsCollected.Load()) / runtime
}

// extractJournaldConfig extracts journald-specific configuration
func extractJournaldConfig(extra map[string]interface{}) (*JournaldConfig, error) {
	config := &JournaldConfig{
		// Defaults for OPINIONATED collection
		FollowCursor:    true,
		MaxAge:          24 * time.Hour,
		Units:           []string{}, // Monitor all units by default
		Priorities:      []string{"0", "1", "2", "3", "4"}, // Emergency to Warning
		MatchPatterns:   getDefaultPatterns(),
		FilterNoisyUnits: true,
		StreamBatchSize: 1000,
	}

	// Override with provided config
	if extra != nil {
		// Implementation would parse extra config
	}

	return config, nil
}

// getDefaultPatterns returns OPINIONATED patterns for critical events
func getDefaultPatterns() []string {
	return []string{
		"OOM",
		"oom-kill",
		"Out of memory",
		"Memory cgroup out of memory",
		"invoked oom-killer",
		"score [0-9]+ or sacrifice",
		"Failed to start",
		"Failed to run",
		"Container .* failed",
		"Error response from daemon",
		"panic:",
		"fatal error:",
		"segmentation fault",
		"core dumped",
		"cannot allocate memory",
		"no space left on device",
		"connection refused",
		"i/o timeout",
		"context deadline exceeded",
	}
}