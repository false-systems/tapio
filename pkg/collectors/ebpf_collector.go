package collectors

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/ebpf"
)

// ebpfCollector implements the Collector interface for eBPF data collection
type ebpfCollector struct {
	config       CollectorConfig
	ebpfClient   ebpf.EnhancedCollector
	
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

// EBPFCollectorFactory implements Factory for eBPF collectors
type EBPFCollectorFactory struct{}

// NewEBPFCollectorFactory creates a new eBPF collector factory
func NewEBPFCollectorFactory() Factory {
	return &EBPFCollectorFactory{}
}

// CreateCollector creates a new eBPF collector instance
func (f *EBPFCollectorFactory) CreateCollector(config CollectorConfig) (Collector, error) {
	if config.Type != "ebpf" {
		return nil, fmt.Errorf("invalid collector type: %s, expected: ebpf", config.Type)
	}
	
	// Validate eBPF-specific configuration
	if err := f.ValidateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid eBPF configuration: %w", err)
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	collector := &ebpfCollector{
		config:    config,
		eventChan: make(chan *Event, config.EventBufferSize),
		ctx:       ctx,
		cancel:    cancel,
	}
	
	collector.enabled.Store(config.Enabled)
	
	// Initialize eBPF client
	ebpfConfig := ebpf.Config{
		EnableNetworkMonitoring: getBoolFromConfig(config.CollectorSpecific, "enable_network", true),
		EnableMemoryMonitoring:  getBoolFromConfig(config.CollectorSpecific, "enable_memory", true),
		EnableProcessMonitoring: getBoolFromConfig(config.CollectorSpecific, "enable_process", true),
		SamplingRate:           config.SamplingRate,
		BufferSize:             config.EventBufferSize,
	}
	
	ebpfClient, err := ebpf.NewEnhancedCollector(ebpfConfig)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create eBPF client: %w", err)
	}
	
	collector.ebpfClient = ebpfClient
	
	return collector, nil
}

// SupportedTypes returns the collector types this factory can create
func (f *EBPFCollectorFactory) SupportedTypes() []string {
	return []string{"ebpf"}
}

// ValidateConfig validates a configuration for eBPF collector
func (f *EBPFCollectorFactory) ValidateConfig(config CollectorConfig) error {
	if config.Type != "ebpf" {
		return fmt.Errorf("invalid collector type: %s", config.Type)
	}
	
	// Validate eBPF-specific settings
	if config.SamplingRate < 0.0 || config.SamplingRate > 1.0 {
		return fmt.Errorf("sampling_rate must be between 0.0 and 1.0")
	}
	
	if config.EventBufferSize <= 0 {
		return fmt.Errorf("event_buffer_size must be positive")
	}
	
	// Check for required eBPF capabilities
	if !ebpf.IsAvailable() {
		return fmt.Errorf("eBPF is not available on this system")
	}
	
	return nil
}

// Collector interface implementation

// Name returns the unique name of this collector
func (ec *ebpfCollector) Name() string {
	return ec.config.Name
}

// Type returns the collector type
func (ec *ebpfCollector) Type() string {
	return "ebpf"
}

// Start begins data collection
func (ec *ebpfCollector) Start(ctx context.Context) error {
	if !ec.started.CompareAndSwap(false, true) {
		return fmt.Errorf("eBPF collector already started")
	}
	
	if !ec.enabled.Load() {
		return fmt.Errorf("eBPF collector is disabled")
	}
	
	// Start eBPF client
	if err := ec.ebpfClient.Start(ctx); err != nil {
		ec.recordError(fmt.Errorf("failed to start eBPF client: %w", err))
		return err
	}
	
	// Start event processing
	ec.wg.Add(1)
	go ec.processEBPFEvents()
	
	return nil
}

// Stop gracefully stops the collector
func (ec *ebpfCollector) Stop() error {
	if !ec.stopped.CompareAndSwap(false, true) {
		return nil // Already stopped
	}
	
	// Cancel context
	ec.cancel()
	
	// Stop eBPF client
	if err := ec.ebpfClient.Stop(); err != nil {
		ec.recordError(fmt.Errorf("failed to stop eBPF client: %w", err))
	}
	
	// Wait for goroutines
	ec.wg.Wait()
	
	// Close event channel
	close(ec.eventChan)
	
	return nil
}

// Events returns a channel that emits events from this collector
func (ec *ebpfCollector) Events() <-chan *Event {
	return ec.eventChan
}

// Health returns the current health status of the collector
func (ec *ebpfCollector) Health() *Health {
	ec.healthMu.RLock()
	defer ec.healthMu.RUnlock()
	
	status := HealthStatusHealthy
	message := "Operating normally"
	
	if ec.stopped.Load() {
		status = HealthStatusStopped
		message = "Stopped"
	} else if !ec.enabled.Load() {
		status = HealthStatusStopped
		message = "Disabled"
	} else if atomic.LoadUint64(&ec.errorCount) > 0 {
		if time.Since(ec.lastErrorTime) < 5*time.Minute {
			status = HealthStatusDegraded
			message = fmt.Sprintf("Recent error: %s", ec.lastError)
		}
	}
	
	// Check if events are flowing
	if status == HealthStatusHealthy && time.Since(ec.lastEventTime) > 2*time.Minute {
		status = HealthStatusDegraded
		message = "No recent events"
	}
	
	return &Health{
		Status:          status,
		Message:         message,
		LastEventTime:   ec.lastEventTime,
		EventsProcessed: atomic.LoadUint64(&ec.eventsCollected),
		EventsDropped:   atomic.LoadUint64(&ec.eventsDropped),
		ErrorCount:      atomic.LoadUint64(&ec.errorCount),
		LastError:       ec.lastError,
		LastErrorTime:   ec.lastErrorTime,
		Metrics: map[string]interface{}{
			"ebpf_programs_loaded": ec.ebpfClient.GetStats().ProgramsLoaded,
			"buffer_utilization":   ec.ebpfClient.GetStats().BufferUtilization,
		},
	}
}

// GetStats returns collector-specific statistics
func (ec *ebpfCollector) GetStats() *Stats {
	ebpfStats := ec.ebpfClient.GetStats()
	
	return &Stats{
		EventsCollected:   atomic.LoadUint64(&ec.eventsCollected),
		EventsDropped:     atomic.LoadUint64(&ec.eventsDropped),
		EventsFiltered:    0, // eBPF filtering happens in kernel
		BytesProcessed:    ebpfStats.BytesProcessed,
		ErrorCount:        atomic.LoadUint64(&ec.errorCount),
		EventsPerSecond:   ebpfStats.EventsPerSecond,
		AvgProcessingTime: ebpfStats.AvgProcessingTime,
		MaxProcessingTime: ebpfStats.MaxProcessingTime,
		MemoryUsageMB:     ebpfStats.MemoryUsageMB,
		CPUUsagePercent:   ebpfStats.CPUUsagePercent,
		StartTime:         ebpfStats.StartTime,
		LastEventTime:     ec.lastEventTime,
		Uptime:            time.Since(ebpfStats.StartTime),
		CollectorMetrics: map[string]interface{}{
			"programs_loaded":     ebpfStats.ProgramsLoaded,
			"maps_created":        ebpfStats.MapsCreated,
			"buffer_utilization":  ebpfStats.BufferUtilization,
			"kernel_events":       ebpfStats.KernelEventsProcessed,
			"user_events":         ebpfStats.UserEventsProcessed,
		},
	}
}

// Configure updates the collector configuration
func (ec *ebpfCollector) Configure(config CollectorConfig) error {
	if config.Type != "ebpf" {
		return fmt.Errorf("invalid collector type: %s", config.Type)
	}
	
	// Update configuration
	ec.config = config
	ec.enabled.Store(config.Enabled)
	
	// Reconfigure eBPF client if needed
	ebpfConfig := ebpf.Config{
		EnableNetworkMonitoring: getBoolFromConfig(config.CollectorSpecific, "enable_network", true),
		EnableMemoryMonitoring:  getBoolFromConfig(config.CollectorSpecific, "enable_memory", true),
		EnableProcessMonitoring: getBoolFromConfig(config.CollectorSpecific, "enable_process", true),
		SamplingRate:           config.SamplingRate,
		BufferSize:             config.EventBufferSize,
	}
	
	return ec.ebpfClient.Configure(ebpfConfig)
}

// IsEnabled returns whether the collector is currently enabled
func (ec *ebpfCollector) IsEnabled() bool {
	return ec.enabled.Load()
}

// processEBPFEvents processes events from the eBPF client
func (ec *ebpfCollector) processEBPFEvents() {
	defer ec.wg.Done()
	
	for {
		select {
		case <-ec.ctx.Done():
			return
			
		case ebpfEvent, ok := <-ec.ebpfClient.Events():
			if !ok {
				return // Channel closed
			}
			
			// Convert eBPF event to collector event
			collectorEvent := ec.convertEBPFEvent(ebpfEvent)
			if collectorEvent == nil {
				continue
			}
			
			// Apply rate limiting
			if !ec.shouldProcessEvent() {
				atomic.AddUint64(&ec.eventsDropped, 1)
				continue
			}
			
			// Update last event time
			ec.healthMu.Lock()
			ec.lastEventTime = time.Now()
			ec.healthMu.Unlock()
			
			// Try to send event
			select {
			case ec.eventChan <- collectorEvent:
				atomic.AddUint64(&ec.eventsCollected, 1)
			default:
				atomic.AddUint64(&ec.eventsDropped, 1)
			}
		}
	}
}

// convertEBPFEvent converts an eBPF event to a collector event
func (ec *ebpfCollector) convertEBPFEvent(ebpfEvent *ebpf.Event) *Event {
	if ebpfEvent == nil {
		return nil
	}
	
	// Determine category and type based on eBPF event
	category, eventType := ec.categorizeEBPFEvent(ebpfEvent)
	
	// Create collector event
	event := &Event{
		ID:          fmt.Sprintf("ebpf_%d_%d", time.Now().UnixNano(), ebpfEvent.ID),
		Timestamp:   ebpfEvent.Timestamp,
		Source:      ec.config.Name,
		SourceType:  "ebpf",
		CollectorID: ec.config.Name,
		Type:        eventType,
		Category:    category,
		Severity:    ec.determineSeverity(ebpfEvent),
		Data:        ec.extractEventData(ebpfEvent),
		Attributes:  ec.extractAttributes(ebpfEvent),
		Labels:      ec.config.Labels,
		Context:     ec.extractContext(ebpfEvent),
	}
	
	// Add actionable recommendations if applicable
	if actionable := ec.generateActionable(ebpfEvent); actionable != nil {
		event.Actionable = actionable
	}
	
	return event
}

// categorizeEBPFEvent determines the category and type of an eBPF event
func (ec *ebpfCollector) categorizeEBPFEvent(event *ebpf.Event) (Category, string) {
	switch event.Type {
	case ebpf.EventTypeNetworkConnection:
		return CategoryNetwork, "network_connection"
	case ebpf.EventTypeNetworkPacket:
		return CategoryNetwork, "network_packet"
	case ebpf.EventTypeMemoryAllocation:
		return CategoryMemory, "memory_allocation"
	case ebpf.EventTypeMemoryOOM:
		return CategoryMemory, "memory_oom"
	case ebpf.EventTypeProcessStart:
		return CategoryProcess, "process_start"
	case ebpf.EventTypeProcessExit:
		return CategoryProcess, "process_exit"
	case ebpf.EventTypeFileOpen:
		return CategorySystem, "file_open"
	case ebpf.EventTypeSystemCall:
		return CategorySystem, "syscall"
	default:
		return CategorySystem, "unknown"
	}
}

// Additional helper methods...

func (ec *ebpfCollector) determineSeverity(event *ebpf.Event) Severity {
	// Implement severity determination logic based on eBPF event
	switch event.Type {
	case ebpf.EventTypeMemoryOOM:
		return SeverityCritical
	case ebpf.EventTypeNetworkConnection:
		return SeverityLow
	default:
		return SeverityMedium
	}
}

func (ec *ebpfCollector) extractEventData(event *ebpf.Event) map[string]interface{} {
	data := make(map[string]interface{})
	
	// Copy relevant fields from eBPF event
	if event.NetworkData != nil {
		data["src_ip"] = event.NetworkData.SrcIP
		data["dst_ip"] = event.NetworkData.DstIP
		data["src_port"] = event.NetworkData.SrcPort
		data["dst_port"] = event.NetworkData.DstPort
		data["protocol"] = event.NetworkData.Protocol
		data["bytes_sent"] = event.NetworkData.BytesSent
		data["bytes_received"] = event.NetworkData.BytesReceived
	}
	
	if event.ProcessData != nil {
		data["pid"] = event.ProcessData.PID
		data["ppid"] = event.ProcessData.PPID
		data["uid"] = event.ProcessData.UID
		data["gid"] = event.ProcessData.GID
		data["comm"] = event.ProcessData.Comm
		data["cmdline"] = event.ProcessData.Cmdline
	}
	
	if event.MemoryData != nil {
		data["memory_usage"] = event.MemoryData.Usage
		data["memory_limit"] = event.MemoryData.Limit
		data["oom_score"] = event.MemoryData.OOMScore
	}
	
	return data
}

func (ec *ebpfCollector) extractAttributes(event *ebpf.Event) map[string]interface{} {
	attributes := make(map[string]interface{})
	
	attributes["ebpf_program"] = event.ProgramName
	attributes["kernel_version"] = event.KernelVersion
	attributes["cpu_id"] = event.CPUID
	
	return attributes
}

func (ec *ebpfCollector) extractContext(event *ebpf.Event) *EventContext {
	context := &EventContext{}
	
	if event.ProcessData != nil {
		context.PID = event.ProcessData.PID
		context.ProcessName = event.ProcessData.Comm
		context.PPID = event.ProcessData.PPID
		context.UID = event.ProcessData.UID
		context.GID = event.ProcessData.GID
	}
	
	if event.NetworkData != nil {
		context.SrcIP = event.NetworkData.SrcIP
		context.DstIP = event.NetworkData.DstIP
		context.SrcPort = uint16(event.NetworkData.SrcPort)
		context.DstPort = uint16(event.NetworkData.DstPort)
		context.Protocol = event.NetworkData.Protocol
	}
	
	// Add Kubernetes context if available
	if event.K8sData != nil {
		context.Namespace = event.K8sData.Namespace
		context.Pod = event.K8sData.PodName
		context.Container = event.K8sData.ContainerName
		context.Node = event.K8sData.NodeName
		context.Labels = event.K8sData.Labels
	}
	
	return context
}

func (ec *ebpfCollector) generateActionable(event *ebpf.Event) *ActionableItem {
	// Generate actionable recommendations based on event type
	switch event.Type {
	case ebpf.EventTypeMemoryOOM:
		return &ActionableItem{
			Title:           "Increase memory limit",
			Description:     "Process was killed due to out-of-memory condition",
			Commands:        []string{"kubectl patch deployment <name> -p '{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"<container>\",\"resources\":{\"limits\":{\"memory\":\"512Mi\"}}}]}}}}'"},
			Risk:            "low",
			EstimatedImpact: "Prevents OOM kills, may increase memory usage",
			AutoApplicable:  false,
			Category:        "resource",
		}
	}
	
	return nil
}

func (ec *ebpfCollector) shouldProcessEvent() bool {
	// Implement rate limiting logic
	if ec.config.MaxEventsPerSec > 0 {
		// Simple rate limiting - could be more sophisticated
		return true
	}
	return true
}

func (ec *ebpfCollector) recordError(err error) {
	atomic.AddUint64(&ec.errorCount, 1)
	
	ec.healthMu.Lock()
	ec.lastError = err.Error()
	ec.lastErrorTime = time.Now()
	ec.healthMu.Unlock()
}

// Helper function to get boolean values from config
func getBoolFromConfig(config map[string]interface{}, key string, defaultValue bool) bool {
	if value, ok := config[key].(bool); ok {
		return value
	}
	return defaultValue
}