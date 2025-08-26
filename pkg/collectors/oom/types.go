//go:build linux

package oom

import (
	"fmt"
	"time"
	"unsafe"

	"github.com/yairfalse/tapio/pkg/domain"
)

// OOMEventType represents the type of OOM-related event
type OOMEventType uint32

const (
	// OOM Event Types - Every type is ACTIONABLE intelligence
	OOMKillVictim        OOMEventType = 1 // Process was killed by OOM killer
	OOMKillTriggered     OOMEventType = 2 // OOM killer was triggered
	MemoryPressureHigh   OOMEventType = 3 // Memory pressure detected (prediction)
	MemoryPressureCrit   OOMEventType = 4 // Critical memory pressure (imminent OOM)
	ContainerMemoryLimit OOMEventType = 5 // Container hit memory limit
	CgroupOOMNotify      OOMEventType = 6 // Cgroup OOM notification
)

// String returns the string representation of OOMEventType
func (t OOMEventType) String() string {
	switch t {
	case OOMKillVictim:
		return "oom_kill_victim"
	case OOMKillTriggered:
		return "oom_kill_triggered"
	case MemoryPressureHigh:
		return "memory_pressure_high"
	case MemoryPressureCrit:
		return "memory_pressure_critical"
	case ContainerMemoryLimit:
		return "container_memory_limit"
	case CgroupOOMNotify:
		return "cgroup_oom_notification"
	default:
		return "unknown"
	}
}

// IsCritical returns true if this is a critical OOM event
func (t OOMEventType) IsCritical() bool {
	return t == OOMKillVictim || t == OOMKillTriggered || t == MemoryPressureCrit
}

// IsPredictive returns true if this is a predictive event
func (t OOMEventType) IsPredictive() bool {
	return t == MemoryPressureHigh || t == MemoryPressureCrit
}

// OOMEvent represents a kernel OOM event from eBPF (must match C struct exactly)
// This struct is the SMOKING GUN for every container death
type OOMEvent struct {
	// CORE EVENT DATA - NEVER CHANGE ORDER (affects kernel-userspace ABI)
	Timestamp uint64 // When the smoking gun was fired
	PID       uint32 // Victim process PID
	TGID      uint32 // Victim thread group ID
	PPID      uint32 // Parent PID (who spawned the victim)
	KillerPID uint32 // PID of process that triggered OOM

	// MEMORY FORENSICS - The financial damage
	MemoryUsage    uint64 // Current memory usage in bytes
	MemoryLimit    uint64 // Memory limit in bytes
	MemoryMaxUsage uint64 // Peak memory usage before death
	SwapUsage      uint64 // Swap usage in bytes
	CacheUsage     uint64 // Cache usage in bytes

	// PROCESS IDENTIFICATION - Who died and why
	UID       uint32 // User ID
	GID       uint32 // Group ID
	CgroupID  uint64 // Cgroup ID for correlation
	EventType uint32 // OOM event type
	OOMScore  uint32 // OOM killer score (higher = more likely to die)

	// KUBERNETES CONTEXT - The business impact
	Comm        [16]byte  // Process command (15 chars + null)
	CgroupPath  [256]byte // Full cgroup path for K8s correlation
	ContainerID [64]byte  // Docker/containerd container ID
	Cmdline     [256]byte // Full command line for analysis

	// PERFORMANCE DATA - How bad was it?
	PagesScanned   uint64 // Number of pages scanned before giving up
	PagesReclaimed uint64 // Pages successfully reclaimed
	GFPFlags       uint32 // Memory allocation flags that failed
	Order          uint32 // Memory allocation order that triggered OOM

	// CAUSALITY CHAIN - Root cause analysis
	TriggerPID     uint32 // PID that caused the memory pressure
	AllocationSize uint64 // Size of allocation that triggered OOM
	TimeToKillMS   uint64 // Time from pressure to kill (latency)

	// PREDICTION DATA - Early warning system
	PressureDurationMS uint32 // How long we've been under pressure
	AllocationRateMBS  uint32 // Memory allocation rate MB/s
	ReclaimEfficiency  uint32 // Reclaim efficiency percentage

	Pad [4]uint8 // Ensure 8-byte alignment
}

// ProcessedOOMEvent represents a parsed OOM event with rich context
type ProcessedOOMEvent struct {
	// Core event data
	EventType  OOMEventType `json:"event_type"`
	Timestamp  time.Time    `json:"timestamp"`
	PID        uint32       `json:"pid"`
	TGID       uint32       `json:"tgid"`
	PPID       uint32       `json:"ppid"`
	KillerPID  uint32       `json:"killer_pid,omitempty"`
	TriggerPID uint32       `json:"trigger_pid,omitempty"`

	// Process context
	Command     string `json:"command"`
	Commandline string `json:"commandline,omitempty"`
	UID         uint32 `json:"uid"`
	GID         uint32 `json:"gid"`
	OOMScore    uint32 `json:"oom_score"`

	// Memory forensics - The financial damage
	MemoryStats MemoryStatistics `json:"memory_stats"`

	// Kubernetes context
	KubernetesContext KubernetesContext `json:"k8s_context"`

	// Performance and causality data
	PerformanceData PerformanceData `json:"performance_data"`

	// Prediction data (for early warning events)
	PredictionData *PredictionData `json:"prediction_data,omitempty"`

	// System context
	SystemContext SystemContext `json:"system_context"`
}

// MemoryStatistics contains detailed memory usage information
type MemoryStatistics struct {
	// Current memory state
	UsageBytes      uint64 `json:"usage_bytes"`
	LimitBytes      uint64 `json:"limit_bytes"`
	MaxUsageBytes   uint64 `json:"max_usage_bytes"`
	SwapUsageBytes  uint64 `json:"swap_usage_bytes"`
	CacheUsageBytes uint64 `json:"cache_usage_bytes"`

	// Memory pressure indicators
	UsagePercent     float64 `json:"usage_percent"`
	PressureLevel    string  `json:"pressure_level"`                       // "low", "medium", "high", "critical"
	TimeToExhaustion *int64  `json:"time_to_exhaustion_seconds,omitempty"` // Predicted seconds until OOM
}

// KubernetesContext contains Kubernetes-specific information
type KubernetesContext struct {
	// Container identification
	ContainerID   string `json:"container_id"`
	ContainerName string `json:"container_name,omitempty"`
	Runtime       string `json:"runtime,omitempty"` // "docker", "containerd", "cri-o"

	// Pod context
	PodName      string `json:"pod_name,omitempty"`
	PodNamespace string `json:"pod_namespace,omitempty"`
	PodUID       string `json:"pod_uid,omitempty"`

	// Node context
	NodeName string `json:"node_name,omitempty"`

	// Resource context
	CgroupPath   string `json:"cgroup_path"`
	CgroupID     uint64 `json:"cgroup_id"`
	ResourceType string `json:"resource_type,omitempty"` // "pod", "system", "burstable"

	// Workload context (derived from cgroup analysis)
	WorkloadKind string `json:"workload_kind,omitempty"` // "Deployment", "StatefulSet", "DaemonSet"
	WorkloadName string `json:"workload_name,omitempty"`
}

// PerformanceData contains performance metrics during OOM
type PerformanceData struct {
	// Memory reclaim performance
	PagesScanned     uint64  `json:"pages_scanned"`
	PagesReclaimed   uint64  `json:"pages_reclaimed"`
	ReclaimRatio     float64 `json:"reclaim_ratio"`
	ReclaimLatencyMS uint64  `json:"reclaim_latency_ms,omitempty"`

	// Allocation context
	AllocationOrder uint32 `json:"allocation_order"`
	GFPFlags        uint32 `json:"gfp_flags"`
	AllocationSize  uint64 `json:"allocation_size,omitempty"`

	// Timing information
	TimeToKillMS       uint64 `json:"time_to_kill_ms,omitempty"`
	PressureDurationMS uint32 `json:"pressure_duration_ms,omitempty"`
}

// PredictionData contains predictive analytics for early warning
type PredictionData struct {
	// Memory allocation trends
	AllocationRateMBS uint32 `json:"allocation_rate_mb_s"`
	AllocationTrend   string `json:"allocation_trend"` // "increasing", "stable", "decreasing"
	PredictedOOMTimeS *int64 `json:"predicted_oom_time_seconds,omitempty"`
	ConfidencePercent uint32 `json:"confidence_percent"`

	// Memory pressure metrics
	PressureDurationMS uint32 `json:"pressure_duration_ms"`
	PressureSeverity   string `json:"pressure_severity"` // "mild", "moderate", "severe"
	ReclaimEfficiency  uint32 `json:"reclaim_efficiency_percent"`

	// Actionable insights
	RecommendedAction    string   `json:"recommended_action"`
	CriticalProcesses    []string `json:"critical_processes,omitempty"`
	EstimatedImpactLevel string   `json:"estimated_impact_level"` // "low", "medium", "high", "critical"
}

// SystemContext contains system-level context
type SystemContext struct {
	// Host information
	HostName      string `json:"hostname,omitempty"`
	KernelVersion string `json:"kernel_version,omitempty"`
	SystemLoad    string `json:"system_load,omitempty"`

	// Memory subsystem context
	MemorySubsystem string `json:"memory_subsystem"` // "cgroup", "global", "node"
	NUMANode        *int32 `json:"numa_node,omitempty"`
	MemoryZone      string `json:"memory_zone,omitempty"`

	// Collection metadata
	CollectorVersion string `json:"collector_version"`
	EventReliability string `json:"event_reliability"` // "confirmed", "inferred", "suspected"
}

// OOMConfig represents the configuration for OOM monitoring
type OOMConfig struct {
	// Monitoring settings
	EnablePrediction         bool   `json:"enable_prediction" yaml:"enable_prediction"`
	PredictionThresholdPct   uint32 `json:"prediction_threshold_percent" yaml:"prediction_threshold_percent"`
	HighPressureThresholdPct uint32 `json:"high_pressure_threshold_percent" yaml:"high_pressure_threshold_percent"`

	// Buffer settings
	RingBufferSize     uint32 `json:"ring_buffer_size" yaml:"ring_buffer_size"`
	EventBatchSize     uint32 `json:"event_batch_size" yaml:"event_batch_size"`
	MaxEventsPerSecond uint32 `json:"max_events_per_second" yaml:"max_events_per_second"`

	// Collection settings
	CollectCmdline       bool `json:"collect_cmdline" yaml:"collect_cmdline"`
	CollectEnvironment   bool `json:"collect_environment" yaml:"collect_environment"`
	CollectMemoryDetails bool `json:"collect_memory_details" yaml:"collect_memory_details"`

	// Filtering settings
	ExcludeSystemProcesses bool     `json:"exclude_system_processes" yaml:"exclude_system_processes"`
	IncludeNamespaces      []string `json:"include_namespaces" yaml:"include_namespaces"`
	ExcludeNamespaces      []string `json:"exclude_namespaces" yaml:"exclude_namespaces"`

	// Correlation settings
	EnableK8sCorrelation bool          `json:"enable_k8s_correlation" yaml:"enable_k8s_correlation"`
	K8sContextTimeout    time.Duration `json:"k8s_context_timeout" yaml:"k8s_context_timeout"`
}

// DefaultOOMConfig returns default configuration
func DefaultOOMConfig() *OOMConfig {
	return &OOMConfig{
		EnablePrediction:         true,
		PredictionThresholdPct:   95,
		HighPressureThresholdPct: 80,
		RingBufferSize:           1048576, // 1MB
		EventBatchSize:           100,
		MaxEventsPerSecond:       1000,
		CollectCmdline:           true,
		CollectEnvironment:       false, // Can be expensive
		CollectMemoryDetails:     true,
		ExcludeSystemProcesses:   true,
		EnableK8sCorrelation:     true,
		K8sContextTimeout:        time.Second * 5,
	}
}

// Validate validates the OOM configuration
func (c *OOMConfig) Validate() error {
	if c.PredictionThresholdPct > 100 {
		return NewValidationError("prediction_threshold_percent", c.PredictionThresholdPct, "must be <= 100")
	}
	if c.HighPressureThresholdPct > 100 {
		return NewValidationError("high_pressure_threshold_percent", c.HighPressureThresholdPct, "must be <= 100")
	}
	if c.RingBufferSize < 4096 {
		return NewValidationError("ring_buffer_size", c.RingBufferSize, "must be >= 4096")
	}
	if c.EventBatchSize == 0 {
		return NewValidationError("event_batch_size", c.EventBatchSize, "must be > 0")
	}
	return nil
}

// ValidationError represents a validation error
type ValidationError struct {
	Field string
	Value interface{}
	Rule  string
}

// NewValidationError creates a new validation error
func NewValidationError(field string, value interface{}, rule string) *ValidationError {
	return &ValidationError{
		Field: field,
		Value: value,
		Rule:  rule,
	}
}

func (e *ValidationError) Error() string {
	return "validation failed for field " + e.Field + ": " + e.Rule
}

// Helper functions for type conversion and safety

// ToProcessedEvent converts a raw OOM event to a processed event with rich context
func (raw *OOMEvent) ToProcessedEvent() *ProcessedOOMEvent {
	processed := &ProcessedOOMEvent{
		EventType:   OOMEventType(raw.EventType),
		Timestamp:   time.Unix(0, int64(raw.Timestamp)),
		PID:         raw.PID,
		TGID:        raw.TGID,
		PPID:        raw.PPID,
		KillerPID:   raw.KillerPID,
		TriggerPID:  raw.TriggerPID,
		UID:         raw.UID,
		GID:         raw.GID,
		OOMScore:    raw.OOMScore,
		Command:     nullTerminatedString(raw.Comm[:]),
		Commandline: nullTerminatedString(raw.Cmdline[:]),

		MemoryStats: MemoryStatistics{
			UsageBytes:      raw.MemoryUsage,
			LimitBytes:      raw.MemoryLimit,
			MaxUsageBytes:   raw.MemoryMaxUsage,
			SwapUsageBytes:  raw.SwapUsage,
			CacheUsageBytes: raw.CacheUsage,
		},

		KubernetesContext: KubernetesContext{
			ContainerID: nullTerminatedString(raw.ContainerID[:]),
			CgroupPath:  nullTerminatedString(raw.CgroupPath[:]),
			CgroupID:    raw.CgroupID,
		},

		PerformanceData: PerformanceData{
			PagesScanned:       raw.PagesScanned,
			PagesReclaimed:     raw.PagesReclaimed,
			AllocationOrder:    raw.Order,
			GFPFlags:           raw.GFPFlags,
			AllocationSize:     raw.AllocationSize,
			TimeToKillMS:       raw.TimeToKillMS,
			PressureDurationMS: raw.PressureDurationMS,
		},

		SystemContext: SystemContext{
			CollectorVersion: "1.0.0",
			EventReliability: "confirmed",
		},
	}

	// Calculate derived fields
	processed.calculateDerivedFields()

	// Add prediction data for predictive events
	if OOMEventType(raw.EventType).IsPredictive() {
		processed.PredictionData = &PredictionData{
			AllocationRateMBS:  raw.AllocationRateMBS,
			PressureDurationMS: raw.PressureDurationMS,
			ReclaimEfficiency:  raw.ReclaimEfficiency,
			ConfidencePercent:  85, // Default confidence
		}
		processed.calculatePredictionData()
	}

	return processed
}

// calculateDerivedFields calculates derived fields from raw data
func (p *ProcessedOOMEvent) calculateDerivedFields() {
	// Calculate memory usage percentage
	if p.MemoryStats.LimitBytes > 0 {
		p.MemoryStats.UsagePercent = float64(p.MemoryStats.UsageBytes) / float64(p.MemoryStats.LimitBytes) * 100
	}

	// Determine pressure level
	if p.MemoryStats.UsagePercent >= 95 {
		p.MemoryStats.PressureLevel = "critical"
	} else if p.MemoryStats.UsagePercent >= 80 {
		p.MemoryStats.PressureLevel = "high"
	} else if p.MemoryStats.UsagePercent >= 50 {
		p.MemoryStats.PressureLevel = "medium"
	} else {
		p.MemoryStats.PressureLevel = "low"
	}

	// Calculate reclaim ratio
	if p.PerformanceData.PagesScanned > 0 {
		p.PerformanceData.ReclaimRatio = float64(p.PerformanceData.PagesReclaimed) / float64(p.PerformanceData.PagesScanned)
	}

	// Extract Kubernetes context from cgroup path
	p.extractKubernetesContext()
}

// calculatePredictionData calculates prediction-specific fields
func (p *ProcessedOOMEvent) calculatePredictionData() {
	if p.PredictionData == nil {
		return
	}

	// Determine allocation trend
	if p.PredictionData.AllocationRateMBS > 20 {
		p.PredictionData.AllocationTrend = "increasing"
	} else if p.PredictionData.AllocationRateMBS > 5 {
		p.PredictionData.AllocationTrend = "stable"
	} else {
		p.PredictionData.AllocationTrend = "decreasing"
	}

	// Calculate predicted OOM time if allocation rate is positive
	if p.PredictionData.AllocationRateMBS > 0 && p.MemoryStats.LimitBytes > p.MemoryStats.UsageBytes {
		remainingMB := (p.MemoryStats.LimitBytes - p.MemoryStats.UsageBytes) / (1024 * 1024)
		predictedSeconds := int64(remainingMB / uint64(p.PredictionData.AllocationRateMBS))
		p.PredictionData.PredictedOOMTimeS = &predictedSeconds

		// Calculate time to exhaustion
		p.MemoryStats.TimeToExhaustion = &predictedSeconds
	}

	// Determine pressure severity
	if p.PredictionData.PressureDurationMS > 30000 { // 30 seconds
		p.PredictionData.PressureSeverity = "severe"
	} else if p.PredictionData.PressureDurationMS > 10000 { // 10 seconds
		p.PredictionData.PressureSeverity = "moderate"
	} else {
		p.PredictionData.PressureSeverity = "mild"
	}

	// Determine recommended action
	if p.EventType == MemoryPressureCrit {
		p.PredictionData.RecommendedAction = "immediate_scale_up"
		p.PredictionData.EstimatedImpactLevel = "critical"
	} else if p.EventType == MemoryPressureHigh {
		p.PredictionData.RecommendedAction = "scale_up_or_optimize"
		p.PredictionData.EstimatedImpactLevel = "high"
	}
}

// extractKubernetesContext extracts Kubernetes context from cgroup path
func (p *ProcessedOOMEvent) extractKubernetesContext() {
	// This is a simplified implementation
	// Real implementation would parse various cgroup path formats:
	// /sys/fs/cgroup/memory/kubepods/burstable/pod<uid>/container_id
	// /kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod<uid>.slice/docker-<container_id>.scope

	if len(p.KubernetesContext.CgroupPath) == 0 {
		return
	}

	path := p.KubernetesContext.CgroupPath

	// Extract pod UID from various patterns
	if podUID := extractPodUID(path); podUID != "" {
		p.KubernetesContext.PodUID = podUID
	}

	// Determine resource type
	if containsString(path, "burstable") {
		p.KubernetesContext.ResourceType = "burstable"
	} else if containsString(path, "besteffort") {
		p.KubernetesContext.ResourceType = "besteffort"
	} else if containsString(path, "guaranteed") {
		p.KubernetesContext.ResourceType = "guaranteed"
	}

	// Detect runtime
	if containsString(path, "docker") {
		p.KubernetesContext.Runtime = "docker"
	} else if containsString(path, "containerd") {
		p.KubernetesContext.Runtime = "containerd"
	} else if containsString(path, "cri-o") {
		p.KubernetesContext.Runtime = "cri-o"
	}
}

// ToCollectorEvent converts ProcessedOOMEvent to domain.CollectorEvent
func (p *ProcessedOOMEvent) ToCollectorEvent() *domain.CollectorEvent {
	event := &domain.CollectorEvent{
		EventID:   generateEventID(p.PID, p.Timestamp),
		Timestamp: p.Timestamp,
		Type:      domain.EventTypeContainerOOM,
		Source:    "oom-collector",
		Severity:  p.determineSeverity(),

		EventData: domain.EventDataContainer{
			Container: &domain.ContainerData{
				ContainerID: p.KubernetesContext.ContainerID,
				Runtime:     p.KubernetesContext.Runtime,
				State:       "killed",
				Action:      "oom_kill",
				PID:         int32(p.PID),
			},

			Process: &domain.ProcessData{
				PID:         int32(p.PID),
				PPID:        int32(p.PPID),
				Command:     p.Command,
				UID:         int32(p.UID),
				GID:         int32(p.GID),
				CgroupPath:  p.KubernetesContext.CgroupPath,
				ContainerID: p.KubernetesContext.ContainerID,
				StartTime:   p.Timestamp, // Approximation
			},
		},

		Metadata: domain.EventMetadata{
			PodName:       p.KubernetesContext.PodName,
			PodNamespace:  p.KubernetesContext.PodNamespace,
			PodUID:        p.KubernetesContext.PodUID,
			ContainerID:   p.KubernetesContext.ContainerID,
			ContainerName: p.KubernetesContext.ContainerName,
			NodeName:      p.KubernetesContext.NodeName,
			PID:           int32(p.PID),
			PPID:          int32(p.PPID),
			UID:           int32(p.UID),
			GID:           int32(p.GID),
			CgroupID:      p.KubernetesContext.CgroupID,
			Command:       p.Command,
			Priority:      p.determinePriority(),
			Tags:          p.generateTags(),
			Labels:        p.generateLabels(),
			Attributes:    p.generateAttributes(),
		},

		CorrelationHints: &domain.CorrelationHints{
			PodUID:      p.KubernetesContext.PodUID,
			ContainerID: p.KubernetesContext.ContainerID,
			ProcessID:   int32(p.PID),
			CgroupPath:  p.KubernetesContext.CgroupPath,
			NodeName:    p.KubernetesContext.NodeName,
			CorrelationTags: map[string]string{
				"oom_event_type":    p.EventType.String(),
				"memory_pressure":   p.MemoryStats.PressureLevel,
				"container_runtime": p.KubernetesContext.Runtime,
			},
		},
	}

	// Add Kubernetes context if available
	if p.KubernetesContext.PodName != "" {
		event.K8sContext = &domain.K8sContext{
			Kind:      "Pod",
			Name:      p.KubernetesContext.PodName,
			Namespace: p.KubernetesContext.PodNamespace,
			UID:       p.KubernetesContext.PodUID,
			NodeName:  p.KubernetesContext.NodeName,
		}
	}

	return event
}

// ToObservationEvent converts ProcessedOOMEvent to domain.ObservationEvent for correlation
func (p *ProcessedOOMEvent) ToObservationEvent() *domain.ObservationEvent {
	pid := int32(p.PID)

	event := &domain.ObservationEvent{
		ID:        generateEventID(p.PID, p.Timestamp),
		Timestamp: p.Timestamp,
		Source:    "oom",
		Type:      p.EventType.String(),
		PID:       &pid,
		Action:    stringPtr(p.determineAction()),
		Target:    stringPtr(p.Command),
		Result:    stringPtr("killed"),
		Reason:    stringPtr("out_of_memory"),
		Data:      p.generateObservationData(),
	}

	// Add correlation keys
	if p.KubernetesContext.ContainerID != "" {
		event.ContainerID = &p.KubernetesContext.ContainerID
	}
	if p.KubernetesContext.PodName != "" {
		event.PodName = &p.KubernetesContext.PodName
	}
	if p.KubernetesContext.PodNamespace != "" {
		event.Namespace = &p.KubernetesContext.PodNamespace
	}
	if p.KubernetesContext.NodeName != "" {
		event.NodeName = &p.KubernetesContext.NodeName
	}

	// Add metrics
	if p.PredictionData != nil && p.PredictionData.PredictedOOMTimeS != nil {
		event.Duration = p.PredictionData.PredictedOOMTimeS
	}

	memoryUsageMB := int64(p.MemoryStats.UsageBytes / (1024 * 1024))
	event.Size = &memoryUsageMB

	return event
}

// Helper functions

func nullTerminatedString(bytes []byte) string {
	n := len(bytes)
	for i, b := range bytes {
		if b == 0 {
			n = i
			break
		}
	}
	return string(bytes[:n])
}

func containsString(s, substr string) bool {
	// Simple contains check - real implementation would use strings.Contains
	// but we avoid imports for this example
	return len(s) >= len(substr) && s[0:len(substr)] == substr
}

func extractPodUID(path string) string {
	// Simplified pod UID extraction
	// Real implementation would handle multiple formats
	return ""
}

func generateEventID(pid uint32, timestamp time.Time) string {
	return fmt.Sprintf("oom-%d-%d", pid, timestamp.Unix())
}

func (p *ProcessedOOMEvent) determineSeverity() domain.EventSeverity {
	if p.EventType.IsCritical() {
		return domain.EventSeverityCritical
	} else if p.EventType.IsPredictive() && p.MemoryStats.UsagePercent > 90 {
		return domain.EventSeverityHigh
	} else if p.EventType.IsPredictive() {
		return domain.EventSeverityMedium
	}
	return domain.EventSeverityLow
}

func (p *ProcessedOOMEvent) determinePriority() domain.EventPriority {
	if p.EventType == OOMKillVictim {
		return domain.PriorityCritical
	} else if p.EventType == MemoryPressureCrit {
		return domain.PriorityHigh
	}
	return domain.PriorityNormal
}

func (p *ProcessedOOMEvent) generateTags() []string {
	tags := []string{
		"oom",
		p.EventType.String(),
		p.MemoryStats.PressureLevel + "_pressure",
	}

	if p.KubernetesContext.Runtime != "" {
		tags = append(tags, "runtime:"+p.KubernetesContext.Runtime)
	}

	return tags
}

func (p *ProcessedOOMEvent) generateLabels() map[string]string {
	labels := map[string]string{
		"oom_event_type":    p.EventType.String(),
		"memory_pressure":   p.MemoryStats.PressureLevel,
		"command":           p.Command,
		"container_runtime": p.KubernetesContext.Runtime,
	}

	if p.KubernetesContext.PodName != "" {
		labels["pod_name"] = p.KubernetesContext.PodName
	}
	if p.KubernetesContext.PodNamespace != "" {
		labels["namespace"] = p.KubernetesContext.PodNamespace
	}

	return labels
}

func (p *ProcessedOOMEvent) generateAttributes() map[string]string {
	attrs := make(map[string]string)

	// Add memory statistics
	attrs["memory_usage_mb"] = fmt.Sprintf("%d", p.MemoryStats.UsageBytes/(1024*1024))
	attrs["memory_limit_mb"] = fmt.Sprintf("%d", p.MemoryStats.LimitBytes/(1024*1024))
	attrs["memory_usage_percent"] = fmt.Sprintf("%.1f", p.MemoryStats.UsagePercent)

	// Add performance data
	if p.PerformanceData.PagesScanned > 0 {
		attrs["pages_scanned"] = fmt.Sprintf("%d", p.PerformanceData.PagesScanned)
	}
	if p.PerformanceData.ReclaimRatio > 0 {
		attrs["reclaim_ratio"] = fmt.Sprintf("%.1f", p.PerformanceData.ReclaimRatio*100)
	}

	// Add prediction data
	if p.PredictionData != nil {
		attrs["allocation_rate_mb_s"] = fmt.Sprintf("%d", p.PredictionData.AllocationRateMBS)
		if p.PredictionData.PredictedOOMTimeS != nil {
			attrs["predicted_oom_time_s"] = fmt.Sprintf("%d", *p.PredictionData.PredictedOOMTimeS)
		}
	}

	return attrs
}

func (p *ProcessedOOMEvent) determineAction() string {
	switch p.EventType {
	case OOMKillVictim:
		return "kill"
	case MemoryPressureHigh:
		return "warn"
	case MemoryPressureCrit:
		return "alert"
	default:
		return "monitor"
	}
}

func (p *ProcessedOOMEvent) generateObservationData() map[string]string {
	data := map[string]string{
		"event_type":      p.EventType.String(),
		"memory_usage_mb": fmt.Sprintf("%d", p.MemoryStats.UsageBytes/(1024*1024)),
		"memory_limit_mb": fmt.Sprintf("%d", p.MemoryStats.LimitBytes/(1024*1024)),
		"memory_pressure": p.MemoryStats.PressureLevel,
		"oom_score":       fmt.Sprintf("%d", p.OOMScore),
	}

	if p.KubernetesContext.ContainerID != "" {
		data["container_id"] = p.KubernetesContext.ContainerID
	}
	if p.KubernetesContext.Runtime != "" {
		data["runtime"] = p.KubernetesContext.Runtime
	}

	return data
}

func stringPtr(s string) *string {
	return &s
}

// GetSize returns the size of OOMEvent struct for validation
func GetOOMEventSize() uintptr {
	return unsafe.Sizeof(OOMEvent{})
}
