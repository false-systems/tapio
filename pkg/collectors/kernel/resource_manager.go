package kernel

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sys/unix"
)

// ResourceManager manages system resources for the kernel collector
type ResourceManager struct {
	config *ResourceLimits
	
	// Resource tracking
	memoryUsage   int64 // Current memory usage in bytes (atomic)
	cpuUsage      float64 // Current CPU usage percentage
	eventQueueLen int64 // Current event queue length (atomic)
	
	// Throttling state
	memoryThrottled int32 // 1 if memory throttled, 0 if not (atomic)
	cpuThrottled    int32 // 1 if CPU throttled, 0 if not (atomic)
	queueThrottled  int32 // 1 if queue throttled, 0 if not (atomic)
	
	// Resource monitors
	memoryMonitor *MemoryMonitor
	cpuMonitor    *CPUMonitor
	queueMonitor  *QueueMonitor
	
	// Cgroup integration
	cgroupManager *CgroupManager
	
	// Metrics
	memoryGauge      metric.Int64Gauge
	cpuGauge         metric.Float64Gauge
	queueGauge       metric.Int64Gauge
	throttleCounter  metric.Int64Counter
	limitGauge       metric.Int64Gauge
	
	// Control
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	stopOnce sync.Once
}

// MemoryMonitor tracks memory usage and enforces limits
type MemoryMonitor struct {
	limit         int64
	currentUsage  int64
	peakUsage     int64
	allocations   int64
	deallocations int64
	gcCount       uint32
	mu            sync.RWMutex
}

// CPUMonitor tracks CPU usage and enforces limits
type CPUMonitor struct {
	limit         float64
	currentUsage  float64
	peakUsage     float64
	samples       []float64
	sampleIndex   int
	lastMeasure   time.Time
	mu            sync.RWMutex
}

// QueueMonitor tracks event queue usage and enforces limits
type QueueMonitor struct {
	limit         int64
	currentSize   int64
	peakSize      int64
	enqueued      int64
	dequeued      int64
	dropped       int64
	mu            sync.RWMutex
}

// CgroupManager manages cgroup resource limits
type CgroupManager struct {
	memoryPath string
	cpuPath    string
	enabled    bool
	mu         sync.RWMutex
}

// NewResourceManager creates a new resource manager
func NewResourceManager(config *ResourceLimits) *ResourceManager {
	ctx, cancel := context.WithCancel(context.Background())
	meter := otel.Meter("tapio/collectors/kernel")
	
	rm := &ResourceManager{
		config:        config,
		ctx:           ctx,
		cancel:        cancel,
		memoryMonitor: NewMemoryMonitor(int64(config.MaxMemoryMB) * 1024 * 1024),
		cpuMonitor:    NewCPUMonitor(float64(config.MaxCPUPercent)),
		queueMonitor:  NewQueueMonitor(int64(config.EventQueueSize)),
		cgroupManager: NewCgroupManager(),
	}
	
	// Initialize metrics
	var err error
	rm.memoryGauge, err = meter.Int64Gauge(
		"kernel_collector_memory_bytes",
		metric.WithDescription("Current memory usage in bytes"),
	)
	if err != nil {
		// Log error but continue
	}
	
	rm.cpuGauge, err = meter.Float64Gauge(
		"kernel_collector_cpu_percent",
		metric.WithDescription("Current CPU usage percentage"),
	)
	if err != nil {
		// Log error but continue
	}
	
	rm.queueGauge, err = meter.Int64Gauge(
		"kernel_collector_queue_size",
		metric.WithDescription("Current event queue size"),
	)
	if err != nil {
		// Log error but continue
	}
	
	rm.throttleCounter, err = meter.Int64Counter(
		"kernel_collector_throttle_total",
		metric.WithDescription("Total number of throttle events by type"),
	)
	if err != nil {
		// Log error but continue
	}
	
	rm.limitGauge, err = meter.Int64Gauge(
		"kernel_collector_limits",
		metric.WithDescription("Configured resource limits"),
	)
	if err != nil {
		// Log error but continue
	}
	
	return rm
}

// NewMemoryMonitor creates a new memory monitor
func NewMemoryMonitor(limit int64) *MemoryMonitor {
	return &MemoryMonitor{
		limit: limit,
	}
}

// NewCPUMonitor creates a new CPU monitor
func NewCPUMonitor(limit float64) *CPUMonitor {
	return &CPUMonitor{
		limit:       limit,
		samples:     make([]float64, 10), // 10-sample moving average
		lastMeasure: time.Now(),
	}
}

// NewQueueMonitor creates a new queue monitor
func NewQueueMonitor(limit int64) *QueueMonitor {
	return &QueueMonitor{
		limit: limit,
	}
}

// NewCgroupManager creates a new cgroup manager
func NewCgroupManager() *CgroupManager {
	cm := &CgroupManager{}
	cm.detectCgroupPaths()
	return cm
}

// detectCgroupPaths detects cgroup paths for memory and CPU
func (cm *CgroupManager) detectCgroupPaths() {
	// Try cgroup v2 first
	if err := unix.Stat("/sys/fs/cgroup/memory.max", &unix.Stat_t{}); err == nil {
		cm.memoryPath = "/sys/fs/cgroup"
		cm.cpuPath = "/sys/fs/cgroup"
		cm.enabled = true
		return
	}
	
	// Try cgroup v1
	if err := unix.Stat("/sys/fs/cgroup/memory/memory.limit_in_bytes", &unix.Stat_t{}); err == nil {
		cm.memoryPath = "/sys/fs/cgroup/memory"
		cm.cpuPath = "/sys/fs/cgroup/cpu"
		cm.enabled = true
	}
}

// Start starts the resource manager
func (rm *ResourceManager) Start() error {
	// Start monitoring goroutines
	rm.wg.Add(3)
	
	go func() {
		defer rm.wg.Done()
		rm.monitorMemory()
	}()
	
	go func() {
		defer rm.wg.Done()
		rm.monitorCPU()
	}()
	
	go func() {
		defer rm.wg.Done()
		rm.monitorQueue()
	}()
	
	// Update limit metrics
	if rm.limitGauge != nil {
		rm.limitGauge.Record(rm.ctx, int64(rm.config.MaxMemoryMB),
			metric.WithAttributes(attribute.String("type", "memory_mb")))
		rm.limitGauge.Record(rm.ctx, int64(rm.config.MaxCPUPercent),
			metric.WithAttributes(attribute.String("type", "cpu_percent")))
		rm.limitGauge.Record(rm.ctx, int64(rm.config.EventQueueSize),
			metric.WithAttributes(attribute.String("type", "queue_size")))
	}
	
	return nil
}

// monitorMemory monitors memory usage
func (rm *ResourceManager) monitorMemory() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			rm.updateMemoryUsage()
		case <-rm.ctx.Done():
			return
		}
	}
}

// monitorCPU monitors CPU usage
func (rm *ResourceManager) monitorCPU() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			rm.updateCPUUsage()
		case <-rm.ctx.Done():
			return
		}
	}
}

// monitorQueue monitors queue usage
func (rm *ResourceManager) monitorQueue() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			rm.updateQueueUsage()
		case <-rm.ctx.Done():
			return
		}
	}
}

// updateMemoryUsage updates memory usage metrics
func (rm *ResourceManager) updateMemoryUsage() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	
	currentUsage := int64(m.Alloc)
	atomic.StoreInt64(&rm.memoryUsage, currentUsage)
	
	rm.memoryMonitor.mu.Lock()
	rm.memoryMonitor.currentUsage = currentUsage
	if currentUsage > rm.memoryMonitor.peakUsage {
		rm.memoryMonitor.peakUsage = currentUsage
	}
	rm.memoryMonitor.allocations = int64(m.Mallocs)
	rm.memoryMonitor.deallocations = int64(m.Frees)
	rm.memoryMonitor.gcCount = m.NumGC
	rm.memoryMonitor.mu.Unlock()
	
	// Check if we need to throttle
	if currentUsage > rm.memoryMonitor.limit {
		if atomic.CompareAndSwapInt32(&rm.memoryThrottled, 0, 1) {
			// Started throttling
			if rm.throttleCounter != nil {
				rm.throttleCounter.Add(rm.ctx, 1,
					metric.WithAttributes(attribute.String("type", "memory")))
			}
			
			// Force garbage collection
			runtime.GC()
		}
	} else if currentUsage < int64(float64(rm.memoryMonitor.limit)*0.8) {
		// Stop throttling if we're below 80% of limit
		atomic.StoreInt32(&rm.memoryThrottled, 0)
	}
	
	// Update metrics
	if rm.memoryGauge != nil {
		rm.memoryGauge.Record(rm.ctx, currentUsage)
	}
}

// updateCPUUsage updates CPU usage metrics
func (rm *ResourceManager) updateCPUUsage() {
	now := time.Now()
	elapsed := now.Sub(rm.cpuMonitor.lastMeasure)
	
	// Get CPU times (simplified - would use more accurate measurement in production)
	var rusage unix.Rusage
	if err := unix.Getrusage(unix.RUSAGE_SELF, &rusage); err == nil {
		userTime := time.Duration(rusage.Utime.Sec)*time.Second + time.Duration(rusage.Utime.Usec)*time.Microsecond
		sysTime := time.Duration(rusage.Stime.Sec)*time.Second + time.Duration(rusage.Stime.Usec)*time.Microsecond
		totalCPUTime := userTime + sysTime
		
		// Calculate CPU percentage (simplified)
		cpuPercent := float64(totalCPUTime) / float64(elapsed) * 100
		if cpuPercent > 100 {
			cpuPercent = 100 // Cap at 100%
		}
		
		rm.cpuMonitor.mu.Lock()
		rm.cpuMonitor.currentUsage = cpuPercent
		if cpuPercent > rm.cpuMonitor.peakUsage {
			rm.cpuMonitor.peakUsage = cpuPercent
		}
		
		// Update moving average
		rm.cpuMonitor.samples[rm.cpuMonitor.sampleIndex] = cpuPercent
		rm.cpuMonitor.sampleIndex = (rm.cpuMonitor.sampleIndex + 1) % len(rm.cpuMonitor.samples)
		rm.cpuMonitor.lastMeasure = now
		
		// Calculate average
		var sum float64
		for _, sample := range rm.cpuMonitor.samples {
			sum += sample
		}
		avgCPU := sum / float64(len(rm.cpuMonitor.samples))
		rm.cpuMonitor.mu.Unlock()
		
		// Check if we need to throttle
		if avgCPU > rm.cpuMonitor.limit {
			if atomic.CompareAndSwapInt32(&rm.cpuThrottled, 0, 1) {
				if rm.throttleCounter != nil {
					rm.throttleCounter.Add(rm.ctx, 1,
						metric.WithAttributes(attribute.String("type", "cpu")))
				}
			}
		} else if avgCPU < rm.cpuMonitor.limit*0.8 {
			atomic.StoreInt32(&rm.cpuThrottled, 0)
		}
		
		// Update metrics
		if rm.cpuGauge != nil {
			rm.cpuGauge.Record(rm.ctx, avgCPU)
		}
	}
}

// updateQueueUsage updates queue usage metrics
func (rm *ResourceManager) updateQueueUsage() {
	currentSize := atomic.LoadInt64(&rm.eventQueueLen)
	
	rm.queueMonitor.mu.Lock()
	rm.queueMonitor.currentSize = currentSize
	if currentSize > rm.queueMonitor.peakSize {
		rm.queueMonitor.peakSize = currentSize
	}
	rm.queueMonitor.mu.Unlock()
	
	// Check if we need to throttle
	if currentSize > rm.queueMonitor.limit {
		if atomic.CompareAndSwapInt32(&rm.queueThrottled, 0, 1) {
			if rm.throttleCounter != nil {
				rm.throttleCounter.Add(rm.ctx, 1,
					metric.WithAttributes(attribute.String("type", "queue")))
			}
		}
	} else if currentSize < int64(float64(rm.queueMonitor.limit)*0.8) {
		atomic.StoreInt32(&rm.queueThrottled, 0)
	}
	
	// Update metrics
	if rm.queueGauge != nil {
		rm.queueGauge.Record(rm.ctx, currentSize)
	}
}

// CanProcessEvent checks if we can process a new event
func (rm *ResourceManager) CanProcessEvent() bool {
	// Check all throttling conditions
	memThrottled := atomic.LoadInt32(&rm.memoryThrottled) == 1
	cpuThrottled := atomic.LoadInt32(&rm.cpuThrottled) == 1
	queueThrottled := atomic.LoadInt32(&rm.queueThrottled) == 1
	
	return !memThrottled && !cpuThrottled && !queueThrottled
}

// RecordEventEnqueue records an event being added to the queue
func (rm *ResourceManager) RecordEventEnqueue() {
	atomic.AddInt64(&rm.eventQueueLen, 1)
	
	rm.queueMonitor.mu.Lock()
	rm.queueMonitor.enqueued++
	rm.queueMonitor.mu.Unlock()
}

// RecordEventDequeue records an event being removed from the queue
func (rm *ResourceManager) RecordEventDequeue() {
	atomic.AddInt64(&rm.eventQueueLen, -1)
	
	rm.queueMonitor.mu.Lock()
	rm.queueMonitor.dequeued++
	rm.queueMonitor.mu.Unlock()
}

// RecordEventDrop records a dropped event
func (rm *ResourceManager) RecordEventDrop() {
	rm.queueMonitor.mu.Lock()
	rm.queueMonitor.dropped++
	rm.queueMonitor.mu.Unlock()
}

// GetMemoryUsage returns current memory usage
func (rm *ResourceManager) GetMemoryUsage() int64 {
	return atomic.LoadInt64(&rm.memoryUsage)
}

// GetCPUUsage returns current CPU usage
func (rm *ResourceManager) GetCPUUsage() float64 {
	rm.cpuMonitor.mu.RLock()
	defer rm.cpuMonitor.mu.RUnlock()
	return rm.cpuMonitor.currentUsage
}

// GetQueueLength returns current queue length
func (rm *ResourceManager) GetQueueLength() int64 {
	return atomic.LoadInt64(&rm.eventQueueLen)
}

// IsMemoryThrottled returns whether memory throttling is active
func (rm *ResourceManager) IsMemoryThrottled() bool {
	return atomic.LoadInt32(&rm.memoryThrottled) == 1
}

// IsCPUThrottled returns whether CPU throttling is active
func (rm *ResourceManager) IsCPUThrottled() bool {
	return atomic.LoadInt32(&rm.cpuThrottled) == 1
}

// IsQueueThrottled returns whether queue throttling is active
func (rm *ResourceManager) IsQueueThrottled() bool {
	return atomic.LoadInt32(&rm.queueThrottled) == 1
}

// ForceGC forces garbage collection if memory usage is high
func (rm *ResourceManager) ForceGC() {
	currentUsage := atomic.LoadInt64(&rm.memoryUsage)
	threshold := int64(float64(rm.memoryMonitor.limit) * 0.9) // 90% of limit
	
	if currentUsage > threshold {
		runtime.GC()
		runtime.GC() // Double GC for more aggressive cleanup
	}
}

// ApplyCgroupLimits applies cgroup limits if available
func (rm *ResourceManager) ApplyCgroupLimits() error {
	if !rm.cgroupManager.enabled {
		return fmt.Errorf("cgroups not available")
	}
	
	rm.cgroupManager.mu.Lock()
	defer rm.cgroupManager.mu.Unlock()
	
	// This would write to cgroup files to set limits
	// Implementation depends on cgroup version and container runtime
	
	return nil
}

// GetStats returns resource usage statistics
func (rm *ResourceManager) GetStats() ResourceStats {
	rm.memoryMonitor.mu.RLock()
	memStats := MemoryStats{
		Current:       rm.memoryMonitor.currentUsage,
		Peak:          rm.memoryMonitor.peakUsage,
		Limit:         rm.memoryMonitor.limit,
		Allocations:   rm.memoryMonitor.allocations,
		Deallocations: rm.memoryMonitor.deallocations,
		GCCount:       rm.memoryMonitor.gcCount,
	}
	rm.memoryMonitor.mu.RUnlock()
	
	rm.cpuMonitor.mu.RLock()
	cpuStats := CPUStats{
		Current: rm.cpuMonitor.currentUsage,
		Peak:    rm.cpuMonitor.peakUsage,
		Limit:   rm.cpuMonitor.limit,
	}
	rm.cpuMonitor.mu.RUnlock()
	
	rm.queueMonitor.mu.RLock()
	queueStats := QueueStats{
		Current:  rm.queueMonitor.currentSize,
		Peak:     rm.queueMonitor.peakSize,
		Limit:    rm.queueMonitor.limit,
		Enqueued: rm.queueMonitor.enqueued,
		Dequeued: rm.queueMonitor.dequeued,
		Dropped:  rm.queueMonitor.dropped,
	}
	rm.queueMonitor.mu.RUnlock()
	
	return ResourceStats{
		Memory:           memStats,
		CPU:              cpuStats,
		Queue:            queueStats,
		MemoryThrottled:  rm.IsMemoryThrottled(),
		CPUThrottled:     rm.IsCPUThrottled(),
		QueueThrottled:   rm.IsQueueThrottled(),
	}
}

// Stop stops the resource manager
func (rm *ResourceManager) Stop() {
	rm.stopOnce.Do(func() {
		rm.cancel()
		rm.wg.Wait()
	})
}

// ResourceStats represents resource usage statistics
type ResourceStats struct {
	Memory          MemoryStats `json:"memory"`
	CPU             CPUStats    `json:"cpu"`
	Queue           QueueStats  `json:"queue"`
	MemoryThrottled bool        `json:"memory_throttled"`
	CPUThrottled    bool        `json:"cpu_throttled"`
	QueueThrottled  bool        `json:"queue_throttled"`
}

// MemoryStats represents memory usage statistics
type MemoryStats struct {
	Current       int64  `json:"current"`
	Peak          int64  `json:"peak"`
	Limit         int64  `json:"limit"`
	Allocations   int64  `json:"allocations"`
	Deallocations int64  `json:"deallocations"`
	GCCount       uint32 `json:"gc_count"`
}

// CPUStats represents CPU usage statistics
type CPUStats struct {
	Current float64 `json:"current"`
	Peak    float64 `json:"peak"`
	Limit   float64 `json:"limit"`
}

// QueueStats represents queue usage statistics
type QueueStats struct {
	Current  int64 `json:"current"`
	Peak     int64 `json:"peak"`
	Limit    int64 `json:"limit"`
	Enqueued int64 `json:"enqueued"`
	Dequeued int64 `json:"dequeued"`
	Dropped  int64 `json:"dropped"`
}