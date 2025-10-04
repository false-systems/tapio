package noderuntime

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// HealthzCollector monitors kubelet health via /healthz endpoint
type HealthzCollector struct {
	*BaseCollector
	httpClient       *http.Client
	kubeletURL       string
	insecure         bool
	tracer           trace.Tracer
	eventsProcessed  metric.Int64Counter
	apiLatency       metric.Float64Histogram
	eventChan        chan<- *domain.CollectorEvent
	recordEventFunc  func()
	recordErrorFunc  func(error)
	traceContextFunc func(context.Context) (string, string)
}

// NewHealthzCollector creates a new HealthzCollector
func NewHealthzCollector(
	observerName, kubeletURL string,
	insecure bool,
	httpClient *http.Client,
	tracer trace.Tracer,
	eventsProcessed metric.Int64Counter,
	apiLatency metric.Float64Histogram,
	eventChan chan<- *domain.CollectorEvent,
	recordEventFunc func(),
	recordErrorFunc func(error),
	traceContextFunc func(context.Context) (string, string),
) *HealthzCollector {
	return &HealthzCollector{
		BaseCollector:    NewBaseCollector("healthz", "/healthz", observerName),
		httpClient:       httpClient,
		kubeletURL:       kubeletURL,
		insecure:         insecure,
		tracer:           tracer,
		eventsProcessed:  eventsProcessed,
		apiLatency:       apiLatency,
		eventChan:        eventChan,
		recordEventFunc:  recordEventFunc,
		recordErrorFunc:  recordErrorFunc,
		traceContextFunc: traceContextFunc,
	}
}

// Collect checks kubelet health and returns events if unhealthy
func (hc *HealthzCollector) Collect(ctx context.Context) ([]domain.CollectorEvent, error) {
	ctx, span := hc.tracer.Start(ctx, "HealthzCollector.Collect")
	defer span.End()

	start := time.Now()

	url := fmt.Sprintf("https://%s%s", hc.kubeletURL, hc.endpoint)
	if hc.insecure {
		url = fmt.Sprintf("http://%s%s", hc.kubeletURL, hc.endpoint)
	}

	span.SetAttributes(
		attribute.String("endpoint", hc.endpoint),
		attribute.String("url", url),
	)

	resp, err := hc.httpClient.Get(url)
	if err != nil {
		hc.SetHealthy(false)
		return nil, fmt.Errorf("failed to check kubelet health: %w", err)
	}
	defer resp.Body.Close()

	duration := time.Since(start)
	if hc.apiLatency != nil {
		hc.apiLatency.Record(ctx, duration.Seconds()*1000, metric.WithAttributes(
			attribute.String("endpoint", hc.endpoint),
		))
	}

	span.SetAttributes(
		attribute.Float64("duration_seconds", duration.Seconds()),
		attribute.Int("status_code", resp.StatusCode),
	)

	if resp.StatusCode != http.StatusOK {
		hc.SetHealthy(false)
		return nil, fmt.Errorf("kubelet health check failed: status %d", resp.StatusCode)
	}

	hc.SetHealthy(true)
	return nil, nil
}
