package cri

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
)

// Config holds CRI collector configuration - lean and performance-focused
type Config struct {
	// Core settings
	Name            string `json:"name" yaml:"name" validate:"required,min=1,max=50"`
	SocketPath      string `json:"socket_path" yaml:"socket_path"`
	EventBufferSize int    `json:"event_buffer_size" yaml:"event_buffer_size" validate:"min=100,max=50000"`

	// Performance tuning
	PollInterval   time.Duration `json:"poll_interval" yaml:"poll_interval" validate:"min=10ms,max=10s"`
	BatchSize      int           `json:"batch_size" yaml:"batch_size" validate:"min=10,max=1000"`
	FlushInterval  time.Duration `json:"flush_interval" yaml:"flush_interval" validate:"min=10ms,max=5s"`
	RingBufferSize int           `json:"ring_buffer_size" yaml:"ring_buffer_size" validate:"min=1024,max=65536"`

	// Feature flags
	EnableMetrics bool `json:"enable_metrics" yaml:"enable_metrics"`
	EnableTracing bool `json:"enable_tracing" yaml:"enable_tracing"`
	EnableEBPF    bool `json:"enable_ebpf" yaml:"enable_ebpf"`

	// OTEL configuration
	TracingEnabled    bool          `json:"tracing_enabled" yaml:"tracing_enabled"`
	TracingSampleRate float64       `json:"tracing_sample_rate" yaml:"tracing_sample_rate" validate:"min=0,max=1"`
	MetricsEnabled    bool          `json:"metrics_enabled" yaml:"metrics_enabled"`
	MetricsInterval   time.Duration `json:"metrics_interval" yaml:"metrics_interval" validate:"min=1s,max=5m"`

	// OTEL exporter settings
	OTLPEndpoint string            `json:"otlp_endpoint" yaml:"otlp_endpoint"`
	OTLPInsecure bool              `json:"otlp_insecure" yaml:"otlp_insecure"`
	OTLPHeaders  map[string]string `json:"otlp_headers" yaml:"otlp_headers"`

	// Span settings
	SpanBufferSize   int           `json:"span_buffer_size" yaml:"span_buffer_size" validate:"min=512,max=32768"`
	SpanBatchTimeout time.Duration `json:"span_batch_timeout" yaml:"span_batch_timeout" validate:"min=100ms,max=30s"`
	SpanBatchSize    int           `json:"span_batch_size" yaml:"span_batch_size" validate:"min=64,max=2048"`

	// Resource attributes
	ServiceName           string `json:"service_name" yaml:"service_name"`
	ServiceVersion        string `json:"service_version" yaml:"service_version"`
	DeploymentEnvironment string `json:"deployment_environment" yaml:"deployment_environment"`

	// Filtering - lean configuration
	KubernetesOnly          bool     `json:"kubernetes_only" yaml:"kubernetes_only"`
	ExcludeSystemContainers bool     `json:"exclude_system_containers" yaml:"exclude_system_containers"`
	IncludeNamespaces       []string `json:"include_namespaces" yaml:"include_namespaces"`
	ExcludeNamespaces       []string `json:"exclude_namespaces" yaml:"exclude_namespaces"`

	// Resource limits - production safety
	MaxMemoryMB   int `json:"max_memory_mb" yaml:"max_memory_mb" validate:"min=50,max=1000"`
	MaxCPUPercent int `json:"max_cpu_percent" yaml:"max_cpu_percent" validate:"min=5,max=50"`

	// Health monitoring
	HealthCheckInterval time.Duration `json:"health_check_interval" yaml:"health_check_interval" validate:"min=5s,max=5m"`
	HealthCheckTimeout  time.Duration `json:"health_check_timeout" yaml:"health_check_timeout" validate:"min=1s,max=30s"`
}

// DefaultConfig returns optimized default configuration for production
func DefaultConfig() Config {
	return Config{
		Name:            CollectorName,
		SocketPath:      "", // Auto-detect
		EventBufferSize: 10000,

		// High-performance defaults
		PollInterval:   100 * time.Millisecond, // 10 Hz - balanced performance
		BatchSize:      EventBatchSize,
		FlushInterval:  FlushInterval,
		RingBufferSize: RingBufferSize,

		// Features
		EnableMetrics: true,
		EnableTracing: true,
		EnableEBPF:    true, // Enable eBPF by default on Linux

		// OTEL defaults
		TracingEnabled:    true,
		TracingSampleRate: 0.1, // Sample 10% in production
		MetricsEnabled:    true,
		MetricsInterval:   30 * time.Second,

		// OTEL exporter defaults
		OTLPEndpoint: "localhost:4317",
		OTLPInsecure: true,
		OTLPHeaders:  make(map[string]string),

		// Span defaults
		SpanBufferSize:   2048,
		SpanBatchTimeout: 5 * time.Second,
		SpanBatchSize:    512,

		// Resource attributes
		ServiceName:           "tapio-cri-collector",
		ServiceVersion:        "1.0.0",
		DeploymentEnvironment: "production",

		// Sensible filtering defaults
		KubernetesOnly:          true,                                   // Focus on K8s containers
		ExcludeSystemContainers: true,                                   // Skip pause containers
		IncludeNamespaces:       []string{},                             // Include all namespaces
		ExcludeNamespaces:       []string{"kube-system", "kube-public"}, // Skip system namespaces

		// Conservative resource limits
		MaxMemoryMB:   100, // 100MB max memory usage
		MaxCPUPercent: 10,  // 10% max CPU usage

		// Health monitoring
		HealthCheckInterval: 30 * time.Second,
		HealthCheckTimeout:  5 * time.Second,
	}
}

// ProductionConfig returns production-optimized configuration
func ProductionConfig() Config {
	config := DefaultConfig()

	// Production-specific overrides
	config.PollInterval = 50 * time.Millisecond   // Higher frequency
	config.EventBufferSize = 20000                // Larger buffer
	config.RingBufferSize = 16384                 // Larger ring buffer
	config.MaxMemoryMB = 200                      // More memory allowed
	config.MaxCPUPercent = 20                     // More CPU allowed
	config.HealthCheckInterval = 15 * time.Second // More frequent health checks

	// Production OTEL settings
	config.TracingSampleRate = 0.05           // Lower sampling in prod (5%)
	config.MetricsInterval = 15 * time.Second // More frequent metrics
	config.SpanBatchTimeout = 2 * time.Second // Faster span batching
	config.SpanBufferSize = 4096              // Larger span buffer

	return config
}

// DevConfig returns development-optimized configuration
func DevConfig() Config {
	config := DefaultConfig()

	// Development-specific overrides
	config.KubernetesOnly = false                // Monitor all containers
	config.ExcludeSystemContainers = false       // Include system containers
	config.ExcludeNamespaces = []string{}        // Monitor all namespaces
	config.PollInterval = 500 * time.Millisecond // Lower frequency
	config.EnableEBPF = false                    // Disable eBPF in dev

	// Development OTEL settings
	config.TracingSampleRate = 1.0               // Sample everything in dev
	config.MetricsInterval = 10 * time.Second    // More frequent metrics
	config.DeploymentEnvironment = "development" // Set environment
	config.SpanBatchTimeout = 1 * time.Second    // Faster span batching for dev

	return config
}

// Validate validates the configuration
func (c Config) Validate() error {
	validator := validator.New()

	if err := validator.Struct(c); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	// Custom validation
	if c.Name == "" {
		return fmt.Errorf("name cannot be empty")
	}

	if c.EventBufferSize <= 0 {
		return fmt.Errorf("event_buffer_size must be positive")
	}

	if c.BatchSize <= 0 {
		return fmt.Errorf("batch_size must be positive")
	}

	if c.BatchSize > c.EventBufferSize {
		return fmt.Errorf("batch_size cannot be larger than event_buffer_size")
	}

	if c.RingBufferSize&(c.RingBufferSize-1) != 0 {
		return fmt.Errorf("ring_buffer_size must be a power of 2")
	}

	if c.PollInterval <= 0 {
		return fmt.Errorf("poll_interval must be positive")
	}

	if c.FlushInterval <= 0 {
		return fmt.Errorf("flush_interval must be positive")
	}

	if c.HealthCheckInterval <= 0 {
		return fmt.Errorf("health_check_interval must be positive")
	}

	if c.HealthCheckTimeout <= 0 {
		return fmt.Errorf("health_check_timeout must be positive")
	}

	if c.HealthCheckTimeout >= c.HealthCheckInterval {
		return fmt.Errorf("health_check_timeout must be less than health_check_interval")
	}

	// OTEL validation
	if c.TracingEnabled {
		if c.TracingSampleRate < 0 || c.TracingSampleRate > 1 {
			return fmt.Errorf("tracing_sample_rate must be between 0 and 1")
		}

		if c.SpanBufferSize <= 0 {
			return fmt.Errorf("span_buffer_size must be positive")
		}

		if c.SpanBatchTimeout <= 0 {
			return fmt.Errorf("span_batch_timeout must be positive")
		}

		if c.SpanBatchSize <= 0 || c.SpanBatchSize > c.SpanBufferSize {
			return fmt.Errorf("span_batch_size must be positive and not exceed span_buffer_size")
		}
	}

	if c.MetricsEnabled {
		if c.MetricsInterval <= 0 {
			return fmt.Errorf("metrics_interval must be positive")
		}
	}

	if c.OTLPEndpoint == "" && (c.TracingEnabled || c.MetricsEnabled) {
		return fmt.Errorf("otlp_endpoint is required when tracing or metrics are enabled")
	}

	return nil
}

// ShouldIncludeContainer determines if container should be monitored
func (c Config) ShouldIncludeContainer(container *ContainerInfo) bool {
	// Check Kubernetes-only filter
	if c.KubernetesOnly && !container.IsKubernetesContainer() {
		return false
	}

	// Check system container exclusion
	if c.ExcludeSystemContainers && c.isSystemContainer(container) {
		return false
	}

	// Extract namespace from container
	namespace := c.extractNamespace(container)

	// Check namespace include list (if specified)
	if len(c.IncludeNamespaces) > 0 {
		if !c.containsString(c.IncludeNamespaces, namespace) {
			return false
		}
	}

	// Check namespace exclude list
	if len(c.ExcludeNamespaces) > 0 {
		if c.containsString(c.ExcludeNamespaces, namespace) {
			return false
		}
	}

	return true
}

// isSystemContainer checks if container is a system container
func (c Config) isSystemContainer(container *ContainerInfo) bool {
	// System container patterns - lean list for performance
	systemPatterns := []string{
		"pause",
		"k8s_POD_",
		"/pause:",
		"registry.k8s.io/pause",
	}

	// Check container name and image
	name := strings.ToLower(container.Name)
	image := strings.ToLower(container.Image)

	for _, pattern := range systemPatterns {
		if strings.Contains(name, pattern) || strings.Contains(image, pattern) {
			return true
		}
	}

	// Check Kubernetes POD container label
	if container.Labels != nil {
		if container.Labels["io.kubernetes.container.name"] == "POD" {
			return true
		}
	}

	return false
}

// extractNamespace extracts namespace from container
func (c Config) extractNamespace(container *ContainerInfo) string {
	if container.Labels == nil {
		return "default"
	}

	namespace, ok := container.Labels["io.kubernetes.pod.namespace"]
	if !ok {
		return "default"
	}

	return namespace
}

// containsString checks if slice contains string
func (c Config) containsString(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
		// Also support glob patterns
		if matched, _ := filepath.Match(s, str); matched {
			return true
		}
	}
	return false
}

// OptimizeForEnvironment adjusts config based on environment characteristics
func (c *Config) OptimizeForEnvironment(containerCount int, eventRate float64) {
	// Adjust buffer sizes based on container count
	if containerCount > 1000 {
		c.EventBufferSize = 50000
		c.RingBufferSize = 32768
		c.MaxMemoryMB = 300
		c.MaxCPUPercent = 25
	} else if containerCount > 500 {
		c.EventBufferSize = 25000
		c.RingBufferSize = 16384
		c.MaxMemoryMB = 200
		c.MaxCPUPercent = 20
	}

	// Adjust poll interval based on event rate
	if eventRate > 100 { // High event rate
		c.PollInterval = 50 * time.Millisecond
		c.FlushInterval = 50 * time.Millisecond
	} else if eventRate < 10 { // Low event rate
		c.PollInterval = 500 * time.Millisecond
		c.FlushInterval = 200 * time.Millisecond
	}
}

// GetEffectiveRingBufferSize returns the actual ring buffer size to use
func (c Config) GetEffectiveRingBufferSize() int {
	// Ensure it's a power of 2 for optimal performance
	size := c.RingBufferSize
	if size <= 0 {
		size = RingBufferSize
	}

	// Round up to next power of 2 if needed
	if size&(size-1) != 0 {
		size = 1
		for size < c.RingBufferSize {
			size <<= 1
		}
	}

	return size
}

// GetEffectiveBatchSize returns the actual batch size to use
func (c Config) GetEffectiveBatchSize() int {
	if c.BatchSize <= 0 {
		return EventBatchSize
	}

	// Ensure batch size doesn't exceed buffer size
	if c.BatchSize > c.EventBufferSize {
		return c.EventBufferSize / 10 // Use 10% of buffer size
	}

	return c.BatchSize
}

// GetOTELConfig returns OTEL-specific configuration for provider setup
func (c Config) GetOTELConfig() OTELConfig {
	return OTELConfig{
		TracingEnabled:        c.TracingEnabled,
		TracingSampleRate:     c.TracingSampleRate,
		MetricsEnabled:        c.MetricsEnabled,
		MetricsInterval:       c.MetricsInterval,
		OTLPEndpoint:          c.OTLPEndpoint,
		OTLPInsecure:          c.OTLPInsecure,
		OTLPHeaders:           c.OTLPHeaders,
		SpanBufferSize:        c.SpanBufferSize,
		SpanBatchTimeout:      c.SpanBatchTimeout,
		SpanBatchSize:         c.SpanBatchSize,
		ServiceName:           c.ServiceName,
		ServiceVersion:        c.ServiceVersion,
		DeploymentEnvironment: c.DeploymentEnvironment,
	}
}

// OTELConfig holds OTEL-specific configuration
type OTELConfig struct {
	TracingEnabled        bool
	TracingSampleRate     float64
	MetricsEnabled        bool
	MetricsInterval       time.Duration
	OTLPEndpoint          string
	OTLPInsecure          bool
	OTLPHeaders           map[string]string
	SpanBufferSize        int
	SpanBatchTimeout      time.Duration
	SpanBatchSize         int
	ServiceName           string
	ServiceVersion        string
	DeploymentEnvironment string
}

// OptimizeOTELForEnvironment adjusts OTEL settings based on environment
func (c *Config) OptimizeOTELForEnvironment(environment string, expectedLoad int) {
	switch strings.ToLower(environment) {
	case "production", "prod":
		// Production: conservative sampling, efficient batching
		c.TracingSampleRate = 0.01 // 1% sampling for high load
		c.SpanBatchTimeout = 5 * time.Second
		c.SpanBatchSize = 1024
		c.MetricsInterval = 30 * time.Second

		if expectedLoad > 10000 { // Very high load
			c.TracingSampleRate = 0.001 // 0.1% sampling
			c.SpanBufferSize = 8192
		}

	case "staging", "stage":
		// Staging: moderate sampling
		c.TracingSampleRate = 0.1 // 10% sampling
		c.SpanBatchTimeout = 3 * time.Second
		c.SpanBatchSize = 512
		c.MetricsInterval = 15 * time.Second

	case "development", "dev":
		// Development: full sampling for debugging
		c.TracingSampleRate = 1.0 // 100% sampling
		c.SpanBatchTimeout = 1 * time.Second
		c.SpanBatchSize = 256
		c.MetricsInterval = 10 * time.Second

	default:
		// Default to production settings
		c.TracingSampleRate = 0.05
		c.SpanBatchTimeout = 5 * time.Second
		c.SpanBatchSize = 512
		c.MetricsInterval = 30 * time.Second
	}

	c.DeploymentEnvironment = environment
}

// ValidateOTELEndpoint validates the OTLP endpoint configuration
func (c Config) ValidateOTELEndpoint() error {
	if c.OTLPEndpoint == "" && (c.TracingEnabled || c.MetricsEnabled) {
		return fmt.Errorf("OTLP endpoint is required when OTEL features are enabled")
	}

	// Basic endpoint validation
	if c.OTLPEndpoint != "" {
		if !strings.Contains(c.OTLPEndpoint, ":") {
			return fmt.Errorf("OTLP endpoint must include port (e.g., localhost:4317)")
		}
	}

	return nil
}

// GetEffectiveTracingSampleRate returns the actual sample rate to use
func (c Config) GetEffectiveTracingSampleRate() float64 {
	if !c.TracingEnabled {
		return 0.0
	}

	sampleRate := c.TracingSampleRate
	if sampleRate <= 0 {
		sampleRate = 0.1 // Default 10%
	} else if sampleRate > 1 {
		sampleRate = 1.0 // Cap at 100%
	}

	return sampleRate
}

// GetEffectiveSpanBufferSize returns the actual span buffer size
func (c Config) GetEffectiveSpanBufferSize() int {
	size := c.SpanBufferSize
	if size <= 0 {
		size = 2048 // Default
	}

	// Ensure it's a reasonable size
	if size < 512 {
		size = 512
	} else if size > 32768 {
		size = 32768
	}

	return size
}
