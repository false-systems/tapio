package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

// RED: Test extractEntityID from K8s events
func TestExtractEntityID_K8sEvent(t *testing.T) {
	tests := []struct {
		name     string
		event    *domain.ObserverEvent
		expected string
	}{
		{
			name: "pod event - extracts namespace/name",
			event: &domain.ObserverEvent{
				Type: "pod",
				K8sData: &domain.K8sEventData{
					ResourceKind:      "Pod",
					ResourceName:      "nginx-abc",
					ResourceNamespace: "default",
				},
			},
			expected: "default/nginx-abc",
		},
		{
			name: "deployment event - extracts namespace/name",
			event: &domain.ObserverEvent{
				Type: "deployment",
				K8sData: &domain.K8sEventData{
					ResourceKind:      "Deployment",
					ResourceName:      "nginx-deployment",
					ResourceNamespace: "production",
				},
			},
			expected: "production/nginx-deployment",
		},
		{
			name: "service event - extracts namespace/name",
			event: &domain.ObserverEvent{
				Type: "service",
				K8sData: &domain.K8sEventData{
					ResourceKind:      "Service",
					ResourceName:      "api-service",
					ResourceNamespace: "default",
				},
			},
			expected: "default/api-service",
		},
		{
			name: "no K8sData - returns empty",
			event: &domain.ObserverEvent{
				Type: "network",
			},
			expected: "",
		},
		{
			name: "K8sData with no namespace - returns empty",
			event: &domain.ObserverEvent{
				Type: "pod",
				K8sData: &domain.K8sEventData{
					ResourceKind: "Pod",
					ResourceName: "nginx",
					// No namespace
				},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entityID := extractEntityID(tt.event) // ❌ Doesn't exist yet
			assert.Equal(t, tt.expected, entityID)
		})
	}
}

// RED: Test extractEntityID from network events
func TestExtractEntityID_NetworkEvent(t *testing.T) {
	tests := []struct {
		name     string
		event    *domain.ObserverEvent
		expected string
	}{
		{
			name: "network event with K8s context - prefers K8s entity",
			event: &domain.ObserverEvent{
				Type: "network",
				K8sData: &domain.K8sEventData{
					ResourceKind:      "Pod",
					ResourceName:      "nginx-pod",
					ResourceNamespace: "default",
				},
				NetworkData: &domain.NetworkEventData{
					SrcIP: "10.0.0.1",
					DstIP: "10.0.0.2",
				},
			},
			expected: "default/nginx-pod", // Prefer K8s entity over IP
		},
		{
			name: "network event without K8s - uses source IP",
			event: &domain.ObserverEvent{
				Type: "network",
				NetworkData: &domain.NetworkEventData{
					SrcIP:   "10.0.0.1",
					SrcPort: 8080,
				},
			},
			expected: "10.0.0.1:8080",
		},
		{
			name: "network event with no port - uses IP only",
			event: &domain.ObserverEvent{
				Type: "network",
				NetworkData: &domain.NetworkEventData{
					SrcIP: "10.0.0.1",
				},
			},
			expected: "10.0.0.1",
		},
		{
			name: "network event with no data - returns empty",
			event: &domain.ObserverEvent{
				Type: "network",
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entityID := extractEntityID(tt.event) // ❌ Doesn't exist yet
			assert.Equal(t, tt.expected, entityID)
		})
	}
}

// RED: Test auto-tracking in ProcessEvent
func TestObserverRuntime_AutoTracksEntityCausality(t *testing.T) {
	processor := &mockProcessor{
		name: "test-observer",
		processFunc: func(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error) {
			event := &domain.ObserverEvent{
				ID:     domain.NewEventID(),
				SpanID: "span-123",
				Type:   "pod",
				K8sData: &domain.K8sEventData{
					ResourceKind:      "Pod",
					ResourceName:      "nginx-abc",
					ResourceNamespace: "default",
				},
			}
			return event, nil
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

	// BEFORE processing first event, entity has no span
	parentSpan := runtime.GetParentSpanForEntity("default/nginx-abc")
	assert.Empty(t, parentSpan, "Entity should have no span before first event")

	// Process first event
	err = runtime.ProcessEvent(ctx, []byte("test-data"))
	require.NoError(t, err)

	// AFTER processing first event, entity has span-123
	parentSpan = runtime.GetParentSpanForEntity("default/nginx-abc")
	assert.Equal(t, "span-123", parentSpan, "Entity should have span-123 after first event")

	// Process a second event for same entity with different span
	processor.processFunc = func(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error) {
		return &domain.ObserverEvent{
			ID:     domain.NewEventID(),
			SpanID: "span-456",
			Type:   "pod",
			K8sData: &domain.K8sEventData{
				ResourceKind:      "Pod",
				ResourceName:      "nginx-abc",
				ResourceNamespace: "default",
			},
		}, nil
	}

	err = runtime.ProcessEvent(ctx, []byte("test-data-2"))
	require.NoError(t, err)

	// AFTER second event, entity now has span-456 (most recent)
	parentSpan = runtime.GetParentSpanForEntity("default/nginx-abc")
	assert.Equal(t, "span-456", parentSpan, "Entity should have span-456 after second event (most recent)")
}

// RED: Test auto-tracking skips events without entity ID
func TestObserverRuntime_AutoTrackSkipsNoEntityID(t *testing.T) {
	processor := &mockProcessor{
		name: "test-observer",
		processFunc: func(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error) {
			return &domain.ObserverEvent{
				ID:     domain.NewEventID(),
				SpanID: "span-123",
				Type:   "network", // No K8sData, no NetworkData
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

	require.Eventually(t, func() bool {
		return runtime.IsHealthy()
	}, 100*time.Millisecond, 10*time.Millisecond)

	// Process event without entity ID
	err = runtime.ProcessEvent(ctx, []byte("test-data"))
	require.NoError(t, err) // Should not error, just skip tracking

	// Causality tracker should be empty (no tracking happened)
	tracker := runtime.CausalityTracker()
	require.NotNil(t, tracker)
}

// RED: Test auto-tracking skips events without SpanID
func TestObserverRuntime_AutoTrackSkipsNoSpanID(t *testing.T) {
	processor := &mockProcessor{
		name: "test-observer",
		processFunc: func(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error) {
			return &domain.ObserverEvent{
				ID:   domain.NewEventID(),
				Type: "pod",
				K8sData: &domain.K8sEventData{
					ResourceKind:      "Pod",
					ResourceName:      "nginx",
					ResourceNamespace: "default",
				},
				// No SpanID!
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

	require.Eventually(t, func() bool {
		return runtime.IsHealthy()
	}, 100*time.Millisecond, 10*time.Millisecond)

	// Process event without SpanID
	err = runtime.ProcessEvent(ctx, []byte("test-data"))
	require.NoError(t, err) // Should not error

	// Query should return empty (no tracking happened)
	parentSpan := runtime.GetParentSpanForEntity("default/nginx")
	assert.Empty(t, parentSpan, "Event without SpanID should not be tracked")
}
