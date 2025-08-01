package correlation

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// CorrelationMetrics provides OTEL metrics for the correlation system
type CorrelationMetrics struct {
	meter  metric.Meter
	logger *zap.Logger

	// Event processing metrics
	eventsProcessed   metric.Int64Counter
	eventsDropped     metric.Int64Counter
	processingLatency metric.Float64Histogram
	queueSize         metric.Int64ObservableGauge

	// Correlation metrics
	correlationsFound     metric.Int64Counter
	correlationConfidence metric.Float64Histogram
	correlationsByType    map[string]metric.Int64Counter
	activeCorrelations    metric.Int64ObservableGauge

	// Pattern detection metrics
	patternsDetected   metric.Int64Counter
	patternConfidence  metric.Float64Histogram
	sequencesCompleted metric.Int64Counter
	temporalMatches    metric.Int64Counter

	// K8s relationship metrics
	relationshipsLoaded  metric.Int64ObservableGauge
	ownershipCacheHits   metric.Int64Counter
	ownershipCacheMisses metric.Int64Counter
	selectorCacheHits    metric.Int64Counter
	selectorCacheMisses  metric.Int64Counter

	// Performance metrics
	cacheEvictions metric.Int64Counter
	memoryUsage    metric.Int64ObservableGauge
	goroutineCount metric.Int64ObservableGauge

	// Error metrics
	errorsByType map[string]metric.Int64Counter
	totalErrors  metric.Int64Counter

	// Internal state for observable gauges
	mu                   sync.RWMutex
	currentQueueSize     int64
	currentActiveCorrs   int64
	currentRelationships int64
	currentMemoryBytes   int64
	currentGoroutines    int64
}

// NewCorrelationMetrics creates a new metrics instance
func NewCorrelationMetrics(logger *zap.Logger) (*CorrelationMetrics, error) {
	meter := otel.Meter("tapio.correlation")

	cm := &CorrelationMetrics{
		meter:              meter,
		logger:             logger,
		correlationsByType: make(map[string]metric.Int64Counter),
		errorsByType:       make(map[string]metric.Int64Counter),
	}

	// Initialize event processing metrics
	eventsProcessed, err := meter.Int64Counter(
		"tapio.correlation.events.processed",
		metric.WithDescription("Total events processed by correlation engine"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	cm.eventsProcessed = eventsProcessed

	eventsDropped, err := meter.Int64Counter(
		"tapio.correlation.events.dropped",
		metric.WithDescription("Events dropped due to errors or overload"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	cm.eventsDropped = eventsDropped

	processingLatency, err := meter.Float64Histogram(
		"tapio.correlation.processing.latency",
		metric.WithDescription("Event processing latency"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}
	cm.processingLatency = processingLatency

	queueSize, err := meter.Int64ObservableGauge(
		"tapio.correlation.queue.size",
		metric.WithDescription("Current event queue size"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	cm.queueSize = queueSize

	// Initialize correlation metrics
	correlationsFound, err := meter.Int64Counter(
		"tapio.correlation.correlations.found",
		metric.WithDescription("Total correlations found"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	cm.correlationsFound = correlationsFound

	correlationConfidence, err := meter.Float64Histogram(
		"tapio.correlation.confidence",
		metric.WithDescription("Confidence scores of correlations"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	cm.correlationConfidence = correlationConfidence

	activeCorrelations, err := meter.Int64ObservableGauge(
		"tapio.correlation.active",
		metric.WithDescription("Currently active correlations"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	cm.activeCorrelations = activeCorrelations

	// Initialize pattern detection metrics
	patternsDetected, err := meter.Int64Counter(
		"tapio.correlation.patterns.detected",
		metric.WithDescription("Patterns detected by correlation engine"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	cm.patternsDetected = patternsDetected

	patternConfidence, err := meter.Float64Histogram(
		"tapio.correlation.patterns.confidence",
		metric.WithDescription("Confidence of detected patterns"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	cm.patternConfidence = patternConfidence

	sequencesCompleted, err := meter.Int64Counter(
		"tapio.correlation.sequences.completed",
		metric.WithDescription("Event sequences completed"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	cm.sequencesCompleted = sequencesCompleted

	temporalMatches, err := meter.Int64Counter(
		"tapio.correlation.temporal.matches",
		metric.WithDescription("Temporal correlations found"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	cm.temporalMatches = temporalMatches

	// Initialize K8s relationship metrics
	relationshipsLoaded, err := meter.Int64ObservableGauge(
		"tapio.correlation.k8s.relationships.loaded",
		metric.WithDescription("K8s relationships loaded in cache"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	cm.relationshipsLoaded = relationshipsLoaded

	ownershipCacheHits, err := meter.Int64Counter(
		"tapio.correlation.k8s.ownership.cache.hits",
		metric.WithDescription("Ownership cache hits"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	cm.ownershipCacheHits = ownershipCacheHits

	ownershipCacheMisses, err := meter.Int64Counter(
		"tapio.correlation.k8s.ownership.cache.misses",
		metric.WithDescription("Ownership cache misses"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	cm.ownershipCacheMisses = ownershipCacheMisses

	selectorCacheHits, err := meter.Int64Counter(
		"tapio.correlation.k8s.selector.cache.hits",
		metric.WithDescription("Selector cache hits"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	cm.selectorCacheHits = selectorCacheHits

	selectorCacheMisses, err := meter.Int64Counter(
		"tapio.correlation.k8s.selector.cache.misses",
		metric.WithDescription("Selector cache misses"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	cm.selectorCacheMisses = selectorCacheMisses

	// Initialize performance metrics
	cacheEvictions, err := meter.Int64Counter(
		"tapio.correlation.cache.evictions",
		metric.WithDescription("Cache evictions"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	cm.cacheEvictions = cacheEvictions

	memoryUsage, err := meter.Int64ObservableGauge(
		"tapio.correlation.memory.usage",
		metric.WithDescription("Memory usage in bytes"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, err
	}
	cm.memoryUsage = memoryUsage

	goroutineCount, err := meter.Int64ObservableGauge(
		"tapio.correlation.goroutines",
		metric.WithDescription("Active goroutines"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	cm.goroutineCount = goroutineCount

	// Initialize error metrics
	totalErrors, err := meter.Int64Counter(
		"tapio.correlation.errors.total",
		metric.WithDescription("Total errors in correlation engine"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	cm.totalErrors = totalErrors

	// Initialize correlation type counters
	correlationTypes := []string{"ownership", "selector", "temporal", "sequence", "causal", "same_resource"}
	for _, corrType := range correlationTypes {
		counter, err := meter.Int64Counter(
			"tapio.correlation.by_type",
			metric.WithDescription("Correlations by type"),
			metric.WithUnit("1"),
		)
		if err != nil {
			return nil, err
		}
		cm.correlationsByType[corrType] = counter
	}

	// Initialize error type counters
	errorTypes := []string{"processing", "k8s_api", "cache", "timeout", "invalid_event"}
	for _, errType := range errorTypes {
		counter, err := meter.Int64Counter(
			"tapio.correlation.errors",
			metric.WithDescription("Errors by type"),
			metric.WithUnit("1"),
		)
		if err != nil {
			return nil, err
		}
		cm.errorsByType[errType] = counter
	}

	// Register observable callbacks
	if err := cm.registerObservableCallbacks(); err != nil {
		return nil, err
	}

	return cm, nil
}

// RecordEventProcessed records a processed event
func (cm *CorrelationMetrics) RecordEventProcessed(ctx context.Context, eventType string, duration time.Duration) {
	cm.eventsProcessed.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("event.type", eventType),
		),
	)

	cm.processingLatency.Record(ctx, duration.Seconds(),
		metric.WithAttributes(
			attribute.String("event.type", eventType),
		),
	)
}

// RecordEventDropped records a dropped event
func (cm *CorrelationMetrics) RecordEventDropped(ctx context.Context, reason string) {
	cm.eventsDropped.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("drop.reason", reason),
		),
	)
}

// RecordCorrelation records a found correlation
func (cm *CorrelationMetrics) RecordCorrelation(ctx context.Context, corrType string, confidence float64, eventCount int) {
	cm.correlationsFound.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("correlation.type", corrType),
			attribute.Int("event.count", eventCount),
		),
	)

	cm.correlationConfidence.Record(ctx, confidence,
		metric.WithAttributes(
			attribute.String("correlation.type", corrType),
		),
	)

	if counter, exists := cm.correlationsByType[corrType]; exists {
		counter.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("correlation.type", corrType),
			),
		)
	}
}

// RecordPatternDetected records a detected pattern
func (cm *CorrelationMetrics) RecordPatternDetected(ctx context.Context, patternType string, confidence float64, occurrences int) {
	cm.patternsDetected.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("pattern.type", patternType),
			attribute.Int("occurrences", occurrences),
		),
	)

	cm.patternConfidence.Record(ctx, confidence,
		metric.WithAttributes(
			attribute.String("pattern.type", patternType),
		),
	)
}

// RecordSequenceCompleted records a completed sequence
func (cm *CorrelationMetrics) RecordSequenceCompleted(ctx context.Context, sequenceLength int, duration time.Duration) {
	cm.sequencesCompleted.Add(ctx, 1,
		metric.WithAttributes(
			attribute.Int("sequence.length", sequenceLength),
			attribute.Float64("duration.seconds", duration.Seconds()),
		),
	)
}

// RecordTemporalMatch records a temporal correlation
func (cm *CorrelationMetrics) RecordTemporalMatch(ctx context.Context, timeDelta time.Duration, confidence float64) {
	cm.temporalMatches.Add(ctx, 1,
		metric.WithAttributes(
			attribute.Float64("time_delta.seconds", timeDelta.Seconds()),
			attribute.Float64("confidence", confidence),
		),
	)
}

// RecordCacheHit records cache hits
func (cm *CorrelationMetrics) RecordCacheHit(ctx context.Context, cacheType string) {
	switch cacheType {
	case "ownership":
		cm.ownershipCacheHits.Add(ctx, 1)
	case "selector":
		cm.selectorCacheHits.Add(ctx, 1)
	}
}

// RecordCacheMiss records cache misses
func (cm *CorrelationMetrics) RecordCacheMiss(ctx context.Context, cacheType string) {
	switch cacheType {
	case "ownership":
		cm.ownershipCacheMisses.Add(ctx, 1)
	case "selector":
		cm.selectorCacheMisses.Add(ctx, 1)
	}
}

// RecordCacheEviction records cache evictions
func (cm *CorrelationMetrics) RecordCacheEviction(ctx context.Context, cacheType string, count int64) {
	cm.cacheEvictions.Add(ctx, count,
		metric.WithAttributes(
			attribute.String("cache.type", cacheType),
		),
	)
}

// RecordError records an error
func (cm *CorrelationMetrics) RecordError(ctx context.Context, errorType string, err error) {
	cm.totalErrors.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("error.type", errorType),
			attribute.String("error.message", err.Error()),
		),
	)

	if counter, exists := cm.errorsByType[errorType]; exists {
		counter.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("error.type", errorType),
			),
		)
	}
}

// UpdateQueueSize updates the current queue size
func (cm *CorrelationMetrics) UpdateQueueSize(size int64) {
	cm.mu.Lock()
	cm.currentQueueSize = size
	cm.mu.Unlock()
}

// UpdateActiveCorrelations updates the active correlation count
func (cm *CorrelationMetrics) UpdateActiveCorrelations(count int64) {
	cm.mu.Lock()
	cm.currentActiveCorrs = count
	cm.mu.Unlock()
}

// UpdateRelationshipCount updates the loaded relationships count
func (cm *CorrelationMetrics) UpdateRelationshipCount(count int64) {
	cm.mu.Lock()
	cm.currentRelationships = count
	cm.mu.Unlock()
}

// UpdateMemoryUsage updates memory usage
func (cm *CorrelationMetrics) UpdateMemoryUsage(bytes int64) {
	cm.mu.Lock()
	cm.currentMemoryBytes = bytes
	cm.mu.Unlock()
}

// UpdateGoroutineCount updates goroutine count
func (cm *CorrelationMetrics) UpdateGoroutineCount(count int64) {
	cm.mu.Lock()
	cm.currentGoroutines = count
	cm.mu.Unlock()
}

// registerObservableCallbacks registers callbacks for observable gauges
func (cm *CorrelationMetrics) registerObservableCallbacks() error {
	// Queue size callback
	_, err := cm.meter.RegisterCallback(
		func(ctx context.Context, o metric.Observer) error {
			cm.mu.RLock()
			size := cm.currentQueueSize
			cm.mu.RUnlock()
			o.ObserveInt64(cm.queueSize, size)
			return nil
		},
		cm.queueSize,
	)
	if err != nil {
		return err
	}

	// Active correlations callback
	_, err = cm.meter.RegisterCallback(
		func(ctx context.Context, o metric.Observer) error {
			cm.mu.RLock()
			count := cm.currentActiveCorrs
			cm.mu.RUnlock()
			o.ObserveInt64(cm.activeCorrelations, count)
			return nil
		},
		cm.activeCorrelations,
	)
	if err != nil {
		return err
	}

	// Relationships loaded callback
	_, err = cm.meter.RegisterCallback(
		func(ctx context.Context, o metric.Observer) error {
			cm.mu.RLock()
			count := cm.currentRelationships
			cm.mu.RUnlock()
			o.ObserveInt64(cm.relationshipsLoaded, count)
			return nil
		},
		cm.relationshipsLoaded,
	)
	if err != nil {
		return err
	}

	// Memory usage callback
	_, err = cm.meter.RegisterCallback(
		func(ctx context.Context, o metric.Observer) error {
			cm.mu.RLock()
			bytes := cm.currentMemoryBytes
			cm.mu.RUnlock()
			o.ObserveInt64(cm.memoryUsage, bytes)
			return nil
		},
		cm.memoryUsage,
	)
	if err != nil {
		return err
	}

	// Goroutine count callback
	_, err = cm.meter.RegisterCallback(
		func(ctx context.Context, o metric.Observer) error {
			cm.mu.RLock()
			count := cm.currentGoroutines
			cm.mu.RUnlock()
			o.ObserveInt64(cm.goroutineCount, count)
			return nil
		},
		cm.goroutineCount,
	)

	return err
}

// GetCacheStats returns cache hit/miss statistics
func (cm *CorrelationMetrics) GetCacheStats() map[string]interface{} {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return map[string]interface{}{
		"queue_size":          cm.currentQueueSize,
		"active_correlations": cm.currentActiveCorrs,
		"relationships":       cm.currentRelationships,
		"memory_bytes":        cm.currentMemoryBytes,
		"goroutines":          cm.currentGoroutines,
	}
}
