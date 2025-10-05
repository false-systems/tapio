package noderuntime

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// ProbesCollector collects probe metrics from kubelet /metrics/probes endpoint
type ProbesCollector struct {
	*BaseCollector
	httpClient       *http.Client
	kubeletURL       string
	insecure         bool
	tracer           trace.Tracer
	apiLatency       metric.Float64Histogram
	traceContextFunc func(context.Context) (string, string)
}

// ProbeMetric represents a parsed probe metric
type ProbeMetric struct {
	ProbeType   string // "Liveness", "Readiness", "Startup"
	Container   string
	Pod         string
	Namespace   string
	PodUID      string
	Result      string // "successful", "failed", "unknown"
	DurationSec float64
}

// NewProbesCollector creates a new ProbesCollector
func NewProbesCollector(
	observerName, kubeletURL string,
	insecure bool,
	httpClient *http.Client,
	tracer trace.Tracer,
	apiLatency metric.Float64Histogram,
	traceContextFunc func(context.Context) (string, string),
) *ProbesCollector {
	return &ProbesCollector{
		BaseCollector:    NewBaseCollector("probes", "/metrics/probes", observerName),
		httpClient:       httpClient,
		kubeletURL:       kubeletURL,
		insecure:         insecure,
		tracer:           tracer,
		apiLatency:       apiLatency,
		traceContextFunc: traceContextFunc,
	}
}

// Collect fetches probe metrics from kubelet and returns domain events
func (pc *ProbesCollector) Collect(ctx context.Context) ([]domain.CollectorEvent, error) {
	ctx, span := pc.tracer.Start(ctx, "ProbesCollector.Collect")
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
		return nil, fmt.Errorf("failed to fetch probes metrics: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		pc.SetHealthy(false)
		return nil, fmt.Errorf("kubelet returned status %d for probes", resp.StatusCode)
	}

	pc.SetHealthy(true)

	duration := time.Since(start)
	if pc.apiLatency != nil {
		pc.apiLatency.Record(ctx, duration.Seconds()*1000, metric.WithAttributes(
			attribute.String("endpoint", pc.endpoint),
		))
	}

	// Parse Prometheus metrics
	probeMetrics, err := pc.parsePrometheusMetrics(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse probe metrics: %w", err)
	}

	span.SetAttributes(
		attribute.Float64("duration_seconds", duration.Seconds()),
		attribute.Int("probe_metrics_count", len(probeMetrics)),
	)

	// Convert to domain events (only report failures and slow probes)
	events := make([]domain.CollectorEvent, 0)
	for _, pm := range probeMetrics {
		// Report failures
		if pm.Result == "failed" {
			event := pc.buildProbeFailureEvent(ctx, &pm)
			events = append(events, *event)
		}
		// Report slow probes (>1 second)
		if pm.DurationSec > 1.0 {
			event := pc.buildSlowProbeEvent(ctx, &pm)
			events = append(events, *event)
		}
	}

	return events, nil
}

// parsePrometheusMetrics parses Prometheus text format metrics
func (pc *ProbesCollector) parsePrometheusMetrics(r io.Reader) ([]ProbeMetric, error) {
	scanner := bufio.NewScanner(r)
	metrics := make([]ProbeMetric, 0)
	currentMetric := make(map[string]ProbeMetric) // Key: namespace/pod/container/probe_type

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse metric line: metric_name{labels} value timestamp
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		metricName, labelsAndValue := parts[0], strings.Join(parts[1:], " ")

		// We care about prober_probe_total (results) and prober_probe_duration_seconds (latency)
		if !strings.HasPrefix(metricName, "prober_probe_") {
			continue
		}

		// Extract labels
		labels := pc.extractLabels(metricName)
		if labels == nil {
			continue
		}

		key := fmt.Sprintf("%s/%s/%s/%s", labels["namespace"], labels["pod"], labels["container"], labels["probe_type"])

		// Get or create metric entry
		pm, exists := currentMetric[key]
		if !exists {
			pm = ProbeMetric{
				ProbeType: labels["probe_type"],
				Container: labels["container"],
				Pod:       labels["pod"],
				Namespace: labels["namespace"],
				PodUID:    labels["pod_uid"],
			}
		}

		// Parse value
		valueStr := strings.TrimSpace(labelsAndValue)
		value, err := strconv.ParseFloat(valueStr, 64)
		if err != nil {
			continue
		}

		// Store based on metric type
		if strings.Contains(metricName, "duration_seconds") {
			pm.DurationSec = value
		} else if strings.Contains(metricName, "total") {
			// For total, extract result from labels
			if result, ok := labels["result"]; ok {
				pm.Result = result
			} else {
				pm.Result = "unknown"
			}
		}

		currentMetric[key] = pm
	}

	// Convert map to slice
	for _, pm := range currentMetric {
		metrics = append(metrics, pm)
	}

	return metrics, scanner.Err()
}

// extractLabels extracts label key-value pairs from Prometheus metric line
func (pc *ProbesCollector) extractLabels(metricLine string) map[string]string {
	labels := make(map[string]string)

	// Find labels between { and }
	start := strings.Index(metricLine, "{")
	end := strings.LastIndex(metricLine, "}")
	if start == -1 || end == -1 {
		return nil
	}

	labelsStr := metricLine[start+1 : end]
	labelPairs := strings.Split(labelsStr, ",")

	for _, pair := range labelPairs {
		kv := strings.Split(pair, "=")
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		value := strings.Trim(strings.TrimSpace(kv[1]), "\"")
		labels[key] = value
	}

	return labels
}

// buildProbeFailureEvent creates an event for probe failures
func (pc *ProbesCollector) buildProbeFailureEvent(ctx context.Context, pm *ProbeMetric) *domain.CollectorEvent {
	traceID, spanID := pc.traceContextFunc(ctx)

	severity := domain.EventSeverityWarning
	if pm.ProbeType == "Liveness" {
		severity = domain.EventSeverityCritical // Liveness failures are critical
	}

	return &domain.CollectorEvent{
		EventID:   generateEventID("probe_failure", pc.observerName),
		Timestamp: time.Now(),
		Source:    pc.observerName,
		Type:      domain.EventTypeKubeletProbeFailure,
		Severity:  severity,
		EventData: domain.EventDataContainer{
			Kubelet: &domain.KubeletData{
				EventType: "probe_failure",
				ProbeResult: &domain.KubeletProbeResult{
					Namespace:   pm.Namespace,
					Pod:         pm.Pod,
					Container:   pm.Container,
					ProbeType:   pm.ProbeType,
					Result:      pm.Result,
					DurationSec: pm.DurationSec,
					Timestamp:   time.Now(),
				},
			},
		},
		Metadata: domain.EventMetadata{
			TraceID: traceID,
			SpanID:  spanID,
			Labels: map[string]string{
				"observer":   pc.observerName,
				"version":    "1.0.0",
				"probe_type": pm.ProbeType,
			},
		},
	}
}

// buildSlowProbeEvent creates an event for slow probes
func (pc *ProbesCollector) buildSlowProbeEvent(ctx context.Context, pm *ProbeMetric) *domain.CollectorEvent {
	traceID, spanID := pc.traceContextFunc(ctx)

	return &domain.CollectorEvent{
		EventID:   generateEventID("probe_slow", pc.observerName),
		Timestamp: time.Now(),
		Source:    pc.observerName,
		Type:      domain.EventTypeKubeletProbeSlow,
		Severity:  domain.EventSeverityWarning,
		EventData: domain.EventDataContainer{
			Kubelet: &domain.KubeletData{
				EventType: "probe_slow",
				ProbeResult: &domain.KubeletProbeResult{
					Namespace:   pm.Namespace,
					Pod:         pm.Pod,
					Container:   pm.Container,
					ProbeType:   pm.ProbeType,
					Result:      pm.Result,
					DurationSec: pm.DurationSec,
					Timestamp:   time.Now(),
				},
			},
		},
		Metadata: domain.EventMetadata{
			TraceID: traceID,
			SpanID:  spanID,
			Labels: map[string]string{
				"observer":   pc.observerName,
				"version":    "1.0.0",
				"probe_type": pm.ProbeType,
			},
		},
	}
}
