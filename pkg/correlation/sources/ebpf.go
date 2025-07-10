package sources

import (
	"context"
	"fmt"
	"time"

	"github.com/falseyair/tapio/pkg/correlation"
	"github.com/falseyair/tapio/pkg/ebpf"
)

// EBPFDataSource implements DataSource interface for eBPF monitoring data
type EBPFDataSource struct {
	monitor ebpf.Monitor
}

// NewEBPFDataSource creates a new eBPF data source
func NewEBPFDataSource(monitor ebpf.Monitor) *EBPFDataSource {
	return &EBPFDataSource{
		monitor: monitor,
	}
}

// GetType returns the source type
func (e *EBPFDataSource) GetType() correlation.SourceType {
	return correlation.SourceEBPF
}

// IsAvailable checks if eBPF monitoring is available
func (e *EBPFDataSource) IsAvailable() bool {
	if e.monitor == nil {
		return false
	}
	return e.monitor.IsAvailable()
}

// GetData retrieves data of the specified type
func (e *EBPFDataSource) GetData(ctx context.Context, dataType string, params map[string]interface{}) (interface{}, error) {
	switch dataType {
	case "memory_stats":
		return e.getMemoryStats(ctx, params)
	case "ebpf_data":
		return e.getEBPFData(ctx, params)
	case "process_stats":
		return e.getProcessStats(ctx, params)
	case "memory_events":
		return e.getMemoryEvents(ctx, params)
	case "cpu_events":
		return e.getCPUEvents(ctx, params)
	case "io_events":
		return e.getIOEvents(ctx, params)
	default:
		return nil, fmt.Errorf("unsupported data type: %s", dataType)
	}
}

// getEBPFData retrieves comprehensive eBPF monitoring data
func (e *EBPFDataSource) getEBPFData(ctx context.Context, params map[string]interface{}) (*correlation.EBPFData, error) {
	// Get memory stats from monitor
	stats, err := e.monitor.GetMemoryStats()
	if err != nil {
		return nil, fmt.Errorf("failed to get memory stats: %w", err)
	}

	// Convert to correlation format
	processStats := make(map[uint32]*correlation.ProcessMemoryStats)
	for _, stat := range stats {
		processStats[stat.PID] = &correlation.ProcessMemoryStats{
			PID:            stat.PID,
			Command:        stat.Command,
			TotalAllocated: stat.TotalAllocated,
			TotalFreed:     stat.TotalFreed,
			CurrentUsage:   stat.CurrentUsage,
			AllocationRate: stat.AllocationRate,
			LastUpdate:     stat.LastUpdate,
			InContainer:    stat.InContainer,
			ContainerPID:   stat.ContainerPID,
			GrowthPattern:  convertGrowthPattern(stat.GrowthPattern),
			// IOBytesWritten and IOBytesRead not available in ebpf.ProcessMemoryStats
		}
	}

	// Note: The current eBPF interface doesn't provide events or system metrics
	// We'll create empty/default values for now
	memoryEvents := []correlation.MemoryEvent{}
	cpuEvents := []correlation.CPUEvent{}
	ioEvents := []correlation.IOEvent{}

	return &correlation.EBPFData{
		ProcessStats: processStats,
		SystemMetrics: correlation.SystemMetrics{
			// System metrics not available from current eBPF interface
			Timestamp: time.Now(),
		},
		MemoryEvents: memoryEvents,
		CPUEvents:    cpuEvents,
		IOEvents:     ioEvents,
		Timestamp:    time.Now(),
	}, nil
}

// getMemoryStats retrieves memory statistics
func (e *EBPFDataSource) getMemoryStats(ctx context.Context, params map[string]interface{}) (map[uint32]*correlation.ProcessMemoryStats, error) {
	stats, err := e.monitor.GetMemoryStats()
	if err != nil {
		return nil, fmt.Errorf("failed to get memory stats: %w", err)
	}

	processStats := make(map[uint32]*correlation.ProcessMemoryStats)
	for _, stat := range stats {
		// Filter by PID if specified
		if targetPID, ok := params["pid"].(uint32); ok && stat.PID != targetPID {
			continue
		}

		// Filter by container if specified
		if inContainer, ok := params["in_container"].(bool); ok && stat.InContainer != inContainer {
			continue
		}

		processStats[stat.PID] = &correlation.ProcessMemoryStats{
			PID:            stat.PID,
			Command:        stat.Command,
			TotalAllocated: stat.TotalAllocated,
			TotalFreed:     stat.TotalFreed,
			CurrentUsage:   stat.CurrentUsage,
			AllocationRate: stat.AllocationRate,
			LastUpdate:     stat.LastUpdate,
			InContainer:    stat.InContainer,
			ContainerPID:   stat.ContainerPID,
			GrowthPattern:  convertGrowthPattern(stat.GrowthPattern),
			// IOBytesWritten and IOBytesRead not available in ebpf.ProcessMemoryStats
		}
	}

	return processStats, nil
}

// getProcessStats retrieves process statistics
func (e *EBPFDataSource) getProcessStats(ctx context.Context, params map[string]interface{}) (map[uint32]*correlation.ProcessMemoryStats, error) {
	// This is an alias for getMemoryStats as they return the same data
	return e.getMemoryStats(ctx, params)
}

// getMemoryEvents retrieves recent memory events
func (e *EBPFDataSource) getMemoryEvents(ctx context.Context, params map[string]interface{}) ([]correlation.MemoryEvent, error) {
	limit := 100
	if l, ok := params["limit"].(int); ok {
		limit = l
	}

	return e.getRecentMemoryEvents(limit), nil
}

// getCPUEvents retrieves recent CPU events
func (e *EBPFDataSource) getCPUEvents(ctx context.Context, params map[string]interface{}) ([]correlation.CPUEvent, error) {
	limit := 100
	if l, ok := params["limit"].(int); ok {
		limit = l
	}

	return e.getRecentCPUEvents(limit), nil
}

// getIOEvents retrieves recent IO events
func (e *EBPFDataSource) getIOEvents(ctx context.Context, params map[string]interface{}) ([]correlation.IOEvent, error) {
	limit := 100
	if l, ok := params["limit"].(int); ok {
		limit = l
	}

	return e.getRecentIOEvents(limit), nil
}

// getRecentMemoryEvents retrieves recent memory events from the monitor
func (e *EBPFDataSource) getRecentMemoryEvents(limit int) []correlation.MemoryEvent {
	// Current eBPF interface doesn't provide event history
	// Return empty slice for now
	return []correlation.MemoryEvent{}
}

// getRecentCPUEvents retrieves recent CPU events from the monitor
func (e *EBPFDataSource) getRecentCPUEvents(limit int) []correlation.CPUEvent {
	// Current eBPF interface doesn't provide event history
	// Return empty slice for now
	return []correlation.CPUEvent{}
}

// getRecentIOEvents retrieves recent IO events from the monitor
func (e *EBPFDataSource) getRecentIOEvents(limit int) []correlation.IOEvent {
	// Current eBPF interface doesn't provide event history
	// Return empty slice for now
	return []correlation.IOEvent{}
}

// convertGrowthPattern converts eBPF growth pattern to correlation format
func convertGrowthPattern(pattern []ebpf.MemoryDataPoint) []correlation.MemoryDataPoint {
	points := make([]correlation.MemoryDataPoint, len(pattern))
	for i, p := range pattern {
		points[i] = correlation.MemoryDataPoint{
			Timestamp: p.Timestamp,
			Usage:     p.Usage,
		}
	}
	return points
}
