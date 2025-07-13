package systemd

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
)

// Collector implements the collectors.Collector interface for systemd monitoring
type Collector struct {
	// Configuration
	config collectors.CollectorConfig
	
	// Core components
	dbus           *DBusConnection
	serviceMonitor *ServiceMonitor
	
	// Event channel
	eventChan      chan *collectors.Event
	
	// State management
	started        atomic.Bool
	stopped        atomic.Bool
	enabled        atomic.Bool
	
	// Lifecycle
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	
	// Statistics
	eventsCollected uint64
	eventsDropped   uint64
	errorCount      uint64
	lastEventTime   time.Time
	
	// Health tracking
	lastError      error
	lastErrorTime  time.Time
	healthMu       sync.RWMutex
}

// NewCollector creates a new systemd collector
func NewCollector(config collectors.CollectorConfig) (*Collector, error) {
	ctx, cancel := context.WithCancel(context.Background())
	
	collector := &Collector{
		config:    config,
		eventChan: make(chan *collectors.Event, config.EventBufferSize),
		ctx:       ctx,
		cancel:    cancel,
	}
	
	collector.enabled.Store(config.Enabled)
	
	// Initialize D-Bus connection
	dbusConfig := DefaultDBusConfig()
	if bufferSize, ok := config.Extra["signal_buffer_size"].(int); ok {
		dbusConfig.SignalBufferSize = bufferSize
	}
	
	dbus, err := NewDBusConnection(dbusConfig)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create D-Bus connection: %w", err)
	}
	collector.dbus = dbus
	
	// Initialize service monitor
	monitorConfig := DefaultServiceMonitorConfig()
	
	// Apply custom configuration
	if whitelist, ok := config.Extra["service_whitelist"].([]string); ok {
		monitorConfig.ServiceWhitelist = whitelist
	}
	if blacklist, ok := config.Extra["service_blacklist"].([]string); ok {
		monitorConfig.ServiceBlacklist = blacklist
	}
	if monitorAll, ok := config.Extra["monitor_all_services"].(bool); ok {
		monitorConfig.MonitorAllServices = monitorAll
	}
	if trackDeps, ok := config.Extra["track_dependencies"].(bool); ok {
		monitorConfig.TrackDependencies = trackDeps
	}
	
	// Apply resource limits
	monitorConfig.EventBufferSize = config.EventBufferSize
	monitorConfig.MaxEventsPerSecond = config.MaxEventsPerSec
	
	serviceMonitor, err := NewServiceMonitor(dbus, monitorConfig)
	if err != nil {
		dbus.Close()
		cancel()
		return nil, fmt.Errorf("failed to create service monitor: %w", err)
	}
	collector.serviceMonitor = serviceMonitor
	
	return collector, nil
}

// Name returns the unique name of this collector
func (c *Collector) Name() string {
	return c.config.Name
}

// Type returns the collector type
func (c *Collector) Type() string {
	return "systemd"
}

// Start begins data collection
func (c *Collector) Start(ctx context.Context) error {
	if !c.started.CompareAndSwap(false, true) {
		return fmt.Errorf("systemd collector already started")
	}
	
	if !c.enabled.Load() {
		return fmt.Errorf("systemd collector is disabled")
	}
	
	// Start service monitor
	if err := c.serviceMonitor.Start(); err != nil {
		c.recordError(fmt.Errorf("failed to start service monitor: %w", err))
		return err
	}
	
	// Start event processing
	c.wg.Add(1)
	go c.processEvents()
	
	return nil
}

// Stop gracefully stops the collector
func (c *Collector) Stop() error {
	if !c.stopped.CompareAndSwap(false, true) {
		return nil // Already stopped
	}
	
	// Cancel context
	c.cancel()
	
	// Stop components
	if err := c.serviceMonitor.Stop(); err != nil {
		c.recordError(fmt.Errorf("failed to stop service monitor: %w", err))
	}
	
	if err := c.dbus.Close(); err != nil {
		c.recordError(fmt.Errorf("failed to close D-Bus connection: %w", err))
	}
	
	// Wait for goroutines
	c.wg.Wait()
	
	// Close event channel
	close(c.eventChan)
	
	return nil
}

// Events returns a channel that emits events from this collector
func (c *Collector) Events() <-chan *collectors.Event {
	return c.eventChan
}

// Health returns the current health status of the collector
func (c *Collector) Health() *collectors.Health {
	c.healthMu.RLock()
	defer c.healthMu.RUnlock()
	
	status := collectors.HealthStatusHealthy
	message := "Operating normally"
	
	if c.stopped.Load() {
		status = collectors.HealthStatusStopped
		message = "Stopped"
	} else if !c.enabled.Load() {
		status = collectors.HealthStatusStopped
		message = "Disabled"
	} else if !c.dbus.GetStats().IsConnected {
		status = collectors.HealthStatusUnhealthy
		message = "D-Bus connection lost"
	} else if atomic.LoadUint64(&c.errorCount) > 0 {
		if time.Since(c.lastErrorTime) < 5*time.Minute {
			status = collectors.HealthStatusDegraded
			message = fmt.Sprintf("Recent error: %v", c.lastError)
		}
	}
	
	// Check if events are flowing
	if status == collectors.HealthStatusHealthy && time.Since(c.lastEventTime) > 5*time.Minute {
		status = collectors.HealthStatusDegraded
		message = "No recent events"
	}
	
	monitorStats := c.serviceMonitor.GetStats()
	
	return &collectors.Health{
		Status:          status,
		Message:         message,
		LastEventTime:   c.lastEventTime,
		EventsProcessed: atomic.LoadUint64(&c.eventsCollected),
		EventsDropped:   atomic.LoadUint64(&c.eventsDropped),
		ErrorCount:      atomic.LoadUint64(&c.errorCount),
		LastError:       c.lastError,
		LastErrorTime:   c.lastErrorTime,
		Metrics: map[string]interface{}{
			"services_monitored":   monitorStats.ServicesMonitored,
			"container_services":   monitorStats.ContainerServices,
			"events_generated":     monitorStats.EventsGenerated,
			"dbus_signals_received": c.dbus.GetStats().SignalsReceived,
			"dbus_connected":       c.dbus.GetStats().IsConnected,
		},
	}
}

// GetStats returns collector-specific statistics
func (c *Collector) GetStats() *collectors.Stats {
	dbusStats := c.dbus.GetStats()
	monitorStats := c.serviceMonitor.GetStats()
	patterns := c.serviceMonitor.restartPatterns.GetAllPatterns()
	
	// Count pattern types
	patternCounts := make(map[string]int)
	anomalyCount := 0
	for _, pattern := range patterns {
		patternCounts[pattern.Type]++
		if pattern.IsAnomaly {
			anomalyCount++
		}
	}
	
	return &collectors.Stats{
		EventsTotal:       atomic.LoadUint64(&c.eventsCollected),
		EventsDropped:     atomic.LoadUint64(&c.eventsDropped),
		EventsPerSecond:   c.calculateEventsPerSecond(),
		ErrorCount:        atomic.LoadUint64(&c.errorCount),
		LastEventTime:     c.lastEventTime,
		CollectorSpecific: map[string]interface{}{
			"services_monitored":      monitorStats.ServicesMonitored,
			"container_services":      monitorStats.ContainerServices,
			"dbus_signals_received":   dbusStats.SignalsReceived,
			"dbus_signals_processed":  dbusStats.SignalsProcessed,
			"dbus_reconnect_count":    dbusStats.ReconnectCount,
			"patterns_detected":       patternCounts,
			"anomalies_detected":      anomalyCount,
			"memory_usage_mb":         c.estimateMemoryUsage() / 1024 / 1024,
		},
	}
}

// Configure updates the collector configuration
func (c *Collector) Configure(config collectors.CollectorConfig) error {
	if config.Type != "systemd" {
		return fmt.Errorf("invalid collector type: %s", config.Type)
	}
	
	// Update configuration
	c.config = config
	c.enabled.Store(config.Enabled)
	
	// Update service monitor configuration if needed
	// This would require implementing configuration updates in ServiceMonitor
	
	return nil
}

// IsEnabled returns whether the collector is currently enabled
func (c *Collector) IsEnabled() bool {
	return c.enabled.Load()
}

// processEvents processes events from the service monitor
func (c *Collector) processEvents() {
	defer c.wg.Done()
	
	serviceEvents := c.serviceMonitor.GetEvents()
	
	for {
		select {
		case <-c.ctx.Done():
			return
			
		case serviceEvent, ok := <-serviceEvents:
			if !ok {
				return // Channel closed
			}
			
			// Convert to collector event
			collectorEvent := c.convertServiceEvent(serviceEvent)
			if collectorEvent == nil {
				continue
			}
			
			// Apply filtering
			if !c.shouldProcessEvent(collectorEvent) {
				atomic.AddUint64(&c.eventsDropped, 1)
				continue
			}
			
			// Update last event time
			c.healthMu.Lock()
			c.lastEventTime = time.Now()
			c.healthMu.Unlock()
			
			// Try to send event
			select {
			case c.eventChan <- collectorEvent:
				atomic.AddUint64(&c.eventsCollected, 1)
			default:
				atomic.AddUint64(&c.eventsDropped, 1)
			}
		}
	}
}

// convertServiceEvent converts a service event to a collector event
func (c *Collector) convertServiceEvent(se *ServiceEvent) *collectors.Event {
	if se == nil {
		return nil
	}
	
	// Determine category
	category := collectors.CategorySystem
	if se.IsAnomaly {
		category = collectors.CategoryPerformance
	}
	
	// Create event context
	context := &collectors.EventContext{
		Node: c.config.Labels["node"],
	}
	
	// Add service-specific context
	if context.Custom == nil {
		context.Custom = make(map[string]string)
	}
	context.Custom["service"] = se.Service
	context.Custom["state"] = se.NewState
	if se.Pattern != "" {
		context.Custom["pattern"] = se.Pattern
	}
	
	// Build event data
	data := map[string]interface{}{
		"service":       se.Service,
		"event_type":    string(se.EventType),
		"old_state":     se.OldState,
		"new_state":     se.NewState,
		"sub_state":     se.SubState,
		"restart_count": se.RestartCount,
	}
	
	if se.PID > 0 {
		data["pid"] = se.PID
		context.PID = se.PID
	}
	if se.ExitCode != 0 {
		data["exit_code"] = se.ExitCode
	}
	if se.ExitStatus != "" {
		data["exit_status"] = se.ExitStatus
	}
	if se.MemoryUsage > 0 {
		data["memory_usage"] = se.MemoryUsage
	}
	if se.CPUUsage > 0 {
		data["cpu_usage"] = se.CPUUsage
	}
	
	// Build attributes
	attributes := map[string]interface{}{
		"collector":  c.Name(),
		"is_anomaly": se.IsAnomaly,
	}
	
	if len(se.AffectedServices) > 0 {
		attributes["affected_services"] = se.AffectedServices
	}
	
	// Create event
	event := &collectors.Event{
		ID:        fmt.Sprintf("systemd_%s_%d", se.Service, se.Timestamp.UnixNano()),
		Timestamp: se.Timestamp,
		Source: collectors.EventSource{
			Collector: c.Name(),
			Component: "service_monitor",
			Node:      c.config.Labels["node"],
		},
		Type:     collectors.EventTypeAlert,
		Category: category,
		Severity: se.Severity,
		Data:     data,
		Attributes: attributes,
		Labels:   c.config.Labels,
		Context:  context,
		Metadata: collectors.EventMetadata{
			Importance:  c.calculateImportance(se),
			Reliability: 0.95, // systemd events are highly reliable
		},
	}
	
	// Add actionable item if applicable
	if actionable := c.generateActionable(se); actionable != nil {
		event.Actionable = actionable
	}
	
	return event
}

// shouldProcessEvent determines if an event should be processed
func (c *Collector) shouldProcessEvent(event *collectors.Event) bool {
	// Apply severity filtering
	if event.Severity < c.config.MinSeverity {
		return false
	}
	
	// Apply category filtering
	if len(c.config.ExcludeCategories) > 0 {
		for _, excludeCategory := range c.config.ExcludeCategories {
			if event.Category == excludeCategory {
				return false
			}
		}
	}
	
	if len(c.config.IncludeCategories) > 0 {
		included := false
		for _, includeCategory := range c.config.IncludeCategories {
			if event.Category == includeCategory {
				included = true
				break
			}
		}
		if !included {
			return false
		}
	}
	
	// Apply sampling rate
	if c.config.SamplingRate < 1.0 {
		// Simple sampling implementation
		if float64(atomic.LoadUint64(&c.eventsCollected)%100)/100.0 > c.config.SamplingRate {
			return false
		}
	}
	
	return true
}

// calculateImportance calculates event importance score
func (c *Collector) calculateImportance(se *ServiceEvent) float32 {
	importance := float32(0.5) // Base importance
	
	// Increase for failures
	if se.EventType == ServiceFailed {
		importance += 0.3
	}
	
	// Increase for anomalies
	if se.IsAnomaly {
		importance += 0.2
	}
	
	// Increase for critical services
	serviceInfo := c.serviceMonitor.GetServiceInfo(se.Service)
	if serviceInfo != nil && (serviceInfo.IsCritical || serviceInfo.IsContainerRuntime) {
		importance += 0.3
	}
	
	// Increase for services with many dependents
	if len(se.AffectedServices) > 5 {
		importance += 0.2
	}
	
	// Cap at 1.0
	if importance > 1.0 {
		importance = 1.0
	}
	
	return importance
}

// generateActionable generates actionable recommendations
func (c *Collector) generateActionable(se *ServiceEvent) *collectors.ActionableItem {
	switch se.EventType {
	case ServiceFailed:
		commands := []string{
			fmt.Sprintf("systemctl status %s", se.Service),
			fmt.Sprintf("journalctl -u %s -n 50", se.Service),
		}
		
		if se.RestartCount > 3 {
			commands = append(commands, fmt.Sprintf("systemctl reset-failed %s", se.Service))
		}
		
		return &collectors.ActionableItem{
			Title:           fmt.Sprintf("Investigate failed service %s", se.Service),
			Description:     fmt.Sprintf("Service %s has failed. Check logs for root cause.", se.Service),
			Commands:        commands,
			Risk:            "low",
			EstimatedImpact: "Restore service functionality",
			AutoApplicable:  false,
			Category:        "troubleshooting",
		}
		
	case ServiceAnomaly:
		return &collectors.ActionableItem{
			Title:           fmt.Sprintf("Anomalous behavior detected in %s", se.Service),
			Description:     fmt.Sprintf("Service %s shows pattern: %s", se.Service, se.Pattern),
			Commands: []string{
				fmt.Sprintf("systemctl status %s", se.Service),
				fmt.Sprintf("journalctl -u %s --since '1 hour ago'", se.Service),
			},
			Risk:            "medium",
			EstimatedImpact: "Prevent service failure",
			AutoApplicable:  false,
			Category:        "prevention",
		}
	}
	
	return nil
}

// recordError records an error
func (c *Collector) recordError(err error) {
	atomic.AddUint64(&c.errorCount, 1)
	
	c.healthMu.Lock()
	c.lastError = err
	c.lastErrorTime = time.Now()
	c.healthMu.Unlock()
}

// calculateEventsPerSecond calculates the current event rate
func (c *Collector) calculateEventsPerSecond() float64 {
	// Simple implementation - would need a rolling window for accuracy
	total := atomic.LoadUint64(&c.eventsCollected)
	if total == 0 {
		return 0
	}
	
	// This is a placeholder - real implementation would track time windows
	return float64(total) / time.Since(c.lastEventTime).Seconds()
}

// estimateMemoryUsage estimates memory usage in bytes
func (c *Collector) estimateMemoryUsage() uint64 {
	// Base estimate
	baseMemory := uint64(10 * 1024 * 1024) // 10MB base
	
	// Add buffer sizes
	bufferMemory := uint64(c.config.EventBufferSize * 1024) // Assume 1KB per event
	
	// Add service tracking overhead
	monitorStats := c.serviceMonitor.GetStats()
	serviceMemory := monitorStats.ServicesMonitored * 10240 // ~10KB per service
	
	return baseMemory + bufferMemory + serviceMemory
}