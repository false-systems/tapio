package deployments

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"k8s.io/client-go/kubernetes/fake"
)

// TDD: Metrics increment tests

func TestDeploymentsObserver_IncrementsMetricsOnUpdate(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	// Create emitter that captures events
	emitter := &captureEmitter{events: make([]*capturedEvent, 0)}

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewDeploymentsObserver("deployments", config)
	require.NoError(t, err)

	// Simulate deployment update
	old := createDeployment("app", 1, 1)
	new := createDeployment("app", 1, 1)
	observer.handleUpdate(old, new)

	// Verify metrics were incremented
	stats := observer.Stats()
	assert.Equal(t, int64(1), stats.EventsProcessed, "deploymentUpdates metric should increment")
}

func TestDeploymentsObserver_IncrementsReplicaChangeMetric(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := &captureEmitter{events: make([]*capturedEvent, 0)}

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewDeploymentsObserver("deployments", config)
	require.NoError(t, err)

	// Simulate replica change
	old := createDeployment("app", 1, 1)
	new := createDeployment("app", 5, 5)
	observer.handleUpdate(old, new)

	// Verify event was processed (replica change detected)
	require.Len(t, emitter.events, 1, "Should emit event for replica change")
	assert.Equal(t, "deployment_scaled", emitter.events[0].event.Type)
}

func TestDeploymentsObserver_IncrementsConditionChangeMetric(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := &captureEmitter{events: make([]*capturedEvent, 0)}

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewDeploymentsObserver("deployments", config)
	require.NoError(t, err)

	// Simulate condition change
	old := createDeploymentWithCondition("app", "Available", "False")
	new := createDeploymentWithCondition("app", "Available", "True")
	observer.handleUpdate(old, new)

	// Verify event was processed (condition change detected)
	require.Len(t, emitter.events, 1, "Should emit event for condition change")
	assert.Equal(t, "deployment_available", emitter.events[0].event.Type)
}

func TestDeploymentsObserver_IncrementsMetricsOnAdd(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := &captureEmitter{events: make([]*capturedEvent, 0)}

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewDeploymentsObserver("deployments", config)
	require.NoError(t, err)

	// Simulate deployment creation
	deploy := createDeployment("app", 3, 3)
	observer.handleAdd(deploy)

	// Verify event was processed
	require.Len(t, emitter.events, 1, "Should emit event for creation")
	assert.Equal(t, "deployment_created", emitter.events[0].event.Type)
}

func TestDeploymentsObserver_IncrementsMetricsOnDelete(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := &captureEmitter{events: make([]*capturedEvent, 0)}

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewDeploymentsObserver("deployments", config)
	require.NoError(t, err)

	// Simulate deployment deletion
	deploy := createDeployment("app", 3, 3)
	observer.handleDelete(deploy)

	// Verify event was processed
	require.Len(t, emitter.events, 1, "Should emit event for deletion")
	assert.Equal(t, "deployment_deleted", emitter.events[0].event.Type)
}

// Helper to capture emitted events for testing
type capturedEvent struct {
	ctx   context.Context
	event *domain.ObserverEvent
}

type captureEmitter struct {
	events []*capturedEvent
}

func (e *captureEmitter) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	e.events = append(e.events, &capturedEvent{ctx: ctx, event: event})
	return nil
}

func (e *captureEmitter) Close() error {
	return nil
}
