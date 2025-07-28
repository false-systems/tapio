package internal

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// ResourceMonitor monitors and manages system resources for CNI collector
type ResourceMonitor struct {
	// Configuration
	maxMemoryMB   int
	maxGoroutines int
	gcInterval    time.Duration

	// Callbacks
	memoryCallback    func(uint64)
	goroutineCallback func(int)

	// State
	running      atomic.Bool
	lastGC       atomic.Value // time.Time
	memoryUsage  atomic.Uint64
	goroutineNum atomic.Int32

	// Monitoring
	stopCh chan struct{}
	wg     sync.WaitGroup
	mu     sync.RWMutex

	// Metrics
	metrics ResourceMetrics
}

// ResourceMetrics tracks resource usage statistics
type ResourceMetrics struct {
	mu sync.RWMutex

	// Memory metrics
	CurrentMemoryMB     int
	PeakMemoryMB        int
	MemoryLimitMB       int
	GCCount             uint64
	LastGCTime          time.Time
	MemoryAllocations   uint64
	MemoryDeallocations uint64

	// Goroutine metrics
	CurrentGoroutines int
	PeakGoroutines    int
	GoroutineLimit    int

	// System metrics
	CPUUsagePercent float64
	Uptime          time.Duration
	StartTime       time.Time
}

// NewResourceMonitor creates a new resource monitor
func NewResourceMonitor(maxMemoryMB, maxGoroutines int) *ResourceMonitor {
	if maxMemoryMB <= 0 {
		maxMemoryMB = 256 // Default 256MB for CNI collector
	}
	if maxGoroutines <= 0 {
		maxGoroutines = 1000 // Default 1000 goroutines
	}

	rm := &ResourceMonitor{
		maxMemoryMB:   maxMemoryMB,
		maxGoroutines: maxGoroutines,
		gcInterval:    30 * time.Second,
		stopCh:        make(chan struct{}),
		metrics: ResourceMetrics{
			MemoryLimitMB:  maxMemoryMB,
			GoroutineLimit: maxGoroutines,
			StartTime:      time.Now(),
		},
	}

	rm.lastGC.Store(time.Now())
	return rm
}

// Start begins resource monitoring
func (rm *ResourceMonitor) Start() {
	if rm.running.Swap(true) {
		return // Already running
	}

	rm.wg.Add(2)
	go rm.monitorMemory()
	go rm.monitorGoroutines()
}

// Stop stops resource monitoring
func (rm *ResourceMonitor) Stop() {
	if !rm.running.Swap(false) {
		return // Already stopped
	}

	close(rm.stopCh)
	rm.wg.Wait()
}

// SetMemoryCallback sets the callback for memory limit violations
func (rm *ResourceMonitor) SetMemoryCallback(callback func(uint64)) {
	rm.mu.Lock()
	rm.memoryCallback = callback
	rm.mu.Unlock()
}

// SetGoroutineCallback sets the callback for goroutine limit violations
func (rm *ResourceMonitor) SetGoroutineCallback(callback func(int)) {
	rm.mu.Lock()
	rm.goroutineCallback = callback
	rm.mu.Unlock()
}

// ForceGC forces garbage collection
func (rm *ResourceMonitor) ForceGC() {
	runtime.GC()
	runtime.Gosched()
	rm.lastGC.Store(time.Now())

	rm.metrics.mu.Lock()
	rm.metrics.GCCount++
	rm.metrics.LastGCTime = time.Now()
	rm.metrics.mu.Unlock()
}

// GetMemoryUsageMB returns current memory usage in MB
func (rm *ResourceMonitor) GetMemoryUsageMB() int {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	return int(memStats.Alloc / 1024 / 1024)
}

// GetMemoryUsagePercent returns current memory usage as percentage
func (rm *ResourceMonitor) GetMemoryUsagePercent() float64 {
	memMB := rm.GetMemoryUsageMB()
	if rm.maxMemoryMB == 0 {
		return 0
	}
	return float64(memMB) / float64(rm.maxMemoryMB) * 100
}

// GetGoroutineCount returns current goroutine count
func (rm *ResourceMonitor) GetGoroutineCount() int {
	return runtime.NumGoroutine()
}

// GetGoroutineUsagePercent returns current goroutine usage as percentage
func (rm *ResourceMonitor) GetGoroutineUsagePercent() float64 {
	current := rm.GetGoroutineCount()
	if rm.maxGoroutines == 0 {
		return 0
	}
	return float64(current) / float64(rm.maxGoroutines) * 100
}

// IsMemoryPressure returns true if memory usage is high
func (rm *ResourceMonitor) IsMemoryPressure() bool {
	return rm.GetMemoryUsagePercent() > 80.0
}

// IsGoroutinePressure returns true if goroutine count is high
func (rm *ResourceMonitor) IsGoroutinePressure() bool {
	return rm.GetGoroutineUsagePercent() > 80.0
}

// monitorMemory monitors memory usage
func (rm *ResourceMonitor) monitorMemory() {
	defer rm.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	gcTicker := time.NewTicker(rm.gcInterval)
	defer gcTicker.Stop()

	for {
		select {
		case <-rm.stopCh:
			return

		case <-ticker.C:
			rm.checkMemory()

		case <-gcTicker.C:
			// Periodic GC if memory usage is above 50%
			if rm.GetMemoryUsagePercent() > 50.0 {
				rm.ForceGC()
			}
		}
	}
}

// monitorGoroutines monitors goroutine count
func (rm *ResourceMonitor) monitorGoroutines() {
	defer rm.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-rm.stopCh:
			return

		case <-ticker.C:
			rm.checkGoroutines()
		}
	}
}

// checkMemory checks current memory usage
func (rm *ResourceMonitor) checkMemory() {
	memMB := rm.GetMemoryUsageMB()
	rm.memoryUsage.Store(uint64(memMB))

	// Update metrics
	rm.metrics.mu.Lock()
	rm.metrics.CurrentMemoryMB = memMB
	if memMB > rm.metrics.PeakMemoryMB {
		rm.metrics.PeakMemoryMB = memMB
	}
	rm.metrics.mu.Unlock()

	// Check if we need to trigger callback
	if memMB > rm.maxMemoryMB {
		rm.mu.RLock()
		callback := rm.memoryCallback
		rm.mu.RUnlock()

		if callback != nil {
			callback(uint64(memMB))
		}

		// Force GC when over limit
		rm.ForceGC()
	}
}

// checkGoroutines checks current goroutine count
func (rm *ResourceMonitor) checkGoroutines() {
	count := rm.GetGoroutineCount()
	rm.goroutineNum.Store(int32(count))

	// Update metrics
	rm.metrics.mu.Lock()
	rm.metrics.CurrentGoroutines = count
	if count > rm.metrics.PeakGoroutines {
		rm.metrics.PeakGoroutines = count
	}
	rm.metrics.mu.Unlock()

	// Check if we need to trigger callback
	if count > rm.maxGoroutines {
		rm.mu.RLock()
		callback := rm.goroutineCallback
		rm.mu.RUnlock()

		if callback != nil {
			callback(count)
		}
	}
}

// GetMetrics returns current resource metrics
func (rm *ResourceMonitor) GetMetrics() ResourceMetrics {
	rm.metrics.mu.RLock()
	defer rm.metrics.mu.RUnlock()

	metrics := rm.metrics
	metrics.Uptime = time.Since(metrics.StartTime)
	return metrics
}

// GetMetricsMap returns metrics as map for monitoring
func (rm *ResourceMonitor) GetMetricsMap() map[string]interface{} {
	metrics := rm.GetMetrics()

	return map[string]interface{}{
		// Memory metrics
		"memory_current_mb":    metrics.CurrentMemoryMB,
		"memory_peak_mb":       metrics.PeakMemoryMB,
		"memory_limit_mb":      metrics.MemoryLimitMB,
		"memory_usage_percent": rm.GetMemoryUsagePercent(),
		"memory_pressure":      rm.IsMemoryPressure(),
		"gc_count":             metrics.GCCount,
		"last_gc_time":         metrics.LastGCTime,

		// Goroutine metrics
		"goroutines_current": metrics.CurrentGoroutines,
		"goroutines_peak":    metrics.PeakGoroutines,
		"goroutines_limit":   metrics.GoroutineLimit,
		"goroutines_percent": rm.GetGoroutineUsagePercent(),
		"goroutine_pressure": rm.IsGoroutinePressure(),

		// System metrics
		"uptime_seconds": metrics.Uptime.Seconds(),
		"start_time":     metrics.StartTime,
	}
}

// Reset resets metrics (for testing)
func (rm *ResourceMonitor) Reset() {
	rm.metrics.mu.Lock()
	defer rm.metrics.mu.Unlock()

	rm.metrics = ResourceMetrics{
		MemoryLimitMB:  rm.maxMemoryMB,
		GoroutineLimit: rm.maxGoroutines,
		StartTime:      time.Now(),
	}
}
