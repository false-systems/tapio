/*
Package config provides a unified configuration framework for all Tapio integrations.

# Overview

This package standardizes configuration across all Tapio integrations by providing:
- Common base configuration fields
- Aspect-specific configurations (security, resilience, monitoring)
- Fluent builder API for configuration construction
- File-based configuration loading (YAML/JSON)
- Built-in validation and defaults

# Architecture

The configuration framework follows a composition pattern where integrations
embed BaseConfig and optionally include aspect-specific configurations:

	type MyIntegrationConfig struct {
		config.BaseConfig `yaml:",inline" json:",inline"`
		
		// Custom fields
		Endpoint string `yaml:"endpoint" json:"endpoint"`
		BatchSize int   `yaml:"batch_size" json:"batch_size"`
		
		// Optional aspects
		Security   *config.SecurityConfig   `yaml:"security,omitempty"`
		Resilience *config.ResilienceConfig `yaml:"resilience,omitempty"`
		Monitoring *config.MonitoringConfig `yaml:"monitoring,omitempty"`
	}

# Base Configuration

BaseConfig provides common fields that all integrations need:
- Identity: name, type, version, environment
- Operations: enabled flag, timeout settings
- Retry: configurable retry behavior
- Observability: tracing, metrics, logging settings
- Resource limits: connections, memory, CPU, rate limits
- Labels and metadata: for operational tagging

# Aspect-Specific Configurations

## Security Configuration

SecurityConfig provides comprehensive security settings:
- TLS: certificate management, cipher suites, version control
- Authentication: JWT, OAuth2, API keys, basic auth, mTLS
- Rate limiting: global, per-endpoint, per-user, per-IP limits
- Network security: CIDR filtering, trusted proxies, connection limits

## Resilience Configuration

ResilienceConfig implements common resilience patterns:
- Circuit breakers: failure detection and recovery
- Load shedding: adaptive and threshold-based load control
- Timeouts: configurable timeouts with adaptive behavior
- Bulkhead isolation: resource isolation and queue management
- Health checks: endpoint monitoring and failure detection

## Monitoring Configuration

MonitoringConfig provides observability features:
- Metrics: Prometheus, OTEL, StatsD integration
- Performance: SLOs, thresholds, profiling
- Alerting: rules, providers, grouping, throttling
- Profiling: CPU, memory, goroutine tracking

# Builder Pattern

The Builder provides a fluent API for programmatic configuration:

	cfg, err := config.NewBuilder().
		WithName("my-integration").
		WithType("collector").
		WithEnvironment("production").
		WithSecurity(securityConfig).
		WithCustom("endpoint", "https://api.example.com").
		Build()

# File-based Configuration

Configurations can be loaded from YAML or JSON files:

	cfg, err := config.LoadFromFile("config.yaml")
	if err != nil {
		log.Fatal(err)
	}
	
	// Validate configuration
	if err := config.Validate(cfg); err != nil {
		log.Fatal(err)
	}

# Integration Interface

All integrations should implement the Integration interface:

	type Integration interface {
		// Lifecycle management
		Start() error
		Stop() error
		Reload(config interface{}) error
		
		// Health and monitoring
		Health() HealthStatus
		Statistics() Statistics
		
		// Configuration access
		GetConfig() interface{}
		ValidateConfig() error
	}

# Usage Patterns

## Basic Integration

	type SimpleIntegration struct {
		config SimpleConfig
		// ... other fields
	}
	
	func (s *SimpleIntegration) Start() error {
		// Use s.config.BaseConfig fields
		if !s.config.Enabled {
			return fmt.Errorf("integration disabled")
		}
		// ... start logic
	}

## Secure Integration

	type SecureIntegration struct {
		config SecureConfig
		tlsConfig *tls.Config
	}
	
	func NewSecureIntegration(cfg SecureConfig) (*SecureIntegration, error) {
		var tlsConfig *tls.Config
		if cfg.Security != nil && cfg.Security.TLS.Enabled {
			// Configure TLS from cfg.Security.TLS
		}
		return &SecureIntegration{config: cfg, tlsConfig: tlsConfig}, nil
	}

## Resilient Integration

	type ResilientIntegration struct {
		config ResilientConfig
		circuitBreaker *CircuitBreaker
	}
	
	func (r *ResilientIntegration) processRequest(req Request) error {
		if r.config.Resilience != nil && r.config.Resilience.CircuitBreaker.Enabled {
			return r.circuitBreaker.Call(func() error {
				// Process request with circuit breaker protection
				return r.doProcess(req)
			})
		}
		return r.doProcess(req)
	}

# Validation

The framework provides built-in validation for common configuration errors:
- Required fields (name, type)
- TLS certificate validation
- Positive thresholds for circuit breakers
- Valid metrics providers
- CIDR format validation

# Best Practices

1. Always embed BaseConfig in your integration configuration struct
2. Use YAML tags for file-based configuration
3. Provide sensible defaults in your constructor
4. Implement the Integration interface for consistency
5. Use the builder pattern for programmatic configuration
6. Validate configuration before use
7. Support configuration reloading for operational flexibility
8. Include comprehensive health checks and statistics

# Migration Guide

For existing integrations:

1. Create a new config struct embedding BaseConfig
2. Move existing config fields to the new struct
3. Update constructors to use the new config
4. Implement the Integration interface
5. Add health checks and statistics
6. Update configuration loading to use the framework
7. Add tests using the provided patterns

# Examples

See the examples/ directory for complete implementations:
- sample_integration.go: Full integration implementation
- sample_integration_test.go: Comprehensive test suite
- sample_config.yaml: Complete configuration example

*/
package config