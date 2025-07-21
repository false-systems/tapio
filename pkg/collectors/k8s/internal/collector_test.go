package internal

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors/k8s/core"
	"github.com/yairfalse/tapio/pkg/domain"
	"k8s.io/client-go/kubernetes/fake"
)

// Mock EventProcessor for testing
type mockEventProcessor struct {
	processed atomic.Uint64
	shouldErr bool
}

func (m *mockEventProcessor) ProcessEvent(ctx context.Context, raw core.RawEvent) (*domain.UnifiedEvent, error) {
	m.processed.Add(1)
	if m.shouldErr {
		return nil, assert.AnError
	}

	return &domain.UnifiedEvent{
		ID:        "test-event-" + raw.Name,
		Timestamp: raw.Timestamp,
		Type:      domain.EventType(raw.ResourceKind + "_" + string(raw.Type)),
		Source:    string(domain.SourceK8s),
		Entity: &domain.EntityContext{
			Type:      raw.ResourceKind,
			Name:      raw.Name,
			Namespace: raw.Namespace,
		},
	}, nil
}

// Mock ResourceWatcher for testing
type mockResourceWatcher struct {
	resourceType string
	eventChan    chan core.RawEvent
	started      atomic.Bool
	stopped      atomic.Bool
}

func newMockResourceWatcher(resourceType string) *mockResourceWatcher {
	return &mockResourceWatcher{
		resourceType: resourceType,
		eventChan:    make(chan core.RawEvent, 10),
	}
}

func (m *mockResourceWatcher) Start(ctx context.Context) error {
	if m.started.Load() {
		return core.ErrAlreadyStarted
	}
	m.started.Store(true)
	return nil
}

func (m *mockResourceWatcher) Stop() error {
	if !m.started.Load() || m.stopped.Load() {
		return nil
	}
	m.stopped.Store(true)
	close(m.eventChan)
	return nil
}

func (m *mockResourceWatcher) Events() <-chan core.RawEvent {
	return m.eventChan
}

func (m *mockResourceWatcher) ResourceType() string {
	return m.resourceType
}

func (m *mockResourceWatcher) SendEvent(event core.RawEvent) {
	if m.started.Load() && !m.stopped.Load() {
		select {
		case m.eventChan <- event:
		default:
		}
	}
}

// Helper function to create valid test config for collector tests
func createCollectorTestConfig() core.Config {
	return core.Config{
		Name:            "test-collector",
		Enabled:         true,
		EventBufferSize: 100,
		InCluster:       false,
		WatchPods:       true,
		WatchEvents:     true,
		ResyncPeriod:    30 * time.Second,
		EventRateLimit:  1000,
	}
}

func TestNewCollector(t *testing.T) {
	tests := []struct {
		name          string
		config        core.Config
		expectedError bool
	}{
		{
			name:          "valid config",
			config:        createCollectorTestConfig(),
			expectedError: false,
		},
		{
			name: "invalid config - no resources watched",
			config: core.Config{
				Name:            "test",
				Enabled:         true,
				EventBufferSize: 100,
			},
			expectedError: false, // Config validation should fix this
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollector(tt.config)

			if tt.expectedError {
				assert.Error(t, err)
				assert.Nil(t, collector)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, collector)

				// Test interface compliance
				assert.Implements(t, (*core.Collector)(nil), collector)
			}
		})
	}
}

func TestCollector_Configure(t *testing.T) {
	config := createCollectorTestConfig()
	collector, err := NewCollector(config)
	require.NoError(t, err)

	// Test valid configuration
	newConfig := createCollectorTestConfig()
	newConfig.Name = "updated-collector"
	newConfig.EventBufferSize = 200

	err = collector.Configure(newConfig)
	assert.NoError(t, err)

	// Test invalid configuration (should be caught by validation)
	invalidConfig := core.Config{
		EventBufferSize: -1,
	}
	err = collector.Configure(invalidConfig)
	assert.NoError(t, err) // Validation fixes negative buffer size
}

func TestCollector_StartStop_WithoutK8s(t *testing.T) {
	config := createCollectorTestConfig()
	config.InCluster = false
	config.KubeConfig = "/nonexistent/kubeconfig"

	collector, err := NewCollector(config)
	require.NoError(t, err)

	ctx := context.Background()

	// Test starting with invalid kubeconfig should fail
	err = collector.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to initialize Kubernetes client")

	// Test stopping non-started collector
	err = collector.Stop()
	assert.Error(t, err)
	assert.Equal(t, core.ErrNotStarted, err)
}

func TestCollector_StartStop_Disabled(t *testing.T) {
	config := createCollectorTestConfig()
	config.Enabled = false

	collector, err := NewCollector(config)
	require.NoError(t, err)

	ctx := context.Background()

	// Test starting disabled collector
	err = collector.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "collector is disabled")
}

func TestCollector_HealthAndStatistics(t *testing.T) {
	config := createCollectorTestConfig()
	collector, err := NewCollector(config)
	require.NoError(t, err)

	// Test health when not started
	health := collector.Health()
	assert.Equal(t, core.HealthStatusUnknown, health.Status)
	assert.Equal(t, "Collector not started", health.Message)
	assert.False(t, health.Connected)

	// Test statistics
	stats := collector.Statistics()
	assert.False(t, stats.StartTime.IsZero())
	assert.Equal(t, uint64(0), stats.EventsCollected)
	assert.Equal(t, uint64(0), stats.EventsDropped)
	assert.Equal(t, 0, stats.WatchersActive)
	assert.Contains(t, stats.Custom, "uptime_seconds")
	assert.Contains(t, stats.Custom, "events_per_second")
	assert.Contains(t, stats.Custom, "connected")
}

func TestCollector_Events(t *testing.T) {
	config := createCollectorTestConfig()
	collector, err := NewCollector(config)
	require.NoError(t, err)

	eventChan := collector.Events()
	assert.NotNil(t, eventChan)
}

func TestCollector_EventsPerSecond(t *testing.T) {
	config := createCollectorTestConfig()
	c := &collector{
		config:    config,
		eventChan: make(chan domain.UnifiedEvent, config.EventBufferSize),
		startTime: time.Now().Add(-10 * time.Second), // Started 10 seconds ago
	}

	// No events processed
	eps := c.getEventsPerSecond()
	assert.Equal(t, float64(0), eps)

	// Simulate some events
	c.stats.eventsCollected.Store(100)
	eps = c.getEventsPerSecond()
	assert.Greater(t, eps, float64(0))
}

func TestCollector_StatisticsResourceMapping(t *testing.T) {
	config := createCollectorTestConfig()
	config.WatchPods = true
	config.WatchNodes = true
	config.WatchServices = false

	collector := &collector{
		config:    config,
		startTime: time.Now(),
	}

	stats := collector.Statistics()

	// Check resource mapping
	assert.Contains(t, stats.ResourcesWatched, "pods")
	assert.Contains(t, stats.ResourcesWatched, "nodes")
	assert.NotContains(t, stats.ResourcesWatched, "services")
	assert.Equal(t, 1, stats.ResourcesWatched["pods"])
	assert.Equal(t, 1, stats.ResourcesWatched["nodes"])
}

func TestCollector_HealthStatusTransitions(t *testing.T) {
	config := createCollectorTestConfig()
	c := &collector{
		config:    config,
		eventChan: make(chan domain.UnifiedEvent, config.EventBufferSize),
		startTime: time.Now(),
	}

	// Initialize atomic values
	c.lastEventTime.Store(time.Now())
	c.connectedAt.Store(time.Time{})
	c.clusterInfo.Store(core.ClusterInfo{})

	// Test not started
	health := c.Health()
	assert.Equal(t, core.HealthStatusUnknown, health.Status)

	// Test started but not connected
	c.started.Store(true)
	health = c.Health()
	assert.Equal(t, core.HealthStatusUnhealthy, health.Status)
	assert.Contains(t, health.Message, "Not connected")

	// Test connected and healthy
	c.connected.Store(true)
	health = c.Health()
	assert.Equal(t, core.HealthStatusHealthy, health.Status)

	// Test high API error count
	c.stats.apiErrors.Store(150)
	health = c.Health()
	assert.Equal(t, core.HealthStatusDegraded, health.Status)
	assert.Contains(t, health.Message, "High API error count")

	// Reset errors, test old last event time
	c.stats.apiErrors.Store(0)
	c.lastEventTime.Store(time.Now().Add(-10 * time.Minute))
	health = c.Health()
	assert.Equal(t, core.HealthStatusDegraded, health.Status)
	assert.Contains(t, health.Message, "No events received")

	// Test stopped collector - but first reset last event time so it doesn't interfere
	c.lastEventTime.Store(time.Now())
	c.stopped.Store(true)
	health = c.Health()
	assert.Equal(t, core.HealthStatusUnhealthy, health.Status)
	assert.Contains(t, health.Message, "Collector stopped")
}

// Test with mock components to simulate full functionality
func TestCollector_WithMocks(t *testing.T) {
	config := createCollectorTestConfig()

	// Create collector
	c := &collector{
		config:    config,
		eventChan: make(chan domain.UnifiedEvent, config.EventBufferSize),
		startTime: time.Now(),
		processor: &mockEventProcessor{},
	}

	// Initialize atomic values
	c.lastEventTime.Store(time.Now())
	c.connectedAt.Store(time.Time{})
	c.clusterInfo.Store(core.ClusterInfo{})

	// Create mock watchers
	podWatcher := newMockResourceWatcher("pods")
	eventWatcher := newMockResourceWatcher("events")
	c.watchers = []core.ResourceWatcher{podWatcher, eventWatcher}

	// Set up fake client
	c.clientset = fake.NewSimpleClientset()

	// Start collector manually (skip K8s initialization)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c.ctx = ctx
	c.cancel = cancel
	c.started.Store(true)
	c.connected.Store(true)

	// Start watchers
	for _, watcher := range c.watchers {
		err := watcher.Start(ctx)
		assert.NoError(t, err)
	}

	// Start event processing
	c.wg.Add(1)
	go c.processEvents()

	// Send test events
	testEvent1 := core.RawEvent{
		Type:         core.EventTypeAdded,
		ResourceKind: "Pod",
		Name:         "test-pod",
		Namespace:    "default",
		Timestamp:    time.Now(),
	}

	testEvent2 := core.RawEvent{
		Type:         core.EventTypeModified,
		ResourceKind: "Event",
		Name:         "test-event",
		Namespace:    "default",
		Timestamp:    time.Now(),
	}

	podWatcher.SendEvent(testEvent1)
	eventWatcher.SendEvent(testEvent2)

	// Allow some processing time
	time.Sleep(100 * time.Millisecond)

	// Check events were processed
	select {
	case event := <-c.Events():
		assert.NotEmpty(t, event.ID)
		assert.Equal(t, string(domain.SourceK8s), event.Source)
	case <-time.After(1 * time.Second):
		t.Fatal("Expected to receive processed event")
	}

	// Check statistics
	stats := c.Statistics()
	assert.Greater(t, stats.EventsCollected, uint64(0))

	// Stop collector
	cancel()
	for _, watcher := range c.watchers {
		watcher.Stop()
	}
	c.wg.Wait()
}

func TestCollector_ProcessEventsWithError(t *testing.T) {
	config := createCollectorTestConfig()

	// Create collector with error-generating processor
	processor := &mockEventProcessor{shouldErr: true}
	c := &collector{
		config:    config,
		eventChan: make(chan domain.UnifiedEvent, config.EventBufferSize),
		startTime: time.Now(),
		processor: processor,
	}

	// Initialize atomic values
	c.lastEventTime.Store(time.Now())
	c.connectedAt.Store(time.Time{})
	c.clusterInfo.Store(core.ClusterInfo{})

	// Create mock watcher
	watcher := newMockResourceWatcher("pods")
	c.watchers = []core.ResourceWatcher{watcher}

	// Start collector manually
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c.ctx = ctx
	c.cancel = cancel
	c.started.Store(true)
	c.connected.Store(true)

	// Start watcher
	err := watcher.Start(ctx)
	assert.NoError(t, err)

	// Start event processing
	c.wg.Add(1)
	go c.processEvents()

	// Send test event
	testEvent := core.RawEvent{
		Type:         core.EventTypeAdded,
		ResourceKind: "Pod",
		Name:         "test-pod",
		Namespace:    "default",
		Timestamp:    time.Now(),
	}

	watcher.SendEvent(testEvent)

	// Allow some processing time
	time.Sleep(100 * time.Millisecond)

	// Check error was recorded
	assert.Greater(t, c.stats.apiErrors.Load(), uint64(0))

	// Stop collector
	cancel()
	watcher.Stop()
	c.wg.Wait()
}

func TestCollector_EventBufferFull(t *testing.T) {
	config := createCollectorTestConfig()
	config.EventBufferSize = 1 // Very small buffer

	c := &collector{
		config:    config,
		eventChan: make(chan domain.UnifiedEvent, config.EventBufferSize),
		startTime: time.Now(),
		processor: &mockEventProcessor{},
	}

	// Initialize atomic values
	c.lastEventTime.Store(time.Now())
	c.connectedAt.Store(time.Time{})
	c.clusterInfo.Store(core.ClusterInfo{})

	// Create mock watcher
	watcher := newMockResourceWatcher("pods")
	c.watchers = []core.ResourceWatcher{watcher}

	// Start collector manually
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c.ctx = ctx
	c.cancel = cancel
	c.started.Store(true)
	c.connected.Store(true)

	// Start watcher
	err := watcher.Start(ctx)
	assert.NoError(t, err)

	// Start event processing
	c.wg.Add(1)
	go c.processEvents()

	// Send multiple events to overflow buffer
	for i := 0; i < 5; i++ {
		testEvent := core.RawEvent{
			Type:         core.EventTypeAdded,
			ResourceKind: "Pod",
			Name:         fmt.Sprintf("test-pod-%d", i),
			Namespace:    "default",
			Timestamp:    time.Now(),
		}
		watcher.SendEvent(testEvent)
	}

	// Allow some processing time
	time.Sleep(100 * time.Millisecond)

	// Check some events were dropped
	stats := c.Statistics()
	assert.Greater(t, stats.EventsDropped, uint64(0))

	// Stop collector
	cancel()
	watcher.Stop()
	c.wg.Wait()
}

// Test configuration validation
func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name     string
		config   core.Config
		expected core.Config
	}{
		{
			name: "zero buffer size",
			config: core.Config{
				EventBufferSize: 0,
			},
			expected: core.Config{
				EventBufferSize: 1000,
				ResyncPeriod:    30 * time.Minute,
				WatchPods:       true,
				WatchEvents:     true,
			},
		},
		{
			name: "zero resync period",
			config: core.Config{
				EventBufferSize: 100,
				ResyncPeriod:    0,
			},
			expected: core.Config{
				EventBufferSize: 100,
				ResyncPeriod:    30 * time.Minute,
				WatchPods:       true,
				WatchEvents:     true,
			},
		},
		{
			name: "no resources watched",
			config: core.Config{
				EventBufferSize: 100,
				ResyncPeriod:    30 * time.Minute,
				WatchPods:       false,
				WatchNodes:      false,
				WatchServices:   false,
			},
			expected: core.Config{
				EventBufferSize: 100,
				ResyncPeriod:    30 * time.Minute,
				WatchPods:       true,
				WatchEvents:     true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			assert.NoError(t, err)
			assert.Equal(t, tt.expected.EventBufferSize, tt.config.EventBufferSize)
			assert.Equal(t, tt.expected.ResyncPeriod, tt.config.ResyncPeriod)
			assert.Equal(t, tt.expected.WatchPods, tt.config.WatchPods)
		})
	}
}

// Benchmark tests
func BenchmarkCollector_ProcessEvent(b *testing.B) {
	processor := &mockEventProcessor{}

	testEvent := core.RawEvent{
		Type:         core.EventTypeAdded,
		ResourceKind: "Pod",
		Name:         "test-pod",
		Namespace:    "default",
		Timestamp:    time.Now(),
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		processor.ProcessEvent(ctx, testEvent)
	}
}

func BenchmarkCollector_Health(b *testing.B) {
	config := createCollectorTestConfig()
	c := &collector{
		config:    config,
		eventChan: make(chan domain.UnifiedEvent, config.EventBufferSize),
		startTime: time.Now(),
	}

	// Initialize atomic values
	c.lastEventTime.Store(time.Now())
	c.connectedAt.Store(time.Time{})
	c.clusterInfo.Store(core.ClusterInfo{})
	c.started.Store(true)
	c.connected.Store(true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Health()
	}
}

func BenchmarkCollector_Statistics(b *testing.B) {
	config := createCollectorTestConfig()
	c := &collector{
		config:    config,
		eventChan: make(chan domain.UnifiedEvent, config.EventBufferSize),
		startTime: time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Statistics()
	}
}
