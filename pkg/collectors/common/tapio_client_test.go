package common

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
)

// MockAdapter implements CollectorAdapter for testing
type MockAdapter struct {
	CollectorID    string
	TracerName     string
	BatchIDPrefix  string
}

func NewMockAdapter() *MockAdapter {
	return &MockAdapter{
		CollectorID:   "test-collector",
		TracerName:    "tapio.test.collector",
		BatchIDPrefix: "test-batch",
	}
}

func (a *MockAdapter) GetCollectorID() string {
	return a.CollectorID
}

func (a *MockAdapter) GetTracerName() string {
	return a.TracerName
}

func (a *MockAdapter) GetBatchIDPrefix() string {
	return a.BatchIDPrefix
}

func (a *MockAdapter) MapEventType(eventType domain.EventType) pb.EventType {
	switch eventType {
	case domain.EventTypeKubernetes:
		return pb.EventType_EVENT_TYPE_KUBERNETES
	case domain.EventTypeNetwork:
		return pb.EventType_EVENT_TYPE_NETWORK
	case domain.EventTypeProcess:
		return pb.EventType_EVENT_TYPE_PROCESS
	default:
		return pb.EventType_EVENT_TYPE_KUBERNETES
	}
}

func (a *MockAdapter) MapSourceType(source domain.SourceType) pb.SourceType {
	switch source {
	case domain.SourceK8s:
		return pb.SourceType_SOURCE_TYPE_KUBERNETES_API
	case domain.SourceEBPF:
		return pb.SourceType_SOURCE_TYPE_EBPF
	default:
		return pb.SourceType_SOURCE_TYPE_KUBERNETES_API
	}
}

func (a *MockAdapter) ExtractMessage(event *domain.UnifiedEvent) string {
	if event.Semantic != nil && event.Semantic.Narrative != "" {
		return event.Semantic.Narrative
	}
	return fmt.Sprintf("Test event %s from %s", event.Type, event.Source)
}

func (a *MockAdapter) CreateEventContext(event *domain.UnifiedEvent) *pb.EventContext {
	if event.Entity == nil {
		return nil
	}
	return &pb.EventContext{
		Namespace: event.Entity.Namespace,
		Labels:    event.Entity.Labels,
	}
}

func (a *MockAdapter) ExtractAttributes(event *domain.UnifiedEvent) map[string]string {
	attributes := make(map[string]string)
	if event.Entity != nil {
		if event.Entity.Name != "" {
			attributes["entity.name"] = event.Entity.Name
		}
		if event.Entity.Type != "" {
			attributes["entity.type"] = event.Entity.Type
		}
	}
	return attributes
}

// TestTapioGRPCClient_NewClient tests basic client creation
func TestTapioGRPCClient_NewClient(t *testing.T) {
	adapter := NewMockAdapter()
	client, err := NewTapioGRPCClient("localhost:8080", adapter)
	if err != nil {
		t.Fatalf("Failed to create Tapio client: %v", err)
	}
	defer client.Close()

	stats := client.GetStatistics()
	if stats["server_addr"].(string) != "localhost:8080" {
		t.Errorf("Expected server address 'localhost:8080', got '%s'", stats["server_addr"].(string))
	}

	if stats["collector_id"].(string) != "test-collector" {
		t.Errorf("Expected collector ID 'test-collector', got '%s'", stats["collector_id"].(string))
	}
}

// TestTapioGRPCClient_SendEvent tests event sending functionality
func TestTapioGRPCClient_SendEvent(t *testing.T) {
	adapter := NewMockAdapter()
	client, err := NewTapioGRPCClient("localhost:8080", adapter)
	if err != nil {
		t.Fatalf("Failed to create Tapio client: %v", err)
	}
	defer client.Close()

	// Create a sample UnifiedEvent
	event := &domain.UnifiedEvent{
		ID:        "test-event-123",
		Type:      domain.EventTypeKubernetes,
		Source:    "test-collector",
		Timestamp: time.Now(),
		Entity: &domain.EntityContext{
			Type:      "pod",
			Name:      "test-pod",
			Namespace: "default",
			Labels: map[string]string{
				"app":     "test-app",
				"version": "v1.0",
			},
		},
		Semantic: &domain.SemanticContext{
			Intent:     "Test event",
			Category:   "test",
			Tags:       []string{"test", "unit"},
			Narrative:  "A test event for unit testing",
			Confidence: 0.9,
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
	adapter := NewMockAdapter()
	client, err := NewTapioGRPCClient("localhost:8080", adapter)
	if err != nil {
		t.Fatalf("Failed to create Tapio client: %v", err)
	}
	defer client.Close()

	// Create a batch of sample UnifiedEvents
	events := make([]*domain.UnifiedEvent, 5)
	for i := 0; i < 5; i++ {
		events[i] = &domain.UnifiedEvent{
			ID:        fmt.Sprintf("batch-event-%d", i),
			Type:      domain.EventTypeKubernetes,
			Source:    "test-collector",
			Timestamp: time.Now(),
			Entity: &domain.EntityContext{
				Type:      "service",
				Name:      fmt.Sprintf("test-service-%d", i),
				Namespace: "default",
			},
			Semantic: &domain.SemanticContext{
				Intent:     "Batch test event",
				Category:   "test",
				Confidence: 0.9,
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
		BufferSize:    5000,
		BatchSize:     50,
		FlushInterval: 2 * time.Second,
		RetryInterval: 10 * time.Second,
		MaxRetries:    3,
		EnableOTEL:    false, // Disable OTEL for this test
	}

	adapter := NewMockAdapter()
	adapter.CollectorID = "custom-test-collector"

	client, err := NewTapioGRPCClientWithConfig(config, adapter)
	if err != nil {
		t.Fatalf("Failed to create Tapio client with custom config: %v", err)
	}
	defer client.Close()

	stats := client.GetStatistics()
	if stats["server_addr"].(string) != "custom.server:9090" {
		t.Errorf("Expected server address 'custom.server:9090', got '%s'", stats["server_addr"].(string))
	}

	if stats["collector_id"].(string) != "custom-test-collector" {
		t.Errorf("Expected collector ID 'custom-test-collector', got '%s'", stats["collector_id"].(string))
	}

	if stats["buffer_capacity"].(int) != 5000 {
		t.Errorf("Expected buffer capacity 5000, got %d", stats["buffer_capacity"].(int))
	}
}

// TestTapioGRPCClient_EventMapping tests the event conversion
func TestTapioGRPCClient_EventMapping(t *testing.T) {
	adapter := NewMockAdapter()
	client, err := NewTapioGRPCClient("localhost:8080", adapter)
	if err != nil {
		t.Fatalf("Failed to create Tapio client: %v", err)
	}
	defer client.Close()

	event := &domain.UnifiedEvent{
		ID:        "mapping-test-event",
		Type:      domain.EventTypeKubernetes,
		Source:    "test-collector",
		Timestamp: time.Now(),
		Entity: &domain.EntityContext{
			Type:      "pod",
			Name:      "test-pod",
			Namespace: "default",
		},
		Semantic: &domain.SemanticContext{
			Narrative:  "Test event for mapping",
			Confidence: 0.8,
		},
	}

	ctx := context.Background()
	pbEvent := client.convertUnifiedEventToProto(ctx, event)

	if pbEvent.Id != event.ID {
		t.Errorf("Expected event ID '%s', got '%s'", event.ID, pbEvent.Id)
	}

	if pbEvent.Type != pb.EventType_EVENT_TYPE_KUBERNETES {
		t.Errorf("Expected event type EVENT_TYPE_KUBERNETES, got %s", pbEvent.Type.String())
	}

	if pbEvent.CollectorId != "test-collector" {
		t.Errorf("Expected collector ID 'test-collector', got '%s'", pbEvent.CollectorId)
	}

	if pbEvent.Message != "Test event for mapping" {
		t.Errorf("Expected message 'Test event for mapping', got '%s'", pbEvent.Message)
	}

	if pbEvent.Confidence < 0.79 || pbEvent.Confidence > 0.81 {
		t.Errorf("Expected confidence around 0.8, got %f", pbEvent.Confidence)
	}
}

// TestTapioGRPCClient_Statistics tests the statistics functionality
func TestTapioGRPCClient_Statistics(t *testing.T) {
	adapter := NewMockAdapter()
	client, err := NewTapioGRPCClient("localhost:8080", adapter)
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

	if stats["collector_id"].(string) != "test-collector" {
		t.Errorf("Expected collector_id to be 'test-collector', got '%s'", stats["collector_id"].(string))
	}
}

// TestTapioGRPCClient_Close tests the client close functionality
func TestTapioGRPCClient_Close(t *testing.T) {
	adapter := NewMockAdapter()
	client, err := NewTapioGRPCClient("localhost:8080", adapter)
	if err != nil {
		t.Fatalf("Failed to create Tapio client: %v", err)
	}

	// Verify client is created and functioning
	stats := client.GetStatistics()
	if stats["connected"].(bool) != false {
		t.Errorf("Expected client to be disconnected initially")
	}

	// Close the client - this should not return an error
	err = client.Close()
	if err != nil {
		t.Errorf("Failed to close client: %v", err)
	}

	// Verify multiple closes don't cause issues
	err = client.Close()
	if err != nil {
		t.Errorf("Second close should not fail: %v", err)
	}
}

// Benchmark test for event sending
func BenchmarkTapioGRPCClient_SendEvent(b *testing.B) {
	adapter := NewMockAdapter()
	client, err := NewTapioGRPCClient("localhost:8080", adapter)
	if err != nil {
		b.Fatalf("Failed to create Tapio client: %v", err)
	}
	defer client.Close()

	event := &domain.UnifiedEvent{
		ID:     "benchmark-event",
		Type:   domain.EventTypeKubernetes,
		Source: "test-collector",
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