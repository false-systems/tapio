package core

import (
	"fmt"
	"time"
)

// CollectorError represents a journald collector error
type CollectorError struct {
	Type      ErrorType
	Message   string
	Cause     error
	Timestamp time.Time
	Context   map[string]interface{}
}

// ErrorType categorizes collector errors
type ErrorType string

const (
	ErrorTypeJournal     ErrorType = "journal"
	ErrorTypeRead        ErrorType = "read"
	ErrorTypeCursor      ErrorType = "cursor"
	ErrorTypeSeek        ErrorType = "seek"
	ErrorTypePermission  ErrorType = "permission"
	ErrorTypeProcess     ErrorType = "process"
	ErrorTypeTimeout     ErrorType = "timeout"
	ErrorTypeUnsupported ErrorType = "unsupported"
)

func (e CollectorError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s error: %s (caused by: %v)", e.Type, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s error: %s", e.Type, e.Message)
}

func (e CollectorError) Unwrap() error {
	return e.Cause
}

// NewCollectorError creates a new collector error
func NewCollectorError(errType ErrorType, message string, cause error) CollectorError {
	return CollectorError{
		Type:      errType,
		Message:   message,
		Cause:     cause,
		Timestamp: time.Now(),
	}
}

// LogFilter defines criteria for filtering log entries
type LogFilter struct {
	// Priority filters
	MinPriority Priority   `json:"min_priority"`
	MaxPriority Priority   `json:"max_priority"`
	Priorities  []Priority `json:"priorities"`

	// Unit filters
	Units        []string `json:"units"`
	ExcludeUnits []string `json:"exclude_units"`

	// Process filters
	PIDs     []int32  `json:"pids"`
	UIDs     []int32  `json:"uids"`
	GIDs     []int32  `json:"gids"`
	Commands []string `json:"commands"`

	// Content filters
	MessageContains []string `json:"message_contains"`
	MessageExcludes []string `json:"message_excludes"`
	IdentifierMatch []string `json:"identifier_match"`

	// Time filters
	Since time.Time `json:"since"`
	Until time.Time `json:"until"`

	// Field filters
	RequiredFields map[string]string `json:"required_fields"`
}

// JournalMetrics tracks metrics for journal reading
type JournalMetrics struct {
	TotalEntries     uint64        `json:"total_entries"`
	FilteredEntries  uint64        `json:"filtered_entries"`
	ProcessedEntries uint64        `json:"processed_entries"`
	BytesRead        uint64        `json:"bytes_read"`
	ReadDuration     time.Duration `json:"read_duration"`
	LastReadTime     time.Time     `json:"last_read_time"`
	AverageEntrySize float64       `json:"average_entry_size"`
	EntriesPerSecond float64       `json:"entries_per_second"`
}

// BootInfo contains information about system boots
type BootInfo struct {
	BootID        string    `json:"boot_id"`
	StartTime     time.Time `json:"start_time"`
	EndTime       time.Time `json:"end_time"`
	MachineID     string    `json:"machine_id"`
	KernelVersion string    `json:"kernel_version"`
}

// FieldMetadata describes a journald field
type FieldMetadata struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // string, int, binary, etc.
	Description string `json:"description"`
	Source      string `json:"source"` // kernel, systemd, application, etc.
	Required    bool   `json:"required"`
}

// MetricType represents different metric types
type MetricType string

const (
	MetricTypeCounter   MetricType = "counter"
	MetricTypeGauge     MetricType = "gauge"
	MetricTypeHistogram MetricType = "histogram"
)

// Metric represents a collector metric
type Metric struct {
	Name      string            `json:"name"`
	Type      MetricType        `json:"type"`
	Value     float64           `json:"value"`
	Labels    map[string]string `json:"labels,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
	Unit      string            `json:"unit,omitempty"`
	Help      string            `json:"help,omitempty"`
}

// JournalState represents the current state of the journal
type JournalState struct {
	Open          bool      `json:"open"`
	CurrentCursor string    `json:"current_cursor"`
	EntriesRead   uint64    `json:"entries_read"`
	LastReadTime  time.Time `json:"last_read_time"`
	BootID        string    `json:"boot_id"`
	MachineID     string    `json:"machine_id"`
	Position      Position  `json:"position"`
}

// Position represents position in the journal
type Position struct {
	Cursor        string    `json:"cursor"`
	Timestamp     time.Time `json:"timestamp"`
	MonotonicTime uint64    `json:"monotonic_time"`
	BootID        string    `json:"boot_id"`
}

// Common journald field names
const (
	// Message fields
	FieldMessage    = "MESSAGE"
	FieldMessageID  = "MESSAGE_ID"
	FieldPriority   = "PRIORITY"
	FieldFacility   = "SYSLOG_FACILITY"
	FieldIdentifier = "SYSLOG_IDENTIFIER"
	FieldPID        = "SYSLOG_PID"
	FieldTag        = "SYSLOG_TAG"

	// Process fields
	FieldProcessPID     = "_PID"
	FieldProcessUID     = "_UID"
	FieldProcessGID     = "_GID"
	FieldProcessComm    = "_COMM"
	FieldProcessExe     = "_EXE"
	FieldProcessCmdline = "_CMDLINE"
	FieldProcessCgroup  = "_SYSTEMD_CGROUP"

	// System fields
	FieldHostname       = "_HOSTNAME"
	FieldMachineID      = "_MACHINE_ID"
	FieldBootID         = "_BOOT_ID"
	FieldTransport      = "_TRANSPORT"
	FieldStreamID       = "_STREAM_ID"
	FieldSelinuxContext = "_SELINUX_CONTEXT"

	// systemd fields
	FieldSystemdUnit     = "_SYSTEMD_UNIT"
	FieldSystemdUserUnit = "_SYSTEMD_USER_UNIT"
	FieldSystemdSlice    = "_SYSTEMD_SLICE"
	FieldSystemdSession  = "_SYSTEMD_SESSION"
	FieldSystemdOwnerUID = "_SYSTEMD_OWNER_UID"

	// Timing fields
	FieldSourceTimestamp    = "_SOURCE_REALTIME_TIMESTAMP"
	FieldRealtimeTimestamp  = "__REALTIME_TIMESTAMP"
	FieldMonotonicTimestamp = "__MONOTONIC_TIMESTAMP"

	// Cursor and position
	FieldCursor = "__CURSOR"
)
