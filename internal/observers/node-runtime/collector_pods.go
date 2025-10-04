package noderuntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	v1 "k8s.io/api/core/v1"
)

// PodsCollector collects pod lifecycle data from kubelet /pods endpoint
type PodsCollector struct {
	*BaseCollector
	httpClient       *http.Client
	kubeletURL       string
	insecure         bool
	tracer           trace.Tracer
	apiLatency       metric.Float64Histogram
	traceContextFunc func(context.Context) (string, string)
}

// NewPodsCollector creates a new PodsCollector
func NewPodsCollector(
	observerName, kubeletURL string,
	insecure bool,
	httpClient *http.Client,
	tracer trace.Tracer,
	apiLatency metric.Float64Histogram,
	traceContextFunc func(context.Context) (string, string),
) *PodsCollector {
	return &PodsCollector{
		BaseCollector:    NewBaseCollector("pods", "/pods", observerName),
		httpClient:       httpClient,
		kubeletURL:       kubeletURL,
		insecure:         insecure,
		tracer:           tracer,
		apiLatency:       apiLatency,
		traceContextFunc: traceContextFunc,
	}
}

// Collect fetches pod data from kubelet and returns domain events
func (pc *PodsCollector) Collect(ctx context.Context) ([]domain.CollectorEvent, error) {
	ctx, span := pc.tracer.Start(ctx, "PodsCollector.Collect")
	defer span.End()

	start := time.Now()

	url := fmt.Sprintf("https://%s%s", pc.kubeletURL, pc.endpoint)
	if pc.insecure {
		url = fmt.Sprintf("http://%s%s", pc.kubeletURL, pc.endpoint)
	}

	span.SetAttributes(
		attribute.String("endpoint", pc.endpoint),
		attribute.String("url", url),
	)

	resp, err := pc.httpClient.Get(url)
	if err != nil {
		pc.SetHealthy(false)
		return nil, fmt.Errorf("failed to fetch pods: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		pc.SetHealthy(false)
		return nil, fmt.Errorf("kubelet returned status %d", resp.StatusCode)
	}

	var podList v1.PodList
	if err := json.NewDecoder(resp.Body).Decode(&podList); err != nil {
		pc.SetHealthy(false)
		return nil, fmt.Errorf("failed to decode pods: %w", err)
	}

	pc.SetHealthy(true)

	duration := time.Since(start)
	if pc.apiLatency != nil {
		pc.apiLatency.Record(ctx, duration.Seconds()*1000, metric.WithAttributes(
			attribute.String("endpoint", pc.endpoint),
		))
	}

	span.SetAttributes(
		attribute.Float64("duration_seconds", duration.Seconds()),
		attribute.Int("pods_count", len(podList.Items)),
	)

	events := make([]domain.CollectorEvent, 0, len(podList.Items))
	for _, pod := range podList.Items {
		podEvents := pc.processPodStatus(ctx, &pod)
		events = append(events, podEvents...)
	}

	return events, nil
}

// processPodStatus checks for pod issues and returns events
func (pc *PodsCollector) processPodStatus(ctx context.Context, pod *v1.Pod) []domain.CollectorEvent {
	events := make([]domain.CollectorEvent, 0)

	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Waiting != nil {
			event := pc.buildContainerWaitingEvent(ctx, pod, &status)
			events = append(events, *event)
		}

		if status.State.Terminated != nil && status.State.Terminated.ExitCode != 0 {
			event := pc.buildContainerTerminatedEvent(ctx, pod, &status)
			events = append(events, *event)
		}

		if status.LastTerminationState.Terminated != nil && status.RestartCount > 3 {
			event := pc.buildCrashLoopEvent(ctx, pod, &status)
			events = append(events, *event)
		}
	}

	for _, condition := range pod.Status.Conditions {
		if condition.Type == v1.PodReady && condition.Status != v1.ConditionTrue {
			event := pc.buildPodNotReadyEvent(ctx, pod, &condition)
			events = append(events, *event)
		}
	}

	return events
}

// buildContainerWaitingEvent creates event for waiting containers
func (pc *PodsCollector) buildContainerWaitingEvent(ctx context.Context, pod *v1.Pod, status *v1.ContainerStatus) *domain.CollectorEvent {
	traceID, spanID := pc.traceContextFunc(ctx)

	return &domain.CollectorEvent{
		EventID:   generateEventID("container_waiting", pc.observerName),
		Timestamp: time.Now(),
		Source:    pc.observerName,
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
				"observer": pc.observerName,
				"version":  "1.0.0",
			},
		},
	}
}

// buildContainerTerminatedEvent creates event for terminated containers
func (pc *PodsCollector) buildContainerTerminatedEvent(ctx context.Context, pod *v1.Pod, status *v1.ContainerStatus) *domain.CollectorEvent {
	traceID, spanID := pc.traceContextFunc(ctx)

	return &domain.CollectorEvent{
		EventID:   generateEventID("container_terminated", pc.observerName),
		Timestamp: time.Now(),
		Source:    pc.observerName,
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
				"observer": pc.observerName,
				"version":  "1.0.0",
			},
		},
	}
}

// buildCrashLoopEvent creates event for crash loop detection
func (pc *PodsCollector) buildCrashLoopEvent(ctx context.Context, pod *v1.Pod, status *v1.ContainerStatus) *domain.CollectorEvent {
	traceID, spanID := pc.traceContextFunc(ctx)

	return &domain.CollectorEvent{
		EventID:   generateEventID("crash_loop", pc.observerName),
		Timestamp: time.Now(),
		Source:    pc.observerName,
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
				"observer": pc.observerName,
				"version":  "1.0.0",
			},
		},
	}
}

// buildPodNotReadyEvent creates event for pods not ready
func (pc *PodsCollector) buildPodNotReadyEvent(ctx context.Context, pod *v1.Pod, condition *v1.PodCondition) *domain.CollectorEvent {
	traceID, spanID := pc.traceContextFunc(ctx)

	return &domain.CollectorEvent{
		EventID:   generateEventID("pod_not_ready", pc.observerName),
		Timestamp: time.Now(),
		Source:    pc.observerName,
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
				"observer": pc.observerName,
				"version":  "1.0.0",
			},
		},
	}
}
