package systemdapi

import (
	"time"

	"github.com/coreos/go-systemd/v22/sdjournal"
)

// Config configures the systemd-api collector for journal reading
type Config struct {
	// Core Configuration
	Name       string        `json:"name"`
	BufferSize int           `json:"buffer_size"`
	FlushBytes int           `json:"flush_bytes"`
	MaxEntries int           `json:"max_entries"`
	Timeout    time.Duration `json:"timeout"`

	// Journal Configuration
	JournalPath string             `json:"journal_path"` // Path to journal files, empty for default
	BootID      string             `json:"boot_id"`      // Filter by boot ID, empty for current boot
	Priority    sdjournal.Priority `json:"priority"`     // Minimum priority level
	Since       time.Duration      `json:"since"`        // How far back to read on startup
	FollowMode  bool               `json:"follow_mode"`  // Follow journal tail (true) or read historical (false)
	Seeked      bool               `json:"seeked"`       // Whether we've sought to a position
	Units       []string           `json:"units"`        // Systemd units to monitor
	Matches     []sdjournal.Match  `json:"matches"`      // Additional journal matches
	Fields      []string           `json:"fields"`       // Journal fields to include

	// Health and Performance
	HealthCheckInterval time.Duration `json:"health_check_interval"`
	MetricsEnabled      bool          `json:"metrics_enabled"`
	MaxRetries          int           `json:"max_retries"`
	RetryDelay          time.Duration `json:"retry_delay"`

	// Rate Limiting
	EventRate    int           `json:"event_rate"`     // Max events per second, 0 for unlimited
	BurstSize    int           `json:"burst_size"`     // Burst size for rate limiting
	RateLimitTTL time.Duration `json:"rate_limit_ttl"` // Rate limit reset interval

	// Filtering
	IncludeSystem bool `json:"include_system"` // Include system-wide events
	IncludeUser   bool `json:"include_user"`   // Include user session events
}

// DefaultConfig returns a production-ready configuration for Kubernetes environments
func DefaultConfig() Config {
	return Config{
		Name:       "systemd-api",
		BufferSize: 10000,
		FlushBytes: 1048576, // 1MB
		MaxEntries: 1000,
		Timeout:    time.Second * 30,

		// Journal settings optimized for Kubernetes
		JournalPath: "",               // Use system default
		BootID:      "",               // Current boot only
		Priority:    sdjournal.PriErr, // ERROR level and above
		Since:       0,                // Start from tail
		FollowMode:  true,             // Real-time monitoring
		Seeked:      false,

		// Critical Kubernetes services to monitor
		Units: []string{
			"kubelet.service",
			"containerd.service",
			"docker.service",
			"systemd-resolved.service",
			"kube-proxy.service",
		},

		// Essential journal fields for correlation
		Fields: []string{
			"MESSAGE",
			"PRIORITY",
			"_PID",
			"_COMM",
			"_SYSTEMD_UNIT",
			"_HOSTNAME",
			"_TRANSPORT",
			"SYSLOG_IDENTIFIER",
			"_SYSTEMD_CGROUP",
			"_MACHINE_ID",
			"_BOOT_ID",
			"_SOURCE_REALTIME_TIMESTAMP",
		},

		// Health and performance settings
		HealthCheckInterval: time.Minute * 1,
		MetricsEnabled:      true,
		MaxRetries:          3,
		RetryDelay:          time.Second * 5,

		// Rate limiting to prevent overwhelming downstream
		EventRate:    1000, // 1000 events/sec max
		BurstSize:    100,  // Allow 100 event bursts
		RateLimitTTL: time.Second * 1,

		// Include both system and user events for comprehensive monitoring
		IncludeSystem: true,
		IncludeUser:   false, // Only system for K8s nodes
	}
}

// TestConfig returns a minimal configuration for testing
func TestConfig() Config {
	config := DefaultConfig()
	config.Name = "systemd-api-test"
	config.BufferSize = 100
	config.MaxEntries = 50
	config.Timeout = time.Second * 5
	config.HealthCheckInterval = time.Second * 10
	config.Units = []string{"test.service"} // Minimal test unit
	config.EventRate = 100                  // Lower rate for tests
	config.BurstSize = 10
	config.Fields = []string{"MESSAGE", "PRIORITY", "_SYSTEMD_UNIT"} // Minimal fields
	return config
}

// DevelopmentConfig returns configuration suitable for development
func DevelopmentConfig() Config {
	config := DefaultConfig()
	config.Name = "systemd-api-dev"
	config.BufferSize = 1000
	config.Priority = sdjournal.PriWarning // Include warnings in dev
	config.HealthCheckInterval = time.Second * 30
	config.EventRate = 500 // Moderate rate for development
	config.BurstSize = 50
	return config
}

// Validate checks configuration validity
func (c *Config) Validate() error {
	if c.Name == "" {
		c.Name = "systemd-api"
	}

	if c.BufferSize <= 0 {
		c.BufferSize = 1000
	}

	if c.MaxEntries <= 0 {
		c.MaxEntries = 100
	}

	if c.Timeout <= 0 {
		c.Timeout = time.Second * 30
	}

	if c.HealthCheckInterval <= 0 {
		c.HealthCheckInterval = time.Minute
	}

	if c.MaxRetries < 0 {
		c.MaxRetries = 3
	}

	if c.RetryDelay <= 0 {
		c.RetryDelay = time.Second * 5
	}

	if c.EventRate < 0 {
		c.EventRate = 0 // Unlimited
	}

	if c.BurstSize <= 0 {
		c.BurstSize = 10
	}

	if c.RateLimitTTL <= 0 {
		c.RateLimitTTL = time.Second
	}

	// Ensure we have some fields to extract
	if len(c.Fields) == 0 {
		c.Fields = []string{"MESSAGE", "PRIORITY", "_SYSTEMD_UNIT"}
	}

	return nil
}

// GetJournalMatches returns journal matches for the configuration
func (c *Config) GetJournalMatches() []sdjournal.Match {
	matches := make([]sdjournal.Match, 0, len(c.Units)+len(c.Matches))

	// Add unit matches
	for _, unit := range c.Units {
		matches = append(matches, sdjournal.Match{
			Field: "_SYSTEMD_UNIT",
			Value: unit,
		})
	}

	// Add custom matches
	matches = append(matches, c.Matches...)

	return matches
}

// ShouldIncludePriority checks if a priority level should be included
func (c *Config) ShouldIncludePriority(priority sdjournal.Priority) bool {
	return priority <= c.Priority
}

// GetSeekPosition returns the position to seek to in the journal
func (c *Config) GetSeekPosition() (time.Time, bool) {
	if c.Since > 0 {
		return time.Now().Add(-c.Since), true
	}
	return time.Time{}, false
}
