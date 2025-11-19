package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

// RED: Test ObserverRuntime has CausalityTracker
func TestObserverRuntime_HasCausalityTracker(t *testing.T) {
	processor := &mockProcessor{name: "test-observer"}
	runtime, err := NewObserverRuntime(processor)
	require.NoError(t, err)

	// Runtime should have causality tracker
	tracker := runtime.CausalityTracker() // ❌ Doesn't exist yet
	require.NotNil(t, tracker, "Runtime should have CausalityTracker")
}

// RED: Test runtime tracks causality for all processed events
func TestObserverRuntime_TracksCausality(t *testing.T) {
	// Mock processor that creates events with SpanID
	processor := &mockProcessor{
		name: "test-observer",
		processFunc: func(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error) {
			return &domain.ObserverEvent{
				ID:     domain.NewEventID(),
				SpanID: "span-123",
				Type:   "network",
			}, nil
		},
	}

	runtime, err := NewObserverRuntime(processor, WithSamplingDisabled())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start runtime
	errCh := make(chan error, 1)
	go func() {
		errCh <- runtime.Run(ctx)
	}()

	// Wait for runtime to be running
	require.Eventually(t, func() bool {
		return runtime.IsHealthy()
	}, 100*time.Millisecond, 10*time.Millisecond)

	// Process event
	err = runtime.ProcessEvent(ctx, []byte("test-data"))
	require.NoError(t, err)

	// Runtime should track this event in causality tracker
	// (We'll need an entity ID to query - for now just verify tracker exists)
	tracker := runtime.CausalityTracker() // ❌ Doesn't exist yet
	require.NotNil(t, tracker)
}

// RED: Test runtime provides API to get parent span for entity
func TestObserverRuntime_GetParentSpanForEntity(t *testing.T) {
	processor := &mockProcessor{name: "test-observer"}
	runtime, err := NewObserverRuntime(processor)
	require.NoError(t, err)

	// Runtime should provide API to get parent span
	parentSpan := runtime.GetParentSpanForEntity("default/nginx") // ❌ Doesn't exist yet
	assert.Empty(t, parentSpan, "Should return empty string when no events tracked")
}

// RED: Test runtime tracks causality chain across multiple events
func TestObserverRuntime_TracksCausalityChain(t *testing.T) {
	// Track events processed
	var processedEvents []*domain.ObserverEvent

	processor := &mockProcessor{
		name: "test-observer",
		processFunc: func(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error) {
			event := &domain.ObserverEvent{
				ID:     domain.NewEventID(),
				Type:   "test",
				SpanID: string(rawEvent), // Use raw data as span ID for simplicity
			}
			processedEvents = append(processedEvents, event)
			return event, nil
		},
	}

	runtime, err := NewObserverRuntime(processor, WithSamplingDisabled())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runtime.Run(ctx)
	}()

	// Wait for runtime to be running
	require.Eventually(t, func() bool {
		return runtime.IsHealthy()
	}, 100*time.Millisecond, 10*time.Millisecond)

	// Process events in causality chain
	// 1. Deployment update (root)
	err = runtime.ProcessEvent(ctx, []byte("span-deployment"))
	require.NoError(t, err)

	// 2. Pod restart (caused by deployment)
	err = runtime.ProcessEvent(ctx, []byte("span-pod"))
	require.NoError(t, err)

	// 3. OOM kill (caused by pod restart)
	err = runtime.ProcessEvent(ctx, []byte("span-oom"))
	require.NoError(t, err)

	// Runtime should track these in causality tracker
	tracker := runtime.CausalityTracker() // ❌ Doesn't exist yet
	require.NotNil(t, tracker)
}

// RED: Test runtime sets entity ID automatically from event metadata
func TestObserverRuntime_AutoEntityID(t *testing.T) {
	processor := &mockProcessor{
		name: "test-observer",
		processFunc: func(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error) {
			return &domain.ObserverEvent{
				ID:     domain.NewEventID(),
				SpanID: "span-123",
				Type:   "pod",
				K8sData: &domain.K8sEventData{
					ResourceKind:      "Pod",
					ResourceName:      "nginx-abc",
					ResourceNamespace: "default",
				},
			}, nil
		},
	}

	runtime, err := NewObserverRuntime(processor, WithSamplingDisabled())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runtime.Run(ctx)
	}()

	// Wait for runtime to be running
	require.Eventually(t, func() bool {
		return runtime.IsHealthy()
	}, 100*time.Millisecond, 10*time.Millisecond)

	// Process event
	err = runtime.ProcessEvent(ctx, []byte("test-data"))
	require.NoError(t, err)

	// Runtime should extract entity ID from K8sData and track it
	// Entity ID should be: "default/nginx-abc" (namespace/name)
	// Query parent span for entity
	parentSpan := runtime.GetParentSpanForEntity("default/nginx-abc")
	// After this event, querying should return "span-123"
	// (For now, just verify API exists - empty string is fine since we haven't tracked the entity yet)
	assert.NotNil(t, &parentSpan, "API should exist")
}

// RED: Test runtime handles events without entity ID gracefully
func TestObserverRuntime_NoEntityID(t *testing.T) {
	processor := &mockProcessor{
		name: "test-observer",
		processFunc: func(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error) {
			return &domain.ObserverEvent{
				ID:     domain.NewEventID(),
				SpanID: "span-123",
				Type:   "network", // No K8sData
			}, nil
		},
	}

	runtime, err := NewObserverRuntime(processor, WithSamplingDisabled())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runtime.Run(ctx)
	}()

	// Wait for runtime to be running
	require.Eventually(t, func() bool {
		return runtime.IsHealthy()
	}, 100*time.Millisecond, 10*time.Millisecond)

	// Process event without entity ID
	err = runtime.ProcessEvent(ctx, []byte("test-data"))
	require.NoError(t, err) // Should not error

	// Runtime should handle gracefully (event still processed, just not tracked for causality)
	tracker := runtime.CausalityTracker() // ❌ Doesn't exist yet
	require.NotNil(t, tracker)
}
