//go:build linux

package storageio

import (
	"fmt"
	"os"
	"runtime"
	"time"
)

// Config represents the configuration for the storage-io collector
type Config struct {
	// Core collector settings
	BufferSize int `json:"buffer_size" yaml:"buffer_size"`

	// Storage I/O monitoring settings
	SlowIOThresholdMs int `json:"slow_io_threshold_ms" yaml:"slow_io_threshold_ms"`

	// Kubernetes-specific monitoring
	MonitoredK8sPaths []string `json:"monitored_k8s_paths" yaml:"monitored_k8s_paths"`

	// eBPF program settings
	EnableEBPF bool `json:"enable_ebpf" yaml:"enable_ebpf"`

	// Sampling and filtering
	SamplingRate  float64 `json:"sampling_rate" yaml:"sampling_rate"`
	MinIOSize     int64   `json:"min_io_size" yaml:"min_io_size"`
	MaxPathLength int     `json:"max_path_length" yaml:"max_path_length"`

	// Correlation settings
	EnableCgroupCorrelation    bool `json:"enable_cgroup_correlation" yaml:"enable_cgroup_correlation"`
	EnableContainerCorrelation bool `json:"enable_container_correlation" yaml:"enable_container_correlation"`

	// Cache and refresh intervals
	MountRefreshInterval time.Duration `json:"mount_refresh_interval" yaml:"mount_refresh_interval"`
	CacheCleanupInterval time.Duration `json:"cache_cleanup_interval" yaml:"cache_cleanup_interval"`
	HealthCheckInterval  time.Duration `json:"health_check_interval" yaml:"health_check_interval"`
	FlushInterval        time.Duration `json:"flush_interval" yaml:"flush_interval"`

	// Performance settings
	MaxSlowEventCache int           `json:"max_slow_event_cache" yaml:"max_slow_event_cache"`
	EventTimeout      time.Duration `json:"event_timeout" yaml:"event_timeout"`

	// VFS probe configuration
	EnableVFSRead       bool `json:"enable_vfs_read" yaml:"enable_vfs_read"`
	EnableVFSWrite      bool `json:"enable_vfs_write" yaml:"enable_vfs_write"`
	EnableVFSFsync      bool `json:"enable_vfs_fsync" yaml:"enable_vfs_fsync"`
	EnableVFSIterateDir bool `json:"enable_vfs_iterate_dir" yaml:"enable_vfs_iterate_dir"`

	// Block I/O monitoring
	EnableBlockIO bool `json:"enable_block_io" yaml:"enable_block_io"`

	// K8s volume type monitoring
	MonitorPVCs       bool `json:"monitor_pvcs" yaml:"monitor_pvcs"`
	MonitorConfigMaps bool `json:"monitor_configmaps" yaml:"monitor_configmaps"`
	MonitorSecrets    bool `json:"monitor_secrets" yaml:"monitor_secrets"`
	MonitorHostPaths  bool `json:"monitor_hostpaths" yaml:"monitor_hostpaths"`
	MonitorEmptyDirs  bool `json:"monitor_emptydirs" yaml:"monitor_emptydirs"`

	// System information for event enrichment
	Hostname      string `json:"hostname" yaml:"hostname"`
	KernelVersion string `json:"kernel_version" yaml:"kernel_version"`
	OSVersion     string `json:"os_version" yaml:"os_version"`
	Architecture  string `json:"architecture" yaml:"architecture"`

	// Advanced filtering
	ExcludedPaths     []string `json:"excluded_paths" yaml:"excluded_paths"`
	IncludedProcesses []string `json:"included_processes" yaml:"included_processes"`
	ExcludedProcesses []string `json:"excluded_processes" yaml:"excluded_processes"`

	// Error handling
	MaxRetries          int           `json:"max_retries" yaml:"max_retries"`
	RetryDelay          time.Duration `json:"retry_delay" yaml:"retry_delay"`
	ContinueOnEBPFError bool          `json:"continue_on_ebpf_error" yaml:"continue_on_ebpf_error"`

	// Debug and development
	DebugMode      bool `json:"debug_mode" yaml:"debug_mode"`
	VerboseLogging bool `json:"verbose_logging" yaml:"verbose_logging"`
}

// NewDefaultConfig creates a configuration with sensible defaults for Kubernetes environments
func NewDefaultConfig() *Config {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}

	return &Config{
		// Core settings
		BufferSize:        10000,
		SlowIOThresholdMs: 10,

		// K8s monitoring paths - focused on critical Kubernetes paths
		MonitoredK8sPaths: []string{
			"/var/lib/kubelet/pods/",
			"/var/lib/kubelet/plugins/",
			"/var/lib/docker/containers/",
			"/var/lib/containerd/",
			"/var/log/containers/",
			"/var/log/pods/",
			"/etc/kubernetes/",
			"/var/lib/etcd/",
		},

		// eBPF settings
		EnableEBPF: true,

		// Sampling and filtering
		SamplingRate:  0.1,  // 10% sampling for non-critical paths
		MinIOSize:     4096, // 4KB minimum
		MaxPathLength: 256,

		// Correlation
		EnableCgroupCorrelation:    true,
		EnableContainerCorrelation: true,

		// Intervals
		MountRefreshInterval: 5 * time.Minute,
		CacheCleanupInterval: 1 * time.Minute,
		HealthCheckInterval:  30 * time.Second,
		FlushInterval:        5 * time.Second,

		// Performance
		MaxSlowEventCache: 1000,
		EventTimeout:      30 * time.Second,

		// VFS probes - enable critical ones for Phase 1
		EnableVFSRead:       true,
		EnableVFSWrite:      true,
		EnableVFSFsync:      true,
		EnableVFSIterateDir: true,

		// Block I/O monitoring
		EnableBlockIO: true,

		// K8s volume monitoring - enable all for comprehensive monitoring
		MonitorPVCs:       true,
		MonitorConfigMaps: true,
		MonitorSecrets:    true,
		MonitorHostPaths:  true,
		MonitorEmptyDirs:  true,

		// System info
		Hostname:      hostname,
		KernelVersion: getKernelVersion(),
		OSVersion:     getOSVersion(),
		Architecture:  runtime.GOARCH,

		// Filtering
		ExcludedPaths: []string{
			"/proc/",
			"/sys/",
			"/dev/",
			"/tmp/.X11-unix/",
		},
		ExcludedProcesses: []string{
			"kthreadd",
			"ksoftirqd",
			"rcu_",
			"watchdog",
		},

		// Error handling
		MaxRetries:          3,
		RetryDelay:          1 * time.Second,
		ContinueOnEBPFError: false,

		// Debug
		DebugMode:      false,
		VerboseLogging: false,
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.BufferSize <= 0 {
		return fmt.Errorf("buffer_size must be positive, got %d", c.BufferSize)
	}

	if c.BufferSize > 100000 {
		return fmt.Errorf("buffer_size too large (max 100000), got %d", c.BufferSize)
	}

	if c.SlowIOThresholdMs <= 0 {
		return fmt.Errorf("slow_io_threshold_ms must be positive, got %d", c.SlowIOThresholdMs)
	}

	if c.SlowIOThresholdMs > 10000 {
		return fmt.Errorf("slow_io_threshold_ms too large (max 10000ms), got %d", c.SlowIOThresholdMs)
	}

	if c.SamplingRate < 0 || c.SamplingRate > 1.0 {
		return fmt.Errorf("sampling_rate must be between 0 and 1.0, got %f", c.SamplingRate)
	}

	if c.MinIOSize < 0 {
		return fmt.Errorf("min_io_size must be non-negative, got %d", c.MinIOSize)
	}

	if c.MaxPathLength <= 0 || c.MaxPathLength > 1024 {
		return fmt.Errorf("max_path_length must be between 1 and 1024, got %d", c.MaxPathLength)
	}

	if c.MountRefreshInterval <= 0 {
		return fmt.Errorf("mount_refresh_interval must be positive")
	}

	if c.CacheCleanupInterval <= 0 {
		return fmt.Errorf("cache_cleanup_interval must be positive")
	}

	if c.HealthCheckInterval <= 0 {
		return fmt.Errorf("health_check_interval must be positive")
	}

	if c.MaxSlowEventCache <= 0 {
		return fmt.Errorf("max_slow_event_cache must be positive, got %d", c.MaxSlowEventCache)
	}

	if c.EventTimeout <= 0 {
		return fmt.Errorf("event_timeout must be positive")
	}

	if c.MaxRetries < 0 {
		return fmt.Errorf("max_retries must be non-negative, got %d", c.MaxRetries)
	}

	if c.RetryDelay <= 0 {
		return fmt.Errorf("retry_delay must be positive")
	}

	// Validate at least one probe is enabled
	if !c.EnableVFSRead && !c.EnableVFSWrite && !c.EnableVFSFsync && !c.EnableVFSIterateDir && !c.EnableBlockIO {
		return fmt.Errorf("at least one probe (VFS or Block I/O) must be enabled")
	}

	// Validate at least one K8s volume type is monitored
	if !c.MonitorPVCs && !c.MonitorConfigMaps && !c.MonitorSecrets && !c.MonitorHostPaths && !c.MonitorEmptyDirs {
		return fmt.Errorf("at least one K8s volume type must be monitored")
	}

	// Validate K8s paths are absolute
	for _, path := range c.MonitoredK8sPaths {
		if len(path) == 0 {
			return fmt.Errorf("monitored K8s path cannot be empty")
		}
		if path[0] != '/' {
			return fmt.Errorf("monitored K8s path must be absolute: %s", path)
		}
	}

	// Validate excluded paths are absolute
	for _, path := range c.ExcludedPaths {
		if len(path) == 0 {
			return fmt.Errorf("excluded path cannot be empty")
		}
		if path[0] != '/' {
			return fmt.Errorf("excluded path must be absolute: %s", path)
		}
	}

	return nil
}

// GetEnabledVFSProbes returns a list of enabled VFS probe types
func (c *Config) GetEnabledVFSProbes() []VFSProbeType {
	var probes []VFSProbeType

	if c.EnableVFSRead {
		probes = append(probes, VFSProbeRead)
	}
	if c.EnableVFSWrite {
		probes = append(probes, VFSProbeWrite)
	}
	if c.EnableVFSFsync {
		probes = append(probes, VFSProbeFsync)
	}
	if c.EnableVFSIterateDir {
		probes = append(probes, VFSProbeIterateDir)
	}

	return probes
}

// GetMonitoredVolumeTypes returns a list of monitored K8s volume types
func (c *Config) GetMonitoredVolumeTypes() []K8sVolumeType {
	var types []K8sVolumeType

	if c.MonitorPVCs {
		types = append(types, K8sVolumePVC)
	}
	if c.MonitorConfigMaps {
		types = append(types, K8sVolumeConfigMap)
	}
	if c.MonitorSecrets {
		types = append(types, K8sVolumeSecret)
	}
	if c.MonitorHostPaths {
		types = append(types, K8sVolumeHostPath)
	}
	if c.MonitorEmptyDirs {
		types = append(types, K8sVolumeEmptyDir)
	}

	return types
}

// ShouldExcludePath checks if a path should be excluded from monitoring
func (c *Config) ShouldExcludePath(path string) bool {
	for _, excludedPath := range c.ExcludedPaths {
		if len(path) >= len(excludedPath) && path[:len(excludedPath)] == excludedPath {
			return true
		}
	}
	return false
}

// ShouldExcludeProcess checks if a process should be excluded from monitoring
func (c *Config) ShouldExcludeProcess(comm string) bool {
	for _, excludedProcess := range c.ExcludedProcesses {
		if comm == excludedProcess {
			return true
		}
		// Support prefix matching for kernel threads
		if len(comm) >= len(excludedProcess) && comm[:len(excludedProcess)] == excludedProcess {
			return true
		}
	}
	return false
}

// ShouldIncludeProcess checks if a process should be included (if include list is configured)
func (c *Config) ShouldIncludeProcess(comm string) bool {
	// If no include list is configured, include by default
	if len(c.IncludedProcesses) == 0 {
		return true
	}

	for _, includedProcess := range c.IncludedProcesses {
		if comm == includedProcess {
			return true
		}
	}
	return false
}

// GetEffectiveSamplingRate returns the effective sampling rate for a given path
func (c *Config) GetEffectiveSamplingRate(path string, isK8sVolume bool, isSlowIO bool) float64 {
	// Always sample slow I/O events
	if isSlowIO {
		return SlowIOSamplingRate
	}

	// Always sample K8s volume events
	if isK8sVolume {
		return K8sSamplingRate
	}

	// Use configured sampling rate for other paths
	return c.SamplingRate
}

// Helper functions for system information

func getKernelVersion() string {
	// Try to read kernel version from /proc/version
	if data, err := os.ReadFile("/proc/version"); err == nil {
		version := string(data)
		if len(version) > 0 {
			return version[:min(len(version), 100)] // Truncate to reasonable length
		}
	}
	return "unknown"
}

func getOSVersion() string {
	// Try to read OS version from /etc/os-release
	if data, err := os.ReadFile("/etc/os-release"); err == nil {
		osRelease := string(data)
		if len(osRelease) > 0 {
			return osRelease[:min(len(osRelease), 200)] // Truncate to reasonable length
		}
	}
	return "unknown"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ConfigValidationError represents a configuration validation error
type ConfigValidationError struct {
	Field   string
	Value   interface{}
	Message string
}

func (e ConfigValidationError) Error() string {
	return fmt.Sprintf("config validation error for field '%s' with value '%v': %s", e.Field, e.Value, e.Message)
}

// NewConfigValidationError creates a new configuration validation error
func NewConfigValidationError(field string, value interface{}, message string) *ConfigValidationError {
	return &ConfigValidationError{
		Field:   field,
		Value:   value,
		Message: message,
	}
}
