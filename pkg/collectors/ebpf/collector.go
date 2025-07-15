package ebpf

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/unified"
	"github.com/yairfalse/tapio/pkg/ebpf"
	"github.com/yairfalse/tapio/pkg/logging"
)

// Collector implements unified.Collector for eBPF data collection
type Collector struct {
	// Configuration
	config unified.CollectorConfig
	logger *logging.Logger

	// eBPF client
	ebpfClient *ebpf.EnhancedCollector

	// Event processing
	eventChan chan *unified.Event

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
		eventsCollected atomic.Uint64
		eventsDropped   atomic.Uint64
		errorCount      atomic.Uint64
	}

	// Health tracking
	lastEventTime atomic.Value // time.Time
	lastError     atomic.Value // string
	lastErrorTime atomic.Value // time.Time
	startTime     time.Time
}

// NewCollector creates a new eBPF collector
func NewCollector(config unified.CollectorConfig) (*Collector, error) {
	logger := logging.Development.WithComponent("ebpf-collector")

	// Note: eBPF configuration is simplified for now since the actual
	// eBPF collector doesn't take configuration parameters

	c := &Collector{
		config:    config,
		logger:    logger,
		eventChan: make(chan *unified.Event, config.EventBufferSize),
		enabled:   atomic.Bool{},
		startTime: time.Now(),
	}

	// Initialize as enabled based on config
	c.enabled.Store(config.Enabled)
	c.lastEventTime.Store(time.Now())
	c.lastError.Store("")
	c.lastErrorTime.Store(time.Time{})

	// Initialize eBPF client
	ebpfClient, err := ebpf.NewEnhancedCollector()
	if err != nil {
		return nil, fmt.Errorf("failed to create eBPF client: %w", err)
	}

	c.ebpfClient = ebpfClient

	return c, nil
}

// Name returns the collector name
func (c *Collector) Name() string {
	return c.config.Name
}

// Type returns the collector type
func (c *Collector) Type() string {
	return "ebpf"
}

// Start begins data collection
func (c *Collector) Start(ctx context.Context) error {
	if !c.enabled.Load() {
		return fmt.Errorf("eBPF collector is disabled")
	}

	if c.started.Load() {
		return fmt.Errorf("eBPF collector already started")
	}

	c.logger.Info("Starting eBPF collector")

	// Create cancellable context
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Start eBPF client
	if err := c.ebpfClient.Start(); err != nil {
		c.recordError(fmt.Errorf("failed to start eBPF client: %w", err))
		return err
	}

	// Mark as started
	c.started.Store(true)

	// Start event processing
	c.wg.Add(1)
	go c.processEBPFEvents()

	c.logger.Info("eBPF collector started successfully")
	return nil
}

// Stop gracefully stops the collector
func (c *Collector) Stop() error {
	if !c.started.Load() {
		return fmt.Errorf("eBPF collector not started")
	}

	if c.stopped.Load() {
		return fmt.Errorf("eBPF collector already stopped")
	}

	c.logger.Info("Stopping eBPF collector")

	// Mark as stopping
	c.stopped.Store(true)

	// Cancel context
	if c.cancel != nil {
		c.cancel()
	}

	// Stop eBPF client
	if err := c.ebpfClient.Stop(); err != nil {
		c.recordError(fmt.Errorf("failed to stop eBPF client: %w", err))
	}

	// Wait for goroutines
	c.wg.Wait()

	// Close event channel
	close(c.eventChan)

	c.logger.Info("eBPF collector stopped",
		"events_collected", c.stats.eventsCollected.Load(),
		"events_dropped", c.stats.eventsDropped.Load(),
	)

	return nil
}

// IsEnabled returns whether the collector is enabled
func (c *Collector) IsEnabled() bool {
	return c.enabled.Load()
}

// Events returns the event channel
func (c *Collector) Events() <-chan *unified.Event {
	return c.eventChan
}

// Health returns the collector health status
func (c *Collector) Health() *unified.Health {
	status := unified.HealthStatusHealthy
	message := "eBPF collector is healthy"

	if !c.started.Load() {
		status = unified.HealthStatusUnknown
		message = "Collector not started"
	} else if c.stopped.Load() {
		status = unified.HealthStatusUnhealthy
		message = "Collector stopped"
	} else if c.stats.errorCount.Load() > 100 {
		status = unified.HealthStatusDegraded
		message = fmt.Sprintf("High error count: %d", c.stats.errorCount.Load())
	}

	lastEvent := c.lastEventTime.Load().(time.Time)
	if time.Since(lastEvent) > 5*time.Minute && c.started.Load() {
		status = unified.HealthStatusDegraded
		message = "No events received in 5 minutes"
	}

	return &unified.Health{
		Status:          status,
		Message:         message,
		LastEventTime:   lastEvent,
		EventsProcessed: c.stats.eventsCollected.Load(),
		EventsDropped:   c.stats.eventsDropped.Load(),
		ErrorCount:      c.stats.errorCount.Load(),
		Metrics: map[string]interface{}{
			"ebpf_programs_loaded": c.getEBPFProgramCount(),
			"buffer_utilization":   c.getBufferUtilization(),
		},
	}
}

// GetStats returns collector statistics
func (c *Collector) GetStats() *unified.Stats {
	uptime := time.Since(c.startTime)
	eventsCollected := c.stats.eventsCollected.Load()

	return &unified.Stats{
		EventsCollected: eventsCollected,
		EventsDropped:   c.stats.eventsDropped.Load(),
		ErrorCount:      c.stats.errorCount.Load(),
		StartTime:       c.startTime,
		LastEventTime:   c.lastEventTime.Load().(time.Time),
		Custom: map[string]interface{}{
			"events_per_second":    float64(eventsCollected) / uptime.Seconds(),
			"uptime_seconds":       uptime.Seconds(),
			"ebpf_programs_loaded": c.getEBPFProgramCount(),
			"buffer_utilization":   c.getBufferUtilization(),
		},
	}
}

// Configure updates the collector configuration
func (c *Collector) Configure(config unified.CollectorConfig) error {
	c.config = config
	c.enabled.Store(config.Enabled)

	c.logger.Info("Updated eBPF collector configuration",
		"enabled", config.Enabled,
		"buffer_size", config.EventBufferSize,
	)

	return nil
}

// processEBPFEvents processes events from the eBPF client
func (c *Collector) processEBPFEvents() {
	defer c.wg.Done()

	for {
		select {
		case <-c.ctx.Done():
			return

		case ebpfEvent, ok := <-c.ebpfClient.GetEventChannel():
			if !ok {
				return // Channel closed
			}

			// Convert eBPF event to unified event
			unifiedEvent := c.convertEBPFEvent(ebpfEvent)
			if unifiedEvent == nil {
				continue
			}

			// Update last event time
			c.lastEventTime.Store(time.Now())

			// Try to send event
			select {
			case c.eventChan <- unifiedEvent:
				c.stats.eventsCollected.Add(1)
			default:
				c.stats.eventsDropped.Add(1)
			}
		}
	}
}

// convertEBPFEvent converts an eBPF event to a unified event
func (c *Collector) convertEBPFEvent(ebpfEvent ebpf.SystemEvent) *unified.Event {

	// Determine category and severity based on eBPF event
	category, severity := c.categorizeEBPFEvent(ebpfEvent)

	// Create unified event
	event := &unified.Event{
		ID:        fmt.Sprintf("ebpf_%d_%d", time.Now().UnixNano(), ebpfEvent.PID),
		Timestamp: ebpfEvent.Timestamp,
		Type:      c.determineEventType(ebpfEvent),
		Category:  category,
		Severity:  severity,
		Source: unified.EventSource{
			Collector: c.config.Name,
			Component: "ebpf",
			Node:      "localhost", // TODO: Get actual node name
			Version:   "1.0.0",
		},
		Message:    c.generateMessage(ebpfEvent),
		Data:       c.extractEventData(ebpfEvent),
		Attributes: c.extractAttributes(ebpfEvent),
		Labels:     c.config.Labels,
		Context:    c.extractContext(ebpfEvent),
		Metadata: unified.EventMetadata{
			CollectedAt:  ebpfEvent.Timestamp,
			ProcessedAt:  time.Now(),
			ProcessingMS: time.Since(ebpfEvent.Timestamp).Milliseconds(),
			Tags:         c.config.Tags,
		},
	}

	// Add actionable recommendations if applicable
	if actionable := c.generateActionable(ebpfEvent); actionable != nil {
		event.Actionable = actionable
	}

	return event
}

// Helper methods

func (c *Collector) categorizeEBPFEvent(event ebpf.SystemEvent) (unified.Category, unified.Severity) {
	switch event.Type {
	case "memory_oom":
		return unified.CategoryMemory, unified.SeverityCritical
	case "network_connection":
		return unified.CategoryNetwork, unified.SeverityInfo
	case "process_start", "process_exit":
		return unified.CategoryProcess, unified.SeverityInfo
	default:
		return unified.CategorySystem, unified.SeverityInfo
	}
}

func (c *Collector) determineEventType(event ebpf.SystemEvent) string {
	return event.Type
}

func (c *Collector) generateMessage(event ebpf.SystemEvent) string {
	switch event.Type {
	case "memory_oom":
		return "Process killed due to out-of-memory condition"
	case "network_connection":
		return "New network connection established"
	case "process_start":
		return "Process started"
	case "process_exit":
		return "Process exited"
	default:
		return fmt.Sprintf("eBPF event: %s", event.Type)
	}
}

func (c *Collector) extractEventData(event ebpf.SystemEvent) map[string]interface{} {
	data := make(map[string]interface{})

	data["type"] = event.Type
	data["pid"] = event.PID
	data["timestamp"] = event.Timestamp

	// Add event data if available
	if event.Data != nil {
		data["data"] = event.Data
	}

	return data
}

func (c *Collector) extractAttributes(event ebpf.SystemEvent) map[string]interface{} {
	attributes := make(map[string]interface{})

	attributes["collector_type"] = "ebpf"
	attributes["event_type"] = event.Type

	return attributes
}

func (c *Collector) extractContext(event ebpf.SystemEvent) *unified.EventContext {
	context := &unified.EventContext{}

	context.PID = event.PID

	return context
}

func (c *Collector) generateActionable(event ebpf.SystemEvent) *unified.ActionableItem {
	switch event.Type {
	case "memory_oom":
		return &unified.ActionableItem{
			Title:           "Increase memory limit",
			Description:     "Process was killed due to out-of-memory condition",
			Commands:        []string{"kubectl patch deployment <name> -p '{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"<container>\",\"resources\":{\"limits\":{\"memory\":\"512Mi\"}}}]}}}}'"},
			Risk:            unified.RiskLow,
			EstimatedImpact: "Prevents OOM kills, may increase memory usage",
			AutoFixable:     false,
			Documentation:   "https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/",
		}
	}

	return nil
}

func (c *Collector) getEBPFProgramCount() int {
	if c.ebpfClient != nil {
		stats := c.ebpfClient.GetStatistics()
		if programsLoaded, ok := stats["programs_loaded"].(int); ok {
			return programsLoaded
		}
	}
	return 0
}

func (c *Collector) getBufferUtilization() float64 {
	if c.ebpfClient != nil {
		stats := c.ebpfClient.GetStatistics()
		if utilization, ok := stats["buffer_utilization"].(float64); ok {
			return utilization
		}
	}
	return 0.0
}

func (c *Collector) recordError(err error) {
	c.stats.errorCount.Add(1)
	c.lastError.Store(err.Error())
	c.lastErrorTime.Store(time.Now())
}

// EBPFConfig contains eBPF-specific configuration
type EBPFConfig struct {
	EnableNetwork bool `json:"enable_network"`
	EnableMemory  bool `json:"enable_memory"`
	EnableProcess bool `json:"enable_process"`
	BufferSize    int  `json:"buffer_size"`
}

func extractEBPFConfig(extra map[string]interface{}) (*EBPFConfig, error) {
	config := &EBPFConfig{
		// Defaults
		EnableNetwork: true,
		EnableMemory:  true,
		EnableProcess: true,
		BufferSize:    8192,
	}

	if extra != nil {
		if enable, ok := extra["enable_network"].(bool); ok {
			config.EnableNetwork = enable
		}
		if enable, ok := extra["enable_memory"].(bool); ok {
			config.EnableMemory = enable
		}
		if enable, ok := extra["enable_process"].(bool); ok {
			config.EnableProcess = enable
		}
		if size, ok := extra["buffer_size"].(float64); ok {
			config.BufferSize = int(size)
		}
	}

	return config, nil
}

func getBoolFromConfig(config *EBPFConfig, key string, defaultValue bool) bool {
	switch key {
	case "enable_network":
		return config.EnableNetwork
	case "enable_memory":
		return config.EnableMemory
	case "enable_process":
		return config.EnableProcess
	default:
		return defaultValue
	}
}
