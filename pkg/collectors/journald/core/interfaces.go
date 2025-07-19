package core

import (
	"context"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// Collector defines the interface for journald log collection
type Collector interface {
	// Lifecycle management
	Start(ctx context.Context) error
	Stop() error

	// Event streaming
	Events() <-chan domain.Event

	// Health and monitoring
	Health() Health
	Statistics() Statistics

	// Configuration
	Configure(config Config) error
}

// PlatformImpl defines the platform-specific interface
type PlatformImpl interface {
	init(config Config) error
	start(ctx context.Context) error
	stop() error
	reader() LogReader
	isOpen() bool
	bootID() string
	machineID() string
	currentCursor() string
}

// Config defines journald collector configuration
type Config struct {
	// Basic settings
	Name            string `json:"name"`
	Enabled         bool   `json:"enabled"`
	EventBufferSize int    `json:"event_buffer_size"`

	// Journal reading configuration
	FollowMode  bool          `json:"follow_mode"`  // Real-time vs batch mode
	SeekToEnd   bool          `json:"seek_to_end"`  // Start from end vs beginning
	MaxEntries  int           `json:"max_entries"`  // Max entries per read
	ReadTimeout time.Duration `json:"read_timeout"` // Timeout for read operations

	// Filtering configuration
	Units      []string   `json:"units"`      // Filter by systemd units
	Priorities []Priority `json:"priorities"` // Filter by log priorities
	BootID     string     `json:"boot_id"`    // Filter by boot ID (empty = current)
	Since      time.Time  `json:"since"`      // Start time filter
	Until      time.Time  `json:"until"`      // End time filter

	// Field filtering
	IncludeFields  []string `json:"include_fields"`  // Fields to include in events
	ExcludeFields  []string `json:"exclude_fields"`  // Fields to exclude from events
	RequiredFields []string `json:"required_fields"` // Only include entries with these fields

	// Cursor management
	PersistCursor bool   `json:"persist_cursor"` // Save cursor to disk
	CursorFile    string `json:"cursor_file"`    // File to save cursor
	InitialCursor string `json:"initial_cursor"` // Starting cursor

	// Performance tuning
	BatchSize      int           `json:"batch_size"`       // Entries to process in batch
	FlushInterval  time.Duration `json:"flush_interval"`   // How often to flush cursor
	EventRateLimit int           `json:"event_rate_limit"` // Max events per second
}

// Health represents collector health status
type Health struct {
	Status          HealthStatus       `json:"status"`
	Message         string             `json:"message"`
	LastEventTime   time.Time          `json:"last_event_time"`
	EventsProcessed uint64             `json:"events_processed"`
	EventsDropped   uint64             `json:"events_dropped"`
	ErrorCount      uint64             `json:"error_count"`
	JournalOpen     bool               `json:"journal_open"`
	CurrentCursor   string             `json:"current_cursor"`
	BootID          string             `json:"boot_id"`
	MachineID       string             `json:"machine_id"`
	Metrics         map[string]float64 `json:"metrics"`
}

// HealthStatus represents the health state
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusUnknown   HealthStatus = "unknown"
)

// Statistics represents runtime statistics
type Statistics struct {
	StartTime       time.Time              `json:"start_time"`
	EventsCollected uint64                 `json:"events_collected"`
	EventsDropped   uint64                 `json:"events_dropped"`
	BytesRead       uint64                 `json:"bytes_read"`
	EntriesRead     uint64                 `json:"entries_read"`
	CursorUpdates   uint64                 `json:"cursor_updates"`
	JournalSeeks    uint64                 `json:"journal_seeks"`
	ReadErrors      uint64                 `json:"read_errors"`
	Custom          map[string]interface{} `json:"custom"`
}

// LogReader reads journal entries
type LogReader interface {
	// Open the journal
	Open() error

	// Close the journal
	Close() error

	// Check if journal is open
	IsOpen() bool

	// Read next entry
	ReadEntry() (*LogEntry, error)

	// Seek to cursor
	SeekCursor(cursor string) error

	// Seek to timestamp
	SeekTime(timestamp time.Time) error

	// Get current cursor
	GetCursor() (string, error)

	// Wait for new entries
	WaitForEntries(timeout time.Duration) error

	// Get boot ID
	GetBootID() string

	// Get machine ID
	GetMachineID() string
}

// EventProcessor processes raw journal entries into domain events
type EventProcessor interface {
	ProcessEntry(ctx context.Context, entry *LogEntry) (domain.Event, error)
}

// LogEntry represents a raw journald log entry
type LogEntry struct {
	// Standard journald fields
	Message    string   `json:"MESSAGE"`
	Priority   Priority `json:"PRIORITY"`
	Facility   string   `json:"SYSLOG_FACILITY"`
	Identifier string   `json:"SYSLOG_IDENTIFIER"`
	PID        int32    `json:"_PID"`
	UID        int32    `json:"_UID"`
	GID        int32    `json:"_GID"`
	Comm       string   `json:"_COMM"`
	Exe        string   `json:"_EXE"`
	Cmdline    string   `json:"_CMDLINE"`
	Unit       string   `json:"_SYSTEMD_UNIT"`
	UserUnit   string   `json:"_SYSTEMD_USER_UNIT"`
	Session    string   `json:"_SYSTEMD_SESSION"`
	HostName   string   `json:"_HOSTNAME"`

	// Timestamps
	Timestamp time.Time `json:"_SOURCE_REALTIME_TIMESTAMP"`
	BootID    string    `json:"_BOOT_ID"`
	MachineID string    `json:"_MACHINE_ID"`

	// Cursor and metadata
	Cursor        string `json:"__CURSOR"`
	MonotonicTime uint64 `json:"__MONOTONIC_TIMESTAMP"`

	// All fields (including custom ones)
	Fields map[string]interface{} `json:"fields"`
}

// Priority represents syslog priority levels
type Priority int

const (
	PriorityEmergency Priority = 0 // System is unusable
	PriorityAlert     Priority = 1 // Action must be taken immediately
	PriorityCritical  Priority = 2 // Critical conditions
	PriorityError     Priority = 3 // Error conditions
	PriorityWarning   Priority = 4 // Warning conditions
	PriorityNotice    Priority = 5 // Normal but significant condition
	PriorityInfo      Priority = 6 // Informational messages
	PriorityDebug     Priority = 7 // Debug-level messages
)

// String returns the string representation of the priority
func (p Priority) String() string {
	switch p {
	case PriorityEmergency:
		return "emergency"
	case PriorityAlert:
		return "alert"
	case PriorityCritical:
		return "critical"
	case PriorityError:
		return "error"
	case PriorityWarning:
		return "warning"
	case PriorityNotice:
		return "notice"
	case PriorityInfo:
		return "info"
	case PriorityDebug:
		return "debug"
	default:
		return "unknown"
	}
}

// CursorManager manages journal cursors for reliable log streaming
type CursorManager interface {
	// Save cursor to persistent storage
	SaveCursor(cursor string) error

	// Load cursor from persistent storage
	LoadCursor() (string, error)

	// Check if cursor exists
	HasCursor() bool

	// Clear saved cursor
	ClearCursor() error
}

// Validate validates the configuration
func (c Config) Validate() error {
	if c.EventBufferSize <= 0 {
		c.EventBufferSize = 1000
	}
	if c.MaxEntries <= 0 {
		c.MaxEntries = 1000
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 100
	}
	if c.ReadTimeout <= 0 {
		c.ReadTimeout = 30 * time.Second
	}
	if c.FlushInterval <= 0 {
		c.FlushInterval = 10 * time.Second
	}

	// Default to info and above if no priorities specified
	if len(c.Priorities) == 0 {
		c.Priorities = []Priority{
			PriorityEmergency,
			PriorityAlert,
			PriorityCritical,
			PriorityError,
			PriorityWarning,
			PriorityNotice,
			PriorityInfo,
		}
	}

	return nil
}
