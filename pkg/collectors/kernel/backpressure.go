package kernel

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// BackpressureManager manages backpressure for the kernel collector
type BackpressureManager struct {
	config *BackpressureConfig
	
	// State tracking
	currentLoad    int64   // Current load percentage (0-100)
	isThrottling   int32   // 1 if throttling, 0 if not (atomic)
	droppedEvents  int64   // Total dropped events (atomic)
	lastDropTime   int64   // Last time we dropped events (atomic)
	
	// Ring buffer monitoring
	bufferUsage    map[string]*BufferUsage
	bufferMutex    sync.RWMutex
	
	// Rate limiting
	eventBucket    *TokenBucket
	memoryPressure *MemoryPressureDetector
	
	// Metrics
	loadGauge        metric.Float64Gauge
	droppedCounter   metric.Int64Counter
	throttleGauge    metric.Int64Gauge
	bufferUsageGauge metric.Float64Gauge
	
	// Control channels
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// BufferUsage tracks individual buffer usage
type BufferUsage struct {
	Name         string
	Size         int64
	Used         int64
	High         int64  // High watermark
	Low          int64  // Low watermark
	LastUpdated  time.Time
}

// TokenBucket implements token bucket rate limiting
type TokenBucket struct {
	capacity    int64
	tokens      int64
	refillRate  int64 // tokens per second
	lastRefill  int64 // nanoseconds
	mu          sync.Mutex
}

// MemoryPressureDetector detects memory pressure from various sources
type MemoryPressureDetector struct {
	cgroupPath      string
	lastMemoryUsage uint64
	threshold       uint64
	mu              sync.RWMutex
}

// NewBackpressureManager creates a new backpressure manager
func NewBackpressureManager(config *BackpressureConfig) *BackpressureManager {
	meter := otel.Meter("tapio/collectors/kernel")
	
	bm := &BackpressureManager{
		config:       config,
		bufferUsage:  make(map[string]*BufferUsage),
		stopChan:     make(chan struct{}),
		eventBucket:  NewTokenBucket(int64(config.MaxEventsPerSec), int64(config.MaxEventsPerSec)),
		memoryPressure: &MemoryPressureDetector{
			threshold: uint64(config.MemoryThresholdMB) * 1024 * 1024,
		},
	}
	
	// Initialize metrics
	var err error
	bm.loadGauge, err = meter.Float64Gauge(
		"kernel_collector_load",
		metric.WithDescription("Current load percentage of kernel collector"),
	)
	if err != nil {
		// Log error but continue
	}
	
	bm.droppedCounter, err = meter.Int64Counter(
		"kernel_collector_events_dropped_total",
		metric.WithDescription("Total number of dropped events due to backpressure"),
	)
	if err != nil {
		// Log error but continue
	}
	
	bm.throttleGauge, err = meter.Int64Gauge(
		"kernel_collector_throttling",
		metric.WithDescription("Whether collector is currently throttling (1=yes, 0=no)"),
	)
	if err != nil {
		// Log error but continue
	}
	
	bm.bufferUsageGauge, err = meter.Float64Gauge(
		"kernel_collector_buffer_usage",
		metric.WithDescription("Buffer usage percentage by buffer type"),
	)
	if err != nil {
		// Log error but continue
	}
	
	// Start monitoring
	bm.startMonitoring()
	
	return bm
}

// NewTokenBucket creates a new token bucket
func NewTokenBucket(capacity, refillRate int64) *TokenBucket {
	return &TokenBucket{
		capacity:   capacity,
		tokens:     capacity,
		refillRate: refillRate,
		lastRefill: time.Now().UnixNano(),
	}
}

// Allow checks if request is allowed and consumes a token
func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	
	now := time.Now().UnixNano()
	elapsed := now - tb.lastRefill
	
	// Refill tokens based on elapsed time
	tokensToAdd := (elapsed * tb.refillRate) / int64(time.Second)
	tb.tokens += tokensToAdd
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}
	tb.lastRefill = now
	
	// Check if we can consume a token
	if tb.tokens > 0 {
		tb.tokens--
		return true
	}
	
	return false
}

// UpdateBufferUsage updates buffer usage information
func (bm *BackpressureManager) UpdateBufferUsage(name string, used, total int64) {
	bm.bufferMutex.Lock()
	defer bm.bufferMutex.Unlock()
	
	usage := bm.bufferUsage[name]
	if usage == nil {
		usage = &BufferUsage{
			Name: name,
			Size: total,
			High: int64(float64(total) * bm.config.HighWatermark),
			Low:  int64(float64(total) * bm.config.LowWatermark),
		}
		bm.bufferUsage[name] = usage
	}
	
	usage.Used = used
	usage.Size = total
	usage.LastUpdated = time.Now()
	
	// Update metrics
	if bm.bufferUsageGauge != nil {
		usagePercent := float64(used) / float64(total) * 100
		bm.bufferUsageGauge.Record(context.Background(), usagePercent,
			metric.WithAttributes(attribute.String("buffer", name)))
	}
}

// ShouldDropEvent determines if an event should be dropped
func (bm *BackpressureManager) ShouldDropEvent() bool {
	if !bm.config.Enabled {
		return false
	}
	
	// Check if we're over drop threshold
	load := atomic.LoadInt64(&bm.currentLoad)
	if float64(load)/100.0 > bm.config.DropThreshold {
		atomic.AddInt64(&bm.droppedEvents, 1)
		atomic.StoreInt64(&bm.lastDropTime, time.Now().UnixNano())
		
		// Update metrics
		if bm.droppedCounter != nil {
			bm.droppedCounter.Add(context.Background(), 1)
		}
		return true
	}
	
	// Check token bucket for rate limiting
	if !bm.eventBucket.Allow() {
		atomic.AddInt64(&bm.droppedEvents, 1)
		if bm.droppedCounter != nil {
			bm.droppedCounter.Add(context.Background(), 1)
		}
		return true
	}
	
	return false
}

// IsThrottling returns whether the collector is currently throttling
func (bm *BackpressureManager) IsThrottling() bool {
	return atomic.LoadInt32(&bm.isThrottling) == 1
}

// GetSamplingRate returns the current sampling rate based on load
func (bm *BackpressureManager) GetSamplingRate() int {
	if !bm.config.Enabled {
		return 1 // No sampling
	}
	
	load := atomic.LoadInt64(&bm.currentLoad)
	loadPercent := float64(load) / 100.0
	
	if loadPercent > bm.config.HighWatermark {
		// Increase sampling rate (reduce events)
		reduction := bm.config.SamplingReduction
		return int(1.0 / reduction)
	}
	
	return 1 // No sampling needed
}

// startMonitoring starts the monitoring goroutines
func (bm *BackpressureManager) startMonitoring() {
	// Buffer monitoring
	bm.wg.Add(1)
	go func() {
		defer bm.wg.Done()
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		
		for {
			select {
			case <-ticker.C:
				bm.calculateLoad()
			case <-bm.stopChan:
				return
			}
		}
	}()
	
	// Memory pressure monitoring
	bm.wg.Add(1)
	go func() {
		defer bm.wg.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		
		for {
			select {
			case <-ticker.C:
				bm.checkMemoryPressure()
			case <-bm.stopChan:
				return
			}
		}
	}()
	
	// Recovery monitoring
	bm.wg.Add(1)
	go func() {
		defer bm.wg.Done()
		ticker := time.NewTicker(bm.config.RecoveryDelay)
		defer ticker.Stop()
		
		for {
			select {
			case <-ticker.C:
				bm.attemptRecovery()
			case <-bm.stopChan:
				return
			}
		}
	}()
}

// calculateLoad calculates current system load
func (bm *BackpressureManager) calculateLoad() {
	bm.bufferMutex.RLock()
	defer bm.bufferMutex.RUnlock()
	
	var totalLoad float64
	var bufferCount int
	
	for _, usage := range bm.bufferUsage {
		if usage.Size > 0 {
			loadPercent := float64(usage.Used) / float64(usage.Size)
			totalLoad += loadPercent
			bufferCount++
		}
	}
	
	// Average load across all buffers
	var currentLoad float64
	if bufferCount > 0 {
		currentLoad = totalLoad / float64(bufferCount) * 100
	}
	
	// Factor in memory pressure
	memLoad := bm.memoryPressure.GetPressure()
	if memLoad > currentLoad {
		currentLoad = memLoad
	}
	
	atomic.StoreInt64(&bm.currentLoad, int64(currentLoad))
	
	// Update throttling state
	wasThrottling := atomic.LoadInt32(&bm.isThrottling)
	shouldThrottle := currentLoad/100.0 > bm.config.HighWatermark
	
	if shouldThrottle && wasThrottling == 0 {
		atomic.StoreInt32(&bm.isThrottling, 1)
	} else if !shouldThrottle && wasThrottling == 1 {
		if currentLoad/100.0 < bm.config.LowWatermark {
			atomic.StoreInt32(&bm.isThrottling, 0)
		}
	}
	
	// Update metrics
	if bm.loadGauge != nil {
		bm.loadGauge.Record(context.Background(), currentLoad)
	}
	if bm.throttleGauge != nil {
		bm.throttleGauge.Record(context.Background(), int64(atomic.LoadInt32(&bm.isThrottling)))
	}
}

// checkMemoryPressure checks for memory pressure
func (bm *BackpressureManager) checkMemoryPressure() {
	// This would check cgroup memory usage, PSI, etc.
	// For now, implement a basic check
	bm.memoryPressure.update()
}

// attemptRecovery attempts to recover from throttling state
func (bm *BackpressureManager) attemptRecovery() {
	if !bm.IsThrottling() {
		return
	}
	
	load := atomic.LoadInt64(&bm.currentLoad)
	if float64(load)/100.0 < bm.config.LowWatermark {
		atomic.StoreInt32(&bm.isThrottling, 0)
		
		// Reset token bucket to full capacity for quick recovery
		bm.eventBucket.mu.Lock()
		bm.eventBucket.tokens = bm.eventBucket.capacity
		bm.eventBucket.mu.Unlock()
	}
}

// update updates memory pressure metrics
func (mp *MemoryPressureDetector) update() {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	
	// Would read from /proc/meminfo, /sys/fs/cgroup/memory/memory.usage_in_bytes, etc.
	// For now, placeholder implementation
	mp.lastMemoryUsage = 0 // Placeholder
}

// GetPressure returns memory pressure percentage
func (mp *MemoryPressureDetector) GetPressure() float64 {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	
	if mp.threshold == 0 {
		return 0
	}
	
	pressure := float64(mp.lastMemoryUsage) / float64(mp.threshold) * 100
	if pressure > 100 {
		pressure = 100
	}
	
	return pressure
}

// Stop stops the backpressure manager
func (bm *BackpressureManager) Stop() {
	close(bm.stopChan)
	bm.wg.Wait()
}

// GetStats returns backpressure statistics
func (bm *BackpressureManager) GetStats() BackpressureStats {
	return BackpressureStats{
		CurrentLoad:    atomic.LoadInt64(&bm.currentLoad),
		IsThrottling:   bm.IsThrottling(),
		DroppedEvents:  atomic.LoadInt64(&bm.droppedEvents),
		SamplingRate:   bm.GetSamplingRate(),
		BufferUsage:    bm.getBufferUsageSnapshot(),
	}
}

// getBufferUsageSnapshot returns a snapshot of buffer usage
func (bm *BackpressureManager) getBufferUsageSnapshot() map[string]float64 {
	bm.bufferMutex.RLock()
	defer bm.bufferMutex.RUnlock()
	
	usage := make(map[string]float64)
	for name, buf := range bm.bufferUsage {
		if buf.Size > 0 {
			usage[name] = float64(buf.Used) / float64(buf.Size) * 100
		}
	}
	
	return usage
}

// BackpressureStats represents backpressure statistics
type BackpressureStats struct {
	CurrentLoad   int64              `json:"current_load"`
	IsThrottling  bool               `json:"is_throttling"`
	DroppedEvents int64              `json:"dropped_events"`
	SamplingRate  int                `json:"sampling_rate"`
	BufferUsage   map[string]float64 `json:"buffer_usage"`
}

