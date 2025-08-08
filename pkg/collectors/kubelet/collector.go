package kubelet

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	statsv1alpha1 "k8s.io/kubelet/pkg/apis/stats/v1alpha1"
)

// Config holds kubelet collector configuration
type Config struct {
	// Node name to collect from (defaults to current node)
	NodeName string

	// Kubelet address (defaults to localhost:10250)
	Address string

	// Use insecure connection (for testing)
	Insecure bool

	// Client certificate for authentication
	ClientCert string
	ClientKey  string

	// Collection intervals
	MetricsInterval time.Duration
	StatsInterval   time.Duration

	// Logger
	Logger *zap.Logger
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		Address:         "localhost:10250",
		MetricsInterval: 30 * time.Second,
		StatsInterval:   10 * time.Second,
		Insecure:        false,
	}
}

// Collector implements the kubelet metrics collector
type Collector struct {
	name    string
	config  *Config
	client  *http.Client
	events  chan collectors.RawEvent
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.RWMutex
	healthy bool
	logger  *zap.Logger

	// Metrics
	stats struct {
		eventsCollected int64
		errorsCount     int64
		lastEventTime   time.Time
	}
}

// NewCollector creates a new kubelet collector
func NewCollector(name string, config *Config) (*Collector, error) {
	if config == nil {
		config = DefaultConfig()
	}

	if config.Logger == nil {
		logger, err := zap.NewProduction()
		if err != nil {
			return nil, fmt.Errorf("failed to create logger: %w", err)
		}
		config.Logger = logger
	}

	// Create HTTP client with proper TLS config
	tlsConfig := &tls.Config{
		InsecureSkipVerify: config.Insecure,
	}

	if config.ClientCert != "" && config.ClientKey != "" {
		cert, err := tls.LoadX509KeyPair(config.ClientCert, config.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificates: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
		Timeout: 10 * time.Second,
	}

	return &Collector{
		name:    name,
		config:  config,
		client:  client,
		events:  make(chan collectors.RawEvent, 10000),
		healthy: true,
		logger:  config.Logger,
	}, nil
}

// Name returns the collector name
func (c *Collector) Name() string {
	return c.name
}

// Start begins collection
func (c *Collector) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Verify connectivity
	if err := c.checkConnectivity(); err != nil {
		return fmt.Errorf("kubelet connectivity check failed: %w", err)
	}

	// Start collection goroutines
	c.wg.Add(2)
	go c.collectStats()
	go c.collectPodMetrics()

	c.logger.Info("Kubelet collector started",
		zap.String("address", c.config.Address),
		zap.Duration("stats_interval", c.config.StatsInterval))

	return nil
}

// Stop gracefully shuts down the collector
func (c *Collector) Stop() error {
	c.cancel()
	c.wg.Wait()
	close(c.events)
	c.healthy = false
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan collectors.RawEvent {
	return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}

// checkConnectivity verifies we can reach kubelet
func (c *Collector) checkConnectivity() error {
	url := fmt.Sprintf("https://%s/healthz", c.config.Address)
	if c.config.Insecure {
		url = fmt.Sprintf("http://%s/healthz", c.config.Address)
	}

	resp, err := c.client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("kubelet health check failed: %s", resp.Status)
	}

	return nil
}

// collectStats collects kubelet stats summary
func (c *Collector) collectStats() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.StatsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if err := c.fetchStats(); err != nil {
				c.logger.Error("Failed to fetch kubelet stats", zap.Error(err))
				c.recordError()
			}
		}
	}
}

// fetchStats fetches and processes stats from kubelet
func (c *Collector) fetchStats() error {
	url := fmt.Sprintf("https://%s/stats/summary", c.config.Address)
	if c.config.Insecure {
		url = fmt.Sprintf("http://%s/stats/summary", c.config.Address)
	}

	resp, err := c.client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch stats: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("stats request failed: %s - failed to read response body: %w", resp.Status, readErr)
		}
		return fmt.Errorf("stats request failed: %s - %s", resp.Status, string(body))
	}

	var summary statsv1alpha1.Summary
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		return fmt.Errorf("failed to decode stats: %w", err)
	}

	// Process node stats
	if summary.Node.CPU != nil {
		c.sendNodeCPUEvent(&summary)
	}

	if summary.Node.Memory != nil {
		c.sendNodeMemoryEvent(&summary)
	}

	// Process pod stats
	for _, pod := range summary.Pods {
		c.processPodStats(&pod)
	}

	return nil
}

// sendNodeCPUEvent sends node CPU metrics
func (c *Collector) sendNodeCPUEvent(summary *statsv1alpha1.Summary) {
	metadata := map[string]string{
		"collector":       "kubelet",
		"event_type":      "node_cpu",
		"node_name":       summary.Node.NodeName,
		"k8s_node":        summary.Node.NodeName,
		"cpu_usage_nano":  fmt.Sprintf("%d", *summary.Node.CPU.UsageNanoCores),
		"cpu_usage_milli": fmt.Sprintf("%d", *summary.Node.CPU.UsageNanoCores/1000000),
	}

	data, err := json.Marshal(map[string]interface{}{
		"node_name":       summary.Node.NodeName,
		"cpu_usage_nano":  *summary.Node.CPU.UsageNanoCores,
		"cpu_usage_milli": *summary.Node.CPU.UsageNanoCores / 1000000,
		"timestamp":       summary.Node.CPU.Time.Time,
	})
	if err != nil {
		c.logger.Error("Failed to marshal node CPU event data", zap.Error(err))
		return
	}

	event := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "kubelet_node_cpu",
		Data:      data,
		Metadata:  metadata,
		TraceID:   collectors.GenerateTraceID(),
		SpanID:    collectors.GenerateSpanID(),
	}

	select {
	case c.events <- event:
		c.recordEvent()
	case <-c.ctx.Done():
	}
}

// sendNodeMemoryEvent sends node memory metrics
func (c *Collector) sendNodeMemoryEvent(summary *statsv1alpha1.Summary) {
	metadata := map[string]string{
		"collector":          "kubelet",
		"event_type":         "node_memory",
		"node_name":          summary.Node.NodeName,
		"k8s_node":           summary.Node.NodeName,
		"memory_usage_bytes": fmt.Sprintf("%d", *summary.Node.Memory.UsageBytes),
		"memory_available":   fmt.Sprintf("%d", *summary.Node.Memory.AvailableBytes),
		"memory_working_set": fmt.Sprintf("%d", *summary.Node.Memory.WorkingSetBytes),
	}

	data, err := json.Marshal(map[string]interface{}{
		"node_name":          summary.Node.NodeName,
		"memory_usage":       *summary.Node.Memory.UsageBytes,
		"memory_available":   *summary.Node.Memory.AvailableBytes,
		"memory_working_set": *summary.Node.Memory.WorkingSetBytes,
		"timestamp":          summary.Node.Memory.Time.Time,
	})
	if err != nil {
		c.logger.Error("Failed to marshal node memory event data", zap.Error(err))
		return
	}

	event := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "kubelet_node_memory",
		Data:      data,
		Metadata:  metadata,
		TraceID:   collectors.GenerateTraceID(),
		SpanID:    collectors.GenerateSpanID(),
	}

	select {
	case c.events <- event:
		c.recordEvent()
	case <-c.ctx.Done():
	}
}

// processPodStats processes stats for a single pod
func (c *Collector) processPodStats(pod *statsv1alpha1.PodStats) {
	// Check for CPU throttling
	for _, container := range pod.Containers {
		if container.CPU != nil && container.CPU.UsageNanoCores != nil {
			c.checkCPUThrottling(pod, &container)
		}

		if container.Memory != nil {
			c.checkMemoryPressure(pod, &container)
		}

		// Note: Restart count is not available in stats API
		// Would need to correlate with pod status from pods endpoint
	}

	// Check ephemeral storage
	if pod.EphemeralStorage != nil {
		c.checkEphemeralStorage(pod)
	}
}

// checkCPUThrottling detects CPU throttling
func (c *Collector) checkCPUThrottling(pod *statsv1alpha1.PodStats, container *statsv1alpha1.ContainerStats) {
	// Note: Real throttling metrics would come from cAdvisor metrics endpoint
	// This is a simplified version
	metadata := map[string]string{
		"collector":      "kubelet",
		"event_type":     "kubelet_cpu_throttling",
		"k8s_namespace":  pod.PodRef.Namespace,
		"k8s_name":       pod.PodRef.Name,
		"k8s_kind":       "Pod",
		"k8s_uid":        string(pod.PodRef.UID),
		"container_name": container.Name,
		"cpu_usage_nano": fmt.Sprintf("%d", *container.CPU.UsageNanoCores),
	}

	data, err := json.Marshal(map[string]interface{}{
		"namespace":      pod.PodRef.Namespace,
		"pod":            pod.PodRef.Name,
		"container":      container.Name,
		"cpu_usage_nano": *container.CPU.UsageNanoCores,
		"timestamp":      container.CPU.Time.Time,
	})
	if err != nil {
		c.logger.Error("Failed to marshal CPU throttling event data", zap.Error(err))
		return
	}

	event := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "kubelet_cpu_throttling",
		Data:      data,
		Metadata:  metadata,
		TraceID:   c.getOrGenerateTraceID(types.UID(pod.PodRef.UID)),
		SpanID:    collectors.GenerateSpanID(),
	}

	select {
	case c.events <- event:
		c.recordEvent()
	case <-c.ctx.Done():
	}
}

// checkMemoryPressure detects memory pressure
func (c *Collector) checkMemoryPressure(pod *statsv1alpha1.PodStats, container *statsv1alpha1.ContainerStats) {
	if container.Memory.WorkingSetBytes == nil || container.Memory.UsageBytes == nil {
		return
	}

	metadata := map[string]string{
		"collector":          "kubelet",
		"event_type":         "kubelet_memory_pressure",
		"k8s_namespace":      pod.PodRef.Namespace,
		"k8s_name":           pod.PodRef.Name,
		"k8s_kind":           "Pod",
		"k8s_uid":            string(pod.PodRef.UID),
		"container_name":     container.Name,
		"memory_usage":       fmt.Sprintf("%d", *container.Memory.UsageBytes),
		"memory_working_set": fmt.Sprintf("%d", *container.Memory.WorkingSetBytes),
	}

	// Check if RSS is available (indicates memory pressure)
	if container.Memory.RSSBytes != nil {
		metadata["memory_rss"] = fmt.Sprintf("%d", *container.Memory.RSSBytes)
	}

	data, err := json.Marshal(map[string]interface{}{
		"namespace":          pod.PodRef.Namespace,
		"pod":                pod.PodRef.Name,
		"container":          container.Name,
		"memory_usage":       *container.Memory.UsageBytes,
		"memory_working_set": *container.Memory.WorkingSetBytes,
		"timestamp":          container.Memory.Time.Time,
	})
	if err != nil {
		c.logger.Error("Failed to marshal memory pressure event data", zap.Error(err))
		return
	}

	event := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "kubelet_memory_pressure",
		Data:      data,
		Metadata:  metadata,
		TraceID:   c.getOrGenerateTraceID(types.UID(pod.PodRef.UID)),
		SpanID:    collectors.GenerateSpanID(),
	}

	select {
	case c.events <- event:
		c.recordEvent()
	case <-c.ctx.Done():
	}
}

// checkEphemeralStorage checks ephemeral storage usage
func (c *Collector) checkEphemeralStorage(pod *statsv1alpha1.PodStats) {
	if pod.EphemeralStorage.UsedBytes == nil || pod.EphemeralStorage.AvailableBytes == nil {
		return
	}

	usedBytes := *pod.EphemeralStorage.UsedBytes
	availableBytes := *pod.EphemeralStorage.AvailableBytes
	totalBytes := usedBytes + availableBytes

	// Calculate usage percentage
	usagePercent := float64(usedBytes) / float64(totalBytes) * 100

	// Only send event if usage is significant (>50%)
	if usagePercent > 50 {
		metadata := map[string]string{
			"collector":               "kubelet",
			"event_type":              "kubelet_ephemeral_storage",
			"k8s_namespace":           pod.PodRef.Namespace,
			"k8s_name":                pod.PodRef.Name,
			"k8s_kind":                "Pod",
			"k8s_uid":                 string(pod.PodRef.UID),
			"storage_used_bytes":      fmt.Sprintf("%d", usedBytes),
			"storage_available_bytes": fmt.Sprintf("%d", availableBytes),
			"storage_usage_percent":   fmt.Sprintf("%.2f", usagePercent),
		}

		data, err := json.Marshal(map[string]interface{}{
			"namespace":       pod.PodRef.Namespace,
			"pod":             pod.PodRef.Name,
			"used_bytes":      usedBytes,
			"available_bytes": availableBytes,
			"usage_percent":   usagePercent,
			"timestamp":       pod.EphemeralStorage.Time.Time,
		})
		if err != nil {
			c.logger.Error("Failed to marshal ephemeral storage event data", zap.Error(err))
			return
		}

		event := collectors.RawEvent{
			Timestamp: time.Now(),
			Type:      "kubelet_ephemeral_storage",
			Data:      data,
			Metadata:  metadata,
			TraceID:   c.getOrGenerateTraceID(types.UID(pod.PodRef.UID)),
			SpanID:    collectors.GenerateSpanID(),
		}

		select {
		case c.events <- event:
			c.recordEvent()
		case <-c.ctx.Done():
		}
	}
}

// collectPodMetrics collects pod lifecycle events
func (c *Collector) collectPodMetrics() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.MetricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if err := c.fetchPodLifecycle(); err != nil {
				c.logger.Error("Failed to fetch pod lifecycle", zap.Error(err))
				c.recordError()
			}
		}
	}
}

// fetchPodLifecycle fetches pod status from kubelet
func (c *Collector) fetchPodLifecycle() error {
	url := fmt.Sprintf("https://%s/pods", c.config.Address)
	if c.config.Insecure {
		url = fmt.Sprintf("http://%s/pods", c.config.Address)
	}

	resp, err := c.client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch pods: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("pods request failed: %s - failed to read response body: %w", resp.Status, readErr)
		}
		return fmt.Errorf("pods request failed: %s - %s", resp.Status, string(body))
	}

	var podList v1.PodList
	if err := json.NewDecoder(resp.Body).Decode(&podList); err != nil {
		return fmt.Errorf("failed to decode pods: %w", err)
	}

	// Process pod statuses
	for _, pod := range podList.Items {
		c.processPodStatus(&pod)
	}

	return nil
}

// processPodStatus checks for pod issues
func (c *Collector) processPodStatus(pod *v1.Pod) {
	// Check container statuses
	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Waiting != nil {
			c.sendContainerWaitingEvent(pod, &status)
		}

		if status.State.Terminated != nil && status.State.Terminated.ExitCode != 0 {
			c.sendContainerTerminatedEvent(pod, &status)
		}

		// Check last termination state for crash loops
		if status.LastTerminationState.Terminated != nil {
			c.sendCrashLoopEvent(pod, &status)
		}
	}

	// Check pod conditions
	for _, condition := range pod.Status.Conditions {
		if condition.Type == v1.PodReady && condition.Status != v1.ConditionTrue {
			c.sendPodNotReadyEvent(pod, &condition)
		}
	}
}

// sendContainerWaitingEvent sends events for waiting containers
func (c *Collector) sendContainerWaitingEvent(pod *v1.Pod, status *v1.ContainerStatus) {
	metadata := map[string]string{
		"collector":       "kubelet",
		"event_type":      "kubelet_container_waiting",
		"k8s_namespace":   pod.Namespace,
		"k8s_name":        pod.Name,
		"k8s_kind":        "Pod",
		"k8s_uid":         string(pod.UID),
		"container_name":  status.Name,
		"waiting_reason":  status.State.Waiting.Reason,
		"waiting_message": status.State.Waiting.Message,
	}

	data, err := json.Marshal(map[string]interface{}{
		"namespace": pod.Namespace,
		"pod":       pod.Name,
		"container": status.Name,
		"reason":    status.State.Waiting.Reason,
		"message":   status.State.Waiting.Message,
		"timestamp": time.Now(),
	})
	if err != nil {
		c.logger.Error("Failed to marshal container waiting event data", zap.Error(err))
		return
	}

	event := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "kubelet_container_waiting",
		Data:      data,
		Metadata:  metadata,
		TraceID:   c.getOrGenerateTraceID(pod.UID),
		SpanID:    collectors.GenerateSpanID(),
	}

	select {
	case c.events <- event:
		c.recordEvent()
	case <-c.ctx.Done():
	}
}

// sendContainerTerminatedEvent sends events for terminated containers
func (c *Collector) sendContainerTerminatedEvent(pod *v1.Pod, status *v1.ContainerStatus) {
	metadata := map[string]string{
		"collector":      "kubelet",
		"event_type":     "container_terminated",
		"k8s_namespace":  pod.Namespace,
		"k8s_name":       pod.Name,
		"k8s_kind":       "Pod",
		"k8s_uid":        string(pod.UID),
		"container_name": status.Name,
		"exit_code":      fmt.Sprintf("%d", status.State.Terminated.ExitCode),
		"exit_reason":    status.State.Terminated.Reason,
		"exit_message":   status.State.Terminated.Message,
	}

	data, err := json.Marshal(map[string]interface{}{
		"namespace": pod.Namespace,
		"pod":       pod.Name,
		"container": status.Name,
		"exit_code": status.State.Terminated.ExitCode,
		"reason":    status.State.Terminated.Reason,
		"message":   status.State.Terminated.Message,
		"timestamp": status.State.Terminated.FinishedAt.Time,
	})
	if err != nil {
		c.logger.Error("Failed to marshal container terminated event data", zap.Error(err))
		return
	}

	event := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "kubelet_container_terminated",
		Data:      data,
		Metadata:  metadata,
		TraceID:   c.getOrGenerateTraceID(pod.UID),
		SpanID:    collectors.GenerateSpanID(),
	}

	select {
	case c.events <- event:
		c.recordEvent()
	case <-c.ctx.Done():
	}
}

// sendCrashLoopEvent detects crash loop patterns
func (c *Collector) sendCrashLoopEvent(pod *v1.Pod, status *v1.ContainerStatus) {
	if status.RestartCount > 3 { // Likely in crash loop
		metadata := map[string]string{
			"collector":        "kubelet",
			"event_type":       "kubelet_crash_loop",
			"k8s_namespace":    pod.Namespace,
			"k8s_name":         pod.Name,
			"k8s_kind":         "Pod",
			"k8s_uid":          string(pod.UID),
			"container_name":   status.Name,
			"restart_count":    fmt.Sprintf("%d", status.RestartCount),
			"last_exit_code":   fmt.Sprintf("%d", status.LastTerminationState.Terminated.ExitCode),
			"last_exit_reason": status.LastTerminationState.Terminated.Reason,
		}

		data, err := json.Marshal(map[string]interface{}{
			"namespace":      pod.Namespace,
			"pod":            pod.Name,
			"container":      status.Name,
			"restart_count":  status.RestartCount,
			"last_exit_code": status.LastTerminationState.Terminated.ExitCode,
			"last_reason":    status.LastTerminationState.Terminated.Reason,
			"timestamp":      time.Now(),
		})
		if err != nil {
			c.logger.Error("Failed to marshal crash loop event data", zap.Error(err))
			return
		}

		event := collectors.RawEvent{
			Timestamp: time.Now(),
			Type:      "kubelet_crash_loop",
			Data:      data,
			Metadata:  metadata,
			TraceID:   c.getOrGenerateTraceID(pod.UID),
			SpanID:    collectors.GenerateSpanID(),
		}

		select {
		case c.events <- event:
			c.recordEvent()
		case <-c.ctx.Done():
		}
	}
}

// sendPodNotReadyEvent sends events for pods not ready
func (c *Collector) sendPodNotReadyEvent(pod *v1.Pod, condition *v1.PodCondition) {
	metadata := map[string]string{
		"collector":         "kubelet",
		"event_type":        "pod_not_ready",
		"k8s_namespace":     pod.Namespace,
		"k8s_name":          pod.Name,
		"k8s_kind":          "Pod",
		"k8s_uid":           string(pod.UID),
		"condition_type":    string(condition.Type),
		"condition_status":  string(condition.Status),
		"condition_reason":  condition.Reason,
		"condition_message": condition.Message,
	}

	data, err := json.Marshal(map[string]interface{}{
		"namespace": pod.Namespace,
		"pod":       pod.Name,
		"condition": string(condition.Type),
		"status":    string(condition.Status),
		"reason":    condition.Reason,
		"message":   condition.Message,
		"timestamp": condition.LastTransitionTime.Time,
	})
	if err != nil {
		c.logger.Error("Failed to marshal pod not ready event data", zap.Error(err))
		return
	}

	event := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "kubelet_pod_not_ready",
		Data:      data,
		Metadata:  metadata,
		TraceID:   c.getOrGenerateTraceID(pod.UID),
		SpanID:    collectors.GenerateSpanID(),
	}

	select {
	case c.events <- event:
		c.recordEvent()
	case <-c.ctx.Done():
	}
}

// Helper methods

// podTraceMap maintains trace IDs per pod
var podTraceMap = make(map[types.UID]string)
var podTraceMu sync.RWMutex

func (c *Collector) getOrGenerateTraceID(podUID types.UID) string {
	podTraceMu.RLock()
	if traceID, exists := podTraceMap[podUID]; exists {
		podTraceMu.RUnlock()
		return traceID
	}
	podTraceMu.RUnlock()

	// Generate new trace ID
	podTraceMu.Lock()
	traceID := collectors.GenerateTraceID()
	podTraceMap[podUID] = traceID
	podTraceMu.Unlock()

	return traceID
}

func (c *Collector) recordEvent() {
	c.mu.Lock()
	c.stats.eventsCollected++
	c.stats.lastEventTime = time.Now()
	c.mu.Unlock()
}

func (c *Collector) recordError() {
	c.mu.Lock()
	c.stats.errorsCount++
	c.mu.Unlock()
}

// Health returns detailed health information
func (c *Collector) Health() (bool, map[string]interface{}) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	health := map[string]interface{}{
		"healthy":          c.healthy,
		"events_collected": c.stats.eventsCollected,
		"errors_count":     c.stats.errorsCount,
		"last_event":       c.stats.lastEventTime,
		"kubelet_address":  c.config.Address,
	}

	return c.healthy, health
}

// Statistics returns collector statistics
func (c *Collector) Statistics() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return map[string]interface{}{
		"events_collected": c.stats.eventsCollected,
		"errors_count":     c.stats.errorsCount,
		"last_event_time":  c.stats.lastEventTime,
		"pod_trace_count":  len(podTraceMap),
	}
}
