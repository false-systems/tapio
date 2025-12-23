//go:build linux

package base

import (
	"errors"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// metricCache caches registered metrics to avoid duplicate registration errors.
// Key is the full metric name, value is the Collector.
var (
	metricCache   = make(map[string]prometheus.Collector)
	metricCacheMu sync.Mutex
)

// PromMetricBuilder provides fluent API for creating observer-specific Prometheus metrics.
// Replaces MetricBuilder (OTEL SDK) with native Prometheus.
// Handles duplicate registration gracefully by reusing existing metrics.
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

// registerOrGet registers a collector or returns existing one if already registered
func registerOrGet[T prometheus.Collector](reg prometheus.Registerer, fullName string, collector T) (T, error) {
	metricCacheMu.Lock()
	defer metricCacheMu.Unlock()

	// Check cache first
	if existing, ok := metricCache[fullName]; ok {
		if typed, ok := existing.(T); ok {
			return typed, nil
		}
	}

	// Try to register
	if err := reg.Register(collector); err != nil {
		var alreadyRegErr prometheus.AlreadyRegisteredError
		if errors.As(err, &alreadyRegErr) {
			// Use the existing collector
			if typed, ok := alreadyRegErr.ExistingCollector.(T); ok {
				metricCache[fullName] = alreadyRegErr.ExistingCollector
				return typed, nil
			}
		}
		var zero T
		return zero, err
	}

	metricCache[fullName] = collector
	return collector, nil
}

// Counter creates a counter metric
func (b *PromMetricBuilder) Counter(target **prometheus.Counter, name, help string) *PromMetricBuilder {
	if b.err != nil {
		return b
	}
	fullName := "tapio_" + b.observerName + "_" + name
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: fullName,
		Help: help,
	})
	registered, err := registerOrGet(b.reg, fullName, counter)
	if err != nil {
		b.err = err
		return b
	}
	*target = &registered
	return b
}

// CounterVec creates a counter vector metric
func (b *PromMetricBuilder) CounterVec(target **prometheus.CounterVec, name, help string, labels []string) *PromMetricBuilder {
	if b.err != nil {
		return b
	}
	fullName := "tapio_" + b.observerName + "_" + name
	counterVec := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: fullName,
		Help: help,
	}, labels)
	registered, err := registerOrGet(b.reg, fullName, counterVec)
	if err != nil {
		b.err = err
		return b
	}
	*target = registered
	return b
}

// Gauge creates a gauge metric
func (b *PromMetricBuilder) Gauge(target **prometheus.Gauge, name, help string) *PromMetricBuilder {
	if b.err != nil {
		return b
	}
	fullName := "tapio_" + b.observerName + "_" + name
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: fullName,
		Help: help,
	})
	registered, err := registerOrGet(b.reg, fullName, gauge)
	if err != nil {
		b.err = err
		return b
	}
	*target = &registered
	return b
}

// GaugeVec creates a gauge vector metric
func (b *PromMetricBuilder) GaugeVec(target **prometheus.GaugeVec, name, help string, labels []string) *PromMetricBuilder {
	if b.err != nil {
		return b
	}
	fullName := "tapio_" + b.observerName + "_" + name
	gaugeVec := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: fullName,
		Help: help,
	}, labels)
	registered, err := registerOrGet(b.reg, fullName, gaugeVec)
	if err != nil {
		b.err = err
		return b
	}
	*target = registered
	return b
}

// Histogram creates a histogram metric
func (b *PromMetricBuilder) Histogram(target **prometheus.Histogram, name, help string, buckets []float64) *PromMetricBuilder {
	if b.err != nil {
		return b
	}
	fullName := "tapio_" + b.observerName + "_" + name
	histogram := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    fullName,
		Help:    help,
		Buckets: buckets,
	})
	registered, err := registerOrGet(b.reg, fullName, histogram)
	if err != nil {
		b.err = err
		return b
	}
	*target = &registered
	return b
}

// Build returns any error that occurred during metric creation
func (b *PromMetricBuilder) Build() error {
	return b.err
}
