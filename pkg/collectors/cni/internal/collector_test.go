package internal

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/cni/core"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestCNICollector_NewCNICollector(t *testing.T) {
	tests := []struct {
		name      string
		config    core.Config
		expectErr bool
	}{
		{
			name: "valid config",
			config: core.Config{
				Name:                    "test-cni-collector",
				Enabled:                 true,
				EventBufferSize:         1000,
				EnableLogMonitoring:     true,
				EnableProcessMonitoring: true,
				PollInterval:            5 * time.Second,
			},
			expectErr: false,
		},
		{
			name: "config with defaults",
			config: core.Config{
				Name:    "test-collector",
				Enabled: true,
			},
			expectErr: false,
		},
		{
			name: "invalid config - no monitoring enabled",
			config: core.Config{
				Name:                    "invalid-collector",
				Enabled:                 true,
				EnableLogMonitoring:     false,
				EnableProcessMonitoring: false,
				EnableEventMonitoring:   false,
				EnableFileMonitoring:    false,
			},
			expectErr: false, // Should enable defaults
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCNICollector(tt.config)

			if tt.expectErr && err == nil {
				t.Errorf("expected error but got none")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !tt.expectErr && collector == nil {
				t.Errorf("expected collector but got nil")
			}

			if collector != nil {
				// Verify initial state
				if collector.config.Name != tt.config.Name {
					t.Errorf("expected name %s, got %s", tt.config.Name, collector.config.Name)
				}

				health := collector.Health()
				if health.Status != core.HealthStatusUnknown {
					t.Errorf("expected initial status unknown, got %s", health.Status)
				}

				if len(collector.monitors) == 0 {
					t.Errorf("expected at least one monitor")
				}
			}
		})
	}
}

func TestCNICollector_Lifecycle(t *testing.T) {
	config := core.Config{
		Name:                "test-collector",
		Enabled:             true,
		EventBufferSize:     10,
		EnableLogMonitoring: true,
		PollInterval:        1 * time.Second,
	}

	collector, err := NewCNICollector(config)
	if err != nil {
		t.Fatalf("failed to create collector: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test Start
	err = collector.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start collector: %v", err)
	}

	// Verify running state
	if !collector.running {
		t.Error("collector should be running")
	}

	health := collector.Health()
	if health.Status == core.HealthStatusUnknown {
		t.Error("health status should be updated after start")
	}

	// Test double start (should fail)
	err = collector.Start(ctx)
	if err == nil {
		t.Error("expected error when starting already running collector")
	}

	// Test Stop
	err = collector.Stop()
	if err != nil {
		t.Errorf("failed to stop collector: %v", err)
	}

	// Verify stopped state
	if collector.running {
		t.Error("collector should not be running")
	}

	// Test double stop (should succeed)
	err = collector.Stop()
	if err != nil {
		t.Errorf("double stop should not error: %v", err)
	}
}

func TestCNICollector_EventProcessing(t *testing.T) {
	config := core.Config{
		Name:            "test-collector",
		Enabled:         true,
		EventBufferSize: 10,
		// Disable actual monitoring to control test events
		EnableLogMonitoring:     false,
		EnableProcessMonitoring: false,
		EnableEventMonitoring:   false,
		EnableFileMonitoring:    false,
	}

	collector, err := NewCNICollector(config)
	if err != nil {
		t.Fatalf("failed to create collector: %v", err)
	}

	// Override with mock monitor for testing
	mockMonitor := &MockCNIMonitor{
		eventChan: make(chan core.CNIRawEvent, 10),
	}
	collector.monitors = []core.CNIMonitor{mockMonitor}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start collector: %v", err)
	}
	defer collector.Stop()

	// Send test event
	testEvent := core.CNIRawEvent{
		ID:         "test-event-1",
		Timestamp:  time.Now(),
		Source:     "test",
		Operation:  core.CNIOperationAdd,
		PluginName: "test-plugin",
		Success:    true,
		PodName:    "test-pod",
		PodUID:     "test-uid",
		AssignedIP: "10.0.0.1",
	}

	mockMonitor.eventChan <- testEvent

	// Wait for processed event
	select {
	case unifiedEvent := <-collector.Events():
		// Verify the event was processed correctly
		if unifiedEvent.ID == "" {
			t.Error("processed event should have ID")
		}
		if unifiedEvent.Source != string(domain.SourceCNI) {
			t.Errorf("expected source %s, got %s", domain.SourceCNI, unifiedEvent.Source)
		}
		if unifiedEvent.Kubernetes == nil {
			t.Error("expected Kubernetes context")
		}
		if !strings.Contains(unifiedEvent.Kubernetes.Object, testEvent.PodName) {
			t.Errorf("expected pod name %s in object %s", testEvent.PodName, unifiedEvent.Kubernetes.Object)
		}
		if unifiedEvent.Network == nil {
			t.Error("expected Network context")
		}
		if unifiedEvent.Network.SourceIP != testEvent.AssignedIP {
			t.Errorf("expected IP %s, got %s", testEvent.AssignedIP, unifiedEvent.Network.SourceIP)
		}

	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for processed event")
	}

	// Verify statistics were updated
	stats := collector.Statistics()
	if stats.EventsCollected == 0 {
		t.Error("expected events collected to be > 0")
	}
	if stats.CNIOperationsTotal == 0 {
		t.Error("expected CNI operations total to be > 0")
	}
}

func TestCNICollector_Configuration(t *testing.T) {
	initialConfig := core.Config{
		Name:            "initial-collector",
		Enabled:         true,
		EventBufferSize: 100,
	}

	collector, err := NewCNICollector(initialConfig)
	if err != nil {
		t.Fatalf("failed to create collector: %v", err)
	}

	// Test reconfiguration while stopped
	newConfig := core.Config{
		Name:                "updated-collector",
		Enabled:             true,
		EventBufferSize:     200,
		EnableLogMonitoring: true,
	}

	err = collector.Configure(newConfig)
	if err != nil {
		t.Errorf("failed to reconfigure: %v", err)
	}

	if collector.config.Name != newConfig.Name {
		t.Errorf("expected name %s, got %s", newConfig.Name, collector.config.Name)
	}

	// Test reconfiguration while running (should fail)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start collector: %v", err)
	}
	defer collector.Stop()

	err = collector.Configure(newConfig)
	if err == nil {
		t.Error("expected error when reconfiguring running collector")
	}
}

func TestCNICollector_HealthMonitoring(t *testing.T) {
	config := core.Config{
		Name:            "health-test-collector",
		Enabled:         true,
		EventBufferSize: 10,
	}

	collector, err := NewCNICollector(config)
	if err != nil {
		t.Fatalf("failed to create collector: %v", err)
	}

	// Initial health should be unknown
	health := collector.Health()
	if health.Status != core.HealthStatusUnknown {
		t.Errorf("expected initial status unknown, got %s", health.Status)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start collector: %v", err)
	}
	defer collector.Stop()

	// Allow health monitoring to run
	time.Sleep(100 * time.Millisecond)

	health = collector.Health()
	if health.Status == core.HealthStatusUnknown {
		t.Error("health status should be updated after start")
	}

	if health.ActiveMonitors != len(collector.monitors) {
		t.Errorf("expected %d active monitors, got %d",
			len(collector.monitors), health.ActiveMonitors)
	}

	// Test error recording
	collector.recordError(fmt.Errorf("test error"))

	stats := collector.Statistics()
	if stats.MonitoringErrors == 0 {
		t.Error("expected monitoring errors to be > 0 after recording error")
	}
}

func TestCNICollector_Statistics(t *testing.T) {
	config := core.Config{
		Name:            "stats-test-collector",
		Enabled:         true,
		EventBufferSize: 10,
	}

	collector, err := NewCNICollector(config)
	if err != nil {
		t.Fatalf("failed to create collector: %v", err)
	}

	stats := collector.Statistics()
	if stats.StartTime.IsZero() {
		t.Error("start time should be set")
	}

	if stats.PluginExecutionTime == nil {
		t.Error("plugin execution time map should be initialized")
	}

	if stats.Custom == nil {
		t.Error("custom stats map should be initialized")
	}

	// Test statistics update
	rawEvent := core.CNIRawEvent{
		Operation:  core.CNIOperationAdd,
		PluginName: "test-plugin",
		Success:    true,
		Duration:   100 * time.Millisecond,
		AssignedIP: "10.0.0.1",
	}

	unifiedEvent := &domain.UnifiedEvent{
		Kubernetes: &domain.KubernetesData{Object: "pod/test-pod"},
	}

	collector.updateStatistics(rawEvent, unifiedEvent)

	updatedStats := collector.Statistics()
	if updatedStats.CNIOperationsTotal != 1 {
		t.Errorf("expected 1 CNI operation, got %d", updatedStats.CNIOperationsTotal)
	}

	if updatedStats.IPAllocationsTotal != 1 {
		t.Errorf("expected 1 IP allocation, got %d", updatedStats.IPAllocationsTotal)
	}

	if updatedStats.K8sEventsProcessed != 1 {
		t.Errorf("expected 1 K8s event, got %d", updatedStats.K8sEventsProcessed)
	}

	if _, exists := updatedStats.PluginExecutionTime["test-plugin"]; !exists {
		t.Error("expected plugin execution time to be recorded")
	}
}

// MockCNIMonitor for testing
type MockCNIMonitor struct {
	eventChan chan core.CNIRawEvent
	running   bool
}

func (m *MockCNIMonitor) Start(ctx context.Context) error {
	m.running = true
	return nil
}

func (m *MockCNIMonitor) Stop() error {
	m.running = false
	if m.eventChan != nil {
		close(m.eventChan)
	}
	return nil
}

func (m *MockCNIMonitor) Events() <-chan core.CNIRawEvent {
	return m.eventChan
}

func (m *MockCNIMonitor) MonitorType() string {
	return "mock"
}

// Benchmark tests
func BenchmarkCNICollector_EventProcessing(b *testing.B) {
	config := core.Config{
		Name:            "bench-collector",
		Enabled:         true,
		EventBufferSize: 1000,
	}

	collector, err := NewCNICollector(config)
	if err != nil {
		b.Fatalf("failed to create collector: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	if err != nil {
		b.Fatalf("failed to start collector: %v", err)
	}
	defer collector.Stop()

	// Prepare test events
	testEvent := core.CNIRawEvent{
		ID:         "bench-event",
		Timestamp:  time.Now(),
		Source:     "bench",
		Operation:  core.CNIOperationAdd,
		PluginName: "bench-plugin",
		Success:    true,
		PodName:    "bench-pod",
		AssignedIP: "10.0.0.1",
	}

	b.ResetTimer()

	// Benchmark event processing
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			unifiedEvent, err := collector.processor.ProcessEvent(ctx, testEvent)
			if err != nil {
				b.Errorf("failed to process event: %v", err)
			}
			if unifiedEvent == nil {
				b.Error("processed event is nil")
			}
		}
	})
}
