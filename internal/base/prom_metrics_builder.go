//go:build linux

package base

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// PromMetricBuilder provides fluent API for creating observer-specific Prometheus metrics.
// Replaces MetricBuilder (OTEL SDK) with native Prometheus.
type PromMetricBuilder struct {
	reg          prometheus.Registerer
	observerName string
	err          error
}

// NewPromMetricBuilder creates a builder for observer-specific metrics
func NewPromMetricBuilder(reg prometheus.Registerer, observerName string) *PromMetricBuilder {
	return &PromMetricBuilder{
		reg:          reg,
		observerName: observerName,
	}
}

// Counter creates a counter metric
func (b *PromMetricBuilder) Counter(target **prometheus.Counter, name, help string) *PromMetricBuilder {
	if b.err != nil {
		return b
	}
	fullName := "tapio_" + b.observerName + "_" + name
	counter := promauto.With(b.reg).NewCounter(prometheus.CounterOpts{
		Name: fullName,
		Help: help,
	})
	*target = &counter
	return b
}

// CounterVec creates a counter vector metric
func (b *PromMetricBuilder) CounterVec(target **prometheus.CounterVec, name, help string, labels []string) *PromMetricBuilder {
	if b.err != nil {
		return b
	}
	fullName := "tapio_" + b.observerName + "_" + name
	*target = promauto.With(b.reg).NewCounterVec(prometheus.CounterOpts{
		Name: fullName,
		Help: help,
	}, labels)
	return b
}

// Gauge creates a gauge metric
func (b *PromMetricBuilder) Gauge(target **prometheus.Gauge, name, help string) *PromMetricBuilder {
	if b.err != nil {
		return b
	}
	fullName := "tapio_" + b.observerName + "_" + name
	gauge := promauto.With(b.reg).NewGauge(prometheus.GaugeOpts{
		Name: fullName,
		Help: help,
	})
	*target = &gauge
	return b
}

// GaugeVec creates a gauge vector metric
func (b *PromMetricBuilder) GaugeVec(target **prometheus.GaugeVec, name, help string, labels []string) *PromMetricBuilder {
	if b.err != nil {
		return b
	}
	fullName := "tapio_" + b.observerName + "_" + name
	*target = promauto.With(b.reg).NewGaugeVec(prometheus.GaugeOpts{
		Name: fullName,
		Help: help,
	}, labels)
	return b
}

// Histogram creates a histogram metric
func (b *PromMetricBuilder) Histogram(target **prometheus.Histogram, name, help string, buckets []float64) *PromMetricBuilder {
	if b.err != nil {
		return b
	}
	fullName := "tapio_" + b.observerName + "_" + name
	histogram := promauto.With(b.reg).NewHistogram(prometheus.HistogramOpts{
		Name:    fullName,
		Help:    help,
		Buckets: buckets,
	})
	*target = &histogram
	return b
}

// Build returns any error that occurred during metric creation
func (b *PromMetricBuilder) Build() error {
	return b.err
}
