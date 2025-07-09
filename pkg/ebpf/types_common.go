package ebpf

import "time"

// ProcessMemoryStats tracks memory usage for a process
type ProcessMemoryStats struct {
	PID            uint32
	Command        string
	TotalAllocated uint64
	TotalFreed     uint64
	CurrentUsage   uint64
	AllocationRate float64 // bytes per second
	LastUpdate     time.Time
	InContainer    bool
	ContainerPID   uint32
	GrowthPattern  []MemoryDataPoint
}

// MemoryDataPoint represents a point in time memory measurement
type MemoryDataPoint struct {
	Timestamp time.Time
	Usage     uint64
}

// OOMPrediction represents a prediction of OOM kill
type OOMPrediction struct {
	WillOOM      bool
	TimeToOOM    time.Duration
	Confidence   float64 // 0.0 to 1.0
	CurrentUsage uint64
	GrowthRate   float64 // bytes per second
	MemoryLimit  uint64
}
