//go:build linux && ebpf
// +build linux,ebpf

package ebpf

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// MemoryEventBatch represents a batch of memory events for processing
type MemoryEventBatch struct {
	Events    []*MemoryEvent
	Size      int
	CreatedAt time.Time
	BatchID   uint64
}

// BatchProcessor processes events in batches for efficiency
type BatchProcessor struct {
	config     BatchConfig
	batchChan  chan *MemoryEventBatch
	eventQueue []*MemoryEvent
	mu         sync.Mutex
	batchID    uint64
	ctx        context.Context
	cancel     context.CancelFunc
	isRunning  int32
}

// BatchConfig configures batch processing
type BatchConfig struct {
	MaxBatchSize int
	BatchTimeout time.Duration
	MaxMemoryMB  int
}

// DefaultBatchConfig returns optimized batch configuration
func DefaultBatchConfig() BatchConfig {
	return BatchConfig{
		MaxBatchSize: 100,
		BatchTimeout: 50 * time.Millisecond,
		MaxMemoryMB:  10,
	}
}

// NewBatchProcessor creates a new batch processor
func NewBatchProcessor(config BatchConfig) *BatchProcessor {
	ctx, cancel := context.WithCancel(context.Background())
	return &BatchProcessor{
		config:     config,
		batchChan:  make(chan *MemoryEventBatch, 10),
		eventQueue: make([]*MemoryEvent, 0, config.MaxBatchSize),
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Start begins batch processing
func (bp *BatchProcessor) Start() error {
	if !atomic.CompareAndSwapInt32(&bp.isRunning, 0, 1) {
		return fmt.Errorf("batch processor already running")
	}

	go bp.processBatches()
	return nil
}

// Stop stops batch processing
func (bp *BatchProcessor) Stop() {
	if atomic.CompareAndSwapInt32(&bp.isRunning, 1, 0) {
		bp.cancel()
		close(bp.batchChan)
	}
}

// AddEvent adds an event to the current batch
func (bp *BatchProcessor) AddEvent(event *MemoryEvent) bool {
	if atomic.LoadInt32(&bp.isRunning) == 0 {
		return false
	}

	bp.mu.Lock()
	defer bp.mu.Unlock()

	bp.eventQueue = append(bp.eventQueue, event)

	if len(bp.eventQueue) >= bp.config.MaxBatchSize {
		bp.flushBatch()
	}

	return true
}

// GetBatches returns the batch channel
func (bp *BatchProcessor) GetBatches() <-chan *MemoryEventBatch {
	return bp.batchChan
}

// GetMetrics returns batch processor metrics
func (bp *BatchProcessor) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"batch_id":     atomic.LoadUint64(&bp.batchID),
		"queue_length": len(bp.eventQueue),
		"is_running":   atomic.LoadInt32(&bp.isRunning) == 1,
	}
}

func (bp *BatchProcessor) processBatches() {
	ticker := time.NewTicker(bp.config.BatchTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-bp.ctx.Done():
			return
		case <-ticker.C:
			bp.mu.Lock()
			if len(bp.eventQueue) > 0 {
				bp.flushBatch()
			}
			bp.mu.Unlock()
		}
	}
}

func (bp *BatchProcessor) flushBatch() {
	if len(bp.eventQueue) == 0 {
		return
	}

	batchID := atomic.AddUint64(&bp.batchID, 1)
	batch := &MemoryEventBatch{
		Events:    make([]*MemoryEvent, len(bp.eventQueue)),
		Size:      len(bp.eventQueue),
		CreatedAt: time.Now(),
		BatchID:   batchID,
	}

	copy(batch.Events, bp.eventQueue)
	bp.eventQueue = bp.eventQueue[:0] // Reset slice

	select {
	case bp.batchChan <- batch:
	default:
		// Drop batch if channel is full
	}
}

// RingBufferStats represents ring buffer statistics
type RingBufferStats struct {
	EventsReceived uint64 `json:"events_received"`
	EventsDropped  uint64 `json:"events_dropped"`
	ParseErrors    uint64 `json:"parse_errors"`
	BufferSize     int    `json:"buffer_size"`
}

// EventPipeline manages high-throughput event processing with worker pools
type EventPipeline struct {
	// Configuration
	workerCount  int
	queueSize    int
	maxRetries   int
	retryBackoff time.Duration

	// Components
	batchProcessor *BatchProcessor
	ringBufferMgr  *RingBufferManager

	// Worker pools
	ingestionWorkers  []*IngestionWorker
	processingWorkers []*ProcessingWorker
	outputWorkers     []*OutputWorker

	// Queues
	ingestionQueue  chan *MemoryEvent
	processingQueue chan *MemoryEventBatch
	outputQueue     chan *ProcessedBatch

	// Circuit breaker
	circuitBreaker *CircuitBreaker

	// State management
	ctx       context.Context
	cancel    context.CancelFunc
	isRunning int32 // atomic

	// Performance metrics
	eventsIngested  uint64
	eventsProcessed uint64
	eventsOutput    uint64
	totalLatency    uint64 // in nanoseconds
	errorCount      uint64

	// Load balancing
	loadBalancer *LoadBalancer

	// Memory management
	memoryPool *MemoryPool
}

// PipelineConfig defines configuration for the event pipeline
type PipelineConfig struct {
	WorkerCount          int           `json:"worker_count"`
	QueueSize            int           `json:"queue_size"`
	MaxRetries           int           `json:"max_retries"`
	RetryBackoff         time.Duration `json:"retry_backoff"`
	EnableCircuitBreaker bool          `json:"enable_circuit_breaker"`
	MemoryPoolSize       int           `json:"memory_pool_size"`
	LoadBalancing        string        `json:"load_balancing"` // "round_robin", "least_loaded", "weighted"
}

// DefaultPipelineConfig returns optimized pipeline configuration
func DefaultPipelineConfig() *PipelineConfig {
	return &PipelineConfig{
		WorkerCount:          runtime.NumCPU(),
		QueueSize:            10000,
		MaxRetries:           3,
		RetryBackoff:         10 * time.Millisecond,
		EnableCircuitBreaker: true,
		MemoryPoolSize:       1000,
		LoadBalancing:        "least_loaded",
	}
}

// NewEventPipeline creates a new high-performance event processing pipeline
func NewEventPipeline(config *PipelineConfig) (*EventPipeline, error) {
	if config == nil {
		config = DefaultPipelineConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create components
	batchProcessor := NewBatchProcessor(DefaultBatchConfig())

	ringBufferMgr, err := NewRingBufferManager(DefaultRingBufferConfig())
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create ring buffer manager: %w", err)
	}

	var circuitBreaker *CircuitBreaker
	if config.EnableCircuitBreaker {
		circuitBreaker = NewCircuitBreaker(10, 30*time.Second)
	}

	loadBalancer := NewLoadBalancer(config.LoadBalancing, config.WorkerCount)
	memoryPool := NewMemoryPool(config.MemoryPoolSize)

	pipeline := &EventPipeline{
		workerCount:     config.WorkerCount,
		queueSize:       config.QueueSize,
		maxRetries:      config.MaxRetries,
		retryBackoff:    config.RetryBackoff,
		batchProcessor:  batchProcessor,
		ringBufferMgr:   ringBufferMgr,
		circuitBreaker:  circuitBreaker,
		ctx:             ctx,
		cancel:          cancel,
		ingestionQueue:  make(chan *MemoryEvent, config.QueueSize),
		processingQueue: make(chan *MemoryEventBatch, config.QueueSize/10),
		outputQueue:     make(chan *ProcessedBatch, config.QueueSize/20),
		loadBalancer:    loadBalancer,
		memoryPool:      memoryPool,
	}

	// Initialize worker pools
	pipeline.initializeWorkers()

	return pipeline, nil
}

// SetTargets configures the pipeline with external components
func (ep *EventPipeline) SetTargets(batchProcessor *BatchProcessor, ringBufferMgr *RingBufferManager) error {
	if ep.batchProcessor != nil && ep.batchProcessor != batchProcessor {
		// Stop existing batch processor if different
		ep.batchProcessor.Stop()
	}
	ep.batchProcessor = batchProcessor

	if ep.ringBufferMgr != nil && ep.ringBufferMgr != ringBufferMgr {
		// Stop existing ring buffer manager if different
		ep.ringBufferMgr.Stop()
	}
	ep.ringBufferMgr = ringBufferMgr

	return nil
}

// Start begins the event processing pipeline
func (ep *EventPipeline) Start() error {
	if !atomic.CompareAndSwapInt32(&ep.isRunning, 0, 1) {
		return fmt.Errorf("pipeline already running")
	}

	// Start components
	if err := ep.batchProcessor.Start(); err != nil {
		return fmt.Errorf("failed to start batch processor: %w", err)
	}

	// Start worker pools
	for _, worker := range ep.ingestionWorkers {
		go worker.Run()
	}

	for _, worker := range ep.processingWorkers {
		go worker.Run()
	}

	for _, worker := range ep.outputWorkers {
		go worker.Run()
	}

	// Start pipeline coordination
	go ep.coordinatePipeline()

	return nil
}

// Stop gracefully stops the event processing pipeline
func (ep *EventPipeline) Stop() error {
	if !atomic.CompareAndSwapInt32(&ep.isRunning, 1, 0) {
		return nil // Already stopped
	}

	ep.cancel()

	// Stop components
	ep.batchProcessor.Stop()
	ep.ringBufferMgr.Stop()

	// Close queues
	close(ep.ingestionQueue)
	close(ep.processingQueue)
	close(ep.outputQueue)

	return nil
}

// IngestEvent adds an event to the pipeline
func (ep *EventPipeline) IngestEvent(event *MemoryEvent) bool {
	if atomic.LoadInt32(&ep.isRunning) == 0 {
		return false
	}

	// Check circuit breaker
	if ep.circuitBreaker != nil && !ep.circuitBreaker.Allow() {
		return false
	}

	select {
	case ep.ingestionQueue <- event:
		atomic.AddUint64(&ep.eventsIngested, 1)
		return true
	default:
		// Queue full
		return false
	}
}

// GetMetrics returns comprehensive pipeline metrics
func (ep *EventPipeline) GetMetrics() *PipelineMetrics {
	metrics := &PipelineMetrics{
		EventsIngested:  atomic.LoadUint64(&ep.eventsIngested),
		EventsProcessed: atomic.LoadUint64(&ep.eventsProcessed),
		EventsOutput:    atomic.LoadUint64(&ep.eventsOutput),
		ErrorCount:      atomic.LoadUint64(&ep.errorCount),
		IsRunning:       atomic.LoadInt32(&ep.isRunning) == 1,
		WorkerCount:     ep.workerCount,
		QueueDepths: QueueDepths{
			Ingestion:  len(ep.ingestionQueue),
			Processing: len(ep.processingQueue),
			Output:     len(ep.outputQueue),
		},
	}

	// Calculate average latency
	totalLatency := atomic.LoadUint64(&ep.totalLatency)
	if metrics.EventsProcessed > 0 {
		metrics.AvgLatencyNs = totalLatency / metrics.EventsProcessed
		metrics.AvgLatencyMs = float64(metrics.AvgLatencyNs) / 1e6
	}

	// Add component metrics
	if ep.batchProcessor != nil {
		metrics.BatchProcessor = ep.batchProcessor.GetMetrics()
	}

	if ep.ringBufferMgr != nil {
		rbMetrics := ep.ringBufferMgr.GetMetrics()
		metrics.RingBuffer = &RingBufferStats{
			EventsReceived: rbMetrics.EventsReceived,
			EventsDropped:  rbMetrics.EventsDropped,
			ParseErrors:    rbMetrics.ParseErrors,
			BufferSize:     len(rbMetrics.BufferMetrics),
		}
	}

	if ep.circuitBreaker != nil {
		metrics.CircuitBreakerState = ep.circuitBreaker.State()
	}

	return metrics
}

// initializeWorkers creates worker pools for each stage
func (ep *EventPipeline) initializeWorkers() {
	// Ingestion workers (receive events from eBPF)
	ep.ingestionWorkers = make([]*IngestionWorker, ep.workerCount/4+1)
	for i := range ep.ingestionWorkers {
		ep.ingestionWorkers[i] = NewIngestionWorker(i, ep)
	}

	// Processing workers (batch processing)
	ep.processingWorkers = make([]*ProcessingWorker, ep.workerCount/2)
	for i := range ep.processingWorkers {
		ep.processingWorkers[i] = NewProcessingWorker(i, ep)
	}

	// Output workers (send to downstream systems)
	ep.outputWorkers = make([]*OutputWorker, ep.workerCount/4+1)
	for i := range ep.outputWorkers {
		ep.outputWorkers[i] = NewOutputWorker(i, ep)
	}
}

// coordinatePipeline manages the flow between pipeline stages
func (ep *EventPipeline) coordinatePipeline() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ep.ctx.Done():
			return
		case <-ticker.C:
			ep.checkPipelineHealth()
			ep.adjustWorkerAllocation()
		}
	}
}

// checkPipelineHealth monitors pipeline performance and health
func (ep *EventPipeline) checkPipelineHealth() {
	// Check queue lengths
	ingestionDepth := len(ep.ingestionQueue)
	processingDepth := len(ep.processingQueue)
	outputDepth := len(ep.outputQueue)

	// Alert on queue buildup
	maxDepth := ep.queueSize / 2

	if ingestionDepth > maxDepth {
		// Ingestion queue backing up - may need more processing workers
		ep.loadBalancer.AdjustLoad("processing", 1.1)
	}

	if processingDepth > maxDepth/10 {
		// Processing queue backing up - may need more output workers
		ep.loadBalancer.AdjustLoad("output", 1.1)
	}

	if outputDepth > maxDepth/20 {
		// Output queue backing up - downstream systems may be slow
		if ep.circuitBreaker != nil {
			ep.circuitBreaker.RecordFailure()
		}
	}
}

// adjustWorkerAllocation dynamically adjusts worker allocation
func (ep *EventPipeline) adjustWorkerAllocation() {
	// Simple load-based adjustment
	totalEvents := atomic.LoadUint64(&ep.eventsIngested)
	processedEvents := atomic.LoadUint64(&ep.eventsProcessed)

	if totalEvents > 0 {
		processingRatio := float64(processedEvents) / float64(totalEvents)

		// If processing is falling behind, record circuit breaker failure
		if processingRatio < 0.8 && ep.circuitBreaker != nil {
			ep.circuitBreaker.RecordFailure()
		} else if ep.circuitBreaker != nil {
			ep.circuitBreaker.RecordSuccess()
		}
	}
}

// Worker types and support structures...

// IngestionWorker handles incoming events from eBPF
type IngestionWorker struct {
	id       int
	pipeline *EventPipeline
	stats    *WorkerStats
}

func NewIngestionWorker(id int, pipeline *EventPipeline) *IngestionWorker {
	return &IngestionWorker{
		id:       id,
		pipeline: pipeline,
		stats:    &WorkerStats{},
	}
}

func (iw *IngestionWorker) Run() {
	for {
		select {
		case <-iw.pipeline.ctx.Done():
			return
		case event, ok := <-iw.pipeline.ingestionQueue:
			if !ok {
				return
			}

			startTime := time.Now()
			iw.processEvent(event)

			// Update stats
			latency := time.Since(startTime)
			atomic.AddUint64(&iw.stats.EventsProcessed, 1)
			atomic.AddUint64(&iw.stats.TotalLatency, uint64(latency.Nanoseconds()))
		}
	}
}

func (iw *IngestionWorker) processEvent(event *MemoryEvent) {
	// Enrich event with metadata
	event.Timestamp = time.Now()

	// Route to batch processor
	if !iw.pipeline.batchProcessor.AddEvent(event) {
		atomic.AddUint64(&iw.pipeline.errorCount, 1)
	}
}

// ProcessingWorker handles batch processing
type ProcessingWorker struct {
	id       int
	pipeline *EventPipeline
	stats    *WorkerStats
}

func NewProcessingWorker(id int, pipeline *EventPipeline) *ProcessingWorker {
	return &ProcessingWorker{
		id:       id,
		pipeline: pipeline,
		stats:    &WorkerStats{},
	}
}

func (pw *ProcessingWorker) Run() {
	batches := pw.pipeline.batchProcessor.GetBatches()

	for {
		select {
		case <-pw.pipeline.ctx.Done():
			return
		case batch, ok := <-batches:
			if !ok {
				return
			}

			startTime := time.Now()
			processedBatch := pw.processBatch(batch)

			// Update pipeline metrics
			atomic.AddUint64(&pw.pipeline.eventsProcessed, uint64(batch.Size))

			// Update latency
			latency := time.Since(startTime)
			atomic.AddUint64(&pw.pipeline.totalLatency, uint64(latency.Nanoseconds()))

			// Send to output queue
			select {
			case pw.pipeline.outputQueue <- processedBatch:
			default:
				// Output queue full
				atomic.AddUint64(&pw.pipeline.errorCount, 1)
			}
		}
	}
}

func (pw *ProcessingWorker) processBatch(batch *MemoryEventBatch) *ProcessedBatch {
	// Process the batch of events
	results := make([]*ProcessedEvent, 0, batch.Size)

	for _, event := range batch.Events {
		processed := pw.processEvent(event)
		if processed != nil {
			results = append(results, processed)
		}
	}

	return &ProcessedBatch{
		OriginalBatch: batch,
		Results:       results,
		ProcessedAt:   time.Now(),
		WorkerID:      pw.id,
	}
}

func (pw *ProcessingWorker) processEvent(event *MemoryEvent) *ProcessedEvent {
	// This is where the actual event processing logic goes
	// For now, we just convert to a processed event
	return &ProcessedEvent{
		OriginalEvent: event,
		ProcessedAt:   time.Now(),
		WorkerID:      pw.id,
		Metrics: map[string]interface{}{
			"processing_time_ns": time.Since(event.Timestamp).Nanoseconds(),
		},
	}
}

// OutputWorker handles sending processed results to downstream systems
type OutputWorker struct {
	id       int
	pipeline *EventPipeline
	stats    *WorkerStats
}

func NewOutputWorker(id int, pipeline *EventPipeline) *OutputWorker {
	return &OutputWorker{
		id:       id,
		pipeline: pipeline,
		stats:    &WorkerStats{},
	}
}

func (ow *OutputWorker) Run() {
	for {
		select {
		case <-ow.pipeline.ctx.Done():
			return
		case batch, ok := <-ow.pipeline.outputQueue:
			if !ok {
				return
			}

			ow.outputBatch(batch)
			atomic.AddUint64(&ow.pipeline.eventsOutput, uint64(len(batch.Results)))
		}
	}
}

func (ow *OutputWorker) outputBatch(batch *ProcessedBatch) {
	// This is where processed events would be sent to downstream systems
	// For now, we just simulate the output
	for _, result := range batch.Results {
		ow.outputEvent(result)
	}
}

func (ow *OutputWorker) outputEvent(event *ProcessedEvent) {
	// Simulate output processing
	// In a real implementation, this would send to:
	// - Prometheus metrics
	// - Log aggregation systems
	// - Alert systems
	// - Storage systems
	_ = event
}

// Support structures

// ProcessedBatch represents a batch of processed events
type ProcessedBatch struct {
	OriginalBatch *MemoryEventBatch
	Results       []*ProcessedEvent
	ProcessedAt   time.Time
	WorkerID      int
}

// ProcessedEvent represents a processed memory event
type ProcessedEvent struct {
	OriginalEvent *MemoryEvent
	ProcessedAt   time.Time
	WorkerID      int
	Metrics       map[string]interface{}
}

// WorkerStats tracks per-worker statistics
type WorkerStats struct {
	EventsProcessed uint64
	TotalLatency    uint64
	ErrorCount      uint64
}

// PipelineMetrics contains comprehensive pipeline metrics
type PipelineMetrics struct {
	EventsIngested      uint64                 `json:"events_ingested"`
	EventsProcessed     uint64                 `json:"events_processed"`
	EventsOutput        uint64                 `json:"events_output"`
	ErrorCount          uint64                 `json:"error_count"`
	AvgLatencyNs        uint64                 `json:"avg_latency_ns"`
	AvgLatencyMs        float64                `json:"avg_latency_ms"`
	IsRunning           bool                   `json:"is_running"`
	WorkerCount         int                    `json:"worker_count"`
	QueueDepths         QueueDepths            `json:"queue_depths"`
	BatchProcessor      map[string]interface{} `json:"batch_processor,omitempty"`
	RingBuffer          *RingBufferStats       `json:"ring_buffer,omitempty"`
	CircuitBreakerState string                 `json:"circuit_breaker_state,omitempty"`
}

// QueueDepths tracks depth of each queue in the pipeline
type QueueDepths struct {
	Ingestion  int `json:"ingestion"`
	Processing int `json:"processing"`
	Output     int `json:"output"`
}

// LoadBalancer manages load distribution across workers
type LoadBalancer struct {
	strategy    string
	workerCount int
	loads       []float64
	mutex       sync.RWMutex
}

func NewLoadBalancer(strategy string, workerCount int) *LoadBalancer {
	return &LoadBalancer{
		strategy:    strategy,
		workerCount: workerCount,
		loads:       make([]float64, workerCount),
	}
}

func (lb *LoadBalancer) AdjustLoad(component string, factor float64) {
	lb.mutex.Lock()
	defer lb.mutex.Unlock()

	// Adjust load factors based on component
	switch component {
	case "processing":
		for i := range lb.loads {
			lb.loads[i] *= factor
		}
	case "output":
		// Different adjustment for output
		for i := range lb.loads {
			lb.loads[i] *= factor * 0.8
		}
	}
}

// MemoryPool manages reusable memory allocations
type MemoryPool struct {
	pool      sync.Pool
	maxSize   int
	allocated int32
}

func NewMemoryPool(maxSize int) *MemoryPool {
	return &MemoryPool{
		maxSize: maxSize,
		pool: sync.Pool{
			New: func() interface{} {
				return make([]*MemoryEvent, 0, 100)
			},
		},
	}
}

func (mp *MemoryPool) Get() []*MemoryEvent {
	if atomic.LoadInt32(&mp.allocated) < int32(mp.maxSize) {
		atomic.AddInt32(&mp.allocated, 1)
		return mp.pool.Get().([]*MemoryEvent)
	}
	return make([]*MemoryEvent, 0, 100)
}

func (mp *MemoryPool) Put(slice []*MemoryEvent) {
	if atomic.LoadInt32(&mp.allocated) > 0 {
		// Reset slice
		slice = slice[:0]
		mp.pool.Put(slice)
		atomic.AddInt32(&mp.allocated, -1)
	}
}

// CircuitBreaker is defined in error_handler.go
