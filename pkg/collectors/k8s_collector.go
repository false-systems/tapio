package collectors

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/k8s"
)

// k8sCollector implements the Collector interface for Kubernetes API data collection
type k8sCollector struct {
	config       CollectorConfig
	k8sClient    k8s.EnhancedClient
	
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

// K8sCollectorFactory implements Factory for Kubernetes collectors
type K8sCollectorFactory struct{}

// NewK8sCollectorFactory creates a new Kubernetes collector factory
func NewK8sCollectorFactory() Factory {
	return &K8sCollectorFactory{}
}

// CreateCollector creates a new Kubernetes collector instance
func (f *K8sCollectorFactory) CreateCollector(config CollectorConfig) (Collector, error) {
	if config.Type != "k8s" {
		return nil, fmt.Errorf("invalid collector type: %s, expected: k8s", config.Type)
	}
	
	// Validate K8s-specific configuration
	if err := f.ValidateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid K8s configuration: %w", err)
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	collector := &k8sCollector{
		config:    config,
		eventChan: make(chan *Event, config.EventBufferSize),
		ctx:       ctx,
		cancel:    cancel,
	}
	
	collector.enabled.Store(config.Enabled)
	
	// Initialize Kubernetes client
	k8sConfig := k8s.ClientConfig{
		InCluster:     getBoolFromConfig(config.CollectorSpecific, "in_cluster", true),
		KubeConfig:    getStringFromConfig(config.CollectorSpecific, "kube_config", ""),
		Namespace:     getStringFromConfig(config.CollectorSpecific, "namespace", ""),
		WatchTimeout:  getDurationFromConfig(config.CollectorSpecific, "watch_timeout", 30*time.Second),
		RetryBackoff:  getDurationFromConfig(config.CollectorSpecific, "retry_backoff", 5*time.Second),
		MaxRetries:    getIntFromConfig(config.CollectorSpecific, "max_retries", 3),
	}
	
	k8sClient, err := k8s.NewEnhancedClient(k8sConfig)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create K8s client: %w", err)
	}
	
	collector.k8sClient = k8sClient
	
	return collector, nil
}

// SupportedTypes returns the collector types this factory can create
func (f *K8sCollectorFactory) SupportedTypes() []string {
	return []string{"k8s"}
}

// ValidateConfig validates a configuration for K8s collector
func (f *K8sCollectorFactory) ValidateConfig(config CollectorConfig) error {
	if config.Type != "k8s" {
		return fmt.Errorf("invalid collector type: %s", config.Type)
	}
	
	if config.EventBufferSize <= 0 {
		return fmt.Errorf("event_buffer_size must be positive")
	}
	
	return nil
}

// Collector interface implementation

// Name returns the unique name of this collector
func (kc *k8sCollector) Name() string {
	return kc.config.Name
}

// Type returns the collector type
func (kc *k8sCollector) Type() string {
	return "k8s"
}

// Start begins data collection
func (kc *k8sCollector) Start(ctx context.Context) error {
	if !kc.started.CompareAndSwap(false, true) {
		return fmt.Errorf("K8s collector already started")
	}
	
	if !kc.enabled.Load() {
		return fmt.Errorf("K8s collector is disabled")
	}
	
	// Start K8s client
	if err := kc.k8sClient.Start(ctx); err != nil {
		kc.recordError(fmt.Errorf("failed to start K8s client: %w", err))
		return err
	}
	
	// Start event processing
	kc.wg.Add(1)
	go kc.processK8sEvents()
	
	return nil
}

// Stop gracefully stops the collector
func (kc *k8sCollector) Stop() error {
	if !kc.stopped.CompareAndSwap(false, true) {
		return nil // Already stopped
	}
	
	// Cancel context
	kc.cancel()
	
	// Stop K8s client
	if err := kc.k8sClient.Stop(); err != nil {
		kc.recordError(fmt.Errorf("failed to stop K8s client: %w", err))
	}
	
	// Wait for goroutines
	kc.wg.Wait()
	
	// Close event channel
	close(kc.eventChan)
	
	return nil
}

// Events returns a channel that emits events from this collector
func (kc *k8sCollector) Events() <-chan *Event {
	return kc.eventChan
}

// Health returns the current health status of the collector
func (kc *k8sCollector) Health() *Health {
	kc.healthMu.RLock()
	defer kc.healthMu.RUnlock()
	
	status := HealthStatusHealthy
	message := "Operating normally"
	
	if kc.stopped.Load() {
		status = HealthStatusStopped
		message = "Stopped"
	} else if !kc.enabled.Load() {
		status = HealthStatusStopped
		message = "Disabled"
	} else if !kc.k8sClient.IsConnected() {
		status = HealthStatusUnhealthy
		message = "Disconnected from Kubernetes API"
	} else if atomic.LoadUint64(&kc.errorCount) > 0 {
		if time.Since(kc.lastErrorTime) < 5*time.Minute {
			status = HealthStatusDegraded
			message = fmt.Sprintf("Recent error: %s", kc.lastError)
		}
	}
	
	return &Health{
		Status:          status,
		Message:         message,
		LastEventTime:   kc.lastEventTime,
		EventsProcessed: atomic.LoadUint64(&kc.eventsCollected),
		EventsDropped:   atomic.LoadUint64(&kc.eventsDropped),
		ErrorCount:      atomic.LoadUint64(&kc.errorCount),
		LastError:       kc.lastError,
		LastErrorTime:   kc.lastErrorTime,
		Metrics: map[string]interface{}{
			"api_connected":     kc.k8sClient.IsConnected(),
			"watches_active":    kc.k8sClient.GetStats().ActiveWatches,
			"api_calls_total":   kc.k8sClient.GetStats().APICalls,
		},
	}
}

// GetStats returns collector-specific statistics
func (kc *k8sCollector) GetStats() *Stats {
	k8sStats := kc.k8sClient.GetStats()
	
	return &Stats{
		EventsCollected:   atomic.LoadUint64(&kc.eventsCollected),
		EventsDropped:     atomic.LoadUint64(&kc.eventsDropped),
		EventsFiltered:    k8sStats.EventsFiltered,
		BytesProcessed:    k8sStats.BytesProcessed,
		ErrorCount:        atomic.LoadUint64(&kc.errorCount),
		EventsPerSecond:   k8sStats.EventsPerSecond,
		AvgProcessingTime: k8sStats.AvgProcessingTime,
		MaxProcessingTime: k8sStats.MaxProcessingTime,
		MemoryUsageMB:     k8sStats.MemoryUsageMB,
		CPUUsagePercent:   k8sStats.CPUUsagePercent,
		StartTime:         k8sStats.StartTime,
		LastEventTime:     kc.lastEventTime,
		Uptime:            time.Since(k8sStats.StartTime),
		CollectorMetrics: map[string]interface{}{
			"api_connected":      kc.k8sClient.IsConnected(),
			"active_watches":     k8sStats.ActiveWatches,
			"api_calls_total":    k8sStats.APICalls,
			"api_errors_total":   k8sStats.APIErrors,
			"cache_hit_rate":     k8sStats.CacheHitRate,
			"objects_cached":     k8sStats.ObjectsCached,
		},
	}
}

// Configure updates the collector configuration
func (kc *k8sCollector) Configure(config CollectorConfig) error {
	if config.Type != "k8s" {
		return fmt.Errorf("invalid collector type: %s", config.Type)
	}
	
	// Update configuration
	kc.config = config
	kc.enabled.Store(config.Enabled)
	
	return nil
}

// IsEnabled returns whether the collector is currently enabled
func (kc *k8sCollector) IsEnabled() bool {
	return kc.enabled.Load()
}

// processK8sEvents processes events from the K8s client
func (kc *k8sCollector) processK8sEvents() {
	defer kc.wg.Done()
	
	for {
		select {
		case <-kc.ctx.Done():
			return
			
		case k8sEvent, ok := <-kc.k8sClient.Events():
			if !ok {
				return // Channel closed
			}
			
			// Convert K8s event to collector event
			collectorEvent := kc.convertK8sEvent(k8sEvent)
			if collectorEvent == nil {
				continue
			}
			
			// Apply filtering if needed
			if !kc.shouldProcessEvent(collectorEvent) {
				atomic.AddUint64(&kc.eventsDropped, 1)
				continue
			}
			
			// Update last event time
			kc.healthMu.Lock()
			kc.lastEventTime = time.Now()
			kc.healthMu.Unlock()
			
			// Try to send event
			select {
			case kc.eventChan <- collectorEvent:
				atomic.AddUint64(&kc.eventsCollected, 1)
			default:
				atomic.AddUint64(&kc.eventsDropped, 1)
			}
		}
	}
}

// convertK8sEvent converts a K8s event to a collector event
func (kc *k8sCollector) convertK8sEvent(k8sEvent *k8s.Event) *Event {
	if k8sEvent == nil {
		return nil
	}
	
	// Determine category and type based on K8s event
	category, eventType := kc.categorizeK8sEvent(k8sEvent)
	
	// Create collector event
	event := &Event{
		ID:          fmt.Sprintf("k8s_%s_%d", k8sEvent.UID, time.Now().UnixNano()),
		Timestamp:   k8sEvent.Timestamp,
		Source:      kc.config.Name,
		SourceType:  "k8s",
		CollectorID: kc.config.Name,
		Type:        eventType,
		Category:    category,
		Severity:    kc.determineSeverity(k8sEvent),
		Data:        kc.extractEventData(k8sEvent),
		Attributes:  kc.extractAttributes(k8sEvent),
		Labels:      kc.mergeLabels(kc.config.Labels, k8sEvent.Labels),
		Context:     kc.extractContext(k8sEvent),
	}
	
	// Add actionable recommendations if applicable
	if actionable := kc.generateActionable(k8sEvent); actionable != nil {
		event.Actionable = actionable
	}
	
	return event
}

// categorizeK8sEvent determines the category and type of a K8s event
func (kc *k8sCollector) categorizeK8sEvent(event *k8s.Event) (Category, string) {
	switch event.EventType {
	case k8s.EventTypePodCreated, k8s.EventTypePodUpdated, k8s.EventTypePodDeleted:
		return CategoryKubernetes, fmt.Sprintf("pod_%s", event.Action)
	case k8s.EventTypeDeploymentScaled:
		return CategoryKubernetes, "deployment_scaled"
	case k8s.EventTypeNodeReady, k8s.EventTypeNodeNotReady:
		return CategoryKubernetes, fmt.Sprintf("node_%s", event.Action)
	case k8s.EventTypePodOOMKilled:
		return CategoryMemory, "pod_oom_killed"
	case k8s.EventTypePodCrashLoop:
		return CategoryApplication, "pod_crash_loop"
	default:
		return CategoryKubernetes, "generic"
	}
}

func (kc *k8sCollector) determineSeverity(event *k8s.Event) Severity {
	switch event.EventType {
	case k8s.EventTypePodOOMKilled, k8s.EventTypePodCrashLoop:
		return SeverityCritical
	case k8s.EventTypeNodeNotReady:
		return SeverityHigh
	case k8s.EventTypePodCreated, k8s.EventTypePodDeleted:
		return SeverityLow
	default:
		return SeverityMedium
	}
}

func (kc *k8sCollector) extractEventData(event *k8s.Event) map[string]interface{} {
	data := make(map[string]interface{})
	
	data["namespace"] = event.Namespace
	data["kind"] = event.ObjectKind
	data["name"] = event.ObjectName
	data["action"] = event.Action
	data["reason"] = event.Reason
	data["message"] = event.Message
	
	if event.ResourceData != nil {
		data["resource_version"] = event.ResourceData.ResourceVersion
		data["generation"] = event.ResourceData.Generation
		data["creation_timestamp"] = event.ResourceData.CreationTimestamp
	}
	
	return data
}

func (kc *k8sCollector) extractAttributes(event *k8s.Event) map[string]interface{} {
	attributes := make(map[string]interface{})
	
	attributes["cluster_name"] = kc.k8sClient.GetClusterName()
	attributes["api_version"] = event.APIVersion
	attributes["event_source"] = event.Source
	
	return attributes
}

func (kc *k8sCollector) extractContext(event *k8s.Event) *EventContext {
	context := &EventContext{
		Namespace: event.Namespace,
		Labels:    event.Labels,
	}
	
	// Extract specific context based on object kind
	switch event.ObjectKind {
	case "Pod":
		context.Pod = event.ObjectName
		if event.ResourceData != nil && event.ResourceData.PodData != nil {
			context.Container = event.ResourceData.PodData.ContainerName
			context.Node = event.ResourceData.PodData.NodeName
		}
	case "Node":
		context.Node = event.ObjectName
	case "Service":
		context.Service = event.ObjectName
	}
	
	// Determine workload type
	if owner := event.OwnerReference; owner != nil {
		context.WorkloadType = owner.Kind
	}
	
	return context
}

func (kc *k8sCollector) generateActionable(event *k8s.Event) *ActionableItem {
	switch event.EventType {
	case k8s.EventTypePodOOMKilled:
		return &ActionableItem{
			Title:           "Increase memory limit",
			Description:     "Pod was killed due to out-of-memory condition",
			Commands:        []string{fmt.Sprintf("kubectl patch deployment %s -n %s -p '{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"<container>\",\"resources\":{\"limits\":{\"memory\":\"512Mi\"}}}]}}}}'", event.ObjectName, event.Namespace)},
			Risk:            "low",
			EstimatedImpact: "Prevents OOM kills, may increase memory usage",
			AutoApplicable:  false,
			Category:        "resource",
		}
	case k8s.EventTypePodCrashLoop:
		return &ActionableItem{
			Title:           "Check application logs",
			Description:     "Pod is in crash loop, investigate logs for root cause",
			Commands:        []string{fmt.Sprintf("kubectl logs %s -n %s --previous", event.ObjectName, event.Namespace)},
			Risk:            "low",
			EstimatedImpact: "Helps identify the cause of crashes",
			AutoApplicable:  false,
			Category:        "troubleshooting",
		}
	}
	
	return nil
}

func (kc *k8sCollector) shouldProcessEvent(event *Event) bool {
	// Apply severity filtering
	if event.Severity < kc.config.MinSeverity {
		return false
	}
	
	// Apply category filtering
	if len(kc.config.ExcludeCategories) > 0 {
		for _, excludeCategory := range kc.config.ExcludeCategories {
			if event.Category == excludeCategory {
				return false
			}
		}
	}
	
	if len(kc.config.IncludeCategories) > 0 {
		included := false
		for _, includeCategory := range kc.config.IncludeCategories {
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

func (kc *k8sCollector) mergeLabels(configLabels, eventLabels map[string]string) map[string]string {
	merged := make(map[string]string)
	
	// Add config labels first
	for k, v := range configLabels {
		merged[k] = v
	}
	
	// Add event labels (can override config labels)
	for k, v := range eventLabels {
		merged[k] = v
	}
	
	return merged
}

func (kc *k8sCollector) recordError(err error) {
	atomic.AddUint64(&kc.errorCount, 1)
	
	kc.healthMu.Lock()
	kc.lastError = err.Error()
	kc.lastErrorTime = time.Now()
	kc.healthMu.Unlock()
}

// Helper functions for configuration extraction
func getStringFromConfig(config map[string]interface{}, key, defaultValue string) string {
	if value, ok := config[key].(string); ok {
		return value
	}
	return defaultValue
}

func getIntFromConfig(config map[string]interface{}, key string, defaultValue int) int {
	if value, ok := config[key].(int); ok {
		return value
	}
	return defaultValue
}

func getDurationFromConfig(config map[string]interface{}, key string, defaultValue time.Duration) time.Duration {
	if value, ok := config[key].(string); ok {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}