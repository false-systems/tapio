package collectors

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

// mockCollector implements CollectorInterface for testing
type mockCollector struct {
	name          string
	collectorType string
	events        chan domain.UnifiedEvent
	health        CollectorHealth
	stats         CollectorStatistics
	startCalled   bool
	stopCalled    bool
	failStart     bool
	failStop      bool
}

func newMockCollector(name, collectorType string) *mockCollector {
	return &mockCollector{
		name:          name,
		collectorType: collectorType,
		events:        make(chan domain.UnifiedEvent, 100),
		health: CollectorHealth{
			Status:  HealthStatusHealthy,
			Message: "Mock collector is healthy",
		},
		stats: CollectorStatistics{
			StartTime: time.Now(),
		},
	}
}

func (m *mockCollector) Start(ctx context.Context) error {
	if m.failStart {
		return assert.AnError
	}
	m.startCalled = true
	return nil
}

func (m *mockCollector) Stop() error {
	if m.failStop {
		return assert.AnError
	}
	m.stopCalled = true
	close(m.events)
	return nil
}

func (m *mockCollector) Events() <-chan domain.UnifiedEvent {
	return m.events
}

func (m *mockCollector) Health() CollectorHealth {
	return m.health
}

func (m *mockCollector) Statistics() CollectorStatistics {
	return m.stats
}

func (m *mockCollector) Name() string {
	return m.name
}

func (m *mockCollector) Type() string {
	return m.collectorType
}

func (m *mockCollector) sendEvent(event domain.UnifiedEvent) {
	select {
	case m.events <- event:
	default:
		// Drop if full
	}
}

func TestManager_Lifecycle(t *testing.T) {
	t.Run("successful start and stop", func(t *testing.T) {
		config := DefaultManagerConfig()
		config.EventBufferSize = 10
		manager := NewManager(config)

		// Register collectors
		collector1 := newMockCollector("mock1", "test")
		collector2 := newMockCollector("mock2", "test")

		err := manager.Register("mock1", collector1)
		require.NoError(t, err)
		err = manager.Register("mock2", collector2)
		require.NoError(t, err)

		// Start manager
		ctx := context.Background()
		err = manager.Start(ctx)
		require.NoError(t, err)
		assert.True(t, manager.IsRunning())
		assert.True(t, collector1.startCalled)
		assert.True(t, collector2.startCalled)

		// Stop manager
		err = manager.Stop()
		require.NoError(t, err)
		assert.False(t, manager.IsRunning())
		assert.True(t, collector1.stopCalled)
		assert.True(t, collector2.stopCalled)
	})

	t.Run("cannot register while running", func(t *testing.T) {
		manager := NewManager(DefaultManagerConfig())
		collector := newMockCollector("mock", "test")

		err := manager.Register("mock", collector)
		require.NoError(t, err)

		ctx := context.Background()
		err = manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		// Try to register another collector
		collector2 := newMockCollector("mock2", "test")
		err = manager.Register("mock2", collector2)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot register collector while running")
	})

	t.Run("cannot start without collectors", func(t *testing.T) {
		manager := NewManager(DefaultManagerConfig())

		ctx := context.Background()
		err := manager.Start(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no collectors registered")
	})

	t.Run("cannot start twice", func(t *testing.T) {
		manager := NewManager(DefaultManagerConfig())
		collector := newMockCollector("mock", "test")

		err := manager.Register("mock", collector)
		require.NoError(t, err)

		ctx := context.Background()
		err = manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		// Try to start again
		err = manager.Start(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "manager already running")
	})

	t.Run("duplicate collector registration", func(t *testing.T) {
		manager := NewManager(DefaultManagerConfig())
		collector1 := newMockCollector("mock", "test")
		collector2 := newMockCollector("mock", "test")

		err := manager.Register("mock", collector1)
		require.NoError(t, err)

		err = manager.Register("mock", collector2)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already registered")
	})
}

func TestManager_EventForwarding(t *testing.T) {
	config := DefaultManagerConfig()
	config.EventBufferSize = 10
	manager := NewManager(config)

	// Register collectors
	collector1 := newMockCollector("mock1", "test")
	collector2 := newMockCollector("mock2", "test")

	err := manager.Register("mock1", collector1)
	require.NoError(t, err)
	err = manager.Register("mock2", collector2)
	require.NoError(t, err)

	// Start manager
	ctx := context.Background()
	err = manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()

	// Send events from collectors
	event1 := domain.UnifiedEvent{
		ID:      "event1",
		Type:    domain.EventTypeLog,
		Message: "Test event 1",
	}
	event2 := domain.UnifiedEvent{
		ID:      "event2",
		Type:    domain.EventTypeMetric,
		Message: "Test event 2",
	}

	collector1.sendEvent(event1)
	collector2.sendEvent(event2)

	// Receive events from manager
	managerEvents := manager.Events()

	received := make([]domain.UnifiedEvent, 0, 2)
	timeout := time.After(1 * time.Second)

	for i := 0; i < 2; i++ {
		select {
		case event := <-managerEvents:
			received = append(received, event)
		case <-timeout:
			t.Fatal("Timeout waiting for events")
		}
	}

	assert.Len(t, received, 2)

	// Check that events have source set
	sources := map[string]bool{}
	for _, event := range received {
		assert.NotEmpty(t, event.Source)
		sources[event.Source] = true
	}
	assert.Len(t, sources, 2) // Should have two different sources
}

func TestManager_Health(t *testing.T) {
	manager := NewManager(DefaultManagerConfig())

	// Register collectors with different health states
	healthyCollector := newMockCollector("healthy", "test")
	healthyCollector.health.Status = HealthStatusHealthy

	unhealthyCollector := newMockCollector("unhealthy", "test")
	unhealthyCollector.health.Status = HealthStatusUnhealthy
	unhealthyCollector.health.Message = "Something is wrong"

	err := manager.Register("healthy", healthyCollector)
	require.NoError(t, err)
	err = manager.Register("unhealthy", unhealthyCollector)
	require.NoError(t, err)

	// Start manager
	ctx := context.Background()
	err = manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()

	// Check health
	health := manager.Health()
	assert.Len(t, health, 3) // 2 collectors + manager

	// Check individual collector health
	assert.Equal(t, HealthStatusHealthy, health["healthy"].Status)
	assert.Equal(t, HealthStatusUnhealthy, health["unhealthy"].Status)
	assert.Equal(t, "Something is wrong", health["unhealthy"].Message)

	// Check manager health (should be degraded due to unhealthy collector)
	assert.Equal(t, HealthStatusDegraded, health["manager"].Status)
}

func TestManager_Statistics(t *testing.T) {
	manager := NewManager(DefaultManagerConfig())

	collector := newMockCollector("mock", "test")
	collector.stats.EventsCollected = 100
	collector.stats.EventsDropped = 5

	err := manager.Register("mock", collector)
	require.NoError(t, err)

	// Start manager
	ctx := context.Background()
	err = manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()

	// Check statistics
	stats := manager.Statistics()
	assert.Len(t, stats, 2) // collector + manager

	// Check collector stats
	mockStats := stats["mock"]
	assert.Equal(t, uint64(100), mockStats.EventsCollected)
	assert.Equal(t, uint64(5), mockStats.EventsDropped)

	// Check manager stats
	managerStats := stats["manager"]
	assert.NotNil(t, managerStats.Custom["collectors_count"])
	assert.Equal(t, 1, managerStats.Custom["collectors_count"])
}

func TestManager_GetCollector(t *testing.T) {
	manager := NewManager(DefaultManagerConfig())

	collector := newMockCollector("mock", "test")
	err := manager.Register("mock", collector)
	require.NoError(t, err)

	// Get existing collector
	retrieved, exists := manager.GetCollector("mock")
	assert.True(t, exists)
	assert.Equal(t, collector, retrieved)

	// Get non-existing collector
	_, exists = manager.GetCollector("nonexistent")
	assert.False(t, exists)
}

func TestManager_StartFailure(t *testing.T) {
	manager := NewManager(DefaultManagerConfig())

	// Register collectors where one fails to start
	goodCollector := newMockCollector("good", "test")
	badCollector := newMockCollector("bad", "test")
	badCollector.failStart = true

	err := manager.Register("good", goodCollector)
	require.NoError(t, err)
	err = manager.Register("bad", badCollector)
	require.NoError(t, err)

	// Start should fail
	ctx := context.Background()
	err = manager.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start collector")
	assert.False(t, manager.IsRunning())

	// Good collector should have been stopped
	assert.True(t, goodCollector.stopCalled)
}
