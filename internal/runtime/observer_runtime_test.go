package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

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

func (m *mockProcessor) Setup(ctx context.Context) error {
	m.setupCalled = true
	return nil
}

func (m *mockProcessor) Teardown(ctx context.Context) error {
	m.teardownCalled = true
	return nil
}

func (m *mockProcessor) Process(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error) {
	if m.processFunc != nil {
		return m.processFunc(ctx, rawEvent)
	}
	return &domain.ObserverEvent{
		Type:    "test",
		Subtype: "mock",
	}, nil
}

// Mock emitter for testing
type mockEmitter struct {
	name        string
	emitted     []*domain.ObserverEvent
	closeCalled bool
}

func (m *mockEmitter) Name() string {
	return m.name
}

func (m *mockEmitter) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	m.emitted = append(m.emitted, event)
	return nil
}

func (m *mockEmitter) Close() error {
	m.closeCalled = true
	return nil
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
	go runtime.Run(ctx)

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

	runtime, err := NewObserverRuntime(processor, WithEmitters(emitter))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Start runtime
	go runtime.Run(ctx)
	time.Sleep(100 * time.Millisecond) // Wait for startup

	// Process event
	rawEvent := []byte("test event")
	err = runtime.ProcessEvent(ctx, rawEvent)
	require.NoError(t, err)

	// Give it time to emit
	time.Sleep(100 * time.Millisecond)

	// Verify event was emitted
	assert.Len(t, emitter.emitted, 1)
	assert.Equal(t, "network", emitter.emitted[0].Type)
	assert.Equal(t, "dns_query", emitter.emitted[0].Subtype)
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

	go runtime.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	// Process event that returns nil
	err = runtime.ProcessEvent(ctx, []byte("test"))
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Verify no event was emitted
	assert.Len(t, emitter.emitted, 0)
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
	go runtime.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	// After starting - healthy
	assert.True(t, runtime.IsHealthy())

	// Cancel and wait
	cancel()
	time.Sleep(100 * time.Millisecond)

	// After stopping - not healthy
	assert.False(t, runtime.IsHealthy())
}
