package intelligence

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds Prometheus metrics for the intelligence service.
type Metrics struct {
	EventsProcessed    *prometheus.CounterVec
	Errors             *prometheus.CounterVec
	ProcessingDuration *prometheus.HistogramVec
}

// NewMetrics creates and registers intelligence service metrics.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		EventsProcessed: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "intelligence_events_processed_total",
				Help: "Total number of events processed by intelligence service",
			},
			[]string{"tier", "event_type"},
		),
		Errors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "intelligence_errors_total",
				Help: "Total number of errors in intelligence service",
			},
			[]string{"tier", "event_type", "error_type"},
		),
		ProcessingDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "intelligence_processing_duration_seconds",
				Help:    "Time spent processing events",
				Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
			},
			[]string{"tier", "event_type"},
		),
	}

	reg.MustRegister(m.EventsProcessed, m.Errors, m.ProcessingDuration)

	return m
}

// RecordEvent increments the events processed counter.
func (m *Metrics) RecordEvent(tier, eventType string) {
	m.EventsProcessed.WithLabelValues(tier, eventType).Inc()
}

// RecordError increments the error counter.
func (m *Metrics) RecordError(tier, eventType, errorType string) {
	m.Errors.WithLabelValues(tier, eventType, errorType).Inc()
}

// RecordDuration records event processing duration.
func (m *Metrics) RecordDuration(tier, eventType string, seconds float64) {
	m.ProcessingDuration.WithLabelValues(tier, eventType).Observe(seconds)
}
