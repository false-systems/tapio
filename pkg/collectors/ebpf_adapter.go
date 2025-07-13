package collectors

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf"
	"github.com/yairfalse/tapio/pkg/types"
)

// EBPFAdapter adapts the existing eBPF memory collector to the new collector interface
type EBPFAdapter struct {
	// Core components
	memoryCollector *ebpf.MemoryCollector
	config         *CollectorConfig
	
	// Event handling
	eventChan      chan *Event
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	
	// State
	enabled        atomic.Bool
	running        atomic.Bool
	
	// Statistics
	eventsProcessed uint64
	eventsDropped   uint64
	errorCount      uint64
	
	// Health tracking
	lastEventTime   time.Time
	lastError       error
	lastErrorTime   time.Time
	healthMu        sync.RWMutex
}

// NewEBPFAdapter creates a new eBPF collector adapter
func NewEBPFAdapter() (*EBPFAdapter, error) {
	memCollector, err := ebpf.NewMemoryCollector()
	if err != nil {
		return nil, fmt.Errorf("failed to create memory collector: %w", err)
	}
	
	return &EBPFAdapter{
		memoryCollector: memCollector,
		eventChan:      make(chan *Event, 10000),
	}, nil
}

// Name returns the collector name
func (e *EBPFAdapter) Name() string {
	return "ebpf"
}

// Type returns the collector type
func (e *EBPFAdapter) Type() string {
	return "kernel"
}

// IsEnabled returns if the collector is enabled
func (e *EBPFAdapter) IsEnabled() bool {
	return e.enabled.Load()
}

// Configure configures the collector
func (e *EBPFAdapter) Configure(config CollectorConfig) error {
	// Convert to eBPF-specific config
	ebpfConfig := ebpf.CollectorConfig{
		Enabled:             config.Enabled,
		MLPredictionEnabled: getConfigBool(config.Extra, "ml_prediction_enabled", true),
		EventRateLimit:      getConfigInt(config.Extra, "event_rate_limit", 1000),
		MinimumMemoryMB:     getConfigInt(config.Extra, "minimum_memory_mb", 10),
		PredictionThreshold: getConfigFloat(config.Extra, "prediction_threshold", 0.8),
		RingBufferSize:      getConfigInt(config.Extra, "ring_buffer_size", 8*1024*1024),
	}
	
	if err := e.memoryCollector.Configure(ebpfConfig); err != nil {
		return fmt.Errorf("failed to configure memory collector: %w", err)
	}
	
	e.config = &config
	e.enabled.Store(config.Enabled)
	
	return nil
}

// Start starts the collector
func (e *EBPFAdapter) Start(ctx context.Context) error {
	if !e.enabled.Load() {
		return fmt.Errorf("collector is disabled")
	}
	
	if !e.running.CompareAndSwap(false, true) {
		return fmt.Errorf("collector already running")
	}
	
	e.ctx, e.cancel = context.WithCancel(ctx)
	
	// Start the underlying memory collector
	if err := e.memoryCollector.Start(); err != nil {
		e.running.Store(false)
		return fmt.Errorf("failed to start memory collector: %w", err)
	}
	
	// Start event processing
	e.wg.Add(1)
	go e.processEvents()
	
	// Start health monitoring
	e.wg.Add(1)
	go e.monitorHealth()
	
	return nil
}

// Stop stops the collector
func (e *EBPFAdapter) Stop() error {
	if !e.running.CompareAndSwap(true, false) {
		return nil // Already stopped
	}
	
	// Cancel context
	if e.cancel != nil {
		e.cancel()
	}
	
	// Stop the underlying collector
	e.memoryCollector.Stop()
	
	// Wait for goroutines
	e.wg.Wait()
	
	// Close event channel
	close(e.eventChan)
	
	return nil
}

// Events returns the event channel
func (e *EBPFAdapter) Events() <-chan *Event {
	return e.eventChan
}

// Health returns the collector health
func (e *EBPFAdapter) Health() *Health {
	e.healthMu.RLock()
	defer e.healthMu.RUnlock()
	
	status := HealthStatusHealthy
	message := "Collector running normally"
	
	// Check if running
	if !e.running.Load() {
		status = HealthStatusStopped
		message = "Collector is stopped"
	} else if e.lastError != nil && time.Since(e.lastErrorTime) < 5*time.Minute {
		status = HealthStatusUnhealthy
		message = fmt.Sprintf("Recent error: %v", e.lastError)
	} else if time.Since(e.lastEventTime) > 1*time.Minute {
		status = HealthStatusDegraded
		message = "No events received in last minute"
	}
	
	// Get eBPF-specific metrics
	ebpfStats := e.memoryCollector.GetStats()
	
	return &Health{
		Status:          status,
		Message:         message,
		LastEventTime:   e.lastEventTime,
		EventsProcessed: atomic.LoadUint64(&e.eventsProcessed),
		EventsDropped:   atomic.LoadUint64(&e.eventsDropped),
		ErrorCount:      atomic.LoadUint64(&e.errorCount),
		LastError:       e.lastError,
		LastErrorTime:   e.lastErrorTime,
		Metrics: map[string]interface{}{
			"ring_buffer_lost_events": ebpfStats["lost_events"],
			"ml_predictions_made":     ebpfStats["predictions_made"],
			"processes_tracked":       ebpfStats["processes_tracked"],
			"memory_events_total":     ebpfStats["memory_events"],
			"oom_predictions":         ebpfStats["oom_predictions"],
		},
	}
}

// GetStats returns collector statistics
func (e *EBPFAdapter) GetStats() *Stats {
	ebpfStats := e.memoryCollector.GetStats()
	
	return &Stats{
		EventsTotal:     atomic.LoadUint64(&e.eventsProcessed),
		EventsDropped:   atomic.LoadUint64(&e.eventsDropped),
		EventsPerSecond: calculateEventRate(e.eventsProcessed),
		ErrorCount:      atomic.LoadUint64(&e.errorCount),
		LastEventTime:   e.lastEventTime,
		CollectorSpecific: map[string]interface{}{
			"kernel_version_compatible": ebpfStats["kernel_compatible"],
			"bpf_programs_loaded":      ebpfStats["programs_loaded"],
			"ring_buffer_size_mb":      ebpfStats["ring_buffer_size_mb"],
			"ml_model_loaded":          ebpfStats["ml_model_loaded"],
			"prediction_accuracy":      ebpfStats["prediction_accuracy"],
		},
	}
}

// processEvents processes events from the eBPF collector
func (e *EBPFAdapter) processEvents() {
	defer e.wg.Done()
	
	// Create subscription to memory events
	sub := e.memoryCollector.Subscribe()
	defer e.memoryCollector.Unsubscribe(sub)
	
	for {
		select {
		case <-e.ctx.Done():
			return
			
		case memEvent := <-sub:
			// Convert eBPF event to unified format
			event := e.convertToUnifiedEvent(memEvent)
			
			// Try to send event
			select {
			case e.eventChan <- event:
				atomic.AddUint64(&e.eventsProcessed, 1)
				e.updateLastEventTime()
			default:
				// Channel full, drop event
				atomic.AddUint64(&e.eventsDropped, 1)
			}
		}
	}
}

// convertToUnifiedEvent converts eBPF memory event to unified event format
func (e *EBPFAdapter) convertToUnifiedEvent(memEvent *ebpf.MemoryEvent) *Event {
	// Determine event type and severity
	eventType := EventTypeMetric
	severity := SeverityInfo
	
	if memEvent.EventType == "oom_kill" {
		eventType = EventTypeAlert
		severity = SeverityCritical
	} else if memEvent.Prediction != nil && memEvent.Prediction.Probability > 0.8 {
		eventType = EventTypeAnomaly
		severity = SeverityWarning
	}
	
	// Build event
	event := &Event{
		ID:        generateEventID(),
		Timestamp: memEvent.Timestamp,
		Type:      eventType,
		Severity:  severity,
		Source: EventSource{
			Collector: "ebpf",
			Component: "memory",
			Node:      getNodeName(),
		},
		Data: map[string]interface{}{
			"process": map[string]interface{}{
				"pid":           memEvent.PID,
				"name":          memEvent.ProcessName,
				"container_id":  memEvent.ContainerID,
				"in_container":  memEvent.InContainer,
			},
			"memory": map[string]interface{}{
				"event_type":      memEvent.EventType,
				"size_bytes":      memEvent.Size,
				"total_bytes":     memEvent.TotalMemory,
				"usage_percent":   calculateMemoryPercent(memEvent),
			},
		},
		Metadata: EventMetadata{
			Importance:  calculateImportance(memEvent),
			Reliability: 0.95, // eBPF data is highly reliable
			Correlation: generateCorrelationIDs(memEvent),
		},
	}
	
	// Add ML prediction if available
	if memEvent.Prediction != nil {
		event.Data["prediction"] = map[string]interface{}{
			"type":              "oom",
			"probability":       memEvent.Prediction.Probability,
			"time_to_oom_secs":  memEvent.Prediction.TimeToOOM.Seconds(),
			"confidence":        memEvent.Prediction.Confidence,
			"features":          memEvent.Prediction.Features,
		}
		
		// Add suggested actions for high-probability predictions
		if memEvent.Prediction.Probability > 0.8 {
			event.Data["suggested_actions"] = []string{
				fmt.Sprintf("Consider increasing memory limit for %s", memEvent.ProcessName),
				"Review recent memory allocation patterns",
				"Check for memory leaks in application",
			}
		}
	}
	
	// Add Kubernetes context if available
	if memEvent.PodName != "" {
		event.Data["kubernetes"] = map[string]interface{}{
			"pod":       memEvent.PodName,
			"namespace": memEvent.Namespace,
			"container": memEvent.ContainerName,
		}
	}
	
	return event
}

// monitorHealth monitors collector health
func (e *EBPFAdapter) monitorHealth() {
	defer e.wg.Done()
	
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-e.ctx.Done():
			return
			
		case <-ticker.C:
			// Check eBPF collector health
			if !e.memoryCollector.IsHealthy() {
				e.recordError(fmt.Errorf("eBPF memory collector unhealthy"))
			}
			
			// Check event flow
			if time.Since(e.lastEventTime) > 2*time.Minute {
				e.recordError(fmt.Errorf("no events received for 2 minutes"))
			}
		}
	}
}

// Helper functions

func (e *EBPFAdapter) updateLastEventTime() {
	e.healthMu.Lock()
	e.lastEventTime = time.Now()
	e.healthMu.Unlock()
}

func (e *EBPFAdapter) recordError(err error) {
	e.healthMu.Lock()
	e.lastError = err
	e.lastErrorTime = time.Now()
	e.healthMu.Unlock()
	atomic.AddUint64(&e.errorCount, 1)
}

func calculateMemoryPercent(event *ebpf.MemoryEvent) float64 {
	// This would be calculated based on system or container limits
	return 0.0
}

func calculateImportance(event *ebpf.MemoryEvent) float32 {
	// High importance for OOM events and high-probability predictions
	if event.EventType == "oom_kill" {
		return 1.0
	}
	if event.Prediction != nil && event.Prediction.Probability > 0.8 {
		return float32(event.Prediction.Probability)
	}
	return 0.3
}

func generateCorrelationIDs(event *ebpf.MemoryEvent) []string {
	ids := []string{
		fmt.Sprintf("pid:%d", event.PID),
		fmt.Sprintf("process:%s", event.ProcessName),
	}
	if event.ContainerID != "" {
		ids = append(ids, fmt.Sprintf("container:%s", event.ContainerID))
	}
	if event.PodName != "" {
		ids = append(ids, fmt.Sprintf("pod:%s", event.PodName))
	}
	return ids
}

func getConfigBool(extra map[string]interface{}, key string, defaultVal bool) bool {
	if val, ok := extra[key].(bool); ok {
		return val
	}
	return defaultVal
}

func getConfigInt(extra map[string]interface{}, key string, defaultVal int) int {
	if val, ok := extra[key].(float64); ok {
		return int(val)
	}
	return defaultVal
}

func getConfigFloat(extra map[string]interface{}, key string, defaultVal float64) float64 {
	if val, ok := extra[key].(float64); ok {
		return val
	}
	return defaultVal
}

func calculateEventRate(totalEvents uint64) float64 {
	// This would calculate events per second based on time window
	return 0.0
}

func generateEventID() string {
	// This would generate a unique event ID
	return fmt.Sprintf("ebpf-%d", time.Now().UnixNano())
}

func getNodeName() string {
	// This would get the actual node name from environment
	return "node-1"
}