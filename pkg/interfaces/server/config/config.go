package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/yairfalse/tapio/pkg/domain"
	"gopkg.in/yaml.v3"
)

// DefaultConfiguration returns a default server configuration
func DefaultConfiguration() *domain.Configuration {
	return &domain.Configuration{
		Server: domain.ServerConfig{
			Name:            "tapio-server",
			Version:         "1.0.0",
			Environment:     "development",
			LogLevel:        "info",
			MaxConnections:  1000,
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			ShutdownTimeout: 10 * time.Second,
			Features:        []string{"health", "metrics", "events"},
		},
		Endpoints: []domain.EndpointConfig{
			{
				Name:      "health",
				Protocol:  "http",
				Address:   "0.0.0.0",
				Port:      8080,
				Path:      "/health",
				Enabled:   true,
				Timeout:   5 * time.Second,
				RateLimit: 100,
			},
			{
				Name:      "metrics",
				Protocol:  "http",
				Address:   "0.0.0.0",
				Port:      8080,
				Path:      "/metrics",
				Enabled:   true,
				Timeout:   5 * time.Second,
				RateLimit: 50,
			},
			{
				Name:      "api",
				Protocol:  "http",
				Address:   "0.0.0.0",
				Port:      8080,
				Path:      "/api",
				Enabled:   true,
				Timeout:   30 * time.Second,
				RateLimit: 1000,
			},
			{
				Name:      "grpc",
				Protocol:  "grpc",
				Address:   "0.0.0.0",
				Port:      9090,
				Path:      "",
				Enabled:   true,
				Timeout:   60 * time.Second,
				RateLimit: 10000,
			},
		},
		Middleware: []domain.MiddlewareConfig{
			{
				Name:    "logging",
				Type:    "logging",
				Enabled: true,
				Config: map[string]interface{}{
					"level": "info",
				},
			},
			{
				Name:    "metrics",
				Type:    "metrics",
				Enabled: true,
				Config: map[string]interface{}{
					"collect_request_metrics":  true,
					"collect_response_metrics": true,
				},
			},
			{
				Name:    "validation",
				Type:    "validation",
				Enabled: true,
				Config: map[string]interface{}{
					"strict_mode": false,
				},
			},
		},
		Logging: domain.LoggingConfig{
			Level:    "info",
			Format:   "json",
			Output:   "stdout",
			Rotation: false,
			MaxSize:  100,
			MaxAge:   30,
			Compress: true,
		},
		Metrics: domain.MetricsConfig{
			Enabled:    true,
			Endpoint:   "/metrics",
			Interval:   30 * time.Second,
			Collectors: []string{"server", "endpoints", "connections"},
			Exporters:  []string{"prometheus"},
		},
		Security: domain.SecurityConfig{
			TLS: domain.TLSConfig{
				Enabled:  false,
				CertFile: "",
				KeyFile:  "",
				CAFile:   "",
			},
			Auth: domain.AuthConfig{
				Type:    "none",
				Enabled: false,
				Config:  map[string]interface{}{},
			},
			RateLimit: domain.RateLimitConfig{
				Enabled:    true,
				Requests:   1000,
				Window:     time.Minute,
				BurstLimit: 100,
			},
			CORS: domain.CORSConfig{
				Enabled:        true,
				AllowedOrigins: []string{"*"},
				AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
				AllowedHeaders: []string{"Content-Type", "Authorization"},
			},
		},
	}
}

// ProductionConfiguration returns a production-ready server configuration
func ProductionConfiguration() *domain.Configuration {
	config := DefaultConfiguration()

	// Production overrides
	config.Server.Environment = "production"
	config.Server.LogLevel = "warn"
	config.Server.MaxConnections = 10000

	// Enable TLS in production
	config.Security.TLS.Enabled = true
	config.Security.TLS.CertFile = "/etc/certs/server.crt"
	config.Security.TLS.KeyFile = "/etc/certs/server.key"

	// Enable authentication in production
	config.Security.Auth.Enabled = true
	config.Security.Auth.Type = "jwt"

	// Stricter CORS in production
	config.Security.CORS.AllowedOrigins = []string{
		"https://tapio.example.com",
		"https://api.tapio.example.com",
	}

	// Production logging
	config.Logging.Level = "warn"
	config.Logging.Output = "/var/log/tapio/server.log"
	config.Logging.Rotation = true
	config.Logging.MaxSize = 500
	config.Logging.MaxAge = 90

	// Production metrics
	config.Metrics.Interval = 60 * time.Second
	config.Metrics.Exporters = []string{"prometheus", "otel"}

	return config
}

// LoadConfiguration loads configuration from various sources
func LoadConfiguration(ctx context.Context) (*domain.Configuration, error) {
	config := DefaultConfiguration()

	// Load from environment variables
	if err := loadFromEnvironment(config); err != nil {
		return nil, fmt.Errorf("failed to load configuration from environment: %w", err)
	}

	// Load from configuration file if specified
	if configFile := os.Getenv("TAPIO_CONFIG_FILE"); configFile != "" {
		if err := loadFromFile(config, configFile); err != nil {
			return nil, fmt.Errorf("failed to load configuration from file %s: %w", configFile, err)
		}
	}

	// Validate configuration
	if err := ValidateConfiguration(config); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return config, nil
}

// loadFromEnvironment loads configuration from environment variables
func loadFromEnvironment(config *domain.Configuration) error {
	// Server configuration
	if name := os.Getenv("TAPIO_SERVER_NAME"); name != "" {
		config.Server.Name = name
	}
	if version := os.Getenv("TAPIO_SERVER_VERSION"); version != "" {
		config.Server.Version = version
	}
	if env := os.Getenv("TAPIO_ENVIRONMENT"); env != "" {
		config.Server.Environment = env
	}
	if logLevel := os.Getenv("TAPIO_LOG_LEVEL"); logLevel != "" {
		config.Server.LogLevel = logLevel
		config.Logging.Level = logLevel
	}

	// Security configuration
	if os.Getenv("TAPIO_TLS_ENABLED") == "true" {
		config.Security.TLS.Enabled = true
	}
	if certFile := os.Getenv("TAPIO_TLS_CERT_FILE"); certFile != "" {
		config.Security.TLS.CertFile = certFile
	}
	if keyFile := os.Getenv("TAPIO_TLS_KEY_FILE"); keyFile != "" {
		config.Security.TLS.KeyFile = keyFile
	}
	if caFile := os.Getenv("TAPIO_TLS_CA_FILE"); caFile != "" {
		config.Security.TLS.CAFile = caFile
	}

	// Authentication configuration
	if os.Getenv("TAPIO_AUTH_ENABLED") == "true" {
		config.Security.Auth.Enabled = true
	}
	if authType := os.Getenv("TAPIO_AUTH_TYPE"); authType != "" {
		config.Security.Auth.Type = authType
	}

	// Metrics configuration
	if os.Getenv("TAPIO_METRICS_ENABLED") == "false" {
		config.Metrics.Enabled = false
	}
	if endpoint := os.Getenv("TAPIO_METRICS_ENDPOINT"); endpoint != "" {
		config.Metrics.Endpoint = endpoint
	}

	// CORS configuration
	if origins := os.Getenv("TAPIO_CORS_ALLOWED_ORIGINS"); origins != "" {
		config.Security.CORS.AllowedOrigins = strings.Split(origins, ",")
	}

	return nil
}

// loadFromFile loads configuration from a file
func loadFromFile(config *domain.Configuration, filename string) error {
	// Check if file exists
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return fmt.Errorf("configuration file not found: %s", filename)
	}

	// Determine file format based on extension
	ext := strings.ToLower(filepath.Ext(filename))

	switch ext {
	case ".json":
		return loadFromJSONFile(config, filename)
	case ".yaml", ".yml":
		return loadFromYAMLFile(config, filename)
	case ".toml":
		return loadFromTOMLFile(config, filename)
	default:
		return fmt.Errorf("unsupported configuration file format: %s", ext)
	}
}

// loadFromJSONFile loads configuration from a JSON file
func loadFromJSONFile(config *domain.Configuration, filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read JSON file %s: %w", filename, err)
	}

	// Parse JSON into a temporary structure
	var jsonConfig domain.Configuration
	if err := json.Unmarshal(data, &jsonConfig); err != nil {
		return fmt.Errorf("failed to parse JSON file %s: %w", filename, err)
	}

	// Merge with existing configuration
	mergeConfigurations(config, &jsonConfig)

	return nil
}

// loadFromYAMLFile loads configuration from a YAML file
func loadFromYAMLFile(config *domain.Configuration, filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read YAML file %s: %w", filename, err)
	}

	// Parse YAML into a temporary structure
	var yamlConfig domain.Configuration
	if err := yaml.Unmarshal(data, &yamlConfig); err != nil {
		return fmt.Errorf("failed to parse YAML file %s: %w", filename, err)
	}

	// Merge with existing configuration
	mergeConfigurations(config, &yamlConfig)

	return nil
}

// loadFromTOMLFile loads configuration from a TOML file
func loadFromTOMLFile(config *domain.Configuration, filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read TOML file %s: %w", filename, err)
	}

	// Parse TOML into a temporary structure
	var tomlConfig domain.Configuration
	if err := toml.Unmarshal(data, &tomlConfig); err != nil {
		return fmt.Errorf("failed to parse TOML file %s: %w", filename, err)
	}

	// Merge with existing configuration
	mergeConfigurations(config, &tomlConfig)

	return nil
}

// mergeConfigurations merges source configuration into target configuration
// Only non-zero values from source are applied to target
func mergeConfigurations(target, source *domain.Configuration) {
	// Merge server configuration
	if source.Server.Name != "" {
		target.Server.Name = source.Server.Name
	}
	if source.Server.Version != "" {
		target.Server.Version = source.Server.Version
	}
	if source.Server.Environment != "" {
		target.Server.Environment = source.Server.Environment
	}
	if source.Server.LogLevel != "" {
		target.Server.LogLevel = source.Server.LogLevel
	}
	if source.Server.MaxConnections > 0 {
		target.Server.MaxConnections = source.Server.MaxConnections
	}
	if source.Server.ReadTimeout > 0 {
		target.Server.ReadTimeout = source.Server.ReadTimeout
	}
	if source.Server.WriteTimeout > 0 {
		target.Server.WriteTimeout = source.Server.WriteTimeout
	}
	if source.Server.ShutdownTimeout > 0 {
		target.Server.ShutdownTimeout = source.Server.ShutdownTimeout
	}
	if len(source.Server.Features) > 0 {
		target.Server.Features = source.Server.Features
	}

	// Merge endpoints
	if len(source.Endpoints) > 0 {
		target.Endpoints = source.Endpoints
	}

	// Merge middleware
	if len(source.Middleware) > 0 {
		target.Middleware = source.Middleware
	}

	// Merge logging configuration
	if source.Logging.Level != "" {
		target.Logging.Level = source.Logging.Level
	}
	if source.Logging.Format != "" {
		target.Logging.Format = source.Logging.Format
	}
	if source.Logging.Output != "" {
		target.Logging.Output = source.Logging.Output
	}
	if source.Logging.MaxSize > 0 {
		target.Logging.MaxSize = source.Logging.MaxSize
	}
	if source.Logging.MaxAge > 0 {
		target.Logging.MaxAge = source.Logging.MaxAge
	}
	target.Logging.Rotation = source.Logging.Rotation
	target.Logging.Compress = source.Logging.Compress

	// Merge metrics configuration
	target.Metrics.Enabled = source.Metrics.Enabled
	if source.Metrics.Endpoint != "" {
		target.Metrics.Endpoint = source.Metrics.Endpoint
	}
	if source.Metrics.Interval > 0 {
		target.Metrics.Interval = source.Metrics.Interval
	}
	if len(source.Metrics.Collectors) > 0 {
		target.Metrics.Collectors = source.Metrics.Collectors
	}
	if len(source.Metrics.Exporters) > 0 {
		target.Metrics.Exporters = source.Metrics.Exporters
	}

	// Merge security configuration only if source has meaningful values
	if source.Security.TLS.Enabled || source.Security.TLS.CertFile != "" || source.Security.TLS.KeyFile != "" || source.Security.TLS.CAFile != "" {
		target.Security.TLS.Enabled = source.Security.TLS.Enabled
		if source.Security.TLS.CertFile != "" {
			target.Security.TLS.CertFile = source.Security.TLS.CertFile
		}
		if source.Security.TLS.KeyFile != "" {
			target.Security.TLS.KeyFile = source.Security.TLS.KeyFile
		}
		if source.Security.TLS.CAFile != "" {
			target.Security.TLS.CAFile = source.Security.TLS.CAFile
		}
	}

	if source.Security.Auth.Enabled || source.Security.Auth.Type != "" || len(source.Security.Auth.Config) > 0 {
		target.Security.Auth.Enabled = source.Security.Auth.Enabled
		if source.Security.Auth.Type != "" {
			target.Security.Auth.Type = source.Security.Auth.Type
		}
		if len(source.Security.Auth.Config) > 0 {
			target.Security.Auth.Config = source.Security.Auth.Config
		}
	}

	if source.Security.RateLimit.Enabled || source.Security.RateLimit.Requests > 0 || source.Security.RateLimit.Window > 0 || source.Security.RateLimit.BurstLimit > 0 {
		target.Security.RateLimit.Enabled = source.Security.RateLimit.Enabled
		if source.Security.RateLimit.Requests > 0 {
			target.Security.RateLimit.Requests = source.Security.RateLimit.Requests
		}
		if source.Security.RateLimit.Window > 0 {
			target.Security.RateLimit.Window = source.Security.RateLimit.Window
		}
		if source.Security.RateLimit.BurstLimit > 0 {
			target.Security.RateLimit.BurstLimit = source.Security.RateLimit.BurstLimit
		}
	}

	if source.Security.CORS.Enabled || len(source.Security.CORS.AllowedOrigins) > 0 || len(source.Security.CORS.AllowedMethods) > 0 || len(source.Security.CORS.AllowedHeaders) > 0 {
		target.Security.CORS.Enabled = source.Security.CORS.Enabled
		if len(source.Security.CORS.AllowedOrigins) > 0 {
			target.Security.CORS.AllowedOrigins = source.Security.CORS.AllowedOrigins
		}
		if len(source.Security.CORS.AllowedMethods) > 0 {
			target.Security.CORS.AllowedMethods = source.Security.CORS.AllowedMethods
		}
		if len(source.Security.CORS.AllowedHeaders) > 0 {
			target.Security.CORS.AllowedHeaders = source.Security.CORS.AllowedHeaders
		}
	}
}

// ValidateConfiguration validates a configuration
func ValidateConfiguration(config *domain.Configuration) error {
	errors := domain.NewValidationErrors()

	// Validate server configuration
	if err := validateServerConfig(&config.Server); err != nil {
		errors.Add(err)
	}

	// Validate endpoints
	for i, endpoint := range config.Endpoints {
		if err := validateEndpointConfig(&endpoint); err != nil {
			errors.Add(fmt.Errorf("endpoint %d: %w", i, err))
		}
	}

	// Validate middleware
	for i, middleware := range config.Middleware {
		if err := validateMiddlewareConfig(&middleware); err != nil {
			errors.Add(fmt.Errorf("middleware %d: %w", i, err))
		}
	}

	// Validate logging configuration
	if err := validateLoggingConfig(&config.Logging); err != nil {
		errors.Add(err)
	}

	// Validate metrics configuration
	if err := validateMetricsConfig(&config.Metrics); err != nil {
		errors.Add(err)
	}

	// Validate security configuration
	if err := validateSecurityConfig(&config.Security); err != nil {
		errors.Add(err)
	}

	return errors.ToError()
}

// validateServerConfig validates server configuration
func validateServerConfig(config *domain.ServerConfig) error {
	errors := domain.NewValidationErrors()

	if err := domain.ValidateNotEmpty(config.Name, "server.name"); err != nil {
		errors.Add(err)
	}

	if err := domain.ValidateNotEmpty(config.Version, "server.version"); err != nil {
		errors.Add(err)
	}

	if err := domain.ValidateNotEmpty(config.Environment, "server.environment"); err != nil {
		errors.Add(err)
	}

	if err := domain.ValidatePositive(config.MaxConnections, "server.max_connections"); err != nil {
		errors.Add(err)
	}

	if config.ReadTimeout <= 0 {
		errors.Add(domain.ErrDataValidationFailed("server.read_timeout must be positive"))
	}

	if config.WriteTimeout <= 0 {
		errors.Add(domain.ErrDataValidationFailed("server.write_timeout must be positive"))
	}

	if config.ShutdownTimeout <= 0 {
		errors.Add(domain.ErrDataValidationFailed("server.shutdown_timeout must be positive"))
	}

	// Validate log level
	validLogLevels := []string{"debug", "info", "warn", "error"}
	if !contains(validLogLevels, config.LogLevel) {
		errors.Add(domain.ErrDataValidationFailed(fmt.Sprintf("server.log_level must be one of: %s", strings.Join(validLogLevels, ", "))))
	}

	return errors.ToError()
}

// validateEndpointConfig validates endpoint configuration
func validateEndpointConfig(config *domain.EndpointConfig) error {
	errors := domain.NewValidationErrors()

	if err := domain.ValidateNotEmpty(config.Name, "endpoint.name"); err != nil {
		errors.Add(err)
	}

	if err := domain.ValidateNotEmpty(config.Protocol, "endpoint.protocol"); err != nil {
		errors.Add(err)
	}

	if err := domain.ValidateNotEmpty(config.Address, "endpoint.address"); err != nil {
		errors.Add(err)
	}

	if err := domain.ValidateRange(config.Port, 1, 65535, "endpoint.port"); err != nil {
		errors.Add(err)
	}

	if config.Timeout <= 0 {
		errors.Add(domain.ErrDataValidationFailed("endpoint.timeout must be positive"))
	}

	if config.RateLimit < 0 {
		errors.Add(domain.ErrDataValidationFailed("endpoint.rate_limit must be non-negative"))
	}

	// Validate protocol
	validProtocols := []string{"http", "https", "grpc", "grpcs"}
	if !contains(validProtocols, config.Protocol) {
		errors.Add(domain.ErrDataValidationFailed(fmt.Sprintf("endpoint.protocol must be one of: %s", strings.Join(validProtocols, ", "))))
	}

	return errors.ToError()
}

// validateMiddlewareConfig validates middleware configuration
func validateMiddlewareConfig(config *domain.MiddlewareConfig) error {
	errors := domain.NewValidationErrors()

	if err := domain.ValidateNotEmpty(config.Name, "middleware.name"); err != nil {
		errors.Add(err)
	}

	if err := domain.ValidateNotEmpty(config.Type, "middleware.type"); err != nil {
		errors.Add(err)
	}

	// Validate middleware type
	validTypes := []string{"logging", "metrics", "validation", "security", "cors", "rate_limit"}
	if !contains(validTypes, config.Type) {
		errors.Add(domain.ErrDataValidationFailed(fmt.Sprintf("middleware.type must be one of: %s", strings.Join(validTypes, ", "))))
	}

	return errors.ToError()
}

// validateLoggingConfig validates logging configuration
func validateLoggingConfig(config *domain.LoggingConfig) error {
	errors := domain.NewValidationErrors()

	// Validate log level
	validLevels := []string{"debug", "info", "warn", "error"}
	if !contains(validLevels, config.Level) {
		errors.Add(domain.ErrDataValidationFailed(fmt.Sprintf("logging.level must be one of: %s", strings.Join(validLevels, ", "))))
	}

	// Validate log format
	validFormats := []string{"json", "text", "structured"}
	if !contains(validFormats, config.Format) {
		errors.Add(domain.ErrDataValidationFailed(fmt.Sprintf("logging.format must be one of: %s", strings.Join(validFormats, ", "))))
	}

	if config.MaxSize <= 0 {
		errors.Add(domain.ErrDataValidationFailed("logging.max_size must be positive"))
	}

	if config.MaxAge <= 0 {
		errors.Add(domain.ErrDataValidationFailed("logging.max_age must be positive"))
	}

	return errors.ToError()
}

// validateMetricsConfig validates metrics configuration
func validateMetricsConfig(config *domain.MetricsConfig) error {
	errors := domain.NewValidationErrors()

	if config.Enabled {
		if err := domain.ValidateNotEmpty(config.Endpoint, "metrics.endpoint"); err != nil {
			errors.Add(err)
		}

		if config.Interval <= 0 {
			errors.Add(domain.ErrDataValidationFailed("metrics.interval must be positive"))
		}

		if len(config.Collectors) == 0 {
			errors.Add(domain.ErrDataValidationFailed("metrics.collectors cannot be empty when metrics are enabled"))
		}

		if len(config.Exporters) == 0 {
			errors.Add(domain.ErrDataValidationFailed("metrics.exporters cannot be empty when metrics are enabled"))
		}
	}

	return errors.ToError()
}

// validateSecurityConfig validates security configuration
func validateSecurityConfig(config *domain.SecurityConfig) error {
	errors := domain.NewValidationErrors()

	// Validate TLS configuration
	if config.TLS.Enabled {
		if err := domain.ValidateNotEmpty(config.TLS.CertFile, "security.tls.cert_file"); err != nil {
			errors.Add(err)
		}

		if err := domain.ValidateNotEmpty(config.TLS.KeyFile, "security.tls.key_file"); err != nil {
			errors.Add(err)
		}
	}

	// Validate auth configuration
	if config.Auth.Enabled {
		if err := domain.ValidateNotEmpty(config.Auth.Type, "security.auth.type"); err != nil {
			errors.Add(err)
		}

		validAuthTypes := []string{"none", "basic", "jwt", "oauth2"}
		if !contains(validAuthTypes, config.Auth.Type) {
			errors.Add(domain.ErrDataValidationFailed(fmt.Sprintf("security.auth.type must be one of: %s", strings.Join(validAuthTypes, ", "))))
		}
	}

	// Validate rate limiting configuration
	if config.RateLimit.Enabled {
		if config.RateLimit.Requests <= 0 {
			errors.Add(domain.ErrDataValidationFailed("security.rate_limit.requests must be positive"))
		}

		if config.RateLimit.Window <= 0 {
			errors.Add(domain.ErrDataValidationFailed("security.rate_limit.window must be positive"))
		}

		if config.RateLimit.BurstLimit <= 0 {
			errors.Add(domain.ErrDataValidationFailed("security.rate_limit.burst_limit must be positive"))
		}
	}

	return errors.ToError()
}

// Helper functions

// contains checks if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// GetEnvironmentSpecificConfig returns configuration for a specific environment
func GetEnvironmentSpecificConfig(environment string) (*domain.Configuration, error) {
	switch environment {
	case "development", "dev":
		return DefaultConfiguration(), nil
	case "production", "prod":
		return ProductionConfiguration(), nil
	case "testing", "test":
		config := DefaultConfiguration()
		config.Server.Environment = "testing"
		config.Server.LogLevel = "debug"
		config.Logging.Level = "debug"
		config.Metrics.Enabled = false
		return config, nil
	default:
		return nil, domain.ErrInvalidConfiguration(fmt.Sprintf("unknown environment: %s", environment))
	}
}

// ConfigurationManager provides configuration management functionality
type ConfigurationManager struct {
	config *domain.Configuration
	logger domain.Logger
}

// NewConfigurationManager creates a new configuration manager
func NewConfigurationManager(logger domain.Logger) *ConfigurationManager {
	return &ConfigurationManager{
		logger: logger,
	}
}

// LoadConfiguration loads configuration from various sources
func (m *ConfigurationManager) LoadConfiguration(ctx context.Context) (*domain.Configuration, error) {
	config, err := LoadConfiguration(ctx)
	if err != nil {
		return nil, err
	}

	m.config = config

	if m.logger != nil {
		m.logger.Info(ctx, fmt.Sprintf("configuration loaded for environment: %s", config.Server.Environment))
	}

	return config, nil
}

// ReloadConfiguration reloads configuration
func (m *ConfigurationManager) ReloadConfiguration(ctx context.Context) error {
	config, err := LoadConfiguration(ctx)
	if err != nil {
		return err
	}

	m.config = config

	if m.logger != nil {
		m.logger.Info(ctx, "configuration reloaded")
	}

	return nil
}

// UpdateConfiguration updates the configuration
func (m *ConfigurationManager) UpdateConfiguration(ctx context.Context, config *domain.Configuration) error {
	if err := ValidateConfiguration(config); err != nil {
		return err
	}

	m.config = config

	if m.logger != nil {
		m.logger.Info(ctx, "configuration updated")
	}

	return nil
}

// UpdateServerConfig updates server configuration
func (m *ConfigurationManager) UpdateServerConfig(ctx context.Context, config *domain.ServerConfig) error {
	if err := validateServerConfig(config); err != nil {
		return err
	}

	m.config.Server = *config

	if m.logger != nil {
		m.logger.Info(ctx, "server configuration updated")
	}

	return nil
}

// UpdateEndpointConfig updates endpoint configuration
func (m *ConfigurationManager) UpdateEndpointConfig(ctx context.Context, endpointName string, config *domain.EndpointConfig) error {
	if err := validateEndpointConfig(config); err != nil {
		return err
	}

	// Find and update the endpoint
	for i, endpoint := range m.config.Endpoints {
		if endpoint.Name == endpointName {
			m.config.Endpoints[i] = *config

			if m.logger != nil {
				m.logger.Info(ctx, fmt.Sprintf("endpoint configuration updated: %s", endpointName))
			}

			return nil
		}
	}

	return domain.ErrResourceNotFound(fmt.Sprintf("endpoint not found: %s", endpointName))
}

// ValidateConfiguration validates configuration
func (m *ConfigurationManager) ValidateConfiguration(ctx context.Context, config *domain.Configuration) error {
	return ValidateConfiguration(config)
}

// GetConfiguration returns the current configuration
func (m *ConfigurationManager) GetConfiguration() *domain.Configuration {
	if m.config == nil {
		return DefaultConfiguration()
	}
	return m.config
}
