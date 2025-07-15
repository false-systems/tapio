package exports

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"
)

// HealthMonitor monitors the health of export plugins
type HealthMonitor struct {
	plugins      map[string]ExportPlugin
	healthStatus map[string]*HealthStatus
	mutex        sync.RWMutex

	checkInterval time.Duration
	stopChan      chan struct{}
	wg            sync.WaitGroup

	// Circuit breaker for unhealthy plugins
	circuitBreakers map[string]*CircuitBreaker
}

// NewHealthMonitor creates a new health monitor
func NewHealthMonitor(checkInterval time.Duration) *HealthMonitor {
	return &HealthMonitor{
		plugins:         make(map[string]ExportPlugin),
		healthStatus:    make(map[string]*HealthStatus),
		checkInterval:   checkInterval,
		stopChan:        make(chan struct{}),
		circuitBreakers: make(map[string]*CircuitBreaker),
	}
}

// RegisterPlugin registers a plugin for health monitoring
func (hm *HealthMonitor) RegisterPlugin(name string, plugin ExportPlugin) {
	hm.mutex.Lock()
	defer hm.mutex.Unlock()

	hm.plugins[name] = plugin
	hm.circuitBreakers[name] = NewCircuitBreaker(name, 5, 1*time.Minute)

	// Perform immediate health check
	go hm.checkPluginHealth(name, plugin)
}

// UnregisterPlugin removes a plugin from health monitoring
func (hm *HealthMonitor) UnregisterPlugin(name string) {
	hm.mutex.Lock()
	defer hm.mutex.Unlock()

	delete(hm.plugins, name)
	delete(hm.healthStatus, name)
	delete(hm.circuitBreakers, name)
}

// Start starts the health monitoring
func (hm *HealthMonitor) Start(ctx context.Context) error {
	hm.wg.Add(1)
	go hm.monitorLoop(ctx)
	return nil
}

// Stop stops the health monitoring
func (hm *HealthMonitor) Stop() {
	close(hm.stopChan)
	hm.wg.Wait()
}

// GetPluginHealth retrieves health status for a specific plugin
func (hm *HealthMonitor) GetPluginHealth(name string) (*HealthStatus, error) {
	hm.mutex.RLock()
	defer hm.mutex.RUnlock()

	status, exists := hm.healthStatus[name]
	if !exists {
		return nil, fmt.Errorf("plugin %s not found", name)
	}

	return status, nil
}

// GetAllHealth retrieves health status for all plugins
func (hm *HealthMonitor) GetAllHealth() map[string]*HealthStatus {
	hm.mutex.RLock()
	defer hm.mutex.RUnlock()

	result := make(map[string]*HealthStatus)
	for name, status := range hm.healthStatus {
		result[name] = status
	}

	return result
}

// UpdateHealth updates the health status for a plugin
func (hm *HealthMonitor) UpdateHealth(name string, status *HealthStatus) {
	hm.mutex.Lock()
	defer hm.mutex.Unlock()

	hm.healthStatus[name] = status

	// Update circuit breaker
	if cb, exists := hm.circuitBreakers[name]; exists {
		if status.Healthy {
			cb.RecordSuccess()
		} else {
			cb.RecordFailure()
		}
	}
}

// IsPluginHealthy checks if a plugin is healthy
func (hm *HealthMonitor) IsPluginHealthy(name string) bool {
	hm.mutex.RLock()
	defer hm.mutex.RUnlock()

	// Check circuit breaker first
	if cb, exists := hm.circuitBreakers[name]; exists && !cb.AllowRequest() {
		return false
	}

	status, exists := hm.healthStatus[name]
	if !exists {
		return false
	}

	// Consider plugin unhealthy if last check was too long ago
	if time.Since(status.LastCheck) > hm.checkInterval*3 {
		return false
	}

	return status.Healthy
}

// monitorLoop is the main monitoring loop
func (hm *HealthMonitor) monitorLoop(ctx context.Context) {
	defer hm.wg.Done()

	ticker := time.NewTicker(hm.checkInterval)
	defer ticker.Stop()

	// Initial health check
	hm.checkAllPlugins(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-hm.stopChan:
			return
		case <-ticker.C:
			hm.checkAllPlugins(ctx)
		}
	}
}

// checkAllPlugins checks health of all registered plugins
func (hm *HealthMonitor) checkAllPlugins(ctx context.Context) {
	hm.mutex.RLock()
	plugins := make(map[string]ExportPlugin)
	for name, plugin := range hm.plugins {
		plugins[name] = plugin
	}
	hm.mutex.RUnlock()

	// Check each plugin concurrently
	var wg sync.WaitGroup
	for name, plugin := range plugins {
		wg.Add(1)
		go func(n string, p ExportPlugin) {
			defer wg.Done()
			hm.checkPluginHealth(n, p)
		}(name, plugin)
	}
	wg.Wait()
}

// checkPluginHealth checks health of a single plugin
func (hm *HealthMonitor) checkPluginHealth(name string, plugin ExportPlugin) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get resource usage before health check
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	// Perform health check
	status, err := plugin.HealthCheck(ctx)
	if err != nil {
		status = &HealthStatus{
			Healthy:   false,
			LastCheck: time.Now(),
			Message:   fmt.Sprintf("Health check failed: %v", err),
		}
	}

	// Get resource usage after health check
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	// Add resource usage if not already set
	if status.ResourceUsage == nil {
		status.ResourceUsage = &ResourceUsage{
			MemoryMB:       float64(memAfter.Alloc-memBefore.Alloc) / 1024 / 1024,
			GoroutineCount: runtime.NumGoroutine(),
		}
	}

	// Get plugin metrics
	if metrics := plugin.GetMetrics(); metrics != nil {
		if status.Details == nil {
			status.Details = make(map[string]interface{})
		}
		status.Details["metrics"] = metrics
	}

	// Update health status
	hm.UpdateHealth(name, status)
}

// CircuitBreaker implements circuit breaker pattern for plugin health
type CircuitBreaker struct {
	name             string
	failureThreshold int
	resetTimeout     time.Duration

	failures        int
	lastFailureTime time.Time
	state           CircuitState
	mutex           sync.Mutex
}

// CircuitState represents circuit breaker states
type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(name string, threshold int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		name:             name,
		failureThreshold: threshold,
		resetTimeout:     timeout,
		state:            CircuitClosed,
	}
}

// AllowRequest checks if requests are allowed
func (cb *CircuitBreaker) AllowRequest() bool {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	switch cb.state {
	case CircuitClosed:
		return true

	case CircuitOpen:
		// Check if we should transition to half-open
		if time.Since(cb.lastFailureTime) > cb.resetTimeout {
			cb.state = CircuitHalfOpen
			cb.failures = 0
			return true
		}
		return false

	case CircuitHalfOpen:
		return true

	default:
		return false
	}
}

// RecordSuccess records a successful operation
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	if cb.state == CircuitHalfOpen {
		cb.state = CircuitClosed
	}
	cb.failures = 0
}

// RecordFailure records a failed operation
func (cb *CircuitBreaker) RecordFailure() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.failures++
	cb.lastFailureTime = time.Now()

	if cb.failures >= cb.failureThreshold {
		cb.state = CircuitOpen
	}
}

// GetState returns the current circuit breaker state
func (cb *CircuitBreaker) GetState() string {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	switch cb.state {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// HealthAggregator aggregates health metrics across plugins
type HealthAggregator struct {
	monitor *HealthMonitor
}

// NewHealthAggregator creates a new health aggregator
func NewHealthAggregator(monitor *HealthMonitor) *HealthAggregator {
	return &HealthAggregator{
		monitor: monitor,
	}
}

// GetSystemHealth returns overall system health
func (ha *HealthAggregator) GetSystemHealth() *SystemHealth {
	allHealth := ha.monitor.GetAllHealth()

	system := &SystemHealth{
		Timestamp:      time.Now(),
		PluginCount:    len(allHealth),
		HealthyPlugins: 0,
		Plugins:        make(map[string]*PluginHealthSummary),
	}

	var totalMemory float64
	var totalCPU float64
	var totalExports float64

	for name, health := range allHealth {
		if health.Healthy {
			system.HealthyPlugins++
		}

		summary := &PluginHealthSummary{
			Name:         name,
			Healthy:      health.Healthy,
			LastCheck:    health.LastCheck,
			Message:      health.Message,
			CircuitState: "unknown",
		}

		// Get circuit breaker state
		if cb, exists := ha.monitor.circuitBreakers[name]; exists {
			summary.CircuitState = cb.GetState()
		}

		// Aggregate resource usage
		if health.ResourceUsage != nil {
			totalMemory += health.ResourceUsage.MemoryMB
			totalCPU += health.ResourceUsage.CPUPercent
			totalExports += health.ResourceUsage.ExportsPerSec

			summary.ResourceUsage = health.ResourceUsage
		}

		system.Plugins[name] = summary
	}

	// Calculate overall health score
	if system.PluginCount > 0 {
		system.HealthScore = float64(system.HealthyPlugins) / float64(system.PluginCount)
		system.TotalMemoryMB = totalMemory
		system.TotalCPUPercent = totalCPU
		system.TotalExportsPerSec = totalExports
	}

	return system
}

// SystemHealth represents overall system health
type SystemHealth struct {
	Timestamp          time.Time                       `json:"timestamp"`
	HealthScore        float64                         `json:"health_score"`
	PluginCount        int                             `json:"plugin_count"`
	HealthyPlugins     int                             `json:"healthy_plugins"`
	TotalMemoryMB      float64                         `json:"total_memory_mb"`
	TotalCPUPercent    float64                         `json:"total_cpu_percent"`
	TotalExportsPerSec float64                         `json:"total_exports_per_sec"`
	Plugins            map[string]*PluginHealthSummary `json:"plugins"`
}

// PluginHealthSummary provides a summary of plugin health
type PluginHealthSummary struct {
	Name          string         `json:"name"`
	Healthy       bool           `json:"healthy"`
	LastCheck     time.Time      `json:"last_check"`
	Message       string         `json:"message,omitempty"`
	CircuitState  string         `json:"circuit_state"`
	ResourceUsage *ResourceUsage `json:"resource_usage,omitempty"`
}

// HealthCheckResult represents a health check result
type HealthCheckResult struct {
	PluginName    string                 `json:"plugin_name"`
	Healthy       bool                   `json:"healthy"`
	CheckDuration time.Duration          `json:"check_duration"`
	Error         error                  `json:"error,omitempty"`
	Details       map[string]interface{} `json:"details,omitempty"`
}

// BatchHealthCheck performs health checks on multiple plugins
func (hm *HealthMonitor) BatchHealthCheck(pluginNames []string) []*HealthCheckResult {
	results := make([]*HealthCheckResult, 0, len(pluginNames))
	resultsChan := make(chan *HealthCheckResult, len(pluginNames))

	var wg sync.WaitGroup
	for _, name := range pluginNames {
		wg.Add(1)
		go func(pluginName string) {
			defer wg.Done()

			result := &HealthCheckResult{
				PluginName: pluginName,
			}

			hm.mutex.RLock()
			plugin, exists := hm.plugins[pluginName]
			hm.mutex.RUnlock()

			if !exists {
				result.Error = fmt.Errorf("plugin not found")
				resultsChan <- result
				return
			}

			start := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			status, err := plugin.HealthCheck(ctx)
			result.CheckDuration = time.Since(start)

			if err != nil {
				result.Error = err
				result.Healthy = false
			} else {
				result.Healthy = status.Healthy
				result.Details = status.Details
			}

			resultsChan <- result
		}(name)
	}

	// Wait for all checks to complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results
	for result := range resultsChan {
		results = append(results, result)
	}

	return results
}
