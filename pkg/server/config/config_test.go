package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/server/domain"
)

func TestDefaultConfiguration(t *testing.T) {
	config := DefaultConfiguration()

	// Test server configuration
	if config.Server.Name != "tapio-server" {
		t.Errorf("Expected server name 'tapio-server', got '%s'", config.Server.Name)
	}
	if config.Server.Version != "1.0.0" {
		t.Errorf("Expected server version '1.0.0', got '%s'", config.Server.Version)
	}
	if config.Server.Environment != "development" {
		t.Errorf("Expected environment 'development', got '%s'", config.Server.Environment)
	}
	if config.Server.MaxConnections != 1000 {
		t.Errorf("Expected max connections 1000, got %d", config.Server.MaxConnections)
	}

	// Test endpoints
	if len(config.Endpoints) != 4 {
		t.Errorf("Expected 4 endpoints, got %d", len(config.Endpoints))
	}

	// Test health endpoint
	healthEndpoint := config.Endpoints[0]
	if healthEndpoint.Name != "health" {
		t.Errorf("Expected health endpoint name 'health', got '%s'", healthEndpoint.Name)
	}
	if healthEndpoint.Port != 8080 {
		t.Errorf("Expected health endpoint port 8080, got %d", healthEndpoint.Port)
	}

	// Test security configuration
	if config.Security.TLS.Enabled {
		t.Error("Expected TLS to be disabled in default configuration")
	}
	if config.Security.Auth.Enabled {
		t.Error("Expected auth to be disabled in default configuration")
	}
	if !config.Security.RateLimit.Enabled {
		t.Error("Expected rate limiting to be enabled in default configuration")
	}
}

func TestProductionConfiguration(t *testing.T) {
	config := ProductionConfiguration()

	// Test production overrides
	if config.Server.Environment != "production" {
		t.Errorf("Expected environment 'production', got '%s'", config.Server.Environment)
	}
	if config.Server.LogLevel != "warn" {
		t.Errorf("Expected log level 'warn', got '%s'", config.Server.LogLevel)
	}
	if config.Server.MaxConnections != 10000 {
		t.Errorf("Expected max connections 10000, got %d", config.Server.MaxConnections)
	}

	// Test TLS enabled
	if !config.Security.TLS.Enabled {
		t.Error("Expected TLS to be enabled in production configuration")
	}
	if config.Security.TLS.CertFile != "/etc/certs/server.crt" {
		t.Errorf("Expected cert file '/etc/certs/server.crt', got '%s'", config.Security.TLS.CertFile)
	}

	// Test auth enabled
	if !config.Security.Auth.Enabled {
		t.Error("Expected auth to be enabled in production configuration")
	}
	if config.Security.Auth.Type != "jwt" {
		t.Errorf("Expected auth type 'jwt', got '%s'", config.Security.Auth.Type)
	}

	// Test CORS restrictions
	expectedOrigins := []string{"https://tapio.example.com", "https://api.tapio.example.com"}
	if len(config.Security.CORS.AllowedOrigins) != len(expectedOrigins) {
		t.Errorf("Expected %d allowed origins, got %d", len(expectedOrigins), len(config.Security.CORS.AllowedOrigins))
	}
}

func TestLoadFromEnvironment(t *testing.T) {
	// Set environment variables
	os.Setenv("TAPIO_SERVER_NAME", "test-server")
	os.Setenv("TAPIO_ENVIRONMENT", "testing")
	os.Setenv("TAPIO_LOG_LEVEL", "debug")
	os.Setenv("TAPIO_TLS_ENABLED", "true")
	os.Setenv("TAPIO_TLS_CERT_FILE", "/path/to/cert.pem")
	os.Setenv("TAPIO_AUTH_ENABLED", "true")
	os.Setenv("TAPIO_AUTH_TYPE", "basic")
	os.Setenv("TAPIO_CORS_ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:8080")

	defer func() {
		// Clean up environment variables
		os.Unsetenv("TAPIO_SERVER_NAME")
		os.Unsetenv("TAPIO_ENVIRONMENT")
		os.Unsetenv("TAPIO_LOG_LEVEL")
		os.Unsetenv("TAPIO_TLS_ENABLED")
		os.Unsetenv("TAPIO_TLS_CERT_FILE")
		os.Unsetenv("TAPIO_AUTH_ENABLED")
		os.Unsetenv("TAPIO_AUTH_TYPE")
		os.Unsetenv("TAPIO_CORS_ALLOWED_ORIGINS")
	}()

	config := DefaultConfiguration()
	err := loadFromEnvironment(config)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Test server name override
	if config.Server.Name != "test-server" {
		t.Errorf("Expected server name 'test-server', got '%s'", config.Server.Name)
	}

	// Test environment override
	if config.Server.Environment != "testing" {
		t.Errorf("Expected environment 'testing', got '%s'", config.Server.Environment)
	}

	// Test log level override
	if config.Server.LogLevel != "debug" {
		t.Errorf("Expected log level 'debug', got '%s'", config.Server.LogLevel)
	}
	if config.Logging.Level != "debug" {
		t.Errorf("Expected logging level 'debug', got '%s'", config.Logging.Level)
	}

	// Test TLS configuration
	if !config.Security.TLS.Enabled {
		t.Error("Expected TLS to be enabled")
	}
	if config.Security.TLS.CertFile != "/path/to/cert.pem" {
		t.Errorf("Expected cert file '/path/to/cert.pem', got '%s'", config.Security.TLS.CertFile)
	}

	// Test auth configuration
	if !config.Security.Auth.Enabled {
		t.Error("Expected auth to be enabled")
	}
	if config.Security.Auth.Type != "basic" {
		t.Errorf("Expected auth type 'basic', got '%s'", config.Security.Auth.Type)
	}

	// Test CORS configuration
	expectedOrigins := []string{"http://localhost:3000", "http://localhost:8080"}
	if len(config.Security.CORS.AllowedOrigins) != len(expectedOrigins) {
		t.Errorf("Expected %d allowed origins, got %d", len(expectedOrigins), len(config.Security.CORS.AllowedOrigins))
	}
	for i, origin := range expectedOrigins {
		if config.Security.CORS.AllowedOrigins[i] != origin {
			t.Errorf("Expected origin '%s', got '%s'", origin, config.Security.CORS.AllowedOrigins[i])
		}
	}
}

func TestLoadFromJSONFile(t *testing.T) {
	// Create temporary JSON file
	tempDir := t.TempDir()
	jsonFile := filepath.Join(tempDir, "config.json")
	
	jsonContent := `{
		"server": {
			"name": "json-server",
			"version": "2.0.0",
			"environment": "staging",
			"logLevel": "info",
			"maxConnections": 5000
		},
		"security": {
			"tls": {
				"enabled": true,
				"certFile": "/etc/ssl/cert.pem",
				"keyFile": "/etc/ssl/key.pem"
			}
		}
	}`
	
	err := os.WriteFile(jsonFile, []byte(jsonContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write JSON file: %v", err)
	}

	config := DefaultConfiguration()
	err = loadFromJSONFile(config, jsonFile)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Test JSON overrides
	if config.Server.Name != "json-server" {
		t.Errorf("Expected server name 'json-server', got '%s'", config.Server.Name)
	}
	if config.Server.Version != "2.0.0" {
		t.Errorf("Expected server version '2.0.0', got '%s'", config.Server.Version)
	}
	if config.Server.Environment != "staging" {
		t.Errorf("Expected environment 'staging', got '%s'", config.Server.Environment)
	}
	if config.Server.MaxConnections != 5000 {
		t.Errorf("Expected max connections 5000, got %d", config.Server.MaxConnections)
	}
	if !config.Security.TLS.Enabled {
		t.Error("Expected TLS to be enabled")
	}
	if config.Security.TLS.CertFile != "/etc/ssl/cert.pem" {
		t.Errorf("Expected cert file '/etc/ssl/cert.pem', got '%s'", config.Security.TLS.CertFile)
	}
}

func TestLoadFromYAMLFile(t *testing.T) {
	// Create temporary YAML file
	tempDir := t.TempDir()
	yamlFile := filepath.Join(tempDir, "config.yaml")
	
	yamlContent := `
server:
  name: yaml-server
  version: 3.0.0
  environment: production
  loglevel: error
  maxconnections: 15000
security:
  auth:
    enabled: true
    type: oauth2
  ratelimit:
    enabled: true
    requests: 500
    window: 60000000000
`
	
	err := os.WriteFile(yamlFile, []byte(yamlContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write YAML file: %v", err)
	}

	config := DefaultConfiguration()
	err = loadFromYAMLFile(config, yamlFile)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Test YAML overrides
	if config.Server.Name != "yaml-server" {
		t.Errorf("Expected server name 'yaml-server', got '%s'", config.Server.Name)
	}
	if config.Server.Version != "3.0.0" {
		t.Errorf("Expected server version '3.0.0', got '%s'", config.Server.Version)
	}
	if config.Server.Environment != "production" {
		t.Errorf("Expected environment 'production', got '%s'", config.Server.Environment)
	}
	// Debug: Print actual values
	t.Logf("DEBUG: MaxConnections = %d", config.Server.MaxConnections)
	t.Logf("DEBUG: RateLimit.Requests = %d", config.Security.RateLimit.Requests)
	if config.Server.MaxConnections != 15000 {
		t.Errorf("Expected max connections 15000, got %d", config.Server.MaxConnections)
	}
	if !config.Security.Auth.Enabled {
		t.Error("Expected auth to be enabled")
	}
	if config.Security.Auth.Type != "oauth2" {
		t.Errorf("Expected auth type 'oauth2', got '%s'", config.Security.Auth.Type)
	}
	if config.Security.RateLimit.Requests != 500 {
		t.Errorf("Expected rate limit requests 500, got %d", config.Security.RateLimit.Requests)
	}
	if config.Security.RateLimit.Window != 60*time.Second {
		t.Errorf("Expected rate limit window 60s, got %v", config.Security.RateLimit.Window)
	}
}

func TestLoadFromTOMLFile(t *testing.T) {
	// Create temporary TOML file
	tempDir := t.TempDir()
	tomlFile := filepath.Join(tempDir, "config.toml")
	
	tomlContent := `
[server]
name = "toml-server"
version = "4.0.0"
environment = "development"
logLevel = "debug"
maxConnections = 2000

[security.tls]
enabled = true
certFile = "/certs/server.crt"
keyFile = "/certs/server.key"

[security.cors]
enabled = true
allowedOrigins = ["https://example.com", "https://api.example.com"]
allowedMethods = ["GET", "POST", "PUT"]
`
	
	err := os.WriteFile(tomlFile, []byte(tomlContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write TOML file: %v", err)
	}

	config := DefaultConfiguration()
	err = loadFromTOMLFile(config, tomlFile)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Test TOML overrides
	if config.Server.Name != "toml-server" {
		t.Errorf("Expected server name 'toml-server', got '%s'", config.Server.Name)
	}
	if config.Server.Version != "4.0.0" {
		t.Errorf("Expected server version '4.0.0', got '%s'", config.Server.Version)
	}
	if config.Server.MaxConnections != 2000 {
		t.Errorf("Expected max connections 2000, got %d", config.Server.MaxConnections)
	}
	if !config.Security.TLS.Enabled {
		t.Error("Expected TLS to be enabled")
	}
	if config.Security.TLS.CertFile != "/certs/server.crt" {
		t.Errorf("Expected cert file '/certs/server.crt', got '%s'", config.Security.TLS.CertFile)
	}
	if !config.Security.CORS.Enabled {
		t.Error("Expected CORS to be enabled")
	}
	expectedOrigins := []string{"https://example.com", "https://api.example.com"}
	if len(config.Security.CORS.AllowedOrigins) != len(expectedOrigins) {
		t.Errorf("Expected %d allowed origins, got %d", len(expectedOrigins), len(config.Security.CORS.AllowedOrigins))
	}
}

func TestLoadConfiguration(t *testing.T) {
	ctx := context.Background()
	
	// Test default configuration loading
	config, err := LoadConfiguration(ctx)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if config == nil {
		t.Error("Expected configuration, got nil")
	}
	
	// Test with environment variable config file
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "test-config.json")
	
	jsonContent := `{
		"server": {
			"name": "test-server",
			"environment": "testing"
		}
	}`
	
	err = os.WriteFile(configFile, []byte(jsonContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}
	
	os.Setenv("TAPIO_CONFIG_FILE", configFile)
	defer os.Unsetenv("TAPIO_CONFIG_FILE")
	
	config, err = LoadConfiguration(ctx)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if config.Server.Name != "test-server" {
		t.Errorf("Expected server name 'test-server', got '%s'", config.Server.Name)
	}
	if config.Server.Environment != "testing" {
		t.Errorf("Expected environment 'testing', got '%s'", config.Server.Environment)
	}
}

func TestValidateConfiguration(t *testing.T) {
	// Test valid configuration
	config := DefaultConfiguration()
	err := ValidateConfiguration(config)
	if err != nil {
		t.Errorf("Expected no error for valid configuration, got: %v", err)
	}
	
	// Test invalid server name
	config.Server.Name = ""
	err = ValidateConfiguration(config)
	if err == nil {
		t.Error("Expected error for empty server name")
	}
	
	// Test invalid port
	config = DefaultConfiguration()
	config.Endpoints[0].Port = 0
	err = ValidateConfiguration(config)
	if err == nil {
		t.Error("Expected error for invalid port")
	}
	
	// Test invalid log level
	config = DefaultConfiguration()
	config.Server.LogLevel = "invalid"
	err = ValidateConfiguration(config)
	if err == nil {
		t.Error("Expected error for invalid log level")
	}
}

func TestGetEnvironmentSpecificConfig(t *testing.T) {
	// Test development environment
	config, err := GetEnvironmentSpecificConfig("development")
	if err != nil {
		t.Errorf("Expected no error for development, got: %v", err)
	}
	if config.Server.Environment != "development" {
		t.Errorf("Expected environment 'development', got '%s'", config.Server.Environment)
	}
	
	// Test production environment
	config, err = GetEnvironmentSpecificConfig("production")
	if err != nil {
		t.Errorf("Expected no error for production, got: %v", err)
	}
	if config.Server.Environment != "production" {
		t.Errorf("Expected environment 'production', got '%s'", config.Server.Environment)
	}
	if !config.Security.TLS.Enabled {
		t.Error("Expected TLS to be enabled in production")
	}
	
	// Test testing environment
	config, err = GetEnvironmentSpecificConfig("testing")
	if err != nil {
		t.Errorf("Expected no error for testing, got: %v", err)
	}
	if config.Server.Environment != "testing" {
		t.Errorf("Expected environment 'testing', got '%s'", config.Server.Environment)
	}
	if config.Server.LogLevel != "debug" {
		t.Errorf("Expected log level 'debug', got '%s'", config.Server.LogLevel)
	}
	if config.Metrics.Enabled {
		t.Error("Expected metrics to be disabled in testing")
	}
	
	// Test invalid environment
	config, err = GetEnvironmentSpecificConfig("invalid")
	if err == nil {
		t.Error("Expected error for invalid environment")
	}
	if config != nil {
		t.Error("Expected nil config for invalid environment")
	}
}

func TestConfigurationManager(t *testing.T) {
	ctx := context.Background()
	manager := NewConfigurationManager(nil)
	
	// Test loading configuration
	config, err := manager.LoadConfiguration(ctx)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if config == nil {
		t.Error("Expected configuration, got nil")
	}
	
	// Test getting configuration
	retrievedConfig := manager.GetConfiguration()
	if retrievedConfig == nil {
		t.Error("Expected configuration, got nil")
	}
	if retrievedConfig.Server.Name != config.Server.Name {
		t.Errorf("Expected same server name, got different values")
	}
	
	// Test updating configuration
	newConfig := DefaultConfiguration()
	newConfig.Server.Name = "updated-server"
	err = manager.UpdateConfiguration(ctx, newConfig)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	
	retrievedConfig = manager.GetConfiguration()
	if retrievedConfig.Server.Name != "updated-server" {
		t.Errorf("Expected server name 'updated-server', got '%s'", retrievedConfig.Server.Name)
	}
	
	// Test updating server config
	serverConfig := &domain.ServerConfig{
		Name:            "new-server",
		Version:         "2.0.0",
		Environment:     "production",
		LogLevel:        "info",
		MaxConnections:  5000,
		ReadTimeout:     30 * time.Second,
		WriteTimeout:    30 * time.Second,
		ShutdownTimeout: 10 * time.Second,
	}
	err = manager.UpdateServerConfig(ctx, serverConfig)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	
	retrievedConfig = manager.GetConfiguration()
	if retrievedConfig.Server.Name != "new-server" {
		t.Errorf("Expected server name 'new-server', got '%s'", retrievedConfig.Server.Name)
	}
	if retrievedConfig.Server.Version != "2.0.0" {
		t.Errorf("Expected server version '2.0.0', got '%s'", retrievedConfig.Server.Version)
	}
}

func TestMergeConfigurations(t *testing.T) {
	target := DefaultConfiguration()
	source := &domain.Configuration{
		Server: domain.ServerConfig{
			Name:           "merged-server",
			Version:        "5.0.0",
			MaxConnections: 8000,
		},
		Security: domain.SecurityConfig{
			TLS: domain.TLSConfig{
				Enabled:  true,
				CertFile: "/new/cert.pem",
			},
		},
	}
	
	mergeConfigurations(target, source)
	
	// Test merged values
	if target.Server.Name != "merged-server" {
		t.Errorf("Expected server name 'merged-server', got '%s'", target.Server.Name)
	}
	if target.Server.Version != "5.0.0" {
		t.Errorf("Expected server version '5.0.0', got '%s'", target.Server.Version)
	}
	if target.Server.MaxConnections != 8000 {
		t.Errorf("Expected max connections 8000, got %d", target.Server.MaxConnections)
	}
	if !target.Security.TLS.Enabled {
		t.Error("Expected TLS to be enabled")
	}
	if target.Security.TLS.CertFile != "/new/cert.pem" {
		t.Errorf("Expected cert file '/new/cert.pem', got '%s'", target.Security.TLS.CertFile)
	}
	
	// Test that non-zero values from target are preserved
	if target.Server.Environment != "development" {
		t.Errorf("Expected environment 'development' to be preserved, got '%s'", target.Server.Environment)
	}
	if target.Server.LogLevel != "info" {
		t.Errorf("Expected log level 'info' to be preserved, got '%s'", target.Server.LogLevel)
	}
}

func TestLoadFromFileUnsupportedFormat(t *testing.T) {
	tempDir := t.TempDir()
	unsupportedFile := filepath.Join(tempDir, "config.xml")
	
	err := os.WriteFile(unsupportedFile, []byte("<config></config>"), 0644)
	if err != nil {
		t.Fatalf("Failed to write unsupported file: %v", err)
	}
	
	config := DefaultConfiguration()
	err = loadFromFile(config, unsupportedFile)
	if err == nil {
		t.Error("Expected error for unsupported file format")
	}
}

func TestLoadFromNonExistentFile(t *testing.T) {
	config := DefaultConfiguration()
	err := loadFromFile(config, "/nonexistent/config.json")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}