package blueprint

import (
	"context"
	"runtime"
	"sync"
	"time"

	"go.uber.org/zap"
)

// resourceMonitor tracks and enforces resource usage limits
type resourceMonitor struct {
	limits  ResourceLimits
	logger  *zap.Logger
	mu      sync.RWMutex
	usage   ResourceUsage
	healthy bool
}

// newResourceMonitor creates a new resource monitor
func newResourceMonitor(limits ResourceLimits, logger *zap.Logger) *resourceMonitor {
	return &resourceMonitor{
		limits:  limits,
		logger:  logger,
		healthy: true,
		usage: ResourceUsage{
			LastUpdated: time.Now(),
		},
	}
}

// start begins resource monitoring
func (r *resourceMonitor) start(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.updateResourceUsage()
		}
	}
}

// updateResourceUsage collects current resource usage
func (r *resourceMonitor) updateResourceUsage() {
	r.mu.Lock()
	defer r.mu.Unlock()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Convert bytes to MB
	allocMB := float64(m.Alloc) / 1024 / 1024
	sysMB := float64(m.Sys) / 1024 / 1024

	r.usage = ResourceUsage{
		MemoryMB:       allocMB,
		CPUPercent:     r.getCPUUsage(), // Simplified - would use more accurate measurement in production
		Goroutines:     runtime.NumGoroutine(),
		OpenFiles:      r.getOpenFileCount(),
		NetworkBytesRx: 0, // Would implement network monitoring
		NetworkBytesTx: 0, // Would implement network monitoring
		DiskUsageMB:    sysMB,
		LastUpdated:    time.Now(),
	}

	// Check if within limits
	r.healthy = r.isWithinLimits(r.usage)

	if !r.healthy {
		r.logger.Warn("Resource limits exceeded",
			zap.Float64("memory_mb", r.usage.MemoryMB),
			zap.Int("max_memory_mb", r.limits.MaxMemoryMB),
			zap.Float64("cpu_percent", r.usage.CPUPercent),
			zap.Int("max_cpu_percent", r.limits.MaxCPUPercent),
			zap.Int("goroutines", r.usage.Goroutines),
			zap.Int("max_goroutines", r.limits.MaxGoroutines),
		)
	}
}

// getResourceUsage returns current resource usage
func (r *resourceMonitor) getResourceUsage() ResourceUsage {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.usage
}

// isWithinLimits checks if usage is within configured limits
func (r *resourceMonitor) isWithinLimits(usage ResourceUsage) bool {
	if usage.MemoryMB > float64(r.limits.MaxMemoryMB) {
		return false
	}
	if usage.CPUPercent > float64(r.limits.MaxCPUPercent) {
		return false
	}
	if usage.Goroutines > r.limits.MaxGoroutines {
		return false
	}
	if usage.OpenFiles > r.limits.MaxOpenFiles {
		return false
	}
	if usage.DiskUsageMB > float64(r.limits.MaxDiskUsageMB) {
		return false
	}
	return true
}

// getCPUUsage returns approximate CPU usage percentage
// In production, this would use more accurate CPU monitoring
func (r *resourceMonitor) getCPUUsage() float64 {
	// Simplified implementation - would use proper CPU monitoring
	// This is just a placeholder
	return 0.0
}

// getOpenFileCount returns approximate open file count
// In production, this would query the actual file descriptor count
func (r *resourceMonitor) getOpenFileCount() int {
	// Simplified implementation - would check /proc/self/fd or similar
	// This is just a placeholder
	return 0
}

// setLimits updates resource limits
func (r *resourceMonitor) setLimits(limits ResourceLimits) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.limits = limits
	r.logger.Info("Resource limits updated",
		zap.Int("max_memory_mb", limits.MaxMemoryMB),
		zap.Int("max_cpu_percent", limits.MaxCPUPercent),
		zap.Int("max_goroutines", limits.MaxGoroutines),
		zap.Int("max_open_files", limits.MaxOpenFiles),
	)
}

// isHealthy returns true if resource usage is within limits
func (r *resourceMonitor) isHealthy() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.healthy
}
