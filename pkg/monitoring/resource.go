package monitoring

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// ResourceMonitor provides lightweight resource usage monitoring for DaemonSet collectors
type ResourceMonitor struct {
	limits        ResourceLimits
	
	// Current usage
	memoryUsageMB   uint64  // Atomic
	cpuUsagePercent uint64  // Atomic (scaled by 100 for precision)
	
	// Statistics
	maxMemoryMB     uint64  // Atomic
	maxCPUPercent   uint64  // Atomic
	alertsTriggered uint64  // Atomic
	
	// Monitoring state
	started         atomic.Bool
	stopped         atomic.Bool
	
	// Lifecycle
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	
	// Callbacks
	alertHandlers  []AlertHandler
	handlersMu     sync.RWMutex
}

// ResourceLimits defines resource usage limits for collectors
type ResourceLimits struct {
	MaxMemoryMB int `json:"max_memory_mb"` // Maximum memory in MB
	MaxCPUMilli int `json:"max_cpu_milli"` // Maximum CPU in milli-cores (1000 = 1 core)
}

// ResourceUsage represents current resource usage
type ResourceUsage struct {
	MemoryMB      float64   `json:"memory_mb"`
	CPUPercent    float64   `json:"cpu_percent"`
	Timestamp     time.Time `json:"timestamp"`
	WithinLimits  bool      `json:"within_limits"`
	MemoryLimit   int       `json:"memory_limit_mb"`
	CPULimit      float64   `json:"cpu_limit_percent"`
}

// AlertHandler defines the interface for handling resource alerts
type AlertHandler interface {
	HandleAlert(alert ResourceAlert)
}

// ResourceAlert represents a resource usage alert
type ResourceAlert struct {
	Type        AlertType `json:"type"`
	Severity    string    `json:"severity"`
	Message     string    `json:"message"`
	CurrentMB   float64   `json:"current_mb,omitempty"`
	LimitMB     int       `json:"limit_mb,omitempty"`
	CurrentCPU  float64   `json:"current_cpu_percent,omitempty"`
	LimitCPU    float64   `json:"limit_cpu_percent,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
}

// AlertType represents the type of resource alert
type AlertType string

const (
	AlertTypeMemoryHigh     AlertType = "memory_high"
	AlertTypeMemoryCritical AlertType = "memory_critical"
	AlertTypeCPUHigh        AlertType = "cpu_high"
	AlertTypeCPUCritical    AlertType = "cpu_critical"
	AlertTypeRecovered      AlertType = "recovered"
)

// NewResourceMonitor creates a new resource monitor
func NewResourceMonitor(limits ResourceLimits) *ResourceMonitor {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &ResourceMonitor{
		limits:        limits,
		ctx:           ctx,
		cancel:        cancel,
		alertHandlers: make([]AlertHandler, 0),
	}
}

// Start begins resource monitoring
func (rm *ResourceMonitor) Start(ctx context.Context) error {
	if !rm.started.CompareAndSwap(false, true) {
		return fmt.Errorf("resource monitor already started")
	}
	
	// Start monitoring goroutine
	rm.wg.Add(1)
	go rm.monitorResources()
	
	return nil
}

// Shutdown stops resource monitoring
func (rm *ResourceMonitor) Shutdown() error {
	if !rm.stopped.CompareAndSwap(false, true) {
		return nil // Already stopped
	}
	
	rm.cancel()
	rm.wg.Wait()
	
	return nil
}

// GetUsage returns current resource usage
func (rm *ResourceMonitor) GetUsage() ResourceUsage {
	memoryMB := float64(atomic.LoadUint64(&rm.memoryUsageMB)) / 100.0
	cpuPercent := float64(atomic.LoadUint64(&rm.cpuUsagePercent)) / 100.0
	
	withinLimits := memoryMB <= float64(rm.limits.MaxMemoryMB) && 
		cpuPercent <= float64(rm.limits.MaxCPUMilli)/10.0
	
	return ResourceUsage{
		MemoryMB:     memoryMB,
		CPUPercent:   cpuPercent,
		Timestamp:    time.Now(),
		WithinLimits: withinLimits,
		MemoryLimit:  rm.limits.MaxMemoryMB,
		CPULimit:     float64(rm.limits.MaxCPUMilli) / 10.0,
	}
}

// GetStats returns detailed monitoring statistics
func (rm *ResourceMonitor) GetStats() ResourceStats {
	maxMemoryMB := float64(atomic.LoadUint64(&rm.maxMemoryMB)) / 100.0
	maxCPUPercent := float64(atomic.LoadUint64(&rm.maxCPUPercent)) / 100.0
	
	return ResourceStats{
		CurrentUsage:    rm.GetUsage(),
		MaxMemoryMB:     maxMemoryMB,
		MaxCPUPercent:   maxCPUPercent,
		AlertsTriggered: atomic.LoadUint64(&rm.alertsTriggered),
		MonitoringTime:  time.Since(time.Now()), // Placeholder
	}
}

// AddAlertHandler adds a handler for resource alerts
func (rm *ResourceMonitor) AddAlertHandler(handler AlertHandler) {
	rm.handlersMu.Lock()
	defer rm.handlersMu.Unlock()
	
	rm.alertHandlers = append(rm.alertHandlers, handler)
}

// monitorResources continuously monitors resource usage
func (rm *ResourceMonitor) monitorResources() {
	defer rm.wg.Done()
	
	ticker := time.NewTicker(5 * time.Second) // Monitor every 5 seconds
	defer ticker.Stop()
	
	var lastMemoryAlert, lastCPUAlert time.Time
	const alertCooldown = 30 * time.Second
	
	for {
		select {
		case <-rm.ctx.Done():
			return
		case <-ticker.C:
			rm.updateResourceUsage()
			rm.checkAlerts(&lastMemoryAlert, &lastCPUAlert, alertCooldown)
		}
	}
}

// updateResourceUsage updates current resource usage metrics
func (rm *ResourceMonitor) updateResourceUsage() {
	// Get memory usage
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	
	// Convert to MB and store (scaled by 100 for precision)
	memoryMB := uint64(float64(memStats.Alloc) / 1024 / 1024 * 100)
	atomic.StoreUint64(&rm.memoryUsageMB, memoryMB)
	
	// Update max memory if needed
	for {
		current := atomic.LoadUint64(&rm.maxMemoryMB)
		if memoryMB <= current || atomic.CompareAndSwapUint64(&rm.maxMemoryMB, current, memoryMB) {
			break
		}
	}
	
	// Get CPU usage (simplified - in production would use more sophisticated measurement)
	cpuPercent := rm.getCPUUsage()
	cpuPercentScaled := uint64(cpuPercent * 100)
	atomic.StoreUint64(&rm.cpuUsagePercent, cpuPercentScaled)
	
	// Update max CPU if needed
	for {
		current := atomic.LoadUint64(&rm.maxCPUPercent)
		if cpuPercentScaled <= current || atomic.CompareAndSwapUint64(&rm.maxCPUPercent, current, cpuPercentScaled) {
			break
		}
	}
}

// getCPUUsage returns current CPU usage percentage (simplified implementation)
func (rm *ResourceMonitor) getCPUUsage() float64 {
	// This is a simplified implementation
	// In production, you'd want to use more sophisticated CPU measurement
	// such as reading from /proc/stat or using cgroups
	
	numGoroutines := runtime.NumGoroutine()
	numCPU := runtime.NumCPU()
	
	// Simple heuristic: assume each goroutine uses a small amount of CPU
	// This is not accurate but provides a baseline
	estimatedCPU := float64(numGoroutines) / float64(numCPU) * 0.1
	
	if estimatedCPU > 100.0 {
		estimatedCPU = 100.0
	}
	
	return estimatedCPU
}

// checkAlerts checks if resource usage exceeds thresholds and triggers alerts
func (rm *ResourceMonitor) checkAlerts(lastMemoryAlert, lastCPUAlert *time.Time, cooldown time.Duration) {
	now := time.Now()
	memoryMB := float64(atomic.LoadUint64(&rm.memoryUsageMB)) / 100.0
	cpuPercent := float64(atomic.LoadUint64(&rm.cpuUsagePercent)) / 100.0
	
	// Check memory alerts
	if memoryMB > float64(rm.limits.MaxMemoryMB) {
		if now.Sub(*lastMemoryAlert) > cooldown {
			alert := ResourceAlert{
				Type:       AlertTypeMemoryCritical,
				Severity:   "critical",
				Message:    fmt.Sprintf("Memory usage %.1fMB exceeds limit %dMB", memoryMB, rm.limits.MaxMemoryMB),
				CurrentMB:  memoryMB,
				LimitMB:    rm.limits.MaxMemoryMB,
				Timestamp:  now,
			}
			rm.triggerAlert(alert)
			*lastMemoryAlert = now
		}
	} else if memoryMB > float64(rm.limits.MaxMemoryMB)*0.8 {
		if now.Sub(*lastMemoryAlert) > cooldown {
			alert := ResourceAlert{
				Type:       AlertTypeMemoryHigh,
				Severity:   "warning",
				Message:    fmt.Sprintf("Memory usage %.1fMB is high (80%% of limit %dMB)", memoryMB, rm.limits.MaxMemoryMB),
				CurrentMB:  memoryMB,
				LimitMB:    rm.limits.MaxMemoryMB,
				Timestamp:  now,
			}
			rm.triggerAlert(alert)
			*lastMemoryAlert = now
		}
	}
	
	// Check CPU alerts
	cpuLimitPercent := float64(rm.limits.MaxCPUMilli) / 10.0
	if cpuPercent > cpuLimitPercent {
		if now.Sub(*lastCPUAlert) > cooldown {
			alert := ResourceAlert{
				Type:       AlertTypeCPUCritical,
				Severity:   "critical",
				Message:    fmt.Sprintf("CPU usage %.1f%% exceeds limit %.1f%%", cpuPercent, cpuLimitPercent),
				CurrentCPU: cpuPercent,
				LimitCPU:   cpuLimitPercent,
				Timestamp:  now,
			}
			rm.triggerAlert(alert)
			*lastCPUAlert = now
		}
	} else if cpuPercent > cpuLimitPercent*0.8 {
		if now.Sub(*lastCPUAlert) > cooldown {
			alert := ResourceAlert{
				Type:       AlertTypeCPUHigh,
				Severity:   "warning",
				Message:    fmt.Sprintf("CPU usage %.1f%% is high (80%% of limit %.1f%%)", cpuPercent, cpuLimitPercent),
				CurrentCPU: cpuPercent,
				LimitCPU:   cpuLimitPercent,
				Timestamp:  now,
			}
			rm.triggerAlert(alert)
			*lastCPUAlert = now
		}
	}
}

// triggerAlert sends an alert to all registered handlers
func (rm *ResourceMonitor) triggerAlert(alert ResourceAlert) {
	atomic.AddUint64(&rm.alertsTriggered, 1)
	
	rm.handlersMu.RLock()
	handlers := make([]AlertHandler, len(rm.alertHandlers))
	copy(handlers, rm.alertHandlers)
	rm.handlersMu.RUnlock()
	
	for _, handler := range handlers {
		go handler.HandleAlert(alert)
	}
}

// ResourceStats provides detailed resource monitoring statistics
type ResourceStats struct {
	CurrentUsage    ResourceUsage `json:"current_usage"`
	MaxMemoryMB     float64       `json:"max_memory_mb"`
	MaxCPUPercent   float64       `json:"max_cpu_percent"`
	AlertsTriggered uint64        `json:"alerts_triggered"`
	MonitoringTime  time.Duration `json:"monitoring_time"`
}

// ConsoleAlertHandler implements AlertHandler for console output
type ConsoleAlertHandler struct{}

// NewConsoleAlertHandler creates a new console alert handler
func NewConsoleAlertHandler() *ConsoleAlertHandler {
	return &ConsoleAlertHandler{}
}

// HandleAlert handles resource alerts by printing to console
func (c *ConsoleAlertHandler) HandleAlert(alert ResourceAlert) {
	icon := "⚠️"
	if alert.Severity == "critical" {
		icon = "🚨"
	}
	
	fmt.Printf("%s Resource Alert [%s]: %s\n", icon, alert.Severity, alert.Message)
}

// LoggingAlertHandler implements AlertHandler for structured logging
type LoggingAlertHandler struct {
	// Would integrate with structured logging library
}

// NewLoggingAlertHandler creates a new logging alert handler
func NewLoggingAlertHandler() *LoggingAlertHandler {
	return &LoggingAlertHandler{}
}

// HandleAlert handles resource alerts by logging them
func (l *LoggingAlertHandler) HandleAlert(alert ResourceAlert) {
	// In production, this would use a structured logging library
	// For now, we'll use simple logging
	fmt.Printf("ALERT: %+v\n", alert)
}

// ResourceLimiter provides resource usage limiting capabilities
type ResourceLimiter struct {
	limits   ResourceLimits
	monitor  *ResourceMonitor
	enabled  atomic.Bool
}

// NewResourceLimiter creates a new resource limiter
func NewResourceLimiter(limits ResourceLimits, monitor *ResourceMonitor) *ResourceLimiter {
	return &ResourceLimiter{
		limits:  limits,
		monitor: monitor,
	}
}

// EnableLimiting enables resource limiting
func (rl *ResourceLimiter) EnableLimiting() {
	rl.enabled.Store(true)
}

// DisableLimiting disables resource limiting
func (rl *ResourceLimiter) DisableLimiting() {
	rl.enabled.Store(false)
}

// ShouldThrottle returns true if operations should be throttled due to resource usage
func (rl *ResourceLimiter) ShouldThrottle() bool {
	if !rl.enabled.Load() {
		return false
	}
	
	usage := rl.monitor.GetUsage()
	
	// Throttle if memory usage is above 90% of limit
	if usage.MemoryMB > float64(rl.limits.MaxMemoryMB)*0.9 {
		return true
	}
	
	// Throttle if CPU usage is above 90% of limit
	if usage.CPUPercent > float64(rl.limits.MaxCPUMilli)/10.0*0.9 {
		return true
	}
	
	return false
}

// ForceGarbageCollection forces a garbage collection cycle if memory usage is high
func (rl *ResourceLimiter) ForceGarbageCollection() {
	usage := rl.monitor.GetUsage()
	
	// Force GC if memory usage is above 80% of limit
	if usage.MemoryMB > float64(rl.limits.MaxMemoryMB)*0.8 {
		runtime.GC()
		runtime.GC() // Call twice to be more aggressive
	}
}