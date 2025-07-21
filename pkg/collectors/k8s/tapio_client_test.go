package k8s

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

	if client.collectorID != "k8s-collector" {
		t.Errorf("Expected collector ID 'k8s-collector', got '%s'", client.collectorID)
	}
}

// TestTapioGRPCClient_SendEvent tests event sending functionality
func TestTapioGRPCClient_SendEvent(t *testing.T) {
	client, err := NewTapioGRPCClient("localhost:8080")
	if err != nil {
		t.Fatalf("Failed to create Tapio client: %v", err)
	}
	defer client.Close()

	// Create a sample K8s UnifiedEvent
	event := &domain.UnifiedEvent{
		ID:        "k8s-test-event-123",
		Type:      domain.EventTypeKubernetes,
		Source:    "k8s-collector",
		Timestamp: time.Now(),
		Entity: &domain.EntityContext{
			Type:      "pod",
			Name:      "test-pod",
			Namespace: "default",
			Labels: map[string]string{
				"app":     "test-app",
				"version": "v1.0",
				"cluster": "test-cluster",
				"node":    "worker-1",
			},
		},
		Semantic: &domain.SemanticContext{
			Intent:     "Pod lifecycle event",
			Category:   "infrastructure",
			Tags:       []string{"k8s", "pod", "creation"},
			Narrative:  "A new pod was created in the default namespace",
			Confidence: 0.9,
		},
		Impact: &domain.ImpactContext{
			Severity:         "low",
			BusinessImpact:   0.1,
			AffectedServices: []string{"test-service"},
		},
		Kubernetes: &domain.KubernetesData{
			EventType:   "Normal",
			Reason:      "Created",
			Message:     "Pod created successfully",
			Action:      "ADDED",
			ObjectKind:  "Pod",
			Object:      "pod/test-pod",
			APIVersion:  "v1",
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

	// Create a batch of sample K8s UnifiedEvents
	events := make([]*domain.UnifiedEvent, 5)
	for i := 0; i < 5; i++ {
		events[i] = &domain.UnifiedEvent{
			ID:        fmt.Sprintf("k8s-batch-event-%d", i),
			Type:      domain.EventTypeKubernetes,
			Source:    "k8s-collector",
			Timestamp: time.Now(),
			Entity: &domain.EntityContext{
				Type:      "service",
				Name:      fmt.Sprintf("test-service-%d", i),
				Namespace: "default",
				Labels: map[string]string{
					"cluster": "test-cluster",
				},
			},
			Semantic: &domain.SemanticContext{
				Intent:     "Service lifecycle event",
				Category:   "infrastructure",
				Confidence: 0.9,
			},
			Kubernetes: &domain.KubernetesData{
				EventType:  "Normal",
				Reason:     "Updated",
				Message:    fmt.Sprintf("Service event %d", i),
				Action:     "MODIFIED",
				ObjectKind: "Service",
				Object:     fmt.Sprintf("service/test-service-%d", i),
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
		CollectorID:   "custom-k8s-collector",
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

	if client.collectorID != "custom-k8s-collector" {
		t.Errorf("Expected collector ID 'custom-k8s-collector', got '%s'", client.collectorID)
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
		{domain.EventTypeKubernetes, "EVENT_TYPE_KUBERNETES"},
		{domain.EventTypeNetwork, "EVENT_TYPE_NETWORK"},
		{domain.EventTypeProcess, "EVENT_TYPE_PROCESS"},
		{domain.EventTypeService, "EVENT_TYPE_HTTP"},
		{domain.EventTypeSystem, "EVENT_TYPE_SYSCALL"},
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

	if stats["collector_id"].(string) != "k8s-collector" {
		t.Errorf("Expected collector_id to be 'k8s-collector', got '%s'", stats["collector_id"].(string))
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
		Type:   domain.EventTypeKubernetes,
		Source: "k8s-collector",
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

	// Try to send another event after close (should fail or be ignored)
	err = client.SendEvent(ctx, event)
	// We expect this to either fail or be silently ignored
	// The exact behavior depends on the implementation
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
		Type:   domain.EventTypeKubernetes,
		Source: "k8s-collector",
		Entity: &domain.EntityContext{
			Type:      "pod",
			Name:      "benchmark-pod",
			Namespace: "default",
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

