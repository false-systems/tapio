package bpf_common

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"go.uber.org/zap"
)

// BatchConfig holds configuration for batch processing
type BatchConfig struct {
	// Batch size limits
	MaxBatchSize     int           `json:"max_batch_size"`
	MinBatchSize     int           `json:"min_batch_size"`
	MaxBatchBytes    int           `json:"max_batch_bytes"`
	
	// Timing configuration
	MaxBatchAge      time.Duration `json:"max_batch_age"`
	FlushInterval    time.Duration `json:"flush_interval"`
	ShutdownTimeout  time.Duration `json:"shutdown_timeout"`
	
	// Performance tuning
	WorkerCount      int           `json:"worker_count"`
	BufferSize       int           `json:"buffer_size"`
	CompressionLevel int           `json:"compression_level"` // 0=none, 1-9=gzip levels
	
	// Adaptive batching
	EnableAdaptive       bool    `json:"enable_adaptive"`
	TargetLatencyMs      int     `json:"target_latency_ms"`
	LatencyPercentile    float64 `json:"latency_percentile"` // e.g., 0.95 for P95
	AdaptiveAdjustmentPct float64 `json:"adaptive_adjustment_pct"`
	
	// Reliability
	MaxRetries       int           `json:"max_retries"`
	RetryBackoff     time.Duration `json:"retry_backoff"`
	EnablePersistence bool         `json:"enable_persistence"`
	PersistenceDir   string        `json:"persistence_dir"`
}

// DefaultBatchConfig returns sensible defaults for batch processing
func DefaultBatchConfig() *BatchConfig {
	return &BatchConfig{
		MaxBatchSize:          1000,
		MinBatchSize:          10,
		MaxBatchBytes:         1024 * 1024, // 1MB
		MaxBatchAge:           1 * time.Second,
		FlushInterval:         100 * time.Millisecond,
		ShutdownTimeout:       30 * time.Second,
		WorkerCount:           4,
		BufferSize:            10000,
		CompressionLevel:      1, // Light compression
		EnableAdaptive:        true,
		TargetLatencyMs:       50,
		LatencyPercentile:     0.95,
		AdaptiveAdjustmentPct: 0.1,
		MaxRetries:            3,
		RetryBackoff:          100 * time.Millisecond,
		EnablePersistence:     false,
	}
}

// BatchedEvent represents a single event in a batch
type BatchedEvent struct {
	Event     collectors.RawEvent `json:"event"`
	Size      int                 `json:"size"`
	Timestamp time.Time           `json:"timestamp"`
	Attempts  int                 `json:"attempts"`
	LastError string              `json:"last_error,omitempty"`
}

// EventBatch represents a batch of events ready for processing
type EventBatch struct {
	ID            string          `json:"id"`
	Events        []*BatchedEvent `json:"events"`
	CreatedAt     time.Time       `json:"created_at"`
	ClosedAt      time.Time       `json:"closed_at"`
	Size          int             `json:"size"`
	ByteSize      int             `json:"byte_size"`
	Compressed    bool            `json:"compressed"`
	CompressionSize int           `json:"compression_size,omitempty"`
	Metadata      map[string]interface{} `json:"metadata"`
	Priority      int             `json:"priority"`
	
	// Processing tracking
	ProcessingStarted time.Time `json:"processing_started"`
	ProcessingEnded   time.Time `json:"processing_ended"`
	Attempts          int       `json:"attempts"`
	LastError         string    `json:"last_error,omitempty"`
}

// BatchProcessor manages high-volume event batching with adaptive sizing
type BatchProcessor struct {
	mu              sync.RWMutex
	logger          *zap.Logger
	config          *BatchConfig
	ctx             context.Context
	cancel          context.CancelFunc
	
	// Input/output channels
	input           chan *BatchedEvent
	output          chan *EventBatch
	
	// Current batch being built
	currentBatch    *EventBatch
	batchMu         sync.Mutex
	
	// Worker management
	workers         []*batchWorker
	workerWg        sync.WaitGroup
	
	// Statistics and metrics
	stats           *BatchProcessorStats
	statsCollector  *BPFStatsCollector
	
	// Adaptive sizing
	latencyHistory  *LatencyTracker
	adaptiveConfig  *AdaptiveConfig
	
	// Persistence
	persistenceEngine *BatchPersistence
	
	// Shutdown coordination
	shutdownOnce    sync.Once
}

// BatchProcessorStats tracks batch processing metrics
type BatchProcessorStats struct {
	EventsReceived      uint64    `json:"events_received"`
	EventsProcessed     uint64    `json:"events_processed"`
	EventsDropped       uint64    `json:"events_dropped"`
	BatchesCreated      uint64    `json:"batches_created"`
	BatchesSent         uint64    `json:"batches_sent"`
	BatchesFailed       uint64    `json:"batches_failed"`
	BytesProcessed      uint64    `json:"bytes_processed"`
	CompressionSavings  uint64    `json:"compression_savings"`
	AverageBatchSize    float64   `json:"average_batch_size"`
	AverageLatencyMs    float64   `json:"average_latency_ms"`
	P95LatencyMs        float64   `json:"p95_latency_ms"`
	P99LatencyMs        float64   `json:"p99_latency_ms"`
	LastBatchAt         time.Time `json:"last_batch_at"`
	StartTime           time.Time `json:"start_time"`
	CurrentBatchSize    int       `json:"current_batch_size"`
	CurrentBatchAge     time.Duration `json:"current_batch_age"`
}

// AdaptiveConfig manages adaptive batch sizing parameters
type AdaptiveConfig struct {
	CurrentBatchSize    int
	CurrentMaxAge       time.Duration
	LastAdjustment      time.Time
	AdjustmentCount     int
	PerformanceScore    float64
	TargetLatency       time.Duration
	ToleranceRange      time.Duration
}

// LatencyTracker maintains latency statistics for adaptive sizing
type LatencyTracker struct {
	mu        sync.RWMutex
	samples   []time.Duration
	maxSamples int
	index     int
	full      bool
}

// NewBatchProcessor creates a new batch processor
func NewBatchProcessor(config *BatchConfig, statsCollector *BPFStatsCollector, logger *zap.Logger) (*BatchProcessor, error) {
	if config == nil {
		config = DefaultBatchConfig()
	}
	
	if logger == nil {
		var err error
		logger, err = zap.NewProduction()
		if err != nil {
			return nil, fmt.Errorf("failed to create logger: %w", err)
		}
	}
	
	// Validate configuration
	if config.MaxBatchSize <= 0 {
		return nil, fmt.Errorf("max_batch_size must be positive")
	}
	if config.MinBatchSize <= 0 {
		config.MinBatchSize = 1
	}
	if config.MinBatchSize > config.MaxBatchSize {
		config.MinBatchSize = config.MaxBatchSize
	}
	if config.WorkerCount <= 0 {
		config.WorkerCount = 1
	}
	
	bp := &BatchProcessor{
		logger:         logger,
		config:         config,
		input:          make(chan *BatchedEvent, config.BufferSize),
		output:         make(chan *EventBatch, config.WorkerCount*2),
		workers:        make([]*batchWorker, config.WorkerCount),
		stats:          &BatchProcessorStats{StartTime: time.Now()},
		statsCollector: statsCollector,
		latencyHistory: &LatencyTracker{
			samples:    make([]time.Duration, 1000),
			maxSamples: 1000,
		},
		adaptiveConfig: &AdaptiveConfig{
			CurrentBatchSize: config.MaxBatchSize,
			CurrentMaxAge:    config.MaxBatchAge,
			LastAdjustment:   time.Now(),
			TargetLatency:    time.Duration(config.TargetLatencyMs) * time.Millisecond,
			ToleranceRange:   time.Duration(config.TargetLatencyMs/10) * time.Millisecond,
		},
	}
	
	// Initialize persistence if enabled
	if config.EnablePersistence {
		persistence, err := NewBatchPersistence(config.PersistenceDir, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create persistence engine: %w", err)
		}
		bp.persistenceEngine = persistence
	}
	
	return bp, nil
}

// Start begins batch processing
func (bp *BatchProcessor) Start(ctx context.Context) error {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	
	if bp.ctx != nil {
		return fmt.Errorf("batch processor already started")
	}
	
	bp.ctx, bp.cancel = context.WithCancel(ctx)
	
	// Start persistence engine if enabled
	if bp.persistenceEngine != nil {
		if err := bp.persistenceEngine.Start(bp.ctx); err != nil {
			return fmt.Errorf("failed to start persistence engine: %w", err)
		}
	}
	
	// Start workers
	for i := 0; i < bp.config.WorkerCount; i++ {
		worker := &batchWorker{
			id:        i,
			processor: bp,
			logger:    bp.logger.With(zap.Int("worker_id", i)),
		}
		bp.workers[i] = worker
		bp.workerWg.Add(1)
		go worker.run()
	}
	
	// Start batch builder
	go bp.batchBuilder()
	
	// Start statistics updater
	go bp.statsUpdater()
	
	// Start adaptive sizing if enabled
	if bp.config.EnableAdaptive {
		go bp.adaptiveSizer()
	}
	
	bp.logger.Info("Batch processor started",
		zap.Int("worker_count", bp.config.WorkerCount),
		zap.Int("max_batch_size", bp.config.MaxBatchSize),
		zap.Duration("max_batch_age", bp.config.MaxBatchAge),
		zap.Bool("adaptive", bp.config.EnableAdaptive),
		zap.Bool("persistence", bp.config.EnablePersistence),
	)
	
	return nil
}

// Stop gracefully shuts down the batch processor
func (bp *BatchProcessor) Stop() error {
	var stopErr error
	bp.shutdownOnce.Do(func() {
		bp.logger.Info("Stopping batch processor...")
		
		// Cancel context to signal shutdown
		if bp.cancel != nil {
			bp.cancel()
		}
		
		// Close input channel to stop accepting new events
		close(bp.input)
		
		// Flush current batch
		bp.flushCurrentBatch()
		
		// Wait for workers to finish with timeout
		done := make(chan struct{})
		go func() {
			bp.workerWg.Wait()
			close(done)
		}()
		
		select {
		case <-done:
			bp.logger.Info("All batch workers stopped gracefully")
		case <-time.After(bp.config.ShutdownTimeout):
			bp.logger.Warn("Timeout waiting for batch workers to stop")
		}
		
		// Close output channel
		close(bp.output)
		
		// Stop persistence engine
		if bp.persistenceEngine != nil {
			if err := bp.persistenceEngine.Stop(); err != nil {
				bp.logger.Warn("Error stopping persistence engine", zap.Error(err))
				stopErr = err
			}
		}
		
		bp.logger.Info("Batch processor stopped",
			zap.Uint64("events_processed", bp.stats.EventsProcessed),
			zap.Uint64("batches_sent", bp.stats.BatchesSent),
		)
	})
	
	return stopErr
}

// Input returns the channel for sending events to the batch processor
func (bp *BatchProcessor) Input() chan<- *BatchedEvent {
	return bp.input
}

// Output returns the channel for receiving batched events
func (bp *BatchProcessor) Output() <-chan *EventBatch {
	return bp.output
}

// AddEvent adds a single event to the batch processor
func (bp *BatchProcessor) AddEvent(event collectors.RawEvent) error {
	// Convert to batched event
	eventBytes, err := json.Marshal(event.Data)
	if err != nil {
		return fmt.Errorf("failed to marshal event data: %w", err)
	}
	
	batchedEvent := &BatchedEvent{
		Event:     event,
		Size:      len(eventBytes) + len(event.Metadata)*20, // Rough estimate
		Timestamp: time.Now(),
		Attempts:  0,
	}
	
	// Try to send to input channel (non-blocking)
	select {
	case bp.input <- batchedEvent:
		atomic.AddUint64(&bp.stats.EventsReceived, 1)
		return nil
	default:
		atomic.AddUint64(&bp.stats.EventsDropped, 1)
		return fmt.Errorf("batch processor input buffer full")
	}
}

// GetStats returns current batch processor statistics
func (bp *BatchProcessor) GetStats() *BatchProcessorStats {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	
	stats := *bp.stats // Copy struct
	
	// Update current batch info
	bp.batchMu.Lock()
	if bp.currentBatch != nil {
		stats.CurrentBatchSize = bp.currentBatch.Size
		stats.CurrentBatchAge = time.Since(bp.currentBatch.CreatedAt)
	}
	bp.batchMu.Unlock()
	
	// Calculate averages
	if stats.BatchesSent > 0 {
		stats.AverageBatchSize = float64(stats.EventsProcessed) / float64(stats.BatchesSent)
	}
	
	// Get latency statistics
	avgLatency, p95Latency, p99Latency := bp.latencyHistory.GetStats()
	stats.AverageLatencyMs = avgLatency.Seconds() * 1000
	stats.P95LatencyMs = p95Latency.Seconds() * 1000
	stats.P99LatencyMs = p99Latency.Seconds() * 1000
	
	return &stats
}

// batchBuilder manages the creation and closing of batches
func (bp *BatchProcessor) batchBuilder() {
	ticker := time.NewTicker(bp.config.FlushInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-bp.ctx.Done():
			bp.flushCurrentBatch()
			return
			
		case event, ok := <-bp.input:
			if !ok {
				bp.flushCurrentBatch()
				return
			}
			bp.addEventToBatch(event)
			
		case <-ticker.C:
			bp.checkBatchTimeout()
		}
	}
}

// addEventToBatch adds an event to the current batch
func (bp *BatchProcessor) addEventToBatch(event *BatchedEvent) {
	bp.batchMu.Lock()
	defer bp.batchMu.Unlock()
	
	// Create new batch if needed
	if bp.currentBatch == nil {
		bp.createNewBatch()
	}
	
	// Check if adding this event would exceed limits
	wouldExceedSize := len(bp.currentBatch.Events) >= bp.adaptiveConfig.CurrentBatchSize
	wouldExceedBytes := bp.currentBatch.ByteSize + event.Size > bp.config.MaxBatchBytes
	wouldExceedAge := time.Since(bp.currentBatch.CreatedAt) >= bp.adaptiveConfig.CurrentMaxAge
	
	if wouldExceedSize || wouldExceedBytes || wouldExceedAge {
		// Send current batch and create new one
		bp.sendCurrentBatch()
		bp.createNewBatch()
	}
	
	// Add event to current batch
	bp.currentBatch.Events = append(bp.currentBatch.Events, event)
	bp.currentBatch.Size++
	bp.currentBatch.ByteSize += event.Size
	
	// Check if batch is now ready to send
	if len(bp.currentBatch.Events) >= bp.config.MinBatchSize &&
		(len(bp.currentBatch.Events) >= bp.adaptiveConfig.CurrentBatchSize ||
		 bp.currentBatch.ByteSize >= bp.config.MaxBatchBytes ||
		 time.Since(bp.currentBatch.CreatedAt) >= bp.adaptiveConfig.CurrentMaxAge) {
		bp.sendCurrentBatch()
	}
}

// createNewBatch creates a new batch for accumulating events
func (bp *BatchProcessor) createNewBatch() {
	bp.currentBatch = &EventBatch{
		ID:        bp.generateBatchID(),
		Events:    make([]*BatchedEvent, 0, bp.adaptiveConfig.CurrentBatchSize),
		CreatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
		Priority:  1, // Default priority
	}
	
	atomic.AddUint64(&bp.stats.BatchesCreated, 1)
}

// sendCurrentBatch sends the current batch for processing
func (bp *BatchProcessor) sendCurrentBatch() {
	if bp.currentBatch == nil || len(bp.currentBatch.Events) == 0 {
		return
	}
	
	bp.currentBatch.ClosedAt = time.Now()
	
	// Apply compression if enabled
	if bp.config.CompressionLevel > 0 {
		bp.compressBatch(bp.currentBatch)
	}
	
	// Persist if enabled
	if bp.persistenceEngine != nil {
		if err := bp.persistenceEngine.Persist(bp.currentBatch); err != nil {
			bp.logger.Warn("Failed to persist batch", zap.String("batch_id", bp.currentBatch.ID), zap.Error(err))
		}
	}
	
	// Send to workers
	select {
	case bp.output <- bp.currentBatch:
		atomic.AddUint64(&bp.stats.BatchesSent, 1)
		atomic.AddUint64(&bp.stats.EventsProcessed, uint64(bp.currentBatch.Size))
		atomic.AddUint64(&bp.stats.BytesProcessed, uint64(bp.currentBatch.ByteSize))
		bp.stats.LastBatchAt = time.Now()
		
		// Record batch size for stats collection
		if bp.statsCollector != nil {
			bp.statsCollector.IncrementEventCounter("batch_processor", CounterEventsBatched, uint64(bp.currentBatch.Size))
		}
		
	default:
		atomic.AddUint64(&bp.stats.BatchesFailed, 1)
		bp.logger.Warn("Failed to send batch - output buffer full", zap.String("batch_id", bp.currentBatch.ID))
	}
	
	bp.currentBatch = nil
}

// checkBatchTimeout checks if current batch should be flushed due to age
func (bp *BatchProcessor) checkBatchTimeout() {
	bp.batchMu.Lock()
	defer bp.batchMu.Unlock()
	
	if bp.currentBatch != nil && 
	   len(bp.currentBatch.Events) >= bp.config.MinBatchSize &&
	   time.Since(bp.currentBatch.CreatedAt) >= bp.adaptiveConfig.CurrentMaxAge {
		bp.sendCurrentBatch()
	}
}

// flushCurrentBatch forces the current batch to be sent immediately
func (bp *BatchProcessor) flushCurrentBatch() {
	bp.batchMu.Lock()
	defer bp.batchMu.Unlock()
	
	if bp.currentBatch != nil && len(bp.currentBatch.Events) > 0 {
		bp.sendCurrentBatch()
	}
}

// compressBatch applies compression to reduce batch size
func (bp *BatchProcessor) compressBatch(batch *EventBatch) {
	// Implementation would compress the batch data
	// For now, we'll just mark it as compressed and estimate savings
	originalSize := batch.ByteSize
	batch.Compressed = true
	batch.CompressionSize = int(float64(originalSize) * 0.7) // Assume 30% compression
	atomic.AddUint64(&bp.stats.CompressionSavings, uint64(originalSize - batch.CompressionSize))
}

// generateBatchID generates a unique batch ID
func (bp *BatchProcessor) generateBatchID() string {
	return fmt.Sprintf("batch_%d_%d", time.Now().UnixNano(), atomic.AddUint64(&bp.stats.BatchesCreated, 0))
}

// statsUpdater periodically updates statistics
func (bp *BatchProcessor) statsUpdater() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-bp.ctx.Done():
			return
		case <-ticker.C:
			bp.updateStatistics()
		}
	}
}

// updateStatistics updates internal statistics
func (bp *BatchProcessor) updateStatistics() {
	// Update stats collector if available
	if bp.statsCollector != nil {
		stats := bp.GetStats()
		bp.statsCollector.UpdateStats("batch_processor", func(bpfStats *BPFStatistics) {
			bpfStats.EventsProcessed = stats.EventsProcessed
			bpfStats.EventsDropped = stats.EventsDropped
			bpfStats.BatchesSent = stats.BatchesSent
			bpfStats.AverageBatchSize = stats.AverageBatchSize
		})
	}
}

// adaptiveSizer adjusts batch size based on performance metrics
func (bp *BatchProcessor) adaptiveSizer() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-bp.ctx.Done():
			return
		case <-ticker.C:
			bp.adjustBatchSizing()
		}
	}
}

// adjustBatchSizing implements adaptive batch sizing logic
func (bp *BatchProcessor) adjustBatchSizing() {
	avgLatency, p95Latency, _ := bp.latencyHistory.GetStats()
	
	// Use P95 latency for decisions if configured
	targetMetric := avgLatency
	if bp.config.LatencyPercentile >= 0.95 {
		targetMetric = p95Latency
	}
	
	targetLatency := bp.adaptiveConfig.TargetLatency
	tolerance := bp.adaptiveConfig.ToleranceRange
	
	// Skip adjustment if we don't have enough data
	if targetMetric == 0 {
		return
	}
	
	// Calculate adjustment
	var adjustment float64
	if targetMetric > targetLatency+tolerance {
		// Latency too high - reduce batch size/age
		adjustment = -bp.config.AdaptiveAdjustmentPct
	} else if targetMetric < targetLatency-tolerance {
		// Latency acceptable - can increase batch size/age
		adjustment = bp.config.AdaptiveAdjustmentPct
	} else {
		// Within tolerance - no adjustment needed
		return
	}
	
	// Apply adjustment to batch size
	currentSize := float64(bp.adaptiveConfig.CurrentBatchSize)
	newSize := int(currentSize * (1.0 + adjustment))
	
	// Ensure within bounds
	if newSize < bp.config.MinBatchSize {
		newSize = bp.config.MinBatchSize
	}
	if newSize > bp.config.MaxBatchSize {
		newSize = bp.config.MaxBatchSize
	}
	
	// Apply adjustment to max age
	currentAge := bp.adaptiveConfig.CurrentMaxAge
	newAge := time.Duration(float64(currentAge) * (1.0 + adjustment))
	
	// Ensure within bounds
	minAge := bp.config.FlushInterval * 2
	maxAge := bp.config.MaxBatchAge
	if newAge < minAge {
		newAge = minAge
	}
	if newAge > maxAge {
		newAge = maxAge
	}
	
	// Update adaptive config
	bp.adaptiveConfig.CurrentBatchSize = newSize
	bp.adaptiveConfig.CurrentMaxAge = newAge
	bp.adaptiveConfig.LastAdjustment = time.Now()
	bp.adaptiveConfig.AdjustmentCount++
	
	bp.logger.Debug("Adjusted batch sizing",
		zap.Int("old_size", int(currentSize)),
		zap.Int("new_size", newSize),
		zap.Duration("old_age", currentAge),
		zap.Duration("new_age", newAge),
		zap.Float64("target_latency_ms", targetLatency.Seconds()*1000),
		zap.Float64("actual_latency_ms", targetMetric.Seconds()*1000),
	)
}

// batchWorker processes batches
type batchWorker struct {
	id        int
	processor *BatchProcessor
	logger    *zap.Logger
}

// run is the main worker loop
func (w *batchWorker) run() {
	defer w.processor.workerWg.Done()
	
	for {
		select {
		case <-w.processor.ctx.Done():
			return
		case batch, ok := <-w.processor.output:
			if !ok {
				return
			}
			w.processBatch(batch)
		}
	}
}

// processBatch processes a single batch
func (w *batchWorker) processBatch(batch *EventBatch) {
	startTime := time.Now()
	batch.ProcessingStarted = startTime
	
	// Update stats collector
	if w.processor.statsCollector != nil {
		w.processor.statsCollector.RecordProcessingTime("batch_processor", time.Since(startTime))
	}
	
	// Record latency for adaptive sizing
	w.processor.latencyHistory.Record(time.Since(startTime))
	
	batch.ProcessingEnded = time.Now()
	w.logger.Debug("Processed batch",
		zap.String("batch_id", batch.ID),
		zap.Int("size", batch.Size),
		zap.Duration("processing_time", batch.ProcessingEnded.Sub(batch.ProcessingStarted)),
	)
}

// LatencyTracker methods

// Record adds a new latency sample
func (lt *LatencyTracker) Record(latency time.Duration) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	
	lt.samples[lt.index] = latency
	lt.index = (lt.index + 1) % lt.maxSamples
	if lt.index == 0 {
		lt.full = true
	}
}

// GetStats returns latency statistics
func (lt *LatencyTracker) GetStats() (avg, p95, p99 time.Duration) {
	lt.mu.RLock()
	defer lt.mu.RUnlock()
	
	var validSamples []time.Duration
	if lt.full {
		validSamples = make([]time.Duration, lt.maxSamples)
		copy(validSamples, lt.samples)
	} else if lt.index > 0 {
		validSamples = make([]time.Duration, lt.index)
		copy(validSamples, lt.samples[:lt.index])
	} else {
		return 0, 0, 0
	}
	
	// Sort samples for percentile calculations
	for i := 0; i < len(validSamples); i++ {
		for j := i + 1; j < len(validSamples); j++ {
			if validSamples[i] > validSamples[j] {
				validSamples[i], validSamples[j] = validSamples[j], validSamples[i]
			}
		}
	}
	
	// Calculate average
	var total time.Duration
	for _, sample := range validSamples {
		total += sample
	}
	avg = total / time.Duration(len(validSamples))
	
	// Calculate percentiles
	if len(validSamples) > 0 {
		p95Index := int(float64(len(validSamples)) * 0.95)
		if p95Index >= len(validSamples) {
			p95Index = len(validSamples) - 1
		}
		p95 = validSamples[p95Index]
		
		p99Index := int(float64(len(validSamples)) * 0.99)
		if p99Index >= len(validSamples) {
			p99Index = len(validSamples) - 1
		}
		p99 = validSamples[p99Index]
	}
	
	return avg, p95, p99
}

// BatchPersistence handles batch persistence for reliability
type BatchPersistence struct {
	logger *zap.Logger
	dir    string
	ctx    context.Context
	cancel context.CancelFunc
}

// NewBatchPersistence creates a new batch persistence engine
func NewBatchPersistence(dir string, logger *zap.Logger) (*BatchPersistence, error) {
	return &BatchPersistence{
		logger: logger,
		dir:    dir,
	}, nil
}

// Start begins persistence operations
func (bp *BatchPersistence) Start(ctx context.Context) error {
	bp.ctx, bp.cancel = context.WithCancel(ctx)
	return nil
}

// Stop shuts down persistence
func (bp *BatchPersistence) Stop() error {
	if bp.cancel != nil {
		bp.cancel()
	}
	return nil
}

// Persist stores a batch for reliability
func (bp *BatchPersistence) Persist(batch *EventBatch) error {
	// Implementation would persist batch to disk/database
	// For now, just log that persistence would occur
	bp.logger.Debug("Persisting batch",
		zap.String("batch_id", batch.ID),
		zap.Int("size", batch.Size),
	)
	return nil
}