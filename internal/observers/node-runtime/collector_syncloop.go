package noderuntime

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// SyncloopCollector monitors kubelet's syncloop health
type SyncloopCollector struct {
	*BaseCollector
	httpClient       *http.Client
	kubeletURL       string
	insecure         bool
	tracer           trace.Tracer
	apiLatency       metric.Float64Histogram
	traceContextFunc func(context.Context) (string, string)
}

// NewSyncloopCollector creates a new SyncloopCollector
func NewSyncloopCollector(
	observerName, kubeletURL string,
	insecure bool,
	httpClient *http.Client,
	tracer trace.Tracer,
	apiLatency metric.Float64Histogram,
	traceContextFunc func(context.Context) (string, string),
) *SyncloopCollector {
	return &SyncloopCollector{
		BaseCollector:    NewBaseCollector("syncloop", "/healthz/syncloop", observerName),
		httpClient:       httpClient,
		kubeletURL:       kubeletURL,
		insecure:         insecure,
		tracer:           tracer,
		apiLatency:       apiLatency,
		traceContextFunc: traceContextFunc,
	}
}

// Collect fetches syncloop health from kubelet
func (sc *SyncloopCollector) Collect(ctx context.Context) ([]domain.CollectorEvent, error) {
	ctx, span := sc.tracer.Start(ctx, "SyncloopCollector.Collect")
	defer span.End()

	start := time.Now()

	url := fmt.Sprintf("https://%s%s", sc.kubeletURL, sc.endpoint)
	if sc.insecure {
		url = fmt.Sprintf("http://%s%s", sc.kubeletURL, sc.endpoint)
	}

	span.SetAttributes(
		attribute.String("endpoint", sc.endpoint),
		attribute.String("url", url),
	)

	resp, err := sc.httpClient.Get(url)
	if err != nil {
		sc.SetHealthy(false)
		return nil, fmt.Errorf("failed to fetch syncloop health: %w", err)
	}
	defer resp.Body.Close()

	// Record API latency
	duration := time.Since(start)
	if sc.apiLatency != nil {
		sc.apiLatency.Record(ctx, duration.Seconds()*1000, metric.WithAttributes(
			attribute.String("endpoint", sc.endpoint),
		))
	}

	// Read response body for details
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		sc.SetHealthy(false)
		return nil, fmt.Errorf("failed to read syncloop response: %w", readErr)
	}

	responseText := strings.TrimSpace(string(body))

	span.SetAttributes(
		attribute.Float64("duration_seconds", duration.Seconds()),
		attribute.Int("status_code", resp.StatusCode),
		attribute.String("response", responseText),
	)

	events := make([]domain.CollectorEvent, 0)

	// syncloop is unhealthy if status is not 200
	prevHealthy := sc.IsHealthy()
	if resp.StatusCode != http.StatusOK {
		sc.SetHealthy(false)
		sc.lastUnhealthy = time.Now()

		// Only emit event on transition from healthy to unhealthy
		if prevHealthy {
			event := sc.buildSyncloopUnhealthyEvent(ctx, resp.StatusCode, responseText)
			events = append(events, *event)
		}
	} else {
		sc.SetHealthy(true)
	}

	return events, nil
}

// buildSyncloopUnhealthyEvent creates an event for syncloop unhealthy state
func (sc *SyncloopCollector) buildSyncloopUnhealthyEvent(
	ctx context.Context,
	statusCode int,
	message string,
) *domain.CollectorEvent {
	traceID, spanID := sc.traceContextFunc(ctx)

	return &domain.CollectorEvent{
		EventID:   generateEventID("syncloop_unhealthy", sc.observerName),
		Timestamp: time.Now(),
		Source:    sc.observerName,
		Type:      domain.EventTypeKubeletSyncloopUnhealthy,
		Severity:  domain.EventSeverityCritical, // Syncloop failures are critical
		EventData: domain.EventDataContainer{
			Kubelet: &domain.KubeletData{
				EventType: "syncloop_unhealthy",
				SyncloopHealth: &domain.KubeletSyncloopHealth{
					Healthy:    false,
					StatusCode: statusCode,
					Message:    message,
					Timestamp:  time.Now(),
				},
			},
		},
		Metadata: domain.EventMetadata{
			TraceID: traceID,
			SpanID:  spanID,
			Labels: map[string]string{
				"observer": sc.observerName,
				"version":  "1.0.0",
			},
		},
	}
}
