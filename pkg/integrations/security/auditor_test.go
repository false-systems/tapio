package security

import (
	"net/http/httptest"
	"testing"
)

func TestNewSecurityAuditor(t *testing.T) {
	config := AuditConfig{
		Enabled:           true,
		LogAuthentication: true,
		LogAuthorization:  true,
		LogDataAccess:     true,
		LogConfigChanges:  true,
		LogErrors:         true,
		RetentionDays:     90,
		EncryptLogs:       true,
		SignLogs:          true,
	}

	logger := createTestLogger()
	auditor := NewSecurityAuditor(config, logger)

	if auditor == nil {
		t.Fatal("NewSecurityAuditor returned nil")
	}

	if !auditor.config.Enabled {
		t.Error("Auditor should be enabled")
	}

	if auditor.config.RetentionDays != 90 {
		t.Errorf("Expected retention days 90, got %d", auditor.config.RetentionDays)
	}
}

func TestSecurityAuditor_Initialize(t *testing.T) {
	config := AuditConfig{
		Enabled: true,
	}

	logger := createTestLogger()
	auditor := NewSecurityAuditor(config, logger)

	err := auditor.Initialize()
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
}

func TestSecurityAuditor_LogRequest(t *testing.T) {
	config := AuditConfig{
		Enabled:       true,
		LogDataAccess: true,
	}

	logger := createTestLogger()
	auditor := NewSecurityAuditor(config, logger)
	auditor.Initialize()

	// Create test request
	req := httptest.NewRequest("GET", "/api/users", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	req.Header.Set("User-Agent", "test-client")
	req.Header.Set("Authorization", "Bearer test-token")

	// Log the request
	auditor.LogRequest(req)

	// Verify event was logged
	auditor.mutex.RLock()
	eventCount := len(auditor.eventLog)
	auditor.mutex.RUnlock()

	if eventCount == 0 {
		t.Error("No audit event was logged for request")
	}

	// Check the logged event
	auditor.mutex.RLock()
	event := auditor.eventLog[0]
	auditor.mutex.RUnlock()

	if event.EventType != "data_access" {
		t.Errorf("Expected event type 'data_access', got '%s'", event.EventType)
	}

	if event.Action != "GET" {
		t.Errorf("Expected action 'GET', got '%s'", event.Action)
	}

	if event.Actor.IPAddress != "192.168.1.100" {
		t.Errorf("Expected IP '192.168.1.100', got '%s'", event.Actor.IPAddress)
	}

	if event.Target.Resource != "/api/users" {
		t.Errorf("Expected resource '/api/users', got '%s'", event.Target.Resource)
	}
}

func TestSecurityAuditor_LogSecurityEvent(t *testing.T) {
	config := AuditConfig{
		Enabled:   true,
		LogErrors: true,
	}

	logger := createTestLogger()
	auditor := NewSecurityAuditor(config, logger)
	auditor.Initialize()

	// Log a security event
	auditor.LogSecurityEvent("authentication_failed", "192.168.1.100")

	// Verify event was logged
	auditor.mutex.RLock()
	eventCount := len(auditor.eventLog)
	auditor.mutex.RUnlock()

	if eventCount == 0 {
		t.Error("No audit event was logged for security event")
	}

	// Check the logged event
	auditor.mutex.RLock()
	event := auditor.eventLog[0]
	auditor.mutex.RUnlock()

	if event.EventType != "security" {
		t.Errorf("Expected event type 'security', got '%s'", event.EventType)
	}

	if event.Title != "authentication_failed" {
		t.Errorf("Expected title 'authentication_failed', got '%s'", event.Title)
	}

	if event.Actor.IPAddress != "192.168.1.100" {
		t.Errorf("Expected IP '192.168.1.100', got '%s'", event.Actor.IPAddress)
	}

	if event.Severity != "high" {
		t.Errorf("Expected severity 'high', got '%s'", event.Severity)
	}
}

func TestSecurityAuditor_GetEvents(t *testing.T) {
	config := AuditConfig{
		Enabled: true,
	}

	logger := createTestLogger()
	auditor := NewSecurityAuditor(config, logger)
	auditor.Initialize()

	// Add some test events
	auditor.LogSecurityEvent("test_event_1", "192.168.1.100")
	auditor.LogSecurityEvent("test_event_2", "192.168.1.101")
	auditor.LogSecurityEvent("test_event_3", "192.168.1.102")

	// Get all events - using the actual method from auditor.go
	criteria := AuditSearchCriteria{}
	events := auditor.GetAuditEvents(criteria)

	if len(events) != 3 {
		t.Errorf("Expected 3 events, got %d", len(events))
	}

	// Test pagination
	criteria.Limit = 2
	events = auditor.GetAuditEvents(criteria)
	if len(events) > 2 {
		t.Errorf("Expected at most 2 events with limit 2, got %d", len(events))
	}
}

func TestSecurityAuditor_RetentionConfig(t *testing.T) {
	config := AuditConfig{
		Enabled:       true,
		RetentionDays: 30, // Test retention configuration
	}

	logger := createTestLogger()
	auditor := NewSecurityAuditor(config, logger)
	auditor.Initialize()

	if auditor.config.RetentionDays != 30 {
		t.Errorf("Expected retention days 30, got %d", auditor.config.RetentionDays)
	}

	// Test logging some events
	auditor.LogSecurityEvent("test_event", "192.168.1.100")

	// Verify event was logged
	auditor.mutex.RLock()
	eventCount := len(auditor.eventLog)
	auditor.mutex.RUnlock()

	if eventCount == 0 {
		t.Error("No events were logged")
	}
}

func TestSecurityAuditor_Disabled(t *testing.T) {
	config := AuditConfig{
		Enabled: false, // Auditing disabled
	}

	logger := createTestLogger()
	auditor := NewSecurityAuditor(config, logger)
	auditor.Initialize()

	// Try to log events
	auditor.LogSecurityEvent("test_event", "192.168.1.100")

	// Should not log any events when disabled
	auditor.mutex.RLock()
	eventCount := len(auditor.eventLog)
	auditor.mutex.RUnlock()

	if eventCount > 0 {
		t.Errorf("No events should be logged when auditing is disabled, got %d", eventCount)
	}
}
