package k8s

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/yairfalse/tapio/pkg/collectors/unified"
	"github.com/yairfalse/tapio/pkg/k8s"
	"github.com/yairfalse/tapio/pkg/logging"
)

// Collector implements unified.Collector for Kubernetes API monitoring
type Collector struct {
	// Configuration
	config unified.CollectorConfig
	logger *logging.Logger

	// Kubernetes client
	k8sClient *k8s.Client

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

// NewCollector creates a new K8s collector
func NewCollector(config unified.CollectorConfig) (*Collector, error) {
	logger := logging.Development.WithComponent("k8s-collector")

	// Extract K8s-specific configuration
	k8sConfig, err := extractK8sConfig(config.Extra)
	if err != nil {
		return nil, fmt.Errorf("invalid K8s configuration: %w", err)
	}

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

	// Initialize Kubernetes client using basic client
	kubeConfig := getStringFromConfig(k8sConfig, "kube_config", "")
	basicClient, err := k8s.NewClient(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	c.k8sClient = basicClient

	return c, nil
}

// Name returns the collector name
func (c *Collector) Name() string {
	return c.config.Name
}

// Type returns the collector type
func (c *Collector) Type() string {
	return "k8s"
}

// Start begins collecting K8s events
func (c *Collector) Start(ctx context.Context) error {
	if !c.enabled.Load() {
		return fmt.Errorf("K8s collector is disabled")
	}

	if c.started.Load() {
		return fmt.Errorf("K8s collector already started")
	}

	c.logger.Info("Starting K8s collector")

	// Create cancellable context
	c.ctx, c.cancel = context.WithCancel(ctx)

	// K8s client is ready to use (no explicit start needed for basic client)

	// Mark as started
	c.started.Store(true)

	// Start event processing
	c.wg.Add(1)
	go c.processK8sEvents()

	c.logger.Info("K8s collector started successfully")
	return nil
}

// Stop gracefully stops the collector
func (c *Collector) Stop() error {
	if !c.started.Load() {
		return fmt.Errorf("K8s collector not started")
	}

	if c.stopped.Load() {
		return fmt.Errorf("K8s collector already stopped")
	}

	c.logger.Info("Stopping K8s collector")

	// Mark as stopping
	c.stopped.Store(true)

	// Cancel context
	if c.cancel != nil {
		c.cancel()
	}

	// Stop monitoring (no explicit stop needed for basic client)

	// Wait for goroutines
	c.wg.Wait()

	// Close event channel
	close(c.eventChan)

	c.logger.Info("K8s collector stopped",
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
	message := "K8s collector is healthy"

	if !c.started.Load() {
		status = unified.HealthStatusUnknown
		message = "Collector not started"
	} else if c.stopped.Load() {
		status = unified.HealthStatusUnhealthy
		message = "Collector stopped"
	} else if !c.isConnected() {
		status = unified.HealthStatusUnhealthy
		message = "Disconnected from Kubernetes API"
	} else if c.stats.errorCount.Load() > 50 {
		status = unified.HealthStatusDegraded
		message = fmt.Sprintf("High error count: %d", c.stats.errorCount.Load())
	}

	lastEvent := c.lastEventTime.Load().(time.Time)
	if time.Since(lastEvent) > 10*time.Minute && c.started.Load() {
		status = unified.HealthStatusDegraded
		message = "No events received in 10 minutes"
	}

	return &unified.Health{
		Status:          status,
		Message:         message,
		LastEventTime:   lastEvent,
		EventsProcessed: c.stats.eventsCollected.Load(),
		EventsDropped:   c.stats.eventsDropped.Load(),
		ErrorCount:      c.stats.errorCount.Load(),
		Metrics: map[string]interface{}{
			"api_connected":   c.isConnected(),
			"active_watches":  c.getActiveWatches(),
			"api_calls_total": c.getAPICalls(),
			"cache_hit_rate":  c.getCacheHitRate(),
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
			"events_per_second": float64(eventsCollected) / uptime.Seconds(),
			"uptime_seconds":    uptime.Seconds(),
			"api_connected":     c.isConnected(),
			"active_watches":    c.getActiveWatches(),
			"api_calls_total":   c.getAPICalls(),
			"api_errors_total":  c.getAPIErrors(),
			"cache_hit_rate":    c.getCacheHitRate(),
			"objects_cached":    c.getObjectsCached(),
		},
	}
}

// Configure updates the collector configuration
func (c *Collector) Configure(config unified.CollectorConfig) error {
	c.config = config
	c.enabled.Store(config.Enabled)

	c.logger.Info("Updated K8s collector configuration",
		"enabled", config.Enabled,
		"buffer_size", config.EventBufferSize,
	)

	return nil
}

// processK8sEvents processes events from the K8s client
func (c *Collector) processK8sEvents() {
	defer c.wg.Done()

	// TODO: Implement proper Kubernetes event watching
	// For now, just run a simple loop that checks if context is done
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-time.After(10 * time.Second):
			// Generate a mock event for testing
			mockEvent := c.generateMockEvent()
			if mockEvent != nil {
				select {
				case c.eventChan <- mockEvent:
					c.stats.eventsCollected.Add(1)
					c.lastEventTime.Store(time.Now())
				default:
					c.stats.eventsDropped.Add(1)
				}
			}
		}
	}
}

// convertK8sEvent converts a K8s event to a unified event
func (c *Collector) convertK8sEvent(k8sEvent corev1.Event) *unified.Event {
	// No nil check needed since k8sEvent is now passed by value

	// Determine category and severity based on K8s event
	category, severity := c.categorizeK8sEvent(&k8sEvent)

	// Create unified event
	event := &unified.Event{
		ID:        fmt.Sprintf("k8s_%s_%d", k8sEvent.ObjectMeta.UID, time.Now().UnixNano()),
		Timestamp: k8sEvent.LastTimestamp.Time,
		Type:      c.determineEventType(&k8sEvent),
		Category:  category,
		Severity:  severity,
		Source: unified.EventSource{
			Collector: c.config.Name,
			Component: "k8s",
			Node:      "kubernetes-api",
			Version:   "1.0.0",
		},
		Message:    c.generateMessage(&k8sEvent),
		Data:       c.extractEventData(&k8sEvent),
		Attributes: c.extractAttributes(&k8sEvent),
		Labels:     c.mergeLabels(c.config.Labels, k8sEvent.ObjectMeta.Labels),
		Context:    c.extractContext(&k8sEvent),
		Metadata: unified.EventMetadata{
			CollectedAt:  k8sEvent.LastTimestamp.Time,
			ProcessedAt:  time.Now(),
			ProcessingMS: time.Since(k8sEvent.LastTimestamp.Time).Milliseconds(),
			Tags:         c.config.Tags,
		},
	}

	// Add actionable recommendations if applicable
	if actionable := c.generateActionable(&k8sEvent); actionable != nil {
		event.Actionable = actionable
	}

	return event
}

// Helper methods

func (c *Collector) categorizeK8sEvent(event *corev1.Event) (unified.Category, unified.Severity) {
	// Use event.Reason to categorize events
	switch event.Reason {
	case "OOMKilling", "OutOfMemory":
		return unified.CategoryMemory, unified.SeverityCritical
	case "FailedMount", "FailedAttachVolume":
		return unified.CategoryStorage, unified.SeverityError
	case "FailedScheduling":
		return unified.CategoryKubernetes, unified.SeverityError
	case "Killing", "Failed":
		return unified.CategoryReliability, unified.SeverityError
	case "Created", "Started", "Scheduled":
		return unified.CategoryKubernetes, unified.SeverityInfo
	default:
		// Use event.Type to determine severity
		if event.Type == "Warning" {
			return unified.CategoryKubernetes, unified.SeverityWarning
		}
		return unified.CategoryKubernetes, unified.SeverityInfo
	}
}

func (c *Collector) determineEventType(event *corev1.Event) string {
	// Use event.Reason to determine type
	switch event.Reason {
	case "Created":
		return "pod_created"
	case "Started":
		return "pod_started"
	case "Killing", "Killed":
		return "pod_killed"
	case "OOMKilling":
		return "pod_oom_killed"
	case "Failed":
		return "pod_failed"
	case "FailedScheduling":
		return "pod_failed_scheduling"
	case "Scheduled":
		return "pod_scheduled"
	case "Pulling", "Pulled":
		return "image_pull"
	case "FailedMount":
		return "volume_mount_failed"
	default:
		return "kubernetes_event"
	}
}

func (c *Collector) generateMessage(event *corev1.Event) string {
	switch event.Reason {
	case "OOMKilling":
		return fmt.Sprintf("Pod %s was killed due to out-of-memory condition", event.InvolvedObject.Name)
	case "Failed":
		return fmt.Sprintf("Pod %s is in crash loop", event.InvolvedObject.Name)
	case "NodeNotReady":
		return fmt.Sprintf("Node %s is not ready", event.InvolvedObject.Name)
	case "Created":
		return fmt.Sprintf("Pod %s was created", event.InvolvedObject.Name)
	case "Killing":
		return fmt.Sprintf("Pod %s was deleted", event.InvolvedObject.Name)
	case "ScalingReplicaSet":
		return fmt.Sprintf("Deployment %s was scaled", event.InvolvedObject.Name)
	default:
		return fmt.Sprintf("Kubernetes event: %s", event.Message)
	}
}

func (c *Collector) extractEventData(event *corev1.Event) map[string]interface{} {
	data := make(map[string]interface{})

	data["namespace"] = event.Namespace
	data["kind"] = event.InvolvedObject.Kind
	data["name"] = event.InvolvedObject.Name
	data["action"] = event.Action
	data["reason"] = event.Reason
	data["message"] = event.Message
	data["uid"] = event.ObjectMeta.UID
	data["resource_version"] = event.ObjectMeta.ResourceVersion
	data["generation"] = event.ObjectMeta.Generation
	data["creation_timestamp"] = event.ObjectMeta.CreationTimestamp

	return data
}

func (c *Collector) extractAttributes(event *corev1.Event) map[string]interface{} {
	attributes := make(map[string]interface{})

	attributes["cluster_name"] = "default" // TODO: Get actual cluster name
	attributes["api_version"] = event.APIVersion
	attributes["event_source"] = event.Source

	return attributes
}

func (c *Collector) extractContext(event *corev1.Event) *unified.EventContext {
	context := &unified.EventContext{
		Namespace: event.Namespace,
		Labels:    event.Labels,
	}

	// Extract specific context based on object kind
	switch event.InvolvedObject.Kind {
	case "Pod":
		context.Pod = event.InvolvedObject.Name
		context.Namespace = event.InvolvedObject.Namespace
	case "Node":
		context.Node = event.InvolvedObject.Name
	}

	// Determine workload type from involved object
	if event.InvolvedObject.Kind != "" {
		// Store workload info in labels
		if context.Labels == nil {
			context.Labels = make(map[string]string)
		}
		context.Labels["workload_type"] = event.InvolvedObject.Kind
	}

	return context
}

func (c *Collector) generateActionable(event *corev1.Event) *unified.ActionableItem {
	switch event.Reason {
	case "OOMKilling":
		return &unified.ActionableItem{
			Title:           "Increase memory limit",
			Description:     "Pod was killed due to out-of-memory condition",
			Commands:        []string{fmt.Sprintf("kubectl patch deployment %s -n %s -p '{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"<container>\",\"resources\":{\"limits\":{\"memory\":\"512Mi\"}}}]}}}}'", event.InvolvedObject.Name, event.Namespace)},
			Risk:            unified.RiskLow,
			EstimatedImpact: "Prevents OOM kills, may increase memory usage",
			AutoFixable:     false,
			Documentation:   "https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/",
		}
	case "Failed":
		return &unified.ActionableItem{
			Title:           "Check application logs",
			Description:     "Pod is in crash loop, investigate logs for root cause",
			Commands:        []string{fmt.Sprintf("kubectl logs %s -n %s --previous", event.InvolvedObject.Name, event.Namespace)},
			Risk:            unified.RiskLow,
			EstimatedImpact: "Helps identify the cause of crashes",
			AutoFixable:     false,
			Documentation:   "https://kubernetes.io/docs/tasks/debug-application-cluster/debug-application/",
		}
	}

	return nil
}

func (c *Collector) mergeLabels(configLabels, eventLabels map[string]string) map[string]string {
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

func (c *Collector) getActiveWatches() int {
	// TODO: Implement client stats
	return 0
}

func (c *Collector) getAPICalls() int64 {
	// TODO: Implement client stats
	return 0
}

func (c *Collector) getAPIErrors() int64 {
	// TODO: Implement client stats
	return 0
}

func (c *Collector) getCacheHitRate() float64 {
	// TODO: Implement client stats
	return 0.0
}

func (c *Collector) getObjectsCached() int {
	// TODO: Implement client stats
	return 0
}

func (c *Collector) recordError(err error) {
	c.stats.errorCount.Add(1)
	c.lastError.Store(err.Error())
	c.lastErrorTime.Store(time.Now())
}

// K8sConfig contains K8s-specific configuration
type K8sConfig struct {
	InCluster    bool          `json:"in_cluster"`
	KubeConfig   string        `json:"kube_config"`
	Namespace    string        `json:"namespace"`
	WatchTimeout time.Duration `json:"watch_timeout"`
	RetryBackoff time.Duration `json:"retry_backoff"`
	MaxRetries   int           `json:"max_retries"`
}

func extractK8sConfig(extra map[string]interface{}) (*K8sConfig, error) {
	config := &K8sConfig{
		// Defaults
		InCluster:    true,
		KubeConfig:   "",
		Namespace:    "",
		WatchTimeout: 30 * time.Second,
		RetryBackoff: 5 * time.Second,
		MaxRetries:   3,
	}

	if extra != nil {
		if inCluster, ok := extra["in_cluster"].(bool); ok {
			config.InCluster = inCluster
		}
		if kubeConfig, ok := extra["kube_config"].(string); ok {
			config.KubeConfig = kubeConfig
		}
		if namespace, ok := extra["namespace"].(string); ok {
			config.Namespace = namespace
		}
		if watchTimeout, ok := extra["watch_timeout"].(string); ok {
			if duration, err := time.ParseDuration(watchTimeout); err == nil {
				config.WatchTimeout = duration
			}
		}
		if retryBackoff, ok := extra["retry_backoff"].(string); ok {
			if duration, err := time.ParseDuration(retryBackoff); err == nil {
				config.RetryBackoff = duration
			}
		}
		if maxRetries, ok := extra["max_retries"].(float64); ok {
			config.MaxRetries = int(maxRetries)
		}
	}

	return config, nil
}

// Helper functions for configuration extraction
func getBoolFromConfig(config *K8sConfig, key string, defaultValue bool) bool {
	switch key {
	case "in_cluster":
		return config.InCluster
	default:
		return defaultValue
	}
}

func getStringFromConfig(config *K8sConfig, key, defaultValue string) string {
	switch key {
	case "kube_config":
		return config.KubeConfig
	case "namespace":
		return config.Namespace
	default:
		return defaultValue
	}
}

func getIntFromConfig(config *K8sConfig, key string, defaultValue int) int {
	switch key {
	case "max_retries":
		return config.MaxRetries
	default:
		return defaultValue
	}
}

func getDurationFromConfig(config *K8sConfig, key string, defaultValue time.Duration) time.Duration {
	switch key {
	case "watch_timeout":
		return config.WatchTimeout
	case "retry_backoff":
		return config.RetryBackoff
	default:
		return defaultValue
	}
}

// isConnected checks if the Kubernetes client is connected
func (c *Collector) isConnected() bool {
	return c.k8sClient != nil && c.k8sClient.Clientset != nil
}

// generateMockEvent creates a mock event for testing
func (c *Collector) generateMockEvent() *unified.Event {
	return &unified.Event{
		ID:        fmt.Sprintf("k8s-mock-%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Type:      "mock",
		Category:  unified.CategorySystem,
		Severity:  unified.SeverityInfo,
		Source: unified.EventSource{
			Collector: c.Name(),
			Component: "mock",
			Node:      "localhost",
			Version:   "1.0.0",
		},
		Message: "Mock Kubernetes event for development",
		Data: map[string]interface{}{
			"mock": true,
		},
		Labels: map[string]string{
			"source": "k8s-collector",
			"mock":   "true",
		},
		Context: &unified.EventContext{
			Node: "localhost",
		},
		Metadata: unified.EventMetadata{
			CollectedAt:  time.Now(),
			ProcessedAt:  time.Now(),
			ProcessingMS: 0,
			Tags:         c.config.Tags,
		},
	}
}
