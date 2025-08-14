package kernel

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// GracefulDegradation manages fallback strategies when eBPF fails
type GracefulDegradation struct {
	config           *Config
	coreCompat       *CoreCompatibility
	backpressure     *BackpressureManager
	
	// State management
	ebpfEnabled      bool
	fallbackActive   map[string]bool
	fallbackMutex    sync.RWMutex
	
	// Fallback implementations
	procfsMonitor    *ProcfsMonitor
	sysfsMonitor     *SysfsMonitor
	netlinkMonitor   *NetlinkMonitor
	
	// Health tracking
	lastEBPFError    time.Time
	ebpfFailureCount int
	healthChecker    *HealthChecker
	
	// Metrics
	fallbackGauge    metric.Int64Gauge
	errorCounter     metric.Int64Counter
	methodGauge      metric.Int64Gauge
	
	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// ProcfsMonitor monitors system state through /proc filesystem
type ProcfsMonitor struct {
	procPaths map[string]string
	lastScan  time.Time
	mu        sync.RWMutex
}

// SysfsMonitor monitors system state through /sys filesystem
type SysfsMonitor struct {
	sysPaths map[string]string
	lastScan time.Time
	mu       sync.RWMutex
}

// NetlinkMonitor monitors network events through netlink sockets
type NetlinkMonitor struct {
	socket   int
	lastScan time.Time
	mu       sync.RWMutex
}

// HealthChecker monitors collector health and triggers failovers
type HealthChecker struct {
	config          *HealthConfig
	lastCheck       time.Time
	consecutiveFails int
	mu              sync.RWMutex
}

// FallbackStrategy represents a fallback monitoring strategy
type FallbackStrategy struct {
	Name        string
	Type        string    // "procfs", "sysfs", "netlink", "polling"
	Path        string
	Interval    time.Duration
	Parser      func(string) (interface{}, error)
	IsHealthy   bool
	LastError   error
	LastSuccess time.Time
}

// NewGracefulDegradation creates a new graceful degradation manager
func NewGracefulDegradation(config *Config, coreCompat *CoreCompatibility, backpressure *BackpressureManager) *GracefulDegradation {
	ctx, cancel := context.WithCancel(context.Background())
	meter := otel.Meter("tapio/collectors/kernel")
	
	gd := &GracefulDegradation{
		config:         config,
		coreCompat:     coreCompat,
		backpressure:   backpressure,
		ebpfEnabled:    true,
		fallbackActive: make(map[string]bool),
		ctx:            ctx,
		cancel:         cancel,
		procfsMonitor:  NewProcfsMonitor(),
		sysfsMonitor:   NewSysfsMonitor(),
		netlinkMonitor: NewNetlinkMonitor(),
		healthChecker:  NewHealthChecker(&config.Health),
	}
	
	// Initialize metrics
	var err error
	gd.fallbackGauge, err = meter.Int64Gauge(
		"kernel_collector_fallback_active",
		metric.WithDescription("Number of active fallback methods"),
	)
	if err != nil {
		// Log error but continue
	}
	
	gd.errorCounter, err = meter.Int64Counter(
		"kernel_collector_errors_total",
		metric.WithDescription("Total number of collector errors by type"),
	)
	if err != nil {
		// Log error but continue
	}
	
	gd.methodGauge, err = meter.Int64Gauge(
		"kernel_collector_method",
		metric.WithDescription("Current collection method (0=ebpf, 1=fallback)"),
	)
	if err != nil {
		// Log error but continue
	}
	
	return gd
}

// NewProcfsMonitor creates a new procfs monitor
func NewProcfsMonitor() *ProcfsMonitor {
	return &ProcfsMonitor{
		procPaths: map[string]string{
			"meminfo":   "/proc/meminfo",
			"stat":      "/proc/stat",
			"loadavg":   "/proc/loadavg",
			"diskstats": "/proc/diskstats",
			"net_dev":   "/proc/net/dev",
			"net_tcp":   "/proc/net/tcp",
			"net_udp":   "/proc/net/udp",
		},
	}
}

// NewSysfsMonitor creates a new sysfs monitor
func NewSysfsMonitor() *SysfsMonitor {
	return &SysfsMonitor{
		sysPaths: map[string]string{
			"cgroup_memory": "/sys/fs/cgroup/memory",
			"cgroup_cpu":    "/sys/fs/cgroup/cpu",
			"block_stats":   "/sys/block",
			"net_stats":     "/sys/class/net",
		},
	}
}

// NewNetlinkMonitor creates a new netlink monitor
func NewNetlinkMonitor() *NetlinkMonitor {
	return &NetlinkMonitor{}
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(config *HealthConfig) *HealthChecker {
	return &HealthChecker{
		config: config,
	}
}

// StartMonitoring starts the degradation monitoring
func (gd *GracefulDegradation) StartMonitoring() error {
	// Start health checking
	gd.wg.Add(1)
	go func() {
		defer gd.wg.Done()
		gd.healthCheckLoop()
	}()
	
	// Test eBPF availability
	if err := gd.testEBPFAvailability(); err != nil {
		gd.handleEBPFFailure("initial_test", err)
	}
	
	return nil
}

// testEBPFAvailability tests if eBPF programs can be loaded
func (gd *GracefulDegradation) testEBPFAvailability() error {
	// Check if we have the necessary kernel features
	if !gd.coreCompat.IsCompatible("ring_buffer") && !gd.coreCompat.IsCompatible("perf_buffer") {
		return fmt.Errorf("no suitable buffer mechanism available")
	}
	
	// Try to load a simple test program
	// This would involve actual eBPF program loading in a real implementation
	// For now, simulate based on kernel compatibility
	if gd.coreCompat.GetKernelVersion().isAtLeast(4, 1, 0) {
		return nil // eBPF supported
	}
	
	return fmt.Errorf("kernel version %s does not support eBPF", gd.coreCompat.GetKernelVersion().String())
}

// handleEBPFFailure handles eBPF failure and activates fallbacks
func (gd *GracefulDegradation) handleEBPFFailure(context string, err error) {
	gd.fallbackMutex.Lock()
	defer gd.fallbackMutex.Unlock()
	
	gd.lastEBPFError = time.Now()
	gd.ebpfFailureCount++
	gd.ebpfEnabled = false
	
	// Update metrics
	if gd.errorCounter != nil {
		gd.errorCounter.Add(gd.ctx, 1,
			metric.WithAttributes(attribute.String("context", context), attribute.String("type", "ebpf_failure")))
	}
	
	if gd.methodGauge != nil {
		gd.methodGauge.Record(gd.ctx, 1) // 1 = fallback mode
	}
	
	// Activate appropriate fallbacks
	gd.activateProcessFallback()
	gd.activateNetworkFallback()
	gd.activateMemoryFallback()
}

// activateProcessFallback activates process monitoring fallback
func (gd *GracefulDegradation) activateProcessFallback() {
	gd.fallbackActive["process"] = true
	
	gd.wg.Add(1)
	go func() {
		defer gd.wg.Done()
		gd.runProcessFallback()
	}()
}

// activateNetworkFallback activates network monitoring fallback
func (gd *GracefulDegradation) activateNetworkFallback() {
	gd.fallbackActive["network"] = true
	
	gd.wg.Add(1)
	go func() {
		defer gd.wg.Done()
		gd.runNetworkFallback()
	}()
}

// activateMemoryFallback activates memory monitoring fallback
func (gd *GracefulDegradation) activateMemoryFallback() {
	gd.fallbackActive["memory"] = true
	
	gd.wg.Add(1)
	go func() {
		defer gd.wg.Done()
		gd.runMemoryFallback()
	}()
}

// runProcessFallback runs process monitoring via procfs
func (gd *GracefulDegradation) runProcessFallback() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			if err := gd.collectProcessInfo(); err != nil {
				if gd.errorCounter != nil {
					gd.errorCounter.Add(gd.ctx, 1,
						metric.WithAttributes(attribute.String("type", "process_fallback")))
				}
			}
		case <-gd.ctx.Done():
			return
		}
	}
}

// runNetworkFallback runs network monitoring via procfs
func (gd *GracefulDegradation) runNetworkFallback() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			if err := gd.collectNetworkInfo(); err != nil {
				if gd.errorCounter != nil {
					gd.errorCounter.Add(gd.ctx, 1,
						metric.WithAttributes(attribute.String("type", "network_fallback")))
				}
			}
		case <-gd.ctx.Done():
			return
		}
	}
}

// runMemoryFallback runs memory monitoring via procfs/sysfs
func (gd *GracefulDegradation) runMemoryFallback() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			if err := gd.collectMemoryInfo(); err != nil {
				if gd.errorCounter != nil {
					gd.errorCounter.Add(gd.ctx, 1,
						metric.WithAttributes(attribute.String("type", "memory_fallback")))
				}
			}
		case <-gd.ctx.Done():
			return
		}
	}
}

// collectProcessInfo collects process information from procfs
func (gd *GracefulDegradation) collectProcessInfo() error {
	// Read /proc/*/stat for process information
	procDirs, err := filepath.Glob("/proc/[0-9]*")
	if err != nil {
		return fmt.Errorf("failed to glob /proc directories: %w", err)
	}
	
	processCount := 0
	for _, procDir := range procDirs {
		statPath := filepath.Join(procDir, "stat")
		if _, err := os.Stat(statPath); err != nil {
			continue
		}
		
		// Read basic process info
		processCount++
		
		// In a real implementation, we would:
		// 1. Parse /proc/PID/stat for process details
		// 2. Check /proc/PID/cgroup for container association
		// 3. Generate events similar to eBPF programs
		// 4. Apply backpressure if needed
		
		if processCount > 1000 { // Limit to prevent overload
			break
		}
	}
	
	return nil
}

// collectNetworkInfo collects network information from procfs
func (gd *GracefulDegradation) collectNetworkInfo() error {
	// Read /proc/net/tcp and /proc/net/udp for connection info
	connections := []string{"tcp", "tcp6", "udp", "udp6"}
	
	for _, conn := range connections {
		path := fmt.Sprintf("/proc/net/%s", conn)
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		
		scanner := bufio.NewScanner(file)
		lineCount := 0
		for scanner.Scan() {
			line := scanner.Text()
			if lineCount == 0 { // Skip header
				lineCount++
				continue
			}
			
			// Parse connection information
			gd.parseConnectionLine(line, conn)
			lineCount++
			
			if lineCount > 10000 { // Limit processing
				break
			}
		}
		file.Close()
	}
	
	return nil
}

// parseConnectionLine parses a connection line from /proc/net/*
func (gd *GracefulDegradation) parseConnectionLine(line, connType string) {
	fields := strings.Fields(line)
	if len(fields) < 10 {
		return
	}
	
	// Parse local and remote addresses
	localAddr := fields[1]
	remoteAddr := fields[2]
	state := fields[3]
	
	// In a real implementation, we would:
	// 1. Parse addresses and ports
	// 2. Map to container processes
	// 3. Generate network events
	// 4. Apply sampling based on backpressure
	
	_ = localAddr
	_ = remoteAddr
	_ = state
}

// collectMemoryInfo collects memory information from various sources
func (gd *GracefulDegradation) collectMemoryInfo() error {
	// Read /proc/meminfo for system memory
	meminfo, err := os.Open("/proc/meminfo")
	if err != nil {
		return fmt.Errorf("failed to open /proc/meminfo: %w", err)
	}
	defer meminfo.Close()
	
	scanner := bufio.NewScanner(meminfo)
	memStats := make(map[string]uint64)
	
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			key := strings.TrimSuffix(parts[0], ":")
			if val, err := strconv.ParseUint(parts[1], 10, 64); err == nil {
				memStats[key] = val * 1024 // Convert from kB to bytes
			}
		}
	}
	
	// Check cgroup memory limits for containers
	gd.checkCgroupMemory()
	
	return nil
}

// checkCgroupMemory checks cgroup memory usage for containers
func (gd *GracefulDegradation) checkCgroupMemory() error {
	cgroupPath := "/sys/fs/cgroup/memory"
	if _, err := os.Stat(cgroupPath); err != nil {
		return nil // cgroup v1 not available
	}
	
	// Walk through cgroup directories to find container memory usage
	return filepath.Walk(cgroupPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}
		
		if info.IsDir() && strings.Contains(path, "docker") {
			usagePath := filepath.Join(path, "memory.usage_in_bytes")
			if _, err := os.Stat(usagePath); err == nil {
				// Read memory usage for this container
				// In real implementation, we would correlate with processes
			}
		}
		
		return nil
	})
}

// healthCheckLoop runs the health checking loop
func (gd *GracefulDegradation) healthCheckLoop() {
	if !gd.config.Health.Enabled {
		return
	}
	
	ticker := time.NewTicker(gd.config.Health.Interval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			gd.performHealthCheck()
		case <-gd.ctx.Done():
			return
		}
	}
}

// performHealthCheck performs a health check and handles failures
func (gd *GracefulDegradation) performHealthCheck() {
	gd.healthChecker.mu.Lock()
	defer gd.healthChecker.mu.Unlock()
	
	healthy := true
	
	// Check if eBPF is working (if enabled)
	if gd.ebpfEnabled {
		if err := gd.testEBPFHealth(); err != nil {
			healthy = false
			gd.healthChecker.consecutiveFails++
		} else {
			gd.healthChecker.consecutiveFails = 0
		}
	}
	
	// Check fallback health
	if !healthy || !gd.ebpfEnabled {
		healthy = gd.checkFallbackHealth()
	}
	
	// Handle consecutive failures
	if gd.healthChecker.consecutiveFails >= gd.config.Health.MaxFailures {
		if gd.config.Health.RestartOnFailure {
			gd.restartCollector()
		} else if gd.ebpfEnabled {
			gd.handleEBPFFailure("health_check", fmt.Errorf("health check failed"))
		}
	}
	
	gd.healthChecker.lastCheck = time.Now()
}

// testEBPFHealth tests if eBPF programs are still working
func (gd *GracefulDegradation) testEBPFHealth() error {
	// This would test if eBPF programs are still loaded and receiving events
	// For now, simulate based on time since last error
	if time.Since(gd.lastEBPFError) < 30*time.Second {
		return fmt.Errorf("recent eBPF error")
	}
	return nil
}

// checkFallbackHealth checks if fallback methods are working
func (gd *GracefulDegradation) checkFallbackHealth() bool {
	gd.fallbackMutex.RLock()
	defer gd.fallbackMutex.RUnlock()
	
	// Check if at least one fallback is active and working
	for fallback, active := range gd.fallbackActive {
		if active {
			// Check if fallback is generating data
			switch fallback {
			case "process":
				if gd.procfsMonitor.isHealthy() {
					return true
				}
			case "network":
				if gd.netlinkMonitor.isHealthy() {
					return true
				}
			case "memory":
				if gd.sysfsMonitor.isHealthy() {
					return true
				}
			}
		}
	}
	
	return false
}

// isHealthy checks if procfs monitor is healthy
func (pm *ProcfsMonitor) isHealthy() bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	
	// Check if we've successfully read data recently
	return time.Since(pm.lastScan) < 2*time.Minute
}

// isHealthy checks if netlink monitor is healthy
func (nm *NetlinkMonitor) isHealthy() bool {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	
	return time.Since(nm.lastScan) < 2*time.Minute
}

// isHealthy checks if sysfs monitor is healthy
func (sm *SysfsMonitor) isHealthy() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	
	return time.Since(sm.lastScan) < 2*time.Minute
}

// restartCollector attempts to restart the collector
func (gd *GracefulDegradation) restartCollector() {
	// In a real implementation, this would:
	// 1. Stop all current monitoring
	// 2. Reload eBPF programs
	// 3. Reset failure counters
	// 4. Restart health checking
	
	gd.ebpfFailureCount = 0
	gd.healthChecker.consecutiveFails = 0
}

// Stop stops graceful degradation monitoring
func (gd *GracefulDegradation) Stop() {
	gd.cancel()
	gd.wg.Wait()
}

// IsEBPFEnabled returns whether eBPF is currently enabled
func (gd *GracefulDegradation) IsEBPFEnabled() bool {
	gd.fallbackMutex.RLock()
	defer gd.fallbackMutex.RUnlock()
	return gd.ebpfEnabled
}

// GetActiveFallbacks returns list of active fallback methods
func (gd *GracefulDegradation) GetActiveFallbacks() []string {
	gd.fallbackMutex.RLock()
	defer gd.fallbackMutex.RUnlock()
	
	var active []string
	for fallback, isActive := range gd.fallbackActive {
		if isActive {
			active = append(active, fallback)
		}
	}
	
	return active
}

// GetStats returns degradation statistics
func (gd *GracefulDegradation) GetStats() DegradationStats {
	gd.fallbackMutex.RLock()
	defer gd.fallbackMutex.RUnlock()
	
	return DegradationStats{
		EBPFEnabled:       gd.ebpfEnabled,
		ActiveFallbacks:   gd.GetActiveFallbacks(),
		LastEBPFError:     gd.lastEBPFError,
		EBPFFailureCount:  gd.ebpfFailureCount,
		HealthCheckPasses: gd.healthChecker.consecutiveFails == 0,
	}
}

// DegradationStats represents degradation statistics
type DegradationStats struct {
	EBPFEnabled       bool      `json:"ebpf_enabled"`
	ActiveFallbacks   []string  `json:"active_fallbacks"`
	LastEBPFError     time.Time `json:"last_ebpf_error"`
	EBPFFailureCount  int       `json:"ebpf_failure_count"`
	HealthCheckPasses bool      `json:"health_check_passes"`
}