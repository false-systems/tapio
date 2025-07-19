package core

import (
	"context"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/server/domain"
)

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

type mockHealthChecker struct {
	healthy bool
}

func (m *mockHealthChecker) CheckHealth(ctx context.Context) (*domain.HealthCheck, error) {
	status := domain.HealthStatusPass
	if !m.healthy {
		status = domain.HealthStatusFail
	}
	return &domain.HealthCheck{
		Name:      "server",
		Status:    status,
		Message:   "Health check result",
		Timestamp: time.Now(),
		Duration:  10 * time.Millisecond,
	}, nil
}

func (m *mockHealthChecker) CheckComponentHealth(ctx context.Context, component string) (*domain.HealthCheck, error) {
	return m.CheckHealth(ctx)
}

func (m *mockHealthChecker) IsHealthy(ctx context.Context) (bool, error) {
	return m.healthy, nil
}

func (m *mockHealthChecker) GetHealthStatus(ctx context.Context) (domain.HealthStatus, error) {
	if m.healthy {
		return domain.HealthStatusPass, nil
	}
	return domain.HealthStatusFail, nil
}

type mockMetricsCollector struct {
	metrics *domain.Metrics
}

func (m *mockMetricsCollector) CollectMetrics(ctx context.Context) (*domain.Metrics, error) {
	if m.metrics == nil {
		return &domain.Metrics{
			Server: domain.ServerMetrics{
				RequestsTotal:       100,
				RequestsPerSecond:   10.0,
				ErrorsTotal:         5,
				ErrorRate:           0.05,
				ActiveConnections:   10,
				AverageResponseTime: 100 * time.Millisecond,
				MemoryUsage:         1024 * 1024,
				CPUUsage:            0.1,
				LastUpdated:         time.Now(),
			},
			Endpoints:   make(map[string]domain.EndpointMetrics),
			Connections: make(map[string]domain.ConnectionMetrics),
			Timestamp:   time.Now(),
		}, nil
	}
	return m.metrics, nil
}

func (m *mockMetricsCollector) CollectServerMetrics(ctx context.Context) (*domain.ServerMetrics, error) {
	metrics, err := m.CollectMetrics(ctx)
	if err != nil {
		return nil, err
	}
	return &metrics.Server, nil
}

func (m *mockMetricsCollector) CollectEndpointMetrics(ctx context.Context, endpointName string) (*domain.EndpointMetrics, error) {
	return &domain.EndpointMetrics{
		RequestsTotal:       50,
		RequestsPerSecond:   5.0,
		ErrorsTotal:         2,
		ErrorRate:           0.04,
		AverageResponseTime: 80 * time.Millisecond,
		P95ResponseTime:     150 * time.Millisecond,
		P99ResponseTime:     200 * time.Millisecond,
		LastRequest:         time.Now(),
	}, nil
}

func (m *mockMetricsCollector) CollectConnectionMetrics(ctx context.Context, connectionID string) (*domain.ConnectionMetrics, error) {
	return &domain.ConnectionMetrics{
		RequestsTotal:   25,
		ErrorsTotal:     1,
		BytesReceived:   1024,
		BytesSent:       2048,
		LastRequestTime: time.Now(),
		AverageLatency:  50 * time.Millisecond,
	}, nil
}

func (m *mockMetricsCollector) RecordRequest(ctx context.Context, request *domain.Request) error {
	return nil
}

func (m *mockMetricsCollector) RecordResponse(ctx context.Context, response *domain.Response) error {
	return nil
}

func (m *mockMetricsCollector) RecordError(ctx context.Context, err error) error {
	return nil
}

type mockEventPublisher struct {
	events []*domain.Event
}

func (m *mockEventPublisher) PublishEvent(ctx context.Context, event *domain.Event) error {
	m.events = append(m.events, event)
	return nil
}

func (m *mockEventPublisher) PublishEvents(ctx context.Context, events []*domain.Event) error {
	m.events = append(m.events, events...)
	return nil
}

func (m *mockEventPublisher) Subscribe(ctx context.Context, eventType domain.EventType, handler domain.EventHandler) error {
	return nil
}

func (m *mockEventPublisher) Unsubscribe(ctx context.Context, eventType domain.EventType, handler domain.EventHandler) error {
	return nil
}

type mockConnectionManager struct {
	connections map[string]*domain.Connection
}

func (m *mockConnectionManager) AcceptConnection(ctx context.Context, connection *domain.Connection) error {
	if m.connections == nil {
		m.connections = make(map[string]*domain.Connection)
	}
	m.connections[connection.ID] = connection
	return nil
}

func (m *mockConnectionManager) CloseConnection(ctx context.Context, connectionID string) error {
	if m.connections != nil {
		if conn, exists := m.connections[connectionID]; exists {
			conn.Status = domain.ConnectionClosed
		}
	}
	return nil
}

func (m *mockConnectionManager) GetConnection(ctx context.Context, connectionID string) (*domain.Connection, error) {
	if m.connections != nil {
		if conn, exists := m.connections[connectionID]; exists {
			return conn, nil
		}
	}
	return nil, domain.ErrResourceNotFound("connection not found")
}

func (m *mockConnectionManager) GetConnections(ctx context.Context) ([]*domain.Connection, error) {
	connections := make([]*domain.Connection, 0, len(m.connections))
	for _, conn := range m.connections {
		connections = append(connections, conn)
	}
	return connections, nil
}

func (m *mockConnectionManager) GetConnectionMetrics(ctx context.Context, connectionID string) (*domain.ConnectionMetrics, error) {
	return &domain.ConnectionMetrics{
		RequestsTotal:   10,
		ErrorsTotal:     0,
		BytesReceived:   512,
		BytesSent:       1024,
		LastRequestTime: time.Now(),
		AverageLatency:  25 * time.Millisecond,
	}, nil
}

func (m *mockConnectionManager) CleanupIdleConnections(ctx context.Context, maxIdle time.Duration) error {
	return nil
}

func createTestService() *ServerService {
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

	logger := &mockLogger{messages: make([]string, 0)}
	healthChecker := &mockHealthChecker{healthy: true}
	metricsCollector := &mockMetricsCollector{}
	eventPublisher := &mockEventPublisher{events: make([]*domain.Event, 0)}
	connectionManager := &mockConnectionManager{connections: make(map[string]*domain.Connection)}

	return NewServerService(config, connectionManager, nil, healthChecker, metricsCollector, eventPublisher, nil)
}

func TestNewService(t *testing.T) {
	service := createTestService()

	if service == nil {
		t.Error("Expected service to be created, got nil")
	}

	if service.config == nil {
		t.Error("Expected service config to be set, got nil")
	}

	if service.healthChecker == nil {
		t.Error("Expected service health checker to be set, got nil")
	}

	if service.metricsCollector == nil {
		t.Error("Expected service metrics collector to be set, got nil")
	}

	if service.eventPublisher == nil {
		t.Error("Expected service event publisher to be set, got nil")
	}

	if service.connectionManager == nil {
		t.Error("Expected service connection manager to be set, got nil")
	}
}

func TestServiceStart(t *testing.T) {
	service := createTestService()
	ctx := context.Background()

	err := service.Start(ctx)
	if err != nil {
		t.Errorf("Expected no error when starting service, got: %v", err)
	}

	// Test that starting an already started service returns error
	err = service.Start(ctx)
	if err == nil {
		t.Error("Expected error when starting already started service")
	}
}

func TestServiceStop(t *testing.T) {
	service := createTestService()
	ctx := context.Background()

	// Start the service first
	err := service.Start(ctx)
	if err != nil {
		t.Errorf("Expected no error when starting service, got: %v", err)
	}

	// Stop the service
	err = service.Stop(ctx)
	if err != nil {
		t.Errorf("Expected no error when stopping service, got: %v", err)
	}

	// Test that stopping an already stopped service returns error
	err = service.Stop(ctx)
	if err == nil {
		t.Error("Expected error when stopping already stopped service")
	}
}

func TestServiceRestart(t *testing.T) {
	service := createTestService()
	ctx := context.Background()

	// Start the service first
	err := service.Start(ctx)
	if err != nil {
		t.Errorf("Expected no error when starting service, got: %v", err)
	}

	// Restart the service
	err = service.Restart(ctx)
	if err != nil {
		t.Errorf("Expected no error when restarting service, got: %v", err)
	}

	// Test that we can restart again
	err = service.Restart(ctx)
	if err != nil {
		t.Errorf("Expected no error when restarting service again, got: %v", err)
	}
}

func TestServiceGetHealth(t *testing.T) {
	service := createTestService()
	ctx := context.Background()

	health, err := service.GetHealth(ctx)
	if err != nil {
		t.Errorf("Expected no error when getting health, got: %v", err)
	}

	if health == nil {
		t.Error("Expected health check result, got nil")
	}

	if health.Name != "server" {
		t.Errorf("Expected health check name 'server', got '%s'", health.Name)
	}

	if health.Status != domain.HealthStatusPass {
		t.Errorf("Expected health status 'pass', got '%s'", health.Status)
	}
}

func TestServiceGetStatus(t *testing.T) {
	service := createTestService()
	ctx := context.Background()

	// Start the service first
	err := service.Start(ctx)
	if err != nil {
		t.Errorf("Expected no error when starting service, got: %v", err)
	}

	status, err := service.GetStatus(ctx)
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
}

func TestServiceGetMetrics(t *testing.T) {
	service := createTestService()
	ctx := context.Background()

	metrics, err := service.GetMetrics(ctx)
	if err != nil {
		t.Errorf("Expected no error when getting metrics, got: %v", err)
	}

	if metrics == nil {
		t.Error("Expected metrics, got nil")
	}

	if metrics.Server.RequestsTotal != 100 {
		t.Errorf("Expected 100 total requests, got %d", metrics.Server.RequestsTotal)
	}

	if metrics.Server.RequestsPerSecond != 10.0 {
		t.Errorf("Expected 10.0 requests per second, got %f", metrics.Server.RequestsPerSecond)
	}

	if metrics.Server.ErrorsTotal != 5 {
		t.Errorf("Expected 5 total errors, got %d", metrics.Server.ErrorsTotal)
	}
}

func TestServiceGetConfig(t *testing.T) {
	service := createTestService()
	ctx := context.Background()

	config, err := service.GetConfig(ctx)
	if err != nil {
		t.Errorf("Expected no error when getting config, got: %v", err)
	}

	if config == nil {
		t.Error("Expected configuration, got nil")
	}

	if config.Server.Name != "test-server" {
		t.Errorf("Expected server name 'test-server', got '%s'", config.Server.Name)
	}

	if config.Server.Version != "1.0.0" {
		t.Errorf("Expected server version '1.0.0', got '%s'", config.Server.Version)
	}

	if config.Server.Environment != "testing" {
		t.Errorf("Expected environment 'testing', got '%s'", config.Server.Environment)
	}
}

func TestServiceUpdateConfig(t *testing.T) {
	service := createTestService()
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

	err := service.UpdateConfig(ctx, newConfig)
	if err != nil {
		t.Errorf("Expected no error when updating config, got: %v", err)
	}

	// Verify the configuration was updated
	config, err := service.GetConfig(ctx)
	if err != nil {
		t.Errorf("Expected no error when getting config, got: %v", err)
	}

	if config.Server.Name != "updated-server" {
		t.Errorf("Expected server name 'updated-server', got '%s'", config.Server.Name)
	}

	if config.Server.Version != "2.0.0" {
		t.Errorf("Expected server version '2.0.0', got '%s'", config.Server.Version)
	}

	if config.Server.Environment != "production" {
		t.Errorf("Expected environment 'production', got '%s'", config.Server.Environment)
	}
}

func TestServicePublishEvent(t *testing.T) {
	service := createTestService()
	ctx := context.Background()

	event := &domain.Event{
		ID:        "test-event-1",
		Type:      domain.EventTypeRequest,
		Severity:  domain.SeverityInfo,
		Source:    "test",
		Message:   "Test event message",
		Timestamp: time.Now(),
		Data:      map[string]interface{}{"key": "value"},
	}

	err := service.PublishEvent(ctx, event)
	if err != nil {
		t.Errorf("Expected no error when publishing event, got: %v", err)
	}

	// Verify the event was published
	eventPublisher := service.eventPublisher.(*mockEventPublisher)
	if len(eventPublisher.events) != 1 {
		t.Errorf("Expected 1 published event, got %d", len(eventPublisher.events))
	}

	publishedEvent := eventPublisher.events[0]
	if publishedEvent.ID != "test-event-1" {
		t.Errorf("Expected event ID 'test-event-1', got '%s'", publishedEvent.ID)
	}

	if publishedEvent.Type != domain.EventTypeRequest {
		t.Errorf("Expected event type 'request', got '%s'", publishedEvent.Type)
	}

	if publishedEvent.Message != "Test event message" {
		t.Errorf("Expected event message 'Test event message', got '%s'", publishedEvent.Message)
	}
}

func TestServiceGetConnections(t *testing.T) {
	service := createTestService()
	ctx := context.Background()

	// Add some test connections
	conn1 := &domain.Connection{
		ID:            "conn-1",
		RemoteAddress: "127.0.0.1:12345",
		Protocol:      "tcp",
		StartTime:     time.Now(),
		LastActivity:  time.Now(),
		Status:        domain.ConnectionActive,
	}

	conn2 := &domain.Connection{
		ID:            "conn-2",
		RemoteAddress: "127.0.0.1:12346",
		Protocol:      "tcp",
		StartTime:     time.Now(),
		LastActivity:  time.Now(),
		Status:        domain.ConnectionActive,
	}

	connectionManager := service.connectionManager.(*mockConnectionManager)
	connectionManager.AcceptConnection(ctx, conn1)
	connectionManager.AcceptConnection(ctx, conn2)

	connections, err := service.GetConnections(ctx)
	if err != nil {
		t.Errorf("Expected no error when getting connections, got: %v", err)
	}

	if len(connections) != 2 {
		t.Errorf("Expected 2 connections, got %d", len(connections))
	}
}

func TestServiceCloseConnection(t *testing.T) {
	service := createTestService()
	ctx := context.Background()

	// Add a test connection
	conn := &domain.Connection{
		ID:            "conn-test",
		RemoteAddress: "127.0.0.1:12345",
		Protocol:      "tcp",
		StartTime:     time.Now(),
		LastActivity:  time.Now(),
		Status:        domain.ConnectionActive,
	}

	connectionManager := service.connectionManager.(*mockConnectionManager)
	connectionManager.AcceptConnection(ctx, conn)

	err := service.CloseConnection(ctx, "conn-test")
	if err != nil {
		t.Errorf("Expected no error when closing connection, got: %v", err)
	}

	// Verify the connection was closed
	closedConn, err := connectionManager.GetConnection(ctx, "conn-test")
	if err != nil {
		t.Errorf("Expected no error when getting connection, got: %v", err)
	}

	if closedConn.Status != domain.ConnectionClosed {
		t.Errorf("Expected connection status 'closed', got '%s'", closedConn.Status)
	}
}

func TestServiceUnhealthyStatus(t *testing.T) {
	service := createTestService()
	ctx := context.Background()

	// Make the health checker return unhealthy
	healthChecker := service.healthChecker.(*mockHealthChecker)
	healthChecker.healthy = false

	// Start the service first
	err := service.Start(ctx)
	if err != nil {
		t.Errorf("Expected no error when starting service, got: %v", err)
	}

	status, err := service.GetStatus(ctx)
	if err != nil {
		t.Errorf("Expected no error when getting status, got: %v", err)
	}

	if status.Status != domain.StatusUnhealthy {
		t.Errorf("Expected server status 'unhealthy', got '%s'", status.Status)
	}
}

func TestServiceInvalidConfigUpdate(t *testing.T) {
	service := createTestService()
	ctx := context.Background()

	// Create invalid configuration (empty server name)
	invalidConfig := &domain.Configuration{
		Server: domain.ServerConfig{
			Name:            "", // Invalid empty name
			Version:         "2.0.0",
			Environment:     "production",
			LogLevel:        "info",
			MaxConnections:  200,
			ReadTimeout:     60 * time.Second,
			WriteTimeout:    60 * time.Second,
			ShutdownTimeout: 20 * time.Second,
		},
	}

	err := service.UpdateConfig(ctx, invalidConfig)
	if err == nil {
		t.Error("Expected error when updating with invalid config")
	}
}
