package performance

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// BatchProcessor processes items in batches for improved efficiency
type BatchProcessor[T any] struct {
	// Configuration
	batchSize    int
	maxWaitTime  time.Duration
	maxQueueSize int
	
	// Processing function
	processFn    BatchProcessFunc[T]
	
	// Buffering
	queue        chan T
	batch        []T
	batchMutex   sync.Mutex
	
	// Control
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	
	// Metrics
	processed    atomic.Uint64
	batches      atomic.Uint64
	dropped      atomic.Uint64
	errors       atomic.Uint64
	avgBatchSize atomic.Uint64
}

// BatchProcessFunc processes a batch of items
type BatchProcessFunc[T any] func(ctx context.Context, batch []T) error

// BatchProcessorConfig configures the batch processor
type BatchProcessorConfig struct {
	BatchSize    int
	MaxWaitTime  time.Duration
	MaxQueueSize int
	ProcessFunc  BatchProcessFunc[any]
}

// NewBatchProcessor creates a new batch processor
func NewBatchProcessor[T any](
	batchSize int,
	maxWaitTime time.Duration,
	maxQueueSize int,
	processFn BatchProcessFunc[T],
) *BatchProcessor[T] {
	if batchSize <= 0 {
		batchSize = 100
	}
	if maxWaitTime <= 0 {
		maxWaitTime = 100 * time.Millisecond
	}
	if maxQueueSize <= 0 {
		maxQueueSize = 10000
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	return &BatchProcessor[T]{
		batchSize:    batchSize,
		maxWaitTime:  maxWaitTime,
		maxQueueSize: maxQueueSize,
		processFn:    processFn,
		queue:        make(chan T, maxQueueSize),
		batch:        make([]T, 0, batchSize),
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start starts the batch processor
func (bp *BatchProcessor[T]) Start() error {
	bp.wg.Add(1)
	go bp.processingLoop()
	return nil
}

// Stop stops the batch processor
func (bp *BatchProcessor[T]) Stop() error {
	// Signal shutdown
	bp.cancel()
	
	// Close queue
	close(bp.queue)
	
	// Wait for processing to complete
	bp.wg.Wait()
	
	// Process any remaining items
	bp.batchMutex.Lock()
	if len(bp.batch) > 0 {
		bp.processBatch(bp.batch)
		bp.batch = bp.batch[:0]
	}
	bp.batchMutex.Unlock()
	
	return nil
}

// Submit submits an item for processing
func (bp *BatchProcessor[T]) Submit(item T) error {
	select {
	case bp.queue <- item:
		return nil
	default:
		bp.dropped.Add(1)
		return errors.New("queue full")
	}
}

// SubmitBatch submits multiple items
func (bp *BatchProcessor[T]) SubmitBatch(items []T) error {
	dropped := 0
	for _, item := range items {
		select {
		case bp.queue <- item:
		default:
			dropped++
		}
	}
	
	if dropped > 0 {
		bp.dropped.Add(uint64(dropped))
		return errors.New("some items dropped")
	}
	
	return nil
}

// processingLoop is the main processing loop
func (bp *BatchProcessor[T]) processingLoop() {
	defer bp.wg.Done()
	
	timer := time.NewTimer(bp.maxWaitTime)
	defer timer.Stop()
	
	for {
		select {
		case <-bp.ctx.Done():
			return
			
		case item, ok := <-bp.queue:
			if !ok {
				// Queue closed
				return
			}
			
			bp.batchMutex.Lock()
			bp.batch = append(bp.batch, item)
			
			if len(bp.batch) >= bp.batchSize {
				// Batch full, process immediately
				batch := bp.batch
				bp.batch = make([]T, 0, bp.batchSize)
				bp.batchMutex.Unlock()
				
				bp.processBatch(batch)
				
				// Reset timer
				if !timer.Stop() {
					<-timer.C
				}
				timer.Reset(bp.maxWaitTime)
			} else {
				bp.batchMutex.Unlock()
			}
			
		case <-timer.C:
			// Timeout reached, process current batch
			bp.batchMutex.Lock()
			if len(bp.batch) > 0 {
				batch := bp.batch
				bp.batch = make([]T, 0, bp.batchSize)
				bp.batchMutex.Unlock()
				
				bp.processBatch(batch)
			} else {
				bp.batchMutex.Unlock()
			}
			
			timer.Reset(bp.maxWaitTime)
		}
	}
}

// processBatch processes a batch of items
func (bp *BatchProcessor[T]) processBatch(batch []T) {
	if len(batch) == 0 {
		return
	}
	
	bp.batches.Add(1)
	
	// Update average batch size (exponential moving average)
	oldAvg := bp.avgBatchSize.Load()
	newAvg := (oldAvg*9 + uint64(len(batch))) / 10
	bp.avgBatchSize.Store(newAvg)
	
	// Process the batch
	if err := bp.processFn(bp.ctx, batch); err != nil {
		bp.errors.Add(1)
	} else {
		bp.processed.Add(uint64(len(batch)))
	}
}

// GetMetrics returns processor metrics
func (bp *BatchProcessor[T]) GetMetrics() BatchProcessorMetrics {
	return BatchProcessorMetrics{
		Processed:    bp.processed.Load(),
		Batches:      bp.batches.Load(),
		Dropped:      bp.dropped.Load(),
		Errors:       bp.errors.Load(),
		QueueSize:    len(bp.queue),
		AvgBatchSize: bp.avgBatchSize.Load(),
	}
}

// BatchProcessorMetrics contains batch processor metrics
type BatchProcessorMetrics struct {
	Processed    uint64
	Batches      uint64
	Dropped      uint64
	Errors       uint64
	QueueSize    int
	AvgBatchSize uint64
}

// AdaptiveBatchProcessor adjusts batch size based on load
type AdaptiveBatchProcessor[T any] struct {
	*BatchProcessor[T]
	
	// Adaptive settings
	minBatchSize      int
	maxBatchSize      int
	targetLatency     time.Duration
	adaptInterval     time.Duration
	
	// Metrics for adaptation
	lastAdapt         time.Time
	totalLatency      atomic.Uint64
	processedSinceAdapt atomic.Uint64
}

// NewAdaptiveBatchProcessor creates an adaptive batch processor
func NewAdaptiveBatchProcessor[T any](
	minBatchSize, maxBatchSize int,
	targetLatency time.Duration,
	processFn BatchProcessFunc[T],
) *AdaptiveBatchProcessor[T] {
	if minBatchSize <= 0 {
		minBatchSize = 10
	}
	if maxBatchSize <= minBatchSize {
		maxBatchSize = minBatchSize * 10
	}
	if targetLatency <= 0 {
		targetLatency = 50 * time.Millisecond
	}
	
	// Start with middle batch size
	initialBatchSize := (minBatchSize + maxBatchSize) / 2
	
	// Create AdaptiveBatchProcessor first
	abp := &AdaptiveBatchProcessor[T]{
		minBatchSize:     minBatchSize,
		maxBatchSize:     maxBatchSize,
		targetLatency:    targetLatency,
		adaptInterval:    10 * time.Second,
		lastAdapt:        time.Now(),
	}
	
	// Wrap process function to measure latency
	wrappedFn := func(ctx context.Context, batch []T) error {
		start := time.Now()
		err := processFn(ctx, batch)
		elapsed := time.Since(start)
		
		// Update latency metrics
		abp.totalLatency.Add(uint64(elapsed))
		abp.processedSinceAdapt.Add(uint64(len(batch)))
		
		return err
	}
	
	bp := NewBatchProcessor(initialBatchSize, targetLatency/2, maxBatchSize*10, wrappedFn)
	
	// Set the BatchProcessor field
	abp.BatchProcessor = bp
	
	return abp
}

// Start starts the adaptive batch processor
func (abp *AdaptiveBatchProcessor[T]) Start() error {
	// Start adaptation loop
	abp.wg.Add(1)
	go abp.adaptationLoop()
	
	// Start base processor
	return abp.BatchProcessor.Start()
}

// adaptationLoop adjusts batch size based on performance
func (abp *AdaptiveBatchProcessor[T]) adaptationLoop() {
	defer abp.wg.Done()
	
	ticker := time.NewTicker(abp.adaptInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-abp.ctx.Done():
			return
		case <-ticker.C:
			abp.adapt()
		}
	}
}

// adapt adjusts the batch size
func (abp *AdaptiveBatchProcessor[T]) adapt() {
	processed := abp.processedSinceAdapt.Swap(0)
	totalLatencyNs := abp.totalLatency.Swap(0)
	
	if processed == 0 {
		return
	}
	
	// Calculate average latency per item
	avgLatency := time.Duration(totalLatencyNs / processed)
	
	// Adjust batch size
	currentBatchSize := abp.batchSize
	newBatchSize := currentBatchSize
	
	if avgLatency > abp.targetLatency {
		// Latency too high, reduce batch size
		newBatchSize = currentBatchSize * 9 / 10
	} else if avgLatency < abp.targetLatency/2 {
		// Latency low, increase batch size
		newBatchSize = currentBatchSize * 11 / 10
	}
	
	// Apply limits
	if newBatchSize < abp.minBatchSize {
		newBatchSize = abp.minBatchSize
	} else if newBatchSize > abp.maxBatchSize {
		newBatchSize = abp.maxBatchSize
	}
	
	// Update batch size if changed
	if newBatchSize != currentBatchSize {
		abp.batchMutex.Lock()
		abp.batchSize = newBatchSize
		abp.batchMutex.Unlock()
	}
	
	abp.lastAdapt = time.Now()
}

// ParallelBatchProcessor processes batches in parallel
type ParallelBatchProcessor[T any] struct {
	processors []*BatchProcessor[T]
	numWorkers int
	router     func(T) int // Route items to specific processors
	
	// Metrics
	totalProcessed atomic.Uint64
	totalDropped   atomic.Uint64
}

// NewParallelBatchProcessor creates a parallel batch processor
func NewParallelBatchProcessor[T any](
	numWorkers int,
	batchSize int,
	maxWaitTime time.Duration,
	processFn BatchProcessFunc[T],
	router func(T) int,
) *ParallelBatchProcessor[T] {
	if numWorkers <= 0 {
		numWorkers = runtime.NumCPU()
	}
	
	processors := make([]*BatchProcessor[T], numWorkers)
	for i := range processors {
		processors[i] = NewBatchProcessor(batchSize, maxWaitTime, batchSize*100, processFn)
	}
	
	if router == nil {
		// Default round-robin routing
		var counter atomic.Uint64
		router = func(T) int {
			return int(counter.Add(1) % uint64(numWorkers))
		}
	}
	
	return &ParallelBatchProcessor[T]{
		processors: processors,
		numWorkers: numWorkers,
		router:     router,
	}
}

// Start starts all processors
func (pbp *ParallelBatchProcessor[T]) Start() error {
	for _, p := range pbp.processors {
		if err := p.Start(); err != nil {
			// Stop already started processors
			for _, started := range pbp.processors {
				if started == p {
					break
				}
				started.Stop()
			}
			return err
		}
	}
	return nil
}

// Stop stops all processors
func (pbp *ParallelBatchProcessor[T]) Stop() error {
	var firstErr error
	for _, p := range pbp.processors {
		if err := p.Stop(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Submit routes an item to appropriate processor
func (pbp *ParallelBatchProcessor[T]) Submit(item T) error {
	idx := pbp.router(item) % pbp.numWorkers
	if err := pbp.processors[idx].Submit(item); err != nil {
		pbp.totalDropped.Add(1)
		return err
	}
	pbp.totalProcessed.Add(1)
	return nil
}

// GetMetrics returns aggregated metrics
func (pbp *ParallelBatchProcessor[T]) GetMetrics() ParallelBatchProcessorMetrics {
	metrics := ParallelBatchProcessorMetrics{
		TotalProcessed: pbp.totalProcessed.Load(),
		TotalDropped:   pbp.totalDropped.Load(),
		WorkerMetrics:  make([]BatchProcessorMetrics, pbp.numWorkers),
	}
	
	for i, p := range pbp.processors {
		metrics.WorkerMetrics[i] = p.GetMetrics()
	}
	
	return metrics
}

// ParallelBatchProcessorMetrics contains parallel processor metrics
type ParallelBatchProcessorMetrics struct {
	TotalProcessed uint64
	TotalDropped   uint64
	WorkerMetrics  []BatchProcessorMetrics
}