package journald

import (
	"context"
	"time"
)

// JournalEntry represents a systemd journal entry
type JournalEntry struct {
	// Core fields
	RealtimeTimestamp  int64  `json:"__REALTIME_TIMESTAMP"`
	MonotonicTimestamp int64  `json:"__MONOTONIC_TIMESTAMP"`
	Message            string `json:"MESSAGE"`
	Priority           int    `json:"PRIORITY"`

	// Process information
	PID     int    `json:"_PID"`
	UID     int    `json:"_UID"`
	GID     int    `json:"_GID"`
	Comm    string `json:"_COMM"`
	Exe     string `json:"_EXE"`
	CmdLine string `json:"_CMDLINE"`

	// Systemd fields
	SystemdUnit     string `json:"_SYSTEMD_UNIT"`
	SystemdSlice    string `json:"_SYSTEMD_SLICE"`
	SystemdCGroup   string `json:"_SYSTEMD_CGROUP"`
	SystemdSession  string `json:"_SYSTEMD_SESSION"`
	SystemdOwnerUID string `json:"_SYSTEMD_OWNER_UID"`

	// Source information
	SyslogIdentifier string `json:"SYSLOG_IDENTIFIER"`
	SyslogFacility   string `json:"SYSLOG_FACILITY"`
	SyslogPID        string `json:"SYSLOG_PID"`

	// Host information
	Hostname  string `json:"_HOSTNAME"`
	MachineID string `json:"_MACHINE_ID"`
	BootID    string `json:"_BOOT_ID"`

	// Additional fields
	Transport      string `json:"_TRANSPORT"`
	SELinuxContext string `json:"_SELINUX_CONTEXT"`
	Cursor         string `json:"__CURSOR"`

	// Container fields (if applicable)
	ContainerName string `json:"CONTAINER_NAME"`
	ContainerID   string `json:"CONTAINER_ID"`
	ContainerTag  string `json:"CONTAINER_TAG"`
	ImageName     string `json:"IMAGE_NAME"`
}

// JournaldConfig contains configuration for the journald collector
type JournaldConfig struct {
	// Collection settings
	FollowCursor bool          `json:"follow_cursor"`
	MaxAge       time.Duration `json:"max_age"`
	Since        string        `json:"since"`
	Until        string        `json:"until"`

	// Filtering
	Units           []string `json:"units"`
	Priorities      []string `json:"priorities"`
	MatchPatterns   []string `json:"match_patterns"`
	ExcludePatterns []string `json:"exclude_patterns"`

	// Performance tuning
	StreamBatchSize  int `json:"stream_batch_size"`
	MaxEntriesPerSec int `json:"max_entries_per_sec"`

	// Noise reduction
	FilterNoisyUnits bool    `json:"filter_noisy_units"`
	NoiseReduction   float64 `json:"noise_reduction_target"` // Target percentage (e.g., 0.95 for 95%)

	// Advanced options
	OutputMode string   `json:"output_mode"` // json, short, verbose
	ExtraArgs  []string `json:"extra_args"`
}

// Reader represents the journald reader interface
type Reader struct {
	entries   chan *JournalEntry
	errors    chan error
	startTime time.Time
	healthy   bool
	cursor    string
}

// NewReader creates a new journald reader
func NewReader(config *JournaldConfig) *Reader {
	return &Reader{
		entries: make(chan *JournalEntry, 1000),
		errors:  make(chan error, 10),
		healthy: true,
	}
}

// Start begins reading from journald
func (r *Reader) Start(ctx context.Context) error {
	r.startTime = time.Now()
	// Implementation would start journalctl process
	return nil
}

// Stop halts the reader
func (r *Reader) Stop() error {
	close(r.entries)
	close(r.errors)
	return nil
}

// Entries returns the entry channel
func (r *Reader) Entries() <-chan *JournalEntry {
	return r.entries
}

// Errors returns the error channel
func (r *Reader) Errors() <-chan error {
	return r.errors
}

// IsHealthy returns reader health status
func (r *Reader) IsHealthy() bool {
	return r.healthy
}

// GetStartTime returns when the reader started
func (r *Reader) GetStartTime() time.Time {
	return r.startTime
}

// GetCursor returns the current journal cursor position
func (r *Reader) GetCursor() string {
	return r.cursor
}

// Priority levels for journald
const (
	PriorityEmergency = 0
	PriorityAlert     = 1
	PriorityCritical  = 2
	PriorityError     = 3
	PriorityWarning   = 4
	PriorityNotice    = 5
	PriorityInfo      = 6
	PriorityDebug     = 7
)

// Common systemd units we care about
var ImportantSystemdUnits = []string{
	"kubelet.service",
	"docker.service",
	"containerd.service",
	"crio.service",
	"etcd.service",
	"kube-apiserver.service",
	"kube-controller-manager.service",
	"kube-scheduler.service",
	"kube-proxy.service",
	"coredns.service",
	"calico-node.service",
	"flannel.service",
	"weave.service",
}

// Common noisy units to filter
var NoisySystemdUnits = []string{
	"systemd-logind.service",
	"systemd-resolved.service",
	"systemd-timesyncd.service",
	"NetworkManager.service",
	"ModemManager.service",
	"snapd.service",
	"packagekit.service",
	"polkit.service",
	"accounts-daemon.service",
	"udisks2.service",
	"avahi-daemon.service",
	"bluetooth.service",
	"cups.service",
}
