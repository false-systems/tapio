//go:build linux
// +build linux

package node

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TDD Cycle 1: Observer constructor + validation

// TestNewObserver_Success verifies constructor with valid config
func TestNewObserver_Success(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := &mockEmitter{events: make([]*domain.ObserverEvent, 0)}

	cfg := Config{
		Clientset: clientset,
		Emitter:   emitter,
	}

	observer, err := NewObserver("test-observer", cfg)
	require.NoError(t, err)
	require.NotNil(t, observer)
	assert.Equal(t, "test-observer", observer.name)
	assert.NotNil(t, observer.stopCh)
}

// TestNewObserver_MissingClientset verifies error when clientset is nil
func TestNewObserver_MissingClientset(t *testing.T) {
	emitter := &mockEmitter{events: make([]*domain.ObserverEvent, 0)}

	cfg := Config{
		Clientset: nil,
		Emitter:   emitter,
	}

	observer, err := NewObserver("test-observer", cfg)
	require.Error(t, err)
	assert.Nil(t, observer)
	assert.Contains(t, err.Error(), "clientset is required")
}

// TestNewObserver_MissingEmitter verifies error when emitter is nil
func TestNewObserver_MissingEmitter(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	cfg := Config{
		Clientset: clientset,
		Emitter:   nil,
	}

	observer, err := NewObserver("test-observer", cfg)
	require.Error(t, err)
	assert.Nil(t, observer)
	assert.Contains(t, err.Error(), "emitter is required")
}

// TDD Cycle 2: Start/Stop lifecycle + event handlers

// TestObserver_StartStop verifies observer lifecycle
func TestObserver_StartStop(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := &mockEmitter{events: make([]*domain.ObserverEvent, 0)}

	cfg := Config{
		Clientset: clientset,
		Emitter:   emitter,
	}

	observer, err := NewObserver("test-observer", cfg)
	require.NoError(t, err)

	// Observer should be healthy before Start (stopCh initialized)
	assert.True(t, observer.IsHealthy())

	// Start should succeed
	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)

	// Observer should still be healthy after Start
	assert.True(t, observer.IsHealthy())

	// Stop should succeed
	err = observer.Stop()
	require.NoError(t, err)

	// Observer should be unhealthy after Stop (stopCh closed)
	assert.False(t, observer.IsHealthy())
}

// TestObserver_StartRegistersHandlers verifies event handlers are registered
func TestObserver_StartRegistersHandlers(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := &mockEmitter{events: make([]*domain.ObserverEvent, 0)}

	cfg := Config{
		Clientset: clientset,
		Emitter:   emitter,
	}

	observer, err := NewObserver("test-observer", cfg)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)

	// Verify informer has handlers registered (non-zero handler count)
	// Note: K8s fake client doesn't actually run informers, so this just
	// verifies Start() doesn't panic and completes successfully
	assert.NotNil(t, observer.informer)
}

// TDD Cycle 3: NodeReady detection

// TestHandleNode_NodeReady verifies NodeReady event emission
func TestHandleNode_NodeReady(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := &mockEmitter{events: make([]*domain.ObserverEvent, 0)}

	cfg := Config{
		Clientset: clientset,
		Emitter:   emitter,
	}

	observer, err := NewObserver("test-observer", cfg)
	require.NoError(t, err)

	// Create a node that becomes Ready
	oldNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:    corev1.NodeReady,
					Status:  corev1.ConditionFalse,
					Reason:  "KubeletNotReady",
					Message: "Kubelet is not ready",
				},
			},
		},
	}

	newNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:    corev1.NodeReady,
					Status:  corev1.ConditionTrue,
					Reason:  "KubeletReady",
					Message: "Kubelet is ready",
				},
			},
		},
	}

	// Process node change
	ctx := context.Background()
	observer.handleNode(ctx, oldNode, newNode)

	// Verify event was emitted
	require.Len(t, emitter.events, 1)
	event := emitter.events[0]
	assert.Equal(t, "node", event.Type)
	assert.Equal(t, "node_ready", event.Subtype)
	require.NotNil(t, event.NodeData)
	assert.Equal(t, "test-node", event.NodeData.NodeName)
	assert.Equal(t, "Ready", event.NodeData.Condition)
	assert.Equal(t, "True", event.NodeData.Status)
	assert.Equal(t, "KubeletReady", event.NodeData.Reason)
}

// TestHandleNode_NodeNotReady verifies NodeNotReady event emission
func TestHandleNode_NodeNotReady(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := &mockEmitter{events: make([]*domain.ObserverEvent, 0)}

	cfg := Config{
		Clientset: clientset,
		Emitter:   emitter,
	}

	observer, err := NewObserver("test-observer", cfg)
	require.NoError(t, err)

	// Create a node that becomes NotReady
	oldNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	newNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:    corev1.NodeReady,
					Status:  corev1.ConditionFalse,
					Reason:  "NetworkUnavailable",
					Message: "Node network is not available",
				},
			},
		},
	}

	// Process node change
	ctx := context.Background()
	observer.handleNode(ctx, oldNode, newNode)

	// Verify event was emitted
	require.Len(t, emitter.events, 1)
	event := emitter.events[0]
	assert.Equal(t, "node", event.Type)
	assert.Equal(t, "node_not_ready", event.Subtype)
	require.NotNil(t, event.NodeData)
	assert.Equal(t, "test-node", event.NodeData.NodeName)
	assert.Equal(t, "Ready", event.NodeData.Condition)
	assert.Equal(t, "False", event.NodeData.Status)
}

// TDD Cycle 4: Pressure detection (Memory/Disk/PID)

// TestHandleNode_MemoryPressure verifies MemoryPressure event emission
func TestHandleNode_MemoryPressure(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := &mockEmitter{events: make([]*domain.ObserverEvent, 0)}

	cfg := Config{
		Clientset: clientset,
		Emitter:   emitter,
	}

	observer, err := NewObserver("test-observer", cfg)
	require.NoError(t, err)

	// Node with memory pressure
	oldNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionFalse},
			},
		},
	}

	newNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:    corev1.NodeMemoryPressure,
					Status:  corev1.ConditionTrue,
					Reason:  "KubeletHasInsufficientMemory",
					Message: "kubelet has insufficient memory available",
				},
			},
		},
	}

	ctx := context.Background()
	observer.handleNode(ctx, oldNode, newNode)

	require.Len(t, emitter.events, 1)
	event := emitter.events[0]
	assert.Equal(t, "node", event.Type)
	assert.Equal(t, "node_memory_pressure", event.Subtype)
	require.NotNil(t, event.NodeData)
	assert.Equal(t, "MemoryPressure", event.NodeData.Condition)
	assert.Equal(t, "True", event.NodeData.Status)
}

// TestHandleNode_DiskPressure verifies DiskPressure event emission
func TestHandleNode_DiskPressure(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := &mockEmitter{events: make([]*domain.ObserverEvent, 0)}

	cfg := Config{
		Clientset: clientset,
		Emitter:   emitter,
	}

	observer, err := NewObserver("test-observer", cfg)
	require.NoError(t, err)

	// Node with disk pressure
	newNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:    corev1.NodeDiskPressure,
					Status:  corev1.ConditionTrue,
					Reason:  "KubeletHasDiskPressure",
					Message: "kubelet has disk pressure",
				},
			},
		},
	}

	ctx := context.Background()
	observer.handleNode(ctx, nil, newNode)

	require.Len(t, emitter.events, 1)
	event := emitter.events[0]
	assert.Equal(t, "node", event.Type)
	assert.Equal(t, "node_disk_pressure", event.Subtype)
	assert.Equal(t, "DiskPressure", event.NodeData.Condition)
}

// TestHandleNode_PIDPressure verifies PIDPressure event emission
func TestHandleNode_PIDPressure(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := &mockEmitter{events: make([]*domain.ObserverEvent, 0)}

	cfg := Config{
		Clientset: clientset,
		Emitter:   emitter,
	}

	observer, err := NewObserver("test-observer", cfg)
	require.NoError(t, err)

	// Node with PID pressure
	newNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:    corev1.NodePIDPressure,
					Status:  corev1.ConditionTrue,
					Reason:  "KubeletHasPIDPressure",
					Message: "kubelet has insufficient PIDs available",
				},
			},
		},
	}

	ctx := context.Background()
	observer.handleNode(ctx, nil, newNode)

	require.Len(t, emitter.events, 1)
	event := emitter.events[0]
	assert.Equal(t, "node", event.Type)
	assert.Equal(t, "node_pid_pressure", event.Subtype)
	assert.Equal(t, "PIDPressure", event.NodeData.Condition)
}

// Mock emitter for testing
type mockEmitter struct {
	events     []*domain.ObserverEvent
	shouldFail bool
}

func (m *mockEmitter) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	if m.shouldFail {
		return fmt.Errorf("mock emit error")
	}
	m.events = append(m.events, event)
	return nil
}
