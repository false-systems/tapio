package orchestrator

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/yairfalse/tapio/pkg/observers"
	"github.com/yairfalse/tapio/pkg/config"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// YAMLConfig represents the complete configuration from YAML file
type YAMLConfig struct {
	Orchestrator OrchestratorYAMLConfig         `yaml:"orchestrator"`
	Observers   map[string]ObserverYAMLConfig `yaml:"observers"`
}

// OrchestratorYAMLConfig holds orchestrator-specific configuration
type OrchestratorYAMLConfig struct {
	Workers    int            `yaml:"workers"`
	BufferSize int            `yaml:"buffer_size"`
	LogLevel   string         `yaml:"log_level"`
	NATS       NATSYAMLConfig `yaml:"nats"`
}

// NATSYAMLConfig holds NATS configuration
type NATSYAMLConfig struct {
	URL           string `yaml:"url"`
	Subject       string `yaml:"subject"`
	MaxReconnects int    `yaml:"max_reconnects"`
	AuthEnabled   bool   `yaml:"auth_enabled"`
	Username      string `yaml:"username"`
	Password      string `yaml:"password"`
	Token         string `yaml:"token"`
}

// ObserverYAMLConfig holds per-observer configuration
type ObserverYAMLConfig struct {
	Enabled bool               `yaml:"enabled"`
	Config  ObserverConfigData `yaml:"config"`
}

// ObserverConfigData holds typed observer configuration
type ObserverConfigData struct {
	// Common fields
	BufferSize   int    `yaml:"buffer_size"`
	EnableEBPF   bool   `yaml:"enable_ebpf"`
	Address      string `yaml:"address"`
	Insecure     bool   `yaml:"insecure"`
	PollInterval string `yaml:"poll_interval"`

	// Kernel-specific
	MonitorConfigMaps    bool `yaml:"monitor_configmaps"`
	MonitorSecrets       bool `yaml:"monitor_secrets"`
	EnablePodCorrelation bool `yaml:"enable_pod_correlation"`
	SyscallSamplingRate  int  `yaml:"syscall_sampling_rate"`

	// Network-specific
	EnableL7         bool    `yaml:"enable_l7"`
	IntelligenceMode bool    `yaml:"intelligence_mode"`
	NoiseReduction   float64 `yaml:"noise_reduction"`
	EnableGRPC       bool    `yaml:"enable_grpc"`
	EnableHTTP       bool    `yaml:"enable_http"`
	GRPCPorts        []int   `yaml:"grpc_ports"`
	HTTPPorts        []int   `yaml:"http_ports"`
	HTTPSPorts       []int   `yaml:"https_ports"`

	// Kubelet-specific
	EnablePodLifecycle      bool `yaml:"enable_pod_lifecycle"`
	EnableResourceMetrics   bool `yaml:"enable_resource_metrics"`
	EnableStorageMonitoring bool `yaml:"enable_storage_monitoring"`

	// DNS-specific
	CacheAnalysis     bool   `yaml:"cache_analysis"`
	QueryTimeout      string `yaml:"query_timeout"`
	EnableCorrelation bool   `yaml:"enable_correlation"`

	// ETCD-specific
	Endpoints   []string `yaml:"endpoints"`
	EnableWatch bool     `yaml:"enable_watch"`
	CertFile    string   `yaml:"cert_file"`
	KeyFile     string   `yaml:"key_file"`
	CAFile      string   `yaml:"ca_file"`

	// CRI-eBPF specific
	EnableOOMKill        bool    `yaml:"enable_oom_kill"`
	EnableMemoryPressure bool    `yaml:"enable_memory_pressure"`
	EnableProcessExit    bool    `yaml:"enable_process_exit"`
	EnableProcessFork    bool    `yaml:"enable_process_fork"`
	MemoryThreshold      float64 `yaml:"memory_threshold"`

	// Storage I/O specific
	MonitorPaths       []string `yaml:"monitor_paths"`
	LatencyThresholdMS int      `yaml:"latency_threshold_ms"`

	// OOM specific
	TrackContainers bool `yaml:"track_containers"`
	TrackProcesses  bool `yaml:"track_processes"`

	// Syscall errors specific
	ErrorCodes   []string `yaml:"error_codes"`
	SamplingRate int      `yaml:"sampling_rate"`

	// OTEL specific
	Endpoint string            `yaml:"endpoint"`
	Protocol string            `yaml:"protocol"`
	Headers  map[string]string `yaml:"headers"`
}

// LoadYAMLConfig loads configuration from a YAML file with environment variable expansion
func LoadYAMLConfig(path string) (*YAMLConfig, error) {
	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	// Expand environment variables
	expanded := expandEnvVars(string(data))

	// Parse YAML
	var config YAMLConfig
	if err := yaml.Unmarshal([]byte(expanded), &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML config: %w", err)
	}

	// Set defaults if not specified
	if config.Orchestrator.Workers == 0 {
		config.Orchestrator.Workers = 4
	}
	if config.Orchestrator.BufferSize == 0 {
		config.Orchestrator.BufferSize = 10000
	}
	if config.Orchestrator.LogLevel == "" {
		config.Orchestrator.LogLevel = "info"
	}
	if config.Orchestrator.NATS.URL == "" {
		config.Orchestrator.NATS.URL = "nats://localhost:4222"
	}
	if config.Orchestrator.NATS.Subject == "" {
		config.Orchestrator.NATS.Subject = "tapio.events"
	}

	return &config, nil
}

// expandEnvVars expands ${VAR} or $VAR in the string
func expandEnvVars(s string) string {
	// Handle ${VAR} format
	s = os.Expand(s, func(key string) string {
		if val := os.Getenv(key); val != "" {
			return val
		}
		// Return the original if env var not found
		return "${" + key + "}"
	})

	// Also handle $VAR format (simpler cases)
	for _, env := range os.Environ() {
		pair := strings.SplitN(env, "=", 2)
		if len(pair) == 2 {
			key := "$" + pair[0]
			if strings.Contains(s, key) {
				s = strings.ReplaceAll(s, key, pair[1])
			}
		}
	}

	return s
}

// ValidateYAMLConfig validates the configuration
func ValidateYAMLConfig(config *YAMLConfig) error {
	// Validate orchestrator config
	if config.Orchestrator.Workers < 1 || config.Orchestrator.Workers > 64 {
		return fmt.Errorf("invalid worker count: %d (must be 1-64)", config.Orchestrator.Workers)
	}

	if config.Orchestrator.BufferSize < 100 || config.Orchestrator.BufferSize > 100000 {
		return fmt.Errorf("invalid buffer size: %d (must be 100-100000)", config.Orchestrator.BufferSize)
	}

	// Validate at least one observer is enabled
	hasEnabled := false
	for name, cfg := range config.Observers {
		if cfg.Enabled {
			hasEnabled = true
			break
		}
		// Check for common misconfigurations
		// Config is a struct, not a pointer, so check if it has meaningful values
		if cfg.Config.BufferSize == 0 && cfg.Enabled {
			return fmt.Errorf("observer %s is enabled but has no config", name)
		}
	}

	if !hasEnabled {
		return fmt.Errorf("no observers are enabled")
	}

	return nil
}

// ToOrchestratorConfig converts YAML config to orchestrator Config
func (c *YAMLConfig) ToOrchestratorConfig() Config {
	return Config{
		Workers:    c.Orchestrator.Workers,
		BufferSize: c.Orchestrator.BufferSize,
		NATSConfig: c.toNATSConfig(),
	}
}

// toNATSConfig converts YAML NATS config to internal NATS config
func (c *YAMLConfig) toNATSConfig() *config.NATSConfig {
	natsConfig := &config.NATSConfig{
		URL:              c.Orchestrator.NATS.URL,
		Name:             "tapio-orchestrator",
		MaxReconnects:    c.Orchestrator.NATS.MaxReconnects,
		JetStreamEnabled: true,
		// Map subject to stream subjects
		RawEventsSubjects: []string{c.Orchestrator.NATS.Subject},
	}

	return natsConfig
}

// GetObserverConfig extracts configuration for a specific observer
func (c *YAMLConfig) GetObserverConfig(name string) (*ObserverConfigData, bool) {
	if cfg, exists := c.Observers[name]; exists && cfg.Enabled {
		return &cfg.Config, true
	}
	return nil, false
}

// IsObserverEnabled checks if an observer is enabled
func (c *YAMLConfig) IsObserverEnabled(name string) bool {
	if cfg, exists := c.Observers[name]; exists {
		return cfg.Enabled
	}
	return false
}

// ObserverFactory is a function that creates an observer
type ObserverFactory func(name string, config *ObserverConfigData, logger *zap.Logger) (observers.Observer, error)

var (
	observerFactories = make(map[string]ObserverFactory)
	factoryMutex      sync.RWMutex
)

// RegisterObserverFactory registers an observer factory
func RegisterObserverFactory(observerType string, factory ObserverFactory) {
	factoryMutex.Lock()
	defer factoryMutex.Unlock()
	observerFactories[observerType] = factory
}

// GetObserverFactory returns a registered observer factory
func GetObserverFactory(observerType string) (ObserverFactory, bool) {
	factoryMutex.RLock()
	defer factoryMutex.RUnlock()
	factory, exists := observerFactories[observerType]
	return factory, exists
}
