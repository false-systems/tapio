package noderuntime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	statsv1alpha1 "k8s.io/kubelet/pkg/apis/stats/v1alpha1"
)

// StatsCollector collects kubelet node and pod statistics from /stats/summary
type StatsCollector struct {
	*BaseCollector
	httpClient       *http.Client
	kubeletURL       string
	tracer           trace.Tracer
	apiLatency       metric.Float64Histogram
	traceContextFunc func(context.Context) (string, string)
}

// NewStatsCollector creates a new StatsCollector
func NewStatsCollector(
	observerName, kubeletURL string,
	httpClient *http.Client,
	tracer trace.Tracer,
	apiLatency metric.Float64Histogram,
	traceContextFunc func(context.Context) (string, string),
) *StatsCollector {
	return &StatsCollector{
		BaseCollector:    NewBaseCollector("stats", "/stats/summary", observerName),
		httpClient:       httpClient,
		kubeletURL:       kubeletURL,
		tracer:           tracer,
		apiLatency:       apiLatency,
		traceContextFunc: traceContextFunc,
	}
}

// Collect fetches stats from kubelet and returns domain events
func (sc *StatsCollector) Collect(ctx context.Context) ([]domain.CollectorEvent, error) {
	ctx, span := sc.tracer.Start(ctx, "StatsCollector.Collect")
	defer span.End()

	start := time.Now()

	// Fetch stats from kubelet
	req, err := http.NewRequestWithContext(ctx, "GET", sc.kubeletURL+sc.endpoint, nil)
	if err != nil {
		sc.SetHealthy(false)
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := sc.httpClient.Do(req)
	if err != nil {
		sc.SetHealthy(false)
		return nil, fmt.Errorf("failed to fetch stats: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		sc.SetHealthy(false)
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("kubelet returned status %d: %s", resp.StatusCode, body)
	}

	var summary statsv1alpha1.Summary
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		sc.SetHealthy(false)
		return nil, fmt.Errorf("failed to decode stats: %w", err)
	}

	sc.SetHealthy(true)

	// Record API latency
	duration := time.Since(start)
	if sc.apiLatency != nil {
		sc.apiLatency.Record(ctx, duration.Seconds()*1000, metric.WithAttributes(
			attribute.String("endpoint", sc.endpoint),
		))
	}

	span.SetAttributes(
		attribute.Float64("duration_seconds", duration.Seconds()),
		attribute.Int("node_stats_count", 1),
		attribute.Int("pod_stats_count", len(summary.Pods)),
	)

	// Process and build events (sending handled by observer)
	events := make([]domain.CollectorEvent, 0, 2+len(summary.Pods))

	// Node CPU event
	if summary.Node.CPU != nil {
		event := sc.buildNodeCPUEvent(ctx, &summary)
		events = append(events, *event)
	}

	// Node memory event
	if summary.Node.Memory != nil {
		event := sc.buildNodeMemoryEvent(ctx, &summary)
		events = append(events, *event)
	}

	// Pod-level events
	for _, pod := range summary.Pods {
		podEvents := sc.processPodStats(ctx, &pod)
		events = append(events, podEvents...)
	}

	return events, nil
}

// buildNodeCPUEvent creates a node CPU event
func (sc *StatsCollector) buildNodeCPUEvent(ctx context.Context, summary *statsv1alpha1.Summary) *domain.CollectorEvent {
	traceID, spanID := sc.traceContextFunc(ctx)

	return &domain.CollectorEvent{
		EventID:   generateEventID("node_cpu", sc.observerName),
		Timestamp: time.Now(),
		Source:    sc.observerName,
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
				"observer": sc.observerName,
				"version":  ObserverVersion,
			},
		},
	}
}

// buildNodeMemoryEvent creates a node memory event
func (sc *StatsCollector) buildNodeMemoryEvent(ctx context.Context, summary *statsv1alpha1.Summary) *domain.CollectorEvent {
	traceID, spanID := sc.traceContextFunc(ctx)

	return &domain.CollectorEvent{
		EventID:   generateEventID("node_memory", sc.observerName),
		Timestamp: time.Now(),
		Source:    sc.observerName,
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
				"observer": sc.observerName,
				"version":  ObserverVersion,
			},
		},
	}
}

// processPodStats processes stats for all pods and returns events
func (sc *StatsCollector) processPodStats(ctx context.Context, pod *statsv1alpha1.PodStats) []domain.CollectorEvent {
	events := make([]domain.CollectorEvent, 0, len(pod.Containers)*2)

	for _, container := range pod.Containers {
		if container.CPU != nil && container.CPU.UsageNanoCores != nil {
			event := sc.buildCPUThrottlingEvent(ctx, pod, &container)
			events = append(events, *event)
		}

		if container.Memory != nil {
			event := sc.buildMemoryPressureEvent(ctx, pod, &container)
			if event != nil {
				events = append(events, *event)
			}
		}
	}

	if pod.EphemeralStorage != nil {
		event := sc.buildEphemeralStorageEvent(ctx, pod)
		if event != nil {
			events = append(events, *event)
		}
	}

	return events
}

// buildCPUThrottlingEvent creates a CPU throttling event
func (sc *StatsCollector) buildCPUThrottlingEvent(ctx context.Context, pod *statsv1alpha1.PodStats, container *statsv1alpha1.ContainerStats) *domain.CollectorEvent {
	traceID, spanID := sc.traceContextFunc(ctx)

	return &domain.CollectorEvent{
		EventID:   generateEventID("cpu_throttling", sc.observerName),
		Timestamp: time.Now(),
		Source:    sc.observerName,
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
				"observer": sc.observerName,
				"version":  ObserverVersion,
			},
		},
	}
}

// buildMemoryPressureEvent creates a memory pressure event
func (sc *StatsCollector) buildMemoryPressureEvent(ctx context.Context, pod *statsv1alpha1.PodStats, container *statsv1alpha1.ContainerStats) *domain.CollectorEvent {
	if container.Memory.WorkingSetBytes == nil || container.Memory.UsageBytes == nil {
		return nil
	}

	traceID, spanID := sc.traceContextFunc(ctx)

	containerMetrics := &domain.KubeletContainerMetrics{
		Namespace:        pod.PodRef.Namespace,
		Pod:              pod.PodRef.Name,
		Container:        container.Name,
		MemoryUsage:      *container.Memory.UsageBytes,
		MemoryWorkingSet: *container.Memory.WorkingSetBytes,
		Timestamp:        container.Memory.Time.Time,
	}

	if container.Memory.RSSBytes != nil {
		containerMetrics.MemoryRSS = *container.Memory.RSSBytes
	}

	return &domain.CollectorEvent{
		EventID:   generateEventID("memory_pressure", sc.observerName),
		Timestamp: time.Now(),
		Source:    sc.observerName,
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
				"observer": sc.observerName,
				"version":  ObserverVersion,
			},
		},
	}
}

// buildEphemeralStorageEvent creates an ephemeral storage event
func (sc *StatsCollector) buildEphemeralStorageEvent(ctx context.Context, pod *statsv1alpha1.PodStats) *domain.CollectorEvent {
	traceID, spanID := sc.traceContextFunc(ctx)

	if pod.EphemeralStorage.UsedBytes == nil || pod.EphemeralStorage.AvailableBytes == nil {
		return nil
	}

	usedBytes := *pod.EphemeralStorage.UsedBytes
	availableBytes := *pod.EphemeralStorage.AvailableBytes
	totalBytes := usedBytes + availableBytes

	usagePercent := 0.0
	if totalBytes > 0 {
		usagePercent = float64(usedBytes) / float64(totalBytes) * 100
	}

	return &domain.CollectorEvent{
		EventID:   generateEventID("ephemeral_storage", sc.observerName),
		Timestamp: time.Now(),
		Source:    sc.observerName,
		Type:      domain.EventTypeKubeletEphemeralStorage,
		Severity:  domain.EventSeverityInfo,
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
				"observer": sc.observerName,
				"version":  ObserverVersion,
			},
		},
	}
}
