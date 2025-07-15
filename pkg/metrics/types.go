package metrics

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Type-safe metric implementations using Go generics
// These provide compile-time type safety and runtime performance optimization

// Counter represents a type-safe counter metric
type Counter[T Numeric] struct {
	// Prometheus integration
	prometheusCounter prometheus.Counter

	// Type-safe value tracking
	value        int64 // Using atomic operations for thread safety
	initialValue T

	// Metadata
	name        string
	labels      Labels
	help        string
	created     time.Time
	lastUpdated time.Time

	// Thread safety
	mu sync.RWMutex

	// Performance tracking
	updateCount int64
	totalDelta  int64

	// Configuration
	constraints CounterConstraints[T]
}

// Gauge represents a type-safe gauge metric
type Gauge[T Numeric] struct {
	// Prometheus integration
	prometheusGauge prometheus.Gauge

	// Type-safe value tracking with atomic operations
	value    int64 // Stored as int64 for atomic ops, converted to T when needed
	minValue int64
	maxValue int64

	// Metadata
	name        string
	labels      Labels
	help        string
	created     time.Time
	lastUpdated time.Time

	// Thread safety
	mu sync.RWMutex

	// Performance tracking
	updateCount  int64
	maxRecorded  T
	minRecorded  T
	averageValue float64

	// Configuration
	constraints GaugeConstraints[T]
}

// Histogram represents a type-safe histogram metric
type Histogram[T Numeric] struct {
	// Prometheus integration
	prometheusHistogram prometheus.Histogram

	// Type-safe bucket tracking
	buckets  []HistogramBucket[T]
	bucketMu sync.RWMutex

	// Metadata
	name        string
	labels      Labels
	help        string
	created     time.Time
	lastUpdated time.Time

	// Statistics
	sampleCount int64
	sampleSum   int64

	// Performance tracking
	observationCount int64
	totalObserved    T
	minObserved      T
	maxObserved      T

	// Configuration
	constraints HistogramConstraints[T]
}

// Summary represents a type-safe summary metric
type Summary[T Numeric] struct {
	// Prometheus integration
	prometheusSummary prometheus.Summary

	// Type-safe quantile tracking
	quantiles  []Quantile[T]
	quantileMu sync.RWMutex

	// Metadata
	name        string
	labels      Labels
	help        string
	created     time.Time
	lastUpdated time.Time

	// Statistics
	sampleCount int64
	sampleSum   int64

	// Performance tracking
	observationCount int64
	totalObserved    T

	// Configuration
	constraints SummaryConstraints[T]

	// Sliding window for quantile calculation
	window *SlidingWindow[T]
}

// Supporting types for constraints and configuration
type (
	// Numeric constraint for type safety
	Numeric interface {
		~int | ~int8 | ~int16 | ~int32 | ~int64 |
			~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 |
			~float32 | ~float64
	}

	// CounterConstraints defines validation rules for counters
	CounterConstraints[T Numeric] struct {
		MinValue     T
		MaxValue     T
		MaxDelta     T
		AllowReset   bool
		ValidateFunc func(T) error
	}

	// GaugeConstraints defines validation rules for gauges
	GaugeConstraints[T Numeric] struct {
		MinValue     T
		MaxValue     T
		MaxDelta     T
		ValidateFunc func(T) error
	}

	// HistogramConstraints defines validation rules for histograms
	HistogramConstraints[T Numeric] struct {
		MinValue     T
		MaxValue     T
		BucketCount  int
		ValidateFunc func(T) error
	}

	// SummaryConstraints defines validation rules for summaries
	SummaryConstraints[T Numeric] struct {
		MinValue     T
		MaxValue     T
		MaxAge       time.Duration
		AgeBuckets   int
		ValidateFunc func(T) error
	}

	// HistogramBucket represents a histogram bucket
	HistogramBucket[T Numeric] struct {
		UpperBound T
		Count      int64
	}

	// Quantile represents a summary quantile
	Quantile[T Numeric] struct {
		Quantile float64
		Value    T
		Error    T
	}

	// SlidingWindow for efficient quantile calculation
	SlidingWindow[T Numeric] struct {
		samples    []Sample[T]
		capacity   int
		head       int
		tail       int
		size       int
		mu         sync.RWMutex
		totalSum   T
		sortedView []T
		dirty      bool
	}

	// Sample represents a sample in the sliding window
	Sample[T Numeric] struct {
		Value     T
		Timestamp time.Time
		Weight    float64
	}
)

// NewCounter creates a new type-safe counter
func NewCounter[T Numeric](name, help string, labels Labels, constraints CounterConstraints[T]) *Counter[T] {
	// Create Prometheus counter
	prometheusCounter := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        name,
		Help:        help,
		ConstLabels: prometheus.Labels(labels),
	})

	counter := &Counter[T]{
		prometheusCounter: prometheusCounter,
		name:              name,
		labels:            labels,
		help:              help,
		created:           time.Now(),
		lastUpdated:       time.Now(),
		constraints:       constraints,
	}

	return counter
}

// Add safely adds a value to the counter with type safety
func (c *Counter[T]) Add(value T) error {
	// Validate constraints
	if err := c.validateAdd(value); err != nil {
		return fmt.Errorf("counter add validation failed: %w", err)
	}

	// Convert to int64 for atomic operations
	deltaInt64 := c.toInt64(value)

	// Atomic update
	newValue := atomic.AddInt64(&c.value, deltaInt64)
	atomic.AddInt64(&c.updateCount, 1)
	atomic.AddInt64(&c.totalDelta, deltaInt64)

	// Update Prometheus counter
	c.prometheusCounter.Add(float64(value))

	// Update metadata
	c.mu.Lock()
	c.lastUpdated = time.Now()
	c.mu.Unlock()

	// Log significant changes if needed
	if c.shouldLogUpdate(newValue) {
		// Log update
	}

	return nil
}

// Inc increments the counter by 1
func (c *Counter[T]) Inc() error {
	return c.Add(T(1))
}

// Value returns the current counter value
func (c *Counter[T]) Value() T {
	return c.fromInt64(atomic.LoadInt64(&c.value))
}

// Reset resets the counter to zero if allowed
func (c *Counter[T]) Reset() error {
	if !c.constraints.AllowReset {
		return fmt.Errorf("counter reset not allowed")
	}

	atomic.StoreInt64(&c.value, 0)

	c.mu.Lock()
	c.lastUpdated = time.Now()
	c.mu.Unlock()

	return nil
}

// GetStats returns counter statistics
func (c *Counter[T]) GetStats() CounterStats[T] {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return CounterStats[T]{
		CurrentValue: c.Value(),
		UpdateCount:  atomic.LoadInt64(&c.updateCount),
		TotalDelta:   c.fromInt64(atomic.LoadInt64(&c.totalDelta)),
		Created:      c.created,
		LastUpdated:  c.lastUpdated,
		AverageRate:  c.calculateAverageRate(),
	}
}

// NewGauge creates a new type-safe gauge
func NewGauge[T Numeric](name, help string, labels Labels, constraints GaugeConstraints[T]) *Gauge[T] {
	// Create Prometheus gauge
	prometheusGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        name,
		Help:        help,
		ConstLabels: prometheus.Labels(labels),
	})

	gauge := &Gauge[T]{
		prometheusGauge: prometheusGauge,
		name:            name,
		labels:          labels,
		help:            help,
		created:         time.Now(),
		lastUpdated:     time.Now(),
		constraints:     constraints,
		minValue:        0x7FFFFFFFFFFFFFFF,         // Max int64 as initial min
		maxValue:        ^int64(0x7FFFFFFFFFFFFFFF), // Min int64 as initial max
	}

	return gauge
}

// Set sets the gauge to a specific value
func (g *Gauge[T]) Set(value T) error {
	// Validate constraints
	if err := g.validateSet(value); err != nil {
		return fmt.Errorf("gauge set validation failed: %w", err)
	}

	// Convert to int64 for atomic operations
	valueInt64 := g.toInt64(value)

	// Atomic update
	oldValue := atomic.SwapInt64(&g.value, valueInt64)
	atomic.AddInt64(&g.updateCount, 1)

	// Update min/max tracking
	g.updateMinMax(valueInt64)

	// Update Prometheus gauge
	g.prometheusGauge.Set(float64(value))

	// Update metadata
	g.mu.Lock()
	g.lastUpdated = time.Now()
	g.updateAverage(g.fromInt64(oldValue), value)
	g.mu.Unlock()

	return nil
}

// Add adds a value to the gauge
func (g *Gauge[T]) Add(delta T) error {
	currentValue := g.Value()
	newValue := currentValue + delta
	return g.Set(newValue)
}

// Sub subtracts a value from the gauge
func (g *Gauge[T]) Sub(delta T) error {
	currentValue := g.Value()
	newValue := currentValue - delta
	return g.Set(newValue)
}

// Value returns the current gauge value
func (g *Gauge[T]) Value() T {
	return g.fromInt64(atomic.LoadInt64(&g.value))
}

// GetStats returns gauge statistics
func (g *Gauge[T]) GetStats() GaugeStats[T] {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return GaugeStats[T]{
		CurrentValue: g.Value(),
		MinValue:     g.fromInt64(atomic.LoadInt64(&g.minValue)),
		MaxValue:     g.fromInt64(atomic.LoadInt64(&g.maxValue)),
		AverageValue: T(g.averageValue),
		UpdateCount:  atomic.LoadInt64(&g.updateCount),
		Created:      g.created,
		LastUpdated:  g.lastUpdated,
	}
}

// NewHistogram creates a new type-safe histogram
func NewHistogram[T Numeric](name, help string, labels Labels, buckets []T, constraints HistogramConstraints[T]) *Histogram[T] {
	// Convert buckets to float64 for Prometheus
	prometheusBuckets := make([]float64, len(buckets))
	for i, bucket := range buckets {
		prometheusBuckets[i] = float64(bucket)
	}

	// Create Prometheus histogram
	prometheusHistogram := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:        name,
		Help:        help,
		ConstLabels: prometheus.Labels(labels),
		Buckets:     prometheusBuckets,
	})

	// Create histogram buckets
	histogramBuckets := make([]HistogramBucket[T], len(buckets))
	for i, bucket := range buckets {
		histogramBuckets[i] = HistogramBucket[T]{
			UpperBound: bucket,
			Count:      0,
		}
	}

	histogram := &Histogram[T]{
		prometheusHistogram: prometheusHistogram,
		buckets:             histogramBuckets,
		name:                name,
		labels:              labels,
		help:                help,
		created:             time.Now(),
		lastUpdated:         time.Now(),
		constraints:         constraints,
	}

	return histogram
}

// Observe records an observation in the histogram
func (h *Histogram[T]) Observe(value T) error {
	// Validate constraints
	if err := h.validateObserve(value); err != nil {
		return fmt.Errorf("histogram observe validation failed: %w", err)
	}

	// Update Prometheus histogram
	h.prometheusHistogram.Observe(float64(value))

	// Update buckets
	h.bucketMu.Lock()
	for i := range h.buckets {
		if value <= h.buckets[i].UpperBound {
			atomic.AddInt64(&h.buckets[i].Count, 1)
		}
	}
	h.bucketMu.Unlock()

	// Update statistics
	atomic.AddInt64(&h.sampleCount, 1)
	atomic.AddInt64(&h.sampleSum, h.toInt64(value))
	atomic.AddInt64(&h.observationCount, 1)

	// Update min/max tracking
	h.updateObservationStats(value)

	// Update metadata
	h.mu.Lock()
	h.lastUpdated = time.Now()
	h.mu.Unlock()

	return nil
}

// GetStats returns histogram statistics
func (h *Histogram[T]) GetStats() HistogramStats[T] {
	h.mu.RLock()
	defer h.mu.RUnlock()

	h.bucketMu.RLock()
	bucketCounts := make([]int64, len(h.buckets))
	for i, bucket := range h.buckets {
		bucketCounts[i] = atomic.LoadInt64(&bucket.Count)
	}
	h.bucketMu.RUnlock()

	return HistogramStats[T]{
		SampleCount:      atomic.LoadInt64(&h.sampleCount),
		SampleSum:        h.fromInt64(atomic.LoadInt64(&h.sampleSum)),
		BucketCounts:     bucketCounts,
		ObservationCount: atomic.LoadInt64(&h.observationCount),
		MinObserved:      h.minObserved,
		MaxObserved:      h.maxObserved,
		Created:          h.created,
		LastUpdated:      h.lastUpdated,
	}
}

// NewSummary creates a new type-safe summary
func NewSummary[T Numeric](name, help string, labels Labels, quantiles []float64, constraints SummaryConstraints[T]) *Summary[T] {
	// Create quantile map for Prometheus
	quantileMap := make(map[float64]float64)
	for _, q := range quantiles {
		quantileMap[q] = 0.01 // Default error
	}

	// Create Prometheus summary
	prometheusSummary := prometheus.NewSummary(prometheus.SummaryOpts{
		Name:        name,
		Help:        help,
		ConstLabels: prometheus.Labels(labels),
		Objectives:  quantileMap,
		MaxAge:      constraints.MaxAge,
		AgeBuckets:  constraints.AgeBuckets,
	})

	// Create quantile tracking
	summaryQuantiles := make([]Quantile[T], len(quantiles))
	for i, q := range quantiles {
		summaryQuantiles[i] = Quantile[T]{
			Quantile: q,
			Value:    T(0),
			Error:    T(0.01),
		}
	}

	summary := &Summary[T]{
		prometheusSummary: prometheusSummary,
		quantiles:         summaryQuantiles,
		name:              name,
		labels:            labels,
		help:              help,
		created:           time.Now(),
		lastUpdated:       time.Now(),
		constraints:       constraints,
		window:            NewSlidingWindow[T](1000), // Default capacity
	}

	return summary
}

// Observe records an observation in the summary
func (s *Summary[T]) Observe(value T) error {
	// Validate constraints
	if err := s.validateObserve(value); err != nil {
		return fmt.Errorf("summary observe validation failed: %w", err)
	}

	// Update Prometheus summary
	s.prometheusSummary.Observe(float64(value))

	// Add to sliding window
	s.window.Add(Sample[T]{
		Value:     value,
		Timestamp: time.Now(),
		Weight:    1.0,
	})

	// Update statistics
	atomic.AddInt64(&s.sampleCount, 1)
	atomic.AddInt64(&s.sampleSum, s.toInt64(value))
	atomic.AddInt64(&s.observationCount, 1)

	// Update quantiles periodically
	s.updateQuantiles()

	// Update metadata
	s.mu.Lock()
	s.lastUpdated = time.Now()
	s.mu.Unlock()

	return nil
}

// GetQuantile returns the value at the specified quantile
func (s *Summary[T]) GetQuantile(quantile float64) (T, error) {
	s.quantileMu.RLock()
	defer s.quantileMu.RUnlock()

	for _, q := range s.quantiles {
		if q.Quantile == quantile {
			return q.Value, nil
		}
	}

	var zero T
	return zero, fmt.Errorf("quantile %f not configured", quantile)
}

// GetStats returns summary statistics
func (s *Summary[T]) GetStats() SummaryStats[T] {
	s.mu.RLock()
	defer s.mu.RUnlock()

	s.quantileMu.RLock()
	quantileValues := make(map[float64]T)
	for _, q := range s.quantiles {
		quantileValues[q.Quantile] = q.Value
	}
	s.quantileMu.RUnlock()

	return SummaryStats[T]{
		SampleCount:      atomic.LoadInt64(&s.sampleCount),
		SampleSum:        s.fromInt64(atomic.LoadInt64(&s.sampleSum)),
		QuantileValues:   quantileValues,
		ObservationCount: atomic.LoadInt64(&s.observationCount),
		Created:          s.created,
		LastUpdated:      s.lastUpdated,
	}
}

// Statistics types
type (
	CounterStats[T Numeric] struct {
		CurrentValue T
		UpdateCount  int64
		TotalDelta   T
		Created      time.Time
		LastUpdated  time.Time
		AverageRate  float64
	}

	GaugeStats[T Numeric] struct {
		CurrentValue T
		MinValue     T
		MaxValue     T
		AverageValue T
		UpdateCount  int64
		Created      time.Time
		LastUpdated  time.Time
	}

	HistogramStats[T Numeric] struct {
		SampleCount      int64
		SampleSum        T
		BucketCounts     []int64
		ObservationCount int64
		MinObserved      T
		MaxObserved      T
		Created          time.Time
		LastUpdated      time.Time
	}

	SummaryStats[T Numeric] struct {
		SampleCount      int64
		SampleSum        T
		QuantileValues   map[float64]T
		ObservationCount int64
		Created          time.Time
		LastUpdated      time.Time
	}
)

// Helper methods for type conversion and atomic operations

func (c *Counter[T]) toInt64(value T) int64 {
	return int64(value)
}

func (c *Counter[T]) fromInt64(value int64) T {
	return T(value)
}

func (c *Counter[T]) validateAdd(value T) error {
	if value < 0 {
		return fmt.Errorf("counter value cannot be negative")
	}

	if c.constraints.ValidateFunc != nil {
		return c.constraints.ValidateFunc(value)
	}

	return nil
}

func (c *Counter[T]) shouldLogUpdate(newValue int64) bool {
	// Log every 1000 updates or significant changes
	return atomic.LoadInt64(&c.updateCount)%1000 == 0
}

func (c *Counter[T]) calculateAverageRate() float64 {
	duration := time.Since(c.created).Seconds()
	if duration == 0 {
		return 0
	}
	return float64(atomic.LoadInt64(&c.updateCount)) / duration
}

func (g *Gauge[T]) toInt64(value T) int64 {
	return int64(value)
}

func (g *Gauge[T]) fromInt64(value int64) T {
	return T(value)
}

func (g *Gauge[T]) validateSet(value T) error {
	if value < g.constraints.MinValue || value > g.constraints.MaxValue {
		return fmt.Errorf("value %v outside constraints [%v, %v]", value, g.constraints.MinValue, g.constraints.MaxValue)
	}

	if g.constraints.ValidateFunc != nil {
		return g.constraints.ValidateFunc(value)
	}

	return nil
}

func (g *Gauge[T]) updateMinMax(value int64) {
	// Update minimum
	for {
		current := atomic.LoadInt64(&g.minValue)
		if value >= current {
			break
		}
		if atomic.CompareAndSwapInt64(&g.minValue, current, value) {
			break
		}
	}

	// Update maximum
	for {
		current := atomic.LoadInt64(&g.maxValue)
		if value <= current {
			break
		}
		if atomic.CompareAndSwapInt64(&g.maxValue, current, value) {
			break
		}
	}
}

func (g *Gauge[T]) updateAverage(oldValue, newValue T) {
	// Simple moving average
	updateCount := float64(atomic.LoadInt64(&g.updateCount))
	if updateCount == 1 {
		g.averageValue = float64(newValue)
	} else {
		g.averageValue = (g.averageValue*(updateCount-1) + float64(newValue)) / updateCount
	}
}

func (h *Histogram[T]) toInt64(value T) int64 {
	return int64(value)
}

func (h *Histogram[T]) fromInt64(value int64) T {
	return T(value)
}

func (h *Histogram[T]) validateObserve(value T) error {
	if value < h.constraints.MinValue || value > h.constraints.MaxValue {
		return fmt.Errorf("value %v outside constraints [%v, %v]", value, h.constraints.MinValue, h.constraints.MaxValue)
	}

	if h.constraints.ValidateFunc != nil {
		return h.constraints.ValidateFunc(value)
	}

	return nil
}

func (h *Histogram[T]) updateObservationStats(value T) {
	// This would update min/max observed values atomically
	// Simplified implementation
	if atomic.LoadInt64(&h.observationCount) == 1 {
		h.minObserved = value
		h.maxObserved = value
	} else {
		if value < h.minObserved {
			h.minObserved = value
		}
		if value > h.maxObserved {
			h.maxObserved = value
		}
	}
}

func (s *Summary[T]) toInt64(value T) int64 {
	return int64(value)
}

func (s *Summary[T]) fromInt64(value int64) T {
	return T(value)
}

func (s *Summary[T]) validateObserve(value T) error {
	if value < s.constraints.MinValue || value > s.constraints.MaxValue {
		return fmt.Errorf("value %v outside constraints [%v, %v]", value, s.constraints.MinValue, s.constraints.MaxValue)
	}

	if s.constraints.ValidateFunc != nil {
		return s.constraints.ValidateFunc(value)
	}

	return nil
}

func (s *Summary[T]) updateQuantiles() {
	// Update quantiles every 100 observations for performance
	if atomic.LoadInt64(&s.observationCount)%100 != 0 {
		return
	}

	samples := s.window.GetSortedSamples()
	if len(samples) == 0 {
		return
	}

	s.quantileMu.Lock()
	defer s.quantileMu.Unlock()

	for i, quantile := range s.quantiles {
		index := int(quantile.Quantile * float64(len(samples)-1))
		if index >= len(samples) {
			index = len(samples) - 1
		}
		s.quantiles[i].Value = samples[index].Value
	}
}

// Sliding window implementation

func NewSlidingWindow[T Numeric](capacity int) *SlidingWindow[T] {
	return &SlidingWindow[T]{
		samples:  make([]Sample[T], capacity),
		capacity: capacity,
		dirty:    true,
	}
}

func (sw *SlidingWindow[T]) Add(sample Sample[T]) {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	if sw.size < sw.capacity {
		sw.samples[sw.tail] = sample
		sw.tail = (sw.tail + 1) % sw.capacity
		sw.size++
	} else {
		// Remove oldest sample
		sw.totalSum -= sw.samples[sw.head].Value
		sw.samples[sw.head] = sample
		sw.head = (sw.head + 1) % sw.capacity
		sw.tail = (sw.tail + 1) % sw.capacity
	}

	sw.totalSum += sample.Value
	sw.dirty = true
}

func (sw *SlidingWindow[T]) GetSortedSamples() []Sample[T] {
	sw.mu.RLock()
	defer sw.mu.RUnlock()

	if !sw.dirty && sw.sortedView != nil {
		samples := make([]Sample[T], len(sw.sortedView))
		for i, value := range sw.sortedView {
			samples[i] = Sample[T]{Value: value}
		}
		return samples
	}

	// Create sorted view
	samples := make([]Sample[T], sw.size)
	for i := 0; i < sw.size; i++ {
		index := (sw.head + i) % sw.capacity
		samples[i] = sw.samples[index]
	}

	// Sort samples by value (simplified bubble sort for demo)
	for i := 0; i < len(samples)-1; i++ {
		for j := i + 1; j < len(samples); j++ {
			if samples[i].Value > samples[j].Value {
				samples[i], samples[j] = samples[j], samples[i]
			}
		}
	}

	return samples
}

// Implement MetricType interface for all metric types

func (c *Counter[T]) GetName() string         { return c.name }
func (c *Counter[T]) GetType() string         { return "counter" }
func (c *Counter[T]) GetLabels() Labels       { return c.labels }
func (c *Counter[T]) GetValue() interface{}   { return c.Value() }
func (c *Counter[T]) GetTimestamp() time.Time { return c.lastUpdated }
func (c *Counter[T]) GetMetadata() map[string]interface{} {
	return map[string]interface{}{
		"help":         c.help,
		"created":      c.created,
		"update_count": atomic.LoadInt64(&c.updateCount),
	}
}
func (c *Counter[T]) Validate() error { return nil }

func (g *Gauge[T]) GetName() string         { return g.name }
func (g *Gauge[T]) GetType() string         { return "gauge" }
func (g *Gauge[T]) GetLabels() Labels       { return g.labels }
func (g *Gauge[T]) GetValue() interface{}   { return g.Value() }
func (g *Gauge[T]) GetTimestamp() time.Time { return g.lastUpdated }
func (g *Gauge[T]) GetMetadata() map[string]interface{} {
	return map[string]interface{}{
		"help":         g.help,
		"created":      g.created,
		"update_count": atomic.LoadInt64(&g.updateCount),
		"min_value":    g.fromInt64(atomic.LoadInt64(&g.minValue)),
		"max_value":    g.fromInt64(atomic.LoadInt64(&g.maxValue)),
	}
}
func (g *Gauge[T]) Validate() error { return nil }

func (h *Histogram[T]) GetName() string         { return h.name }
func (h *Histogram[T]) GetType() string         { return "histogram" }
func (h *Histogram[T]) GetLabels() Labels       { return h.labels }
func (h *Histogram[T]) GetValue() interface{}   { return atomic.LoadInt64(&h.sampleCount) }
func (h *Histogram[T]) GetTimestamp() time.Time { return h.lastUpdated }
func (h *Histogram[T]) GetMetadata() map[string]interface{} {
	return map[string]interface{}{
		"help":              h.help,
		"created":           h.created,
		"sample_count":      atomic.LoadInt64(&h.sampleCount),
		"observation_count": atomic.LoadInt64(&h.observationCount),
	}
}
func (h *Histogram[T]) Validate() error { return nil }

func (s *Summary[T]) GetName() string         { return s.name }
func (s *Summary[T]) GetType() string         { return "summary" }
func (s *Summary[T]) GetLabels() Labels       { return s.labels }
func (s *Summary[T]) GetValue() interface{}   { return atomic.LoadInt64(&s.sampleCount) }
func (s *Summary[T]) GetTimestamp() time.Time { return s.lastUpdated }
func (s *Summary[T]) GetMetadata() map[string]interface{} {
	return map[string]interface{}{
		"help":              s.help,
		"created":           s.created,
		"sample_count":      atomic.LoadInt64(&s.sampleCount),
		"observation_count": atomic.LoadInt64(&s.observationCount),
	}
}
func (s *Summary[T]) Validate() error { return nil }
