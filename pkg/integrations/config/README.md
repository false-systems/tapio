# Unified Configuration Framework

This package provides a unified configuration framework for all Tapio integrations, ensuring consistency and reusability across the platform.

## Overview

The configuration framework provides:

- **Base Configuration**: Common fields shared by all integrations
- **Aspect-Specific Configs**: Security, resilience, monitoring configurations
- **Builder Pattern**: Fluent API for constructing configurations
- **Validation**: Built-in validation for configuration correctness
- **File Support**: Load/save configurations from/to YAML/JSON files

## Architecture

```
config/
├── base.go        # Base configuration and interfaces
├── security.go    # Security-specific configuration
├── resilience.go  # Resilience patterns configuration
├── monitoring.go  # Monitoring and observability configuration
└── builder.go     # Configuration builder and utilities
```

## Usage

### Basic Example

```go
import "github.com/yairfalse/tapio/pkg/integrations/config"

// Using the builder pattern
cfg, err := config.NewBuilder().
    WithName("my-integration").
    WithType("collector").
    WithEnvironment("production").
    WithRetry(config.RetryConfig{
        Enabled:     true,
        MaxAttempts: 5,
        InitialWait: 100 * time.Millisecond,
    }).
    Build()
```

### Integration Implementation

```go
// Your integration config embeds BaseConfig
type MyIntegrationConfig struct {
    config.BaseConfig `yaml:",inline" json:",inline"`
    
    // Custom fields
    CustomField string `yaml:"custom_field" json:"custom_field"`
    BatchSize   int    `yaml:"batch_size" json:"batch_size"`
}

// Your integration implements the Integration interface
type MyIntegration struct {
    config MyIntegrationConfig
    // ... other fields
}

func (m *MyIntegration) Start() error {
    // Implementation
}

func (m *MyIntegration) Health() config.HealthStatus {
    return config.HealthStatus{
        Healthy: true,
        Status:  "running",
        LastCheck: time.Now(),
    }
}
```

### Security Configuration

```go
secConfig := config.DefaultSecurityConfig()
secConfig.TLS.Enabled = true
secConfig.TLS.CertFile = "/path/to/cert.pem"
secConfig.TLS.KeyFile = "/path/to/key.pem"
secConfig.Auth.Method = "jwt"

cfg, _ := config.NewBuilder().
    WithName("secure-api").
    WithSecurity(secConfig).
    Build()
```

### Resilience Configuration

```go
resConfig := config.DefaultResilienceConfig()
resConfig.CircuitBreaker.Enabled = true
resConfig.CircuitBreaker.FailureThreshold = 10
resConfig.LoadShedding.Enabled = true
resConfig.LoadShedding.CPUThreshold = 75.0

cfg, _ := config.NewBuilder().
    WithName("resilient-service").
    WithResilience(resConfig).
    Build()
```

### Monitoring Configuration

```go
monConfig := config.DefaultMonitoringConfig()
monConfig.Metrics.Provider = "prometheus"
monConfig.Metrics.Endpoint = "http://prometheus:9090"
monConfig.Performance.SLO.AvailabilityTarget = 99.95

cfg, _ := config.NewBuilder().
    WithName("monitored-service").
    WithMonitoring(monConfig).
    Build()
```

### Loading from File

```go
// Load from YAML or JSON
cfg, err := config.LoadFromFile("config.yaml")
if err != nil {
    log.Fatal(err)
}

// Validate the configuration
if err := config.Validate(cfg); err != nil {
    log.Fatal(err)
}
```

### Saving to File

```go
cfg, _ := config.NewBuilder().
    WithName("my-service").
    WithType("api").
    Build()

// Save as YAML
err := config.SaveToFile(cfg, "config.yaml", "yaml")

// Save as JSON
err := config.SaveToFile(cfg, "config.json", "json")
```

## Configuration Fields

### Base Configuration

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Integration name |
| `type` | string | Integration type |
| `version` | string | Integration version |
| `environment` | string | Deployment environment |
| `enabled` | bool | Whether integration is enabled |
| `timeout` | duration | Default timeout |
| `retry` | RetryConfig | Retry configuration |
| `observability` | ObservabilityConfig | Observability settings |
| `limits` | ResourceLimits | Resource usage limits |
| `labels` | map[string]string | Custom labels |
| `metadata` | map[string]string | Custom metadata |

### Security Configuration

- **TLS**: Certificate paths, cipher suites, min version
- **Authentication**: JWT, OAuth2, API keys, basic auth
- **Rate Limiting**: Per-IP, per-user, global limits
- **Network Security**: Allowed/denied CIDRs, trusted proxies

### Resilience Configuration

- **Circuit Breaker**: Failure thresholds, timeout, half-open state
- **Load Shedding**: CPU/memory thresholds, priority levels
- **Timeouts**: Adaptive timeouts, per-method configuration
- **Bulkhead**: Concurrency limits, queue management
- **Health Checks**: Endpoints, intervals, failure detection

### Monitoring Configuration

- **Metrics**: Provider selection, histograms, quantiles
- **Performance**: SLOs, thresholds, profiling
- **Alerting**: Rules, providers, grouping, throttling
- **Profiling**: CPU, memory, goroutine tracking

## Best Practices

1. **Always embed BaseConfig** in your integration configs
2. **Use the builder pattern** for programmatic configuration
3. **Implement the Integration interface** for consistency
4. **Validate configurations** before use
5. **Use defaults** as starting points and customize as needed
6. **Keep sensitive data** out of configuration files

## Migration Guide

For existing integrations:

1. Identify current configuration structures
2. Embed `BaseConfig` in your config struct
3. Move common fields to use base fields
4. Implement the `Integration` interface
5. Update configuration loading to use the framework

## Examples

See the [examples/](../examples/) directory for:
- Complete integration implementation
- Configuration file examples
- Advanced configuration patterns
- Testing configurations