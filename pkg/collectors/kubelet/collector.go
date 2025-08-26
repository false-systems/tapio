package kubelet

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
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

// (Config defined in config.go)

// generateEventID creates a unique event ID for kubelet events
func generateEventID(eventType, source string) string {
	timestamp := time.Now().UnixNano()
	data := fmt.Sprintf("%s-%s-%d", eventType, source, timestamp)
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("kubelet-%s", hex.EncodeToString(hash[:])[:16])
}

// Event data structures

// Old data structures removed - now using typed domain structures

// (DefaultConfig defined in config.go)

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
	events          chan *domain.CollectorEvent
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

	// OTEL instrumentation - 5 Core Metrics (MANDATORY)
	tracer          trace.Tracer
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
	droppedEvents   metric.Int64Counter
	bufferUsage     metric.Int64Gauge

	// kubelet-specific metrics (optional)
	apiLatency  metric.Float64Histogram
	pollsActive metric.Int64UpDownCounter
	apiFailures metric.Int64Counter
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

	droppedEvents, err := meter.Int64Counter(
		fmt.Sprintf("%s_dropped_events_total", name),
		metric.WithDescription(fmt.Sprintf("Total dropped events by %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create dropped events counter", zap.Error(err))
	}

	bufferUsage, err := meter.Int64Gauge(
		fmt.Sprintf("%s_buffer_usage", name),
		metric.WithDescription(fmt.Sprintf("Current buffer usage for %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create buffer usage gauge", zap.Error(err))
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
		events:          make(chan *domain.CollectorEvent, 10000),
		healthy:         true,
		logger:          config.Logger,
		podTraceManager: NewPodTraceManager(),
		tracer:          tracer,
		eventsProcessed: eventsProcessed,
		errorsTotal:     errorsTotal,
		processingTime:  processingTime,
		droppedEvents:   droppedEvents,
		bufferUsage:     bufferUsage,
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
func (c *Collector) Events() <-chan *domain.CollectorEvent {
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
	traceID, spanID := c.extractTraceContext(ctx)

	event := &domain.CollectorEvent{
		EventID:   generateEventID("node_cpu", c.name),
		Timestamp: time.Now(),
		Source:    c.name,
		Type:      domain.EventTypeKubeletNodeCPU,
		Severity:  domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			Kubelet: &domain.KubeletData{
				EventType: "node_cpu",
				NodeMetrics: &domain.KubeletNodeMetrics{
					NodeName:      summary.Node.NodeName,
					CPUUsageNano:  *summary.Node.CPU.UsageNanoCores,
					CPUUsageMilli: *summary.Node.CPU.UsageNanoCores / 1000000,
					Timestamp:     summary.Node.CPU.Time.Time,
				},
			},
		},
		Metadata: domain.EventMetadata{
			TraceID: traceID,
			SpanID:  spanID,
		},
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
	traceID, spanID := c.extractTraceContext(ctx)

	event := &domain.CollectorEvent{
		EventID:   generateEventID("node_memory", c.name),
		Timestamp: time.Now(),
		Source:    c.name,
		Type:      domain.EventTypeKubeletNodeMemory,
		Severity:  domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			Kubelet: &domain.KubeletData{
				EventType: "node_memory",
				NodeMetrics: &domain.KubeletNodeMetrics{
					NodeName:         summary.Node.NodeName,
					MemoryUsage:      *summary.Node.Memory.UsageBytes,
					MemoryAvailable:  *summary.Node.Memory.AvailableBytes,
					MemoryWorkingSet: *summary.Node.Memory.WorkingSetBytes,
					Timestamp:        summary.Node.Memory.Time.Time,
				},
			},
		},
		Metadata: domain.EventMetadata{
			TraceID: traceID,
			SpanID:  spanID,
		},
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
	traceID, spanID := c.extractTraceContext(ctx)

	event := &domain.CollectorEvent{
		EventID:   generateEventID("cpu_throttling", c.name),
		Timestamp: time.Now(),
		Source:    c.name,
		Type:      domain.EventTypeKubeletCPUThrottling,
		Severity:  domain.EventSeverityWarning,
		EventData: domain.EventDataContainer{
			Kubelet: &domain.KubeletData{
				EventType: "cpu_throttling",
				ContainerMetrics: &domain.KubeletContainerMetrics{
					Namespace:    pod.PodRef.Namespace,
					Pod:          pod.PodRef.Name,
					Container:    container.Name,
					CPUUsageNano: *container.CPU.UsageNanoCores,
					Timestamp:    container.CPU.Time.Time,
				},
			},
		},
		Metadata: domain.EventMetadata{
			TraceID: traceID,
			SpanID:  spanID,
		},
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

	traceID, spanID := c.extractTraceContext(ctx)

	containerMetrics := &domain.KubeletContainerMetrics{
		Namespace:        pod.PodRef.Namespace,
		Pod:              pod.PodRef.Name,
		Container:        container.Name,
		MemoryUsage:      *container.Memory.UsageBytes,
		MemoryWorkingSet: *container.Memory.WorkingSetBytes,
		Timestamp:        container.Memory.Time.Time,
	}

	// Check if RSS is available (indicates memory pressure)
	if container.Memory.RSSBytes != nil {
		containerMetrics.MemoryRSS = *container.Memory.RSSBytes
	}

	event := &domain.CollectorEvent{
		EventID:   generateEventID("memory_pressure", c.name),
		Timestamp: time.Now(),
		Source:    c.name,
		Type:      domain.EventTypeKubeletMemoryPressure,
		Severity:  domain.EventSeverityWarning,
		EventData: domain.EventDataContainer{
			Kubelet: &domain.KubeletData{
				EventType:        "memory_pressure",
				ContainerMetrics: containerMetrics,
			},
		},
		Metadata: domain.EventMetadata{
			TraceID: traceID,
			SpanID:  spanID,
		},
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
		traceID, spanID := c.extractTraceContext(ctx)

		event := &domain.CollectorEvent{
			EventID:   generateEventID("ephemeral_storage", c.name),
			Timestamp: time.Now(),
			Source:    c.name,
			Type:      domain.EventTypeKubeletEphemeralStorage,
			Severity:  domain.EventSeverityWarning,
			EventData: domain.EventDataContainer{
				Kubelet: &domain.KubeletData{
					EventType: "ephemeral_storage",
					StorageEvent: &domain.KubeletStorageEvent{
						Namespace:      pod.PodRef.Namespace,
						Pod:            pod.PodRef.Name,
						UsedBytes:      usedBytes,
						AvailableBytes: availableBytes,
						UsagePercent:   usagePercent,
						Timestamp:      pod.EphemeralStorage.Time.Time,
					},
				},
			},
			Metadata: domain.EventMetadata{
				TraceID: traceID,
				SpanID:  spanID,
			},
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
	traceID, spanID := c.extractTraceContext(ctx)

	event := &domain.CollectorEvent{
		EventID:   generateEventID("container_waiting", c.name),
		Timestamp: time.Now(),
		Source:    c.name,
		Type:      domain.EventTypeKubeletContainerWaiting,
		Severity:  domain.EventSeverityWarning,
		EventData: domain.EventDataContainer{
			Kubelet: &domain.KubeletData{
				EventType: "container_waiting",
				PodLifecycle: &domain.KubeletPodLifecycle{
					Namespace: pod.Namespace,
					Pod:       pod.Name,
					Container: status.Name,
					Reason:    status.State.Waiting.Reason,
					Message:   status.State.Waiting.Message,
					Timestamp: time.Now(),
				},
			},
		},
		Metadata: domain.EventMetadata{
			TraceID: traceID,
			SpanID:  spanID,
		},
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
				attribute.String("waiting_reason", status.State.Waiting.Reason),
			))
		}
	case <-c.ctx.Done():
	}
}

// sendContainerTerminatedEvent sends events for terminated containers
func (c *Collector) sendContainerTerminatedEvent(ctx context.Context, pod *v1.Pod, status *v1.ContainerStatus) {
	traceID, spanID := c.extractTraceContext(ctx)

	event := &domain.CollectorEvent{
		EventID:   generateEventID("container_terminated", c.name),
		Timestamp: time.Now(),
		Source:    c.name,
		Type:      domain.EventTypeKubeletContainerTerminated,
		Severity:  domain.EventSeverityError,
		EventData: domain.EventDataContainer{
			Kubelet: &domain.KubeletData{
				EventType: "container_terminated",
				PodLifecycle: &domain.KubeletPodLifecycle{
					Namespace: pod.Namespace,
					Pod:       pod.Name,
					Container: status.Name,
					ExitCode:  status.State.Terminated.ExitCode,
					Reason:    status.State.Terminated.Reason,
					Message:   status.State.Terminated.Message,
					Timestamp: status.State.Terminated.FinishedAt.Time,
				},
			},
		},
		Metadata: domain.EventMetadata{
			TraceID: traceID,
			SpanID:  spanID,
		},
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
				attribute.Int("exit_code", int(status.State.Terminated.ExitCode)),
			))
		}
	case <-c.ctx.Done():
	}
}

// sendCrashLoopEvent detects crash loop patterns
func (c *Collector) sendCrashLoopEvent(ctx context.Context, pod *v1.Pod, status *v1.ContainerStatus) {
	if status.RestartCount > 3 { // Likely in crash loop
		traceID, spanID := c.extractTraceContext(ctx)

		event := &domain.CollectorEvent{
			EventID:   generateEventID("crash_loop", c.name),
			Timestamp: time.Now(),
			Source:    c.name,
			Type:      domain.EventTypeKubeletCrashLoop,
			Severity:  domain.EventSeverityCritical,
			EventData: domain.EventDataContainer{
				Kubelet: &domain.KubeletData{
					EventType: "crash_loop",
					PodLifecycle: &domain.KubeletPodLifecycle{
						Namespace:    pod.Namespace,
						Pod:          pod.Name,
						Container:    status.Name,
						RestartCount: status.RestartCount,
						LastExitCode: status.LastTerminationState.Terminated.ExitCode,
						LastReason:   status.LastTerminationState.Terminated.Reason,
						Timestamp:    time.Now(),
					},
				},
			},
			Metadata: domain.EventMetadata{
				TraceID: traceID,
				SpanID:  spanID,
			},
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
					attribute.Int("restart_count", int(status.RestartCount)),
				))
			}
		case <-c.ctx.Done():
		}
	}
}

// sendPodNotReadyEvent sends events for pods not ready
func (c *Collector) sendPodNotReadyEvent(ctx context.Context, pod *v1.Pod, condition *v1.PodCondition) {
	traceID, spanID := c.extractTraceContext(ctx)

	event := &domain.CollectorEvent{
		EventID:   generateEventID("pod_not_ready", c.name),
		Timestamp: time.Now(),
		Source:    c.name,
		Type:      domain.EventTypeKubeletPodNotReady,
		Severity:  domain.EventSeverityWarning,
		EventData: domain.EventDataContainer{
			Kubelet: &domain.KubeletData{
				EventType: "pod_not_ready",
				PodLifecycle: &domain.KubeletPodLifecycle{
					Namespace: pod.Namespace,
					Pod:       pod.Name,
					Condition: string(condition.Type),
					Status:    string(condition.Status),
					Reason:    condition.Reason,
					Message:   condition.Message,
					Timestamp: condition.LastTransitionTime.Time,
				},
			},
		},
		Metadata: domain.EventMetadata{
			TraceID: traceID,
			SpanID:  spanID,
		},
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
				attribute.String("condition_type", string(condition.Type)),
				attribute.String("condition_reason", condition.Reason),
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
