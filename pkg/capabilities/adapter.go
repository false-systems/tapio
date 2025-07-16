package capabilities

import (
	"context"
	"github.com/yairfalse/tapio/pkg/capabilities/plugins"
)

// pluginAdapter adapts plugins.Capability to capabilities.Capability
type pluginAdapter struct {
	plugin plugins.Capability
}

// Name returns the capability name
func (a *pluginAdapter) Name() string {
	return a.plugin.Name()
}

// Info returns capability information
func (a *pluginAdapter) Info() *CapabilityInfo {
	pluginInfo := a.plugin.Info()
	return &CapabilityInfo{
		Name:         pluginInfo.Name,
		Status:       CapabilityStatus(pluginInfo.Status),
		Platform:     pluginInfo.Platform,
		Requirements: pluginInfo.Requirements,
		Error:        pluginInfo.Error,
		Metadata:     pluginInfo.Metadata,
	}
}

// IsAvailable checks if the capability is available
func (a *pluginAdapter) IsAvailable() bool {
	return a.plugin.IsAvailable()
}

// Start initializes the capability
func (a *pluginAdapter) Start(ctx context.Context) error {
	return a.plugin.Start(ctx)
}

// Stop gracefully shuts down the capability
func (a *pluginAdapter) Stop() error {
	return a.plugin.Stop()
}

// Health returns the current health status
func (a *pluginAdapter) Health() *HealthStatus {
	pluginHealth := a.plugin.Health()
	return &HealthStatus{
		Status:    CapabilityStatus(pluginHealth.Status),
		Message:   pluginHealth.Message,
		Timestamp: pluginHealth.Timestamp,
		Metrics:   pluginHealth.Metrics,
	}
}

// GetMemoryStats implements MemoryCapability if the plugin supports it
func (a *pluginAdapter) GetMemoryStats() ([]ProcessMemoryStats, error) {
	if memCap, ok := a.plugin.(plugins.MemoryCapability); ok {
		pluginStats, err := memCap.GetMemoryStats()
		if err != nil {
			return nil, err
		}
		
		// Convert plugin stats to capabilities stats
		stats := make([]ProcessMemoryStats, len(pluginStats))
		for i, ps := range pluginStats {
			stats[i] = ProcessMemoryStats{
				PID:            ps.PID,
				Command:        ps.Command,
				TotalAllocated: ps.TotalAllocated,
				TotalFreed:     ps.TotalFreed,
				CurrentUsage:   ps.CurrentUsage,
				AllocationRate: ps.AllocationRate,
				LastUpdate:     ps.LastUpdate,
				InContainer:    ps.InContainer,
				ContainerPID:   ps.ContainerPID,
				GrowthPattern:  convertMemoryDataPoints(ps.GrowthPattern),
			}
		}
		return stats, nil
	}
	return nil, NewCapabilityError(a.plugin.Name(), "does not implement MemoryCapability", "")
}

// GetMemoryPredictions implements MemoryCapability if the plugin supports it
func (a *pluginAdapter) GetMemoryPredictions(limits map[uint32]uint64) (map[uint32]*OOMPrediction, error) {
	if memCap, ok := a.plugin.(plugins.MemoryCapability); ok {
		pluginPreds, err := memCap.GetMemoryPredictions(limits)
		if err != nil {
			return nil, err
		}
		
		// Convert plugin predictions to capabilities predictions
		predictions := make(map[uint32]*OOMPrediction)
		for pid, pp := range pluginPreds {
			predictions[pid] = &OOMPrediction{
				PID:                pp.PID,
				TimeToOOM:          pp.TimeToOOM,
				Confidence:         pp.Confidence,
				CurrentUsage:       pp.CurrentUsage,
				MemoryLimit:        pp.MemoryLimit,
				PredictedPeakUsage: pp.PredictedPeakUsage,
			}
		}
		return predictions, nil
	}
	return nil, NewCapabilityError(a.plugin.Name(), "does not implement MemoryCapability", "")
}

// convertMemoryDataPoints converts plugin memory data points to capabilities format
func convertMemoryDataPoints(pluginPoints []plugins.MemoryDataPoint) []MemoryDataPoint {
	points := make([]MemoryDataPoint, len(pluginPoints))
	for i, pp := range pluginPoints {
		points[i] = MemoryDataPoint{
			Timestamp: pp.Timestamp,
			Usage:     pp.Usage,
		}
	}
	return points
}