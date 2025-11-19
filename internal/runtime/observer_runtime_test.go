package runtime

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

// eventIDCounter generates unique IDs for test events to prevent race conditions
var eventIDCounter atomic.Uint64

func generateTestEventID() string {
	return fmt.Sprintf("test-event-%d", eventIDCounter.Add(1))
}

// Mock processor for testing
type mockProcessor struct {
	name           string
	setupCalled    bool
	teardownCalled bool
	processFunc    func(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error)
}

func (m *mockProcessor) Name() string {
	return m.name
}

func (m *mockProcessor) Setup(ctx context.Context, cfg Config) error {
	m.setupCalled = true
	return nil
}

func (m *mockProcessor) Teardown(ctx context.Context) error {
	m.teardownCalled = true
	return nil
}

func (m *mockProcessor) Process(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error) {
	if m.processFunc != nil {
		event, err := m.processFunc(ctx, rawEvent)
		// Ensure all events have unique IDs to prevent race conditions
		if event != nil && event.ID == "" {
			event.ID = generateTestEventID()
		}
		return event, err
	}
	return &domain.ObserverEvent{
		ID:      generateTestEventID(),
		Type:    "test",
		Subtype: "mock",
	}, nil
}

// Mock emitter for testing
type mockEmitter struct {
	mu           sync.Mutex
	name         string
	critical     bool
	emitted      []*domain.ObserverEvent
	closeCalled  bool
	failNext     bool // For testing error handling (fails once)
	alwaysFail   bool // For testing persistent failures
	attemptCount int  // Track number of emit attempts
}

func (m *mockEmitter) Name() string {
	return m.name
}

func (m *mockEmitter) IsCritical() bool {
	return m.critical
}

func (m *mockEmitter) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.attemptCount++
	if m.alwaysFail {
		return fmt.Errorf("mock emit failure")
	}
	if m.failNext {
		m.failNext = false
		return fmt.Errorf("mock emit failure")
	}
	m.emitted = append(m.emitted, event)
	return nil
}

func (m *mockEmitter) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeCalled = true
	return nil
}

// Helper methods for thread-safe access
func (m *mockEmitter) EmittedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.emitted)
}

func (m *mockEmitter) EmittedEvents() []*domain.ObserverEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Return a copy to prevent races
	events := make([]*domain.ObserverEvent, len(m.emitted))
	copy(events, m.emitted)
	return events
}

func (m *mockEmitter) AttemptCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.attemptCount
}

// RED: Test ObserverRuntime creation
func TestNewObserverRuntime(t *testing.T) {
	processor := &mockProcessor{name: "test"}

	runtime, err := NewObserverRuntime(processor)
	require.NoError(t, err)
	require.NotNil(t, runtime)
}

// RED: Test processor is required
func TestNewObserverRuntime_NilProcessor(t *testing.T) {
	runtime, err := NewObserverRuntime(nil)
	assert.Error(t, err)
	assert.Nil(t, runtime)
	assert.Contains(t, err.Error(), "processor is required")
}

// RED: Test Setup is called
func TestObserverRuntime_Setup(t *testing.T) {
	processor := &mockProcessor{name: "test"}
	runtime, err := NewObserverRuntime(processor)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Start runtime (should call Setup)
	errCh := make(chan error, 1)
	go func() {
		errCh <- runtime.Run(ctx)
	}()

	// Give it time to setup
	time.Sleep(100 * time.Millisecond)
	cancel()

	assert.True(t, processor.setupCalled, "Setup should be called")
}

// RED: Test Teardown is called on shutdown
func TestObserverRuntime_Teardown(t *testing.T) {
	processor := &mockProcessor{name: "test"}
	runtime, err := NewObserverRuntime(processor)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Run and wait for completion
	err = runtime.Run(ctx)
	require.NoError(t, err)

	assert.True(t, processor.teardownCalled, "Teardown should be called after shutdown")
}

// RED: Test event processing
func TestObserverRuntime_ProcessEvent(t *testing.T) {
	processor := &mockProcessor{
		name: "test",
		processFunc: func(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error) {
			return &domain.ObserverEvent{
				Type:    "network",
				Subtype: "dns_query",
			}, nil
		},
	}

	emitter := &mockEmitter{name: "test-emitter"}

	runtime, err := NewObserverRuntime(processor, func(r *ObserverRuntime) {
		r.config.Sampling.Enabled = false // Disable sampling for this test
	}, WithEmitters(emitter))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Start runtime
	errCh := make(chan error, 1)
	go func() {
		errCh <- runtime.Run(ctx)
	}()
	time.Sleep(100 * time.Millisecond) // Wait for startup

	// Process event
	rawEvent := []byte("test event")
	err = runtime.ProcessEvent(ctx, rawEvent)
	require.NoError(t, err)

	// Give it time to emit
	time.Sleep(100 * time.Millisecond)

	// Verify event was emitted
	assert.Equal(t, 1, emitter.EmittedCount())
	assert.Equal(t, "network", emitter.EmittedEvents()[0].Type)
	assert.Equal(t, "dns_query", emitter.EmittedEvents()[0].Subtype)
}

// RED: Test processor can return nil (ignore event)
func TestObserverRuntime_ProcessEvent_NilEvent(t *testing.T) {
	processor := &mockProcessor{
		name: "test",
		processFunc: func(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error) {
			return nil, nil // Ignore event
		},
	}

	emitter := &mockEmitter{name: "test-emitter"}

	runtime, err := NewObserverRuntime(processor, WithEmitters(emitter))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runtime.Run(ctx)
	}()
	time.Sleep(100 * time.Millisecond)

	// Process event that returns nil
	err = runtime.ProcessEvent(ctx, []byte("test"))
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Verify no event was emitted
	assert.Equal(t, 0, emitter.EmittedCount())
}

// RED: Test IsHealthy
func TestObserverRuntime_IsHealthy(t *testing.T) {
	processor := &mockProcessor{name: "test"}
	runtime, err := NewObserverRuntime(processor)
	require.NoError(t, err)

	// Before starting - not healthy
	assert.False(t, runtime.IsHealthy())

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Start runtime
	errCh := make(chan error, 1)
	go func() {
		errCh <- runtime.Run(ctx)
	}()
	time.Sleep(100 * time.Millisecond)

	// After starting - healthy
	assert.True(t, runtime.IsHealthy())

	// Cancel and wait
	cancel()
	time.Sleep(100 * time.Millisecond)

	// After stopping - not healthy
	assert.False(t, runtime.IsHealthy())
}

// RED: Test critical emitter failure
func TestObserverRuntime_CriticalEmitterFailure(t *testing.T) {
	processor := &mockProcessor{
		name: "test",
		processFunc: func(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error) {
			return &domain.ObserverEvent{
				Type:    "network",
				Subtype: "dns_query",
			}, nil
		},
	}

	criticalEmitter := &mockEmitter{name: "otlp", critical: true, alwaysFail: true}
	nonCriticalEmitter := &mockEmitter{name: "nats", critical: false}

	runtime, err := NewObserverRuntime(processor, func(r *ObserverRuntime) {
		r.config.Sampling.Enabled = false // Disable sampling for this test
	}, WithEmitters(criticalEmitter, nonCriticalEmitter))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runtime.Run(ctx)
	}()
	time.Sleep(20 * time.Millisecond)

	// Process event - ProcessEvent should succeed (just enqueues)
	err = runtime.ProcessEvent(ctx, []byte("test"))
	require.NoError(t, err)

	// Wait for drainQueue attempts (will retry but keep failing)
	time.Sleep(50 * time.Millisecond)

	// Non-critical emitter should NOT receive event (we break on critical failure)
	assert.Equal(t, 0, nonCriticalEmitter.EmittedCount(), "Non-critical emitter should not receive event when critical emitter fails")
}

// RED: Test max retries prevents infinite retry loop
func TestObserverRuntime_MaxRetries(t *testing.T) {
	processor := &mockProcessor{
		name: "test",
		processFunc: func(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error) {
			return &domain.ObserverEvent{Type: "test", Subtype: "event"}, nil
		},
	}

	criticalEmitter := &mockEmitter{
		name:       "otlp",
		critical:   true,
		alwaysFail: true,
	}

	runtime, err := NewObserverRuntime(processor, func(r *ObserverRuntime) {
		r.config.Sampling.Enabled = false
		r.config.Backpressure.MaxRetries = 3 // Max 3 retry attempts
	}, WithEmitters(criticalEmitter))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runtime.Run(ctx)
	}()
	time.Sleep(20 * time.Millisecond)

	// Process event
	err = runtime.ProcessEvent(ctx, []byte("test"))
	require.NoError(t, err)

	// Wait for retries to exhaust
	time.Sleep(150 * time.Millisecond)

	// Should attempt initial + 3 retries = 4 total
	assert.LessOrEqual(t, criticalEmitter.AttemptCount(), 4, "Should not exceed max retries (1 initial + 3 retries)")
}

// RED: Test non-critical emitter failure
func TestObserverRuntime_NonCriticalEmitterFailure(t *testing.T) {
	processor := &mockProcessor{
		name: "test",
		processFunc: func(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error) {
			return &domain.ObserverEvent{
				Type:    "network",
				Subtype: "dns_query",
			}, nil
		},
	}

	criticalEmitter := &mockEmitter{name: "otlp", critical: true}
	nonCriticalEmitter := &mockEmitter{name: "nats", critical: false, failNext: true}

	runtime, err := NewObserverRuntime(processor, func(r *ObserverRuntime) {
		r.config.Sampling.Enabled = false // Disable sampling for this test
	}, WithEmitters(criticalEmitter, nonCriticalEmitter))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runtime.Run(ctx)
	}()
	time.Sleep(100 * time.Millisecond)

	// Process event - should succeed despite non-critical emitter failure
	err = runtime.ProcessEvent(ctx, []byte("test"))
	assert.NoError(t, err)

	// Wait for drainQueue to emit
	time.Sleep(200 * time.Millisecond)

	// Critical emitter should have received event
	assert.Equal(t, 1, criticalEmitter.EmittedCount())
}

func TestObserverRuntime_SamplingDropsEvents(t *testing.T) {
	proc := &mockProcessor{name: "test"}
	emitter := &mockEmitter{name: "test", critical: true}

	// Create runtime with 0% sampling (drop all)
	runtime, err := NewObserverRuntime(proc, func(r *ObserverRuntime) {
		r.config.Sampling.Enabled = true
		r.config.Sampling.DefaultRate = 0.0 // Drop all
	}, WithEmitters(emitter))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runtime.Run(ctx)
	}()
	time.Sleep(100 * time.Millisecond)

	// Process event
	proc.processFunc = func(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error) {
		return &domain.ObserverEvent{Type: "test", Subtype: "event"}, nil
	}

	err = runtime.ProcessEvent(ctx, []byte("data"))
	require.NoError(t, err)

	// Emitter should NOT receive event (sampled out)
	assert.Equal(t, 0, emitter.EmittedCount(), "Event should be sampled out")
}

func TestObserverRuntime_SamplingKeepsEvents(t *testing.T) {
	proc := &mockProcessor{name: "test"}
	emitter := &mockEmitter{name: "test", critical: true}

	// Create runtime with 100% sampling (keep all)
	runtime, err := NewObserverRuntime(proc, func(r *ObserverRuntime) {
		r.config.Sampling.Enabled = true
		r.config.Sampling.DefaultRate = 1.0 // Keep all
	}, WithEmitters(emitter))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runtime.Run(ctx)
	}()
	time.Sleep(100 * time.Millisecond)

	// Process event
	proc.processFunc = func(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error) {
		return &domain.ObserverEvent{Type: "test", Subtype: "event"}, nil
	}

	err = runtime.ProcessEvent(ctx, []byte("data"))
	require.NoError(t, err)

	// Give it time to emit
	time.Sleep(50 * time.Millisecond)

	// Emitter SHOULD receive event (100% sampling)
	assert.Equal(t, 1, emitter.EmittedCount(), "Event should be kept")
}

// RED: Test multiple emitters fan-out (OTLP + File simultaneously)
func TestObserverRuntime_MultipleEmitters_FanOut(t *testing.T) {
	proc := &mockProcessor{name: "test"}

	// Create two emitters
	emitter1 := &mockEmitter{name: "otlp", critical: true}
	emitter2 := &mockEmitter{name: "file", critical: false}

	runtime, err := NewObserverRuntime(proc,
		WithEmitters(emitter1, emitter2),
		WithSamplingDisabled(), // Disable sampling for predictable tests
	)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() {
		err := runtime.Run(ctx)
		assert.NoError(t, err)
	}()

	time.Sleep(100 * time.Millisecond)

	// Process one event
	err = runtime.ProcessEvent(ctx, []byte("test-data"))
	require.NoError(t, err)

	// Give it time to drain queue and emit to both (default drain interval is 50ms)
	time.Sleep(500 * time.Millisecond)

	// Both emitters should receive the same event
	assert.Equal(t, 1, emitter1.EmittedCount(), "OTLP emitter should receive event")
	assert.Equal(t, 1, emitter2.EmittedCount(), "File emitter should receive event")

	// Verify they got the same event (only if both received it)
	if emitter1.EmittedCount() > 0 && emitter2.EmittedCount() > 0 {
		assert.Equal(t, emitter1.EmittedEvents()[0].ID, emitter2.EmittedEvents()[0].ID)
	}
}

// RED: Test one emitter failure doesn't block others
func TestObserverRuntime_MultipleEmitters_OneFailure(t *testing.T) {
	proc := &mockProcessor{name: "test"}

	// Create emitters (emitter1 will fail)
	emitter1 := &mockEmitter{name: "failing", critical: false, alwaysFail: true}
	emitter2 := &mockEmitter{name: "working", critical: false}

	runtime, err := NewObserverRuntime(proc,
		WithEmitters(emitter1, emitter2),
		WithSamplingDisabled(),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() {
		err := runtime.Run(ctx)
		assert.NoError(t, err)
	}()

	time.Sleep(100 * time.Millisecond)

	// Process event
	err = runtime.ProcessEvent(ctx, []byte("test-data"))
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond)

	// Working emitter should still succeed
	assert.Equal(t, 1, emitter2.EmittedCount(), "Working emitter should receive event")
	assert.Greater(t, emitter1.AttemptCount(), 0, "Failing emitter should have tried")
}

// RED: Test critical emitter failure blocks processing
func TestObserverRuntime_MultipleEmitters_CriticalFailure(t *testing.T) {
	proc := &mockProcessor{name: "test"}

	// Create emitters (critical one fails)
	emitter1 := &mockEmitter{name: "critical-failing", critical: true, alwaysFail: true}
	emitter2 := &mockEmitter{name: "non-critical", critical: false}

	runtime, err := NewObserverRuntime(proc,
		WithEmitters(emitter1, emitter2),
		WithSamplingDisabled(),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() {
		err := runtime.Run(ctx)
		assert.NoError(t, err)
	}()

	time.Sleep(100 * time.Millisecond)

	// Process event
	err = runtime.ProcessEvent(ctx, []byte("test-data"))
	require.NoError(t, err)

	// Give it time to retry and fail
	time.Sleep(500 * time.Millisecond)

	// Critical emitter failure should cause retries
	assert.Greater(t, emitter1.AttemptCount(), 1, "Should have retried critical emitter")
}
