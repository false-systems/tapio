# Migration Guide: Adopting the Unified Configuration Framework

This guide helps existing integrations migrate to the unified configuration framework while maintaining backward compatibility and operational continuity.

## Overview

The migration process involves:
1. **Assessment** - Analyze current configuration structure
2. **Planning** - Design new configuration schema
3. **Implementation** - Implement framework adoption
4. **Testing** - Validate functionality and compatibility
5. **Deployment** - Roll out changes safely

## Phase 1: Assessment

### Current Configuration Audit

Before migrating, analyze your existing configuration:

```bash
# Find all config structs in your integration
grep -r "type.*Config.*struct" pkg/integrations/your-integration/

# Identify configuration files
find . -name "*.yaml" -o -name "*.json" | grep -E "(config|settings)"

# Check for hardcoded configuration
grep -r "const.*=" pkg/integrations/your-integration/ | grep -E "(timeout|retry|limit)"
```

### Configuration Mapping

Create a mapping table of your current config to framework equivalents:

| Current Field | Framework Equivalent | Notes |
|---------------|---------------------|-------|
| `ServiceName` | `BaseConfig.Name` | Direct mapping |
| `RetryCount` | `BaseConfig.Retry.MaxAttempts` | Semantic equivalent |
| `TLSCert` | `SecurityConfig.TLS.CertFile` | Move to security aspect |
| `MetricsPort` | `MonitoringConfig.Metrics.Endpoint` | Move to monitoring aspect |

### Compatibility Requirements

Document compatibility needs:
- **Configuration Files**: Existing YAML/JSON formats
- **Environment Variables**: Current env var usage
- **API Contracts**: External configuration APIs
- **Operational Tools**: Deployment scripts, monitoring

## Phase 2: Planning

### New Configuration Schema

Design your new configuration structure:

```go
// Before (example)
type OldConfig struct {
    ServiceName   string        `yaml:"service_name"`
    Port         int           `yaml:"port"`
    RetryCount   int           `yaml:"retry_count"`
    RetryDelay   time.Duration `yaml:"retry_delay"`
    TLSEnabled   bool          `yaml:"tls_enabled"`
    TLSCertFile  string        `yaml:"tls_cert_file"`
    TLSKeyFile   string        `yaml:"tls_key_file"`
    MetricsPort  int           `yaml:"metrics_port"`
    LogLevel     string        `yaml:"log_level"`
}

// After (using framework)
type NewConfig struct {
    config.BaseConfig `yaml:",inline" json:",inline"`
    
    // Custom fields
    Port int `yaml:"port" json:"port"`
    
    // Optional aspects
    Security   *config.SecurityConfig   `yaml:"security,omitempty" json:"security,omitempty"`
    Monitoring *config.MonitoringConfig `yaml:"monitoring,omitempty" json:"monitoring,omitempty"`
}
```

### Migration Strategy

Choose an appropriate migration strategy:

#### Strategy 1: Big Bang Migration
- **When**: Small integrations, few deployments
- **Pros**: Clean cut, no compatibility layer
- **Cons**: Higher risk, requires coordinated deployment

#### Strategy 2: Gradual Migration
- **When**: Large integrations, many deployments
- **Pros**: Lower risk, incremental validation
- **Cons**: Temporary complexity, longer timeline

#### Strategy 3: Compatibility Layer
- **When**: Critical integrations, external dependencies
- **Pros**: Zero downtime, backward compatibility
- **Cons**: Code complexity, maintenance overhead

### Rollback Plan

Plan for rollback scenarios:
- Keep old configuration structs as fallback
- Maintain configuration file compatibility
- Document rollback procedures
- Test rollback in staging environment

## Phase 3: Implementation

### Step 1: Add Framework Dependency

```go
// go.mod (if separate module)
require (
    github.com/yairfalse/tapio/pkg/integrations/config v0.1.0
)

// Import in your code
import "github.com/yairfalse/tapio/pkg/integrations/config"
```

### Step 2: Create New Configuration Struct

```go
package myintegration

import (
    "time"
    "github.com/yairfalse/tapio/pkg/integrations/config"
)

type Config struct {
    config.BaseConfig `yaml:",inline" json:",inline"`
    
    // Integration-specific fields
    Endpoint    string        `yaml:"endpoint" json:"endpoint"`
    Port        int           `yaml:"port" json:"port"`
    BatchSize   int           `yaml:"batch_size" json:"batch_size"`
    WorkerCount int           `yaml:"worker_count" json:"worker_count"`
    
    // Optional framework aspects
    Security   *config.SecurityConfig   `yaml:"security,omitempty" json:"security,omitempty"`
    Resilience *config.ResilienceConfig `yaml:"resilience,omitempty" json:"resilience,omitempty"`
    Monitoring *config.MonitoringConfig `yaml:"monitoring,omitempty" json:"monitoring,omitempty"`
}

// Provide defaults
func DefaultConfig() Config {
    return Config{
        BaseConfig:  config.DefaultBaseConfig(),
        Endpoint:    "http://localhost:8080",
        Port:        8080,
        BatchSize:   100,
        WorkerCount: 5,
    }
}

// Validation
func (c Config) Validate() error {
    if c.Endpoint == "" {
        return fmt.Errorf("endpoint is required")
    }
    if c.Port <= 0 || c.Port > 65535 {
        return fmt.Errorf("port must be between 1 and 65535")
    }
    return nil
}
```

### Step 3: Implement Integration Interface

```go
type MyIntegration struct {
    config Config
    // ... other fields
}

// Implement config.Integration interface
func (m *MyIntegration) Start() error {
    if !m.config.Enabled {
        return fmt.Errorf("integration disabled")
    }
    // ... start logic using m.config fields
    return nil
}

func (m *MyIntegration) Stop() error {
    // ... stop logic
    return nil
}

func (m *MyIntegration) Reload(newConfig interface{}) error {
    cfg, ok := newConfig.(Config)
    if !ok {
        return fmt.Errorf("invalid configuration type")
    }
    
    if err := cfg.Validate(); err != nil {
        return fmt.Errorf("invalid configuration: %w", err)
    }
    
    // Update runtime configuration
    m.config = cfg
    return nil
}

func (m *MyIntegration) Health() config.HealthStatus {
    return config.HealthStatus{
        Healthy:   true, // your health logic
        Status:    "running",
        LastCheck: time.Now(),
        Details: map[string]interface{}{
            "endpoint": m.config.Endpoint,
            "port":     m.config.Port,
        },
    }
}

func (m *MyIntegration) Statistics() config.Statistics {
    return config.Statistics{
        StartTime:      time.Now(), // track actual start time
        Uptime:         time.Since(time.Now()), // calculate uptime
        ProcessedCount: 0, // your metrics
        ErrorCount:     0, // your metrics
        LastActivity:   time.Now(),
    }
}

func (m *MyIntegration) GetConfig() interface{} {
    return m.config
}

func (m *MyIntegration) ValidateConfig() error {
    return m.config.Validate()
}
```

### Step 4: Migration Compatibility Layer

For gradual migration, create a compatibility layer:

```go
// Legacy configuration support
type LegacyConfig struct {
    ServiceName  string        `yaml:"service_name"`
    Port        int           `yaml:"port"`
    RetryCount  int           `yaml:"retry_count"`
    RetryDelay  time.Duration `yaml:"retry_delay"`
    TLSEnabled  bool          `yaml:"tls_enabled"`
    TLSCertFile string        `yaml:"tls_cert_file"`
    TLSKeyFile  string        `yaml:"tls_key_file"`
    MetricsPort int           `yaml:"metrics_port"`
    LogLevel    string        `yaml:"log_level"`
}

// Convert legacy config to new format
func ConvertLegacyConfig(legacy LegacyConfig) Config {
    cfg := DefaultConfig()
    
    // Map basic fields
    cfg.Name = legacy.ServiceName
    cfg.Port = legacy.Port
    
    // Map retry configuration
    if legacy.RetryCount > 0 {
        cfg.Retry = config.RetryConfig{
            Enabled:     true,
            MaxAttempts: legacy.RetryCount,
            InitialWait: legacy.RetryDelay,
        }
    }
    
    // Map TLS configuration
    if legacy.TLSEnabled {
        cfg.Security = &config.SecurityConfig{
            BaseConfig: cfg.BaseConfig,
            TLS: config.TLSConfig{
                Enabled:  true,
                CertFile: legacy.TLSCertFile,
                KeyFile:  legacy.TLSKeyFile,
            },
        }
    }
    
    // Map monitoring configuration
    if legacy.MetricsPort > 0 {
        cfg.Monitoring = &config.MonitoringConfig{
            BaseConfig: cfg.BaseConfig,
            Metrics: config.MetricsConfig{
                Enabled:  true,
                Endpoint: fmt.Sprintf("localhost:%d", legacy.MetricsPort),
            },
        }
    }
    
    // Map logging
    if legacy.LogLevel != "" {
        cfg.Observability.LogLevel = legacy.LogLevel
    }
    
    return cfg
}

// Auto-detect configuration format
func LoadConfig(path string) (Config, error) {
    // Try new format first
    if cfg, err := loadNewFormatConfig(path); err == nil {
        return cfg, nil
    }
    
    // Fall back to legacy format
    legacyConfig, err := loadLegacyConfig(path)
    if err != nil {
        return Config{}, fmt.Errorf("failed to load config in any format: %w", err)
    }
    
    // Convert and warn
    log.Println("WARN: Using legacy configuration format, please migrate to new format")
    return ConvertLegacyConfig(legacyConfig), nil
}
```

### Step 5: Update Configuration Files

Migrate configuration files to new format:

```yaml
# Before (legacy format)
service_name: my-integration
port: 8080
retry_count: 3
retry_delay: 1s
tls_enabled: true
tls_cert_file: /etc/certs/cert.pem
tls_key_file: /etc/certs/key.pem
metrics_port: 9090
log_level: info

# After (new framework format)
name: my-integration
type: collector
version: 1.0.0
environment: production
enabled: true

# Integration-specific fields
port: 8080

# Framework configurations
retry:
  enabled: true
  max_attempts: 3
  initial_wait: 1s

observability:
  log_level: info
  metrics_enabled: true
  metrics_endpoint: http://localhost:9090

security:
  tls:
    enabled: true
    cert_file: /etc/certs/cert.pem
    key_file: /etc/certs/key.pem
```

## Phase 4: Testing

### Unit Tests

```go
func TestConfigMigration(t *testing.T) {
    legacy := LegacyConfig{
        ServiceName: "test-service",
        Port:        8080,
        RetryCount:  5,
        RetryDelay:  2 * time.Second,
        TLSEnabled:  true,
        TLSCertFile: "/path/to/cert.pem",
        TLSKeyFile:  "/path/to/key.pem",
    }
    
    newConfig := ConvertLegacyConfig(legacy)
    
    // Verify basic fields
    assert.Equal(t, "test-service", newConfig.Name)
    assert.Equal(t, 8080, newConfig.Port)
    
    // Verify retry configuration
    assert.True(t, newConfig.Retry.Enabled)
    assert.Equal(t, 5, newConfig.Retry.MaxAttempts)
    assert.Equal(t, 2*time.Second, newConfig.Retry.InitialWait)
    
    // Verify security configuration
    assert.NotNil(t, newConfig.Security)
    assert.True(t, newConfig.Security.TLS.Enabled)
    assert.Equal(t, "/path/to/cert.pem", newConfig.Security.TLS.CertFile)
}

func TestIntegrationInterface(t *testing.T) {
    cfg := DefaultConfig()
    cfg.Name = "test-integration"
    
    integration := &MyIntegration{config: cfg}
    
    // Test interface compliance
    var _ config.Integration = integration
    
    // Test lifecycle
    err := integration.Start()
    assert.NoError(t, err)
    
    health := integration.Health()
    assert.True(t, health.Healthy)
    
    stats := integration.Statistics()
    assert.NotZero(t, stats.StartTime)
    
    err = integration.Stop()
    assert.NoError(t, err)
}
```

### Integration Tests

```go
func TestConfigFileLoading(t *testing.T) {
    // Test new format
    newFormatFile := "testdata/new-format.yaml"
    cfg, err := LoadConfig(newFormatFile)
    assert.NoError(t, err)
    assert.Equal(t, "my-integration", cfg.Name)
    
    // Test legacy format
    legacyFormatFile := "testdata/legacy-format.yaml"
    cfg, err = LoadConfig(legacyFormatFile)
    assert.NoError(t, err)
    assert.Equal(t, "my-integration", cfg.Name) // Should be converted
}

func TestEndToEndMigration(t *testing.T) {
    // Start with legacy configuration
    legacyConfig := createLegacyConfig()
    
    // Convert to new format
    newConfig := ConvertLegacyConfig(legacyConfig)
    
    // Create integration with new config
    integration := &MyIntegration{config: newConfig}
    
    // Verify functionality is preserved
    err := integration.Start()
    assert.NoError(t, err)
    
    // Test specific functionality
    assert.Equal(t, legacyConfig.Port, integration.config.Port)
    
    if legacyConfig.TLSEnabled {
        assert.NotNil(t, integration.config.Security)
        assert.True(t, integration.config.Security.TLS.Enabled)
    }
    
    err = integration.Stop()
    assert.NoError(t, err)
}
```

## Phase 5: Deployment

### Deployment Strategies

#### Blue-Green Deployment
1. Deploy new version with framework to green environment
2. Test thoroughly with production data
3. Switch traffic to green environment
4. Keep blue environment as rollback option

#### Canary Deployment  
1. Deploy to small subset of instances
2. Monitor metrics and logs for issues
3. Gradually increase deployment percentage
4. Rollback if issues detected

#### Rolling Deployment
1. Update instances one by one
2. Verify each instance after update
3. Continue if healthy, rollback if issues
4. Complete when all instances updated

### Monitoring During Migration

```yaml
# Add monitoring alerts for migration
aliases:
  - alert: ConfigFrameworkMigrationIssue
    expr: increase(config_load_errors_total[5m]) > 0
    labels:
      severity: critical
    annotations:
      summary: Configuration loading errors during migration
      description: "{{ $labels.instance }} has configuration loading errors"
      
  - alert: IntegrationHealthDegraded
    expr: integration_health_status != 1
    for: 2m
    labels:
      severity: warning
    annotations:
      summary: Integration health degraded after migration
      description: "{{ $labels.integration }} health status is {{ $value }}"
```

### Rollback Procedures

```bash
#!/bin/bash
# rollback-config-migration.sh

set -e

echo "Starting configuration migration rollback..."

# 1. Stop new version
sudo systemctl stop my-integration

# 2. Restore legacy configuration
sudo cp /etc/my-integration/config.yaml.backup /etc/my-integration/config.yaml

# 3. Deploy previous version
sudo systemctl start my-integration-legacy

# 4. Verify service health
if curl -f http://localhost:8080/health; then
    echo "Rollback successful"
else
    echo "Rollback failed, manual intervention required"
    exit 1
fi
```

## Common Migration Issues

### Issue 1: Configuration Field Mapping

**Problem**: Legacy field doesn't map directly to framework field

**Solution**: Create custom conversion logic
```go
func convertCustomField(legacy string) config.RetryConfig {
    // Custom conversion logic
    parts := strings.Split(legacy, ",")
    return config.RetryConfig{
        Enabled:     len(parts) > 0,
        MaxAttempts: parseInt(parts[0]),
        InitialWait: parseDuration(parts[1]),
    }
}
```

### Issue 2: Validation Failures

**Problem**: Legacy configurations fail new validation rules

**Solution**: Add migration-specific validation exceptions
```go
func validateMigratedConfig(cfg Config, fromLegacy bool) error {
    if fromLegacy {
        // Relaxed validation for migrated configs
        if cfg.Name == "" {
            cfg.Name = "legacy-integration" // Set default
        }
    }
    return cfg.Validate()
}
```

### Issue 3: Default Value Conflicts

**Problem**: Framework defaults conflict with legacy behavior

**Solution**: Override defaults during migration
```go
func ConvertLegacyConfig(legacy LegacyConfig) Config {
    cfg := Config{} // Don't use defaults
    cfg.BaseConfig = config.BaseConfig{
        // Set fields explicitly to match legacy behavior
        Enabled: true,
        Timeout: 30 * time.Second, // Match legacy default
    }
    // ... rest of conversion
}
```

### Issue 4: Environment Variable Conflicts

**Problem**: New framework uses different environment variable names

**Solution**: Support both old and new env vars during transition
```go
func loadFromEnv(cfg *Config) {
    // Support both old and new environment variables
    if name := os.Getenv("SERVICE_NAME"); name != "" {
        cfg.Name = name
    } else if name := os.Getenv("INTEGRATION_NAME"); name != "" {
        cfg.Name = name
    }
}
```

## Migration Checklist

### Pre-Migration
- [ ] Audit current configuration structure
- [ ] Identify all configuration sources (files, env vars, APIs)
- [ ] Design new configuration schema
- [ ] Plan migration strategy
- [ ] Create rollback procedures
- [ ] Update documentation

### Implementation
- [ ] Add framework dependency
- [ ] Create new configuration struct
- [ ] Implement Integration interface
- [ ] Add compatibility layer (if needed)
- [ ] Update configuration loading
- [ ] Migrate configuration files

### Testing
- [ ] Unit tests for configuration conversion
- [ ] Integration tests for end-to-end functionality
- [ ] Load tests with new configuration
- [ ] Rollback testing
- [ ] Documentation testing

### Deployment
- [ ] Deploy to staging environment
- [ ] Validate with production-like data
- [ ] Set up monitoring and alerts
- [ ] Execute deployment strategy
- [ ] Monitor for issues
- [ ] Clean up legacy code (after confirmation)

### Post-Migration
- [ ] Remove compatibility layer
- [ ] Clean up legacy configuration files
- [ ] Update operational documentation
- [ ] Train operations team on new format
- [ ] Monitor long-term stability

## Timeline Template

### Week 1-2: Assessment and Planning
- Configuration audit
- Schema design
- Strategy selection
- Documentation updates

### Week 3-4: Implementation
- Framework integration
- Code changes
- Compatibility layer
- Unit testing

### Week 5-6: Testing and Validation
- Integration testing
- Performance testing
- Staging deployment
- Rollback testing

### Week 7-8: Production Deployment
- Canary deployment
- Monitoring and validation
- Full rollout
- Post-deployment cleanup

## Success Metrics

- **Zero downtime** during migration
- **No configuration-related errors** in logs
- **Health checks pass** consistently
- **Performance metrics** remain stable
- **Rollback capability** validated
- **Documentation** updated and accurate
- **Team confidence** in new system

This migration guide provides a structured approach to adopting the unified configuration framework while maintaining operational stability and minimizing risk.