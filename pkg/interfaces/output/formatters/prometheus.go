package formatters

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// PrometheusFormatter converts universal metrics to Prometheus format
type PrometheusFormatter struct {
	namespace  string
	subsystem  string
	registry   *prometheus.Registry
	gauges     map[string]*prometheus.GaugeVec
	counters   map[string]*prometheus.CounterVec
	histograms map[string]*prometheus.HistogramVec
}

// NewPrometheusFormatter creates a new Prometheus formatter
func NewPrometheusFormatter(namespace, subsystem string, registry *prometheus.Registry) *PrometheusFormatter {
	if registry == nil {
		registry = prometheus.NewRegistry()
	}

	return &PrometheusFormatter{
		namespace:  namespace,
		subsystem:  subsystem,
		registry:   registry,
		gauges:     make(map[string]*prometheus.GaugeVec),
		counters:   make(map[string]*prometheus.CounterVec),
		histograms: make(map[string]*prometheus.HistogramVec),
	}
}

// FormatMetric converts a UniversalMetric to Prometheus metric
func (f *PrometheusFormatter) FormatMetric(metric *universal.UniversalMetric) error {
	if metric == nil {
		return fmt.Errorf("nil metric provided")
	}

	// Build metric name
	metricName := f.buildMetricName(metric.Name)

	// Build labels
	labels := f.buildLabels(metric)
	labelNames := f.getLabelNames(labels)
	labelValues := f.getLabelValues(labels, labelNames)

	switch metric.Type {
	case universal.MetricTypeGauge:
		gauge := f.getOrCreateGauge(metricName, metric.Unit, labelNames)
		gauge.WithLabelValues(labelValues...).Set(metric.Value)

	case universal.MetricTypeCounter:
		counter := f.getOrCreateCounter(metricName, metric.Unit, labelNames)
		counter.WithLabelValues(labelValues...).Add(metric.Value)

	case universal.MetricTypeHistogram:
		histogram := f.getOrCreateHistogram(metricName, metric.Unit, labelNames)
		histogram.WithLabelValues(labelValues...).Observe(metric.Value)

	default:
		// Default to gauge for unknown types
		gauge := f.getOrCreateGauge(metricName, metric.Unit, labelNames)
		gauge.WithLabelValues(labelValues...).Set(metric.Value)
	}

	return nil
}

// FormatEvent converts a UniversalEvent to Prometheus metrics
func (f *PrometheusFormatter) FormatEvent(event *universal.UniversalEvent) error {
	if event == nil {
		return fmt.Errorf("nil event provided")
	}

	// Create event counter metric
	eventMetric := universal.GetMetric()
	defer universal.PutMetric(eventMetric)

	eventMetric.Name = fmt.Sprintf("events_%s_total", strings.ToLower(string(event.Type)))
	eventMetric.Type = universal.MetricTypeCounter
	eventMetric.Value = 1
	eventMetric.Target = event.Target
	eventMetric.Labels = map[string]string{
		"level": string(event.Level),
		"type":  string(event.Type),
	}

	return f.FormatMetric(eventMetric)
}

// FormatPrediction converts a UniversalPrediction to Prometheus metrics
func (f *PrometheusFormatter) FormatPrediction(prediction *universal.UniversalPrediction) error {
	if prediction == nil {
		return fmt.Errorf("nil prediction provided")
	}

	// Create prediction gauge metrics
	metrics := []*universal.UniversalMetric{
		{
			Name:   fmt.Sprintf("prediction_%s_probability", strings.ToLower(string(prediction.Type))),
			Type:   universal.MetricTypeGauge,
			Value:  prediction.Probability,
			Target: prediction.Target,
			Labels: map[string]string{
				"impact": string(prediction.Impact),
			},
		},
		{
			Name:   fmt.Sprintf("prediction_%s_time_to_event_seconds", strings.ToLower(string(prediction.Type))),
			Type:   universal.MetricTypeGauge,
			Value:  prediction.TimeToEvent.Seconds(),
			Target: prediction.Target,
			Labels: map[string]string{
				"impact": string(prediction.Impact),
			},
		},
	}

	for _, metric := range metrics {
		if err := f.FormatMetric(metric); err != nil {
			return err
		}
	}

	return nil
}

// BatchFormat processes multiple universal data items
func (f *PrometheusFormatter) BatchFormat(items []interface{}) error {
	for _, item := range items {
		switch v := item.(type) {
		case *universal.UniversalMetric:
			if err := f.FormatMetric(v); err != nil {
				return err
			}
		case *universal.UniversalEvent:
			if err := f.FormatEvent(v); err != nil {
				return err
			}
		case *universal.UniversalPrediction:
			if err := f.FormatPrediction(v); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported type: %T", v)
		}
	}
	return nil
}

// GetRegistry returns the Prometheus registry
func (f *PrometheusFormatter) GetRegistry() *prometheus.Registry {
	return f.registry
}

// buildMetricName constructs the full metric name
func (f *PrometheusFormatter) buildMetricName(name string) string {
	parts := []string{}
	if f.namespace != "" {
		parts = append(parts, f.namespace)
	}
	if f.subsystem != "" {
		parts = append(parts, f.subsystem)
	}
	parts = append(parts, name)

	return strings.Join(parts, "_")
}

// buildLabels constructs labels from metric and target
func (f *PrometheusFormatter) buildLabels(metric *universal.UniversalMetric) map[string]string {
	labels := make(map[string]string)

	// Add target labels
	switch metric.Target.Type {
	case universal.TargetTypePod:
		labels["pod"] = metric.Target.Name
		labels["namespace"] = metric.Target.Namespace
	case universal.TargetTypeContainer:
		labels["container"] = metric.Target.Container
		labels["pod"] = metric.Target.Name
		labels["namespace"] = metric.Target.Namespace
	case universal.TargetTypeNode:
		labels["node"] = metric.Target.Name
	case universal.TargetTypeProcess:
		labels["process"] = metric.Target.Name
		if metric.Target.PID > 0 {
			labels["pid"] = fmt.Sprintf("%d", metric.Target.PID)
		}
	}

	// Add quality labels if low confidence
	if metric.Quality.Confidence < 0.8 {
		labels["quality"] = "degraded"
	}

	// Add custom labels
	for k, v := range metric.Labels {
		labels[k] = v
	}

	return labels
}

// getLabelNames extracts sorted label names
func (f *PrometheusFormatter) getLabelNames(labels map[string]string) []string {
	names := make([]string, 0, len(labels))
	for name := range labels {
		names = append(names, name)
	}
	return names
}

// getLabelValues extracts label values in the same order as names
func (f *PrometheusFormatter) getLabelValues(labels map[string]string, names []string) []string {
	values := make([]string, len(names))
	for i, name := range names {
		values[i] = labels[name]
	}
	return values
}

// getOrCreateGauge gets or creates a gauge vector
func (f *PrometheusFormatter) getOrCreateGauge(name, help string, labelNames []string) *prometheus.GaugeVec {
	key := name + strings.Join(labelNames, ",")
	if gauge, exists := f.gauges[key]; exists {
		return gauge
	}

	gauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: name,
			Help: help,
		},
		labelNames,
	)

	f.registry.MustRegister(gauge)
	f.gauges[key] = gauge
	return gauge
}

// getOrCreateCounter gets or creates a counter vector
func (f *PrometheusFormatter) getOrCreateCounter(name, help string, labelNames []string) *prometheus.CounterVec {
	key := name + strings.Join(labelNames, ",")
	if counter, exists := f.counters[key]; exists {
		return counter
	}

	counter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: name,
			Help: help,
		},
		labelNames,
	)

	f.registry.MustRegister(counter)
	f.counters[key] = counter
	return counter
}

// getOrCreateHistogram gets or creates a histogram vector
func (f *PrometheusFormatter) getOrCreateHistogram(name, help string, labelNames []string) *prometheus.HistogramVec {
	key := name + strings.Join(labelNames, ",")
	if histogram, exists := f.histograms[key]; exists {
		return histogram
	}

	histogram := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    name,
			Help:    help,
			Buckets: prometheus.DefBuckets,
		},
		labelNames,
	)

	f.registry.MustRegister(histogram)
	f.histograms[key] = histogram
	return histogram
}

// MetricsHandler creates an HTTP handler for Prometheus metrics
func (f *PrometheusFormatter) MetricsHandler() http.Handler {
	return promhttp.HandlerFor(f.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
		Timeout:           10 * time.Second,
	})
}
