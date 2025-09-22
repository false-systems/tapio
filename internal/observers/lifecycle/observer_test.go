package lifecycle

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/yairfalse/tapio/internal/observers/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
)

// TestDefaultConfig tests default configuration
func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.Equal(t, 10000, config.BufferSize)
	assert.Equal(t, 30*time.Minute, config.ResyncPeriod)
	assert.True(t, config.TrackPods)
	assert.True(t, config.TrackDeployments)
	assert.True(t, config.TrackNodes)
	assert.True(t, config.TrackServices)
}

// TestNewObserver tests observer creation with mock client
func TestNewObserver(t *testing.T) {
	// We can't easily test NewObserver without dependency injection
	// because it creates its own K8s client internally
	// This would require refactoring to accept a client
	t.Skip("Requires dependency injection for K8s client")
}

// TestObserverWithMockClient tests observer with injected client
func TestObserverWithMockClient(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	fakeClient := fake.NewSimpleClientset()

	// Create observer manually with mock client
	observer := &Observer{
		BaseObserver:        base.NewBaseObserver("lifecycle", 30*time.Second),
		EventChannelManager: base.NewEventChannelManager(config.BufferSize, "lifecycle", logger),
		LifecycleManager:    base.NewLifecycleManager(context.Background(), logger),
		logger:              logger,
		client:              fakeClient,
		detector:            NewTransitionDetector(),
		tracker:             NewStateTracker(),
		informers:           make([]cache.SharedIndexInformer, 0),
	}

	assert.NotNil(t, observer)
	assert.Equal(t, "lifecycle", observer.Name())
	assert.NotNil(t, observer.Events())
}

// TestHandleTransition tests transition handling
func TestHandleTransition(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	fakeClient := fake.NewSimpleClientset()

	observer := &Observer{
		BaseObserver:        base.NewBaseObserver("lifecycle", 30*time.Second),
		EventChannelManager: base.NewEventChannelManager(config.BufferSize, "lifecycle", logger),
		LifecycleManager:    base.NewLifecycleManager(context.Background(), logger),
		logger:              logger,
		client:              fakeClient,
		detector:            NewTransitionDetector(),
		tracker:             NewStateTracker(),
		informers:           make([]cache.SharedIndexInformer, 0),
	}

	// Create a breaking transition
	transition := &LifecycleTransition{
		Type:      TransitionScaleToZero,
		Timestamp: time.Now(),
		State: StateChange{
			Resource: ResourceIdentifier{
				Kind:       "Deployment",
				Name:       "test-app",
				Namespace:  "default",
				UID:        types.UID("test-uid"),
				APIVersion: "apps/v1",
			},
			FromState: "3 replicas",
			ToState:   "0 replicas",
		},
		Resources: AffectedResources{
			DirectCount: 3,
			Pods: []ResourceIdentifier{
				{Kind: "Pod", Name: "pod1", Namespace: "default"},
				{Kind: "Pod", Name: "pod2", Namespace: "default"},
				{Kind: "Pod", Name: "pod3", Namespace: "default"},
			},
		},
	}

	// Handle the transition
	observer.handleTransition(transition)

	// Check that event was sent
	select {
	case event := <-observer.Events():
		assert.NotNil(t, event)
		assert.Contains(t, event.EventID, "lifecycle-test-uid")
		assert.Equal(t, domain.EventSeverityCritical, event.Severity)
		assert.Equal(t, "lifecycle", event.Source)
		assert.NotNil(t, event.EventData.KubernetesResource)
		assert.Equal(t, "Deployment", event.EventData.KubernetesResource.Kind)
		assert.Equal(t, "test-app", event.EventData.KubernetesResource.Name)
		assert.Equal(t, "lifecycle", event.Metadata.Labels["observer"])
		assert.Equal(t, "1.0.0", event.Metadata.Labels["version"])
	case <-time.After(time.Second):
		t.Fatal("expected event not received")
	}
}

// TestConvertToDomainEvent tests event conversion
func TestConvertToDomainEvent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	observer := &Observer{
		logger: logger,
	}

	transition := &LifecycleTransition{
		Type:      TransitionCrashLoop,
		Timestamp: time.Now(),
		State: StateChange{
			Resource: ResourceIdentifier{
				Kind:       "Pod",
				Name:       "failing-pod",
				Namespace:  "production",
				UID:        types.UID("pod-123"),
				APIVersion: "v1",
			},
			FromState: "Running",
			ToState:   "CrashLoopBackOff",
		},
		Resources: AffectedResources{
			DirectCount: 1,
		},
	}

	event := observer.convertToDomainEvent(transition)

	assert.NotNil(t, event)
	assert.Contains(t, event.EventID, "lifecycle-pod-123")
	assert.Equal(t, "lifecycle", event.Source)
	assert.Equal(t, domain.EventSeverityCritical, event.Severity)
	assert.Equal(t, "Pod", event.EventData.KubernetesResource.Kind)
	assert.Equal(t, "failing-pod", event.EventData.KubernetesResource.Name)
	assert.Equal(t, "production", event.EventData.KubernetesResource.Namespace)
	assert.Equal(t, "lifecycle", event.Metadata.Labels["observer"])
	assert.Equal(t, "1.0.0", event.Metadata.Labels["version"])
	assert.Equal(t, string(TransitionCrashLoop), event.Metadata.Labels["transition_type"])
}

// TestMapSeverity tests severity mapping
func TestMapSeverity(t *testing.T) {
	logger := zaptest.NewLogger(t)
	observer := &Observer{
		logger: logger,
	}

	tests := []struct {
		transition TransitionType
		expected   domain.EventSeverity
	}{
		{TransitionScaleToZero, domain.EventSeverityCritical},
		{TransitionDeletion, domain.EventSeverityCritical},
		{TransitionOOMKill, domain.EventSeverityCritical},
		{TransitionCrashLoop, domain.EventSeverityCritical},
		{TransitionScaleDown, domain.EventSeverityError},
		{TransitionResourceCut, domain.EventSeverityError},
		{TransitionEviction, domain.EventSeverityError},
		{TransitionRollout, domain.EventSeverityWarning},
		{TransitionConfigChange, domain.EventSeverityWarning},
		{TransitionType("unknown"), domain.EventSeverityInfo},
	}

	for _, tt := range tests {
		t.Run(string(tt.transition), func(t *testing.T) {
			severity := observer.mapSeverity(tt.transition)
			assert.Equal(t, tt.expected, severity)
		})
	}
}

// TestIsHealthy tests health check
func TestIsHealthy(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	fakeClient := fake.NewSimpleClientset()

	observer := &Observer{
		BaseObserver:        base.NewBaseObserver("lifecycle", 30*time.Second),
		EventChannelManager: base.NewEventChannelManager(config.BufferSize, "lifecycle", logger),
		LifecycleManager:    base.NewLifecycleManager(context.Background(), logger),
		logger:              logger,
		client:              fakeClient,
		detector:            NewTransitionDetector(),
		tracker:             NewStateTracker(),
		informers:           make([]cache.SharedIndexInformer, 0),
	}

	// Check initial state (BaseObserver might default to healthy or unhealthy)
	initialHealth := observer.IsHealthy()

	// Set to opposite of initial
	observer.BaseObserver.SetHealthy(!initialHealth)
	assert.Equal(t, !initialHealth, observer.IsHealthy())

	// Set healthy
	observer.BaseObserver.SetHealthy(true)
	assert.True(t, observer.IsHealthy())

	// Set unhealthy
	observer.BaseObserver.SetHealthy(false)
	assert.False(t, observer.IsHealthy())
}

// TestObserverLifecycle tests Start and Stop
func TestObserverLifecycle(t *testing.T) {
	t.Skip("Requires K8s informers setup which needs actual K8s resources")
	// This would need more complex mocking of K8s informers and watchers
}
