package server

import (
	"context"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/server/domain"
)

func TestNewServer(t *testing.T) {
	config := &domain.Configuration{
		Server: domain.ServerConfig{
			Name:            "test-server",
			Version:         "1.0.0",
			Environment:     "testing",
			LogLevel:        "debug",
			MaxConnections:  100,
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			ShutdownTimeout: 10 * time.Second,
		},
	}
	
	server, err := NewServer(WithConfiguration(config))
	if err != nil {
		t.Errorf("Expected no error when creating server, got: %v", err)
	}
	
	if server == nil {
		t.Error("Expected server to be created, got nil")
	}
	
	if server.config == nil {
		t.Error("Expected server config to be set, got nil")
	}
	
	if server.serverService == nil {
		t.Error("Expected server service to be set, got nil")
	}
	
	if server.requestHandler == nil {
		t.Error("Expected server request handler to be set, got nil")
	}
	
	if server.responseHandler == nil {
		t.Error("Expected server response handler to be set, got nil")
	}
	
	if server.middlewareManager == nil {
		t.Error("Expected server middleware manager to be set, got nil")
	}
}

func TestNewServerWithNilConfig(t *testing.T) {
	server, err := NewServer(WithConfiguration(nil))
	if err == nil {
		t.Error("Expected error when creating server with nil config")
	}
	
	if server != nil {
		t.Error("Expected no server to be created with nil config")
	}
}

func TestNewServerWithInvalidConfig(t *testing.T) {
	config := &domain.Configuration{
		Server: domain.ServerConfig{
			Name:            "", // Invalid empty name
			Version:         "1.0.0",
			Environment:     "testing",
			LogLevel:        "debug",
			MaxConnections:  100,
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			ShutdownTimeout: 10 * time.Second,
		},
	}
	
	server, err := NewServer(WithConfiguration(config))
	if err == nil {
		t.Error("Expected error when creating server with invalid config")
	}
	
	if server != nil {
		t.Error("Expected no server to be created with invalid config")
	}
}

func TestServerBuilder(t *testing.T) {
	builder := NewServerBuilder()
	
	if builder == nil {
		t.Error("Expected server builder to be created, got nil")
	}
	
	// Test with default configuration
	server, err := builder.Build()
	if err != nil {
		t.Errorf("Expected no error when building server with defaults, got: %v", err)
	}
	
	if server == nil {
		t.Error("Expected server to be built, got nil")
	}
	
	// Test with custom configuration
	config := &domain.Configuration{
		Server: domain.ServerConfig{
			Name:            "custom-server",
			Version:         "2.0.0",
			Environment:     "production",
			LogLevel:        "info",
			MaxConnections:  1000,
			ReadTimeout:     60 * time.Second,
			WriteTimeout:    60 * time.Second,
			ShutdownTimeout: 30 * time.Second,
		},
	}
	
	server, err = builder.WithConfig(config).Build()
	if err != nil {
		t.Errorf("Expected no error when building server with custom config, got: %v", err)
	}
	
	if server == nil {
		t.Error("Expected server to be built with custom config, got nil")
	}
	
	if server.config.Server.Name != "custom-server" {
		t.Errorf("Expected server name 'custom-server', got '%s'", server.config.Server.Name)
	}
	
	if server.config.Server.Version != "2.0.0" {
		t.Errorf("Expected server version '2.0.0', got '%s'", server.config.Server.Version)
	}
}

func TestServerBuilderWithEnvironment(t *testing.T) {
	builder := NewServerBuilder()
	
	// Test with development environment
	devConfig, err := config.GetEnvironmentSpecificConfig("development")
	if err != nil {
		t.Errorf("Expected no error getting development config, got: %v", err)
	}
	
	server, err := builder.WithConfig(devConfig).Build()
	if err != nil {
		t.Errorf("Expected no error when building server with development environment, got: %v", err)
	}
	
	if server.config.Server.Environment != "development" {
		t.Errorf("Expected environment 'development', got '%s'", server.config.Server.Environment)
	}
	
	// Test with production environment
	prodConfig, err := config.GetEnvironmentSpecificConfig("production")
	if err != nil {
		t.Errorf("Expected no error getting production config, got: %v", err)
	}
	
	server, err = builder.WithConfig(prodConfig).Build()
	if err != nil {
		t.Errorf("Expected no error when building server with production environment, got: %v", err)
	}
	
	if server.config.Server.Environment != "production" {
		t.Errorf("Expected environment 'production', got '%s'", server.config.Server.Environment)
	}
	
	if !server.config.Security.TLS.Enabled {
		t.Error("Expected TLS to be enabled in production environment")
	}
}

func TestServerBuilderWithLogger(t *testing.T) {
	builder := NewServerBuilder()
	
	// Create a mock logger
	logger := &mockLogger{messages: make([]string, 0)}
	
	server, err := builder.WithLogger(logger).Build()
	if err != nil {
		t.Errorf("Expected no error when building server with logger, got: %v", err)
	}
	
	if server == nil {
		t.Error("Expected server to be built with logger, got nil")
	}
	
	// Test that the logger is actually used
	ctx := context.Background()
	server.Start(ctx)
	
	// Check that log messages were recorded
	if len(logger.messages) == 0 {
		t.Error("Expected log messages to be recorded")
	}
	
	// Clean up
	server.Stop(ctx)
}

func TestServerStart(t *testing.T) {
	config := &domain.Configuration{
		Server: domain.ServerConfig{
			Name:            "test-server",
			Version:         "1.0.0",
			Environment:     "testing",
			LogLevel:        "debug",
			MaxConnections:  100,
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			ShutdownTimeout: 10 * time.Second,
		},
	}
	
	server, err := NewServer(WithConfiguration(config))
	if err != nil {
		t.Errorf("Expected no error when creating server, got: %v", err)
	}
	
	ctx := context.Background()
	
	err = server.Start(ctx)
	if err != nil {
		t.Errorf("Expected no error when starting server, got: %v", err)
	}
	
	// Test that starting an already started server returns error
	err = server.Start(ctx)
	if err == nil {
		t.Error("Expected error when starting already started server")
	}
	
	// Stop the server for cleanup
	server.Stop(ctx)
}

func TestServerStop(t *testing.T) {
	config := &domain.Configuration{
		Server: domain.ServerConfig{
			Name:            "test-server",
			Version:         "1.0.0",
			Environment:     "testing",
			LogLevel:        "debug",
			MaxConnections:  100,
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			ShutdownTimeout: 10 * time.Second,
		},
	}
	
	server, err := NewServer(WithConfiguration(config))
	if err != nil {
		t.Errorf("Expected no error when creating server, got: %v", err)
	}
	
	ctx := context.Background()
	
	// Start the server first
	err = server.Start(ctx)
	if err != nil {
		t.Errorf("Expected no error when starting server, got: %v", err)
	}
	
	// Stop the server
	err = server.Stop(ctx)
	if err != nil {
		t.Errorf("Expected no error when stopping server, got: %v", err)
	}
	
	// Test that stopping an already stopped server returns error
	err = server.Stop(ctx)
	if err == nil {
		t.Error("Expected error when stopping already stopped server")
	}
}

func TestServerRestart(t *testing.T) {
	config := &domain.Configuration{
		Server: domain.ServerConfig{
			Name:            "test-server",
			Version:         "1.0.0",
			Environment:     "testing",
			LogLevel:        "debug",
			MaxConnections:  100,
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			ShutdownTimeout: 10 * time.Second,
		},
	}
	
	server, err := NewServer(WithConfiguration(config))
	if err != nil {
		t.Errorf("Expected no error when creating server, got: %v", err)
	}
	
	ctx := context.Background()
	
	// Start the server first
	err = server.Start(ctx)
	if err != nil {
		t.Errorf("Expected no error when starting server, got: %v", err)
	}
	
	// Restart the server
	err = server.Restart(ctx)
	if err != nil {
		t.Errorf("Expected no error when restarting server, got: %v", err)
	}
	
	// Stop the server for cleanup
	server.Stop(ctx)
}

func TestServerGetHealth(t *testing.T) {
	config := &domain.Configuration{
		Server: domain.ServerConfig{
			Name:            "test-server",
			Version:         "1.0.0",
			Environment:     "testing",
			LogLevel:        "debug",
			MaxConnections:  100,
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			ShutdownTimeout: 10 * time.Second,
		},
	}
	
	server, err := NewServer(WithConfiguration(config))
	if err != nil {
		t.Errorf("Expected no error when creating server, got: %v", err)
	}
	
	ctx := context.Background()
	
	health, err := server.GetHealth(ctx)
	if err != nil {
		t.Errorf("Expected no error when getting health, got: %v", err)
	}
	
	if health == nil {
		t.Error("Expected health check result, got nil")
	}
	
	if health.Status != domain.HealthStatusPass {
		t.Errorf("Expected health status 'pass', got '%s'", health.Status)
	}
}

func TestServerGetStatus(t *testing.T) {
	config := &domain.Configuration{
		Server: domain.ServerConfig{
			Name:            "test-server",
			Version:         "1.0.0",
			Environment:     "testing",
			LogLevel:        "debug",
			MaxConnections:  100,
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			ShutdownTimeout: 10 * time.Second,
		},
	}
	
	server, err := NewServer(WithConfiguration(config))
	if err != nil {
		t.Errorf("Expected no error when creating server, got: %v", err)
	}
	
	ctx := context.Background()
	
	// Start the server first
	err = server.Start(ctx)
	if err != nil {
		t.Errorf("Expected no error when starting server, got: %v", err)
	}
	
	status, err := server.GetStatus(ctx)
	if err != nil {
		t.Errorf("Expected no error when getting status, got: %v", err)
	}
	
	if status == nil {
		t.Error("Expected server status, got nil")
	}
	
	if status.Name != "test-server" {
		t.Errorf("Expected server name 'test-server', got '%s'", status.Name)
	}
	
	if status.Version != "1.0.0" {
		t.Errorf("Expected server version '1.0.0', got '%s'", status.Version)
	}
	
	if status.Status != domain.StatusHealthy {
		t.Errorf("Expected server status 'healthy', got '%s'", status.Status)
	}
	
	// Stop the server for cleanup
	server.Stop(ctx)
}

func TestServerGetMetrics(t *testing.T) {
	config := &domain.Configuration{
		Server: domain.ServerConfig{
			Name:            "test-server",
			Version:         "1.0.0",
			Environment:     "testing",
			LogLevel:        "debug",
			MaxConnections:  100,
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			ShutdownTimeout: 10 * time.Second,
		},
	}
	
	server, err := NewServer(WithConfiguration(config))
	if err != nil {
		t.Errorf("Expected no error when creating server, got: %v", err)
	}
	
	ctx := context.Background()
	
	metrics, err := server.GetMetrics(ctx)
	if err != nil {
		t.Errorf("Expected no error when getting metrics, got: %v", err)
	}
	
	if metrics == nil {
		t.Error("Expected metrics, got nil")
	}
	
	if metrics.Server.RequestsTotal < 0 {
		t.Error("Expected non-negative requests total")
	}
	
	if metrics.Server.RequestsPerSecond < 0 {
		t.Error("Expected non-negative requests per second")
	}
}

func TestServerGetConfig(t *testing.T) {
	config := &domain.Configuration{
		Server: domain.ServerConfig{
			Name:            "test-server",
			Version:         "1.0.0",
			Environment:     "testing",
			LogLevel:        "debug",
			MaxConnections:  100,
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			ShutdownTimeout: 10 * time.Second,
		},
	}
	
	server, err := NewServer(WithConfiguration(config))
	if err != nil {
		t.Errorf("Expected no error when creating server, got: %v", err)
	}
	
	ctx := context.Background()
	
	retrievedConfig, err := server.GetConfiguration(ctx)
	if err != nil {
		t.Errorf("Expected no error when getting config, got: %v", err)
	}
	
	if retrievedConfig == nil {
		t.Error("Expected configuration, got nil")
	}
	
	if retrievedConfig.Server.Name != "test-server" {
		t.Errorf("Expected server name 'test-server', got '%s'", retrievedConfig.Server.Name)
	}
	
	if retrievedConfig.Server.Version != "1.0.0" {
		t.Errorf("Expected server version '1.0.0', got '%s'", retrievedConfig.Server.Version)
	}
	
	if retrievedConfig.Server.Environment != "testing" {
		t.Errorf("Expected environment 'testing', got '%s'", retrievedConfig.Server.Environment)
	}
}

func TestServerUpdateConfig(t *testing.T) {
	config := &domain.Configuration{
		Server: domain.ServerConfig{
			Name:            "test-server",
			Version:         "1.0.0",
			Environment:     "testing",
			LogLevel:        "debug",
			MaxConnections:  100,
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			ShutdownTimeout: 10 * time.Second,
		},
	}
	
	server, err := NewServer(WithConfiguration(config))
	if err != nil {
		t.Errorf("Expected no error when creating server, got: %v", err)
	}
	
	ctx := context.Background()
	
	// Create new configuration
	newConfig := &domain.Configuration{
		Server: domain.ServerConfig{
			Name:            "updated-server",
			Version:         "2.0.0",
			Environment:     "production",
			LogLevel:        "info",
			MaxConnections:  200,
			ReadTimeout:     60 * time.Second,
			WriteTimeout:    60 * time.Second,
			ShutdownTimeout: 20 * time.Second,
		},
	}
	
	err = server.UpdateConfiguration(ctx, newConfig)
	if err != nil {
		t.Errorf("Expected no error when updating config, got: %v", err)
	}
	
	// Verify the configuration was updated
	retrievedConfig, err := server.GetConfiguration(ctx)
	if err != nil {
		t.Errorf("Expected no error when getting config, got: %v", err)
	}
	
	if retrievedConfig.Server.Name != "updated-server" {
		t.Errorf("Expected server name 'updated-server', got '%s'", retrievedConfig.Server.Name)
	}
	
	if retrievedConfig.Server.Version != "2.0.0" {
		t.Errorf("Expected server version '2.0.0', got '%s'", retrievedConfig.Server.Version)
	}
	
	if retrievedConfig.Server.Environment != "production" {
		t.Errorf("Expected environment 'production', got '%s'", retrievedConfig.Server.Environment)
	}
}

func TestServerHandleRequest(t *testing.T) {
	config := &domain.Configuration{
		Server: domain.ServerConfig{
			Name:            "test-server",
			Version:         "1.0.0",
			Environment:     "testing",
			LogLevel:        "debug",
			MaxConnections:  100,
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			ShutdownTimeout: 10 * time.Second,
		},
	}
	
	server, err := NewServer(WithConfiguration(config))
	if err != nil {
		t.Errorf("Expected no error when creating server, got: %v", err)
	}
	
	ctx := context.Background()
	
	// Create a test request
	request := &domain.Request{
		ID:        "test-request-1",
		Type:      domain.RequestTypeHealth,
		Timestamp: time.Now(),
		Source:    "test-client",
		Context:   ctx,
	}
	
	response, err := server.HandleRequest(ctx, request)
	if err != nil {
		t.Errorf("Expected no error when handling request, got: %v", err)
	}
	
	if response == nil {
		t.Error("Expected response, got nil")
	}
	
	if response.RequestID != request.ID {
		t.Errorf("Expected response request ID '%s', got '%s'", request.ID, response.RequestID)
	}
	
	if response.Type != domain.ResponseTypeSuccess {
		t.Errorf("Expected response type 'success', got '%s'", response.Type)
	}
	
	if response.Status != domain.ResponseStatusOK {
		t.Errorf("Expected response status 'ok', got '%s'", response.Status)
	}
}

func TestServerHandleInvalidRequest(t *testing.T) {
	config := &domain.Configuration{
		Server: domain.ServerConfig{
			Name:            "test-server",
			Version:         "1.0.0",
			Environment:     "testing",
			LogLevel:        "debug",
			MaxConnections:  100,
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			ShutdownTimeout: 10 * time.Second,
		},
	}
	
	server, err := NewServer(WithConfiguration(config))
	if err != nil {
		t.Errorf("Expected no error when creating server, got: %v", err)
	}
	
	ctx := context.Background()
	
	// Test with nil request
	response, err := server.HandleRequest(ctx, nil)
	if err == nil {
		t.Error("Expected error for nil request")
	}
	if response != nil {
		t.Error("Expected no response for nil request")
	}
	
	// Test with invalid request
	invalidRequest := &domain.Request{
		ID:        "", // Invalid empty ID
		Type:      domain.RequestTypeHealth,
		Timestamp: time.Now(),
		Source:    "test-client",
		Context:   ctx,
	}
	
	response, err = server.HandleRequest(ctx, invalidRequest)
	if err == nil {
		t.Error("Expected error for invalid request")
	}
	if response != nil {
		t.Error("Expected no response for invalid request")
	}
}

// mockLogger is defined in the core package tests, so we'll define it here too
type mockLogger struct {
	messages []string
}

func (m *mockLogger) Debug(ctx context.Context, message string, fields ...interface{}) {
	m.messages = append(m.messages, "DEBUG: "+message)
}

func (m *mockLogger) Info(ctx context.Context, message string, fields ...interface{}) {
	m.messages = append(m.messages, "INFO: "+message)
}

func (m *mockLogger) Warn(ctx context.Context, message string, fields ...interface{}) {
	m.messages = append(m.messages, "WARN: "+message)
}

func (m *mockLogger) Error(ctx context.Context, message string, fields ...interface{}) {
	m.messages = append(m.messages, "ERROR: "+message)
}

func (m *mockLogger) WithFields(fields map[string]interface{}) domain.Logger {
	return m
}

func (m *mockLogger) WithError(err error) domain.Logger {
	return m
}

func (m *mockLogger) WithRequest(request *domain.Request) domain.Logger {
	return m
}

func (m *mockLogger) WithResponse(response *domain.Response) domain.Logger {
	return m
}