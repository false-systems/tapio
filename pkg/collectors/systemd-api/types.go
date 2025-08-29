//go:build linux

package systemdapi

import (
	"encoding/json"
	"time"

	"github.com/coreos/go-systemd/v22/sdjournal"
	"github.com/yairfalse/tapio/pkg/domain"
)

// SystemdEventType represents the type of systemd event
type SystemdEventType string

const (
	// Service events
	SystemdEventServiceStart   SystemdEventType = "systemd.service.start"
	SystemdEventServiceStop    SystemdEventType = "systemd.service.stop"
	SystemdEventServiceRestart SystemdEventType = "systemd.service.restart"
	SystemdEventServiceFailed  SystemdEventType = "systemd.service.failed"
	SystemdEventServiceReload  SystemdEventType = "systemd.service.reload"

	// Unit events
	SystemdEventUnitActive   SystemdEventType = "systemd.unit.active"
	SystemdEventUnitInactive SystemdEventType = "systemd.unit.inactive"
	SystemdEventUnitFailed   SystemdEventType = "systemd.unit.failed"

	// Journal events
	SystemdEventJournalEntry SystemdEventType = "systemd.journal.entry"
	SystemdEventJournalError SystemdEventType = "systemd.journal.error"

	// System events
	SystemdEventSystemBoot     SystemdEventType = "systemd.system.boot"
	SystemdEventSystemShutdown SystemdEventType = "systemd.system.shutdown"
)

// JournalEntry represents a structured journal entry from systemd
type JournalEntry struct {
	// Core journal fields
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
	Priority  int       `json:"priority"`

	// Process information
	PID     int32  `json:"pid,omitempty"`
	Command string `json:"command,omitempty"`

	// Systemd-specific fields
	Unit         string `json:"unit,omitempty"`
	UnitResult   string `json:"unit_result,omitempty"`
	JobID        string `json:"job_id,omitempty"`
	JobType      string `json:"job_type,omitempty"`
	JobResult    string `json:"job_result,omitempty"`
	InvocationID string `json:"invocation_id,omitempty"`

	// System information
	Hostname  string `json:"hostname,omitempty"`
	MachineID string `json:"machine_id,omitempty"`
	BootID    string `json:"boot_id,omitempty"`
	Transport string `json:"transport,omitempty"`
	SyslogID  string `json:"syslog_id,omitempty"`

	// Container/cgroup information for K8s correlation
	CgroupPath  string `json:"cgroup_path,omitempty"`
	ContainerID string `json:"container_id,omitempty"`

	// Additional fields for debugging and correlation
	Fields map[string]string `json:"fields,omitempty"`

	// Extra journal data - all journal values are strings
	ExtraData map[string]string `json:"extra_data,omitempty"`
}

// SystemdEventData represents systemd-specific event data for CollectorEvent
type SystemdEventData struct {
	// Event classification
	EventType SystemdEventType `json:"event_type"`
	Source    string           `json:"source"` // "journal", "dbus", etc.

	// Journal entry (for journal events)
	JournalEntry *JournalEntry `json:"journal_entry,omitempty"`

	// Service/unit information
	UnitName   string    `json:"unit_name,omitempty"`
	UnitType   string    `json:"unit_type,omitempty"`  // service, socket, timer, etc.
	UnitState  string    `json:"unit_state,omitempty"` // active, inactive, failed
	SubState   string    `json:"sub_state,omitempty"`  // running, dead, failed
	ActiveTime time.Time `json:"active_time,omitempty"`

	// Error information
	ErrorCode    int32  `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	ExitCode     int32  `json:"exit_code,omitempty"`
	Signal       int32  `json:"signal,omitempty"`

	// Performance data
	MemoryUsage  int64         `json:"memory_usage,omitempty"`  // bytes
	CPUUsage     float64       `json:"cpu_usage,omitempty"`     // percentage
	Duration     time.Duration `json:"duration,omitempty"`      // operation duration
	RestartCount int32         `json:"restart_count,omitempty"` // service restart count

	// Correlation keys for linking with other events
	ProcessID     int32  `json:"process_id,omitempty"`
	ContainerID   string `json:"container_id,omitempty"`
	PodName       string `json:"pod_name,omitempty"`
	KubernetesUID string `json:"kubernetes_uid,omitempty"`
	CgroupPath    string `json:"cgroup_path,omitempty"`
	NodeName      string `json:"node_name,omitempty"`
}

// CollectorStats represents statistics for the systemd-api collector
type CollectorStats struct {
	// Event counts
	EntriesProcessed int64 `json:"entries_processed"`
	EntriesDropped   int64 `json:"entries_dropped"`
	ErrorsTotal      int64 `json:"errors_total"`

	// Journal stats
	JournalPosition   uint64  `json:"journal_position"`
	JournalConnected  bool    `json:"journal_connected"`
	JournalSize       int64   `json:"journal_size"`
	BufferUtilization float64 `json:"buffer_utilization"`

	// Performance metrics
	ProcessingLatency float64 `json:"processing_latency_ms"`
	EventRate         float64 `json:"event_rate_per_sec"`
	BytesProcessed    int64   `json:"bytes_processed"`

	// Health metrics
	LastActivity      time.Time `json:"last_activity"`
	UptimeSeconds     int64     `json:"uptime_seconds"`
	ConnectionRetries int64     `json:"connection_retries"`
}

// Priority levels mapping to domain types
var PriorityMap = map[sdjournal.Priority]domain.EventPriority{
	sdjournal.PriEmerg:   domain.PriorityCritical,
	sdjournal.PriAlert:   domain.PriorityCritical,
	sdjournal.PriCrit:    domain.PriorityCritical,
	sdjournal.PriErr:     domain.PriorityHigh,
	sdjournal.PriWarning: domain.PriorityNormal,
	sdjournal.PriNotice:  domain.PriorityNormal,
	sdjournal.PriInfo:    domain.PriorityLow,
	sdjournal.PriDebug:   domain.PriorityLow,
}

// GetDomainPriority converts systemd priority to domain priority
func GetDomainPriority(priority sdjournal.Priority) domain.EventPriority {
	if domainPriority, exists := PriorityMap[priority]; exists {
		return domainPriority
	}
	return domain.PriorityNormal
}

// GetEventType determines the event type from journal entry
func (j *JournalEntry) GetEventType() SystemdEventType {
	// Analyze the message and unit to determine event type
	if j.Unit != "" {
		switch j.UnitResult {
		case "success":
			if j.Message != "" {
				if contains(j.Message, "Started") {
					return SystemdEventServiceStart
				}
				if contains(j.Message, "Stopped") {
					return SystemdEventServiceStop
				}
				if contains(j.Message, "Reloaded") {
					return SystemdEventServiceReload
				}
			}
		case "failed":
			return SystemdEventServiceFailed
		}

		// Check for restart patterns
		if contains(j.Message, "Restarting") || contains(j.Message, "restart") {
			return SystemdEventServiceRestart
		}
	}

	// Check for system-level events
	if contains(j.Message, "Boot") || contains(j.Message, "boot") {
		return SystemdEventSystemBoot
	}
	if contains(j.Message, "Shutdown") || contains(j.Message, "shutdown") {
		return SystemdEventSystemShutdown
	}

	// Default to journal entry
	return SystemdEventJournalEntry
}

// GetCorrelationHints extracts correlation hints from journal entry
func (j *JournalEntry) GetCorrelationHints() domain.CorrelationHints {
	hints := domain.CorrelationHints{
		ProcessID:  j.PID,
		NodeName:   j.Hostname,
		CgroupPath: j.CgroupPath,
	}

	// Extract container ID from cgroup path if available
	if j.CgroupPath != "" {
		if containerID := extractContainerIDFromCgroup(j.CgroupPath); containerID != "" {
			hints.ContainerID = containerID
		}
	}

	// Add correlation tags for systemd-specific fields
	hints.CorrelationTags = make(map[string]string)
	if j.Unit != "" {
		hints.CorrelationTags["systemd_unit"] = j.Unit
	}
	if j.InvocationID != "" {
		hints.CorrelationTags["systemd_invocation"] = j.InvocationID
	}
	if j.MachineID != "" {
		hints.CorrelationTags["machine_id"] = j.MachineID
	}
	if j.BootID != "" {
		hints.CorrelationTags["boot_id"] = j.BootID
	}

	return hints
}

// ToCollectorEvent converts SystemdEventData to domain.CollectorEvent
func (s *SystemdEventData) ToCollectorEvent(collectorName string) *domain.CollectorEvent {
	// Create the structured systemd data
	systemdData := &domain.SystemdData{
		EventType:     string(s.EventType),
		Source:        s.Source,
		UnitName:      s.UnitName,
		UnitType:      s.UnitType,
		UnitState:     s.UnitState,
		SubState:      s.SubState,
		ErrorCode:     s.ErrorCode,
		ErrorMessage:  s.ErrorMessage,
		ExitCode:      s.ExitCode,
		Signal:        s.Signal,
		Duration:      s.Duration,
		MemoryUsage:   s.MemoryUsage,
		CPUUsage:      s.CPUUsage,
		RestartCount:  s.RestartCount,
		ProcessID:     s.ProcessID,
		ContainerID:   s.ContainerID,
		PodName:       s.PodName,
		KubernetesUID: s.KubernetesUID,
		CgroupPath:    s.CgroupPath,
		NodeName:      s.NodeName,
	}

	// Add journal entry data if available
	if s.JournalEntry != nil {
		systemdData.Message = s.JournalEntry.Message
		systemdData.Priority = int32(s.JournalEntry.Priority)
		systemdData.Timestamp = s.JournalEntry.Timestamp
		systemdData.PID = s.JournalEntry.PID
		systemdData.Command = s.JournalEntry.Command
		systemdData.Hostname = s.JournalEntry.Hostname
		systemdData.MachineID = s.JournalEntry.MachineID
		systemdData.BootID = s.JournalEntry.BootID
		systemdData.Transport = s.JournalEntry.Transport
		systemdData.CgroupPath = s.JournalEntry.CgroupPath
		systemdData.ContainerID = s.JournalEntry.ContainerID
		systemdData.SyslogID = s.JournalEntry.SyslogID
		systemdData.Fields = s.JournalEntry.Fields
		systemdData.ExtraFields = s.JournalEntry.ExtraData
	}

	event := &domain.CollectorEvent{
		EventID:   generateEventID(),
		Timestamp: time.Now(),
		Type:      mapToCollectorEventType(s.EventType),
		Source:    collectorName,

		EventData: domain.EventDataContainer{
			Systemd: systemdData,
		},

		Metadata: domain.EventMetadata{
			Priority:      GetDomainPriority(sdjournal.Priority(s.JournalEntry.Priority)),
			Tags:          []string{"systemd", "journal"},
			SchemaVersion: "v1",
		},

		CollectionContext: domain.CollectionContext{
			CollectorVersion: "1.0.0",
			HostInfo: domain.HostInfo{
				Hostname: s.JournalEntry.Hostname,
			},
		},
	}

	// Add correlation hints if journal entry is available
	if s.JournalEntry != nil {
		event.CorrelationHints = s.JournalEntry.GetCorrelationHints()
	}

	// Add systemd-specific metadata
	if event.Metadata.Labels == nil {
		event.Metadata.Labels = make(map[string]string)
	}

	if s.UnitName != "" {
		event.Metadata.Labels["systemd_unit"] = s.UnitName
		event.Metadata.Labels["systemd_unit_type"] = s.UnitType
	}

	if s.Source != "" {
		event.Metadata.Labels["systemd_source"] = s.Source
	}

	return event
}

// Helper functions

func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			(len(s) > len(substr) &&
				(s[:len(substr)] == substr ||
					s[len(s)-len(substr):] == substr ||
					indexOfSubstring(s, substr) >= 0)))
}

func indexOfSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func extractContainerIDFromCgroup(cgroupPath string) string {
	// Extract container ID from typical K8s cgroup paths
	// Example: /kubepods/burstable/pod.../12345abcdef...
	// This is a simplified extraction - production would use regex
	if len(cgroupPath) > 64 {
		parts := splitString(cgroupPath, "/")
		for _, part := range parts {
			if len(part) == 64 && isHexString(part) {
				return part
			}
		}
	}
	return ""
}

func splitString(s, sep string) []string {
	if s == "" {
		return []string{}
	}

	parts := []string{}
	start := 0
	sepLen := len(sep)

	for i := 0; i <= len(s)-sepLen; i++ {
		if s[i:i+sepLen] == sep {
			parts = append(parts, s[start:i])
			start = i + sepLen
		}
	}
	parts = append(parts, s[start:])

	return parts
}

func isHexString(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return len(s) > 0
}

func mapToCollectorEventType(systemdType SystemdEventType) domain.CollectorEventType {
	// Map systemd events to domain collector event types
	switch systemdType {
	case SystemdEventServiceStart, SystemdEventServiceStop, SystemdEventServiceRestart:
		return domain.EventTypeSystemdService
	case SystemdEventServiceFailed, SystemdEventUnitFailed, SystemdEventUnitActive, SystemdEventUnitInactive:
		return domain.EventTypeSystemdUnit
	case SystemdEventSystemBoot, SystemdEventSystemShutdown:
		return domain.EventTypeSystemdSystem
	case SystemdEventJournalEntry, SystemdEventJournalError:
		return domain.EventTypeSystemdJournal
	default:
		return domain.EventTypeSystemdJournal // Default to journal events
	}
}

func generateEventID() string {
	// Simple event ID generation - in production, use proper UUID
	return "systemd-" + timeToString(time.Now().UnixNano())
}

func timeToString(t int64) string {
	// Convert timestamp to string representation
	chars := "0123456789abcdef"
	result := ""
	for t > 0 {
		result = string(chars[t%16]) + result
		t = t / 16
	}
	if result == "" {
		result = "0"
	}
	return result
}

// mustMarshalJSON marshals any type to JSON, returning empty object on error
// Uses generics instead of interface{} for type safety
func mustMarshalJSON[T any](v T) []byte {
	// Use proper JSON marshaling with generic type
	data, err := json.Marshal(v)
	if err != nil {
		// Fallback to empty object on error
		return []byte("{}")
	}
	return data
}
