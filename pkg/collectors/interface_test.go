package collectors

import (
	"context"
	"sync"
	"testing"
	"time"
)

// mockCollector implements Collector interface for testing
type mockCollector struct {
	name       string
	events     chan RawEvent
	ctx        context.Context
	cancel     context.CancelFunc
	started    bool
	stopped    bool
	healthy    bool
	mu         sync.Mutex
	eventCount int
}

func newMockCollector(name string, bufferSize int) *mockCollector {
	return &mockCollector{
		name:    name,
		events:  make(chan RawEvent, bufferSize),
		healthy: true,
	}
}

func (m *mockCollector) Name() string {
	return m.name
}

func (m *mockCollector) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil
	}

	m.ctx, m.cancel = context.WithCancel(ctx)
	m.started = true

	// Simulate event generation
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-m.ctx.Done():
				close(m.events)
				return
			case <-ticker.C:
				event := RawEvent{
					Timestamp: time.Now(),
					Type:      m.name,
					Data:      []byte("test event"),
					Metadata: map[string]string{
						"source": "mock",
					},
				}
				select {
				case m.events <- event:
					m.mu.Lock()
					m.eventCount++
					m.mu.Unlock()
				case <-m.ctx.Done():
					close(m.events)
					return
				}
			}
		}
	}()

	return nil
}

func (m *mockCollector) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started || m.stopped {
		return nil
	}

	m.cancel()
	m.stopped = true
	m.healthy = false
	return nil
}

func (m *mockCollector) Events() <-chan RawEvent {
	return m.events
}

func (m *mockCollector) IsHealthy() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.healthy
}

func TestCollectorInterface(t *testing.T) {
	ctx := context.Background()
	collector := newMockCollector("test-collector", 100)

	// Test Name
	if collector.Name() != "test-collector" {
		t.Errorf("expected name 'test-collector', got %s", collector.Name())
	}

	// Test IsHealthy before start
	if !collector.IsHealthy() {
		t.Error("expected collector to be healthy before start")
	}

	// Test Start
	if err := collector.Start(ctx); err != nil {
		t.Fatalf("failed to start collector: %v", err)
	}

	// Test event generation
	eventCount := 0
	timeout := time.After(100 * time.Millisecond)
	done := false

	for !done {
		select {
		case event := <-collector.Events():
			if event.Type != "test-collector" {
				t.Errorf("expected event type 'test-collector', got %s", event.Type)
			}
			if string(event.Data) != "test event" {
				t.Errorf("expected event data 'test event', got %s", string(event.Data))
			}
			if event.Metadata["source"] != "mock" {
				t.Errorf("expected metadata source 'mock', got %s", event.Metadata["source"])
			}
			eventCount++
			if eventCount >= 3 {
				done = true
			}
		case <-timeout:
			done = true
		}
	}

	if eventCount < 3 {
		t.Errorf("expected at least 3 events, got %d", eventCount)
	}

	// Test Stop
	if err := collector.Stop(); err != nil {
		t.Fatalf("failed to stop collector: %v", err)
	}

	// Verify channel is closed
	select {
	case _, ok := <-collector.Events():
		if ok {
			t.Error("expected events channel to be closed after stop")
		}
	case <-time.After(50 * time.Millisecond):
		t.Error("events channel not closed after stop")
	}

	// Test IsHealthy after stop
	if collector.IsHealthy() {
		t.Error("expected collector to be unhealthy after stop")
	}
}

func TestRawEvent(t *testing.T) {
	now := time.Now()
	event := RawEvent{
		Timestamp: now,
		Type:      "test",
		Data:      []byte("test data"),
		Metadata: map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
	}

	if !event.Timestamp.Equal(now) {
		t.Errorf("expected timestamp %v, got %v", now, event.Timestamp)
	}

	if event.Type != "test" {
		t.Errorf("expected type 'test', got %s", event.Type)
	}

	if string(event.Data) != "test data" {
		t.Errorf("expected data 'test data', got %s", string(event.Data))
	}

	if len(event.Metadata) != 2 {
		t.Errorf("expected 2 metadata entries, got %d", len(event.Metadata))
	}
}

func TestCollectorConfig(t *testing.T) {
	// Test default config
	config := DefaultCollectorConfig()

	if config.BufferSize != 1000 {
		t.Errorf("expected default buffer size 1000, got %d", config.BufferSize)
	}

	if !config.MetricsEnabled {
		t.Error("expected metrics to be enabled by default")
	}

	if config.Labels == nil {
		t.Error("expected labels map to be initialized")
	}

	// Test custom config
	customConfig := CollectorConfig{
		BufferSize:     500,
		MetricsEnabled: false,
		Labels: map[string]string{
			"env": "test",
		},
	}

	if customConfig.BufferSize != 500 {
		t.Errorf("expected buffer size 500, got %d", customConfig.BufferSize)
	}

	if customConfig.MetricsEnabled {
		t.Error("expected metrics to be disabled")
	}

	if customConfig.Labels["env"] != "test" {
		t.Errorf("expected label env='test', got %s", customConfig.Labels["env"])
	}
}

func TestConcurrentCollectors(t *testing.T) {
	ctx := context.Background()
	collectors := make([]Collector, 3)

	// Create and start multiple collectors
	for i := 0; i < 3; i++ {
		collector := newMockCollector(string(rune('a'+i)), 100)
		if err := collector.Start(ctx); err != nil {
			t.Fatalf("failed to start collector %d: %v", i, err)
		}
		collectors[i] = collector
	}

	// Collect events from all collectors
	eventCounts := make(map[string]int)
	var wg sync.WaitGroup

	for _, collector := range collectors {
		wg.Add(1)
		go func(c Collector) {
			defer wg.Done()
			timeout := time.After(100 * time.Millisecond)
			for {
				select {
				case event := <-c.Events():
					eventCounts[event.Type]++
				case <-timeout:
					return
				}
			}
		}(collector)
	}

	wg.Wait()

	// Verify all collectors generated events
	for i, collector := range collectors {
		name := collector.Name()
		if count, ok := eventCounts[name]; !ok || count == 0 {
			t.Errorf("collector %d (%s) did not generate events", i, name)
		}
	}

	// Stop all collectors
	for _, collector := range collectors {
		if err := collector.Stop(); err != nil {
			t.Errorf("failed to stop collector %s: %v", collector.Name(), err)
		}
	}
}

func TestCollectorStartStop(t *testing.T) {
	ctx := context.Background()
	collector := newMockCollector("start-stop-test", 10)

	// Multiple starts should be idempotent
	for i := 0; i < 3; i++ {
		if err := collector.Start(ctx); err != nil {
			t.Fatalf("start %d failed: %v", i, err)
		}
	}

	// Multiple stops should be idempotent
	for i := 0; i < 3; i++ {
		if err := collector.Stop(); err != nil {
			t.Fatalf("stop %d failed: %v", i, err)
		}
	}
}

func TestCollectorContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	collector := newMockCollector("ctx-test", 10)

	if err := collector.Start(ctx); err != nil {
		t.Fatalf("failed to start collector: %v", err)
	}

	// Cancel context
	cancel()

	// Wait for channel to close
	timeout := time.After(100 * time.Millisecond)
	select {
	case _, ok := <-collector.Events():
		if ok {
			t.Error("expected events channel to be closed after context cancellation")
		}
	case <-timeout:
		t.Error("events channel not closed after context cancellation")
	}
}
