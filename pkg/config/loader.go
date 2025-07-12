package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Loader handles configuration loading from multiple sources
type Loader struct {
	searchPaths  []string
	envPrefix    string
	allowMissing bool
	configFile   string
	envOverrides map[string]string
}

// NewLoader creates a new configuration loader
func NewLoader() *Loader {
	return &Loader{
		searchPaths:  GetConfigPaths(),
		envPrefix:    "TAPIO_",
		allowMissing: true,
		envOverrides: make(map[string]string),
	}
}

// WithSearchPaths sets custom search paths for configuration files
func (l *Loader) WithSearchPaths(paths []string) *Loader {
	l.searchPaths = paths
	return l
}

// WithEnvPrefix sets the environment variable prefix
func (l *Loader) WithEnvPrefix(prefix string) *Loader {
	l.envPrefix = prefix
	return l
}

// WithConfigFile sets a specific configuration file to load
func (l *Loader) WithConfigFile(file string) *Loader {
	l.configFile = file
	return l
}

// WithEnvOverrides sets environment variable overrides
func (l *Loader) WithEnvOverrides(overrides map[string]string) *Loader {
	l.envOverrides = overrides
	return l
}

// RequireConfigFile makes configuration file mandatory
func (l *Loader) RequireConfigFile() *Loader {
	l.allowMissing = false
	return l
}

// Load loads configuration from all sources in priority order:
// 1. Default configuration
// 2. Configuration file (if found)
// 3. Environment variables
// 4. Command line flags (handled externally)
func (l *Loader) Load() (*Config, error) {
	// Start with default configuration
	config := DefaultConfig()

	// Load from configuration file
	configFile, err := l.loadConfigFile(config)
	if err != nil {
		return nil, err
	}

	// Apply environment variable overrides
	if err := l.applyEnvOverrides(config); err != nil {
		return nil, NewConfigError("env_override",
			fmt.Sprintf("failed to apply environment overrides: %v", err),
			"check environment variable format and values")
	}

	// Apply defaults for any missing values
	config.ApplyDefaults()

	// Validate the final configuration
	if err := config.Validate(); err != nil {
		return nil, err
	}

	// Store the config file path for reference
	if configFile != "" {
		config.Advanced.CustomEnvVars["TAPIO_CONFIG_FILE"] = configFile
	}

	return config, nil
}

// LoadZeroConfig loads a zero-configuration setup for first-time users
func (l *Loader) LoadZeroConfig() (*Config, error) {
	config := ZeroConfig()

	// Apply environment overrides even in zero-config mode
	if err := l.applyEnvOverrides(config); err != nil {
		// In zero-config mode, log but don't fail on env errors
		fmt.Fprintf(os.Stderr, "Warning: failed to apply environment overrides: %v\n", err)
	}

	config.ApplyDefaults()

	// In zero-config mode, validation errors are warnings
	if err := config.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: configuration validation issues: %v\n", err)
		// Continue anyway - zero-config should be permissive
	}

	return config, nil
}

// loadConfigFile finds and loads a configuration file
func (l *Loader) loadConfigFile(config *Config) (string, error) {
	var configFile string

	// If specific config file is set, use it
	if l.configFile != "" {
		configFile = l.configFile
		if _, err := os.Stat(configFile); os.IsNotExist(err) {
			return "", NewConfigFileError("not_found", configFile,
				"specified config file does not exist",
				"check the file path or use 'tapio config init' to create one")
		}
	} else {
		// Search for config file in standard paths
		configFile = l.findConfigFile()
		if configFile == "" {
			if !l.allowMissing {
				return "", NewConfigError("not_found",
					"no configuration file found",
					"create a config file with 'tapio config init' or set TAPIO_CONFIG environment variable")
			}
			// No config file found, but that's okay
			return "", nil
		}
	}

	// Load and parse the config file
	data, err := os.ReadFile(configFile)
	if err != nil {
		return "", NewConfigFileError("read_error", configFile,
			fmt.Sprintf("failed to read config file: %v", err),
			"check file permissions and ensure the file is readable")
	}

	// Parse YAML
	if err := yaml.Unmarshal(data, config); err != nil {
		return "", NewConfigFileError("parse_error", configFile,
			fmt.Sprintf("failed to parse YAML: %v", err),
			"check YAML syntax or validate with 'tapio config validate'")
	}

	return configFile, nil
}

// findConfigFile searches for a configuration file in standard paths
func (l *Loader) findConfigFile() string {
	// Check environment variable first
	if envFile := os.Getenv("TAPIO_CONFIG"); envFile != "" {
		if _, err := os.Stat(envFile); err == nil {
			return envFile
		}
	}

	// Search in standard paths
	for _, path := range l.searchPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}

// applyEnvOverrides applies environment variable overrides to configuration
func (l *Loader) applyEnvOverrides(config *Config) error {
	// Define mapping from environment variables to config fields
	envMappings := map[string]func(string) error{
		"LOG_LEVEL": func(val string) error {
			config.LogLevel = val
			return nil
		},
		"LOG_FORMAT": func(val string) error {
			config.LogFormat = val
			return nil
		},
		"UPDATE_INTERVAL": func(val string) error {
			duration, err := parseDuration(val)
			if err != nil {
				return fmt.Errorf("invalid duration format: %v", err)
			}
			config.UpdateInterval = duration
			return nil
		},
		"ENABLE_EBPF": func(val string) error {
			config.Features.EnableEBPF = parseBool(val)
			return nil
		},
		"ENABLE_PREDICTION": func(val string) error {
			config.Features.EnablePrediction = parseBool(val)
			return nil
		},
		"ENABLE_METRICS": func(val string) error {
			config.Features.EnableMetrics = parseBool(val)
			return nil
		},
		"ENABLE_CORRELATION": func(val string) error {
			config.Features.EnableCorrelation = parseBool(val)
			return nil
		},
		"KUBE_CONTEXT": func(val string) error {
			config.Kubernetes.Context = val
			return nil
		},
		"KUBE_CONFIG": func(val string) error {
			config.Kubernetes.Kubeconfig = val
			return nil
		},
		"IN_CLUSTER": func(val string) error {
			config.Kubernetes.InCluster = parseBool(val)
			return nil
		},
		"MAX_MEMORY_MB": func(val string) error {
			memory, err := parseInt(val)
			if err != nil {
				return fmt.Errorf("invalid memory value: %v", err)
			}
			config.Resources.MaxMemoryUsage = memory
			return nil
		},
		"MAX_CPU_PERCENT": func(val string) error {
			cpu, err := parseFloat(val)
			if err != nil {
				return fmt.Errorf("invalid CPU percentage: %v", err)
			}
			config.Resources.MaxCPUPercent = cpu
			return nil
		},
		"OUTPUT_FORMAT": func(val string) error {
			config.Output.Format = val
			return nil
		},
		"OUTPUT_COLOR": func(val string) error {
			config.Output.Color = parseBool(val)
			return nil
		},
		"OUTPUT_VERBOSE": func(val string) error {
			config.Output.Verbose = parseBool(val)
			return nil
		},
		"METRICS_ENABLED": func(val string) error {
			config.Metrics.Enabled = parseBool(val)
			return nil
		},
		"METRICS_PORT": func(val string) error {
			port, err := parseInt(val)
			if err != nil {
				return fmt.Errorf("invalid port number: %v", err)
			}
			config.Metrics.Port = port
			return nil
		},
		"DEBUG_MODE": func(val string) error {
			config.Advanced.DebugMode = parseBool(val)
			return nil
		},
		"PROFILER_ENABLED": func(val string) error {
			config.Advanced.ProfilerEnabled = parseBool(val)
			return nil
		},
	}

	// Apply environment variable overrides
	for envKey, applyFunc := range envMappings {
		fullEnvKey := l.envPrefix + envKey
		if val := os.Getenv(fullEnvKey); val != "" {
			if err := applyFunc(val); err != nil {
				return fmt.Errorf("environment variable %s: %v", fullEnvKey, err)
			}
		}
	}

	// Apply custom environment overrides
	for key, value := range l.envOverrides {
		if applyFunc, exists := envMappings[key]; exists {
			if err := applyFunc(value); err != nil {
				return fmt.Errorf("override %s: %v", key, err)
			}
		}
	}

	return nil
}

// SaveConfig saves configuration to a file
func (l *Loader) SaveConfig(config *Config, filepath string) error {
	// Ensure directory exists
	dir := filepath[:strings.LastIndex(filepath, "/")]
	if err := os.MkdirAll(dir, 0755); err != nil {
		return NewConfigFileError("create_dir", dir,
			fmt.Sprintf("failed to create config directory: %v", err),
			"check directory permissions or create manually")
	}

	// Marshal to YAML
	data, err := yaml.Marshal(config)
	if err != nil {
		return NewConfigError("marshal",
			fmt.Sprintf("failed to marshal config to YAML: %v", err),
			"check configuration structure for serializable fields")
	}

	// Write to file
	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return NewConfigFileError("write", filepath,
			fmt.Sprintf("failed to write config file: %v", err),
			"check file permissions and disk space")
	}

	return nil
}

// GetDefaultConfigPath returns the default configuration file path
func (l *Loader) GetDefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	if home != "" {
		return filepath.Join(home, ".tapio", "config.yaml")
	}
	return "./tapio.yaml"
}

// InitConfig creates a new configuration file with defaults
func (l *Loader) InitConfig(filepath string, template string) error {
	var config *Config

	switch template {
	case "minimal":
		config = ZeroConfig()
	case "production":
		config = DefaultConfig()
		// Production overrides
		config.LogLevel = "warn"
		config.Features.EnableEBPF = true
		config.Metrics.Enabled = true
		config.Resources.MaxMemoryUsage = 1024
		config.Resources.MaxCPUPercent = 50
	case "development":
		config = DefaultConfig()
		// Development overrides
		config.LogLevel = "debug"
		config.Advanced.DebugMode = true
		config.Advanced.ProfilerEnabled = true
		config.Output.Verbose = true
	default:
		config = DefaultConfig()
	}

	return l.SaveConfig(config, filepath)
}

// ValidateConfigFile validates a configuration file without loading it
func (l *Loader) ValidateConfigFile(filepath string) error {
	// Check if file exists
	if _, err := os.Stat(filepath); os.IsNotExist(err) {
		return NewConfigFileError("not_found", filepath,
			"configuration file does not exist",
			"create the file or check the path")
	}

	// Read file
	data, err := os.ReadFile(filepath)
	if err != nil {
		return NewConfigFileError("read_error", filepath,
			fmt.Sprintf("failed to read file: %v", err),
			"check file permissions")
	}

	// Parse YAML
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return NewConfigFileError("parse_error", filepath,
			fmt.Sprintf("invalid YAML: %v", err),
			"check YAML syntax and structure")
	}

	// Apply defaults and validate
	config.ApplyDefaults()
	if err := config.Validate(); err != nil {
		return err
	}

	return nil
}

// GetEffectiveConfig returns the configuration that would be loaded without actually loading it
func (l *Loader) GetEffectiveConfig() (*Config, []string, error) {
	var sources []string

	// Start with defaults
	config := DefaultConfig()
	_ = config // Used for effective config calculation
	sources = append(sources, "defaults")

	// Find config file
	configFile := l.findConfigFile()
	if configFile != "" {
		sources = append(sources, fmt.Sprintf("file: %s", configFile))
	}

	// Check for environment overrides
	envOverrides := l.getActiveEnvOverrides()
	if len(envOverrides) > 0 {
		sources = append(sources, fmt.Sprintf("env: %d overrides", len(envOverrides)))
	}

	// Load with all sources
	loadedConfig, err := l.Load()
	if err != nil {
		return nil, sources, err
	}

	return loadedConfig, sources, nil
}

// getActiveEnvOverrides returns a list of active environment variable overrides
func (l *Loader) getActiveEnvOverrides() []string {
	var active []string

	envKeys := []string{
		"LOG_LEVEL", "LOG_FORMAT", "UPDATE_INTERVAL",
		"ENABLE_EBPF", "ENABLE_PREDICTION", "ENABLE_METRICS", "ENABLE_CORRELATION",
		"KUBE_CONTEXT", "KUBE_CONFIG", "IN_CLUSTER",
		"MAX_MEMORY_MB", "MAX_CPU_PERCENT",
		"OUTPUT_FORMAT", "OUTPUT_COLOR", "OUTPUT_VERBOSE",
		"METRICS_ENABLED", "METRICS_PORT",
		"DEBUG_MODE", "PROFILER_ENABLED",
	}

	for _, key := range envKeys {
		fullKey := l.envPrefix + key
		if os.Getenv(fullKey) != "" {
			active = append(active, fullKey)
		}
	}

	return active
}
