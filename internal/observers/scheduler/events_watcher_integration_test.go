package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/pkg/domain"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

// TestEventsWatcher_Integration verifies complete Events API watcher workflow
func TestEventsWatcher_Integration(t *testing.T) {
	// Create fake K8s client
	clientset := fake.NewSimpleClientset()

	// Create mock emitter to capture events
	mockEmitter := &mockEmitter{events: make([]*domain.ObserverEvent, 0)}

	// Create Scheduler Observer with Events watcher
	baseObs, err := base.NewBaseObserver("test-scheduler")
	require.NoError(t, err)

	obs := &SchedulerObserver{
		BaseObserver: baseObs,
		emitter:      mockEmitter,
	}

	// Create and start EventsWatcher
	watcher := NewEventsWatcher(clientset, obs)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run watcher in background
	go func() {
		if err := watcher.Run(ctx); err != nil {
			t.Logf("watcher.Run() error: %v", err)
		}
	}()

	// Give informer time to start and initialize cache
	// NOTE: fake.NewSimpleClientset() requires async initialization before events
	// can be processed. This is NOT flakiness - it's real async behavior we're testing.
	// Alternative (retry loops) would add complexity for zero benefit.
	time.Sleep(200 * time.Millisecond)

	// Create initial event (should be skipped by OnAdd)
	initialEvent := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nginx-scheduling-failed",
			Namespace: "default",
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:      "Pod",
			Name:      "nginx-pod",
			Namespace: "default",
			UID:       types.UID("pod-123"),
		},
		Reason:  "FailedScheduling",
		Message: "0/3 nodes are available: 2 Insufficient cpu, 1 Insufficient memory.",
		Count:   1,
		LastTimestamp: metav1.Time{
			Time: time.Now(),
		},
	}

	_, err = clientset.CoreV1().Events("default").Create(ctx, initialEvent, metav1.CreateOptions{})
	require.NoError(t, err)

	// Wait for event processing
	time.Sleep(200 * time.Millisecond)

	// Initial event should be skipped (OnAdd)
	assert.Equal(t, 0, mockEmitter.eventCount(), "OnAdd events should be skipped")

	// Update event (increment count - new scheduling attempt)
	initialEvent.Count = 2
	initialEvent.LastTimestamp = metav1.Time{Time: time.Now()}

	_, err = clientset.CoreV1().Events("default").Update(ctx, initialEvent, metav1.UpdateOptions{})
	require.NoError(t, err)

	// Wait for event processing
	time.Sleep(200 * time.Millisecond)

	// Should have emitted one event
	require.Equal(t, 1, mockEmitter.eventCount(), "OnUpdate should emit event")

	events := mockEmitter.getEvents()
	require.Len(t, events, 1)
	event := events[0]
	assert.Equal(t, "scheduler", event.Type)
	assert.Equal(t, "failed_scheduling", event.Subtype)
	assert.Equal(t, "scheduler", event.Source)

	// Verify K8s data
	require.NotNil(t, event.K8sData)
	assert.Equal(t, "Pod", event.K8sData.ResourceKind)
	assert.Equal(t, "nginx-pod", event.K8sData.ResourceName)
	assert.Equal(t, "FailedScheduling", event.K8sData.Reason)

	// Verify scheduling data
	require.NotNil(t, event.SchedulingData)
	assert.Equal(t, "pod-123", event.SchedulingData.PodUID)
	assert.Equal(t, int32(2), event.SchedulingData.Attempts)
	assert.Equal(t, 0, event.SchedulingData.NodesFailed)
	assert.Equal(t, 3, event.SchedulingData.NodesTotal)
	assert.Equal(t, 2, len(event.SchedulingData.FailureReasons))
	assert.Equal(t, 2, event.SchedulingData.FailureReasons["Insufficient cpu"])
	assert.Equal(t, 1, event.SchedulingData.FailureReasons["Insufficient memory."])
}

// TestEventsWatcher_IgnoreNonScheduling verifies non-scheduling events are ignored
func TestEventsWatcher_IgnoreNonScheduling(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	mockEmitter := &mockEmitter{events: make([]*domain.ObserverEvent, 0)}

	baseObs, err := base.NewBaseObserver("test-scheduler")
	require.NoError(t, err)

	obs := &SchedulerObserver{
		BaseObserver: baseObs,
		emitter:      mockEmitter,
	}

	watcher := NewEventsWatcher(clientset, obs)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run watcher in background
	go func() {
		if err := watcher.Run(ctx); err != nil {
			t.Logf("watcher.Run() error: %v", err)
		}
	}()

	time.Sleep(200 * time.Millisecond)

	// Create non-scheduling event
	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nginx-pulling",
			Namespace: "default",
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:      "Pod",
			Name:      "nginx-pod",
			Namespace: "default",
		},
		Reason:  "Pulling",
		Message: "Pulling image nginx:1.20",
		Count:   1,
	}

	_, err = clientset.CoreV1().Events("default").Create(ctx, event, metav1.CreateOptions{})
	require.NoError(t, err)

	// Update event
	event.Count = 2
	_, err = clientset.CoreV1().Events("default").Update(ctx, event, metav1.UpdateOptions{})
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)

	// Should not emit any events
	assert.Equal(t, 0, mockEmitter.eventCount(), "Non-scheduling events should be ignored")
}

// TestEventsWatcher_IgnoreNonPod verifies non-pod events are ignored
func TestEventsWatcher_IgnoreNonPod(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	mockEmitter := &mockEmitter{events: make([]*domain.ObserverEvent, 0)}

	baseObs, err := base.NewBaseObserver("test-scheduler")
	require.NoError(t, err)

	obs := &SchedulerObserver{
		BaseObserver: baseObs,
		emitter:      mockEmitter,
	}

	watcher := NewEventsWatcher(clientset, obs)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run watcher in background
	go func() {
		if err := watcher.Run(ctx); err != nil {
			t.Logf("watcher.Run() error: %v", err)
		}
	}()

	time.Sleep(200 * time.Millisecond)

	// Create deployment event
	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deployment-event",
			Namespace: "default",
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:      "Deployment",
			Name:      "nginx",
			Namespace: "default",
		},
		Reason:  "FailedScheduling",
		Message: "Some message",
		Count:   1,
	}

	_, err = clientset.CoreV1().Events("default").Create(ctx, event, metav1.CreateOptions{})
	require.NoError(t, err)

	// Update event
	event.Count = 2
	_, err = clientset.CoreV1().Events("default").Update(ctx, event, metav1.UpdateOptions{})
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)

	// Should not emit any events
	assert.Equal(t, 0, mockEmitter.eventCount(), "Non-pod events should be ignored")
}

// TestEventsWatcher_NoCountIncrease verifies events with no count increase are skipped
func TestEventsWatcher_NoCountIncrease(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	mockEmitter := &mockEmitter{events: make([]*domain.ObserverEvent, 0)}

	baseObs, err := base.NewBaseObserver("test-scheduler")
	require.NoError(t, err)

	obs := &SchedulerObserver{
		BaseObserver: baseObs,
		emitter:      mockEmitter,
	}

	watcher := NewEventsWatcher(clientset, obs)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run watcher in background
	go func() {
		if err := watcher.Run(ctx); err != nil {
			t.Logf("watcher.Run() error: %v", err)
		}
	}()

	time.Sleep(200 * time.Millisecond)

	// Create initial event
	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nginx-failed",
			Namespace: "default",
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:      "Pod",
			Name:      "nginx-pod",
			Namespace: "default",
		},
		Reason:  "FailedScheduling",
		Message: "0/3 nodes are available: 3 Insufficient memory.",
		Count:   5,
	}

	_, err = clientset.CoreV1().Events("default").Create(ctx, event, metav1.CreateOptions{})
	require.NoError(t, err)

	// Update event without count increase
	event.Message = "Updated message"
	_, err = clientset.CoreV1().Events("default").Update(ctx, event, metav1.UpdateOptions{})
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)

	// Should not emit (count didn't increase)
	assert.Equal(t, 0, mockEmitter.eventCount(), "Should skip events without count increase")
}

// mockEmitter captures emitted events for testing (thread-safe)
type mockEmitter struct {
	mu     sync.Mutex
	events []*domain.ObserverEvent
}

func (m *mockEmitter) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
	return nil
}

func (m *mockEmitter) getEvents() []*domain.ObserverEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*domain.ObserverEvent{}, m.events...)
}

func (m *mockEmitter) eventCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.events)
}

func (m *mockEmitter) Close() error {
	return nil
}
