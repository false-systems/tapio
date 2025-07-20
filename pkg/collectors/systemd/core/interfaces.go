package core

import (
	"context"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// Collector defines the interface for systemd event collection
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

// Config defines systemd collector configuration
type Config struct {
	// Basic settings
	Name            string `json:"name"`
	Enabled         bool   `json:"enabled"`
	EventBufferSize int    `json:"event_buffer_size"`

	// Service monitoring configuration
	WatchAllServices bool     `json:"watch_all_services"`
	ServiceFilter    []string `json:"service_filter"`  // List of services to watch
	ServiceExclude   []string `json:"service_exclude"` // List of services to exclude
	UnitTypes        []string `json:"unit_types"`      // Types: service, socket, timer, etc.

	// Event configuration
	WatchServiceStates   bool `json:"watch_service_states"`   // Start/stop/restart
	WatchServiceFailures bool `json:"watch_service_failures"` // Failed states
	WatchServiceReloads  bool `json:"watch_service_reloads"`  // Reload events
	WatchJobQueue        bool `json:"watch_job_queue"`        // systemd job queue

	// Performance tuning
	PollInterval       time.Duration `json:"poll_interval"`
	EventRateLimit     int           `json:"event_rate_limit"`
	DBusTimeout        time.Duration `json:"dbus_timeout"`
	MaxConcurrentWatch int           `json:"max_concurrent_watch"`
}

// Health represents collector health status
type Health struct {
	Status          HealthStatus       `json:"status"`
	Message         string             `json:"message"`
	LastEventTime   time.Time          `json:"last_event_time"`
	EventsProcessed uint64             `json:"events_processed"`
	EventsDropped   uint64             `json:"events_dropped"`
	ErrorCount      uint64             `json:"error_count"`
	DBusConnected   bool               `json:"dbus_connected"`
	SystemdVersion  string             `json:"systemd_version"`
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
	StartTime         time.Time              `json:"start_time"`
	EventsCollected   uint64                 `json:"events_collected"`
	EventsDropped     uint64                 `json:"events_dropped"`
	ServicesMonitored int                    `json:"services_monitored"`
	ActiveServices    int                    `json:"active_services"`
	FailedServices    int                    `json:"failed_services"`
	DBusCallsTotal    uint64                 `json:"dbus_calls_total"`
	DBusErrors        uint64                 `json:"dbus_errors"`
	ReconnectCount    uint64                 `json:"reconnect_count"`
	Custom            map[string]interface{} `json:"custom"`
}

// ServiceWatcher watches systemd services
type ServiceWatcher interface {
	// Start watching services
	Start(ctx context.Context) error

	// Stop watching
	Stop() error

	// Events channel
	Events() <-chan RawEvent

	// Get watched services
	WatchedServices() []string
}

// EventProcessor processes raw systemd events into domain events
type EventProcessor interface {
	ProcessEvent(ctx context.Context, raw RawEvent) (domain.Event, error)
}

// RawEvent represents a raw systemd event
type RawEvent struct {
	Type       EventType              `json:"type"`
	UnitName   string                 `json:"unit_name"`
	UnitType   string                 `json:"unit_type"` // service, socket, timer, etc.
	OldState   string                 `json:"old_state"`
	NewState   string                 `json:"new_state"`
	SubState   string                 `json:"sub_state"` // running, exited, failed, etc.
	Result     string                 `json:"result"`    // success, exit-code, signal, etc.
	MainPID    int32                  `json:"main_pid"`
	ExitCode   int32                  `json:"exit_code"`
	ExitStatus int32                  `json:"exit_status"`
	Timestamp  time.Time              `json:"timestamp"`
	Properties map[string]interface{} `json:"properties"`
}

// EventType represents the type of systemd event
type EventType string

const (
	EventTypeStateChange    EventType = "state_change"
	EventTypeReload         EventType = "reload"
	EventTypeRestart        EventType = "restart"
	EventTypeFailure        EventType = "failure"
	EventTypeStart          EventType = "start"
	EventTypeStop           EventType = "stop"
	EventTypeJobNew         EventType = "job_new"
	EventTypeJobRemoved     EventType = "job_removed"
	EventTypePropertyChange EventType = "property_change"
)

// DBusConnection represents a D-Bus connection interface
type DBusConnection interface {
	// Connect to D-Bus
	Connect() error

	// Disconnect from D-Bus
	Disconnect() error

	// Check if connected
	IsConnected() bool

	// Subscribe to systemd signals
	Subscribe() error

	// Unsubscribe from signals
	Unsubscribe() error

	// Get unit properties
	GetUnitProperties(unitName string) (map[string]interface{}, error)

	// List units
	ListUnits() ([]UnitStatus, error)

	// Get systemd version
	GetSystemdVersion() (string, error)
}

// UnitStatus represents systemd unit status
type UnitStatus struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	LoadState   string `json:"load_state"`
	ActiveState string `json:"active_state"`
	SubState    string `json:"sub_state"`
	UnitPath    string `json:"unit_path"`
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.EventBufferSize <= 0 {
		c.EventBufferSize = 1000
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 5 * time.Second
	}
	if c.DBusTimeout <= 0 {
		c.DBusTimeout = 30 * time.Second
	}
	if c.MaxConcurrentWatch <= 0 {
		c.MaxConcurrentWatch = 100
	}

	// Default unit types if none specified
	if len(c.UnitTypes) == 0 {
		c.UnitTypes = []string{"service"}
	}

	return nil
}
