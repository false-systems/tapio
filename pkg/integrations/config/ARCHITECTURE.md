# Unified Configuration Framework Architecture

This document describes the architectural design and implementation details of the unified configuration framework for Tapio integrations.

## Design Principles

### 1. Composition Over Inheritance

The framework uses composition to build configurations:
- BaseConfig provides common functionality
- Aspect-specific configs are composed as needed
- Custom fields are added by embedding BaseConfig

### 2. Zero Dependencies Between Aspects

Each aspect-specific configuration is independent:
- SecurityConfig doesn't depend on ResilienceConfig
- MonitoringConfig can be used without SecurityConfig
- Aspects can be mixed and matched as needed

### 3. Sensible Defaults

Every configuration level provides defaults:
- BaseConfig has production-ready defaults
- Aspect configs have safe defaults
- Missing configs are handled gracefully

### 4. Validation at Multiple Levels

- Structural validation (required fields, types)
- Semantic validation (positive values, valid enums)
- Cross-field validation (TLS cert + key consistency)
- Custom validation hooks for integrations

## Component Architecture

### Core Components

```
config/
├── base.go          # BaseConfig, Integration interface, core types
├── security.go      # Security aspect configuration
├── resilience.go    # Resilience patterns configuration  
├── monitoring.go    # Observability configuration
├── builder.go       # Fluent API and utilities
└── doc.go          # Package documentation
```

### Configuration Hierarchy

```
BaseConfig (Level 0)
├── Identity (name, type, version, environment)
├── Operations (enabled, timeout)
├── Retry (attempts, backoff, jitter)
├── Observability (tracing, metrics, logging)
├── Limits (connections, memory, CPU, rates)
└── Metadata (labels, custom fields)

SecurityConfig (Level 1)
├── BaseConfig (embedded)
├── TLS (certificates, versions, ciphers)
├── Auth (JWT, OAuth2, API keys, basic, mTLS)
├── RateLimit (global, per-endpoint, per-user, per-IP)
└── Network (CIDRs, proxies, connection limits)

ResilienceConfig (Level 1)  
├── BaseConfig (embedded)
├── CircuitBreaker (thresholds, timeouts, observation)
├── LoadShedding (adaptive, threshold, priority-based)
├── Timeout (default, per-method, adaptive)
├── Bulkhead (concurrency, queues, pools)
└── HealthCheck (endpoints, intervals, thresholds)

MonitoringConfig (Level 1)
├── BaseConfig (embedded)
├── Metrics (providers, histograms, cardinality)
├── Performance (SLOs, thresholds, profiling)
├── Alerting (rules, providers, grouping, throttling)
└── Profiling (CPU, memory, goroutine tracking)
```

### Integration Patterns

#### Pattern 1: Simple Integration
```go
type SimpleConfig struct {
    config.BaseConfig `yaml:",inline"`
    
    // Custom fields only
    Endpoint string `yaml:"endpoint"`
    BatchSize int  `yaml:"batch_size"`
}
```

#### Pattern 2: Secure Integration
```go
type SecureConfig struct {
    config.BaseConfig   `yaml:",inline"`
    config.SecurityConfig `yaml:",inline"`
    
    // Custom fields
    APIKey string `yaml:"api_key"`
}
```

#### Pattern 3: Full-Featured Integration
```go
type FullConfig struct {
    config.BaseConfig `yaml:",inline"`
    
    // Custom fields
    Endpoint string `yaml:"endpoint"`
    
    // Optional aspects
    Security   *config.SecurityConfig   `yaml:"security,omitempty"`
    Resilience *config.ResilienceConfig `yaml:"resilience,omitempty"`
    Monitoring *config.MonitoringConfig `yaml:"monitoring,omitempty"`
}
```

## Configuration Lifecycle

### 1. Construction Phase

```
File Loading ──→ Parsing ──→ Validation ──→ Default Application ──→ Final Config
     │               │            │                   │               │
     │               │            │                   │               │
  YAML/JSON      Unmarshal    Structural         Fill Missing     Ready to Use
   Content        to Map      Validation          Defaults
```

### 2. Runtime Phase

```
Config Usage ──→ Health Monitoring ──→ Statistics ──→ Reload ──→ Shutdown
      │                │                   │           │          │
      │                │                   │           │          │
  Start()          Health()         Statistics()   Reload()    Stop()
Integration       Check Status    Runtime Metrics  Update      Cleanup
```

### 3. Validation Pipeline

```
Raw Config ──→ Structure ──→ Semantics ──→ Cross-Field ──→ Custom ──→ Valid Config
     │            │             │             │           │           │
     │            │             │             │           │           │
  File/Builder  Required      Range/Enum    Dependencies  Integration  Ready
   Content      Fields        Checks        Validation    Specific
```

## Builder Pattern Implementation

### Builder State Machine

```
New() ──→ WithName() ──→ WithType() ──→ WithAspects() ──→ WithCustom() ──→ Build()
  │           │             │              │                │              │
  │           │             │              │                │              │
Empty      Identity      Required       Optional         Custom         Final
Builder     Set          Fields         Aspects          Fields         Config
```

### Fluent API Design

```go
type Builder struct {
    baseConfig       BaseConfig                // Always present
    securityConfig   *SecurityConfig          // Optional
    resilienceConfig *ResilienceConfig        // Optional  
    monitoringConfig *MonitoringConfig        // Optional
    customConfigs    map[string]interface{}   // Custom fields
}
```

## File Format Support

### YAML Structure
```yaml
# Base configuration (inline)
name: my-integration
type: collector
enabled: true

# Custom fields (top-level)
endpoint: https://api.example.com
batch_size: 100

# Aspect configurations (nested)
security:
  tls:
    enabled: true
    cert_file: /path/to/cert.pem
    
resilience:
  circuit_breaker:
    enabled: true
    failure_threshold: 5
```

### JSON Structure
```json
{
  "name": "my-integration",
  "type": "collector",
  "enabled": true,
  "endpoint": "https://api.example.com",
  "batch_size": 100,
  "security": {
    "tls": {
      "enabled": true,
      "cert_file": "/path/to/cert.pem"
    }
  },
  "resilience": {
    "circuit_breaker": {
      "enabled": true,
      "failure_threshold": 5
    }
  }
}
```

## Validation Architecture

### Validation Layers

1. **Structural Validation**
   - Required fields present
   - Correct data types
   - Valid enum values

2. **Semantic Validation**
   - Positive numeric values
   - Valid duration formats
   - Reasonable limits and ranges

3. **Cross-Field Validation**
   - TLS cert/key consistency
   - Timeout relationships
   - Threshold dependencies

4. **Custom Validation**
   - Integration-specific rules
   - Business logic validation
   - External dependency checks

### Validation Rules

#### BaseConfig Rules
- `name` and `type` are required
- `timeout` must be positive
- `retry.max_attempts` must be >= 1
- `limits` values must be positive

#### SecurityConfig Rules
- If TLS enabled, cert_file and key_file required
- Rate limit values must be positive
- CIDR formats must be valid
- Auth method must be supported

#### ResilienceConfig Rules
- Circuit breaker thresholds must be positive
- Timeout values must be positive and ordered
- Bulkhead limits must be reasonable
- Health check intervals must be positive

#### MonitoringConfig Rules
- Metrics provider must be supported
- SLO targets must be between 0 and 100
- Alert thresholds must be positive
- Profiling rates must be reasonable

## Error Handling Strategy

### Error Categories

1. **Configuration Errors** (user fixable)
   - Missing required fields
   - Invalid values
   - File format errors
   - Validation failures

2. **System Errors** (environment issues)
   - File not found
   - Permission denied
   - Network unreachable
   - Resource exhaustion

3. **Logic Errors** (code bugs)
   - Invalid state transitions
   - Nil pointer dereferences
   - Type assertion failures
   - Unexpected conditions

### Error Handling Patterns

```go
// Validation errors include context
func validateConfig(cfg Config) error {
    if cfg.Name == "" {
        return fmt.Errorf("missing required field: name")
    }
    if cfg.Timeout <= 0 {
        return fmt.Errorf("timeout must be positive, got %v", cfg.Timeout)
    }
    return nil
}

// Wrapped errors preserve context
func LoadFromFile(path string) (Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return Config{}, fmt.Errorf("failed to read config file %s: %w", path, err)
    }
    // ... parse and validate
}
```

## Performance Considerations

### Memory Usage
- Configs are typically small (< 1KB)
- Lazy initialization of optional aspects
- Shared defaults to reduce allocation
- String interning for common values

### CPU Usage
- Validation is O(1) for most checks
- File parsing dominates load time
- Builder pattern adds minimal overhead
- Caching for repeated operations

### Concurrency
- Configs are immutable after construction
- Safe for concurrent read access
- Reload operations use copy-on-write
- No locks needed for normal operation

## Testing Strategy

### Unit Tests
- Each component tested in isolation
- All validation rules covered
- Error conditions tested
- Default value verification

### Integration Tests
- End-to-end configuration loading
- Builder pattern workflows
- File format compatibility
- Cross-aspect interactions

### Property-Based Tests
- Random config generation
- Invariant checking
- Serialization round-trips
- Validation consistency

### Performance Tests
- Config loading benchmarks
- Memory usage profiling
- Validation performance
- Builder pattern overhead

## Future Enhancements

### Planned Features
1. **Config Templating** - Template-based configuration generation
2. **Dynamic Validation** - Runtime validation rule updates
3. **Config Diffing** - Compare configurations for changes
4. **Hot Reloading** - Watch files for automatic reloading
5. **Config Encryption** - Encrypt sensitive configuration data
6. **Schema Generation** - Generate JSON schemas from Go structs

### Backward Compatibility
- Semantic versioning for config format changes
- Migration utilities for config upgrades
- Deprecation warnings for old formats
- Gradual feature rollout strategy

## Integration Guidelines

### For New Integrations
1. Embed BaseConfig in your config struct
2. Add custom fields as needed
3. Choose appropriate aspect configs
4. Implement the Integration interface
5. Add comprehensive tests
6. Document configuration options

### For Existing Integrations
1. Create wrapper config struct
2. Migrate existing fields gradually
3. Maintain backward compatibility
4. Add new framework features incrementally
5. Update documentation and examples
6. Validate against existing deployments