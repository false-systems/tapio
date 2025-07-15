package capabilities

import (
	"context"
	"fmt"
	"time"
)

// CapabilityError represents an error when a capability is not available
type CapabilityError struct {
	Capability string
	Reason     string
	Platform   string
}

func (e *CapabilityError) Error() string {
	return fmt.Sprintf("capability '%s' not available on %s: %s", e.Capability, e.Platform, e.Reason)
}

// IsCapabilityError checks if an error is a capability error
func IsCapabilityError(err error) bool {
	_, ok := err.(*CapabilityError)
	return ok
}

// NewCapabilityError creates a new capability error
func NewCapabilityError(capability, reason, platform string) *CapabilityError {
	return &CapabilityError{
		Capability: capability,
		Reason:     reason,
		Platform:   platform,
	}
}

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

// Capability defines the interface for all system capabilities
type Capability interface {
	// Name returns the capability name
	Name() string

	// Info returns capability information and status
	Info() *CapabilityInfo

	// IsAvailable checks if the capability is available on this platform
	IsAvailable() bool

	// Start initializes the capability
	Start(ctx context.Context) error

	// Stop gracefully shuts down the capability
	Stop() error

	// Health returns the current health status
	Health() *HealthStatus
}

// HealthStatus represents the health of a capability
type HealthStatus struct {
	Status    CapabilityStatus `json:"status"`
	Message   string           `json:"message"`
	Timestamp time.Time        `json:"timestamp"`
	Metrics   map[string]any   `json:"metrics,omitempty"`
}

// MemoryCapability defines the interface for memory monitoring
type MemoryCapability interface {
	Capability

	// GetMemoryStats returns current memory statistics
	GetMemoryStats() ([]ProcessMemoryStats, error)

	// GetMemoryPredictions returns OOM predictions
	GetMemoryPredictions(limits map[uint32]uint64) (map[uint32]*OOMPrediction, error)
}

// NetworkCapability defines the interface for network monitoring
type NetworkCapability interface {
	Capability

	// GetNetworkStats returns network connection statistics
	GetNetworkStats() ([]NetworkConnectionStats, error)

	// GetDNSStats returns DNS query statistics
	GetDNSStats() ([]DNSQueryStats, error)
}

// SystemCapability defines the interface for system-level monitoring
type SystemCapability interface {
	Capability

	// GetSystemEvents returns system events channel
	GetSystemEvents() (<-chan SystemEvent, error)

	// GetJournalLogs returns journal log entries
	GetJournalLogs(since time.Time) ([]LogEntry, error)
}

// Data structures used by capabilities

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

// NetworkConnectionStats represents network connection statistics
type NetworkConnectionStats struct {
	StartTime       time.Time     `json:"start_time"`
	LastSeen        time.Time     `json:"last_seen"`
	BytesSent       uint64        `json:"bytes_sent"`
	BytesReceived   uint64        `json:"bytes_received"`
	PacketsSent     uint64        `json:"packets_sent"`
	PacketsReceived uint64        `json:"packets_received"`
	Retransmits     uint64        `json:"retransmits"`
	Latency         time.Duration `json:"latency"`
}

// DNSQueryStats represents DNS query statistics
type DNSQueryStats struct {
	Domain       string        `json:"domain"`
	QueryCount   uint64        `json:"query_count"`
	SuccessCount uint64        `json:"success_count"`
	FailureCount uint64        `json:"failure_count"`
	AvgLatency   time.Duration `json:"avg_latency"`
	LastQueried  time.Time     `json:"last_queried"`
}

// SystemEvent represents a system event
type SystemEvent struct {
	Type      string                 `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	PID       uint32                 `json:"pid"`
	Data      map[string]interface{} `json:"data"`
}

// LogEntry represents a log entry from journald or other sources
type LogEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Source    string                 `json:"source"`
	PID       uint32                 `json:"pid,omitempty"`
	Unit      string                 `json:"unit,omitempty"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
}