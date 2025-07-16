package plugins

import (
	"context"
	"fmt"
	"runtime"
	"time"
)

// CapabilityStatus represents the status of a capability
type CapabilityStatus int

const (
	CapabilityNotAvailable CapabilityStatus = iota
	CapabilityAvailable
	CapabilityEnabled
	CapabilityError
)

func (s CapabilityStatus) String() string {
	switch s {
	case CapabilityNotAvailable:
		return "not_available"
	case CapabilityAvailable:
		return "available"
	case CapabilityEnabled:
		return "enabled"
	case CapabilityError:
		return "error"
	default:
		return "unknown"
	}
}

// CapabilityInfo provides information about a capability
type CapabilityInfo struct {
	Name         string            `json:"name"`
	Status       CapabilityStatus  `json:"status"`
	Platform     string            `json:"platform"`
	Requirements []string          `json:"requirements,omitempty"`
	Error        string            `json:"error,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// HealthStatus represents the health of a capability
type HealthStatus struct {
	Status    CapabilityStatus `json:"status"`
	Message   string           `json:"message"`
	Timestamp time.Time        `json:"timestamp"`
	Metrics   map[string]any   `json:"metrics,omitempty"`
}

// Capability defines the interface for all system capabilities
type Capability interface {
	Name() string
	Info() *CapabilityInfo
	IsAvailable() bool
	Start(ctx context.Context) error
	Stop() error
	Health() *HealthStatus
}

// MemoryCapability defines memory monitoring interface
type MemoryCapability interface {
	Capability
	GetMemoryStats() ([]ProcessMemoryStats, error)
	GetMemoryPredictions(limits map[uint32]uint64) (map[uint32]*OOMPrediction, error)
}

// ProcessMemoryStats represents memory statistics for a process
type ProcessMemoryStats struct {
	PID            uint32            `json:"pid"`
	Command        string            `json:"command"`
	TotalAllocated uint64            `json:"total_allocated"`
	TotalFreed     uint64            `json:"total_freed"`
	CurrentUsage   uint64            `json:"current_usage"`
	AllocationRate float64           `json:"allocation_rate"` // bytes per second
	LastUpdate     time.Time         `json:"last_update"`
	InContainer    bool              `json:"in_container"`
	ContainerPID   uint32            `json:"container_pid"`
	GrowthPattern  []MemoryDataPoint `json:"growth_pattern"`
}

// MemoryDataPoint represents a single memory measurement
type MemoryDataPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Usage     uint64    `json:"usage"`
}

// OOMPrediction represents an out-of-memory prediction
type OOMPrediction struct {
	PID                uint32        `json:"pid"`
	TimeToOOM          time.Duration `json:"time_to_oom"`
	Confidence         float64       `json:"confidence"`
	CurrentUsage       uint64        `json:"current_usage"`
	MemoryLimit        uint64        `json:"memory_limit"`
	PredictedPeakUsage uint64        `json:"predicted_peak_usage"`
}

// CapabilityError represents an error when a capability is not available
type CapabilityError struct {
	Name     string `json:"name"`
	Reason   string `json:"reason"`
	Platform string `json:"platform"`
}

// Error implements the error interface
func (e *CapabilityError) Error() string {
	return fmt.Sprintf("capability '%s' not available on %s: %s", e.Name, e.Platform, e.Reason)
}

// NewCapabilityError creates a new capability error
func NewCapabilityError(name, reason, platform string) error {
	if platform == "" {
		platform = runtime.GOOS
	}
	return &CapabilityError{
		Name:     name,
		Reason:   reason,
		Platform: platform,
	}
}
