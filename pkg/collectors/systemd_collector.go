package collectors

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/systemd"
)

// systemdCollector implements the Collector interface for systemd service monitoring
type systemdCollector struct {
	config       CollectorConfig
	systemdClient *systemd.Collector
	
	// Event processing
	eventChan    chan *Event
	
	// State management
	started      atomic.Bool
	stopped      atomic.Bool
	enabled      atomic.Bool
	
	// Lifecycle
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	
	// Statistics
	eventsCollected uint64
	eventsDropped   uint64
	errorCount      uint64
	
	// Health tracking
	lastEventTime time.Time
	lastError     string
	lastErrorTime time.Time
	healthMu      sync.RWMutex
}

// SystemdCollectorFactory implements Factory for systemd collectors
type SystemdCollectorFactory struct{}

// NewSystemdCollectorFactory creates a new systemd collector factory
func NewSystemdCollectorFactory() Factory {
	return &SystemdCollectorFactory{}
}

// CreateCollector creates a new systemd collector instance
func (f *SystemdCollectorFactory) CreateCollector(config CollectorConfig) (Collector, error) {
	if config.Type != "systemd" {
		return nil, fmt.Errorf("invalid collector type: %s, expected: systemd", config.Type)
	}
	
	// Validate systemd-specific configuration
	if err := f.ValidateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid systemd configuration: %w", err)
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	collector := &systemdCollector{
		config:    config,
		eventChan: make(chan *Event, config.EventBufferSize),
		ctx:       ctx,
		cancel:    cancel,
	}
	
	collector.enabled.Store(config.Enabled)
	
	// Initialize systemd client
	systemdConfig := systemd.Config{
		MonitorServices:     getBoolFromConfig(config.CollectorSpecific, "monitor_services", true),
		MonitorSockets:      getBoolFromConfig(config.CollectorSpecific, "monitor_sockets", false),
		MonitorTimers:       getBoolFromConfig(config.CollectorSpecific, "monitor_timers", false),
		ServiceFilter:       getStringSliceFromConfig(config.CollectorSpecific, "service_filter", nil),
		ExcludeSystemServices: getBoolFromConfig(config.CollectorSpecific, "exclude_system", true),
		PollInterval:        getDurationFromConfig(config.CollectorSpecific, "poll_interval", 30*time.Second),
	}
	
	systemdClient := systemd.NewCollector(systemdConfig)
	collector.systemdClient = systemdClient
	
	return collector, nil
}

// SupportedTypes returns the collector types this factory can create
func (f *SystemdCollectorFactory) SupportedTypes() []string {
	return []string{"systemd"}
}

// ValidateConfig validates a configuration for systemd collector
func (f *SystemdCollectorFactory) ValidateConfig(config CollectorConfig) error {
	if config.Type != "systemd" {
		return fmt.Errorf("invalid collector type: %s", config.Type)
	}
	
	if config.EventBufferSize <= 0 {
		return fmt.Errorf("event_buffer_size must be positive")
	}
	
	// Check if systemd is available
	if !systemd.IsAvailable() {
		return fmt.Errorf("systemd is not available on this system")
	}
	
	return nil
}

// Collector interface implementation

// Name returns the unique name of this collector
func (sc *systemdCollector) Name() string {
	return sc.config.Name
}

// Type returns the collector type
func (sc *systemdCollector) Type() string {
	return "systemd"
}

// Start begins data collection
func (sc *systemdCollector) Start(ctx context.Context) error {
	if !sc.started.CompareAndSwap(false, true) {
		return fmt.Errorf("systemd collector already started")
	}
	
	if !sc.enabled.Load() {
		return fmt.Errorf("systemd collector is disabled")
	}
	
	// Start systemd client
	if err := sc.systemdClient.Start(ctx); err != nil {
		sc.recordError(fmt.Errorf("failed to start systemd client: %w", err))
		return err
	}
	
	// Start event processing
	sc.wg.Add(1)
	go sc.processSystemdEvents()
	
	return nil
}

// Stop gracefully stops the collector
func (sc *systemdCollector) Stop() error {
	if !sc.stopped.CompareAndSwap(false, true) {
		return nil // Already stopped
	}
	
	// Cancel context
	sc.cancel()
	
	// Stop systemd client
	if err := sc.systemdClient.Stop(); err != nil {
		sc.recordError(fmt.Errorf("failed to stop systemd client: %w", err))
	}
	
	// Wait for goroutines
	sc.wg.Wait()
	
	// Close event channel
	close(sc.eventChan)
	
	return nil
}

// Events returns a channel that emits events from this collector
func (sc *systemdCollector) Events() <-chan *Event {
	return sc.eventChan
}

// Health returns the current health status of the collector
func (sc *systemdCollector) Health() *Health {
	sc.healthMu.RLock()
	defer sc.healthMu.RUnlock()
	
	status := HealthStatusHealthy
	message := "Operating normally"
	
	if sc.stopped.Load() {
		status = HealthStatusStopped
		message = "Stopped"
	} else if !sc.enabled.Load() {
		status = HealthStatusStopped
		message = "Disabled"
	} else if atomic.LoadUint64(&sc.errorCount) > 0 {
		if time.Since(sc.lastErrorTime) < 5*time.Minute {
			status = HealthStatusDegraded
			message = fmt.Sprintf("Recent error: %s", sc.lastError)
		}
	}
	
	// Check if events are flowing
	if status == HealthStatusHealthy && time.Since(sc.lastEventTime) > 5*time.Minute {
		status = HealthStatusDegraded
		message = "No recent events"
	}
	
	return &Health{
		Status:          status,
		Message:         message,
		LastEventTime:   sc.lastEventTime,
		EventsProcessed: atomic.LoadUint64(&sc.eventsCollected),
		EventsDropped:   atomic.LoadUint64(&sc.eventsDropped),
		ErrorCount:      atomic.LoadUint64(&sc.errorCount),
		LastError:       sc.lastError,
		LastErrorTime:   sc.lastErrorTime,
		Metrics: map[string]interface{}{
			"services_monitored": sc.systemdClient.GetStats().ServicesMonitored,
			"services_failed":    sc.systemdClient.GetStats().ServicesFailed,
		},
	}
}

// GetStats returns collector-specific statistics
func (sc *systemdCollector) GetStats() *Stats {
	systemdStats := sc.systemdClient.GetStats()
	
	return &Stats{
		EventsCollected:   atomic.LoadUint64(&sc.eventsCollected),
		EventsDropped:     atomic.LoadUint64(&sc.eventsDropped),
		EventsFiltered:    systemdStats.EventsFiltered,
		BytesProcessed:    systemdStats.BytesProcessed,
		ErrorCount:        atomic.LoadUint64(&sc.errorCount),
		EventsPerSecond:   systemdStats.EventsPerSecond,
		AvgProcessingTime: systemdStats.AvgProcessingTime,
		MaxProcessingTime: systemdStats.MaxProcessingTime,
		MemoryUsageMB:     systemdStats.MemoryUsageMB,
		CPUUsagePercent:   systemdStats.CPUUsagePercent,
		StartTime:         systemdStats.StartTime,
		LastEventTime:     sc.lastEventTime,
		Uptime:            time.Since(systemdStats.StartTime),
		CollectorMetrics: map[string]interface{}{
			"services_monitored": systemdStats.ServicesMonitored,
			"services_active":    systemdStats.ServicesActive,
			"services_failed":    systemdStats.ServicesFailed,
			"services_inactive":  systemdStats.ServicesInactive,
			"sockets_monitored":  systemdStats.SocketsMonitored,
			"timers_monitored":   systemdStats.TimersMonitored,
		},
	}
}

// Configure updates the collector configuration
func (sc *systemdCollector) Configure(config CollectorConfig) error {
	if config.Type != "systemd" {
		return fmt.Errorf("invalid collector type: %s", config.Type)
	}
	
	// Update configuration
	sc.config = config
	sc.enabled.Store(config.Enabled)
	
	return nil
}

// IsEnabled returns whether the collector is currently enabled
func (sc *systemdCollector) IsEnabled() bool {
	return sc.enabled.Load()
}

// processSystemdEvents processes events from the systemd client
func (sc *systemdCollector) processSystemdEvents() {
	defer sc.wg.Done()
	
	for {
		select {
		case <-sc.ctx.Done():
			return
			
		case systemdEvent, ok := <-sc.systemdClient.Events():
			if !ok {
				return // Channel closed
			}
			
			// Convert systemd event to collector event
			collectorEvent := sc.convertSystemdEvent(systemdEvent)
			if collectorEvent == nil {
				continue
			}
			
			// Apply filtering if needed
			if !sc.shouldProcessEvent(collectorEvent) {
				atomic.AddUint64(&sc.eventsDropped, 1)
				continue
			}
			
			// Update last event time
			sc.healthMu.Lock()
			sc.lastEventTime = time.Now()
			sc.healthMu.Unlock()
			
			// Try to send event
			select {
			case sc.eventChan <- collectorEvent:
				atomic.AddUint64(&sc.eventsCollected, 1)
			default:
				atomic.AddUint64(&sc.eventsDropped, 1)
			}
		}
	}
}

// convertSystemdEvent converts a systemd event to a collector event
func (sc *systemdCollector) convertSystemdEvent(systemdEvent *systemd.Event) *Event {
	if systemdEvent == nil {
		return nil
	}
	
	// Determine category and type based on systemd event
	category, eventType := sc.categorizeSystemdEvent(systemdEvent)
	
	// Create collector event
	event := &Event{
		ID:          fmt.Sprintf("systemd_%s_%d", systemdEvent.Unit, time.Now().UnixNano()),
		Timestamp:   systemdEvent.Timestamp,
		Source:      sc.config.Name,
		SourceType:  "systemd",
		CollectorID: sc.config.Name,
		Type:        eventType,
		Category:    category,
		Severity:    sc.determineSeverity(systemdEvent),
		Data:        sc.extractEventData(systemdEvent),
		Attributes:  sc.extractAttributes(systemdEvent),
		Labels:      sc.config.Labels,
		Context:     sc.extractContext(systemdEvent),
	}
	
	// Add actionable recommendations if applicable
	if actionable := sc.generateActionable(systemdEvent); actionable != nil {
		event.Actionable = actionable
	}
	
	return event
}

// categorizeSystemdEvent determines the category and type of a systemd event
func (sc *systemdCollector) categorizeSystemdEvent(event *systemd.Event) (Category, string) {
	switch event.EventType {
	case systemd.EventTypeServiceStarted:
		return CategorySystem, "service_started"
	case systemd.EventTypeServiceStopped:
		return CategorySystem, "service_stopped"
	case systemd.EventTypeServiceFailed:
		return CategorySystem, "service_failed"
	case systemd.EventTypeServiceRestarted:
		return CategorySystem, "service_restarted"
	case systemd.EventTypeSocketActivated:
		return CategorySystem, "socket_activated"
	case systemd.EventTypeTimerTriggered:
		return CategorySystem, "timer_triggered"
	default:
		return CategorySystem, "generic"
	}
}

func (sc *systemdCollector) determineSeverity(event *systemd.Event) Severity {
	switch event.EventType {
	case systemd.EventTypeServiceFailed:
		if event.IsCriticalService {
			return SeverityCritical
		}
		return SeverityHigh
	case systemd.EventTypeServiceRestarted:
		return SeverityMedium
	case systemd.EventTypeServiceStarted, systemd.EventTypeServiceStopped:
		return SeverityLow
	default:
		return SeverityLow
	}
}

func (sc *systemdCollector) extractEventData(event *systemd.Event) map[string]interface{} {
	data := make(map[string]interface{})
	
	data["unit"] = event.Unit
	data["unit_type"] = event.UnitType
	data["state"] = event.State
	data["sub_state"] = event.SubState
	data["active_state"] = event.ActiveState
	data["load_state"] = event.LoadState
	data["description"] = event.Description
	data["exit_code"] = event.ExitCode
	data["exit_status"] = event.ExitStatus
	data["restart_count"] = event.RestartCount
	data["memory_usage"] = event.MemoryUsage
	data["cpu_usage"] = event.CPUUsage
	
	if event.FailureReason != "" {
		data["failure_reason"] = event.FailureReason
	}
	
	return data
}

func (sc *systemdCollector) extractAttributes(event *systemd.Event) map[string]interface{} {
	attributes := make(map[string]interface{})
	
	attributes["systemd_version"] = event.SystemdVersion
	attributes["is_critical_service"] = event.IsCriticalService
	attributes["is_user_service"] = event.IsUserService
	
	return attributes
}

func (sc *systemdCollector) extractContext(event *systemd.Event) *EventContext {
	context := &EventContext{}
	
	// Add custom context for systemd events
	if context.Custom == nil {
		context.Custom = make(map[string]string)
	}
	
	context.Custom["systemd_unit"] = event.Unit
	context.Custom["systemd_unit_type"] = event.UnitType
	context.Custom["systemd_state"] = event.State
	
	// Try to correlate with process information if available
	if event.MainPID > 0 {
		context.PID = event.MainPID
		context.ProcessName = event.Unit
	}
	
	return context
}

func (sc *systemdCollector) generateActionable(event *systemd.Event) *ActionableItem {
	switch event.EventType {
	case systemd.EventTypeServiceFailed:
		return &ActionableItem{
			Title:           "Restart failed service",
			Description:     fmt.Sprintf("Service %s has failed and may need to be restarted", event.Unit),
			Commands:        []string{fmt.Sprintf("systemctl restart %s", event.Unit), fmt.Sprintf("systemctl status %s", event.Unit)},
			Risk:            "low",
			EstimatedImpact: "Restores service functionality",
			AutoApplicable:  false,
			Category:        "service",
		}
	case systemd.EventTypeServiceRestarted:
		if event.RestartCount > 3 {
			return &ActionableItem{
				Title:           "Investigate frequent restarts",
				Description:     fmt.Sprintf("Service %s has restarted %d times, investigate logs", event.Unit, event.RestartCount),
				Commands:        []string{fmt.Sprintf("journalctl -u %s --since='1 hour ago'", event.Unit)},
				Risk:            "low",
				EstimatedImpact: "Helps identify the cause of instability",
				AutoApplicable:  false,
				Category:        "troubleshooting",
			}
		}
	}
	
	return nil
}

func (sc *systemdCollector) shouldProcessEvent(event *Event) bool {
	// Apply severity filtering
	if event.Severity < sc.config.MinSeverity {
		return false
	}
	
	// Apply category filtering
	if len(sc.config.ExcludeCategories) > 0 {
		for _, excludeCategory := range sc.config.ExcludeCategories {
			if event.Category == excludeCategory {
				return false
			}
		}
	}
	
	if len(sc.config.IncludeCategories) > 0 {
		included := false
		for _, includeCategory := range sc.config.IncludeCategories {
			if event.Category == includeCategory {
				included = true
				break
			}
		}
		if !included {
			return false
		}
	}
	
	return true
}

func (sc *systemdCollector) recordError(err error) {
	atomic.AddUint64(&sc.errorCount, 1)
	
	sc.healthMu.Lock()
	sc.lastError = err.Error()
	sc.lastErrorTime = time.Now()
	sc.healthMu.Unlock()
}

// Helper function to get string slice from config
func getStringSliceFromConfig(config map[string]interface{}, key string, defaultValue []string) []string {
	if value, ok := config[key].([]string); ok {
		return value
	}
	if value, ok := config[key].([]interface{}); ok {
		stringSlice := make([]string, len(value))
		for i, v := range value {
			if str, ok := v.(string); ok {
				stringSlice[i] = str
			}
		}
		return stringSlice
	}
	return defaultValue
}