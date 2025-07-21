package internal

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
)

// ResourceManager manages system resources for the eBPF collector
type ResourceManager struct {
	config         *ResourceConfig
	limits         *ResourceLimits
	usage          *ResourceUsage
	monitor        *ResourceMonitor
	poolManager    *PoolManager
	memoryPressure atomic.Bool
	cpuPressure    atomic.Bool
	lastCleanup    atomic.Value // time.Time
	mu             sync.RWMutex
	stopCh         chan struct{}
	wg             sync.WaitGroup
}

// ResourceConfig defines resource management parameters
type ResourceConfig struct {
	// Memory limits
	MaxMemoryMB     int
	MemoryWarningMB int
	MemoryCleanupMB int

	// CPU limits
	MaxCPUPercent     float64
	CPUWarningPercent float64

	// File descriptor limits
	MaxFileDescriptors int
	FDWarningThreshold int

	// Buffer pool configuration
	EnableBufferPool bool
	BufferPoolSize   int
	BufferSize       int

	// Monitoring
	MonitorInterval time.Duration
	CleanupInterval time.Duration

	// Garbage collection
	ForceGCThreshold int // Force GC when memory exceeds this (MB)
	GCInterval       time.Duration

	// Adaptive resource management
	EnableAdaptive   bool
	AdaptiveInterval time.Duration
}

// ResourceLimits tracks configured limits
type ResourceLimits struct {
	MemoryBytes     uint64
	CPUPercent      float64
	FileDescriptors int
	EventsPerSecond int64
	MapsPerProgram  int
	ProgramsTotal   int
}

// ResourceUsage tracks current usage
type ResourceUsage struct {
	memoryBytes     atomic.Uint64
	cpuPercent      atomic.Uint64 // Stored as percent * 100
	fileDescriptors atomic.Int64
	goroutines      atomic.Int64
	eventsProcessed atomic.Uint64
	bytesProcessed  atomic.Uint64
	lastUpdate      atomic.Value // time.Time
}

// ResourceMonitor monitors system resources
type ResourceMonitor struct {
	interval     time.Duration
	samples      []ResourceSample
	maxSamples   int
	currentIndex int
	mu           sync.Mutex
	stopCh       chan struct{}
	wg           sync.WaitGroup
}

// ResourceSample represents a point-in-time resource measurement
type ResourceSample struct {
	Timestamp       time.Time
	MemoryBytes     uint64
	CPUPercent      float64
	FileDescriptors int
	Goroutines      int
	EventsPerSecond float64
}

// PoolManager manages object pools for efficient memory usage
type PoolManager struct {
	bufferPool    *sync.Pool
	eventPool     *sync.Pool
	metricsPool   *sync.Pool
	allocations   atomic.Uint64
	deallocations atomic.Uint64
	poolMisses    atomic.Uint64
}

// NewResourceManager creates a new resource manager
func NewResourceManager(config *ResourceConfig) *ResourceManager {
	if config == nil {
		config = DefaultResourceConfig()
	}

	rm := &ResourceManager{
		config: config,
		limits: defaultLimits(config),
		usage:  &ResourceUsage{},
		stopCh: make(chan struct{}),
	}

	rm.lastCleanup.Store(time.Now())
	rm.usage.lastUpdate.Store(time.Now())

	// Initialize monitor
	rm.monitor = &ResourceMonitor{
		interval:   config.MonitorInterval,
		maxSamples: 60, // Keep 1 minute of samples at 1s intervals
		samples:    make([]ResourceSample, 60),
		stopCh:     make(chan struct{}),
	}

	// Initialize pool manager
	if config.EnableBufferPool {
		rm.poolManager = NewPoolManager(config.BufferPoolSize, config.BufferSize)
	}

	// Start background workers
	rm.wg.Add(3)
	go rm.monitorResources()
	go rm.cleanupRoutine()
	go rm.adaptiveManagement()

	return rm
}

// DefaultResourceConfig returns production-ready defaults
func DefaultResourceConfig() *ResourceConfig {
	return &ResourceConfig{
		MaxMemoryMB:        512,
		MemoryWarningMB:    384, // 75% of max
		MemoryCleanupMB:    256, // 50% of max
		MaxCPUPercent:      20.0,
		CPUWarningPercent:  15.0,
		MaxFileDescriptors: 1000,
		FDWarningThreshold: 800,
		EnableBufferPool:   true,
		BufferPoolSize:     1000,
		BufferSize:         4096,
		MonitorInterval:    1 * time.Second,
		CleanupInterval:    30 * time.Second,
		ForceGCThreshold:   400, // Force GC at 400MB
		GCInterval:         1 * time.Minute,
		EnableAdaptive:     true,
		AdaptiveInterval:   10 * time.Second,
	}
}

func defaultLimits(config *ResourceConfig) *ResourceLimits {
	return &ResourceLimits{
		MemoryBytes:     uint64(config.MaxMemoryMB) * 1024 * 1024,
		CPUPercent:      config.MaxCPUPercent,
		FileDescriptors: config.MaxFileDescriptors,
		EventsPerSecond: 10000,
		MapsPerProgram:  10,
		ProgramsTotal:   100,
	}
}

// CheckMemory checks if memory usage is within limits
func (rm *ResourceManager) CheckMemory() error {
	current := rm.usage.memoryBytes.Load()
	limit := rm.limits.MemoryBytes

	if current > limit {
		rm.memoryPressure.Store(true)
		return fmt.Errorf("memory usage %d exceeds limit %d", current, limit)
	}

	warningThreshold := uint64(rm.config.MemoryWarningMB) * 1024 * 1024
	if current > warningThreshold {
		rm.memoryPressure.Store(true)
	} else {
		rm.memoryPressure.Store(false)
	}

	return nil
}

// CheckCPU checks if CPU usage is within limits
func (rm *ResourceManager) CheckCPU() error {
	currentPct := float64(rm.usage.cpuPercent.Load()) / 100.0

	if currentPct > rm.limits.CPUPercent {
		rm.cpuPressure.Store(true)
		return fmt.Errorf("CPU usage %.2f%% exceeds limit %.2f%%",
			currentPct, rm.limits.CPUPercent)
	}

	if currentPct > rm.config.CPUWarningPercent {
		rm.cpuPressure.Store(true)
	} else {
		rm.cpuPressure.Store(false)
	}

	return nil
}

// AllocateMemory attempts to allocate memory with resource checking
func (rm *ResourceManager) AllocateMemory(size int) ([]byte, error) {
	// Check if allocation would exceed limits
	current := rm.usage.memoryBytes.Load()
	if current+uint64(size) > rm.limits.MemoryBytes {
		return nil, fmt.Errorf("allocation of %d bytes would exceed memory limit", size)
	}

	// Try to get from pool first
	if rm.poolManager != nil && size <= rm.config.BufferSize {
		if buf := rm.poolManager.GetBuffer(); buf != nil {
			rm.usage.memoryBytes.Add(uint64(len(buf)))
			return buf, nil
		}
	}

	// Allocate new buffer
	buf := make([]byte, size)
	rm.usage.memoryBytes.Add(uint64(size))

	return buf, nil
}

// ReleaseMemory releases allocated memory
func (rm *ResourceManager) ReleaseMemory(buf []byte) {
	if buf == nil {
		return
	}

	size := uint64(len(buf))
	// Subtract using atomic operations
	for {
		current := rm.usage.memoryBytes.Load()
		if current >= size {
			if rm.usage.memoryBytes.CompareAndSwap(current, current-size) {
				break
			}
		} else {
			rm.usage.memoryBytes.Store(0)
			break
		}
	}

	// Return to pool if applicable
	if rm.poolManager != nil && len(buf) == rm.config.BufferSize {
		rm.poolManager.PutBuffer(buf)
	}
}

// IsUnderPressure returns true if system is under resource pressure
func (rm *ResourceManager) IsUnderPressure() bool {
	return rm.memoryPressure.Load() || rm.cpuPressure.Load()
}

// GetUsage returns current resource usage
func (rm *ResourceManager) GetUsage() ResourceSample {
	return ResourceSample{
		Timestamp:       time.Now(),
		MemoryBytes:     rm.usage.memoryBytes.Load(),
		CPUPercent:      float64(rm.usage.cpuPercent.Load()) / 100.0,
		FileDescriptors: int(rm.usage.fileDescriptors.Load()),
		Goroutines:      int(rm.usage.goroutines.Load()),
		EventsPerSecond: rm.calculateEventsPerSecond(),
	}
}

// GetMetrics returns resource management metrics
func (rm *ResourceManager) GetMetrics() map[string]interface{} {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	usage := rm.GetUsage()

	metrics := map[string]interface{}{
		"memory_bytes":      usage.MemoryBytes,
		"memory_mb":         usage.MemoryBytes / 1024 / 1024,
		"memory_percent":    float64(usage.MemoryBytes) / float64(rm.limits.MemoryBytes) * 100,
		"cpu_percent":       usage.CPUPercent,
		"file_descriptors":  usage.FileDescriptors,
		"goroutines":        usage.Goroutines,
		"events_per_second": usage.EventsPerSecond,
		"memory_pressure":   rm.memoryPressure.Load(),
		"cpu_pressure":      rm.cpuPressure.Load(),
		"under_pressure":    rm.IsUnderPressure(),
		"last_cleanup":      rm.lastCleanup.Load().(time.Time),
	}

	if rm.poolManager != nil {
		metrics["pool_stats"] = rm.poolManager.GetStats()
	}

	// Add monitor stats
	if rm.monitor != nil {
		metrics["monitor_stats"] = rm.monitor.GetStats()
	}

	return metrics
}

// ApplyBackpressure applies resource-based backpressure
func (rm *ResourceManager) ApplyBackpressure() error {
	if !rm.IsUnderPressure() {
		return nil
	}

	// Force cleanup
	if err := rm.cleanup(); err != nil {
		return fmt.Errorf("cleanup failed: %w", err)
	}

	// Force garbage collection if memory pressure
	if rm.memoryPressure.Load() {
		runtime.GC()
		runtime.Gosched() // Yield to let GC run
	}

	return nil
}

// Stop gracefully stops the resource manager
func (rm *ResourceManager) Stop() {
	close(rm.stopCh)
	rm.wg.Wait()

	if rm.monitor != nil {
		rm.monitor.Stop()
	}
}

// Background workers

func (rm *ResourceManager) monitorResources() {
	defer rm.wg.Done()

	ticker := time.NewTicker(rm.config.MonitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rm.stopCh:
			return
		case <-ticker.C:
			rm.updateResourceUsage()
			rm.checkResourceLimits()
		}
	}
}

func (rm *ResourceManager) cleanupRoutine() {
	defer rm.wg.Done()

	ticker := time.NewTicker(rm.config.CleanupInterval)
	defer ticker.Stop()

	gcTicker := time.NewTicker(rm.config.GCInterval)
	defer gcTicker.Stop()

	for {
		select {
		case <-rm.stopCh:
			return
		case <-ticker.C:
			rm.cleanup()
		case <-gcTicker.C:
			rm.performGC()
		}
	}
}

func (rm *ResourceManager) adaptiveManagement() {
	defer rm.wg.Done()

	if !rm.config.EnableAdaptive {
		return
	}

	ticker := time.NewTicker(rm.config.AdaptiveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rm.stopCh:
			return
		case <-ticker.C:
			rm.adaptLimits()
		}
	}
}

// Helper methods

func (rm *ResourceManager) updateResourceUsage() {
	// Update memory stats
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	rm.usage.memoryBytes.Store(memStats.Alloc)

	// Update goroutine count
	rm.usage.goroutines.Store(int64(runtime.NumGoroutine()))

	// Update file descriptor count (simplified)
	rm.usage.fileDescriptors.Store(int64(runtime.NumGoroutine() * 2)) // Estimate

	// Update CPU usage (simplified - would use proper CPU monitoring in production)
	// This is a placeholder
	rm.usage.cpuPercent.Store(500) // 5%

	rm.usage.lastUpdate.Store(time.Now())
}

func (rm *ResourceManager) checkResourceLimits() {
	rm.CheckMemory()
	rm.CheckCPU()

	// Check file descriptors
	fds := rm.usage.fileDescriptors.Load()
	if fds > int64(rm.config.FDWarningThreshold) {
		// Log warning or take action
	}
}

func (rm *ResourceManager) cleanup() error {
	rm.lastCleanup.Store(time.Now())

	// Free unused memory from pools
	if rm.poolManager != nil {
		rm.poolManager.Cleanup()
	}

	// Force GC if memory usage is high
	memBytes := rm.usage.memoryBytes.Load()
	memMB := int(memBytes / 1024 / 1024)
	if memMB > rm.config.MemoryCleanupMB {
		runtime.GC()
	}

	return nil
}

func (rm *ResourceManager) performGC() {
	memMB := int(rm.usage.memoryBytes.Load() / 1024 / 1024)
	if memMB > rm.config.ForceGCThreshold {
		runtime.GC()
		runtime.Gosched()
	}
}

func (rm *ResourceManager) adaptLimits() {
	// Adaptive limit adjustment based on usage patterns
	// This is a simplified implementation

	_ = rm.GetUsage()

	// If consistently under 50% memory usage, we could reduce limits
	// If consistently near limits, we might need to increase them
	// This requires more sophisticated tracking in production
}

func (rm *ResourceManager) calculateEventsPerSecond() float64 {
	events := rm.usage.eventsProcessed.Load()
	lastUpdate := rm.usage.lastUpdate.Load().(time.Time)
	elapsed := time.Since(lastUpdate).Seconds()

	if elapsed > 0 {
		return float64(events) / elapsed
	}
	return 0
}

// PoolManager implementation

func NewPoolManager(poolSize, bufferSize int) *PoolManager {
	var pm *PoolManager
	pm = &PoolManager{
		bufferPool: &sync.Pool{
			New: func() interface{} {
				pm.poolMisses.Add(1)
				return make([]byte, bufferSize)
			},
		},
		eventPool: &sync.Pool{
			New: func() interface{} {
				return &core.RawEvent{}
			},
		},
		metricsPool: &sync.Pool{
			New: func() interface{} {
				return make(map[string]interface{})
			},
		},
	}

	// Pre-populate pools
	for i := 0; i < poolSize; i++ {
		pm.bufferPool.Put(make([]byte, bufferSize))
	}

	return pm
}

func (pm *PoolManager) GetBuffer() []byte {
	pm.allocations.Add(1)
	if buf := pm.bufferPool.Get(); buf != nil {
		return buf.([]byte)
	}
	return nil
}

func (pm *PoolManager) PutBuffer(buf []byte) {
	if buf == nil {
		return
	}

	// Clear buffer before returning to pool
	for i := range buf {
		buf[i] = 0
	}

	pm.deallocations.Add(1)
	pm.bufferPool.Put(buf)
}

func (pm *PoolManager) GetEvent() *core.RawEvent {
	pm.allocations.Add(1)
	if event := pm.eventPool.Get(); event != nil {
		return event.(*core.RawEvent)
	}
	return &core.RawEvent{}
}

func (pm *PoolManager) PutEvent(event *core.RawEvent) {
	if event == nil {
		return
	}

	// Reset event
	*event = core.RawEvent{}

	pm.deallocations.Add(1)
	pm.eventPool.Put(event)
}

func (pm *PoolManager) Cleanup() {
	// Pools handle their own cleanup via GC
}

func (pm *PoolManager) GetStats() map[string]uint64 {
	return map[string]uint64{
		"allocations":   pm.allocations.Load(),
		"deallocations": pm.deallocations.Load(),
		"pool_misses":   pm.poolMisses.Load(),
		"active_items":  pm.allocations.Load() - pm.deallocations.Load(),
	}
}

// ResourceMonitor methods

func (rm *ResourceMonitor) Start() {
	rm.wg.Add(1)
	go rm.run()
}

func (rm *ResourceMonitor) Stop() {
	close(rm.stopCh)
	rm.wg.Wait()
}

func (rm *ResourceMonitor) run() {
	defer rm.wg.Done()

	ticker := time.NewTicker(rm.interval)
	defer ticker.Stop()

	for {
		select {
		case <-rm.stopCh:
			return
		case <-ticker.C:
			rm.takeSample()
		}
	}
}

func (rm *ResourceMonitor) takeSample() {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	sample := ResourceSample{
		Timestamp:       time.Now(),
		MemoryBytes:     memStats.Alloc,
		CPUPercent:      5.0,                        // Placeholder
		FileDescriptors: runtime.NumGoroutine() * 2, // Estimate
		Goroutines:      runtime.NumGoroutine(),
		EventsPerSecond: 0, // Would be updated by collector
	}

	rm.samples[rm.currentIndex] = sample
	rm.currentIndex = (rm.currentIndex + 1) % rm.maxSamples
}

func (rm *ResourceMonitor) GetStats() map[string]interface{} {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Calculate averages over samples
	var totalMem uint64
	var totalCPU float64
	var count int

	for _, sample := range rm.samples {
		if !sample.Timestamp.IsZero() {
			totalMem += sample.MemoryBytes
			totalCPU += sample.CPUPercent
			count++
		}
	}

	if count == 0 {
		return map[string]interface{}{}
	}

	return map[string]interface{}{
		"avg_memory_mb":   totalMem / uint64(count) / 1024 / 1024,
		"avg_cpu_percent": totalCPU / float64(count),
		"sample_count":    count,
	}
}
