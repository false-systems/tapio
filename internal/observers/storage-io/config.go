package storageio

import "time"

// Config holds configuration for storage I/O observer
type Config struct {
	// Observer name
	Name string

	// eBPF configuration
	EnableEBPF     bool
	RingBufferSize int
	BufferSize     int

	// Monitoring thresholds
	SlowIOThresholdMs     int    // Threshold for slow I/O operations
	BlockingIOThresholdMs int    // Threshold for blocking I/O
	RateLimitNs           uint64 // Rate limiting between events (nanoseconds)

	// Kubernetes paths to monitor
	MonitoredK8sPaths []string

	// Feature flags
	EnableK8sIntegration bool
	EnableMetrics        bool
	EnableProfiling      bool

	// Performance tuning
	MaxEventsPerSecond   int
	CacheCleanupInterval time.Duration
	EventChannelSize     int

	// Filtering
	FilterPIDs    []uint32 // Optional: only monitor these PIDs
	FilterCgroups []uint64 // Optional: only monitor these cgroup IDs
	ExcludePaths  []string // Paths to exclude from monitoring
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		Name:                  "storage-io-observer",
		EnableEBPF:            true,
		RingBufferSize:        8 * 1024 * 1024, // 8MB
		BufferSize:            10000,
		SlowIOThresholdMs:     100,
		BlockingIOThresholdMs: 1000,
		RateLimitNs:           1_000_000, // 1ms between events
		MonitoredK8sPaths: []string{
			"/var/lib/kubelet/pods",
			"/var/lib/docker/volumes",
			"/var/lib/containerd/io.containerd.snapshotter.v1.overlayfs",
			"/var/lib/rancher",
			"/var/lib/docker/overlay2",
		},
		EnableK8sIntegration: true,
		EnableMetrics:        true,
		EnableProfiling:      false,
		MaxEventsPerSecond:   1000,
		CacheCleanupInterval: 5 * time.Minute,
		EventChannelSize:     10000,
		FilterPIDs:           []uint32{},
		FilterCgroups:        []uint64{},
		ExcludePaths: []string{
			"/proc",
			"/sys",
			"/dev",
		},
	}
}

// Validate checks if configuration is valid
func (c *Config) Validate() error {
	if c.SlowIOThresholdMs <= 0 {
		c.SlowIOThresholdMs = 100
	}
	if c.BlockingIOThresholdMs <= c.SlowIOThresholdMs {
		c.BlockingIOThresholdMs = c.SlowIOThresholdMs * 10
	}
	if c.RingBufferSize < 1024*1024 {
		c.RingBufferSize = 8 * 1024 * 1024
	}
	if c.BufferSize < 100 {
		c.BufferSize = 10000
	}
	if c.EventChannelSize < 100 {
		c.EventChannelSize = 10000
	}
	if c.MaxEventsPerSecond <= 0 {
		c.MaxEventsPerSecond = 1000
	}
	if c.CacheCleanupInterval < time.Minute {
		c.CacheCleanupInterval = 5 * time.Minute
	}
	if c.RateLimitNs < 100_000 { // Minimum 100µs
		c.RateLimitNs = 1_000_000
	}
	return nil
}
