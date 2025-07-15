package plugins

import (
	"fmt"
	"runtime"
	"time"
)

// MemoryCapability defines memory monitoring interface
type MemoryCapability interface {
	GetMemoryStats() ([]ProcessMemoryStats, error)
	GetMemoryPredictions(limits map[uint32]uint64) (map[uint32]*OOMPrediction, error)
}

// ProcessMemoryStats represents memory statistics for a process
type ProcessMemoryStats struct {
	PID       uint32    `json:"pid"`
	RSS       uint64    `json:"rss"`    // Resident Set Size
	VMS       uint64    `json:"vms"`    // Virtual Memory Size
	Shared    uint64    `json:"shared"` // Shared memory
	Text      uint64    `json:"text"`   // Text (code) memory
	Data      uint64    `json:"data"`   // Data + stack memory
	Timestamp time.Time `json:"timestamp"`
}

// OOMPrediction represents an out-of-memory prediction
type OOMPrediction struct {
	PID            uint32        `json:"pid"`
	Probability    float64       `json:"probability"`     // 0.0 to 1.0
	TimeToOOM      time.Duration `json:"time_to_oom"`     // Estimated time until OOM
	CurrentMemory  uint64        `json:"current_memory"`  // Current memory usage
	MemoryLimit    uint64        `json:"memory_limit"`    // Memory limit
	GrowthRate     float64       `json:"growth_rate"`     // Memory growth rate per second
	Confidence     float64       `json:"confidence"`      // Confidence in prediction
	PredictionTime time.Time     `json:"prediction_time"` // When prediction was made
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
func NewCapabilityError(name, reason, platform string) *CapabilityError {
	if platform == "" {
		platform = runtime.GOOS
	}
	return &CapabilityError{
		Name:     name,
		Reason:   reason,
		Platform: platform,
	}
}
