package config

import (
	"encoding/json"
	"fmt"
	"time"
)

// BaseConfig provides common configuration fields for all collectors
type BaseConfig struct {
	// Name is the unique identifier for the collector instance
	Name string `json:"name" yaml:"name"`

	// BufferSize for the events channel (default: 1000)
	BufferSize int `json:"buffer_size" yaml:"buffer_size"`

	// MetricsEnabled determines if the collector should expose metrics (default: true)
	MetricsEnabled bool `json:"metrics_enabled" yaml:"metrics_enabled"`

	// Labels to add to all events from this collector
	Labels map[string]string `json:"labels" yaml:"labels"`

	// ProcessingTimeout for individual event processing (default: 5s)
	ProcessingTimeout time.Duration `json:"processing_timeout" yaml:"processing_timeout"`

	// MaxRetries for recoverable operations (default: 3)
	MaxRetries int `json:"max_retries" yaml:"max_retries"`

	// HealthCheckInterval for internal health checks (default: 30s)
	HealthCheckInterval time.Duration `json:"health_check_interval" yaml:"health_check_interval"`
}

// CollectorConfig defines the interface all collector configurations must implement
type CollectorConfig interface {
	// GetBaseConfig returns the embedded base configuration
	GetBaseConfig() *BaseConfig

	// Validate performs configuration validation
	Validate() error

	// SetDefaults applies default values to unset fields
	SetDefaults()
}

// DefaultBaseConfig returns a BaseConfig with sensible defaults
func DefaultBaseConfig() *BaseConfig {
	return &BaseConfig{
		BufferSize:          1000,
		MetricsEnabled:      true,
		Labels:              make(map[string]string),
		ProcessingTimeout:   5 * time.Second,
		MaxRetries:          3,
		HealthCheckInterval: 30 * time.Second,
	}
}

// GetBaseConfig implements CollectorConfig interface
func (c *BaseConfig) GetBaseConfig() *BaseConfig {
	return c
}

// Validate performs base configuration validation
func (c *BaseConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("collector name cannot be empty")
	}

	if c.BufferSize <= 0 {
		return fmt.Errorf("buffer_size must be positive, got %d", c.BufferSize)
	}

	if c.BufferSize > 1000000 {
		return fmt.Errorf("buffer_size too large, got %d (max: 1000000)", c.BufferSize)
	}

	if c.ProcessingTimeout <= 0 {
		return fmt.Errorf("processing_timeout must be positive, got %v", c.ProcessingTimeout)
	}

	if c.MaxRetries < 0 {
		return fmt.Errorf("max_retries cannot be negative, got %d", c.MaxRetries)
	}

	if c.HealthCheckInterval <= 0 {
		return fmt.Errorf("health_check_interval must be positive, got %v", c.HealthCheckInterval)
	}

	return nil
}

// SetDefaults applies default values to unset fields
func (c *BaseConfig) SetDefaults() {
	if c.BufferSize == 0 {
		c.BufferSize = 1000
	}

	if c.Labels == nil {
		c.Labels = make(map[string]string)
	}

	if c.ProcessingTimeout == 0 {
		c.ProcessingTimeout = 5 * time.Second
	}

	if c.MaxRetries == 0 {
		c.MaxRetries = 3
	}

	if c.HealthCheckInterval == 0 {
		c.HealthCheckInterval = 30 * time.Second
	}
}

// NamespaceConfig holds configuration specific to the namespace collector
type NamespaceConfig struct {
	*BaseConfig `json:",inline" yaml:",inline"`

	// EnableEBPF determines if eBPF monitoring should be used (default: true)
	EnableEBPF bool `json:"enable_ebpf" yaml:"enable_ebpf"`

	// PodCIDR for filtering namespace events (optional)
	PodCIDR string `json:"pod_cidr" yaml:"pod_cidr"`

	// InterfacePrefix to monitor (default: "eth")
	InterfacePrefix string `json:"interface_prefix" yaml:"interface_prefix"`

	// EnableNetworkPolicies to track network policy events (default: true)
	EnableNetworkPolicies bool `json:"enable_network_policies" yaml:"enable_network_policies"`

	// TrackBandwidth to monitor interface bandwidth (default: false, high overhead)
	TrackBandwidth bool `json:"track_bandwidth" yaml:"track_bandwidth"`
}

// NewNamespaceConfig creates a new namespace configuration with defaults
func NewNamespaceConfig(name string) *NamespaceConfig {
	config := &NamespaceConfig{
		BaseConfig: DefaultBaseConfig(),
	}
	config.Name = name
	config.SetDefaults()
	return config
}

// SetDefaults applies namespace-specific defaults
func (c *NamespaceConfig) SetDefaults() {
	c.BaseConfig.SetDefaults()

	c.EnableEBPF = true // Default to eBPF enabled

	if c.InterfacePrefix == "" {
		c.InterfacePrefix = "eth"
	}

	if !c.EnableNetworkPolicies {
		c.EnableNetworkPolicies = true
	}
}

// Validate performs namespace-specific validation
func (c *NamespaceConfig) Validate() error {
	if err := c.BaseConfig.Validate(); err != nil {
		return fmt.Errorf("base config validation failed: %w", err)
	}

	if c.PodCIDR != "" {
		// Basic CIDR validation
		if len(c.PodCIDR) < 7 { // Minimum: "0.0.0.0/0"
			return fmt.Errorf("invalid pod_cidr format: %s", c.PodCIDR)
		}
	}

	if c.InterfacePrefix == "" {
		return fmt.Errorf("interface_prefix cannot be empty")
	}

	return nil
}

// CRIConfig holds configuration specific to the CRI collector
type CRIConfig struct {
	*BaseConfig `json:",inline" yaml:",inline"`

	// RuntimeEndpoint for CRI communication (default: unix:///var/run/containerd/containerd.sock)
	RuntimeEndpoint string `json:"runtime_endpoint" yaml:"runtime_endpoint"`

	// RuntimeTimeout for CRI requests (default: 10s)
	RuntimeTimeout time.Duration `json:"runtime_timeout" yaml:"runtime_timeout"`

	// EnableMemoryTracking via eBPF (default: true)
	EnableMemoryTracking bool `json:"enable_memory_tracking" yaml:"enable_memory_tracking"`

	// EnableCPUTracking via eBPF (default: true)
	EnableCPUTracking bool `json:"enable_cpu_tracking" yaml:"enable_cpu_tracking"`

	// ContainerStatsInterval for periodic container stats collection (default: 30s)
	ContainerStatsInterval time.Duration `json:"container_stats_interval" yaml:"container_stats_interval"`
}

// NewCRIConfig creates a new CRI configuration with defaults
func NewCRIConfig(name string) *CRIConfig {
	config := &CRIConfig{
		BaseConfig: DefaultBaseConfig(),
	}
	config.Name = name
	config.SetDefaults()
	return config
}

// SetDefaults applies CRI-specific defaults
func (c *CRIConfig) SetDefaults() {
	c.BaseConfig.SetDefaults()

	if c.RuntimeEndpoint == "" {
		c.RuntimeEndpoint = "unix:///var/run/containerd/containerd.sock"
	}

	if c.RuntimeTimeout == 0 {
		c.RuntimeTimeout = 10 * time.Second
	}

	c.EnableMemoryTracking = true
	c.EnableCPUTracking = true

	if c.ContainerStatsInterval == 0 {
		c.ContainerStatsInterval = 30 * time.Second
	}
}

// Validate performs CRI-specific validation
func (c *CRIConfig) Validate() error {
	if err := c.BaseConfig.Validate(); err != nil {
		return fmt.Errorf("base config validation failed: %w", err)
	}

	if c.RuntimeEndpoint == "" {
		return fmt.Errorf("runtime_endpoint cannot be empty")
	}

	if c.RuntimeTimeout <= 0 {
		return fmt.Errorf("runtime_timeout must be positive, got %v", c.RuntimeTimeout)
	}

	if c.ContainerStatsInterval <= 0 {
		return fmt.Errorf("container_stats_interval must be positive, got %v", c.ContainerStatsInterval)
	}

	return nil
}

// KernelConfig holds configuration specific to the kernel collector
type KernelConfig struct {
	*BaseConfig `json:",inline" yaml:",inline"`

	// EnableMemoryTracking via kmem tracepoints (default: true)
	EnableMemoryTracking bool `json:"enable_memory_tracking" yaml:"enable_memory_tracking"`

	// EnableProcessTracking via sched tracepoints (default: true)
	EnableProcessTracking bool `json:"enable_process_tracking" yaml:"enable_process_tracking"`

	// EnableNetworkTracking via kprobes (default: true, may fail without privileges)
	EnableNetworkTracking bool `json:"enable_network_tracking" yaml:"enable_network_tracking"`

	// EnableFileTracking via syscall tracepoints (default: false, high overhead)
	EnableFileTracking bool `json:"enable_file_tracking" yaml:"enable_file_tracking"`

	// PerfBufferSize for eBPF ring buffer (default: 64 pages)
	PerfBufferSize int `json:"perf_buffer_size" yaml:"perf_buffer_size"`

	// Aliases for compatibility
	EnableNetworking bool `json:"enable_networking" yaml:"enable_networking"`
	EnableProcess    bool `json:"enable_process" yaml:"enable_process"`
	EnableSecurity   bool `json:"enable_security" yaml:"enable_security"`
}

// NewKernelConfig creates a new kernel configuration with defaults
func NewKernelConfig(name string) *KernelConfig {
	config := &KernelConfig{
		BaseConfig: DefaultBaseConfig(),
	}
	config.Name = name
	config.SetDefaults()
	return config
}

// SetDefaults applies kernel-specific defaults
func (c *KernelConfig) SetDefaults() {
	c.BaseConfig.SetDefaults()

	c.EnableMemoryTracking = true
	c.EnableProcessTracking = true
	c.EnableNetworkTracking = true
	c.EnableFileTracking = false // High overhead by default

	// Set compatibility aliases
	c.EnableNetworking = c.EnableNetworkTracking
	c.EnableProcess = c.EnableProcessTracking
	c.EnableSecurity = false // No security tracking by default

	if c.PerfBufferSize == 0 {
		c.PerfBufferSize = 64 // pages
	}
}

// GetName returns the collector name
func (c *KernelConfig) GetName() string {
	return c.BaseConfig.Name
}

// GetBufferSize returns the buffer size
func (c *KernelConfig) GetBufferSize() int {
	return c.BaseConfig.BufferSize
}

// Validate performs kernel-specific validation
func (c *KernelConfig) Validate() error {
	if err := c.BaseConfig.Validate(); err != nil {
		return fmt.Errorf("base config validation failed: %w", err)
	}

	if c.PerfBufferSize <= 0 {
		return fmt.Errorf("perf_buffer_size must be positive, got %d", c.PerfBufferSize)
	}

	if c.PerfBufferSize > 1024 {
		return fmt.Errorf("perf_buffer_size too large, got %d (max: 1024)", c.PerfBufferSize)
	}

	// Must be power of 2
	if c.PerfBufferSize&(c.PerfBufferSize-1) != 0 {
		return fmt.Errorf("perf_buffer_size must be power of 2, got %d", c.PerfBufferSize)
	}

	return nil
}

// ParseConfig parses JSON configuration into the appropriate config type
func ParseConfig(configType string, data []byte) (CollectorConfig, error) {
	switch configType {
	case "namespace":
		var config NamespaceConfig
		if err := json.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("failed to parse namespace config: %w", err)
		}
		config.SetDefaults()
		if err := config.Validate(); err != nil {
			return nil, fmt.Errorf("namespace config validation failed: %w", err)
		}
		return &config, nil

	case "cri":
		var config CRIConfig
		if err := json.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("failed to parse CRI config: %w", err)
		}
		config.SetDefaults()
		if err := config.Validate(); err != nil {
			return nil, fmt.Errorf("CRI config validation failed: %w", err)
		}
		return &config, nil

	case "kernel":
		var config KernelConfig
		if err := json.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("failed to parse kernel config: %w", err)
		}
		config.SetDefaults()
		if err := config.Validate(); err != nil {
			return nil, fmt.Errorf("kernel config validation failed: %w", err)
		}
		return &config, nil

	default:
		return nil, fmt.Errorf("unknown collector type: %s", configType)
	}
}

// DNSConfig holds configuration specific to the DNS collector
type DNSConfig struct {
	*BaseConfig `json:",inline" yaml:",inline"`

	// EnableEBPF determines if eBPF monitoring should be used (default: true)
	EnableEBPF bool `json:"enable_ebpf" yaml:"enable_ebpf"`

	// Interface to monitor (default: auto-detect)
	Interface string `json:"interface" yaml:"interface"`

	// EnableSocket determines if socket-based monitoring should be used (default: false)
	EnableSocket bool `json:"enable_socket" yaml:"enable_socket"`
}

// NewDNSConfig creates a new DNS configuration with defaults
func NewDNSConfig(name string) *DNSConfig {
	config := &DNSConfig{
		BaseConfig: DefaultBaseConfig(),
	}
	config.Name = name
	config.SetDefaults()
	return config
}

// SetDefaults applies DNS-specific defaults
func (c *DNSConfig) SetDefaults() {
	c.BaseConfig.SetDefaults()

	c.EnableEBPF = true    // Default to eBPF enabled
	c.EnableSocket = false // Default to socket monitoring disabled
}

// GetName returns the collector name
func (c *DNSConfig) GetName() string {
	return c.BaseConfig.Name
}

// GetBufferSize returns the buffer size
func (c *DNSConfig) GetBufferSize() int {
	return c.BaseConfig.BufferSize
}

// Validate performs DNS-specific validation
func (c *DNSConfig) Validate() error {
	if err := c.BaseConfig.Validate(); err != nil {
		return fmt.Errorf("base config validation failed: %w", err)
	}

	// At least one monitoring method must be enabled
	if !c.EnableEBPF && !c.EnableSocket {
		return fmt.Errorf("at least one of EnableEBPF or EnableSocket must be true")
	}

	return nil
}

// ETCDConfig holds configuration specific to the ETCD collector
type ETCDConfig struct {
	*BaseConfig `json:",inline" yaml:",inline"`

	// EnableEBPF determines if eBPF monitoring should be used (default: true)
	EnableEBPF bool `json:"enable_ebpf" yaml:"enable_ebpf"`

	// Endpoints for API monitoring
	Endpoints []string `json:"endpoints" yaml:"endpoints"`

	// Authentication
	Username string `json:"username" yaml:"username"`
	Password string `json:"password" yaml:"password"`

	// TLS configuration
	TLS *ETCDTLSConfig `json:"tls" yaml:"tls"`
}

// ETCDTLSConfig holds TLS configuration for ETCD
type ETCDTLSConfig struct {
	CertFile string `json:"cert_file" yaml:"cert_file"`
	KeyFile  string `json:"key_file" yaml:"key_file"`
	CAFile   string `json:"ca_file" yaml:"ca_file"`
}

// NewETCDConfig creates a new ETCD configuration with defaults
func NewETCDConfig(name string) *ETCDConfig {
	config := &ETCDConfig{
		BaseConfig: DefaultBaseConfig(),
	}
	config.Name = name
	config.SetDefaults()
	return config
}

// SetDefaults applies ETCD-specific defaults
func (c *ETCDConfig) SetDefaults() {
	c.BaseConfig.SetDefaults()

	c.EnableEBPF = true // Default to eBPF enabled

	if len(c.Endpoints) == 0 {
		c.Endpoints = []string{"localhost:2379"} // Default etcd endpoint
	}
}

// GetName returns the collector name
func (c *ETCDConfig) GetName() string {
	return c.BaseConfig.Name
}

// GetBufferSize returns the buffer size
func (c *ETCDConfig) GetBufferSize() int {
	return c.BaseConfig.BufferSize
}

// Validate performs ETCD-specific validation
func (c *ETCDConfig) Validate() error {
	if err := c.BaseConfig.Validate(); err != nil {
		return fmt.Errorf("base config validation failed: %w", err)
	}

	// Validate TLS configuration if present
	if c.TLS != nil {
		if c.TLS.CertFile != "" && c.TLS.KeyFile == "" {
			return fmt.Errorf("cert file specified but key file is missing")
		}
		if c.TLS.KeyFile != "" && c.TLS.CertFile == "" {
			return fmt.Errorf("key file specified but cert file is missing")
		}
	}

	return nil
}
