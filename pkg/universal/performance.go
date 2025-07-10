package universal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// BufferPool manages reusable byte buffers for serialization
type BufferPool struct {
	pool sync.Pool
}

// NewBufferPool creates a new buffer pool
func NewBufferPool() *BufferPool {
	return &BufferPool{
		pool: sync.Pool{
			New: func() interface{} {
				return new(bytes.Buffer)
			},
		},
	}
}

// Get retrieves a buffer from the pool
func (p *BufferPool) Get() *bytes.Buffer {
	buf := p.pool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

// Put returns a buffer to the pool
func (p *BufferPool) Put(buf *bytes.Buffer) {
	if buf != nil {
		p.pool.Put(buf)
	}
}

// Serializer provides fast serialization with pre-allocated buffers
type Serializer struct {
	bufferPool *BufferPool
	encoder    *json.Encoder
}

// NewSerializer creates a new serializer
func NewSerializer() *Serializer {
	return &Serializer{
		bufferPool: NewBufferPool(),
	}
}

// SerializeMetric serializes a metric efficiently
func (s *Serializer) SerializeMetric(metric *UniversalMetric) ([]byte, error) {
	buf := s.bufferPool.Get()
	defer s.bufferPool.Put(buf)

	encoder := json.NewEncoder(buf)
	if err := encoder.Encode(metric); err != nil {
		return nil, fmt.Errorf("failed to serialize metric: %w", err)
	}

	// Make a copy to return (buffer will be reused)
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())

	return result, nil
}

// SerializeEvent serializes an event efficiently
func (s *Serializer) SerializeEvent(event *UniversalEvent) ([]byte, error) {
	buf := s.bufferPool.Get()
	defer s.bufferPool.Put(buf)

	encoder := json.NewEncoder(buf)
	if err := encoder.Encode(event); err != nil {
		return nil, fmt.Errorf("failed to serialize event: %w", err)
	}

	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())

	return result, nil
}

// SerializePrediction serializes a prediction efficiently
func (s *Serializer) SerializePrediction(prediction *UniversalPrediction) ([]byte, error) {
	buf := s.bufferPool.Get()
	defer s.bufferPool.Put(buf)

	encoder := json.NewEncoder(buf)
	if err := encoder.Encode(prediction); err != nil {
		return nil, fmt.Errorf("failed to serialize prediction: %w", err)
	}

	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())

	return result, nil
}

// BatchProcessor processes data in batches for efficiency
type BatchProcessor struct {
	metricBatch     []*UniversalMetric
	eventBatch      []*UniversalEvent
	predictionBatch []*UniversalPrediction

	metricMu     sync.Mutex
	eventMu      sync.Mutex
	predictionMu sync.Mutex

	batchSize    int
	flushTimeout time.Duration
	processor    DataProcessor

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// DataProcessor interface for processing universal data
type DataProcessor interface {
	ProcessMetrics(metrics []*UniversalMetric) error
	ProcessEvents(events []*UniversalEvent) error
	ProcessPredictions(predictions []*UniversalPrediction) error
}

// NewBatchProcessor creates a new batch processor
func NewBatchProcessor(batchSize int, flushTimeout time.Duration, processor DataProcessor) *BatchProcessor {
	bp := &BatchProcessor{
		metricBatch:     make([]*UniversalMetric, 0, batchSize),
		eventBatch:      make([]*UniversalEvent, 0, batchSize),
		predictionBatch: make([]*UniversalPrediction, 0, batchSize),
		batchSize:       batchSize,
		flushTimeout:    flushTimeout,
		processor:       processor,
		stopCh:          make(chan struct{}),
	}

	// Start flush timer
	bp.wg.Add(1)
	go bp.flushLoop()

	return bp
}

// AddMetric adds a metric to the batch
func (bp *BatchProcessor) AddMetric(metric *UniversalMetric) error {
	bp.metricMu.Lock()
	defer bp.metricMu.Unlock()

	bp.metricBatch = append(bp.metricBatch, metric)

	if len(bp.metricBatch) >= bp.batchSize {
		return bp.flushMetricsLocked()
	}

	return nil
}

// AddEvent adds an event to the batch
func (bp *BatchProcessor) AddEvent(event *UniversalEvent) error {
	bp.eventMu.Lock()
	defer bp.eventMu.Unlock()

	bp.eventBatch = append(bp.eventBatch, event)

	if len(bp.eventBatch) >= bp.batchSize {
		return bp.flushEventsLocked()
	}

	return nil
}

// AddPrediction adds a prediction to the batch
func (bp *BatchProcessor) AddPrediction(prediction *UniversalPrediction) error {
	bp.predictionMu.Lock()
	defer bp.predictionMu.Unlock()

	bp.predictionBatch = append(bp.predictionBatch, prediction)

	if len(bp.predictionBatch) >= bp.batchSize {
		return bp.flushPredictionsLocked()
	}

	return nil
}

// flushLoop periodically flushes batches
func (bp *BatchProcessor) flushLoop() {
	defer bp.wg.Done()

	ticker := time.NewTicker(bp.flushTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			bp.Flush()
		case <-bp.stopCh:
			bp.Flush() // Final flush
			return
		}
	}
}

// Flush forces a flush of all batches
func (bp *BatchProcessor) Flush() {
	bp.metricMu.Lock()
	bp.flushMetricsLocked()
	bp.metricMu.Unlock()

	bp.eventMu.Lock()
	bp.flushEventsLocked()
	bp.eventMu.Unlock()

	bp.predictionMu.Lock()
	bp.flushPredictionsLocked()
	bp.predictionMu.Unlock()
}

// flushMetricsLocked flushes metrics (must be called with lock held)
func (bp *BatchProcessor) flushMetricsLocked() error {
	if len(bp.metricBatch) == 0 {
		return nil
	}

	// Process batch
	err := bp.processor.ProcessMetrics(bp.metricBatch)

	// Clear batch (keep allocated memory)
	bp.metricBatch = bp.metricBatch[:0]

	return err
}

// flushEventsLocked flushes events (must be called with lock held)
func (bp *BatchProcessor) flushEventsLocked() error {
	if len(bp.eventBatch) == 0 {
		return nil
	}

	err := bp.processor.ProcessEvents(bp.eventBatch)
	bp.eventBatch = bp.eventBatch[:0]

	return err
}

// flushPredictionsLocked flushes predictions (must be called with lock held)
func (bp *BatchProcessor) flushPredictionsLocked() error {
	if len(bp.predictionBatch) == 0 {
		return nil
	}

	err := bp.processor.ProcessPredictions(bp.predictionBatch)
	bp.predictionBatch = bp.predictionBatch[:0]

	return err
}

// Stop stops the batch processor
func (bp *BatchProcessor) Stop() {
	close(bp.stopCh)
	bp.wg.Wait()
}

// MetricAggregator provides efficient metric aggregation
type MetricAggregator struct {
	metrics map[string]*AggregatedMetric
	mu      sync.RWMutex
}

// AggregatedMetric represents an aggregated metric
type AggregatedMetric struct {
	Name       string
	Target     Target
	Count      int64
	Sum        float64
	Min        float64
	Max        float64
	LastValue  float64
	LastUpdate time.Time
}

// NewMetricAggregator creates a new metric aggregator
func NewMetricAggregator() *MetricAggregator {
	return &MetricAggregator{
		metrics: make(map[string]*AggregatedMetric),
	}
}

// Aggregate adds a metric to the aggregation
func (ma *MetricAggregator) Aggregate(metric *UniversalMetric) {
	key := fmt.Sprintf("%s_%s_%s", metric.Target.Type, metric.Target.Name, metric.Name)

	ma.mu.Lock()
	defer ma.mu.Unlock()

	agg, exists := ma.metrics[key]
	if !exists {
		agg = &AggregatedMetric{
			Name:   metric.Name,
			Target: metric.Target,
			Min:    metric.Value,
			Max:    metric.Value,
		}
		ma.metrics[key] = agg
	}

	agg.Count++
	agg.Sum += metric.Value
	agg.LastValue = metric.Value
	agg.LastUpdate = metric.Timestamp

	if metric.Value < agg.Min {
		agg.Min = metric.Value
	}
	if metric.Value > agg.Max {
		agg.Max = metric.Value
	}
}

// GetAggregated returns aggregated metrics
func (ma *MetricAggregator) GetAggregated() map[string]*AggregatedMetric {
	ma.mu.RLock()
	defer ma.mu.RUnlock()

	// Return a copy to avoid races
	result := make(map[string]*AggregatedMetric, len(ma.metrics))
	for k, v := range ma.metrics {
		// Shallow copy is sufficient for read-only access
		copy := *v
		result[k] = &copy
	}

	return result
}

// Reset clears all aggregated data
func (ma *MetricAggregator) Reset() {
	ma.mu.Lock()
	defer ma.mu.Unlock()

	ma.metrics = make(map[string]*AggregatedMetric)
}
