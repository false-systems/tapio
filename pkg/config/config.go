package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
)

// Config represents the complete Tapio configuration
type Config struct {
	// Global settings
	Version        string        `yaml:"version" json:"version"`
	UpdateInterval time.Duration `yaml:"update_interval" json:"update_interval"`
	LogLevel       string        `yaml:"log_level" json:"log_level"`
	LogFormat      string        `yaml:"log_format" json:"log_format"`

	// Feature flags for easy enablement/disabling
	Features FeaturesConfig `yaml:"features" json:"features"`

	// Component-specific configurations
	EBPF       EBPFConfig       `yaml:"ebpf" json:"ebpf"`
	Kubernetes KubernetesConfig `yaml:"kubernetes" json:"kubernetes"`
	Collector  CollectorConfig  `yaml:"collector" json:"collector"`
	Output     OutputConfig     `yaml:"output" json:"output"`
	Metrics    MetricsConfig    `yaml:"metrics" json:"metrics"`

	// Resource limits and performance tuning
	Resources ResourcesConfig `yaml:"resources" json:"resources"`

	// Advanced settings
	Advanced AdvancedConfig `yaml:"advanced" json:"advanced"`
}

// FeaturesConfig controls which features are enabled
type FeaturesConfig struct {
	EnableEBPF        bool `yaml:"enable_ebpf" json:"enable_ebpf"`
	EnablePrediction  bool `yaml:"enable_prediction" json:"enable_prediction"`
	EnableMetrics     bool `yaml:"enable_metrics" json:"enable_metrics"`
	EnableTracing     bool `yaml:"enable_tracing" json:"enable_tracing"`
	EnableCorrelation bool `yaml:"enable_correlation" json:"enable_correlation"`
}

// EBPFConfig wraps the existing eBPF configuration with additional settings
type EBPFConfig struct {
	// Embed existing eBPF config
	core.Config `yaml:",inline" json:",inline"`

	// Additional Tapio-specific eBPF settings
	AutoDetectCapabilities bool     `yaml:"auto_detect_capabilities" json:"auto_detect_capabilities"`
	FallbackOnFailure      bool     `yaml:"fallback_on_failure" json:"fallback_on_failure"`
	RequiredPrograms       []string `yaml:"required_programs" json:"required_programs"`
	OptionalPrograms       []string `yaml:"optional_programs" json:"optional_programs"`
}

// KubernetesConfig configures Kubernetes connectivity and monitoring
type KubernetesConfig struct {
	Context      string          `yaml:"context" json:"context"`
	Kubeconfig   string          `yaml:"kubeconfig" json:"kubeconfig"`
	InCluster    bool            `yaml:"in_cluster" json:"in_cluster"`
	Namespaces   NamespaceConfig `yaml:"namespaces" json:"namespaces"`
	QPS          float32         `yaml:"qps" json:"qps"`
	Burst        int             `yaml:"burst" json:"burst"`
	Timeout      time.Duration   `yaml:"timeout" json:"timeout"`
	RetryCount   int             `yaml:"retry_count" json:"retry_count"`
	RetryBackoff time.Duration   `yaml:"retry_backoff" json:"retry_backoff"`
}

// NamespaceConfig controls which namespaces to monitor
type NamespaceConfig struct {
	Include  []string `yaml:"include" json:"include"`
	Exclude  []string `yaml:"exclude" json:"exclude"`
	AllowAll bool     `yaml:"allow_all" json:"allow_all"`
	SystemNS bool     `yaml:"system_namespaces" json:"system_namespaces"`
}

// CollectorConfig configures the data collection behavior
type CollectorConfig struct {
	Interval          time.Duration `yaml:"interval" json:"interval"`
	Timeout           time.Duration `yaml:"timeout" json:"timeout"`
	MaxRetries        int           `yaml:"max_retries" json:"max_retries"`
	BackoffMultiplier float64       `yaml:"backoff_multiplier" json:"backoff_multiplier"`
	EnableSystemd     bool          `yaml:"enable_systemd" json:"enable_systemd"`
	EnableJournald    bool          `yaml:"enable_journald" json:"enable_journald"`
	EnableProcessInfo bool          `yaml:"enable_process_info" json:"enable_process_info"`
	EnableNetworkInfo bool          `yaml:"enable_network_info" json:"enable_network_info"`
}

// OutputConfig configures how results are presented
type OutputConfig struct {
	Format         string            `yaml:"format" json:"format"`
	Color          bool              `yaml:"color" json:"color"`
	Verbose        bool              `yaml:"verbose" json:"verbose"`
	ShowTimestamps bool              `yaml:"show_timestamps" json:"show_timestamps"`
	ShowSource     bool              `yaml:"show_source" json:"show_source"`
	Fields         []string          `yaml:"fields" json:"fields"`
	CustomFields   map[string]string `yaml:"custom_fields" json:"custom_fields"`
	PaginationSize int               `yaml:"pagination_size" json:"pagination_size"`
}

// MetricsConfig configures metrics collection and export
type MetricsConfig struct {
	Enabled          bool              `yaml:"enabled" json:"enabled"`
	Port             int               `yaml:"port" json:"port"`
	Path             string            `yaml:"path" json:"path"`
	Interval         time.Duration     `yaml:"interval" json:"interval"`
	Labels           map[string]string `yaml:"labels" json:"labels"`
	HistogramBuckets []float64         `yaml:"histogram_buckets" json:"histogram_buckets"`
}

// ResourcesConfig sets resource limits and performance parameters
type ResourcesConfig struct {
	MaxMemoryUsage      int           `yaml:"max_memory_usage_mb" json:"max_memory_usage_mb"`
	MaxCPUPercent       float64       `yaml:"max_cpu_percent" json:"max_cpu_percent"`
	EventBufferSize     int           `yaml:"event_buffer_size" json:"event_buffer_size"`
	MaxEventsPerSecond  int           `yaml:"max_events_per_second" json:"max_events_per_second"`
	CleanupInterval     time.Duration `yaml:"cleanup_interval" json:"cleanup_interval"`
	ParallelWorkers     int           `yaml:"parallel_workers" json:"parallel_workers"`
}

// AdvancedConfig contains advanced settings for power users
type AdvancedConfig struct {
	DebugMode            bool              `yaml:"debug_mode" json:"debug_mode"`
	ProfilerEnabled      bool              `yaml:"profiler_enabled" json:"profiler_enabled"`
	ProfilerPort         int               `yaml:"profiler_port" json:"profiler_port"`
	ExperimentalFeatures []string          `yaml:"experimental_features" json:"experimental_features"`
	CustomEnvVars        map[string]string `yaml:"custom_env_vars" json:"custom_env_vars"`
	FeatureGates         map[string]bool   `yaml:"feature_gates" json:"feature_gates"`
}

// DefaultConfig returns a sensible default configuration
func DefaultConfig() *Config {
	return &Config{
		Version:        "1.0.0",
		UpdateInterval: 30 * time.Second,
		LogLevel:       "info",
		LogFormat:      "human",

		Features: FeaturesConfig{
			EnableEBPF:        true,
			EnablePrediction:  true,
			EnableMetrics:     true,
			EnableTracing:     false, // Disabled by default to reduce overhead
			EnableCorrelation: true,
		},

		EBPF: EBPFConfig{
			Config: core.Config{
				Enabled:                 true,
				EnableMemory:  true,
				EnableNetwork: true,
			},
			AutoDetectCapabilities: true,
			FallbackOnFailure:      true,
			RequiredPrograms:       []string{"memory_monitor", "basic_network"},
			OptionalPrograms:       []string{"packet_analysis", "protocol_analysis"},
		},

		Kubernetes: KubernetesConfig{
			Context:    "", // Auto-detect from current context
			Kubeconfig: "", // Auto-detect from standard locations
			InCluster:  false,
			Namespaces: NamespaceConfig{
				Include:  []string{},
				Exclude:  []string{"kube-system", "kube-public", "kube-node-lease"},
				AllowAll: false,
				SystemNS: false,
			},
			QPS:          10.0,
			Burst:        20,
			Timeout:      30 * time.Second,
			RetryCount:   3,
			RetryBackoff: 1 * time.Second,
		},

		Collector: CollectorConfig{
			Interval:          5 * time.Second,
			Timeout:           30 * time.Second,
			MaxRetries:        3,
			BackoffMultiplier: 2.0,
			EnableSystemd:     detectSystemd(),
			EnableJournald:    detectJournald(),
			EnableProcessInfo: true,
			EnableNetworkInfo: true,
		},

		Output: OutputConfig{
			Format:         "human",
			Color:          isColorTerminal(),
			Verbose:        false,
			ShowTimestamps: false,
			ShowSource:     false,
			Fields:         []string{"name", "status", "reason", "confidence"},
			CustomFields:   make(map[string]string),
			PaginationSize: 20,
		},

		Metrics: MetricsConfig{
			Enabled:          false, // Disabled by default
			Port:             9090,
			Path:             "/metrics",
			Interval:         30 * time.Second,
			Labels:           make(map[string]string),
			HistogramBuckets: []float64{0.1, 0.5, 1.0, 2.5, 5.0, 10.0},
		},

		Resources: ResourcesConfig{
			MaxMemoryUsage:      512, // 512MB
			MaxCPUPercent:       25.0,
			EventBufferSize:     1024,
			MaxEventsPerSecond:  1000,
			CleanupInterval:     1 * time.Hour,
			ParallelWorkers:     getDefaultWorkerCount(),
		},

		Advanced: AdvancedConfig{
			DebugMode:            false,
			ProfilerEnabled:      false,
			ProfilerPort:         6060,
			ExperimentalFeatures: []string{},
			CustomEnvVars:        make(map[string]string),
			FeatureGates:         make(map[string]bool),
		},
	}
}

// ZeroConfig returns a minimal configuration for zero-config experience
func ZeroConfig() *Config {
	config := DefaultConfig()

	// Override defaults for zero-config experience
	config.Features.EnableEBPF = detectEBPFSupport()
	config.Kubernetes.InCluster = detectInCluster()
	config.Kubernetes.Namespaces.AllowAll = true // Monitor everything by default
	config.Output.Color = isColorTerminal()

	// Reduce resource usage for unknown environments
	config.Resources.MaxMemoryUsage = 256
	config.Resources.MaxCPUPercent = 15.0
	config.EBPF.SamplingInterval = 100 * time.Millisecond // Reduce sampling for performance

	return config
}

// Validate checks if the configuration is valid and provides fix suggestions
func (c *Config) Validate() error {
	var errors []ValidationError

	// Validate log level
	validLogLevels := []string{"debug", "info", "warn", "error", "fatal", "panic", "trace"}
	isValidLogLevel := false
	for _, level := range validLogLevels {
		if c.LogLevel == level {
			isValidLogLevel = true
			break
		}
	}
	if !isValidLogLevel {
		errors = append(errors, ValidationError{
			Field:      "log_level",
			Message:    fmt.Sprintf("invalid log level '%s'", c.LogLevel),
			Suggestion: fmt.Sprintf("use one of: %s", strings.Join(validLogLevels, ", ")),
			FixCommand: fmt.Sprintf("tapio config set log_level info"),
		})
	}

	// Validate resource limits
	if c.Resources.MaxMemoryUsage < 0 {
		errors = append(errors, ValidationError{
			Field:      "resources.max_memory_usage",
			Message:    "memory usage cannot be negative",
			Suggestion: "set to 0 for unlimited or a positive value in MB",
			FixCommand: "tapio config set resources.max_memory_usage 512",
		})
	}

	if c.Resources.MaxCPUPercent < 0 || c.Resources.MaxCPUPercent > 100 {
		errors = append(errors, ValidationError{
			Field:      "resources.max_cpu_percent",
			Message:    "CPU percentage must be between 0 and 100",
			Suggestion: "use 0 for unlimited or 1-100 for percentage limit",
			FixCommand: "tapio config set resources.max_cpu_percent 50",
		})
	}

	// Validate metrics port
	if c.Metrics.Enabled && (c.Metrics.Port < 1 || c.Metrics.Port > 65535) {
		errors = append(errors, ValidationError{
			Field:      "metrics.port",
			Message:    fmt.Sprintf("invalid port number %d", c.Metrics.Port),
			Suggestion: "use a port between 1 and 65535 (e.g., 9090)",
			FixCommand: "tapio config set metrics.port 9090",
		})
	}

	// Check for port conflicts
	if c.Metrics.Enabled && c.Advanced.ProfilerEnabled && c.Metrics.Port == c.Advanced.ProfilerPort {
		errors = append(errors, ValidationError{
			Field:      "metrics.port",
			Message:    fmt.Sprintf("metrics and profiler ports conflict (%d)", c.Metrics.Port),
			Suggestion: "use different ports for metrics and profiler",
			FixCommand: fmt.Sprintf("tapio config set metrics.port %d", c.Metrics.Port+1),
		})
	}

	// Validate retention periods

	// Validate output format
	validFormats := []string{"human", "json", "yaml", "table", "csv"}
	isValidFormat := false
	for _, format := range validFormats {
		if c.Output.Format == format {
			isValidFormat = true
			break
		}
	}
	if !isValidFormat {
		errors = append(errors, ValidationError{
			Field:      "output.format",
			Message:    fmt.Sprintf("invalid output format '%s'", c.Output.Format),
			Suggestion: fmt.Sprintf("use one of: %s", strings.Join(validFormats, ", ")),
			FixCommand: "tapio config set output.format human",
		})
	}

	// Validate namespaces
	if len(c.Kubernetes.Namespaces.Exclude) > 0 {
		for _, ns := range c.Kubernetes.Namespaces.Exclude {
			if ns == "" {
				errors = append(errors, ValidationError{
					Field:      "kubernetes.namespaces.exclude",
					Message:    "empty namespace in exclude list",
					Suggestion: "remove empty entries from namespaces.exclude",
				})
				break
			}
		}
	}

	// Validate Kubernetes configuration
	if c.Kubernetes.Kubeconfig != "" && c.Kubernetes.InCluster {
		errors = append(errors, ValidationError{
			Field:      "kubernetes",
			Message:    "both kubeconfig and in_cluster are set",
			Suggestion: "use either kubeconfig path OR in_cluster mode, not both",
			FixCommand: "tapio config set kubernetes.in_cluster false",
		})
	}

	// Validate eBPF configuration
	if c.Features.EnableEBPF && runtime.GOOS != "linux" {
		errors = append(errors, ValidationError{
			Field:      "features.enable_ebpf",
			Message:    fmt.Sprintf("eBPF is not supported on %s", runtime.GOOS),
			Suggestion: "disable eBPF or run on Linux",
			FixCommand: "tapio config set features.enable_ebpf false",
			Warning:    true,
		})
	}

	// Validate log format
	validLogFormats := []string{"text", "json"}
	isValidLogFormat := false
	for _, format := range validLogFormats {
		if c.LogFormat == format {
			isValidLogFormat = true
			break
		}
	}
	if !isValidLogFormat {
		errors = append(errors, ValidationError{
			Field:      "log_format",
			Message:    fmt.Sprintf("invalid log format '%s'", c.LogFormat),
			Suggestion: fmt.Sprintf("use one of: %s", strings.Join(validLogFormats, ", ")),
			FixCommand: "tapio config set log_format text",
		})
	}

	// Storage validation removed - no storage config in current version

	// Performance warnings
	if c.UpdateInterval < 5*time.Second && c.UpdateInterval > 0 {
		errors = append(errors, ValidationError{
			Field:      "update_interval",
			Message:    fmt.Sprintf("update interval %v is very short", c.UpdateInterval),
			Suggestion: "consider using at least 5s to avoid high CPU usage",
			FixCommand: "tapio config set update_interval 10s",
			Warning:    true,
		})
	}

	if c.Resources.MaxMemoryUsage > 0 && c.Resources.MaxMemoryUsage < 128 {
		errors = append(errors, ValidationError{
			Field:      "resources.max_memory_usage",
			Message:    fmt.Sprintf("memory limit %dMB may be too low", c.Resources.MaxMemoryUsage),
			Suggestion: "consider at least 128MB for stable operation",
			FixCommand: "tapio config set resources.max_memory_usage 256",
			Warning:    true,
		})
	}

	// Return consolidated error if any
	if len(errors) > 0 {
		return NewValidationErrors(errors)
	}

	return nil
}

// validatePath checks if a path is valid and accessible
func validatePath(path string) error {
	// Expand environment variables and home directory
	path = os.ExpandEnv(path)
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot expand home directory: %v", err)
		}
		path = filepath.Join(home, path[2:])
	}

	// Check if parent directory exists
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("parent directory does not exist: %s", dir)
	}

	return nil
}

// ApplyDefaults fills in missing values with defaults
func (c *Config) ApplyDefaults() {
	defaults := DefaultConfig()

	if c.Version == "" {
		c.Version = defaults.Version
	}
	if c.UpdateInterval == 0 {
		c.UpdateInterval = defaults.UpdateInterval
	}
	if c.LogLevel == "" {
		c.LogLevel = defaults.LogLevel
	}
	if c.LogFormat == "" {
		c.LogFormat = defaults.LogFormat
	}

	// Apply resource defaults
	if c.Resources.MaxMemoryUsage == 0 {
		c.Resources.MaxMemoryUsage = defaults.Resources.MaxMemoryUsage
	}
	if c.Resources.MaxCPUPercent == 0 {
		c.Resources.MaxCPUPercent = defaults.Resources.MaxCPUPercent
	}
	if c.Resources.ParallelWorkers == 0 {
		c.Resources.ParallelWorkers = defaults.Resources.ParallelWorkers
	}

	// Apply output defaults
	if c.Output.Format == "" {
		c.Output.Format = defaults.Output.Format
	}
	if c.Output.PaginationSize == 0 {
		c.Output.PaginationSize = defaults.Output.PaginationSize
	}
}

// GetConfigPaths returns the standard configuration file search paths
func GetConfigPaths() []string {
	home, _ := os.UserHomeDir()
	paths := []string{
		"./tapio.yaml",
		"./tapio.yml",
		"./.tapio.yaml",
		"./.tapio.yml",
	}

	if home != "" {
		paths = append(paths,
			filepath.Join(home, ".tapio", "config.yaml"),
			filepath.Join(home, ".tapio", "config.yml"),
			filepath.Join(home, ".config", "tapio", "config.yaml"),
		)
	}

	paths = append(paths,
		"/etc/tapio/config.yaml",
		"/etc/tapio/config.yml",
	)

	// Add OS-specific paths
	if runtime.GOOS == "darwin" {
		if home != "" {
			paths = append(paths,
				filepath.Join(home, "Library", "Application Support", "Tapio", "config.yaml"),
			)
		}
		paths = append(paths, "/usr/local/etc/tapio/config.yaml")
	}

	return paths
}

// Helper functions
func detectSystemd() bool {
	_, err := os.Stat("/run/systemd/system")
	return err == nil
}

func detectJournald() bool {
	_, err := os.Stat("/var/log/journal")
	if err == nil {
		return true
	}
	_, err = os.Stat("/run/log/journal")
	return err == nil
}

func detectEBPFSupport() bool {
	// Check for eBPF filesystem
	_, err := os.Stat("/sys/fs/bpf")
	if err != nil {
		return false
	}

	// Check kernel version (very basic check)
	// Real implementation would check /proc/version
	return true
}

func detectInCluster() bool {
	// Check for service account token
	_, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount/token")
	return err == nil
}

func isColorTerminal() bool {
	term := os.Getenv("TERM")
	return term != "" && term != "dumb" && os.Getenv("NO_COLOR") == ""
}

func getDefaultWorkerCount() int {
	workers := runtime.NumCPU()
	if workers > 8 {
		workers = 8 // Cap at 8 workers
	}
	if workers < 2 {
		workers = 2 // Minimum 2 workers
	}
	return workers
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
