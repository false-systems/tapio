package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// MockObserver implements a test observer with race-safe operations
type MockObserver struct {
	name     string
	events   chan *domain.CollectorEvent
	ctx      context.Context
	cancel   context.CancelFunc
	stopOnce sync.Once
	mu       sync.RWMutex
	stopped  bool
}

func NewMockObserver(name string) *MockObserver {
	return &MockObserver{
		name:   name,
		events: make(chan *domain.CollectorEvent, 100), // Larger buffer for stress testing
	}
}

func (m *MockObserver) Name() string { return m.name }

func (m *MockObserver) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ctx, m.cancel = context.WithCancel(ctx)
	m.stopped = false
	return nil
}

func (m *MockObserver) Stop() error {
	m.stopOnce.Do(func() {
		m.mu.Lock()
		m.stopped = true
		if m.cancel != nil {
			m.cancel()
		}
		m.mu.Unlock()

		// Give a moment for any in-flight sends to complete
		time.Sleep(1 * time.Millisecond)

		// Close events channel to signal no more events
		close(m.events)
	})
	return nil
}

func (m *MockObserver) Events() <-chan *domain.CollectorEvent {
	return m.events
}

func (m *MockObserver) IsHealthy() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return !m.stopped
}

func (m *MockObserver) SendEvent(event *domain.CollectorEvent) {
	m.mu.RLock()
	if m.stopped {
		m.mu.RUnlock()
		return
	}

	// Try to send with timeout to prevent blocking
	select {
	case m.events <- event:
		m.mu.RUnlock()
	case <-m.ctx.Done():
		m.mu.RUnlock()
	case <-time.After(10 * time.Millisecond):
		// Drop event if we can't send quickly
		m.mu.RUnlock()
	}
}

func TestEventPipeline(t *testing.T) {
	logger := zap.NewNop()

	t.Run("RegisterObserver", func(t *testing.T) {
		config := DefaultConfig()
		config.NATSConfig = nil // Disable NATS for unit test

		pipeline, err := New(logger, config)
		require.NoError(t, err)

		observer := NewMockObserver("test")
		err = pipeline.RegisterObserver("test", observer)
		assert.NoError(t, err)

		// Cannot register same name twice
		err = pipeline.RegisterObserver("test", observer)
		assert.Error(t, err)
	})

	t.Run("StartStop", func(t *testing.T) {
		config := DefaultConfig()
		config.NATSConfig = nil // Disable NATS for unit test
		config.Workers = 1      // Use single worker for predictable testing

		pipeline, err := New(logger, config)
		require.NoError(t, err)

		observer := NewMockObserver("test")
		err = pipeline.RegisterObserver("test", observer)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = pipeline.Start(ctx)
		assert.NoError(t, err)

		// Cannot start twice
		err = pipeline.Start(ctx)
		assert.Error(t, err)

		// Send test event
		testEvent := domain.RawEvent{
			Timestamp: time.Now(),
			Source:    "test",
			Data:      []byte("test data"),
		}
		observer.SendEvent(testEvent)

		// Give time to process event
		time.Sleep(50 * time.Millisecond)

		// Graceful shutdown
		err = pipeline.Stop()
		assert.NoError(t, err)

		// Should be able to call Stop() multiple times safely
		err = pipeline.Stop()
		assert.NoError(t, err)
	})

	t.Run("RaceConditionStressTest", func(t *testing.T) {
		config := DefaultConfig()
		config.NATSConfig = nil // Disable NATS for unit test
		config.Workers = 4      // Multiple workers for race testing
		config.BufferSize = 100 // Smaller buffer to trigger backpressure

		pipeline, err := New(logger, config)
		require.NoError(t, err)

		// Create multiple observers
		testObservers := make([]*MockObserver, 3)
		for i := 0; i < 3; i++ {
			testObservers[i] = NewMockObserver(fmt.Sprintf("test-%d", i))
			err = pipeline.RegisterObserver(fmt.Sprintf("test-%d", i), testObservers[i])
			require.NoError(t, err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err = pipeline.Start(ctx)
		require.NoError(t, err)

		// Start sending events from multiple goroutines
		eventCount := 200
		var wg sync.WaitGroup

		for i, observer := range testObservers {
			wg.Add(1)
			go func(observerIdx int, c *MockObserver) {
				defer wg.Done()
				for j := 0; j < eventCount; j++ {
					event := domain.RawEvent{
						Timestamp: time.Now(),
						Source:    fmt.Sprintf("test-%d", observerIdx),
						Data:      []byte(fmt.Sprintf("test data %d-%d", observerIdx, j)),
					}
					c.SendEvent(event)

					// Add some randomness to trigger race conditions
					if j%50 == 0 {
						time.Sleep(time.Millisecond)
					}
				}
			}(i, observer)
		}

		// Let events flow for a bit
		time.Sleep(100 * time.Millisecond)

		// Start shutdown while events are still being sent
		shutdownDone := make(chan bool)
		go func() {
			err := pipeline.Stop()
			assert.NoError(t, err)
			shutdownDone <- true
		}()

		// Wait for all event senders to finish
		wg.Wait()

		// Wait for shutdown to complete
		select {
		case <-shutdownDone:
			// Success
		case <-time.After(15 * time.Second):
			t.Fatal("Pipeline shutdown timed out")
		}

		// Verify pipeline can be stopped multiple times
		err = pipeline.Stop()
		assert.NoError(t, err)
	})

	t.Run("ChannelClosureRaceTest", func(t *testing.T) {
		config := DefaultConfig()
		config.NATSConfig = nil // Disable NATS for unit test
		config.Workers = 2
		config.BufferSize = 10 // Very small buffer

		pipeline, err := New(logger, config)
		require.NoError(t, err)

		observer := NewMockObserver("test")
		err = pipeline.RegisterObserver("test", observer)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = pipeline.Start(ctx)
		require.NoError(t, err)

		// Send events in rapid succession
		go func() {
			for i := 0; i < 50; i++ {
				event := domain.RawEvent{
					Timestamp: time.Now(),
					Source:    "test",
					Data:      []byte(fmt.Sprintf("rapid event %d", i)),
				}
				observer.SendEvent(event)
			}
		}()

		// Stop almost immediately to test channel closure races
		time.Sleep(5 * time.Millisecond)
		err = pipeline.Stop()
		assert.NoError(t, err)
	})
}

func TestEnrichedEvent(t *testing.T) {
	raw := &domain.RawEvent{
		Timestamp: time.Now(),
		Source:    "kubeapi",
		Data:      []byte(`{"kind":"Pod","name":"test-pod"}`),
	}

	enriched := &EnrichedEvent{
		Raw: raw,
		K8sObject: &K8sObjectInfo{
			Kind:      "Pod",
			Name:      "test-pod",
			Namespace: "default",
			UID:       "test-uid",
			Labels:    map[string]string{"app": "test"},
		},
	}

	// Test that enriched event preserves raw event data
	assert.Equal(t, raw.Timestamp, enriched.Raw.Timestamp)
	assert.Equal(t, "kubeapi", enriched.Raw.Source)
	assert.NotNil(t, enriched.K8sObject)
	assert.Equal(t, "Pod", enriched.K8sObject.Kind)
	assert.Equal(t, "test-pod", enriched.K8sObject.Name)
}
