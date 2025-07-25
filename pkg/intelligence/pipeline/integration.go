package pipeline

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// PipelineIntegration manages the integration between ring buffer pipeline and storage
type PipelineIntegration struct {
	// Ring buffer pipeline for high-performance processing
	pipeline IntelligencePipeline

	// Storage for correlation outputs
	correlationStore CorrelationStore

	// In-memory correlation cache for fast lookups
	correlationCache *CorrelationCache

	// Buffer for batching correlation outputs
	outputBuffer *CorrelationBuffer

	// Control
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running bool

	// Configuration
	batchSize     int
	flushInterval time.Duration

	// Metrics
	outputsStored  uint64
	outputsDropped uint64
	cacheHits      uint64
	cacheMisses    uint64
}

// CorrelationCache provides fast in-memory access to recent correlations
type CorrelationCache struct {
	cache      map[string]*CorrelationOutput
	maxSize    int
	accessTime map[string]time.Time
	mu         sync.RWMutex
}

// PipelineIntegrationConfig configures the integration layer
type PipelineIntegrationConfig struct {
	Pipeline         IntelligencePipeline
	CorrelationStore CorrelationStore
	BatchSize        int
	FlushInterval    time.Duration
	CacheSize        int
	BufferSize       uint64
}

// NewPipelineIntegration creates a new pipeline integration
func NewPipelineIntegration(config PipelineIntegrationConfig) (*PipelineIntegration, error) {
	if config.Pipeline == nil {
		return nil, fmt.Errorf("pipeline is required")
	}

	if config.BatchSize <= 0 {
		config.BatchSize = 100
	}

	if config.FlushInterval <= 0 {
		config.FlushInterval = 1 * time.Second
	}

	if config.CacheSize <= 0 {
		config.CacheSize = 1000
	}

	if config.BufferSize <= 0 {
		config.BufferSize = 2048
	}

	outputBuffer := NewCorrelationBuffer(config.BufferSize)

	correlationCache := &CorrelationCache{
		cache:      make(map[string]*CorrelationOutput),
		maxSize:    config.CacheSize,
		accessTime: make(map[string]time.Time),
	}

	return &PipelineIntegration{
		pipeline:         config.Pipeline,
		correlationStore: config.CorrelationStore,
		correlationCache: correlationCache,
		outputBuffer:     outputBuffer,
		batchSize:        config.BatchSize,
		flushInterval:    config.FlushInterval,
	}, nil
}

// Start starts the pipeline integration
func (pi *PipelineIntegration) Start(ctx context.Context) error {
	pi.ctx, pi.cancel = context.WithCancel(ctx)
	pi.running = true

	// Start the ring buffer pipeline
	if err := pi.pipeline.Start(pi.ctx); err != nil {
		return fmt.Errorf("failed to start pipeline: %w", err)
	}

	// Start the correlation output consumer
	pi.wg.Add(1)
	go pi.startCorrelationOutputConsumer()

	// Start the storage writer
	if pi.correlationStore != nil {
		pi.wg.Add(1)
		go pi.startStorageWriter()
	}

	return nil
}

// Stop stops the pipeline integration
func (pi *PipelineIntegration) Stop() error {
	if !pi.running {
		return nil
	}

	pi.running = false
	if pi.cancel != nil {
		pi.cancel()
	}

	// Stop the pipeline
	if err := pi.pipeline.Stop(); err != nil {
		log.Printf("Error stopping pipeline: %v", err)
	}

	// Wait for goroutines to finish
	pi.wg.Wait()

	return nil
}

// ProcessEvent processes an event through the pipeline
func (pi *PipelineIntegration) ProcessEvent(event *domain.UnifiedEvent) error {
	return pi.pipeline.ProcessEvent(event)
}

// ProcessBatch processes multiple events through the pipeline
func (pi *PipelineIntegration) ProcessBatch(events []*domain.UnifiedEvent) error {
	return pi.pipeline.ProcessBatch(events)
}

// startCorrelationOutputConsumer consumes correlation outputs from the pipeline
func (pi *PipelineIntegration) startCorrelationOutputConsumer() {
	defer pi.wg.Done()

	outputs := make([]CorrelationOutput, pi.batchSize)

	for {
		select {
		case <-pi.ctx.Done():
			return
		default:
		}

		// Try to get correlation outputs from pipeline
		count := 0
		if rbPipeline, ok := pi.pipeline.(*RingBufferPipeline); ok {
			count = rbPipeline.GetCorrelationOutputs(outputs)
		}

		if count > 0 {
			// Process each correlation output
			for i := 0; i < count; i++ {
				output := outputs[i]

				// Only buffer significant outputs
				if output.IsSignificant() {
					// Add to output buffer for storage
					if !pi.outputBuffer.Put(output) {
						pi.outputsDropped++
						log.Printf("Correlation output buffer full, dropping output")
					}

					// Update correlation cache
					pi.updateCorrelationCache(&output)
				}
			}
		} else {
			// No outputs available, sleep briefly
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// startStorageWriter writes correlation outputs to persistent storage
func (pi *PipelineIntegration) startStorageWriter() {
	defer pi.wg.Done()

	ticker := time.NewTicker(pi.flushInterval)
	defer ticker.Stop()

	outputs := make([]CorrelationOutput, pi.batchSize)

	for {
		select {
		case <-pi.ctx.Done():
			// Flush remaining outputs before exit
			pi.flushOutputs(outputs)
			return

		case <-ticker.C:
			pi.flushOutputs(outputs)
		}
	}
}

// flushOutputs flushes correlation outputs to storage
func (pi *PipelineIntegration) flushOutputs(outputs []CorrelationOutput) {
	count := pi.outputBuffer.GetBatch(outputs)
	if count == 0 {
		return
	}

	// Convert to slice of pointers for storage interface
	outputPtrs := make([]*CorrelationOutput, count)
	for i := 0; i < count; i++ {
		outputPtrs[i] = &outputs[i]
	}

	// Store in persistent storage
	if pi.correlationStore != nil {
		if err := pi.correlationStore.StoreBatch(outputPtrs); err != nil {
			log.Printf("Failed to store correlation outputs: %v", err)
			pi.outputsDropped += uint64(count)
		} else {
			pi.outputsStored += uint64(count)
		}
	}
}

// updateCorrelationCache updates the in-memory correlation cache
func (pi *PipelineIntegration) updateCorrelationCache(output *CorrelationOutput) {
	pi.correlationCache.mu.Lock()
	defer pi.correlationCache.mu.Unlock()

	// Generate cache key from correlation data
	key := pi.generateCacheKey(output)

	// Add to cache
	pi.correlationCache.cache[key] = output
	pi.correlationCache.accessTime[key] = time.Now()

	// Evict oldest entries if cache is full
	if len(pi.correlationCache.cache) > pi.correlationCache.maxSize {
		pi.evictOldestCacheEntry()
	}
}

// generateCacheKey generates a cache key for a correlation output
func (pi *PipelineIntegration) generateCacheKey(output *CorrelationOutput) string {
	if output.CorrelationData != nil && output.CorrelationData.ID != "" {
		return output.CorrelationData.ID
	}
	// Fallback to event source + type + timestamp
	return fmt.Sprintf("%s_%s_%d",
		output.OriginalEvent.Source,
		output.OriginalEvent.Type,
		output.ProcessedAt.Unix())
}

// evictOldestCacheEntry removes the oldest entry from cache
func (pi *PipelineIntegration) evictOldestCacheEntry() {
	var oldestKey string
	var oldestTime time.Time

	for key, accessTime := range pi.correlationCache.accessTime {
		if oldestKey == "" || accessTime.Before(oldestTime) {
			oldestKey = key
			oldestTime = accessTime
		}
	}

	if oldestKey != "" {
		delete(pi.correlationCache.cache, oldestKey)
		delete(pi.correlationCache.accessTime, oldestKey)
	}
}

// GetCachedCorrelation retrieves a correlation from cache
func (pi *PipelineIntegration) GetCachedCorrelation(key string) (*CorrelationOutput, bool) {
	pi.correlationCache.mu.RLock()
	defer pi.correlationCache.mu.RUnlock()

	output, exists := pi.correlationCache.cache[key]
	if exists {
		pi.correlationCache.accessTime[key] = time.Now()
		pi.cacheHits++
		return output, true
	}

	pi.cacheMisses++
	return nil, false
}

// GetMetrics returns integration metrics
func (pi *PipelineIntegration) GetMetrics() IntegrationMetrics {
	return IntegrationMetrics{
		OutputsStored:     pi.outputsStored,
		OutputsDropped:    pi.outputsDropped,
		CacheHits:         pi.cacheHits,
		CacheMisses:       pi.cacheMisses,
		CacheSize:         uint64(len(pi.correlationCache.cache)),
		BufferUtilization: float64(pi.outputBuffer.Size()) / float64(pi.outputBuffer.capacity),
		PipelineMetrics:   pi.pipeline.GetMetrics(),
	}
}

// IntegrationMetrics contains metrics for the pipeline integration
type IntegrationMetrics struct {
	OutputsStored     uint64
	OutputsDropped    uint64
	CacheHits         uint64
	CacheMisses       uint64
	CacheSize         uint64
	BufferUtilization float64
	PipelineMetrics   PipelineMetrics
}
