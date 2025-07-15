package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

// PrometheusMetricCollector implements MetricCollector with advanced rate limiting and backpressure management
type PrometheusMetricCollector[T MetricType] struct {
	// Dependencies
	logger      *slog.Logger
	rateLimiter *rate.Limiter

	// Configuration
	config CollectorConfig[T]

	// State management
	mu         sync.RWMutex
	running    int32
	shutdown   chan struct{}
	collectors map[string]activeCollector[T]

	// Rate limiting and backpressure
	backpressureHandler *BackpressureHandler[T]
	circuitBreaker      *CircuitBreaker

	// Performance tracking
	stats CollectorStats

	// Worker pool for concurrent collection
	workerPool    *WorkerPool
	resultChannel chan CollectionResult[T]
	batchChannel  chan BatchResult[T]

	// Memory management
	itemPool   sync.Pool
	resultPool sync.Pool
	batchPool  sync.Pool
}

// CollectorConfig configures the metric collector with advanced features
type CollectorConfig[T MetricType] struct {
	// Core collection settings
	CollectorName      string
	CollectionInterval time.Duration
	Timeout            time.Duration

	// Rate limiting
	RateLimit RateLimitSettings

	// Backpressure management
	Backpressure BackpressureSettings

	// Circuit breaker
	CircuitBreaker CircuitBreakerSettings

	// Worker pool
	WorkerPool WorkerPoolSettings

	// Memory management
	MemoryLimit    int64
	MaxConcurrency int
	BufferSize     int
	BatchSize      int

	// Error handling
	ErrorStrategy ErrorHandlingStrategy
	RetryPolicy   RetryPolicySettings

	// Monitoring
	EnableMetrics   bool
	MetricsInterval time.Duration

	// Custom collection function
	CollectionFunc func(context.Context, CollectionOptions) ([]T, error)

	// Filtering and transformation
	FilterFunc     func(T) bool
	TransformFunc  func(T) T
	ValidationFunc func(T) error
}

// activeCollector tracks an active collection instance
type activeCollector[T MetricType] struct {
	id             string
	config         CollectorConfig[T]
	lastRun        time.Time
	runCount       int64
	errorCount     int64
	itemsCollected int64
	enabled        bool
	ticker         *time.Ticker
	context        context.Context
	cancelFunc     context.CancelFunc
}

// BackpressureHandler manages backpressure scenarios
type BackpressureHandler[T MetricType] struct {
	strategy  BackpressureStrategy
	options   BackpressureOptions
	buffer    *RingBuffer[T]
	stats     BackpressureStats
	mu        sync.RWMutex
	alertFunc func(BackpressureStats)
}

// CircuitBreaker provides circuit breaker functionality for resilient collection
type CircuitBreaker struct {
	mu               sync.RWMutex
	state            CircuitState
	failureCount     int64
	successCount     int64
	lastFailureTime  time.Time
	failureThreshold int64
	successThreshold int64
	timeout          time.Duration
}

// WorkerPool manages concurrent collection workers
type WorkerPool struct {
	workers     int
	taskQueue   chan WorkerTask
	resultQueue chan WorkerResult
	shutdown    chan struct{}
	wg          sync.WaitGroup
}

// RingBuffer provides efficient circular buffer for backpressure handling
type RingBuffer[T MetricType] struct {
	buffer   []T
	head     int64
	tail     int64
	size     int64
	capacity int64
	mu       sync.RWMutex
}

// Supporting types for configuration
type (
	RateLimitSettings struct {
		RequestsPerSecond float64
		BurstSize         int
		Algorithm         string
		Enabled           bool
	}

	BackpressureSettings struct {
		Strategy        BackpressureStrategy
		BufferSize      int
		DropThreshold   float64
		AlertThreshold  float64
		RecoveryTimeout time.Duration
		EnableAlerts    bool
	}

	CircuitBreakerSettings struct {
		FailureThreshold int64
		SuccessThreshold int64
		Timeout          time.Duration
		Enabled          bool
	}

	WorkerPoolSettings struct {
		Size          int
		QueueSize     int
		WorkerTimeout time.Duration
		IdleTimeout   time.Duration
	}

	ErrorHandlingStrategy struct {
		OnError         string // "retry", "skip", "abort"
		MaxRetries      int
		RetryBackoff    time.Duration
		DeadLetterQueue bool
	}

	RetryPolicySettings struct {
		MaxAttempts   int
		InitialDelay  time.Duration
		MaxDelay      time.Duration
		BackoffFactor float64
		JitterEnabled bool
	}

	WorkerTask struct {
		ID        string
		Collector func(context.Context) ([]MetricType, error)
		Context   context.Context
		Timeout   time.Duration
		Metadata  map[string]interface{}
	}

	WorkerResult struct {
		ID       string
		Items    []MetricType
		Error    error
		Duration time.Duration
		Metadata map[string]interface{}
	}
)

// NewPrometheusMetricCollector creates a new metric collector with advanced features
func NewPrometheusMetricCollector[T MetricType](config CollectorConfig[T], logger *slog.Logger) (*PrometheusMetricCollector[T], error) {
	// Validate configuration
	if err := validateCollectorConfig(config); err != nil {
		return nil, fmt.Errorf("invalid collector config: %w", err)
	}

	// Apply defaults
	applyCollectorDefaults(&config)

	if logger == nil {
		logger = slog.Default().With("component", "metric-collector")
	}

	// Create rate limiter
	var rateLimiter *rate.Limiter
	if config.RateLimit.Enabled {
		rateLimiter = rate.NewLimiter(
			rate.Limit(config.RateLimit.RequestsPerSecond),
			config.RateLimit.BurstSize,
		)
	}

	// Create backpressure handler
	backpressureHandler := &BackpressureHandler[T]{
		strategy: config.Backpressure.Strategy,
		options: BackpressureOptions{
			MaxBufferSize:   config.Backpressure.BufferSize,
			DropStrategy:    "oldest",
			AlertThreshold:  config.Backpressure.AlertThreshold,
			RecoveryTimeout: config.Backpressure.RecoveryTimeout,
		},
		buffer: NewRingBuffer[T](config.Backpressure.BufferSize),
	}

	// Create circuit breaker
	var circuitBreaker *CircuitBreaker
	if config.CircuitBreaker.Enabled {
		circuitBreaker = &CircuitBreaker{
			state:            CircuitClosed,
			failureThreshold: config.CircuitBreaker.FailureThreshold,
			successThreshold: config.CircuitBreaker.SuccessThreshold,
			timeout:          config.CircuitBreaker.Timeout,
		}
	}

	// Create worker pool
	workerPool := &WorkerPool{
		workers:     config.WorkerPool.Size,
		taskQueue:   make(chan WorkerTask, config.WorkerPool.QueueSize),
		resultQueue: make(chan WorkerResult, config.WorkerPool.QueueSize),
		shutdown:    make(chan struct{}),
	}

	collector := &PrometheusMetricCollector[T]{
		logger:              logger,
		rateLimiter:         rateLimiter,
		config:              config,
		shutdown:            make(chan struct{}),
		collectors:          make(map[string]activeCollector[T]),
		backpressureHandler: backpressureHandler,
		circuitBreaker:      circuitBreaker,
		workerPool:          workerPool,
		resultChannel:       make(chan CollectionResult[T], config.BufferSize),
		batchChannel:        make(chan BatchResult[T], config.BufferSize/config.BatchSize),
		stats: CollectorStats{
			LastCollection: time.Now(),
		},
	}

	// Initialize object pools for memory efficiency
	collector.initializePools()

	// Start worker pool
	collector.startWorkerPool()

	// Start background tasks
	go collector.runMonitoring()

	atomic.StoreInt32(&collector.running, 1)

	return collector, nil
}

// Collect collects metrics with rate limiting and backpressure
func (c *PrometheusMetricCollector[T]) Collect(ctx context.Context, opts CollectionOptions) (<-chan CollectionResult[T], error) {
	if atomic.LoadInt32(&c.running) == 0 {
		return nil, fmt.Errorf("collector is not running")
	}

	// Apply rate limiting
	if c.rateLimiter != nil {
		if err := c.rateLimiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limit exceeded: %w", err)
		}
	}

	// Check circuit breaker
	if c.circuitBreaker != nil && !c.circuitBreaker.canExecute() {
		return nil, fmt.Errorf("circuit breaker is %s", c.circuitBreaker.getState())
	}

	// Start collection
	go c.performCollection(ctx, opts)

	return c.resultChannel, nil
}

// CollectBatch collects metrics in batches for efficiency
func (c *PrometheusMetricCollector[T]) CollectBatch(ctx context.Context, batchSize int, opts CollectionOptions) (<-chan BatchResult[T], error) {
	if atomic.LoadInt32(&c.running) == 0 {
		return nil, fmt.Errorf("collector is not running")
	}

	if batchSize <= 0 {
		batchSize = c.config.BatchSize
	}

	// Start batch collection
	go c.performBatchCollection(ctx, batchSize, opts)

	return c.batchChannel, nil
}

// SetRateLimit configures collection rate limiting
func (c *PrometheusMetricCollector[T]) SetRateLimit(requestsPerSecond float64, burstSize int) error {
	if requestsPerSecond <= 0 {
		return fmt.Errorf("requests per second must be positive")
	}
	if burstSize <= 0 {
		return fmt.Errorf("burst size must be positive")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.rateLimiter = rate.NewLimiter(rate.Limit(requestsPerSecond), burstSize)
	c.config.RateLimit.RequestsPerSecond = requestsPerSecond
	c.config.RateLimit.BurstSize = burstSize
	c.config.RateLimit.Enabled = true

	c.logger.Info("Rate limit updated",
		"requests_per_second", requestsPerSecond,
		"burst_size", burstSize)

	return nil
}

// SetBackpressure configures backpressure handling
func (c *PrometheusMetricCollector[T]) SetBackpressure(strategy BackpressureStrategy, options BackpressureOptions) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.backpressureHandler.strategy = strategy
	c.backpressureHandler.options = options

	// Resize buffer if needed
	if options.MaxBufferSize != c.backpressureHandler.buffer.capacity {
		c.backpressureHandler.buffer = NewRingBuffer[T](options.MaxBufferSize)
	}

	c.logger.Info("Backpressure configuration updated",
		"strategy", strategy,
		"buffer_size", options.MaxBufferSize,
		"drop_strategy", options.DropStrategy)

	return nil
}

// GetStats returns collection performance statistics
func (c *PrometheusMetricCollector[T]) GetStats() CollectorStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := c.stats

	// Update rate limit from limiter
	if c.rateLimiter != nil {
		stats.RateLimit = float64(c.rateLimiter.Limit())
	}

	// Update backpressure stats
	c.backpressureHandler.mu.RLock()
	stats.BackpressureHits = c.backpressureHandler.stats.DroppedEvents
	c.backpressureHandler.mu.RUnlock()

	return stats
}

// Reset resets collector state and statistics
func (c *PrometheusMetricCollector[T]) Reset() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Reset statistics
	c.stats = CollectorStats{
		LastCollection: time.Now(),
	}

	// Reset backpressure handler
	c.backpressureHandler.mu.Lock()
	c.backpressureHandler.stats = BackpressureStats{}
	c.backpressureHandler.buffer.reset()
	c.backpressureHandler.mu.Unlock()

	// Reset circuit breaker
	if c.circuitBreaker != nil {
		c.circuitBreaker.reset()
	}

	c.logger.Info("Collector state reset")

	return nil
}

// Close gracefully shuts down the collector
func (c *PrometheusMetricCollector[T]) Close(ctx context.Context) error {
	if !atomic.CompareAndSwapInt32(&c.running, 1, 0) {
		return nil // Already stopped
	}

	c.logger.Info("Starting collector shutdown")

	// Signal shutdown
	close(c.shutdown)

	// Stop all active collectors
	c.mu.Lock()
	for id, collector := range c.collectors {
		if collector.cancelFunc != nil {
			collector.cancelFunc()
		}
		if collector.ticker != nil {
			collector.ticker.Stop()
		}
		delete(c.collectors, id)
	}
	c.mu.Unlock()

	// Stop worker pool
	c.stopWorkerPool(ctx)

	// Drain remaining results
	c.drainChannels(ctx)

	c.logger.Info("Collector shutdown completed")

	return nil
}

// Private methods

func (c *PrometheusMetricCollector[T]) performCollection(ctx context.Context, opts CollectionOptions) {
	start := time.Now()

	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	// Execute collection function with circuit breaker protection
	var items []T
	var err error

	if c.circuitBreaker != nil {
		err = c.circuitBreaker.execute(func() error {
			items, err = c.config.CollectionFunc(timeoutCtx, opts)
			return err
		})
	} else {
		items, err = c.config.CollectionFunc(timeoutCtx, opts)
	}

	duration := time.Since(start)

	// Update statistics
	atomic.AddInt64(&c.stats.CollectionCount, 1)
	if err != nil {
		atomic.AddInt64(&c.stats.ErrorCount, 1)
	} else {
		atomic.AddInt64(&c.stats.MetricsCollected, int64(len(items)))
	}

	// Update average latency
	c.updateAverageLatency(duration)

	// Apply filtering and transformation
	if c.config.FilterFunc != nil || c.config.TransformFunc != nil {
		items = c.processItems(items)
	}

	// Handle backpressure if result channel is full
	result := CollectionResult[T]{
		Metrics:   items,
		Error:     err,
		Duration:  duration,
		Timestamp: start,
		Source:    c.config.CollectorName,
		Metadata: map[string]interface{}{
			"collection_count": atomic.LoadInt64(&c.stats.CollectionCount),
			"error_count":      atomic.LoadInt64(&c.stats.ErrorCount),
		},
	}

	select {
	case c.resultChannel <- result:
		// Successfully sent
	default:
		// Channel is full, apply backpressure strategy
		c.handleBackpressure(result)
	}

	c.stats.LastCollection = time.Now()
}

func (c *PrometheusMetricCollector[T]) performBatchCollection(ctx context.Context, batchSize int, opts CollectionOptions) {
	var batch []T
	batchStart := time.Now()
	sequence := atomic.AddInt64(&c.stats.CollectionCount, 1)

	// Collect items until batch is full or context is cancelled
	for len(batch) < batchSize {
		select {
		case <-ctx.Done():
			// Send partial batch if we have items
			if len(batch) > 0 {
				c.sendBatchResult(batch, sequence, batchStart, ctx.Err())
			}
			return
		default:
			// Perform single collection
			items, err := c.config.CollectionFunc(ctx, opts)
			if err != nil {
				atomic.AddInt64(&c.stats.ErrorCount, 1)
				continue
			}

			// Add items to batch
			for _, item := range items {
				if len(batch) >= batchSize {
					break
				}
				batch = append(batch, item)
			}
		}
	}

	// Send complete batch
	c.sendBatchResult(batch, sequence, batchStart, nil)
}

func (c *PrometheusMetricCollector[T]) sendBatchResult(batch []T, sequence int64, start time.Time, err error) {
	duration := time.Since(start)

	result := BatchResult[T]{
		Batch:     batch,
		BatchSize: len(batch),
		Error:     err,
		Duration:  duration,
		Timestamp: start,
		Sequence:  sequence,
	}

	select {
	case c.batchChannel <- result:
		atomic.AddInt64(&c.stats.MetricsCollected, int64(len(batch)))
	default:
		// Batch channel is full, handle backpressure
		atomic.AddInt64(&c.stats.BackpressureHits, 1)
		c.logger.Warn("Batch channel full, dropping batch", "batch_size", len(batch))
	}
}

func (c *PrometheusMetricCollector[T]) processItems(items []T) []T {
	var processed []T

	for _, item := range items {
		// Apply filter
		if c.config.FilterFunc != nil && !c.config.FilterFunc(item) {
			continue
		}

		// Apply transformation
		if c.config.TransformFunc != nil {
			item = c.config.TransformFunc(item)
		}

		// Apply validation
		if c.config.ValidationFunc != nil {
			if err := c.config.ValidationFunc(item); err != nil {
				c.logger.Warn("Item validation failed", "error", err)
				continue
			}
		}

		processed = append(processed, item)
	}

	return processed
}

func (c *PrometheusMetricCollector[T]) handleBackpressure(result CollectionResult[T]) {
	c.backpressureHandler.mu.Lock()
	defer c.backpressureHandler.mu.Unlock()

	switch c.backpressureHandler.strategy {
	case BackpressureStrategyDrop:
		atomic.AddInt64(&c.backpressureHandler.stats.DroppedEvents, 1)
		c.logger.Warn("Dropping collection result due to backpressure")

	case BackpressureStrategyBuffer:
		// Try to add to buffer
		for _, metric := range result.Metrics {
			if !c.backpressureHandler.buffer.add(metric) {
				atomic.AddInt64(&c.backpressureHandler.stats.DroppedEvents, 1)
				break
			}
		}

	case BackpressureStrategyAdaptive:
		// Implement adaptive backpressure based on buffer utilization
		utilization := c.backpressureHandler.buffer.utilization()
		if utilization > c.backpressureHandler.options.AlertThreshold {
			// Alert callback if configured
			if c.backpressureHandler.alertFunc != nil {
				go c.backpressureHandler.alertFunc(c.backpressureHandler.stats)
			}
		}
	}

	atomic.AddInt64(&c.stats.BackpressureHits, 1)
}

func (c *PrometheusMetricCollector[T]) updateAverageLatency(duration time.Duration) {
	currentAvg := c.stats.AverageLatency
	if currentAvg == 0 {
		c.stats.AverageLatency = duration
	} else {
		// Simple moving average
		c.stats.AverageLatency = (currentAvg + duration) / 2
	}
}

func (c *PrometheusMetricCollector[T]) initializePools() {
	c.itemPool.New = func() interface{} {
		return make([]T, 0, c.config.BatchSize)
	}

	c.resultPool.New = func() interface{} {
		return &CollectionResult[T]{}
	}

	c.batchPool.New = func() interface{} {
		return &BatchResult[T]{}
	}
}

func (c *PrometheusMetricCollector[T]) startWorkerPool() {
	for i := 0; i < c.workerPool.workers; i++ {
		c.workerPool.wg.Add(1)
		go c.workerPoolWorker(i)
	}
}

func (c *PrometheusMetricCollector[T]) stopWorkerPool(ctx context.Context) {
	close(c.workerPool.shutdown)

	// Wait for workers to finish with timeout
	done := make(chan struct{})
	go func() {
		c.workerPool.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		c.logger.Info("Worker pool stopped gracefully")
	case <-ctx.Done():
		c.logger.Warn("Worker pool shutdown timeout")
	}
}

func (c *PrometheusMetricCollector[T]) workerPoolWorker(id int) {
	defer c.workerPool.wg.Done()

	logger := c.logger.With("worker_id", id)
	logger.Debug("Worker started")

	for {
		select {
		case <-c.workerPool.shutdown:
			logger.Debug("Worker stopped")
			return
		case task := <-c.workerPool.taskQueue:
			c.executeWorkerTask(task)
		}
	}
}

func (c *PrometheusMetricCollector[T]) executeWorkerTask(task WorkerTask) {
	start := time.Now()

	// Execute task with timeout
	timeoutCtx, cancel := context.WithTimeout(task.Context, task.Timeout)
	defer cancel()

	items, err := task.Collector(timeoutCtx)
	duration := time.Since(start)

	result := WorkerResult{
		ID:       task.ID,
		Items:    items,
		Error:    err,
		Duration: duration,
		Metadata: task.Metadata,
	}

	select {
	case c.workerPool.resultQueue <- result:
		// Successfully sent result
	default:
		// Result queue is full
		c.logger.Warn("Worker result queue full, dropping result", "task_id", task.ID)
	}
}

func (c *PrometheusMetricCollector[T]) drainChannels(ctx context.Context) {
	timeout := time.After(5 * time.Second)

	for {
		select {
		case <-timeout:
			c.logger.Warn("Channel drain timeout")
			return
		case <-c.resultChannel:
			// Drain result channel
		case <-c.batchChannel:
			// Drain batch channel
		case <-c.workerPool.resultQueue:
			// Drain worker result queue
		default:
			// All channels drained
			return
		}
	}
}

func (c *PrometheusMetricCollector[T]) runMonitoring() {
	if !c.config.EnableMetrics {
		return
	}

	ticker := time.NewTicker(c.config.MetricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.shutdown:
			return
		case <-ticker.C:
			c.reportMetrics()
		}
	}
}

func (c *PrometheusMetricCollector[T]) reportMetrics() {
	stats := c.GetStats()

	c.logger.Info("Collector metrics",
		"collection_count", stats.CollectionCount,
		"metrics_collected", stats.MetricsCollected,
		"error_count", stats.ErrorCount,
		"backpressure_hits", stats.BackpressureHits,
		"average_latency", stats.AverageLatency)
}

// Utility functions

func validateCollectorConfig[T MetricType](config CollectorConfig[T]) error {
	if config.CollectorName == "" {
		return fmt.Errorf("collector name is required")
	}
	if config.CollectionFunc == nil {
		return fmt.Errorf("collection function is required")
	}
	if config.CollectionInterval <= 0 {
		return fmt.Errorf("collection interval must be positive")
	}
	if config.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	return nil
}

func applyCollectorDefaults[T MetricType](config *CollectorConfig[T]) {
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.BufferSize == 0 {
		config.BufferSize = 1000
	}
	if config.BatchSize == 0 {
		config.BatchSize = 100
	}
	if config.MaxConcurrency == 0 {
		config.MaxConcurrency = 10
	}
	if config.MemoryLimit == 0 {
		config.MemoryLimit = 100 * 1024 * 1024 // 100MB
	}
	if config.MetricsInterval == 0 {
		config.MetricsInterval = time.Minute
	}

	// Rate limiting defaults
	if config.RateLimit.RequestsPerSecond == 0 {
		config.RateLimit.RequestsPerSecond = 100
	}
	if config.RateLimit.BurstSize == 0 {
		config.RateLimit.BurstSize = 10
	}

	// Backpressure defaults
	if config.Backpressure.BufferSize == 0 {
		config.Backpressure.BufferSize = 5000
	}
	if config.Backpressure.DropThreshold == 0 {
		config.Backpressure.DropThreshold = 0.8
	}
	if config.Backpressure.AlertThreshold == 0 {
		config.Backpressure.AlertThreshold = 0.9
	}
	if config.Backpressure.RecoveryTimeout == 0 {
		config.Backpressure.RecoveryTimeout = 30 * time.Second
	}

	// Worker pool defaults
	if config.WorkerPool.Size == 0 {
		config.WorkerPool.Size = 5
	}
	if config.WorkerPool.QueueSize == 0 {
		config.WorkerPool.QueueSize = 100
	}
	if config.WorkerPool.WorkerTimeout == 0 {
		config.WorkerPool.WorkerTimeout = 30 * time.Second
	}
}

// Ring buffer implementation for efficient backpressure handling

func NewRingBuffer[T MetricType](capacity int) *RingBuffer[T] {
	return &RingBuffer[T]{
		buffer:   make([]T, capacity),
		capacity: int64(capacity),
	}
}

func (rb *RingBuffer[T]) add(item T) bool {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.size >= rb.capacity {
		return false // Buffer is full
	}

	rb.buffer[rb.tail] = item
	rb.tail = (rb.tail + 1) % rb.capacity
	rb.size++

	return true
}

func (rb *RingBuffer[T]) get() (T, bool) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	var zero T
	if rb.size == 0 {
		return zero, false
	}

	item := rb.buffer[rb.head]
	rb.buffer[rb.head] = zero // Clear reference
	rb.head = (rb.head + 1) % rb.capacity
	rb.size--

	return item, true
}

func (rb *RingBuffer[T]) utilization() float64 {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	return float64(rb.size) / float64(rb.capacity)
}

func (rb *RingBuffer[T]) reset() {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.head = 0
	rb.tail = 0
	rb.size = 0

	// Clear buffer
	var zero T
	for i := range rb.buffer {
		rb.buffer[i] = zero
	}
}

// Circuit breaker implementation

func (cb *CircuitBreaker) canExecute() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		return time.Since(cb.lastFailureTime) >= cb.timeout
	case CircuitHalfOpen:
		return true
	default:
		return false
	}
}

func (cb *CircuitBreaker) execute(fn func() error) error {
	if !cb.canExecute() {
		return fmt.Errorf("circuit breaker is %s", cb.getState())
	}

	err := fn()
	cb.recordResult(err)
	return err
}

func (cb *CircuitBreaker) recordResult(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.failureCount++
		cb.lastFailureTime = time.Now()

		if cb.state == CircuitClosed && cb.failureCount >= cb.failureThreshold {
			cb.state = CircuitOpen
		} else if cb.state == CircuitHalfOpen {
			cb.state = CircuitOpen
		}
	} else {
		if cb.state == CircuitHalfOpen {
			cb.successCount++
			if cb.successCount >= cb.successThreshold {
				cb.state = CircuitClosed
				cb.failureCount = 0
				cb.successCount = 0
			}
		} else if cb.state == CircuitOpen {
			cb.state = CircuitHalfOpen
			cb.successCount = 1
		} else {
			cb.failureCount = 0
		}
	}
}

func (cb *CircuitBreaker) getState() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

func (cb *CircuitBreaker) reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = CircuitClosed
	cb.failureCount = 0
	cb.successCount = 0
}
