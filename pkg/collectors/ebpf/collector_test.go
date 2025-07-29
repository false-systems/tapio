package ebpf

import (
	"context"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
)

func TestSimpleCollectorInterface(t *testing.T) {
	config := DefaultSimpleConfig()
	collector := NewSimpleCollector(config)

	// Test Name
	if collector.Name() != "ebpf" {
		t.Errorf("expected name 'ebpf', got %s", collector.Name())
	}

	// Test initial health
	if !collector.IsHealthy() {
		t.Error("expected collector to be healthy initially")
	}

	// Test events channel exists
	events := collector.Events()
	if events == nil {
		t.Error("expected non-nil events channel")
	}
}

func TestSimpleCollectorStartStop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping eBPF test in short mode")
	}

	config := DefaultSimpleConfig()
	collector := NewSimpleCollector(config)

	ctx := context.Background()

	// Multiple starts should be idempotent
	for i := 0; i < 3; i++ {
		err := collector.Start(ctx)
		// Note: This might fail if not running as root or if eBPF is not available
		if err != nil {
			t.Skipf("skipping test: %v (probably need root or eBPF support)", err)
		}
	}

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	// Multiple stops should be idempotent
	for i := 0; i < 3; i++ {
		if err := collector.Stop(); err != nil {
			t.Fatalf("stop %d failed: %v", i, err)
		}
	}

	// After stop, should not be healthy
	if collector.IsHealthy() {
		t.Error("expected collector to be unhealthy after stop")
	}

	// Channel should be closed
	select {
	case _, ok := <-collector.Events():
		if ok {
			t.Error("expected events channel to be closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("events channel not closed after stop")
	}
}

func TestSimpleCollectorConfig(t *testing.T) {
	config := DefaultSimpleConfig()

	if config.BufferSize != 1000 {
		t.Errorf("expected buffer size 1000, got %d", config.BufferSize)
	}

	if config.Labels["collector"] != "ebpf" {
		t.Errorf("expected collector label 'ebpf', got %s", config.Labels["collector"])
	}
}

func TestSimpleCollectorEventType(t *testing.T) {
	collector := &SimpleCollector{}

	// Test memory allocation event
	allocData := make([]byte, 40)
	allocData[32] = 0 // event_type = alloc
	if et := collector.determineEventType(allocData); et != "memory_alloc" {
		t.Errorf("expected 'memory_alloc', got %s", et)
	}

	// Test memory free event
	freeData := make([]byte, 40)
	freeData[32] = 1 // event_type = free
	if et := collector.determineEventType(freeData); et != "memory_free" {
		t.Errorf("expected 'memory_free', got %s", et)
	}

	// Test OOM kill event
	oomData := make([]byte, 40)
	oomData[32] = 2 // event_type = oom
	if et := collector.determineEventType(oomData); et != "oom_kill" {
		t.Errorf("expected 'oom_kill', got %s", et)
	}

	// Test unknown event
	unknownData := make([]byte, 40)
	unknownData[32] = 99 // unknown type
	if et := collector.determineEventType(unknownData); et != "unknown" {
		t.Errorf("expected 'unknown', got %s", et)
	}

	// Test short data
	shortData := make([]byte, 10)
	if et := collector.determineEventType(shortData); et != "unknown" {
		t.Errorf("expected 'unknown' for short data, got %s", et)
	}
}

// mockSimpleCollector for testing without actual eBPF
type mockSimpleCollector struct {
	*SimpleCollector
}

func newMockSimpleCollector(config collectors.CollectorConfig) *mockSimpleCollector {
	return &mockSimpleCollector{
		SimpleCollector: &SimpleCollector{
			config:  config,
			events:  make(chan collectors.RawEvent, config.BufferSize),
			healthy: true,
		},
	}
}

func (m *mockSimpleCollector) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil
	}

	m.ctx, m.cancel = context.WithCancel(ctx)
	m.started = true

	// Simulate event generation
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()

		eventCount := 0
		for {
			select {
			case <-m.ctx.Done():
				return
			case <-ticker.C:
				// Create mock event data
				data := make([]byte, 40)
				data[32] = byte(eventCount % 3) // Cycle through event types

				event := collectors.RawEvent{
					Timestamp: time.Now(),
					Type:      "ebpf",
					Data:      data,
					Metadata: map[string]string{
						"cpu":        "0",
						"size":       "40",
						"event_type": m.determineEventType(data),
					},
				}

				select {
				case m.events <- event:
					eventCount++
				case <-m.ctx.Done():
					return
				}
			}
		}
	}()

	return nil
}

func TestMockSimpleCollector(t *testing.T) {
	config := DefaultSimpleConfig()
	collector := newMockSimpleCollector(config)

	ctx := context.Background()
	if err := collector.Start(ctx); err != nil {
		t.Fatalf("failed to start mock collector: %v", err)
	}

	// Collect some events
	eventTypes := make(map[string]int)
	timeout := time.After(100 * time.Millisecond)
	done := false

	for !done {
		select {
		case event := <-collector.Events():
			eventTypes[event.Metadata["event_type"]]++
			if len(eventTypes) >= 3 {
				done = true
			}
		case <-timeout:
			done = true
		}
	}

	// Should have seen all three event types
	if len(eventTypes) < 3 {
		t.Errorf("expected 3 event types, got %d: %v", len(eventTypes), eventTypes)
	}

	// Stop collector
	if err := collector.Stop(); err != nil {
		t.Fatalf("failed to stop collector: %v", err)
	}
}
