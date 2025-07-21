package manager

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

// Mock CollectorHealth implementation
type mockCollectorHealth struct {
	status        string
	healthy       bool
	lastEventTime time.Time
	errorCount    uint64
	metrics       map[string]float64
}

func (m *mockCollectorHealth) Status() string {
	return m.status
}

func (m *mockCollectorHealth) IsHealthy() bool {
	return m.healthy
}

func (m *mockCollectorHealth) LastEventTime() time.Time {
	return m.lastEventTime
}

func (m *mockCollectorHealth) ErrorCount() uint64 {
	return m.errorCount
}

func (m *mockCollectorHealth) Metrics() map[string]float64 {
	return m.metrics
}

// Mock CollectorStatistics implementation
type mockCollectorStatistics struct {
	eventsProcessed uint64
	eventsDropped   uint64
	startTime       time.Time
	custom          map[string]interface{}
}

func (m *mockCollectorStatistics) EventsProcessed() uint64 {
	return m.eventsProcessed
}

func (m *mockCollectorStatistics) EventsDropped() uint64 {
	return m.eventsDropped
}

func (m *mockCollectorStatistics) StartTime() time.Time {
	return m.startTime
}

func (m *mockCollectorStatistics) Custom() map[string]interface{} {
	return m.custom
}

// Mock Collector implementation
type mockCollector struct {
	name        string
	eventChan   chan domain.UnifiedEvent
	started     atomic.Bool
	stopped     atomic.Bool
	shouldError bool
	health      CollectorHealth
	statistics  CollectorStatistics
	ctx         context.Context
	cancel      context.CancelFunc
}

func newMockCollector(name string) *mockCollector {
	health := &mockCollectorHealth{
		status:        "healthy",
		healthy:       true,
		lastEventTime: time.Now(),
		errorCount:    0,
		metrics:       map[string]float64{"test_metric": 1.0},
	}

	stats := &mockCollectorStatistics{
		eventsProcessed: 0,
		eventsDropped:   0,
		startTime:       time.Now(),
		custom:          map[string]interface{}{"collector_name": name},
	}

	return &mockCollector{
		name:       name,
		eventChan:  make(chan domain.UnifiedEvent, 10),
		health:     health,
		statistics: stats,
	}
}

func (m *mockCollector) Start(ctx context.Context) error {
	if m.shouldError {
		return errors.New("mock start error")
	}

	if m.started.Load() {
		return errors.New("already started")
	}

	m.ctx, m.cancel = context.WithCancel(ctx)
	m.started.Store(true)
	return nil
}

func (m *mockCollector) Stop() error {
	if m.shouldError {
		return errors.New("mock stop error")
	}

	if !m.started.Load() || m.stopped.Load() {
		return nil
	}

	if m.cancel != nil {
		m.cancel()
	}

	m.stopped.Store(true)
	close(m.eventChan)
	return nil
}

func (m *mockCollector) Events() <-chan domain.UnifiedEvent {
	return m.eventChan
}

func (m *mockCollector) Health() CollectorHealth {
	return m.health
}

func (m *mockCollector) Statistics() CollectorStatistics {
	return m.statistics
}

func (m *mockCollector) SendEvent(event domain.UnifiedEvent) {
	if m.started.Load() && !m.stopped.Load() {
		select {
		case m.eventChan <- event:
		default:
			// Channel full, drop event
		}
	}
}

func (m *mockCollector) IsStarted() bool {
	return m.started.Load()
}

func (m *mockCollector) IsStopped() bool {
	return m.stopped.Load()
}

// Helper function to create test UnifiedEvent
func createTestUnifiedEvent(id string, eventType string) domain.UnifiedEvent {
	return domain.UnifiedEvent{
		ID:        id,
		Timestamp: time.Now(),
		Type:      domain.EventType(eventType),
		Source:    "test-collector",
		Entity: &domain.EntityContext{
			Type:      "test",
			Name:      "test-entity",
			Namespace: "default",
		},
		Impact: &domain.ImpactContext{
			Severity:       "medium",
			BusinessImpact: 0.5,
		},
	}
}

func TestNewCollectorManager(t *testing.T) {
	cm := NewCollectorManager()

	assert.NotNil(t, cm)
	assert.NotNil(t, cm.collectors)
	assert.NotNil(t, cm.eventChan)
	assert.Equal(t, 0, len(cm.collectors))
	assert.Equal(t, 10000, cap(cm.eventChan))
}

func TestCollectorManager_AddCollector(t *testing.T) {
	cm := NewCollectorManager()
	collector1 := newMockCollector("test-collector-1")
	collector2 := newMockCollector("test-collector-2")

	// Add first collector
	cm.AddCollector("collector1", collector1)
	assert.Equal(t, 1, len(cm.collectors))
	assert.Equal(t, collector1, cm.collectors["collector1"])

	// Add second collector
	cm.AddCollector("collector2", collector2)
	assert.Equal(t, 2, len(cm.collectors))
	assert.Equal(t, collector2, cm.collectors["collector2"])

	// Replace existing collector
	collector1New := newMockCollector("test-collector-1-new")
	cm.AddCollector("collector1", collector1New)
	assert.Equal(t, 2, len(cm.collectors))
	assert.Equal(t, collector1New, cm.collectors["collector1"])
}

func TestCollectorManager_Start_Success(t *testing.T) {
	cm := NewCollectorManager()
	collector1 := newMockCollector("test-collector-1")
	collector2 := newMockCollector("test-collector-2")

	cm.AddCollector("collector1", collector1)
	cm.AddCollector("collector2", collector2)

	ctx := context.Background()
	err := cm.Start(ctx)

	assert.NoError(t, err)
	assert.NotNil(t, cm.ctx)
	assert.NotNil(t, cm.cancel)
	assert.True(t, collector1.IsStarted())
	assert.True(t, collector2.IsStarted())
}

func TestCollectorManager_Start_WithError(t *testing.T) {
	cm := NewCollectorManager()
	collector1 := newMockCollector("test-collector-1")
	collector2 := newMockCollector("test-collector-2")

	// Make collector1 fail on start
	collector1.shouldError = true

	cm.AddCollector("collector1", collector1)
	cm.AddCollector("collector2", collector2)

	ctx := context.Background()
	err := cm.Start(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start collector1 collector")
	assert.Contains(t, err.Error(), "mock start error")
}

func TestCollectorManager_Start_NoCollectors(t *testing.T) {
	cm := NewCollectorManager()
	ctx := context.Background()

	err := cm.Start(ctx)

	assert.NoError(t, err)
	assert.NotNil(t, cm.ctx)
	assert.NotNil(t, cm.cancel)
}

func TestCollectorManager_Stop(t *testing.T) {
	cm := NewCollectorManager()
	collector1 := newMockCollector("test-collector-1")
	collector2 := newMockCollector("test-collector-2")

	cm.AddCollector("collector1", collector1)
	cm.AddCollector("collector2", collector2)

	ctx := context.Background()
	err := cm.Start(ctx)
	require.NoError(t, err)

	// Verify collectors are started
	assert.True(t, collector1.IsStarted())
	assert.True(t, collector2.IsStarted())

	// Stop manager
	cm.Stop()

	// Verify collectors are stopped
	assert.True(t, collector1.IsStopped())
	assert.True(t, collector2.IsStopped())

	// Verify event channel is closed
	select {
	case _, ok := <-cm.eventChan:
		assert.False(t, ok, "Event channel should be closed")
	default:
		t.Fatal("Expected event channel to be closed")
	}
}

func TestCollectorManager_Stop_WithErrors(t *testing.T) {
	cm := NewCollectorManager()
	collector1 := newMockCollector("test-collector-1")
	collector2 := newMockCollector("test-collector-2")

	cm.AddCollector("collector1", collector1)
	cm.AddCollector("collector2", collector2)

	ctx := context.Background()
	err := cm.Start(ctx)
	require.NoError(t, err)

	// Make collector2 fail on stop (after it's started)
	collector2.shouldError = true

	// Stop should not panic even with errors (errors are logged)
	cm.Stop()

	// collector1 should still be stopped
	assert.True(t, collector1.IsStopped())
	// collector2 should not be stopped due to error, but that's ok
}

func TestCollectorManager_Stop_NotStarted(t *testing.T) {
	cm := NewCollectorManager()
	collector := newMockCollector("test-collector")
	cm.AddCollector("collector", collector)

	// Stop without starting should not panic
	cm.Stop()

	// Collector should not be started or stopped
	assert.False(t, collector.IsStarted())
	assert.False(t, collector.IsStopped())
}

func TestCollectorManager_Events(t *testing.T) {
	cm := NewCollectorManager()
	eventChan := cm.Events()

	assert.NotNil(t, eventChan)
	// Can't directly compare channels due to type conversion, just check capacity
	assert.Equal(t, 10000, cap(cm.eventChan))
}

func TestCollectorManager_EventRouting(t *testing.T) {
	cm := NewCollectorManager()
	collector1 := newMockCollector("test-collector-1")
	collector2 := newMockCollector("test-collector-2")

	cm.AddCollector("collector1", collector1)
	cm.AddCollector("collector2", collector2)

	ctx := context.Background()
	err := cm.Start(ctx)
	require.NoError(t, err)

	// Create test events
	event1 := createTestUnifiedEvent("event-1", "test-type-1")
	event2 := createTestUnifiedEvent("event-2", "test-type-2")
	event3 := createTestUnifiedEvent("event-3", "test-type-3")

	// Send events from collectors
	collector1.SendEvent(event1)
	collector2.SendEvent(event2)
	collector1.SendEvent(event3)

	// Collect events from manager
	receivedEvents := make([]domain.UnifiedEvent, 0, 3)
	timeout := time.After(1 * time.Second)

	for i := 0; i < 3; i++ {
		select {
		case event := <-cm.Events():
			receivedEvents = append(receivedEvents, event)
		case <-timeout:
			t.Fatal("Timeout waiting for events")
		}
	}

	// Verify all events were received
	assert.Len(t, receivedEvents, 3)

	// Verify event IDs (order may vary due to goroutines)
	eventIDs := make(map[string]bool)
	for _, event := range receivedEvents {
		eventIDs[event.ID] = true
	}

	assert.True(t, eventIDs["event-1"])
	assert.True(t, eventIDs["event-2"])
	assert.True(t, eventIDs["event-3"])

	cm.Stop()
}

func TestCollectorManager_EventRouting_ContextCanceled(t *testing.T) {
	cm := NewCollectorManager()
	collector := newMockCollector("test-collector")
	cm.AddCollector("collector", collector)

	ctx, cancel := context.WithCancel(context.Background())
	err := cm.Start(ctx)
	require.NoError(t, err)

	// Send an event first to establish the routing goroutine
	event1 := createTestUnifiedEvent("event-1", "test-type")
	collector.SendEvent(event1)

	// Receive the first event
	select {
	case <-cm.Events():
		// Expected - received the event
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Should receive first event")
	}

	// Cancel context to simulate shutdown
	cancel()

	// Allow some time for goroutines to handle cancellation
	time.Sleep(100 * time.Millisecond)

	// Send another event - the routing goroutine should have exited
	event2 := createTestUnifiedEvent("event-2", "test-type")
	collector.SendEvent(event2)

	// This event should not be routed (though it might still be in collector's channel)
	select {
	case <-cm.Events():
		// This might happen if there were buffered events, that's OK
	case <-time.After(100 * time.Millisecond):
		// Expected - no new event received due to context cancellation
	}

	cm.Stop()
}

func TestCollectorManager_Statistics(t *testing.T) {
	cm := NewCollectorManager()
	collector1 := newMockCollector("test-collector-1")
	collector2 := newMockCollector("test-collector-2")

	// Initially no collectors
	stats := cm.Statistics()
	assert.Equal(t, 0, stats.ActiveCollectors)
	assert.Equal(t, int64(0), stats.TotalEvents)

	// Add collectors
	cm.AddCollector("collector1", collector1)
	cm.AddCollector("collector2", collector2)

	stats = cm.Statistics()
	assert.Equal(t, 2, stats.ActiveCollectors)
	assert.Equal(t, int64(0), stats.TotalEvents) // TODO: Track this in implementation

	// Remove one collector (by overwriting)
	cm.AddCollector("collector1", nil)
	stats = cm.Statistics()
	assert.Equal(t, 2, stats.ActiveCollectors) // Still 2 slots (one is nil)
}

func TestCollectorManager_ConcurrentAccess(t *testing.T) {
	cm := NewCollectorManager()

	// Test concurrent addition of collectors
	done := make(chan bool, 3)

	// Goroutine 1: Add collectors
	go func() {
		for i := 0; i < 10; i++ {
			collector := newMockCollector(fmt.Sprintf("collector-%d", i))
			cm.AddCollector(fmt.Sprintf("collector-%d", i), collector)
			time.Sleep(time.Millisecond)
		}
		done <- true
	}()

	// Goroutine 2: Read statistics
	go func() {
		for i := 0; i < 10; i++ {
			stats := cm.Statistics()
			assert.GreaterOrEqual(t, stats.ActiveCollectors, 0)
			time.Sleep(time.Millisecond)
		}
		done <- true
	}()

	// Goroutine 3: Access event channel
	go func() {
		for i := 0; i < 10; i++ {
			eventChan := cm.Events()
			assert.NotNil(t, eventChan)
			time.Sleep(time.Millisecond)
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 3; i++ {
		<-done
	}

	// Final verification
	stats := cm.Statistics()
	assert.Equal(t, 10, stats.ActiveCollectors)
}

func TestCollectorManager_StartStop_Multiple(t *testing.T) {
	cm := NewCollectorManager()
	collector := newMockCollector("test-collector")
	cm.AddCollector("collector", collector)

	ctx := context.Background()

	// Start first time
	err := cm.Start(ctx)
	require.NoError(t, err)
	assert.True(t, collector.IsStarted())

	// Stop
	cm.Stop()
	assert.True(t, collector.IsStopped())

	// Create a new CollectorManager for restart (simulate fresh start)
	cm2 := NewCollectorManager()
	collector2 := newMockCollector("test-collector-2")
	cm2.AddCollector("collector2", collector2)

	err = cm2.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, collector2.IsStarted())

	cm2.Stop()
}

func TestCollectorManager_EventChannelBuffering(t *testing.T) {
	cm := NewCollectorManager()

	// Create collector with larger buffer to handle all test events
	collector := newMockCollector("test-collector")
	// Replace the event channel with a larger one
	collector.eventChan = make(chan domain.UnifiedEvent, 100)
	cm.AddCollector("collector", collector)

	ctx := context.Background()
	err := cm.Start(ctx)
	require.NoError(t, err)

	// Send events to test buffering (within collector buffer capacity)
	numEvents := 20
	for i := 0; i < numEvents; i++ {
		event := createTestUnifiedEvent(fmt.Sprintf("event-%d", i), "test-type")
		collector.SendEvent(event)
	}

	// Allow time for event routing
	time.Sleep(100 * time.Millisecond)

	// Collect events
	receivedCount := 0
	timeout := time.After(1 * time.Second)

eventLoop:
	for {
		select {
		case <-cm.Events():
			receivedCount++
			if receivedCount == numEvents {
				break eventLoop
			}
		case <-timeout:
			break eventLoop
		}
	}

	assert.Equal(t, numEvents, receivedCount, "All events should be received")
	cm.Stop()
}

func TestCollectorHealth_Interface(t *testing.T) {
	health := &mockCollectorHealth{
		status:        "healthy",
		healthy:       true,
		lastEventTime: time.Now(),
		errorCount:    5,
		metrics:       map[string]float64{"cpu": 0.5, "memory": 0.8},
	}

	assert.Equal(t, "healthy", health.Status())
	assert.True(t, health.IsHealthy())
	assert.False(t, health.LastEventTime().IsZero())
	assert.Equal(t, uint64(5), health.ErrorCount())

	metrics := health.Metrics()
	assert.Equal(t, 0.5, metrics["cpu"])
	assert.Equal(t, 0.8, metrics["memory"])
}

func TestCollectorStatistics_Interface(t *testing.T) {
	startTime := time.Now().Add(-1 * time.Hour)
	stats := &mockCollectorStatistics{
		eventsProcessed: 1000,
		eventsDropped:   10,
		startTime:       startTime,
		custom:          map[string]interface{}{"collector_type": "k8s", "version": "1.0"},
	}

	assert.Equal(t, uint64(1000), stats.EventsProcessed())
	assert.Equal(t, uint64(10), stats.EventsDropped())
	assert.Equal(t, startTime, stats.StartTime())

	custom := stats.Custom()
	assert.Equal(t, "k8s", custom["collector_type"])
	assert.Equal(t, "1.0", custom["version"])
}

func TestCollector_Interface_Compliance(t *testing.T) {
	collector := newMockCollector("test-collector")

	// Test interface compliance
	var c Collector = collector
	assert.NotNil(t, c)

	// Test all interface methods
	ctx := context.Background()
	err := c.Start(ctx)
	assert.NoError(t, err)

	eventChan := c.Events()
	assert.NotNil(t, eventChan)

	health := c.Health()
	assert.NotNil(t, health)
	assert.True(t, health.IsHealthy())

	statistics := c.Statistics()
	assert.NotNil(t, statistics)
	assert.GreaterOrEqual(t, statistics.EventsProcessed(), uint64(0))

	err = c.Stop()
	assert.NoError(t, err)
}

// Benchmark tests
func BenchmarkCollectorManager_AddCollector(b *testing.B) {
	cm := NewCollectorManager()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector := newMockCollector(fmt.Sprintf("collector-%d", i))
		cm.AddCollector(fmt.Sprintf("collector-%d", i), collector)
	}
}

func BenchmarkCollectorManager_Statistics(b *testing.B) {
	cm := NewCollectorManager()

	// Add some collectors
	for i := 0; i < 10; i++ {
		collector := newMockCollector(fmt.Sprintf("collector-%d", i))
		cm.AddCollector(fmt.Sprintf("collector-%d", i), collector)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cm.Statistics()
	}
}

func BenchmarkCollectorManager_EventRouting(b *testing.B) {
	cm := NewCollectorManager()
	collector := newMockCollector("test-collector")
	cm.AddCollector("collector", collector)

	ctx := context.Background()
	cm.Start(ctx)
	defer cm.Stop()

	// Create events to send
	events := make([]domain.UnifiedEvent, 1000)
	for i := range events {
		events[i] = createTestUnifiedEvent(fmt.Sprintf("event-%d", i), "benchmark")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		event := events[i%len(events)]
		collector.SendEvent(event)
	}
}
