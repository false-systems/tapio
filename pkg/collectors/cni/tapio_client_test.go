package cni

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

	if client.collectorID != "cni-collector" {
		t.Errorf("Expected collector ID 'cni-collector', got '%s'", client.collectorID)
	}
}

// TestTapioGRPCClient_SendEvent tests event sending functionality
func TestTapioGRPCClient_SendEvent(t *testing.T) {
	client, err := NewTapioGRPCClient("localhost:8080")
	if err != nil {
		t.Fatalf("Failed to create Tapio client: %v", err)
	}
	defer client.Close()

	// Create a sample CNI UnifiedEvent
	event := &domain.UnifiedEvent{
		ID:        "cni-test-event-123",
		Type:      domain.EventTypeNetwork,
		Source:    "cni-collector",
		Timestamp: time.Now(),
		Entity: &domain.EntityContext{
			Type:      "pod",
			Name:      "test-pod-123",
			Namespace: "default",
			Labels: map[string]string{
				"app":     "web-server",
				"cluster": "production",
				"node":    "worker-1",
			},
		},
		Semantic: &domain.SemanticContext{
			Intent:     "CNI network operation",
			Category:   "network",
			Tags:       []string{"cni", "network", "pod", "allocation"},
			Narrative:  "IP allocation for pod test-pod-123 in default namespace",
			Confidence: 0.95,
		},
		Impact: &domain.ImpactContext{
			Severity:         "low",
			BusinessImpact:   0.1,
			AffectedServices: []string{"web-server"},
		},
		Network: &domain.NetworkData{
			SourceIP:   "10.244.1.5",
			DestIP:     "10.96.0.1",
			Protocol:   "TCP",
			Direction:  "outbound",
			SourcePort: 8080,
			DestPort:   80,
		},
		Kubernetes: &domain.KubernetesData{
			EventType:   "Normal",
			Reason:      "NetworkReady",
			Message:     "Pod network configured successfully",
			Action:      "ADDED",
			ObjectKind:  "Pod",
			Object:      "pod/test-pod-123",
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

	// Create a batch of sample CNI UnifiedEvents
	events := make([]*domain.UnifiedEvent, 5)
	for i := 0; i < 5; i++ {
		events[i] = &domain.UnifiedEvent{
			ID:        fmt.Sprintf("cni-batch-event-%d", i),
			Type:      domain.EventTypeNetwork,
			Source:    "cni-collector",
			Timestamp: time.Now(),
			Entity: &domain.EntityContext{
				Type:      "pod",
				Name:      fmt.Sprintf("test-pod-%d", i),
				Namespace: "test",
				Labels: map[string]string{
					"cluster": "test-cluster",
					"node":    fmt.Sprintf("worker-%d", i%3),
				},
			},
			Semantic: &domain.SemanticContext{
				Intent:     "CNI network operation",
				Category:   "network",
				Confidence: 0.9,
			},
			Network: &domain.NetworkData{
				SourceIP:   fmt.Sprintf("10.244.%d.%d", i/254, i%254+1),
				Protocol:   "TCP",
				Direction:  "ingress",
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
		CollectorID:   "custom-cni-collector",
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

	if client.collectorID != "custom-cni-collector" {
		t.Errorf("Expected collector ID 'custom-cni-collector', got '%s'", client.collectorID)
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
		{domain.EventTypeNetwork, "EVENT_TYPE_NETWORK"},
		{domain.EventTypeKubernetes, "EVENT_TYPE_KUBERNETES"},
		{domain.EventTypeSystem, "EVENT_TYPE_SYSCALL"},
		{domain.EventTypeProcess, "EVENT_TYPE_PROCESS"},
		{domain.EventTypeService, "EVENT_TYPE_HTTP"},
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

// TestTapioGRPCClient_SourceMapping tests source type mapping
func TestTapioGRPCClient_SourceMapping(t *testing.T) {
	client, err := NewTapioGRPCClient("localhost:8080")
	if err != nil {
		t.Fatalf("Failed to create Tapio client: %v", err)
	}
	defer client.Close()

	// Test source type mapping
	sourceTestCases := []struct {
		domainSource domain.SourceType
		expected     string
	}{
		{domain.SourceCNI, "SOURCE_TYPE_KUBERNETES_API"},
		{domain.SourceK8s, "SOURCE_TYPE_KUBERNETES_API"},
		{domain.SourceEBPF, "SOURCE_TYPE_EBPF"},
		{domain.SourceSystemd, "SOURCE_TYPE_JOURNALD"},
	}

	for _, tc := range sourceTestCases {
		pbSource := client.mapSourceType(tc.domainSource)
		if pbSource.String() != tc.expected {
			t.Errorf("Source type mapping failed: expected %s, got %s", tc.expected, pbSource.String())
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

	if stats["collector_id"].(string) != "cni-collector" {
		t.Errorf("Expected collector_id to be 'cni-collector', got '%s'", stats["collector_id"].(string))
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
		Type:   domain.EventTypeNetwork,
		Source: "cni-collector",
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
			Message: "CNI plugin executed successfully",
		},
	}

	message := client.extractMessage(event)
	expected := "CNI plugin executed successfully"
	if message != expected {
		t.Errorf("Expected message '%s', got '%s'", expected, message)
	}

	// Test message extraction from Kubernetes context
	event2 := &domain.UnifiedEvent{
		Kubernetes: &domain.KubernetesData{
			Message: "Pod network configured",
		},
	}

	message2 := client.extractMessage(event2)
	expected2 := "Pod network configured"
	if message2 != expected2 {
		t.Errorf("Expected message '%s', got '%s'", expected2, message2)
	}

	// Test message extraction from Semantic context
	event3 := &domain.UnifiedEvent{
		Semantic: &domain.SemanticContext{
			Narrative: "Network interface created for pod",
		},
	}

	message3 := client.extractMessage(event3)
	expected3 := "Network interface created for pod"
	if message3 != expected3 {
		t.Errorf("Expected message '%s', got '%s'", expected3, message3)
	}

	// Test fallback message
	event4 := &domain.UnifiedEvent{
		Type:   domain.EventTypeNetwork,
		Source: "cni",
	}

	message4 := client.extractMessage(event4)
	expected4 := "CNI event network from cni"
	if message4 != expected4 {
		t.Errorf("Expected message '%s', got '%s'", expected4, message4)
	}
}

// TestTapioGRPCClient_ExtractTags tests tag extraction from events
func TestTapioGRPCClient_ExtractTags(t *testing.T) {
	client, err := NewTapioGRPCClient("localhost:8080")
	if err != nil {
		t.Fatalf("Failed to create Tapio client: %v", err)
	}
	defer client.Close()

	// Test tag extraction from Semantic context
	event := &domain.UnifiedEvent{
		Type:   domain.EventTypeNetwork,
		Source: "cni-collector",
		Semantic: &domain.SemanticContext{
			Tags: []string{"cni", "network", "pod", "allocation"},
		},
	}

	tags := client.extractTags(event)
	expectedTags := []string{"cni", "network", "pod", "allocation"}
	if len(tags) != len(expectedTags) {
		t.Errorf("Expected %d tags, got %d", len(expectedTags), len(tags))
	}

	// Check that all expected tags are present
	tagMap := make(map[string]bool)
	for _, tag := range tags {
		tagMap[tag] = true
	}

	for _, expectedTag := range expectedTags {
		if !tagMap[expectedTag] {
			t.Errorf("Expected tag '%s' not found in tags: %v", expectedTag, tags)
		}
	}

	// Test fallback tag generation
	event2 := &domain.UnifiedEvent{
		Type:   domain.EventTypeNetwork,
		Source: "cni",
		Entity: &domain.EntityContext{
			Type: "pod",
		},
	}

	tags2 := client.extractTags(event2)
	expectedTags2 := []string{"network", "cni", "pod"}
	if len(tags2) != len(expectedTags2) {
		t.Errorf("Expected %d fallback tags, got %d", len(expectedTags2), len(tags2))
	}
}

// TestTapioGRPCClient_ConvertEventAttributes tests attribute conversion
func TestTapioGRPCClient_ConvertEventAttributes(t *testing.T) {
	client, err := NewTapioGRPCClient("localhost:8080")
	if err != nil {
		t.Fatalf("Failed to create Tapio client: %v", err)
	}
	defer client.Close()

	// Create event with rich attribute data
	event := &domain.UnifiedEvent{
		Entity: &domain.EntityContext{
			UID:       "pod-123-uid",
			Type:      "pod",
			Name:      "test-pod",
			Namespace: "default",
			Labels: map[string]string{
				"app":     "web",
				"version": "v1.0",
			},
			Attributes: map[string]string{
				"cpu":    "100m",
				"memory": "128Mi",
			},
		},
		Network: &domain.NetworkData{
			SourceIP: "10.244.1.5",
			Protocol: "TCP",
		},
		Kubernetes: &domain.KubernetesData{
			EventType:  "Normal",
			Reason:     "NetworkReady",
			ObjectKind: "Pod",
		},
	}

	attributes := client.convertEventAttributes(event)

	// Test entity attributes
	if attributes["entity.uid"] != "pod-123-uid" {
		t.Errorf("Expected entity.uid to be 'pod-123-uid', got '%s'", attributes["entity.uid"])
	}

	if attributes["entity.name"] != "test-pod" {
		t.Errorf("Expected entity.name to be 'test-pod', got '%s'", attributes["entity.name"])
	}

	if attributes["entity.label.app"] != "web" {
		t.Errorf("Expected entity.label.app to be 'web', got '%s'", attributes["entity.label.app"])
	}

	if attributes["entity.cpu"] != "100m" {
		t.Errorf("Expected entity.cpu to be '100m', got '%s'", attributes["entity.cpu"])
	}

	// Test network attributes
	if attributes["network.protocol"] != "TCP" {
		t.Errorf("Expected network.protocol to be 'TCP', got '%s'", attributes["network.protocol"])
	}

	if attributes["network.source_ip"] != "10.244.1.5" {
		t.Errorf("Expected network.source_ip to be '10.244.1.5', got '%s'", attributes["network.source_ip"])
	}

	// Test Kubernetes attributes
	if attributes["k8s.event_type"] != "Normal" {
		t.Errorf("Expected k8s.event_type to be 'Normal', got '%s'", attributes["k8s.event_type"])
	}

	if attributes["k8s.object_kind"] != "Pod" {
		t.Errorf("Expected k8s.object_kind to be 'Pod', got '%s'", attributes["k8s.object_kind"])
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
		Type:   domain.EventTypeNetwork,
		Source: "cni-collector",
		Entity: &domain.EntityContext{
			Type: "pod",
			Name: "benchmark-pod",
		},
		Network: &domain.NetworkData{
			Protocol: "TCP",
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