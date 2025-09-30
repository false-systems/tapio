package base

import (
	"context"
	"fmt"
	"sync"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// OTELEmitter emits domain-specific metrics to OTEL
// This is separate from the base observer's meta-metrics
type OTELEmitter struct {
	logger *zap.Logger
	meter  metric.Meter
	cache  *domainMetricsCache
	mu     sync.RWMutex
}

// NewOTELEmitter creates a new OTEL domain metrics emitter
func NewOTELEmitter(logger *zap.Logger, meterName string) (*OTELEmitter, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}
	if meterName == "" {
		return nil, fmt.Errorf("meter name is required")
	}

	// Create a separate meter for domain metrics
	// This keeps domain metrics separate from observer meta-metrics
	domainMeterName := fmt.Sprintf("%s.domain", meterName)
	meter := otel.Meter(domainMeterName)

	return &OTELEmitter{
		logger: logger,
		meter:  meter,
		cache:  newDomainMetricsCache(),
	}, nil
}

// EmitEvent emits a CollectorEvent to OTEL (not implemented for OTEL emitter)
// OTEL emitter focuses on domain metrics, not raw events
func (e *OTELEmitter) EmitEvent(ctx context.Context, event *domain.CollectorEvent) error {
	// OTEL emitter doesn't emit raw events, only domain metrics
	// This method exists to satisfy the OutputEmitter interface
	return nil
}

// EmitDomainMetric emits a domain-specific counter metric to OTEL
func (e *OTELEmitter) EmitDomainMetric(ctx context.Context, dm DomainMetric) error {
	if dm.Name == "" {
		return fmt.Errorf("metric name is required")
	}

	// Get or create counter from cache
	counter, err := e.getOrCreateCounter(dm.Name)
	if err != nil {
		e.logger.Warn("Failed to get/create counter",
			zap.String("metric", dm.Name),
			zap.Error(err))
		return fmt.Errorf("failed to get counter %s: %w", dm.Name, err)
	}

	// Add the metric value with attributes
	counter.Add(ctx, dm.Value, metric.WithAttributes(dm.Attributes...))

	return nil
}

// EmitDomainGauge emits a domain-specific gauge metric to OTEL
func (e *OTELEmitter) EmitDomainGauge(ctx context.Context, dg DomainGauge) error {
	if dg.Name == "" {
		return fmt.Errorf("gauge name is required")
	}

	// Get or create gauge from cache
	gauge, err := e.getOrCreateGauge(dg.Name)
	if err != nil {
		e.logger.Warn("Failed to get/create gauge",
			zap.String("gauge", dg.Name),
			zap.Error(err))
		return fmt.Errorf("failed to get gauge %s: %w", dg.Name, err)
	}

	// Record the gauge value with attributes
	gauge.Record(ctx, dg.Value, metric.WithAttributes(dg.Attributes...))

	return nil
}

// Close closes the emitter and releases resources
func (e *OTELEmitter) Close() error {
	// OTEL meters don't need explicit cleanup
	return nil
}

// getOrCreateCounter gets or creates a counter metric
func (e *OTELEmitter) getOrCreateCounter(name string) (metric.Int64Counter, error) {
	// Fast path: check cache with read lock
	e.mu.RLock()
	if counter, exists := e.cache.counters[name]; exists {
		e.mu.RUnlock()
		return counter, nil
	}
	e.mu.RUnlock()

	// Slow path: create counter with write lock
	e.mu.Lock()
	defer e.mu.Unlock()

	// Double-check after acquiring write lock (another goroutine might have created it)
	if counter, exists := e.cache.counters[name]; exists {
		return counter, nil
	}

	// Create new counter
	counter, err := e.meter.Int64Counter(
		name,
		metric.WithDescription(fmt.Sprintf("Domain metric: %s", name)),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create counter: %w", err)
	}

	// Cache it
	e.cache.counters[name] = counter

	e.logger.Debug("Created new domain counter",
		zap.String("metric", name))

	return counter, nil
}

// getOrCreateGauge gets or creates a gauge metric
func (e *OTELEmitter) getOrCreateGauge(name string) (metric.Int64Gauge, error) {
	// Fast path: check cache with read lock
	e.mu.RLock()
	if gauge, exists := e.cache.gauges[name]; exists {
		e.mu.RUnlock()
		return gauge, nil
	}
	e.mu.RUnlock()

	// Slow path: create gauge with write lock
	e.mu.Lock()
	defer e.mu.Unlock()

	// Double-check after acquiring write lock
	if gauge, exists := e.cache.gauges[name]; exists {
		return gauge, nil
	}

	// Create new gauge
	gauge, err := e.meter.Int64Gauge(
		name,
		metric.WithDescription(fmt.Sprintf("Domain gauge: %s", name)),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create gauge: %w", err)
	}

	// Cache it
	e.cache.gauges[name] = gauge

	e.logger.Debug("Created new domain gauge",
		zap.String("gauge", name))

	return gauge, nil
}

// GetCacheSize returns the number of cached metrics (for testing/debugging)
func (e *OTELEmitter) GetCacheSize() (counters int, gauges int) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.cache.counters), len(e.cache.gauges)
}
