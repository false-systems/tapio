package noderuntime

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

	"github.com/yairfalse/tapio/internal/observers"
	"github.com/yairfalse/tapio/internal/observers/base"
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

// generateEventID creates a unique event ID for node-runtime events
func generateEventID(eventType, source string) string {
	timestamp := time.Now().UnixNano()
	data := fmt.Sprintf("%s-%s-%d", eventType, source, timestamp)
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("node-runtime-%s", hex.EncodeToString(hash[:])[:16])
}

// Observer implements the node-runtime metrics observer
type Observer struct {
	*base.BaseObserver        // Provides Statistics() and Health()
	*base.EventChannelManager // Handles event channel with drop counting
	*base.LifecycleManager    // Manages goroutines and graceful shutdown

	name            string
	config          *Config
	client          *http.Client
	logger          *zap.Logger
	podTraceManager *PodTraceManager

	// OTEL instrumentation - 5 Core Metrics (MANDATORY)
	tracer          trace.Tracer
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
	droppedEvents   metric.Int64Counter
	bufferUsage     metric.Int64Gauge

	// node-runtime-specific metrics (optional)
	apiLatency  metric.Float64Histogram
	pollsActive metric.Int64UpDownCounter
	apiFailures metric.Int64Counter
}

// NewObserver creates a new node-runtime observer
func NewObserver(name string, config *Config) (*Observer, error) {
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
		Timeout: config.RequestTimeout,
	}

	return &Observer{
		BaseObserver:        base.NewBaseObserver(name, 30*time.Second),
		EventChannelManager: base.NewEventChannelManager(10000, name, config.Logger),
		LifecycleManager:    base.NewLifecycleManager(context.Background(), config.Logger),

		name:            name,
		config:          config,
		client:          client,
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

// Name returns the observer name
func (o *Observer) Name() string {
	return o.name
}

// Start begins collection
func (o *Observer) Start(ctx context.Context) error {
	o.logger.Info("Starting node-runtime observer")

	// Create new lifecycle manager with provided context
	o.LifecycleManager = base.NewLifecycleManager(ctx, o.logger)

	// Verify connectivity
	if err := o.checkConnectivity(); err != nil {
		return fmt.Errorf("node-runtime connectivity check failed: %w", err)
	}

	// Start collection goroutines using LifecycleManager
	o.LifecycleManager.Start("collect-stats", func() {
		o.collectStats()
	})

	o.LifecycleManager.Start("collect-pod-metrics", func() {
		o.collectPodMetrics()
	})

	o.BaseObserver.SetHealthy(true)

	o.logger.Info("Node-runtime observer started",
		zap.String("address", o.config.Address),
		zap.Duration("stats_interval", o.config.StatsInterval))

	return nil
}

// Stop gracefully shuts down the observer
func (o *Observer) Stop() error {
	o.logger.Info("Stopping node-runtime observer")

	// Stop lifecycle manager (waits for goroutines)
	o.LifecycleManager.Stop(5 * time.Second)

	// Stop pod trace manager
	if o.podTraceManager != nil {
		o.podTraceManager.Stop()
	}

	// Close event channel
	o.EventChannelManager.Close()

	// Mark as unhealthy
	o.BaseObserver.SetHealthy(false)

	o.logger.Info("Node-runtime observer stopped")
	return nil
}

// Events returns the event channel
func (o *Observer) Events() <-chan *domain.CollectorEvent {
	return o.EventChannelManager.GetChannel()
}

// IsHealthy returns health status
func (o *Observer) IsHealthy() bool {
	health := o.BaseObserver.Health()
	return health.Status == domain.HealthHealthy
}

// checkConnectivity verifies we can reach the kubelet API
func (o *Observer) checkConnectivity() error {
	url := fmt.Sprintf("https://%s/healthz", o.config.Address)
	if o.config.Insecure {
		url = fmt.Sprintf("http://%s/healthz", o.config.Address)
	}

	resp, err := o.client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("node-runtime health check failed: %s", resp.Status)
	}

	return nil
}

// collectStats collects node-runtime stats summary
func (o *Observer) collectStats() {
	ticker := time.NewTicker(o.config.StatsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-o.LifecycleManager.Context().Done():
			return
		case <-ticker.C:
			if err := o.fetchStats(); err != nil {
				o.logger.Error("Failed to fetch node-runtime stats", zap.Error(err))
				o.BaseObserver.RecordError(err)
			}
		}
	}
}

// fetchStats fetches and processes stats from kubelet API
func (o *Observer) fetchStats() error {
	start := time.Now()
	ctx, span := o.tracer.Start(o.LifecycleManager.Context(), "node-runtime.fetch_stats")
	defer span.End()

	// Track active poll
	if o.pollsActive != nil {
		o.pollsActive.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", "fetch_stats"),
		))
		defer o.pollsActive.Add(ctx, -1, metric.WithAttributes(
			attribute.String("operation", "fetch_stats"),
		))
	}

	url := fmt.Sprintf("https://%s/stats/summary", o.config.Address)
	if o.config.Insecure {
		url = fmt.Sprintf("http://%s/stats/summary", o.config.Address)
	}

	span.SetAttributes(
		attribute.String("node-runtime.endpoint", "/stats/summary"),
		attribute.String("node-runtime.url", url),
	)

	resp, err := o.client.Get(url)
	if err != nil {
		// Record API failure
		if o.apiFailures != nil {
			o.apiFailures.Add(ctx, 1, metric.WithAttributes(
				attribute.String("endpoint", "/stats/summary"),
				attribute.String("error", "request_failed"),
			))
		}
		if o.errorsTotal != nil {
			o.errorsTotal.Add(ctx, 1, metric.WithAttributes(
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
		if o.apiFailures != nil {
			o.apiFailures.Add(ctx, 1, metric.WithAttributes(
				attribute.String("endpoint", "/stats/summary"),
				attribute.String("error", "http_status"),
				attribute.Int("status_code", resp.StatusCode),
			))
		}
		if o.errorsTotal != nil {
			o.errorsTotal.Add(ctx, 1, metric.WithAttributes(
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
		if o.errorsTotal != nil {
			o.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "decode"),
			))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to decode stats")
		return fmt.Errorf("failed to decode stats: %w", err)
	}

	// Record API latency
	duration := time.Since(start)
	if o.apiLatency != nil {
		o.apiLatency.Record(ctx, duration.Seconds()*1000, metric.WithAttributes(
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
		o.sendNodeCPUEvent(ctx, &summary)
	}

	if summary.Node.Memory != nil {
		o.sendNodeMemoryEvent(ctx, &summary)
	}

	// Process pod stats
	for _, pod := range summary.Pods {
		o.processPodStats(ctx, &pod)
	}

	return nil
}

// sendNodeCPUEvent sends node CPU metrics
func (o *Observer) sendNodeCPUEvent(ctx context.Context, summary *statsv1alpha1.Summary) {
	traceID, spanID := o.extractTraceContext(ctx)

	event := &domain.CollectorEvent{
		EventID:   generateEventID("node_cpu", o.name),
		Timestamp: time.Now(),
		Source:    o.name,
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
			Labels: map[string]string{
				"observer": o.name,
				"version":  "1.0.0",
			},
		},
	}

	if o.EventChannelManager.SendEvent(event) {
		o.BaseObserver.RecordEvent()
		// Record OTEL event metric
		if o.eventsProcessed != nil {
			o.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", "node_runtime_cpu"),
				attribute.String("node_name", summary.Node.NodeName),
			))
		}
	} else {
		o.BaseObserver.RecordError(fmt.Errorf("channel full"))
		if o.droppedEvents != nil {
			o.droppedEvents.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", "node_runtime_cpu"),
			))
		}
	}
}

// sendNodeMemoryEvent sends node memory metrics
func (o *Observer) sendNodeMemoryEvent(ctx context.Context, summary *statsv1alpha1.Summary) {
	traceID, spanID := o.extractTraceContext(ctx)

	event := &domain.CollectorEvent{
		EventID:   generateEventID("node_memory", o.name),
		Timestamp: time.Now(),
		Source:    o.name,
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
			Labels: map[string]string{
				"observer": o.name,
				"version":  "1.0.0",
			},
		},
	}

	if o.EventChannelManager.SendEvent(event) {
		o.BaseObserver.RecordEvent()
		// Record OTEL event metric
		if o.eventsProcessed != nil {
			o.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", "node_runtime_memory"),
				attribute.String("node_name", summary.Node.NodeName),
			))
		}
	} else {
		o.BaseObserver.RecordError(fmt.Errorf("channel full"))
		if o.droppedEvents != nil {
			o.droppedEvents.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", "node_runtime_memory"),
			))
		}
	}
}

// processPodStats processes stats for a single pod
func (o *Observer) processPodStats(ctx context.Context, pod *statsv1alpha1.PodStats) {
	// Check for CPU throttling
	for _, container := range pod.Containers {
		if container.CPU != nil && container.CPU.UsageNanoCores != nil {
			o.checkCPUThrottling(ctx, pod, &container)
		}

		if container.Memory != nil {
			o.checkMemoryPressure(ctx, pod, &container)
		}
	}

	// Check ephemeral storage
	if pod.EphemeralStorage != nil {
		o.checkEphemeralStorage(ctx, pod)
	}
}

// checkCPUThrottling detects CPU throttling
func (o *Observer) checkCPUThrottling(ctx context.Context, pod *statsv1alpha1.PodStats, container *statsv1alpha1.ContainerStats) {
	// Note: Real throttling metrics would come from cAdvisor metrics endpoint
	// This is a simplified version
	traceID, spanID := o.extractTraceContext(ctx)

	event := &domain.CollectorEvent{
		EventID:   generateEventID("cpu_throttling", o.name),
		Timestamp: time.Now(),
		Source:    o.name,
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
			Labels: map[string]string{
				"observer": o.name,
				"version":  "1.0.0",
			},
		},
	}

	if o.EventChannelManager.SendEvent(event) {
		o.BaseObserver.RecordEvent()
		// Record OTEL event metric
		if o.eventsProcessed != nil {
			o.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", "node_runtime_cpu_throttling"),
				attribute.String("namespace", pod.PodRef.Namespace),
				attribute.String("pod", pod.PodRef.Name),
				attribute.String("container", container.Name),
			))
		}
	} else {
		o.BaseObserver.RecordError(fmt.Errorf("channel full"))
	}
}

// checkMemoryPressure detects memory pressure
func (o *Observer) checkMemoryPressure(ctx context.Context, pod *statsv1alpha1.PodStats, container *statsv1alpha1.ContainerStats) {
	if container.Memory.WorkingSetBytes == nil || container.Memory.UsageBytes == nil {
		return
	}

	traceID, spanID := o.extractTraceContext(ctx)

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
		EventID:   generateEventID("memory_pressure", o.name),
		Timestamp: time.Now(),
		Source:    o.name,
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
			Labels: map[string]string{
				"observer": o.name,
				"version":  "1.0.0",
			},
		},
	}

	if o.EventChannelManager.SendEvent(event) {
		o.BaseObserver.RecordEvent()
		// Record OTEL event metric
		if o.eventsProcessed != nil {
			o.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", "node_runtime_memory_pressure"),
				attribute.String("namespace", pod.PodRef.Namespace),
				attribute.String("pod", pod.PodRef.Name),
				attribute.String("container", container.Name),
			))
		}
	} else {
		o.BaseObserver.RecordError(fmt.Errorf("channel full"))
	}
}

// checkEphemeralStorage checks ephemeral storage usage
func (o *Observer) checkEphemeralStorage(ctx context.Context, pod *statsv1alpha1.PodStats) {
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
		traceID, spanID := o.extractTraceContext(ctx)

		event := &domain.CollectorEvent{
			EventID:   generateEventID("ephemeral_storage", o.name),
			Timestamp: time.Now(),
			Source:    o.name,
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
				Labels: map[string]string{
					"observer": o.name,
					"version":  "1.0.0",
				},
			},
		}

		if o.EventChannelManager.SendEvent(event) {
			o.BaseObserver.RecordEvent()
			// Record OTEL event metric
			if o.eventsProcessed != nil {
				o.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
					attribute.String("event_type", "node_runtime_ephemeral_storage"),
					attribute.String("namespace", pod.PodRef.Namespace),
					attribute.String("pod", pod.PodRef.Name),
				))
			}
		} else {
			o.BaseObserver.RecordError(fmt.Errorf("channel full"))
		}
	}
}

// collectPodMetrics collects pod lifecycle events
func (o *Observer) collectPodMetrics() {
	ticker := time.NewTicker(o.config.MetricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-o.LifecycleManager.Context().Done():
			return
		case <-ticker.C:
			if err := o.fetchPodLifecycle(); err != nil {
				o.logger.Error("Failed to fetch pod lifecycle", zap.Error(err))
				o.BaseObserver.RecordError(err)
			}
		}
	}
}

// fetchPodLifecycle fetches pod status from kubelet
func (o *Observer) fetchPodLifecycle() error {
	start := time.Now()
	ctx, span := o.tracer.Start(o.LifecycleManager.Context(), "node-runtime.fetch_pod_lifecycle")
	defer span.End()

	// Track active poll
	if o.pollsActive != nil {
		o.pollsActive.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", "fetch_pod_lifecycle"),
		))
		defer o.pollsActive.Add(ctx, -1, metric.WithAttributes(
			attribute.String("operation", "fetch_pod_lifecycle"),
		))
	}

	url := fmt.Sprintf("https://%s/pods", o.config.Address)
	if o.config.Insecure {
		url = fmt.Sprintf("http://%s/pods", o.config.Address)
	}

	span.SetAttributes(
		attribute.String("node-runtime.endpoint", "/pods"),
		attribute.String("node-runtime.url", url),
	)

	resp, err := o.client.Get(url)
	if err != nil {
		// Record API failure
		if o.apiFailures != nil {
			o.apiFailures.Add(ctx, 1, metric.WithAttributes(
				attribute.String("endpoint", "/pods"),
				attribute.String("error", "request_failed"),
			))
		}
		if o.errorsTotal != nil {
			o.errorsTotal.Add(ctx, 1, metric.WithAttributes(
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
		if o.apiFailures != nil {
			o.apiFailures.Add(ctx, 1, metric.WithAttributes(
				attribute.String("endpoint", "/pods"),
				attribute.String("error", "http_status"),
				attribute.Int("status_code", resp.StatusCode),
			))
		}
		if o.errorsTotal != nil {
			o.errorsTotal.Add(ctx, 1, metric.WithAttributes(
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
		if o.errorsTotal != nil {
			o.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "decode"),
			))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to decode pods")
		return fmt.Errorf("failed to decode pods: %w", err)
	}

	// Record API latency
	duration := time.Since(start)
	if o.apiLatency != nil {
		o.apiLatency.Record(ctx, duration.Seconds()*1000, metric.WithAttributes(
			attribute.String("endpoint", "/pods"),
		))
	}

	span.SetAttributes(
		attribute.Float64("duration_seconds", duration.Seconds()),
		attribute.Int("pods_count", len(podList.Items)),
	)

	// Process pod statuses
	for _, pod := range podList.Items {
		o.processPodStatus(ctx, &pod)
	}

	return nil
}

// processPodStatus checks for pod issues
func (o *Observer) processPodStatus(ctx context.Context, pod *v1.Pod) {
	// Check container statuses
	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Waiting != nil {
			o.sendContainerWaitingEvent(ctx, pod, &status)
		}

		if status.State.Terminated != nil && status.State.Terminated.ExitCode != 0 {
			o.sendContainerTerminatedEvent(ctx, pod, &status)
		}

		// Check last termination state for crash loops
		if status.LastTerminationState.Terminated != nil {
			o.sendCrashLoopEvent(ctx, pod, &status)
		}
	}

	// Check pod conditions
	for _, condition := range pod.Status.Conditions {
		if condition.Type == v1.PodReady && condition.Status != v1.ConditionTrue {
			o.sendPodNotReadyEvent(ctx, pod, &condition)
		}
	}
}

// sendContainerWaitingEvent sends events for waiting containers
func (o *Observer) sendContainerWaitingEvent(ctx context.Context, pod *v1.Pod, status *v1.ContainerStatus) {
	traceID, spanID := o.extractTraceContext(ctx)

	event := &domain.CollectorEvent{
		EventID:   generateEventID("container_waiting", o.name),
		Timestamp: time.Now(),
		Source:    o.name,
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
			Labels: map[string]string{
				"observer": o.name,
				"version":  "1.0.0",
			},
		},
	}

	if o.EventChannelManager.SendEvent(event) {
		o.BaseObserver.RecordEvent()
		// Record OTEL event metric
		if o.eventsProcessed != nil {
			o.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", "node_runtime_container_waiting"),
				attribute.String("namespace", pod.Namespace),
				attribute.String("pod", pod.Name),
				attribute.String("container", status.Name),
				attribute.String("waiting_reason", status.State.Waiting.Reason),
			))
		}
	} else {
		o.BaseObserver.RecordError(fmt.Errorf("channel full"))
	}
}

// sendContainerTerminatedEvent sends events for terminated containers
func (o *Observer) sendContainerTerminatedEvent(ctx context.Context, pod *v1.Pod, status *v1.ContainerStatus) {
	traceID, spanID := o.extractTraceContext(ctx)

	event := &domain.CollectorEvent{
		EventID:   generateEventID("container_terminated", o.name),
		Timestamp: time.Now(),
		Source:    o.name,
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
			Labels: map[string]string{
				"observer": o.name,
				"version":  "1.0.0",
			},
		},
	}

	if o.EventChannelManager.SendEvent(event) {
		o.BaseObserver.RecordEvent()
		// Record OTEL event metric
		if o.eventsProcessed != nil {
			o.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", "node_runtime_container_terminated"),
				attribute.String("namespace", pod.Namespace),
				attribute.String("pod", pod.Name),
				attribute.String("container", status.Name),
				attribute.Int("exit_code", int(status.State.Terminated.ExitCode)),
			))
		}
	} else {
		o.BaseObserver.RecordError(fmt.Errorf("channel full"))
	}
}

// sendCrashLoopEvent detects crash loop patterns
func (o *Observer) sendCrashLoopEvent(ctx context.Context, pod *v1.Pod, status *v1.ContainerStatus) {
	if status.RestartCount > 3 { // Likely in crash loop
		traceID, spanID := o.extractTraceContext(ctx)

		event := &domain.CollectorEvent{
			EventID:   generateEventID("crash_loop", o.name),
			Timestamp: time.Now(),
			Source:    o.name,
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
				Labels: map[string]string{
					"observer": o.name,
					"version":  "1.0.0",
				},
			},
		}

		if o.EventChannelManager.SendEvent(event) {
			o.BaseObserver.RecordEvent()
			// Record OTEL event metric
			if o.eventsProcessed != nil {
				o.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
					attribute.String("event_type", "node_runtime_crash_loop"),
					attribute.String("namespace", pod.Namespace),
					attribute.String("pod", pod.Name),
					attribute.String("container", status.Name),
					attribute.Int("restart_count", int(status.RestartCount)),
				))
			}
		} else {
			o.BaseObserver.RecordError(fmt.Errorf("channel full"))
		}
	}
}

// sendPodNotReadyEvent sends events for pods not ready
func (o *Observer) sendPodNotReadyEvent(ctx context.Context, pod *v1.Pod, condition *v1.PodCondition) {
	traceID, spanID := o.extractTraceContext(ctx)

	event := &domain.CollectorEvent{
		EventID:   generateEventID("pod_not_ready", o.name),
		Timestamp: time.Now(),
		Source:    o.name,
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
			Labels: map[string]string{
				"observer": o.name,
				"version":  "1.0.0",
			},
		},
	}

	if o.EventChannelManager.SendEvent(event) {
		o.BaseObserver.RecordEvent()
		// Record OTEL event metric
		if o.eventsProcessed != nil {
			o.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", "node_runtime_pod_not_ready"),
				attribute.String("namespace", pod.Namespace),
				attribute.String("pod", pod.Name),
				attribute.String("condition_type", string(condition.Type)),
				attribute.String("condition_reason", condition.Reason),
			))
		}
	} else {
		o.BaseObserver.RecordError(fmt.Errorf("channel full"))
	}
}

// Helper methods

// extractTraceContext extracts trace and span IDs from context
func (o *Observer) extractTraceContext(ctx context.Context) (traceID, spanID string) {
	traceID = observers.GenerateTraceID()
	spanID = observers.GenerateSpanID()
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
	traceID := observers.GenerateTraceID()
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

// Legacy compatibility methods for migration

// Statistics returns observer statistics
func (o *Observer) Statistics() interface{} {
	stats := o.BaseObserver.Statistics()
	// Add kubelet-specific stats as custom metrics
	if stats.CustomMetrics == nil {
		stats.CustomMetrics = make(map[string]string)
	}
	stats.CustomMetrics["pod_traces"] = fmt.Sprintf("%d", o.podTraceManager.Count())
	stats.CustomMetrics["node_runtime_address"] = o.config.Address
	return stats
}

// Health returns health status
func (o *Observer) Health() *domain.HealthStatus {
	health := o.BaseObserver.Health()
	health.Component = o.name
	// Add error count from statistics
	stats := o.BaseObserver.Statistics()
	if stats != nil {
		health.ErrorCount = stats.ErrorCount
	}
	return health
}
