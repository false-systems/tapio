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
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
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

// Event data structures

// NodeCPUEventData holds node CPU event data
type NodeCPUEventData struct {
	NodeName      string    `json:"node_name"`
	CPUUsageNano  uint64    `json:"cpu_usage_nano"`
	CPUUsageMilli uint64    `json:"cpu_usage_milli"`
	Timestamp     time.Time `json:"timestamp"`
}

// NodeMemoryEventData holds node memory event data
type NodeMemoryEventData struct {
	NodeName         string    `json:"node_name"`
	MemoryUsage      uint64    `json:"memory_usage"`
	MemoryAvailable  uint64    `json:"memory_available"`
	MemoryWorkingSet uint64    `json:"memory_working_set"`
	Timestamp        time.Time `json:"timestamp"`
}

// CPUThrottlingEventData holds CPU throttling event data
type CPUThrottlingEventData struct {
	Namespace    string    `json:"namespace"`
	Pod          string    `json:"pod"`
	Container    string    `json:"container"`
	CPUUsageNano uint64    `json:"cpu_usage_nano"`
	Timestamp    time.Time `json:"timestamp"`
}

// MemoryPressureEventData holds memory pressure event data
type MemoryPressureEventData struct {
	Namespace        string    `json:"namespace"`
	Pod              string    `json:"pod"`
	Container        string    `json:"container"`
	MemoryUsage      uint64    `json:"memory_usage"`
	MemoryWorkingSet uint64    `json:"memory_working_set"`
	Timestamp        time.Time `json:"timestamp"`
}

// EphemeralStorageEventData holds ephemeral storage event data
type EphemeralStorageEventData struct {
	Namespace      string    `json:"namespace"`
	Pod            string    `json:"pod"`
	UsedBytes      uint64    `json:"used_bytes"`
	AvailableBytes uint64    `json:"available_bytes"`
	UsagePercent   float64   `json:"usage_percent"`
	Timestamp      time.Time `json:"timestamp"`
}

// ContainerWaitingEventData holds container waiting event data
type ContainerWaitingEventData struct {
	Namespace string    `json:"namespace"`
	Pod       string    `json:"pod"`
	Container string    `json:"container"`
	Reason    string    `json:"reason"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// ContainerTerminatedEventData holds container terminated event data
type ContainerTerminatedEventData struct {
	Namespace string    `json:"namespace"`
	Pod       string    `json:"pod"`
	Container string    `json:"container"`
	ExitCode  int32     `json:"exit_code"`
	Reason    string    `json:"reason"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// CrashLoopEventData holds crash loop event data
type CrashLoopEventData struct {
	Namespace    string    `json:"namespace"`
	Pod          string    `json:"pod"`
	Container    string    `json:"container"`
	RestartCount int32     `json:"restart_count"`
	LastExitCode int32     `json:"last_exit_code"`
	LastReason   string    `json:"last_reason"`
	Timestamp    time.Time `json:"timestamp"`
}

// PodNotReadyEventData holds pod not ready event data
type PodNotReadyEventData struct {
	Namespace string    `json:"namespace"`
	Pod       string    `json:"pod"`
	Condition string    `json:"condition"`
	Status    string    `json:"status"`
	Reason    string    `json:"reason"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
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

// HealthStatus represents kubelet collector health status
type HealthStatus struct {
	Healthy         bool      `json:"healthy"`
	EventsCollected int64     `json:"events_collected"`
	ErrorsCount     int64     `json:"errors_count"`
	LastEventTime   time.Time `json:"last_event_time"`
	KubeletAddress  string    `json:"kubelet_address"`
}

// Statistics represents kubelet collector statistics
type Statistics struct {
	EventsCollected int64     `json:"events_collected"`
	ErrorsCount     int64     `json:"errors_count"`
	LastEventTime   time.Time `json:"last_event_time"`
	PodTraceCount   int       `json:"pod_trace_count"`
}

// Collector implements the kubelet metrics collector
type Collector struct {
	name            string
	config          *Config
	client          *http.Client
	events          chan collectors.RawEvent
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	mu              sync.RWMutex
	healthy         bool
	logger          *zap.Logger
	podTraceManager *PodTraceManager

	// Metrics
	stats struct {
		eventsCollected int64
		errorsCount     int64
		lastEventTime   time.Time
	}

	// OTEL instrumentation - REQUIRED fields
	tracer          trace.Tracer
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
	apiLatency      metric.Float64Histogram
	pollsActive     metric.Int64UpDownCounter
	apiFailures     metric.Int64Counter
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

	// Initialize OTEL components - MANDATORY pattern
	tracer := otel.Tracer(name)
	meter := otel.Meter(name)

	// Create metrics with descriptive names and descriptions
	eventsProcessed, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription(fmt.Sprintf("Total events processed by %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create events counter", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription(fmt.Sprintf("Total errors in %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	processingTime, err := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_duration_ms", name),
		metric.WithDescription(fmt.Sprintf("Processing duration for %s in milliseconds", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	apiLatency, err := meter.Float64Histogram(
		fmt.Sprintf("%s_api_latency_ms", name),
		metric.WithDescription(fmt.Sprintf("API call latency for %s in milliseconds", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create API latency histogram", zap.Error(err))
	}

	pollsActive, err := meter.Int64UpDownCounter(
		fmt.Sprintf("%s_active_polls", name),
		metric.WithDescription(fmt.Sprintf("Active polling operations in %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create polls active gauge", zap.Error(err))
	}

	apiFailures, err := meter.Int64Counter(
		fmt.Sprintf("%s_api_failures_total", name),
		metric.WithDescription(fmt.Sprintf("API failures in %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create API failures counter", zap.Error(err))
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
		name:            name,
		config:          config,
		client:          client,
		events:          make(chan collectors.RawEvent, 10000),
		healthy:         true,
		logger:          config.Logger,
		podTraceManager: NewPodTraceManager(),
		tracer:          tracer,
		eventsProcessed: eventsProcessed,
		errorsTotal:     errorsTotal,
		processingTime:  processingTime,
		apiLatency:      apiLatency,
		pollsActive:     pollsActive,
		apiFailures:     apiFailures,
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
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
	if c.podTraceManager != nil {
		c.podTraceManager.Stop()
	}
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
	start := time.Now()
	ctx, span := c.tracer.Start(c.ctx, "kubelet.fetch_stats")
	defer span.End()

	// Track active poll
	if c.pollsActive != nil {
		c.pollsActive.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", "fetch_stats"),
		))
		defer c.pollsActive.Add(ctx, -1, metric.WithAttributes(
			attribute.String("operation", "fetch_stats"),
		))
	}

	url := fmt.Sprintf("https://%s/stats/summary", c.config.Address)
	if c.config.Insecure {
		url = fmt.Sprintf("http://%s/stats/summary", c.config.Address)
	}

	span.SetAttributes(
		attribute.String("kubelet.endpoint", "/stats/summary"),
		attribute.String("kubelet.url", url),
	)

	resp, err := c.client.Get(url)
	if err != nil {
		// Record API failure
		if c.apiFailures != nil {
			c.apiFailures.Add(ctx, 1, metric.WithAttributes(
				attribute.String("endpoint", "/stats/summary"),
				attribute.String("error", "request_failed"),
			))
		}
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "api_request"),
			))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to fetch stats")
		return fmt.Errorf("failed to fetch stats: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Record API failure
		if c.apiFailures != nil {
			c.apiFailures.Add(ctx, 1, metric.WithAttributes(
				attribute.String("endpoint", "/stats/summary"),
				attribute.String("error", "http_status"),
				attribute.Int("status_code", resp.StatusCode),
			))
		}
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "api_status"),
			))
		}

		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			err := fmt.Errorf("stats request failed: %s - failed to read response body: %w", resp.Status, readErr)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		err := fmt.Errorf("stats request failed: %s - %s", resp.Status, string(body))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	var summary statsv1alpha1.Summary
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "decode"),
			))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to decode stats")
		return fmt.Errorf("failed to decode stats: %w", err)
	}

	// Record API latency
	duration := time.Since(start)
	if c.apiLatency != nil {
		c.apiLatency.Record(ctx, duration.Seconds()*1000, metric.WithAttributes(
			attribute.String("endpoint", "/stats/summary"),
		))
	}

	span.SetAttributes(
		attribute.Float64("duration_seconds", duration.Seconds()),
		attribute.Int("node_stats_count", 1),
		attribute.Int("pod_stats_count", len(summary.Pods)),
	)

	// Process node stats
	if summary.Node.CPU != nil {
		c.sendNodeCPUEvent(ctx, &summary)
	}

	if summary.Node.Memory != nil {
		c.sendNodeMemoryEvent(ctx, &summary)
	}

	// Process pod stats
	for _, pod := range summary.Pods {
		c.processPodStats(ctx, &pod)
	}

	return nil
}

// sendNodeCPUEvent sends node CPU metrics
func (c *Collector) sendNodeCPUEvent(ctx context.Context, summary *statsv1alpha1.Summary) {
	eventData := NodeCPUEventData{
		NodeName:      summary.Node.NodeName,
		CPUUsageNano:  *summary.Node.CPU.UsageNanoCores,
		CPUUsageMilli: *summary.Node.CPU.UsageNanoCores / 1000000,
		Timestamp:     summary.Node.CPU.Time.Time,
	}

	data, err := json.Marshal(eventData)
	if err != nil {
		c.logger.Error("Failed to marshal node CPU event data", zap.Error(err))
		c.recordError()
		return
	}

	metadata := map[string]string{
		"collector":       "kubelet",
		"event_type":      "node_cpu",
		"node_name":       summary.Node.NodeName,
		"k8s_node":        summary.Node.NodeName,
		"cpu_usage_nano":  fmt.Sprintf("%d", eventData.CPUUsageNano),
		"cpu_usage_milli": fmt.Sprintf("%d", eventData.CPUUsageMilli),
	}

	// Extract trace context from current span if available
	traceID, spanID := c.extractTraceContext(ctx)

	event := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "kubelet_node_cpu",
		Data:      data,
		Metadata:  metadata,
		TraceID:   traceID,
		SpanID:    spanID,
	}

	select {
	case c.events <- event:
		c.recordEvent()
		// Record OTEL event metric
		if c.eventsProcessed != nil {
			c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", "kubelet_node_cpu"),
				attribute.String("node_name", summary.Node.NodeName),
			))
		}
	case <-c.ctx.Done():
	}
}

// sendNodeMemoryEvent sends node memory metrics
func (c *Collector) sendNodeMemoryEvent(ctx context.Context, summary *statsv1alpha1.Summary) {
	eventData := NodeMemoryEventData{
		NodeName:         summary.Node.NodeName,
		MemoryUsage:      *summary.Node.Memory.UsageBytes,
		MemoryAvailable:  *summary.Node.Memory.AvailableBytes,
		MemoryWorkingSet: *summary.Node.Memory.WorkingSetBytes,
		Timestamp:        summary.Node.Memory.Time.Time,
	}

	data, err := json.Marshal(eventData)
	if err != nil {
		c.logger.Error("Failed to marshal node memory event data", zap.Error(err))
		c.recordError()
		return
	}

	metadata := map[string]string{
		"collector":          "kubelet",
		"event_type":         "node_memory",
		"node_name":          summary.Node.NodeName,
		"k8s_node":           summary.Node.NodeName,
		"memory_usage_bytes": fmt.Sprintf("%d", eventData.MemoryUsage),
		"memory_available":   fmt.Sprintf("%d", eventData.MemoryAvailable),
		"memory_working_set": fmt.Sprintf("%d", eventData.MemoryWorkingSet),
	}

	// Extract trace context from current span if available
	traceID, spanID := c.extractTraceContext(ctx)

	event := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "kubelet_node_memory",
		Data:      data,
		Metadata:  metadata,
		TraceID:   traceID,
		SpanID:    spanID,
	}

	select {
	case c.events <- event:
		c.recordEvent()
		// Record OTEL event metric
		if c.eventsProcessed != nil {
			c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", "kubelet_node_memory"),
				attribute.String("node_name", summary.Node.NodeName),
			))
		}
	case <-c.ctx.Done():
	}
}

// processPodStats processes stats for a single pod
func (c *Collector) processPodStats(ctx context.Context, pod *statsv1alpha1.PodStats) {
	// Check for CPU throttling
	for _, container := range pod.Containers {
		if container.CPU != nil && container.CPU.UsageNanoCores != nil {
			c.checkCPUThrottling(ctx, pod, &container)
		}

		if container.Memory != nil {
			c.checkMemoryPressure(ctx, pod, &container)
		}

		// Note: Restart count is not available in stats API
		// Would need to correlate with pod status from pods endpoint
	}

	// Check ephemeral storage
	if pod.EphemeralStorage != nil {
		c.checkEphemeralStorage(ctx, pod)
	}
}

// checkCPUThrottling detects CPU throttling
func (c *Collector) checkCPUThrottling(ctx context.Context, pod *statsv1alpha1.PodStats, container *statsv1alpha1.ContainerStats) {
	// Note: Real throttling metrics would come from cAdvisor metrics endpoint
	// This is a simplified version
	eventData := CPUThrottlingEventData{
		Namespace:    pod.PodRef.Namespace,
		Pod:          pod.PodRef.Name,
		Container:    container.Name,
		CPUUsageNano: *container.CPU.UsageNanoCores,
		Timestamp:    container.CPU.Time.Time,
	}

	data, err := json.Marshal(eventData)
	if err != nil {
		c.logger.Error("Failed to marshal CPU throttling event data", zap.Error(err))
		c.recordError()
		return
	}

	metadata := map[string]string{
		"collector":      "kubelet",
		"event_type":     "kubelet_cpu_throttling",
		"k8s_namespace":  pod.PodRef.Namespace,
		"k8s_name":       pod.PodRef.Name,
		"k8s_kind":       "Pod",
		"k8s_uid":        string(pod.PodRef.UID),
		"container_name": container.Name,
		"cpu_usage_nano": fmt.Sprintf("%d", eventData.CPUUsageNano),
	}

	// Extract trace context from current span if available
	traceID, spanID := c.extractTraceContext(ctx)

	event := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "kubelet_cpu_throttling",
		Data:      data,
		Metadata:  metadata,
		TraceID:   traceID,
		SpanID:    spanID,
	}

	select {
	case c.events <- event:
		c.recordEvent()
		// Record OTEL event metric
		if c.eventsProcessed != nil {
			c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", "kubelet_cpu_throttling"),
				attribute.String("namespace", pod.PodRef.Namespace),
				attribute.String("pod", pod.PodRef.Name),
				attribute.String("container", container.Name),
			))
		}
	case <-c.ctx.Done():
	}
}

// checkMemoryPressure detects memory pressure
func (c *Collector) checkMemoryPressure(ctx context.Context, pod *statsv1alpha1.PodStats, container *statsv1alpha1.ContainerStats) {
	if container.Memory.WorkingSetBytes == nil || container.Memory.UsageBytes == nil {
		return
	}

	eventData := MemoryPressureEventData{
		Namespace:        pod.PodRef.Namespace,
		Pod:              pod.PodRef.Name,
		Container:        container.Name,
		MemoryUsage:      *container.Memory.UsageBytes,
		MemoryWorkingSet: *container.Memory.WorkingSetBytes,
		Timestamp:        container.Memory.Time.Time,
	}

	data, err := json.Marshal(eventData)
	if err != nil {
		c.logger.Error("Failed to marshal memory pressure event data", zap.Error(err))
		c.recordError()
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
		"memory_usage":       fmt.Sprintf("%d", eventData.MemoryUsage),
		"memory_working_set": fmt.Sprintf("%d", eventData.MemoryWorkingSet),
	}

	// Check if RSS is available (indicates memory pressure)
	if container.Memory.RSSBytes != nil {
		metadata["memory_rss"] = fmt.Sprintf("%d", *container.Memory.RSSBytes)
	}

	// Extract trace context from current span if available
	traceID, spanID := c.extractTraceContext(ctx)

	event := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "kubelet_memory_pressure",
		Data:      data,
		Metadata:  metadata,
		TraceID:   traceID,
		SpanID:    spanID,
	}

	select {
	case c.events <- event:
		c.recordEvent()
		// Record OTEL event metric
		if c.eventsProcessed != nil {
			c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", "kubelet_memory_pressure"),
				attribute.String("namespace", pod.PodRef.Namespace),
				attribute.String("pod", pod.PodRef.Name),
				attribute.String("container", container.Name),
			))
		}
	case <-c.ctx.Done():
	}
}

// checkEphemeralStorage checks ephemeral storage usage
func (c *Collector) checkEphemeralStorage(ctx context.Context, pod *statsv1alpha1.PodStats) {
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
		eventData := EphemeralStorageEventData{
			Namespace:      pod.PodRef.Namespace,
			Pod:            pod.PodRef.Name,
			UsedBytes:      usedBytes,
			AvailableBytes: availableBytes,
			UsagePercent:   usagePercent,
			Timestamp:      pod.EphemeralStorage.Time.Time,
		}

		data, err := json.Marshal(eventData)
		if err != nil {
			c.logger.Error("Failed to marshal ephemeral storage event data", zap.Error(err))
			c.recordError()
			return
		}

		metadata := map[string]string{
			"collector":               "kubelet",
			"event_type":              "kubelet_ephemeral_storage",
			"k8s_namespace":           pod.PodRef.Namespace,
			"k8s_name":                pod.PodRef.Name,
			"k8s_kind":                "Pod",
			"k8s_uid":                 string(pod.PodRef.UID),
			"storage_used_bytes":      fmt.Sprintf("%d", eventData.UsedBytes),
			"storage_available_bytes": fmt.Sprintf("%d", eventData.AvailableBytes),
			"storage_usage_percent":   fmt.Sprintf("%.2f", eventData.UsagePercent),
		}

		// Extract trace context from current span if available
		traceID, spanID := c.extractTraceContext(ctx)

		event := collectors.RawEvent{
			Timestamp: time.Now(),
			Type:      "kubelet_ephemeral_storage",
			Data:      data,
			Metadata:  metadata,
			TraceID:   traceID,
			SpanID:    spanID,
		}

		select {
		case c.events <- event:
			c.recordEvent()
			// Record OTEL event metric
			if c.eventsProcessed != nil {
				c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
					attribute.String("event_type", "kubelet_ephemeral_storage"),
					attribute.String("namespace", pod.PodRef.Namespace),
					attribute.String("pod", pod.PodRef.Name),
				))
			}
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
	start := time.Now()
	ctx, span := c.tracer.Start(c.ctx, "kubelet.fetch_pod_lifecycle")
	defer span.End()

	// Track active poll
	if c.pollsActive != nil {
		c.pollsActive.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", "fetch_pod_lifecycle"),
		))
		defer c.pollsActive.Add(ctx, -1, metric.WithAttributes(
			attribute.String("operation", "fetch_pod_lifecycle"),
		))
	}

	url := fmt.Sprintf("https://%s/pods", c.config.Address)
	if c.config.Insecure {
		url = fmt.Sprintf("http://%s/pods", c.config.Address)
	}

	span.SetAttributes(
		attribute.String("kubelet.endpoint", "/pods"),
		attribute.String("kubelet.url", url),
	)

	resp, err := c.client.Get(url)
	if err != nil {
		// Record API failure
		if c.apiFailures != nil {
			c.apiFailures.Add(ctx, 1, metric.WithAttributes(
				attribute.String("endpoint", "/pods"),
				attribute.String("error", "request_failed"),
			))
		}
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "api_request"),
			))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to fetch pods")
		return fmt.Errorf("failed to fetch pods: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Record API failure
		if c.apiFailures != nil {
			c.apiFailures.Add(ctx, 1, metric.WithAttributes(
				attribute.String("endpoint", "/pods"),
				attribute.String("error", "http_status"),
				attribute.Int("status_code", resp.StatusCode),
			))
		}
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "api_status"),
			))
		}

		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			err := fmt.Errorf("pods request failed: %s - failed to read response body: %w", resp.Status, readErr)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		err := fmt.Errorf("pods request failed: %s - %s", resp.Status, string(body))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	var podList v1.PodList
	if err := json.NewDecoder(resp.Body).Decode(&podList); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "decode"),
			))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to decode pods")
		return fmt.Errorf("failed to decode pods: %w", err)
	}

	// Record API latency
	duration := time.Since(start)
	if c.apiLatency != nil {
		c.apiLatency.Record(ctx, duration.Seconds()*1000, metric.WithAttributes(
			attribute.String("endpoint", "/pods"),
		))
	}

	span.SetAttributes(
		attribute.Float64("duration_seconds", duration.Seconds()),
		attribute.Int("pods_count", len(podList.Items)),
	)

	// Process pod statuses
	for _, pod := range podList.Items {
		c.processPodStatus(ctx, &pod)
	}

	return nil
}

// processPodStatus checks for pod issues
func (c *Collector) processPodStatus(ctx context.Context, pod *v1.Pod) {
	// Check container statuses
	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Waiting != nil {
			c.sendContainerWaitingEvent(ctx, pod, &status)
		}

		if status.State.Terminated != nil && status.State.Terminated.ExitCode != 0 {
			c.sendContainerTerminatedEvent(ctx, pod, &status)
		}

		// Check last termination state for crash loops
		if status.LastTerminationState.Terminated != nil {
			c.sendCrashLoopEvent(ctx, pod, &status)
		}
	}

	// Check pod conditions
	for _, condition := range pod.Status.Conditions {
		if condition.Type == v1.PodReady && condition.Status != v1.ConditionTrue {
			c.sendPodNotReadyEvent(ctx, pod, &condition)
		}
	}
}

// sendContainerWaitingEvent sends events for waiting containers
func (c *Collector) sendContainerWaitingEvent(ctx context.Context, pod *v1.Pod, status *v1.ContainerStatus) {
	eventData := ContainerWaitingEventData{
		Namespace: pod.Namespace,
		Pod:       pod.Name,
		Container: status.Name,
		Reason:    status.State.Waiting.Reason,
		Message:   status.State.Waiting.Message,
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(eventData)
	if err != nil {
		c.logger.Error("Failed to marshal container waiting event data", zap.Error(err))
		c.recordError()
		return
	}

	metadata := map[string]string{
		"collector":       "kubelet",
		"event_type":      "kubelet_container_waiting",
		"k8s_namespace":   pod.Namespace,
		"k8s_name":        pod.Name,
		"k8s_kind":        "Pod",
		"k8s_uid":         string(pod.UID),
		"container_name":  status.Name,
		"waiting_reason":  eventData.Reason,
		"waiting_message": eventData.Message,
	}

	// Extract trace context from current span if available
	traceID, spanID := c.extractTraceContext(ctx)

	event := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "kubelet_container_waiting",
		Data:      data,
		Metadata:  metadata,
		TraceID:   traceID,
		SpanID:    spanID,
	}

	select {
	case c.events <- event:
		c.recordEvent()
		// Record OTEL event metric
		if c.eventsProcessed != nil {
			c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", "kubelet_container_waiting"),
				attribute.String("namespace", pod.Namespace),
				attribute.String("pod", pod.Name),
				attribute.String("container", status.Name),
				attribute.String("waiting_reason", eventData.Reason),
			))
		}
	case <-c.ctx.Done():
	}
}

// sendContainerTerminatedEvent sends events for terminated containers
func (c *Collector) sendContainerTerminatedEvent(ctx context.Context, pod *v1.Pod, status *v1.ContainerStatus) {
	eventData := ContainerTerminatedEventData{
		Namespace: pod.Namespace,
		Pod:       pod.Name,
		Container: status.Name,
		ExitCode:  status.State.Terminated.ExitCode,
		Reason:    status.State.Terminated.Reason,
		Message:   status.State.Terminated.Message,
		Timestamp: status.State.Terminated.FinishedAt.Time,
	}

	data, err := json.Marshal(eventData)
	if err != nil {
		c.logger.Error("Failed to marshal container terminated event data", zap.Error(err))
		c.recordError()
		return
	}

	metadata := map[string]string{
		"collector":      "kubelet",
		"event_type":     "container_terminated",
		"k8s_namespace":  pod.Namespace,
		"k8s_name":       pod.Name,
		"k8s_kind":       "Pod",
		"k8s_uid":        string(pod.UID),
		"container_name": status.Name,
		"exit_code":      fmt.Sprintf("%d", eventData.ExitCode),
		"exit_reason":    eventData.Reason,
		"exit_message":   eventData.Message,
	}

	// Extract trace context from current span if available
	traceID, spanID := c.extractTraceContext(ctx)

	event := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "kubelet_container_terminated",
		Data:      data,
		Metadata:  metadata,
		TraceID:   traceID,
		SpanID:    spanID,
	}

	select {
	case c.events <- event:
		c.recordEvent()
		// Record OTEL event metric
		if c.eventsProcessed != nil {
			c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", "kubelet_container_terminated"),
				attribute.String("namespace", pod.Namespace),
				attribute.String("pod", pod.Name),
				attribute.String("container", status.Name),
				attribute.Int("exit_code", int(eventData.ExitCode)),
			))
		}
	case <-c.ctx.Done():
	}
}

// sendCrashLoopEvent detects crash loop patterns
func (c *Collector) sendCrashLoopEvent(ctx context.Context, pod *v1.Pod, status *v1.ContainerStatus) {
	if status.RestartCount > 3 { // Likely in crash loop
		eventData := CrashLoopEventData{
			Namespace:    pod.Namespace,
			Pod:          pod.Name,
			Container:    status.Name,
			RestartCount: status.RestartCount,
			LastExitCode: status.LastTerminationState.Terminated.ExitCode,
			LastReason:   status.LastTerminationState.Terminated.Reason,
			Timestamp:    time.Now(),
		}

		data, err := json.Marshal(eventData)
		if err != nil {
			c.logger.Error("Failed to marshal crash loop event data", zap.Error(err))
			c.recordError()
			return
		}

		metadata := map[string]string{
			"collector":        "kubelet",
			"event_type":       "kubelet_crash_loop",
			"k8s_namespace":    pod.Namespace,
			"k8s_name":         pod.Name,
			"k8s_kind":         "Pod",
			"k8s_uid":          string(pod.UID),
			"container_name":   status.Name,
			"restart_count":    fmt.Sprintf("%d", eventData.RestartCount),
			"last_exit_code":   fmt.Sprintf("%d", eventData.LastExitCode),
			"last_exit_reason": eventData.LastReason,
		}

		// Extract trace context from current span if available
		traceID, spanID := c.extractTraceContext(ctx)

		event := collectors.RawEvent{
			Timestamp: time.Now(),
			Type:      "kubelet_crash_loop",
			Data:      data,
			Metadata:  metadata,
			TraceID:   traceID,
			SpanID:    spanID,
		}

		select {
		case c.events <- event:
			c.recordEvent()
			// Record OTEL event metric
			if c.eventsProcessed != nil {
				c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
					attribute.String("event_type", "kubelet_crash_loop"),
					attribute.String("namespace", pod.Namespace),
					attribute.String("pod", pod.Name),
					attribute.String("container", status.Name),
					attribute.Int("restart_count", int(eventData.RestartCount)),
				))
			}
		case <-c.ctx.Done():
		}
	}
}

// sendPodNotReadyEvent sends events for pods not ready
func (c *Collector) sendPodNotReadyEvent(ctx context.Context, pod *v1.Pod, condition *v1.PodCondition) {
	eventData := PodNotReadyEventData{
		Namespace: pod.Namespace,
		Pod:       pod.Name,
		Condition: string(condition.Type),
		Status:    string(condition.Status),
		Reason:    condition.Reason,
		Message:   condition.Message,
		Timestamp: condition.LastTransitionTime.Time,
	}

	data, err := json.Marshal(eventData)
	if err != nil {
		c.logger.Error("Failed to marshal pod not ready event data", zap.Error(err))
		c.recordError()
		return
	}

	metadata := map[string]string{
		"collector":         "kubelet",
		"event_type":        "pod_not_ready",
		"k8s_namespace":     pod.Namespace,
		"k8s_name":          pod.Name,
		"k8s_kind":          "Pod",
		"k8s_uid":           string(pod.UID),
		"condition_type":    eventData.Condition,
		"condition_status":  eventData.Status,
		"condition_reason":  eventData.Reason,
		"condition_message": eventData.Message,
	}

	// Extract trace context from current span if available
	traceID, spanID := c.extractTraceContext(ctx)

	event := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "kubelet_pod_not_ready",
		Data:      data,
		Metadata:  metadata,
		TraceID:   traceID,
		SpanID:    spanID,
	}

	select {
	case c.events <- event:
		c.recordEvent()
		// Record OTEL event metric
		if c.eventsProcessed != nil {
			c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", "kubelet_pod_not_ready"),
				attribute.String("namespace", pod.Namespace),
				attribute.String("pod", pod.Name),
				attribute.String("condition_type", eventData.Condition),
				attribute.String("condition_reason", eventData.Reason),
			))
		}
	case <-c.ctx.Done():
	}
}

// Helper methods

// extractTraceContext extracts trace and span IDs from context
func (c *Collector) extractTraceContext(ctx context.Context) (traceID, spanID string) {
	traceID = collectors.GenerateTraceID()
	spanID = collectors.GenerateSpanID()
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		traceID = span.SpanContext().TraceID().String()
		spanID = span.SpanContext().SpanID().String()
	}
	return traceID, spanID
}

// PodTraceEntry holds trace ID with timestamp for TTL cleanup
type PodTraceEntry struct {
	TraceID   string
	Timestamp time.Time
}

// PodTraceManager manages trace IDs with TTL cleanup
type PodTraceManager struct {
	entries map[types.UID]*PodTraceEntry
	mu      sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewPodTraceManager creates a new pod trace manager with TTL cleanup
func NewPodTraceManager() *PodTraceManager {
	ctx, cancel := context.WithCancel(context.Background())
	ptm := &PodTraceManager{
		entries: make(map[types.UID]*PodTraceEntry),
		ctx:     ctx,
		cancel:  cancel,
	}

	// Start cleanup goroutine
	go ptm.cleanup()

	return ptm
}

// GetOrGenerate gets existing trace ID or generates new one
func (ptm *PodTraceManager) GetOrGenerate(podUID types.UID) string {
	ptm.mu.RLock()
	if entry, exists := ptm.entries[podUID]; exists {
		ptm.mu.RUnlock()
		return entry.TraceID
	}
	ptm.mu.RUnlock()

	// Generate new trace ID
	ptm.mu.Lock()
	traceID := collectors.GenerateTraceID()
	ptm.entries[podUID] = &PodTraceEntry{
		TraceID:   traceID,
		Timestamp: time.Now(),
	}
	ptm.mu.Unlock()

	return traceID
}

// Count returns the number of tracked pod traces
func (ptm *PodTraceManager) Count() int {
	ptm.mu.RLock()
	defer ptm.mu.RUnlock()
	return len(ptm.entries)
}

// Stop stops the cleanup goroutine
func (ptm *PodTraceManager) Stop() {
	ptm.cancel()
}

// cleanup runs periodic cleanup of expired entries (every 5 minutes)
func (ptm *PodTraceManager) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ptm.ctx.Done():
			return
		case <-ticker.C:
			ptm.cleanupExpired()
		}
	}
}

// cleanupExpired removes entries older than 1 hour
func (ptm *PodTraceManager) cleanupExpired() {
	ptm.mu.Lock()
	defer ptm.mu.Unlock()

	expiry := time.Now().Add(-1 * time.Hour)
	for uid, entry := range ptm.entries {
		if entry.Timestamp.Before(expiry) {
			delete(ptm.entries, uid)
		}
	}
}

func (c *Collector) getOrGenerateTraceID(podUID types.UID) string {
	return c.podTraceManager.GetOrGenerate(podUID)
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

// Health returns detailed health information with typed structure
func (c *Collector) Health() (bool, *HealthStatus) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	health := &HealthStatus{
		Healthy:         c.healthy,
		EventsCollected: c.stats.eventsCollected,
		ErrorsCount:     c.stats.errorsCount,
		LastEventTime:   c.stats.lastEventTime,
		KubeletAddress:  c.config.Address,
	}

	return c.healthy, health
}

// Statistics returns collector statistics with typed structure
func (c *Collector) Statistics() *Statistics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return &Statistics{
		EventsCollected: c.stats.eventsCollected,
		ErrorsCount:     c.stats.errorsCount,
		LastEventTime:   c.stats.lastEventTime,
		PodTraceCount:   c.podTraceManager.Count(),
	}
}
