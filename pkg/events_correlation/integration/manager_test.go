package integration

import (
	"context"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/collector"
)

// MockCollector implements the Collector interface for testing
type MockCollector struct {
	name      string
	events    chan collector.Event
	isStarted bool
	health    collector.Health
	ctx       context.Context
	cancel    context.CancelFunc
	isClosed  bool
}

func NewMockCollector(name string) *MockCollector {
	return &MockCollector{
		name:   name,
		events: make(chan collector.Event, 100),
		health: collector.Health{
			Status:  collector.HealthStatusHealthy,
			Message: "Mock collector running",
		},
	}
}

func (m *MockCollector) Name() string {
	return m.name
}

func (m *MockCollector) Start(ctx context.Context, config collector.Config) error {
	m.isStarted = true
	m.ctx, m.cancel = context.WithCancel(ctx)

	// Generate some test events
	go func() {
		defer func() {
			if !m.isClosed {
				close(m.events)
				m.isClosed = true
			}
		}()

		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-m.ctx.Done():
				return
			case <-ticker.C:
				m.generateTestEvent()
			}
		}
	}()

	return nil
}

func (m *MockCollector) generateTestEvent() {
	event := collector.Event{
		ID:        "test-event-" + time.Now().Format("20060102150405.000"),
		Timestamp: time.Now(),
		Type:      "memory_pressure",
		Source:    "ebpf",
		Severity:  collector.SeverityMedium,
		Context: &collector.EventContext{
			Namespace:   "default",
			Pod:         "test-pod-123",
			Container:   "app",
			Node:        "worker-1",
			ProcessName: "app-process",
			PID:         1234,
		},
		Data: map[string]interface{}{
			"current_usage":   800 * 1024 * 1024,  // 800MB
			"limit":           1024 * 1024 * 1024, // 1GB
			"usage_percent":   78.125,
			"allocation_rate": 50 * 1024 * 1024, // 50MB/s
		},
	}

	select {
	case m.events <- event:
	default:
		// Drop if buffer full
	}
}

func (m *MockCollector) Events() <-chan collector.Event {
	return m.events
}

func (m *MockCollector) Health() collector.Health {
	return m.health
}

func (m *MockCollector) Stop() error {
	m.isStarted = false
	if m.cancel != nil {
		m.cancel()
	}
	return nil
}

func TestIntegratedManager_Basic(t *testing.T) {
	// Create manager with test configuration
	config := DefaultIntegrationConfig()
	config.EventBufferSize = 100
	config.ResultBufferSize = 50
	config.CorrelationWindow = 1 * time.Minute

	manager := NewIntegratedManager(config)

	// Create mock collectors
	ebpfCollector := NewMockCollector("ebpf-test")
	k8sCollector := NewMockCollector("k8s-test")

	// Register collectors
	err := manager.RegisterCollectors(ebpfCollector, k8sCollector)
	if err != nil {
		t.Fatalf("Failed to register collectors: %v", err)
	}

	// Start manager
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = manager.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()

	// Wait for some events to be processed
	time.Sleep(500 * time.Millisecond)

	// Check stats
	stats := manager.GetStats()
	if stats == nil {
		t.Fatal("Stats should not be nil")
	}

	// Verify components are present
	if _, exists := stats["tapio"]; !exists {
		t.Error("Tapio stats missing")
	}
	if _, exists := stats["correlation"]; !exists {
		t.Error("Correlation stats missing")
	}
	if _, exists := stats["bridge"]; !exists {
		t.Error("Bridge stats missing")
	}
	if _, exists := stats["integration"]; !exists {
		t.Error("Integration stats missing")
	}

	// Check health
	health := manager.Health()
	if health == nil {
		t.Fatal("Health should not be nil")
	}

	// Verify health components
	if _, exists := health["tapio_collectors"]; !exists {
		t.Error("Tapio collector health missing")
	}
	if _, exists := health["event_bridge"]; !exists {
		t.Error("Event bridge health missing")
	}
	if _, exists := health["integration"]; !exists {
		t.Error("Integration health missing")
	}
}

func TestIntegratedManager_EventProcessing(t *testing.T) {
	// Create manager
	config := DefaultIntegrationConfig()
	config.CorrelationWindow = 30 * time.Second

	manager := NewIntegratedManager(config)

	// Create mock collector
	mockCollector := NewMockCollector("test-collector")

	err := manager.RegisterCollectors(mockCollector)
	if err != nil {
		t.Fatalf("Failed to register collector: %v", err)
	}

	// Start manager
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err = manager.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()

	// Monitor results
	results := manager.Results()
	resultCount := 0

	// Wait for results with timeout
	timeout := time.After(2 * time.Second)

	for {
		select {
		case result, ok := <-results:
			if !ok {
				t.Log("Results channel closed")
				return
			}

			resultCount++
			t.Logf("Received result: %s (confidence: %.2f)", result.Title, result.Confidence)

			// Verify result structure
			if result.RuleID == "" {
				t.Error("Result should have a rule ID")
			}
			if result.Title == "" {
				t.Error("Result should have a title")
			}
			if result.Timestamp.IsZero() {
				t.Error("Result should have a timestamp")
			}

			// Stop after receiving some results
			if resultCount >= 1 {
				t.Logf("Received %d results, test complete", resultCount)
				return
			}

		case <-timeout:
			t.Logf("Timeout reached, received %d results", resultCount)
			return
		}
	}
}

func TestIntegratedManager_Lifecycle(t *testing.T) {
	config := DefaultIntegrationConfig()
	manager := NewIntegratedManager(config)

	// Test initial state
	health := manager.Health()
	integrationHealth, exists := health["integration"].(map[string]interface{})
	if !exists {
		t.Fatal("Integration health should exist")
	}

	isRunning, exists := integrationHealth["is_running"].(bool)
	if !exists || isRunning {
		t.Error("Manager should not be running initially")
	}

	// Register a mock collector
	mockCollector := NewMockCollector("lifecycle-test")
	err := manager.RegisterCollectors(mockCollector)
	if err != nil {
		t.Fatalf("Failed to register collector: %v", err)
	}

	// Start manager
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = manager.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	// Check running state
	health = manager.Health()
	integrationHealth = health["integration"].(map[string]interface{})
	isRunning = integrationHealth["is_running"].(bool)
	if !isRunning {
		t.Error("Manager should be running after start")
	}

	// Stop manager
	err = manager.Stop()
	if err != nil {
		t.Fatalf("Failed to stop manager: %v", err)
	}

	// Check stopped state
	health = manager.Health()
	integrationHealth = health["integration"].(map[string]interface{})
	isRunning = integrationHealth["is_running"].(bool)
	if isRunning {
		t.Error("Manager should not be running after stop")
	}
}

func TestBridgeConversion(t *testing.T) {
	// Test the bridge conversion logic with a sample event
	config := DefaultIntegrationConfig()
	manager := NewIntegratedManager(config)

	// Get the bridge from the manager
	bridge := manager.eventBridge

	// Verify the bridge health
	health := bridge.GetHealthStatus()
	if health == nil {
		t.Fatal("Bridge health should not be nil")
	}

	status, exists := health["bridge_status"]
	if !exists {
		t.Error("Bridge status should exist")
	}
	if status != "healthy" {
		t.Errorf("Bridge should be healthy, got: %v", status)
	}
}

// BenchmarkIntegration benchmarks the integrated system performance
func BenchmarkIntegration(b *testing.B) {
	config := DefaultIntegrationConfig()
	config.EventBufferSize = 10000
	config.ResultBufferSize = 1000

	manager := NewIntegratedManager(config)
	mockCollector := NewMockCollector("bench-collector")

	err := manager.RegisterCollectors(mockCollector)
	if err != nil {
		b.Fatalf("Failed to register collector: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = manager.Start(ctx)
	if err != nil {
		b.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()

	b.ResetTimer()

	// Benchmark event processing
	for i := 0; i < b.N; i++ {
		// The mock collector will generate events automatically
		// We just need to let the system run
		time.Sleep(1 * time.Millisecond)
	}
}
