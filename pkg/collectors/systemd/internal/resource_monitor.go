package internal

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ResourceMonitor tracks CPU/memory usage per service via cgroups
type ResourceMonitor struct {
	mu             sync.RWMutex
	serviceMetrics map[string]*ServiceResourceMetrics
	cgroupVersion  int // 1 or 2
	cgroupBasePath string
	updateInterval time.Duration

	// Metrics
	totalServices    int
	monitoringErrors uint64
}

// ServiceResourceMetrics contains resource usage for a service
type ServiceResourceMetrics struct {
	ServiceName      string
	CPUUsageNanos    uint64
	CPUUsagePercent  float64
	MemoryUsageBytes uint64
	MemoryLimitBytes uint64
	MemoryPercent    float64
	FDCount          int
	ThreadCount      int
	RestartCount     int
	LastUpdate       time.Time

	// Historical data for trend detection
	CPUHistory     []float64
	MemoryHistory  []uint64
	RestartHistory []time.Time
}

// ResourceAlert represents a resource-related alert
type ResourceAlert struct {
	ServiceName string
	AlertType   string
	Message     string
	Value       float64
	Threshold   float64
	Timestamp   time.Time
}

// NewResourceMonitor creates a new resource monitor
func NewResourceMonitor() *ResourceMonitor {
	rm := &ResourceMonitor{
		serviceMetrics: make(map[string]*ServiceResourceMetrics),
		updateInterval: 10 * time.Second,
	}

	// Detect cgroup version
	rm.detectCgroupVersion()

	return rm
}

// detectCgroupVersion detects whether we're using cgroups v1 or v2
func (rm *ResourceMonitor) detectCgroupVersion() {
	// Check for cgroup v2
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err == nil {
		rm.cgroupVersion = 2
		rm.cgroupBasePath = "/sys/fs/cgroup"
	} else {
		rm.cgroupVersion = 1
		rm.cgroupBasePath = "/sys/fs/cgroup/systemd"
	}
}

// MonitorService starts monitoring a specific service
func (rm *ResourceMonitor) MonitorService(serviceName string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if _, exists := rm.serviceMetrics[serviceName]; exists {
		return nil // Already monitoring
	}

	metrics := &ServiceResourceMetrics{
		ServiceName:    serviceName,
		CPUHistory:     make([]float64, 0, 60), // Keep 10 minutes of history
		MemoryHistory:  make([]uint64, 0, 60),
		RestartHistory: make([]time.Time, 0, 10),
		LastUpdate:     time.Now(),
	}

	rm.serviceMetrics[serviceName] = metrics
	rm.totalServices++

	return nil
}

// UpdateMetrics updates metrics for all monitored services
func (rm *ResourceMonitor) UpdateMetrics() {
	rm.mu.RLock()
	services := make([]string, 0, len(rm.serviceMetrics))
	for name := range rm.serviceMetrics {
		services = append(services, name)
	}
	rm.mu.RUnlock()

	for _, serviceName := range services {
		if err := rm.updateServiceMetrics(serviceName); err != nil {
			rm.monitoringErrors++
		}
	}
}

// updateServiceMetrics updates metrics for a specific service
func (rm *ResourceMonitor) updateServiceMetrics(serviceName string) error {
	rm.mu.Lock()
	metrics, exists := rm.serviceMetrics[serviceName]
	if !exists {
		rm.mu.Unlock()
		return fmt.Errorf("service not monitored: %s", serviceName)
	}
	rm.mu.Unlock()

	// Get cgroup path for service
	cgroupPath := rm.getServiceCgroupPath(serviceName)

	// Update CPU usage
	cpuUsage, err := rm.getCPUUsage(cgroupPath)
	if err == nil {
		metrics.CPUUsageNanos = cpuUsage
		// Calculate percentage (simplified - would need previous value for accurate calc)
		metrics.CPUUsagePercent = float64(cpuUsage) / 1e9 / 100.0

		// Update history
		metrics.CPUHistory = append(metrics.CPUHistory, metrics.CPUUsagePercent)
		if len(metrics.CPUHistory) > 60 {
			metrics.CPUHistory = metrics.CPUHistory[1:]
		}
	}

	// Update memory usage
	memUsage, memLimit, err := rm.getMemoryUsage(cgroupPath)
	if err == nil {
		metrics.MemoryUsageBytes = memUsage
		metrics.MemoryLimitBytes = memLimit
		if memLimit > 0 {
			metrics.MemoryPercent = float64(memUsage) / float64(memLimit) * 100.0
		}

		// Update history
		metrics.MemoryHistory = append(metrics.MemoryHistory, memUsage)
		if len(metrics.MemoryHistory) > 60 {
			metrics.MemoryHistory = metrics.MemoryHistory[1:]
		}
	}

	// Update file descriptor count
	fdCount, err := rm.getFileDescriptorCount(serviceName)
	if err == nil {
		metrics.FDCount = fdCount
	}

	// Update thread count
	threadCount, err := rm.getThreadCount(serviceName)
	if err == nil {
		metrics.ThreadCount = threadCount
	}

	metrics.LastUpdate = time.Now()

	rm.mu.Lock()
	rm.serviceMetrics[serviceName] = metrics
	rm.mu.Unlock()

	return nil
}

// getServiceCgroupPath returns the cgroup path for a service
func (rm *ResourceMonitor) getServiceCgroupPath(serviceName string) string {
	if rm.cgroupVersion == 2 {
		return filepath.Join(rm.cgroupBasePath, "system.slice", serviceName)
	}
	return filepath.Join(rm.cgroupBasePath, "system.slice", serviceName)
}

// getCPUUsage reads CPU usage from cgroup
func (rm *ResourceMonitor) getCPUUsage(cgroupPath string) (uint64, error) {
	var cpuStatPath string
	if rm.cgroupVersion == 2 {
		cpuStatPath = filepath.Join(cgroupPath, "cpu.stat")
	} else {
		cpuStatPath = filepath.Join(strings.Replace(cgroupPath, "systemd", "cpu,cpuacct", 1), "cpuacct.usage")
	}

	data, err := os.ReadFile(cpuStatPath)
	if err != nil {
		return 0, err
	}

	if rm.cgroupVersion == 2 {
		// Parse cpu.stat format
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) >= 2 && fields[0] == "usage_usec" {
				return strconv.ParseUint(fields[1], 10, 64)
			}
		}
		return 0, fmt.Errorf("usage_usec not found in cpu.stat")
	} else {
		// cgroup v1: direct usage in nanoseconds
		return strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	}
}

// getMemoryUsage reads memory usage from cgroup
func (rm *ResourceMonitor) getMemoryUsage(cgroupPath string) (usage, limit uint64, err error) {
	var memStatPath, memLimitPath string

	if rm.cgroupVersion == 2 {
		memStatPath = filepath.Join(cgroupPath, "memory.current")
		memLimitPath = filepath.Join(cgroupPath, "memory.max")
	} else {
		memPath := strings.Replace(cgroupPath, "systemd", "memory", 1)
		memStatPath = filepath.Join(memPath, "memory.usage_in_bytes")
		memLimitPath = filepath.Join(memPath, "memory.limit_in_bytes")
	}

	// Read current usage
	data, err := os.ReadFile(memStatPath)
	if err != nil {
		return 0, 0, err
	}
	usage, err = strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, 0, err
	}

	// Read limit
	data, err = os.ReadFile(memLimitPath)
	if err != nil {
		return usage, 0, nil // Limit might not be set
	}

	limitStr := strings.TrimSpace(string(data))
	if limitStr == "max" || limitStr == "9223372036854775807" {
		// No limit set
		limit = 0
	} else {
		limit, err = strconv.ParseUint(limitStr, 10, 64)
		if err != nil {
			limit = 0
		}
	}

	return usage, limit, nil
}

// getFileDescriptorCount gets the number of open file descriptors
func (rm *ResourceMonitor) getFileDescriptorCount(serviceName string) (int, error) {
	// This would typically read from /proc/[pid]/fd/
	// For now, return a placeholder
	// In production, you'd get the main PID from systemctl show -p MainPID
	return 0, nil
}

// getThreadCount gets the number of threads
func (rm *ResourceMonitor) getThreadCount(serviceName string) (int, error) {
	// This would typically read from /proc/[pid]/status
	// For now, return a placeholder
	return 0, nil
}

// DetectAnomalies checks for resource anomalies
func (rm *ResourceMonitor) DetectAnomalies() []ResourceAlert {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	alerts := []ResourceAlert{}

	for serviceName, metrics := range rm.serviceMetrics {
		// Check CPU spike
		if metrics.CPUUsagePercent > 80.0 {
			alerts = append(alerts, ResourceAlert{
				ServiceName: serviceName,
				AlertType:   "cpu_high",
				Message:     fmt.Sprintf("CPU usage is %.1f%%", metrics.CPUUsagePercent),
				Value:       metrics.CPUUsagePercent,
				Threshold:   80.0,
				Timestamp:   time.Now(),
			})
		}

		// Check memory usage
		if metrics.MemoryPercent > 90.0 {
			alerts = append(alerts, ResourceAlert{
				ServiceName: serviceName,
				AlertType:   "memory_high",
				Message:     fmt.Sprintf("Memory usage is %.1f%%", metrics.MemoryPercent),
				Value:       metrics.MemoryPercent,
				Threshold:   90.0,
				Timestamp:   time.Now(),
			})
		}

		// Check restart frequency
		recentRestarts := 0
		cutoff := time.Now().Add(-5 * time.Minute)
		for _, restartTime := range metrics.RestartHistory {
			if restartTime.After(cutoff) {
				recentRestarts++
			}
		}

		if recentRestarts >= 3 {
			alerts = append(alerts, ResourceAlert{
				ServiceName: serviceName,
				AlertType:   "restart_loop",
				Message:     fmt.Sprintf("Service restarted %d times in 5 minutes", recentRestarts),
				Value:       float64(recentRestarts),
				Threshold:   3.0,
				Timestamp:   time.Now(),
			})
		}

		// Check file descriptor exhaustion
		if metrics.FDCount > 900 { // Assuming 1024 limit
			alerts = append(alerts, ResourceAlert{
				ServiceName: serviceName,
				AlertType:   "fd_exhaustion",
				Message:     fmt.Sprintf("File descriptors: %d/1024", metrics.FDCount),
				Value:       float64(metrics.FDCount),
				Threshold:   900.0,
				Timestamp:   time.Now(),
			})
		}
	}

	return alerts
}

// GetServiceMetrics returns metrics for a specific service
func (rm *ResourceMonitor) GetServiceMetrics(serviceName string) (*ServiceResourceMetrics, bool) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	metrics, exists := rm.serviceMetrics[serviceName]
	return metrics, exists
}

// GetAllMetrics returns metrics for all monitored services
func (rm *ResourceMonitor) GetAllMetrics() map[string]*ServiceResourceMetrics {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	// Return a copy to avoid race conditions
	result := make(map[string]*ServiceResourceMetrics)
	for name, metrics := range rm.serviceMetrics {
		metricsCopy := *metrics
		result[name] = &metricsCopy
	}

	return result
}

// RecordRestart records a service restart
func (rm *ResourceMonitor) RecordRestart(serviceName string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if metrics, exists := rm.serviceMetrics[serviceName]; exists {
		metrics.RestartCount++
		metrics.RestartHistory = append(metrics.RestartHistory, time.Now())
		if len(metrics.RestartHistory) > 10 {
			metrics.RestartHistory = metrics.RestartHistory[1:]
		}
	}
}

// Metrics returns monitoring metrics
func (rm *ResourceMonitor) Metrics() map[string]interface{} {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	return map[string]interface{}{
		"total_services":    rm.totalServices,
		"monitoring_errors": rm.monitoringErrors,
		"cgroup_version":    rm.cgroupVersion,
	}
}
