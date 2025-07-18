package converters

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf"
	"github.com/yairfalse/tapio/pkg/universal"
)

// EBPFConverter converts eBPF data types to universal format
type EBPFConverter struct {
	pidMapper      *PIDMapper
	sourceID       string
	version        string
	defaultQuality float64
}

// NewEBPFConverter creates a new eBPF data converter
func NewEBPFConverter(sourceID, version string) *EBPFConverter {
	return &EBPFConverter{
		pidMapper:      NewPIDMapper(),
		sourceID:       sourceID,
		version:        version,
		defaultQuality: 0.9, // eBPF data is generally high quality
	}
}

// ConvertProcessMemoryStats converts ProcessMemoryStats to UniversalMetric
func (c *EBPFConverter) ConvertProcessMemoryStats(stats *ebpf.ProcessMemoryStats) (*universal.UniversalMetric, error) {
	if stats == nil {
		return nil, fmt.Errorf("nil ProcessMemoryStats")
	}

	// Get metric from pool
	metric := universal.GetMetric()

	// Set core fields
	metric.ID = fmt.Sprintf("ebpf_memory_%d_%d", stats.PID, time.Now().UnixNano())
	metric.Timestamp = time.Now()

	// Map PID to target
	target, err := c.pidMapper.MapPIDToTarget(int32(stats.PID))
	if err != nil {
		// Fallback to process-only target
		metric.Target = universal.Target{
			Type: universal.TargetTypeProcess,
			Name: fmt.Sprintf("pid-%d", stats.PID),
			PID:  int32(stats.PID),
		}
		metric.FallbackUsed = true
		metric.ErrorContext = fmt.Sprintf("PID mapping failed: %v", err)
	} else {
		metric.Target = *target
	}

	// Set metric data
	metric.Name = "memory_usage_bytes"
	metric.Value = float64(stats.CurrentUsage)
	metric.Unit = "bytes"
	metric.Type = universal.MetricTypeGauge

	// Add labels
	metric.Labels["source"] = "ebpf"
	if stats.InContainer {
		metric.Labels["in_container"] = "true"
		if stats.ContainerPID != 0 {
			metric.Labels["container_pid"] = fmt.Sprintf("%d", stats.ContainerPID)
		}
	}

	// Set quality
	metric.Quality = universal.DataQuality{
		Confidence: c.calculateConfidence(stats),
		Source:     c.sourceID,
		Version:    c.version,
		Tags: map[string]string{
			"collector": "ebpf",
			"kernel":    "true",
		},
		Metadata: map[string]interface{}{
			"sample_time":     stats.LastUpdate,
			"total_allocated": stats.TotalAllocated,
			"total_freed":     stats.TotalFreed,
		},
	}

	return metric, nil
}

// ConvertMemoryGrowthToMetrics converts memory growth pattern to a series of metrics
func (c *EBPFConverter) ConvertMemoryGrowthToMetrics(stats *ebpf.ProcessMemoryStats) ([]*universal.UniversalMetric, error) {
	if stats == nil || len(stats.GrowthPattern) == 0 {
		return nil, fmt.Errorf("no growth pattern data")
	}

	metrics := make([]*universal.UniversalMetric, 0, len(stats.GrowthPattern))

	for _, point := range stats.GrowthPattern {
		metric := universal.GetMetric()

		// Set core fields
		metric.ID = fmt.Sprintf("ebpf_memory_growth_%d_%d", stats.PID, point.Timestamp.UnixNano())
		metric.Timestamp = point.Timestamp

		// Map PID to target
		target, err := c.pidMapper.MapPIDToTarget(int32(stats.PID))
		if err != nil {
			metric.Target = universal.Target{
				Type: universal.TargetTypeProcess,
				Name: fmt.Sprintf("pid-%d", stats.PID),
				PID:  int32(stats.PID),
			}
			metric.FallbackUsed = true
		} else {
			metric.Target = *target
		}

		// Set metric data
		metric.Name = "memory_usage_bytes"
		metric.Value = float64(point.Usage)
		metric.Unit = "bytes"
		metric.Type = universal.MetricTypeGauge

		// Add labels
		metric.Labels["source"] = "ebpf"
		metric.Labels["pattern"] = "growth"

		// Set quality
		metric.Quality = universal.DataQuality{
			Confidence: c.defaultQuality,
			Source:     c.sourceID,
			Version:    c.version,
			Tags: map[string]string{
				"collector": "ebpf",
				"series":    "growth_pattern",
			},
		}

		metrics = append(metrics, metric)
	}

	return metrics, nil
}

// ConvertAllocationRateToMetric converts allocation rate to metric
func (c *EBPFConverter) ConvertAllocationRateToMetric(stats *ebpf.ProcessMemoryStats) (*universal.UniversalMetric, error) {
	if stats == nil {
		return nil, fmt.Errorf("nil ProcessMemoryStats")
	}

	metric := universal.GetMetric()

	// Set core fields
	metric.ID = fmt.Sprintf("ebpf_alloc_rate_%d_%d", stats.PID, time.Now().UnixNano())
	metric.Timestamp = time.Now()

	// Map PID to target
	target, err := c.pidMapper.MapPIDToTarget(int32(stats.PID))
	if err != nil {
		metric.Target = universal.Target{
			Type: universal.TargetTypeProcess,
			Name: fmt.Sprintf("pid-%d", stats.PID),
			PID:  int32(stats.PID),
		}
		metric.FallbackUsed = true
	} else {
		metric.Target = *target
	}

	// Set metric data
	metric.Name = "memory_allocation_rate_bytes_per_second"
	metric.Value = stats.AllocationRate
	metric.Unit = "bytes/s"
	metric.Type = universal.MetricTypeGauge

	// Add labels
	metric.Labels["source"] = "ebpf"

	// Set quality
	metric.Quality = universal.DataQuality{
		Confidence: c.defaultQuality,
		Source:     c.sourceID,
		Version:    c.version,
		Tags: map[string]string{
			"collector": "ebpf",
			"metric":    "allocation_rate",
		},
		Metadata: map[string]interface{}{
			"total_allocated": stats.TotalAllocated,
			"total_freed":     stats.TotalFreed,
		},
	}

	return metric, nil
}

// ConvertOOMEvent converts an OOM event to UniversalEvent
func (c *EBPFConverter) ConvertOOMEvent(pid int32, timestamp time.Time, details map[string]interface{}) (*universal.UniversalEvent, error) {
	event := universal.GetEvent()

	// Set core fields
	event.ID = fmt.Sprintf("ebpf_oom_%d_%d", pid, timestamp.UnixNano())
	event.Timestamp = timestamp

	// Map PID to target
	target, err := c.pidMapper.MapPIDToTarget(pid)
	if err != nil {
		event.Target = universal.Target{
			Type: universal.TargetTypeProcess,
			Name: fmt.Sprintf("pid-%d", pid),
			PID:  pid,
		}
	} else {
		event.Target = *target
	}

	// Set event data
	event.Type = universal.EventTypeOOMKill
	event.Level = universal.EventLevelCritical
	event.Message = fmt.Sprintf("Process %d killed due to out of memory", pid)
	event.Details = details

	// Set quality
	event.Quality = universal.DataQuality{
		Confidence: 1.0, // OOM events are definitive
		Source:     c.sourceID,
		Version:    c.version,
		Tags: map[string]string{
			"collector": "ebpf",
			"event":     "oom_kill",
		},
	}

	return event, nil
}

// ConvertMemoryPressureEvent converts memory pressure detection to event
func (c *EBPFConverter) ConvertMemoryPressureEvent(stats *ebpf.ProcessMemoryStats, threshold float64) (*universal.UniversalEvent, error) {
	if stats == nil {
		return nil, fmt.Errorf("nil ProcessMemoryStats")
	}

	event := universal.GetEvent()

	// Set core fields
	event.ID = fmt.Sprintf("ebpf_memory_pressure_%d_%d", stats.PID, time.Now().UnixNano())
	event.Timestamp = time.Now()

	// Map PID to target
	target, err := c.pidMapper.MapPIDToTarget(int32(stats.PID))
	if err != nil {
		event.Target = universal.Target{
			Type: universal.TargetTypeProcess,
			Name: fmt.Sprintf("pid-%d", stats.PID),
			PID:  int32(stats.PID),
		}
	} else {
		event.Target = *target
	}

	// Calculate pressure level based on threshold
	// Use threshold as an approximation for limit
	var pressureRatio float64
	if threshold > 0 {
		pressureRatio = float64(stats.CurrentUsage) / (float64(stats.CurrentUsage) / threshold)
	} else {
		// Fallback: use allocation pattern to estimate pressure
		if stats.TotalAllocated > 0 {
			pressureRatio = float64(stats.CurrentUsage) / float64(stats.TotalAllocated)
		} else {
			pressureRatio = 0.5 // Default moderate pressure
		}
	}

	// Set event data
	event.Type = universal.EventTypeMemoryPressure
	event.Level = c.calculatePressureLevel(pressureRatio)
	event.Message = fmt.Sprintf("Memory pressure detected: %.2f%% threshold", pressureRatio*100)
	event.Details = map[string]interface{}{
		"current_usage":   stats.CurrentUsage,
		"total_allocated": stats.TotalAllocated,
		"total_freed":     stats.TotalFreed,
		"pressure_ratio":  pressureRatio,
		"allocation_rate": stats.AllocationRate,
		"threshold":       threshold,
	}

	// Set quality
	event.Quality = universal.DataQuality{
		Confidence: c.calculateConfidence(stats),
		Source:     c.sourceID,
		Version:    c.version,
		Tags: map[string]string{
			"collector": "ebpf",
			"event":     "memory_pressure",
		},
	}

	return event, nil
}

// calculateConfidence calculates confidence based on data quality indicators
func (c *EBPFConverter) calculateConfidence(stats *ebpf.ProcessMemoryStats) float64 {
	confidence := c.defaultQuality

	// Reduce confidence for various factors
	if stats.CurrentUsage == 0 {
		confidence *= 0.5 // Suspicious if no memory usage
	}

	if len(stats.GrowthPattern) < 3 {
		confidence *= 0.8 // Less data points means less confidence
	}

	if stats.TotalAllocated < stats.TotalFreed {
		confidence *= 0.7 // Data inconsistency
	}

	return confidence
}

// calculatePressureLevel determines event level based on pressure ratio
func (c *EBPFConverter) calculatePressureLevel(ratio float64) universal.EventLevel {
	switch {
	case ratio >= 0.95:
		return universal.EventLevelCritical
	case ratio >= 0.85:
		return universal.EventLevelError
	case ratio >= 0.75:
		return universal.EventLevelWarning
	default:
		return universal.EventLevelInfo
	}
}

// PIDMapper maps PIDs to container/pod information
type PIDMapper struct {
	// In a real implementation, this would cache PID to container/pod mappings
	cache map[int32]*universal.Target
}

// NewPIDMapper creates a new PID mapper
func NewPIDMapper() *PIDMapper {
	return &PIDMapper{
		cache: make(map[int32]*universal.Target),
	}
}

// MapPIDToTarget maps a PID to a universal target
func (m *PIDMapper) MapPIDToTarget(pid int32) (*universal.Target, error) {
	// Check cache first
	if target, ok := m.cache[pid]; ok {
		return target, nil
	}

	// In a real implementation, this would:
	// 1. Read /proc/{pid}/cgroup to get container ID
	// 2. Query container runtime for container details
	// 3. Map container to pod/namespace
	// 4. Cache the result

	// For now, return a simple process target
	target := &universal.Target{
		Type: universal.TargetTypeProcess,
		Name: fmt.Sprintf("process-%d", pid),
		PID:  pid,
	}

	m.cache[pid] = target
	return target, nil
}

// UpdateMapping updates the PID to target mapping
func (m *PIDMapper) UpdateMapping(pid int32, target *universal.Target) {
	if target != nil {
		m.cache[pid] = target
	}
}

// ClearCache clears the PID mapping cache
func (m *PIDMapper) ClearCache() {
	m.cache = make(map[int32]*universal.Target)
}
