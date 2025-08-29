package resourcestarvation

import (
	"fmt"
	"time"
)

// EventType represents different types of resource starvation
type EventType uint32

const (
	EventSchedWait      EventType = 1 // Process waiting for CPU (invisible latency)
	EventCFSThrottle    EventType = 2 // Container hit CPU quota
	EventPriorityInvert EventType = 3 // Priority inversion detected
	EventCoreMigrate    EventType = 4 // Process bounced between CPUs
	EventNoisyNeighbor  EventType = 5 // CPU hog detected
)

// String returns the string representation of the event type
func (e EventType) String() string {
	switch e {
	case EventSchedWait:
		return "scheduling_delay"
	case EventCFSThrottle:
		return "cfs_throttle"
	case EventPriorityInvert:
		return "priority_inversion"
	case EventCoreMigrate:
		return "core_migration"
	case EventNoisyNeighbor:
		return "noisy_neighbor"
	default:
		return fmt.Sprintf("unknown_%d", e)
	}
}

// IsCritical returns true if this is a critical starvation event
func (e EventType) IsCritical() bool {
	return e == EventSchedWait || e == EventCFSThrottle || e == EventPriorityInvert
}

// StarvationEvent represents a resource starvation event from eBPF
// This struct MUST match the C struct exactly for proper unmarshaling
type StarvationEvent struct {
	// When and what
	Timestamp uint64 `json:"timestamp"`
	EventType uint32 `json:"event_type"`
	CPUCore   uint32 `json:"cpu_core"`

	// The victim (who got starved)
	VictimPID  uint32 `json:"victim_pid"`
	VictimTGID uint32 `json:"victim_tgid"`
	WaitTimeNS uint64 `json:"wait_time_ns"` // THE INVISIBLE METRIC!
	RunTimeNS  uint64 `json:"run_time_ns"`

	// The culprit (who caused starvation)
	CulpritPID     uint32 `json:"culprit_pid"`
	CulpritTGID    uint32 `json:"culprit_tgid"`
	CulpritRuntime uint64 `json:"culprit_runtime"`

	// Throttling data
	ThrottledNS uint64 `json:"throttled_ns"`
	NrPeriods   uint32 `json:"nr_periods"`
	NrThrottled uint32 `json:"nr_throttled"`

	// Context for correlation
	VictimCgroupID  uint64   `json:"victim_cgroup_id"`
	CulpritCgroupID uint64   `json:"culprit_cgroup_id"`
	VictimComm      [16]byte `json:"-"` // Will be converted to string
	CulpritComm     [16]byte `json:"-"` // Will be converted to string

	// Scheduling info
	VictimPrio   int32  `json:"victim_prio"`
	CulpritPrio  int32  `json:"culprit_prio"`
	VictimPolicy uint32 `json:"victim_policy"`
}

// ProcessedEvent is the enriched event with Kubernetes context
type ProcessedEvent struct {
	Timestamp time.Time `json:"timestamp"`
	EventType string    `json:"event_type"`
	CPUCore   uint32    `json:"cpu_core"`
	NodeName  string    `json:"node_name,omitempty"`

	// Victim information
	Victim VictimInfo `json:"victim"`

	// Culprit information (if identified)
	Culprit *CulpritInfo `json:"culprit,omitempty"`

	// Impact metrics
	Impact ImpactMetrics `json:"impact"`

	// Pattern detection
	Pattern StarvationPattern `json:"pattern"`
}

// VictimInfo contains information about the starved process
type VictimInfo struct {
	PID        uint32  `json:"pid"`
	TGID       uint32  `json:"tgid"`
	Command    string  `json:"command"`
	WaitTimeMS float64 `json:"wait_time_ms"` // Converted to milliseconds
	RunTimeMS  float64 `json:"run_time_ms"`
	Priority   int32   `json:"priority"`
	Policy     string  `json:"scheduling_policy"`

	// Kubernetes context
	PodName       string `json:"pod_name,omitempty"`
	PodNamespace  string `json:"pod_namespace,omitempty"`
	ContainerName string `json:"container_name,omitempty"`
	ContainerID   string `json:"container_id,omitempty"`
}

// CulpritInfo contains information about the process causing starvation
type CulpritInfo struct {
	PID       uint32  `json:"pid"`
	TGID      uint32  `json:"tgid"`
	Command   string  `json:"command"`
	RuntimeMS float64 `json:"runtime_ms"`
	Priority  int32   `json:"priority"`

	// Kubernetes context
	PodName       string `json:"pod_name,omitempty"`
	PodNamespace  string `json:"pod_namespace,omitempty"`
	ContainerName string `json:"container_name,omitempty"`
	ContainerID   string `json:"container_id,omitempty"`
}

// ImpactMetrics quantifies the impact of starvation
type ImpactMetrics struct {
	WaitTimeMS         float64 `json:"wait_time_ms"`
	ThrottleTimeMS     float64 `json:"throttle_time_ms,omitempty"`
	WaitToRunRatio     float64 `json:"wait_to_run_ratio"`    // Wait time / run time
	SeverityLevel      string  `json:"severity_level"`       // minor, moderate, severe, critical
	EstimatedLatencyMS float64 `json:"estimated_latency_ms"` // User-visible impact
	PercentThrottled   float64 `json:"percent_throttled,omitempty"`
}

// StarvationPattern represents detected patterns
type StarvationPattern struct {
	Type        string  `json:"type"`
	Description string  `json:"description"`
	Confidence  float64 `json:"confidence"` // 0-1 confidence score
	Recurring   bool    `json:"recurring"`
}

// Pattern types
const (
	PatternThrottle      = "cpu_throttle"       // Hit CPU quota
	PatternNoisyNeighbor = "noisy_neighbor"     // Another container hogging CPU
	PatternBurst         = "burst_consumption"  // Burst workload pattern
	PatternSustained     = "sustained_pressure" // Continuous starvation
	PatternPriorityInv   = "priority_inversion" // Low-pri blocking high-pri
	PatternCacheThrash   = "cache_thrashing"    // Frequent CPU migrations
)

// Severity levels based on wait time
const (
	SeverityMinor    = "minor"    // <100ms wait
	SeverityModerate = "moderate" // 100-500ms wait
	SeveritySevere   = "severe"   // 500ms-2s wait
	SeverityCritical = "critical" // >2s wait
)

// GetSeverity returns the severity level based on wait time
func GetSeverity(waitTimeNS uint64) string {
	waitMS := waitTimeNS / 1_000_000
	switch {
	case waitMS < 100:
		return SeverityMinor
	case waitMS < 500:
		return SeverityModerate
	case waitMS < 2000:
		return SeveritySevere
	default:
		return SeverityCritical
	}
}

// GetSchedulingPolicy returns the human-readable scheduling policy
func GetSchedulingPolicy(policy uint32) string {
	switch policy {
	case 0:
		return "SCHED_NORMAL"
	case 1:
		return "SCHED_FIFO"
	case 2:
		return "SCHED_RR"
	case 3:
		return "SCHED_BATCH"
	case 5:
		return "SCHED_IDLE"
	case 6:
		return "SCHED_DEADLINE"
	default:
		return fmt.Sprintf("UNKNOWN_%d", policy)
	}
}
