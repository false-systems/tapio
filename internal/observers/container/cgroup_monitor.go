//go:build linux

package container

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// ErrCgroupNotFound indicates the cgroup was deleted (container terminated)
var ErrCgroupNotFound = errors.New("cgroup not found")

// CgroupInfo contains container resource limits and usage from cgroup v2
type CgroupInfo struct {
	ContainerID string
	CgroupPath  string

	// Memory metrics
	MemoryCurrentBytes uint64
	MemoryLimitBytes   uint64
	MemoryUsagePct     float64

	// Memory pressure (PSI)
	MemoryPressureSomeAvg10 float64
	MemoryPressureSomeAvg60 float64

	// CPU metrics
	CPUUsageUsec      uint64
	CPUThrottledUsec  uint64
	CPUThrottledCount uint64
	CPUThrottledPct   float64

	// Timestamp when fetched
	FetchedAt time.Time
}

// cachedCgroupInfo wraps CgroupInfo with cache metadata
type cachedCgroupInfo struct {
	info      CgroupInfo
	fetchedAt time.Time
}

// CgroupMonitor reads container cgroup metrics with LRU caching
type CgroupMonitor struct {
	basePath string
	cacheTTL time.Duration
	cache    *lru.Cache[string, cachedCgroupInfo]
	mu       sync.RWMutex

	// Cgroup ID → Container ID cache (Issue #566)
	// Survives cgroup deletion - populated on successful lookups
	cgroupIDCache *lru.Cache[uint64, string]

	// OTEL metrics
	cacheHits   metric.Int64Counter
	cacheMisses metric.Int64Counter
	readErrors  metric.Int64Counter
}

// CgroupMonitorConfig holds configuration for CgroupMonitor
type CgroupMonitorConfig struct {
	BasePath  string        // cgroup v2 base path (default: /sys/fs/cgroup)
	CacheSize int           // LRU cache size (default: 1000)
	CacheTTL  time.Duration // Cache entry TTL (default: 30s)
}

// NewCgroupMonitor creates a new cgroup monitor with LRU cache
func NewCgroupMonitor(cfg CgroupMonitorConfig, meter metric.Meter) (*CgroupMonitor, error) {
	// Apply defaults
	if cfg.BasePath == "" {
		cfg.BasePath = "/sys/fs/cgroup"
	}
	if cfg.CacheSize <= 0 {
		cfg.CacheSize = 1000
	}
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = 30 * time.Second
	}

	cache, err := lru.New[string, cachedCgroupInfo](cfg.CacheSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create LRU cache: %w", err)
	}

	// Cgroup ID cache for surviving cgroup deletion (Issue #566)
	cgroupIDCache, err := lru.New[uint64, string](cfg.CacheSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create cgroup ID cache: %w", err)
	}

	m := &CgroupMonitor{
		basePath:      cfg.BasePath,
		cacheTTL:      cfg.CacheTTL,
		cache:         cache,
		cgroupIDCache: cgroupIDCache,
	}

	// Register OTEL metrics if meter provided
	if meter != nil {
		var err error
		m.cacheHits, err = meter.Int64Counter("container_cgroup_cache_hits_total",
			metric.WithDescription("Number of cgroup cache hits"))
		if err != nil {
			return nil, fmt.Errorf("failed to create cacheHits counter: %w", err)
		}
		m.cacheMisses, err = meter.Int64Counter("container_cgroup_cache_misses_total",
			metric.WithDescription("Number of cgroup cache misses"))
		if err != nil {
			return nil, fmt.Errorf("failed to create cacheMisses counter: %w", err)
		}
		m.readErrors, err = meter.Int64Counter("container_cgroup_read_errors_total",
			metric.WithDescription("Number of cgroup read errors"))
		if err != nil {
			return nil, fmt.Errorf("failed to create readErrors counter: %w", err)
		}
	}

	return m, nil
}

// GetInfo retrieves cgroup info for a container, using cache when available
func (m *CgroupMonitor) GetInfo(ctx context.Context, containerID string) (CgroupInfo, error) {
	cgroupPath := m.findCgroupPath(containerID)
	if cgroupPath == "" {
		return CgroupInfo{}, ErrCgroupNotFound
	}

	// Check cache first (hot path)
	m.mu.RLock()
	if cached, ok := m.cache.Get(cgroupPath); ok {
		if time.Since(cached.fetchedAt) < m.cacheTTL {
			m.mu.RUnlock()
			if m.cacheHits != nil {
				m.cacheHits.Add(ctx, 1)
			}
			return cached.info, nil
		}
	}
	m.mu.RUnlock()

	// Cache miss or expired - read from cgroupfs
	if m.cacheMisses != nil {
		m.cacheMisses.Add(ctx, 1)
	}

	info, err := m.readCgroupInfo(cgroupPath, containerID)
	if err != nil {
		if m.readErrors != nil {
			m.readErrors.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", errorType(err)),
			))
		}
		return CgroupInfo{}, err
	}

	// Update cache
	m.mu.Lock()
	m.cache.Add(cgroupPath, cachedCgroupInfo{
		info:      info,
		fetchedAt: time.Now(),
	})
	m.mu.Unlock()

	return info, nil
}

// findCgroupPath locates the cgroup path for a container ID
func (m *CgroupMonitor) findCgroupPath(containerID string) string {
	if containerID == "" {
		return ""
	}

	// Common cgroup v2 paths for containers
	patterns := []string{
		// Docker
		filepath.Join(m.basePath, "system.slice", "docker-"+containerID+".scope"),
		// containerd (K8s)
		filepath.Join(m.basePath, "system.slice", "containerd-"+containerID+".scope"),
		// cri-containerd
		filepath.Join(m.basePath, "system.slice", "cri-containerd-"+containerID+".scope"),
		// K8s kubepods
		filepath.Join(m.basePath, "kubepods.slice", "*", "*-"+containerID+".scope"),
	}

	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err == nil && len(matches) > 0 {
			return matches[0]
		}
	}

	return ""
}

// readCgroupInfo reads metrics from cgroupfs
func (m *CgroupMonitor) readCgroupInfo(cgroupPath, containerID string) (CgroupInfo, error) {
	info := CgroupInfo{
		ContainerID: containerID,
		CgroupPath:  cgroupPath,
		FetchedAt:   time.Now(),
	}

	// Read memory.current
	memCurrent, err := readUint64File(filepath.Join(cgroupPath, "memory.current"))
	if err != nil {
		if os.IsNotExist(err) {
			return info, ErrCgroupNotFound
		}
		return info, fmt.Errorf("failed to read memory.current: %w", err)
	}
	info.MemoryCurrentBytes = memCurrent

	// Read memory.max (might be "max" for unlimited)
	memMax, err := readUint64OrMax(filepath.Join(cgroupPath, "memory.max"))
	if err == nil {
		info.MemoryLimitBytes = memMax
		if memMax > 0 && memMax != ^uint64(0) {
			info.MemoryUsagePct = (float64(memCurrent) / float64(memMax)) * 100
		}
	}

	// Read memory.pressure (PSI)
	if psi, err := readPSIFile(filepath.Join(cgroupPath, "memory.pressure")); err == nil {
		info.MemoryPressureSomeAvg10 = psi.SomeAvg10
		info.MemoryPressureSomeAvg60 = psi.SomeAvg60
	}

	// Read cpu.stat
	if cpuStat, err := readCPUStat(filepath.Join(cgroupPath, "cpu.stat")); err == nil {
		info.CPUUsageUsec = cpuStat.UsageUsec
		info.CPUThrottledUsec = cpuStat.ThrottledUsec
		info.CPUThrottledCount = cpuStat.NrThrottled
		if cpuStat.UsageUsec > 0 {
			info.CPUThrottledPct = (float64(cpuStat.ThrottledUsec) / float64(cpuStat.UsageUsec)) * 100
		}
	}

	return info, nil
}

// PSIData holds Pressure Stall Information
type PSIData struct {
	SomeAvg10  float64
	SomeAvg60  float64
	SomeAvg300 float64
	SomeTotal  uint64
	FullAvg10  float64
	FullAvg60  float64
	FullAvg300 float64
	FullTotal  uint64
}

// CPUStatData holds cpu.stat contents
type CPUStatData struct {
	UsageUsec     uint64
	UserUsec      uint64
	SystemUsec    uint64
	NrPeriods     uint64
	NrThrottled   uint64
	ThrottledUsec uint64
}

// readUint64File reads a single uint64 value from a file
func readUint64File(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
}

// readUint64OrMax reads uint64, treating "max" as max uint64
func readUint64OrMax(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(data))
	if s == "max" {
		return ^uint64(0), nil // Max uint64
	}
	return strconv.ParseUint(s, 10, 64)
}

// readPSIFile parses PSI format:
// some avg10=0.00 avg60=0.00 avg300=0.00 total=0
// full avg10=0.00 avg60=0.00 avg300=0.00 total=0
func readPSIFile(path string) (PSIData, error) {
	file, err := os.Open(path)
	if err != nil {
		return PSIData{}, err
	}
	defer file.Close() //nolint:errcheck // read-only file, close errors non-actionable

	var psi PSIData
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "some ") {
			//nolint:errcheck // PSI format is kernel-stable, partial parse acceptable
			fmt.Sscanf(line, "some avg10=%f avg60=%f avg300=%f total=%d",
				&psi.SomeAvg10, &psi.SomeAvg60, &psi.SomeAvg300, &psi.SomeTotal)
		} else if strings.HasPrefix(line, "full ") {
			//nolint:errcheck // PSI format is kernel-stable, partial parse acceptable
			fmt.Sscanf(line, "full avg10=%f avg60=%f avg300=%f total=%d",
				&psi.FullAvg10, &psi.FullAvg60, &psi.FullAvg300, &psi.FullTotal)
		}
	}

	return psi, scanner.Err()
}

// readCPUStat parses cpu.stat format:
// usage_usec 12345
// nr_throttled 5
// throttled_usec 1000
func readCPUStat(path string) (CPUStatData, error) {
	file, err := os.Open(path)
	if err != nil {
		return CPUStatData{}, err
	}
	defer file.Close() //nolint:errcheck // read-only file, close errors non-actionable

	var stat CPUStatData
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			continue
		}

		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}

		switch fields[0] {
		case "usage_usec":
			stat.UsageUsec = value
		case "user_usec":
			stat.UserUsec = value
		case "system_usec":
			stat.SystemUsec = value
		case "nr_periods":
			stat.NrPeriods = value
		case "nr_throttled":
			stat.NrThrottled = value
		case "throttled_usec":
			stat.ThrottledUsec = value
		}
	}

	return stat, scanner.Err()
}

// errorType returns a short error type for metrics
func errorType(err error) string {
	if os.IsNotExist(err) {
		return "not_found"
	}
	if os.IsPermission(err) {
		return "permission"
	}
	return "other"
}

// CacheCgroupID stores a cgroup ID → container ID mapping (Issue #566)
// Called when we successfully resolve a container ID to cache for later
func (m *CgroupMonitor) CacheCgroupID(cgroupID uint64, containerID string) {
	if cgroupID == 0 || containerID == "" {
		return
	}
	m.mu.Lock()
	m.cgroupIDCache.Add(cgroupID, containerID)
	m.mu.Unlock()
}

// GetContainerIDByCgroupID looks up container ID from cached cgroup ID (Issue #566)
// Returns container ID and true if found, empty string and false otherwise
// Use this as fallback when cgroup path resolution fails (cgroup deleted)
func (m *CgroupMonitor) GetContainerIDByCgroupID(cgroupID uint64) (string, bool) {
	if cgroupID == 0 {
		return "", false
	}
	m.mu.RLock()
	containerID, ok := m.cgroupIDCache.Get(cgroupID)
	m.mu.RUnlock()
	return containerID, ok
}
