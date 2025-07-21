package systemd

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// TestTapioGRPCClient_NewClient tests basic client creation
func TestTapioGRPCClient_NewClient(t *testing.T) {
	client, err := NewTapioGRPCClient("localhost:8080")
	if err != nil {
		t.Fatalf("Failed to create Tapio client: %v", err)
	}
	defer client.Close()

	if client.serverAddr != "localhost:8080" {
		t.Errorf("Expected server address 'localhost:8080', got '%s'", client.serverAddr)
	}

	if client.collectorID != "systemd-collector" {
		t.Errorf("Expected collector ID 'systemd-collector', got '%s'", client.collectorID)
	}
}

// TestTapioGRPCClient_SendEvent tests event sending functionality
func TestTapioGRPCClient_SendEvent(t *testing.T) {
	client, err := NewTapioGRPCClient("localhost:8080")
	if err != nil {
		t.Fatalf("Failed to create Tapio client: %v", err)
	}
	defer client.Close()

	// Create a sample systemd UnifiedEvent
	event := &domain.UnifiedEvent{
		ID:        "systemd-test-event-123",
		Type:      domain.EventTypeService,
		Source:    "systemd-collector",
		Timestamp: time.Now(),
		Entity: &domain.EntityContext{
			Type: "service",
			Name: "nginx.service",
			Labels: map[string]string{
				"state": "active",
				"type":  "service",
			},
		},
		Semantic: &domain.SemanticContext{
			Intent:     "Service state change",
			Category:   "infrastructure",
			Tags:       []string{"systemd", "service", "state-change"},
			Narrative:  "nginx service state changed to active",
			Confidence: 0.95,
		},
		Impact: &domain.ImpactContext{
			Severity:         "low",
			BusinessImpact:   0.1,
			AffectedServices: []string{"web-server"},
		},
		Application: &domain.ApplicationData{
			Message: "Service nginx.service started successfully",
			Level:   "info",
			Custom: map[string]interface{}{
				"unit_name":  "nginx.service",
				"unit_type":  "service",
				"old_state":  "inactive",
				"new_state":  "active",
				"result":     "success",
				"exit_code":  0,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Send event (will buffer since no actual server connection)
	err = client.SendEvent(ctx, event)
	if err != nil {
		t.Errorf("Failed to send event: %v", err)
	}

	// Verify statistics
	stats := client.GetStatistics()
	if stats["buffer_size"].(int) != 1 {
		t.Errorf("Expected buffer size 1, got %d", stats["buffer_size"].(int))
	}
}

// TestTapioGRPCClient_SendBatch tests batch sending functionality
func TestTapioGRPCClient_SendBatch(t *testing.T) {
	client, err := NewTapioGRPCClient("localhost:8080")
	if err != nil {
		t.Fatalf("Failed to create Tapio client: %v", err)
	}
	defer client.Close()

	// Create a batch of sample systemd UnifiedEvents
	events := make([]*domain.UnifiedEvent, 5)
	for i := 0; i < 5; i++ {
		events[i] = &domain.UnifiedEvent{
			ID:        fmt.Sprintf("systemd-batch-event-%d", i),
			Type:      domain.EventTypeSystem,
			Source:    "systemd-collector",
			Timestamp: time.Now(),
			Entity: &domain.EntityContext{
				Type: "service",
				Name: fmt.Sprintf("test-service-%d.service", i),
				Labels: map[string]string{
					"state": "active",
					"type":  "service",
				},
			},
			Semantic: &domain.SemanticContext{
				Intent:     "Service lifecycle event",
				Category:   "infrastructure",
				Confidence: 0.9,
			},
			Application: &domain.ApplicationData{
				Message: fmt.Sprintf("Service event %d", i),
				Level:   "info",
				Custom: map[string]interface{}{
					"unit_name": fmt.Sprintf("test-service-%d.service", i),
					"unit_type": "service",
					"new_state": "active",
				},
			},
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Send batch (will buffer since no actual server connection)
	err = client.SendBatch(ctx, events)
	if err != nil {
		t.Errorf("Failed to send batch: %v", err)
	}

	// Verify statistics
	stats := client.GetStatistics()
	if stats["buffer_size"].(int) != 5 {
		t.Errorf("Expected buffer size 5, got %d", stats["buffer_size"].(int))
	}
}

// TestTapioGRPCClient_CustomConfig tests client creation with custom configuration
func TestTapioGRPCClient_CustomConfig(t *testing.T) {
	config := &TapioClientConfig{
		ServerAddr:    "custom.server:9090",
		CollectorID:   "custom-systemd-collector",
		BufferSize:    5000,
		BatchSize:     50,
		FlushInterval: 2 * time.Second,
		RetryInterval: 10 * time.Second,
		MaxRetries:    3,
		EnableOTEL:    false, // Disable OTEL for this test
	}

	client, err := NewTapioGRPCClientWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create Tapio client with custom config: %v", err)
	}
	defer client.Close()

	if client.serverAddr != "custom.server:9090" {
		t.Errorf("Expected server address 'custom.server:9090', got '%s'", client.serverAddr)
	}

	if client.collectorID != "custom-systemd-collector" {
		t.Errorf("Expected collector ID 'custom-systemd-collector', got '%s'", client.collectorID)
	}

	if cap(client.eventBuffer) != 5000 {
		t.Errorf("Expected buffer capacity 5000, got %d", cap(client.eventBuffer))
	}

	if client.batchSize != 50 {
		t.Errorf("Expected batch size 50, got %d", client.batchSize)
	}
}

// TestTapioGRPCClient_EventMapping tests the event type and severity mapping
func TestTapioGRPCClient_EventMapping(t *testing.T) {
	client, err := NewTapioGRPCClient("localhost:8080")
	if err != nil {
		t.Fatalf("Failed to create Tapio client: %v", err)
	}
	defer client.Close()

	// Test event type mapping
	testCases := []struct {
		domainType domain.EventType
		expected   string // We'll check the string representation
	}{
		{domain.EventTypeSystem, "EVENT_TYPE_PROCESS"},
		{domain.EventTypeService, "EVENT_TYPE_PROCESS"},
		{domain.EventTypeLog, "EVENT_TYPE_AUDIT"},
		{domain.EventTypeNetwork, "EVENT_TYPE_NETWORK"},
		{domain.EventTypeProcess, "EVENT_TYPE_PROCESS"},
	}

	for _, tc := range testCases {
		pbType := client.mapEventType(tc.domainType)
		if pbType.String() != tc.expected {
			t.Errorf("Event type mapping failed: expected %s, got %s", tc.expected, pbType.String())
		}
	}

	// Test severity mapping
	severityTestCases := []struct {
		domainSeverity domain.EventSeverity
		expected       string
	}{
		{domain.EventSeverityDebug, "EVENT_SEVERITY_DEBUG"},
		{domain.EventSeverityInfo, "EVENT_SEVERITY_INFO"},
		{domain.EventSeverityWarning, "EVENT_SEVERITY_WARNING"},
		{domain.EventSeverityError, "EVENT_SEVERITY_ERROR"},
		{domain.EventSeverityCritical, "EVENT_SEVERITY_CRITICAL"},
	}

	for _, tc := range severityTestCases {
		pbSeverity := client.mapEventSeverity(tc.domainSeverity)
		if pbSeverity.String() != tc.expected {
			t.Errorf("Severity mapping failed: expected %s, got %s", tc.expected, pbSeverity.String())
		}
	}
}

// TestTapioGRPCClient_Statistics tests the statistics functionality
func TestTapioGRPCClient_Statistics(t *testing.T) {
	client, err := NewTapioGRPCClient("localhost:8080")
	if err != nil {
		t.Fatalf("Failed to create Tapio client: %v", err)
	}
	defer client.Close()

	stats := client.GetStatistics()

	// Check expected fields
	expectedFields := []string{
		"connected", "events_sent", "events_dropped", "reconnects",
		"buffer_size", "buffer_capacity", "last_sent", "server_addr", "collector_id",
	}

	for _, field := range expectedFields {
		if _, exists := stats[field]; !exists {
			t.Errorf("Missing expected field in statistics: %s", field)
		}
	}

	// Check initial values
	if stats["connected"].(bool) != false {
		t.Errorf("Expected connected to be false initially, got %t", stats["connected"].(bool))
	}

	if stats["events_sent"].(uint64) != 0 {
		t.Errorf("Expected events_sent to be 0 initially, got %d", stats["events_sent"].(uint64))
	}

	if stats["server_addr"].(string) != "localhost:8080" {
		t.Errorf("Expected server_addr to be 'localhost:8080', got '%s'", stats["server_addr"].(string))
	}

	if stats["collector_id"].(string) != "systemd-collector" {
		t.Errorf("Expected collector_id to be 'systemd-collector', got '%s'", stats["collector_id"].(string))
	}
}

// TestTapioGRPCClient_Close tests the client close functionality
func TestTapioGRPCClient_Close(t *testing.T) {
	client, err := NewTapioGRPCClient("localhost:8080")
	if err != nil {
		t.Fatalf("Failed to create Tapio client: %v", err)
	}

	// Send an event before closing
	event := &domain.UnifiedEvent{
		ID:     "test-close-event",
		Type:   domain.EventTypeSystem,
		Source: "systemd-collector",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err = client.SendEvent(ctx, event)
	if err != nil {
		t.Errorf("Failed to send event before close: %v", err)
	}

	// Close the client
	err = client.Close()
	if err != nil {
		t.Errorf("Failed to close client: %v", err)
	}

	// Try to send another event after close (should fail)
	err = client.SendEvent(ctx, event)
	if err == nil {
		t.Error("Expected error when sending to closed client, got nil")
	}
}

// Benchmark test for event sending
func BenchmarkTapioGRPCClient_SendEvent(b *testing.B) {
	client, err := NewTapioGRPCClient("localhost:8080")
	if err != nil {
		b.Fatalf("Failed to create Tapio client: %v", err)
	}
	defer client.Close()

	event := &domain.UnifiedEvent{
		ID:     "benchmark-event",
		Type:   domain.EventTypeSystem,
		Source: "systemd-collector",
		Entity: &domain.EntityContext{
			Type: "service",
			Name: "benchmark-service",
		},
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		event.ID = fmt.Sprintf("benchmark-event-%d", i)
		err := client.SendEvent(ctx, event)
		if err != nil {
			b.Errorf("Failed to send event: %v", err)
		}
	}
}

// TestTapioGRPCClient_ExtractMessage tests message extraction from events
func TestTapioGRPCClient_ExtractMessage(t *testing.T) {
	client, err := NewTapioGRPCClient("localhost:8080")
	if err != nil {
		t.Fatalf("Failed to create Tapio client: %v", err)
	}
	defer client.Close()

	// Test message extraction from Application context
	event := &domain.UnifiedEvent{
		Application: &domain.ApplicationData{
			Message: "Service started successfully",
		},
	}

	message := client.extractMessage(event)
	expected := "Service started successfully"
	if message != expected {
		t.Errorf("Expected message '%s', got '%s'", expected, message)
	}

	// Test message extraction from Semantic context
	event2 := &domain.UnifiedEvent{
		Semantic: &domain.SemanticContext{
			Narrative: "System event occurred",
		},
	}

	message2 := client.extractMessage(event2)
	expected2 := "System event occurred"
	if message2 != expected2 {
		t.Errorf("Expected message '%s', got '%s'", expected2, message2)
	}

	// Test fallback message
	event3 := &domain.UnifiedEvent{
		Type:   domain.EventTypeService,
		Source: "systemd",
	}

	message3 := client.extractMessage(event3)
	expected3 := "Systemd event service from systemd"
	if message3 != expected3 {
		t.Errorf("Expected message '%s', got '%s'", expected3, message3)
	}
}