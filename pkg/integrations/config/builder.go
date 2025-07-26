package config

import (
	"encoding/json"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Builder provides a fluent API for building integration configurations
type Builder struct {
	baseConfig       BaseConfig
	securityConfig   *SecurityConfig
	resilienceConfig *ResilienceConfig
	monitoringConfig *MonitoringConfig
	customConfigs    map[string]interface{}
}

// NewBuilder creates a new configuration builder
func NewBuilder() *Builder {
	return &Builder{
		baseConfig:    DefaultBaseConfig(),
		customConfigs: make(map[string]interface{}),
	}
}

// WithName sets the integration name
func (b *Builder) WithName(name string) *Builder {
	b.baseConfig.Name = name
	return b
}

// WithType sets the integration type
func (b *Builder) WithType(integrationType string) *Builder {
	b.baseConfig.Type = integrationType
	return b
}

// WithEnvironment sets the environment
func (b *Builder) WithEnvironment(env string) *Builder {
	b.baseConfig.Environment = env
	return b
}

// WithRetry configures retry settings
func (b *Builder) WithRetry(retry RetryConfig) *Builder {
	b.baseConfig.Retry = retry
	return b
}

// WithObservability configures observability settings
func (b *Builder) WithObservability(obs ObservabilityConfig) *Builder {
	b.baseConfig.Observability = obs
	return b
}

// WithLimits configures resource limits
func (b *Builder) WithLimits(limits ResourceLimits) *Builder {
	b.baseConfig.Limits = limits
	return b
}

// WithSecurity adds security configuration
func (b *Builder) WithSecurity(security SecurityConfig) *Builder {
	b.securityConfig = &security
	return b
}

// WithResilience adds resilience configuration
func (b *Builder) WithResilience(resilience ResilienceConfig) *Builder {
	b.resilienceConfig = &resilience
	return b
}

// WithMonitoring adds monitoring configuration
func (b *Builder) WithMonitoring(monitoring MonitoringConfig) *Builder {
	b.monitoringConfig = &monitoring
	return b
}

// WithCustom adds custom configuration
func (b *Builder) WithCustom(key string, value interface{}) *Builder {
	b.customConfigs[key] = value
	return b
}

// Build constructs the final configuration
func (b *Builder) Build() (map[string]interface{}, error) {
	config := make(map[string]interface{})

	// Add base configuration
	baseMap, err := structToMap(b.baseConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to convert base config: %w", err)
	}
	for k, v := range baseMap {
		config[k] = v
	}

	// Add optional configurations
	if b.securityConfig != nil {
		secMap, err := structToMap(*b.securityConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to convert security config: %w", err)
		}
		config["security"] = secMap
	}

	if b.resilienceConfig != nil {
		resMap, err := structToMap(*b.resilienceConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to convert resilience config: %w", err)
		}
		config["resilience"] = resMap
	}

	if b.monitoringConfig != nil {
		monMap, err := structToMap(*b.monitoringConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to convert monitoring config: %w", err)
		}
		config["monitoring"] = monMap
	}

	// Add custom configurations
	for k, v := range b.customConfigs {
		config[k] = v
	}

	return config, nil
}

// LoadFromFile loads configuration from a file
func LoadFromFile(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	config := make(map[string]interface{})

	// Try YAML first
	err = yaml.Unmarshal(data, &config)
	if err == nil {
		return config, nil
	}

	// Try JSON
	err = json.Unmarshal(data, &config)
	if err == nil {
		return config, nil
	}

	return nil, fmt.Errorf("failed to parse config file as YAML or JSON")
}

// SaveToFile saves configuration to a file
func SaveToFile(config map[string]interface{}, path string, format string) error {
	var data []byte
	var err error

	switch format {
	case "yaml", "yml":
		data, err = yaml.Marshal(config)
	case "json":
		data, err = json.MarshalIndent(config, "", "  ")
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}

	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	return os.WriteFile(path, data, 0644)
}

// structToMap converts a struct to a map
func structToMap(v interface{}) (map[string]interface{}, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	return result, err
}

// Validate validates the configuration
func Validate(config map[string]interface{}) error {
	// Check required base fields
	if _, ok := config["name"]; !ok {
		return fmt.Errorf("missing required field: name")
	}

	if _, ok := config["type"]; !ok {
		return fmt.Errorf("missing required field: type")
	}

	// Validate specific configurations if present
	if security, ok := config["security"].(map[string]interface{}); ok {
		if err := validateSecurityConfig(security); err != nil {
			return fmt.Errorf("security config validation failed: %w", err)
		}
	}

	if resilience, ok := config["resilience"].(map[string]interface{}); ok {
		if err := validateResilienceConfig(resilience); err != nil {
			return fmt.Errorf("resilience config validation failed: %w", err)
		}
	}

	if monitoring, ok := config["monitoring"].(map[string]interface{}); ok {
		if err := validateMonitoringConfig(monitoring); err != nil {
			return fmt.Errorf("monitoring config validation failed: %w", err)
		}
	}

	return nil
}

func validateSecurityConfig(config map[string]interface{}) error {
	// Validate TLS config if present
	if tls, ok := config["tls"].(map[string]interface{}); ok {
		if enabled, ok := tls["enabled"].(bool); ok && enabled {
			if _, ok := tls["cert_file"]; !ok {
				return fmt.Errorf("TLS enabled but cert_file not specified")
			}
			if _, ok := tls["key_file"]; !ok {
				return fmt.Errorf("TLS enabled but key_file not specified")
			}
		}
	}
	return nil
}

func validateResilienceConfig(config map[string]interface{}) error {
	// Validate circuit breaker config
	if cb, ok := config["circuit_breaker"].(map[string]interface{}); ok {
		if threshold, ok := cb["failure_threshold"].(float64); ok {
			if threshold <= 0 {
				return fmt.Errorf("failure_threshold must be positive")
			}
		}
	}
	return nil
}

func validateMonitoringConfig(config map[string]interface{}) error {
	// Validate metrics config
	if metrics, ok := config["metrics"].(map[string]interface{}); ok {
		if provider, ok := metrics["provider"].(string); ok {
			validProviders := map[string]bool{
				"prometheus": true,
				"otel":       true,
				"statsd":     true,
			}
			if !validProviders[provider] {
				return fmt.Errorf("invalid metrics provider: %s", provider)
			}
		}
	}
	return nil
}