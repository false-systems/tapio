//go:build linux

package node

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/pkg/intelligence"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TDD Cycle 1: Observer constructor + validation

// TestNew_Success verifies constructor with valid config
func TestNew_Success(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := intelligence.NewMock()
	deps := base.NewDeps(nil, emitter)

	cfg := Config{
		Clientset: clientset,
	}

	observer, err := New(cfg, deps)
	require.NoError(t, err)
	require.NotNil(t, observer)
	assert.Equal(t, "node", observer.name)
}

// TestNew_MissingClientset verifies error when clientset is nil
func TestNew_MissingClientset(t *testing.T) {
	emitter := intelligence.NewMock()
	deps := base.NewDeps(nil, emitter)

	cfg := Config{
		Clientset: nil,
	}

	observer, err := New(cfg, deps)
	require.Error(t, err)
	assert.Nil(t, observer)
	assert.Contains(t, err.Error(), "clientset is required")
}

// TDD Cycle 2: Run lifecycle + event handlers

// TestObserver_Run verifies observer lifecycle
func TestObserver_Run(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := intelligence.NewMock()
	deps := base.NewDeps(nil, emitter)

	cfg := Config{
		Clientset: clientset,
	}

	observer, err := New(cfg, deps)
	require.NoError(t, err)

	// Run should block until context is cancelled
	ctx, cancel := context.WithCancel(context.Background())

	// Run in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- observer.Run(ctx)
	}()

	// Cancel context
	cancel()

	// Run should return without error
	err = <-errCh
	assert.NoError(t, err)
}

// TestObserver_RunRegistersHandlers verifies event handlers are registered
func TestObserver_RunRegistersHandlers(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := intelligence.NewMock()
	deps := base.NewDeps(nil, emitter)

	cfg := Config{
		Clientset: clientset,
	}

	observer, err := New(cfg, deps)
	require.NoError(t, err)

	// Verify informer has handlers registered (non-zero handler count)
	// Note: K8s fake client doesn't actually run informers, so this just
	// verifies constructor doesn't panic and completes successfully
	assert.NotNil(t, observer.informer)
}

// TDD Cycle 3: NodeReady detection

// TestHandleNode_NodeReady verifies NodeReady event emission
func TestHandleNode_NodeReady(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := intelligence.NewMock()
	deps := base.NewDeps(nil, emitter)

	cfg := Config{
		Clientset: clientset,
	}

	observer, err := New(cfg, deps)
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
	require.Len(t, emitter.Events(), 1)
	event := emitter.Events()[0]
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
	emitter := intelligence.NewMock()
	deps := base.NewDeps(nil, emitter)

	cfg := Config{
		Clientset: clientset,
	}

	observer, err := New(cfg, deps)
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
	require.Len(t, emitter.Events(), 1)
	event := emitter.Events()[0]
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
	emitter := intelligence.NewMock()
	deps := base.NewDeps(nil, emitter)

	cfg := Config{
		Clientset: clientset,
	}

	observer, err := New(cfg, deps)
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

	require.Len(t, emitter.Events(), 1)
	event := emitter.Events()[0]
	assert.Equal(t, "node", event.Type)
	assert.Equal(t, "node_memory_pressure", event.Subtype)
	require.NotNil(t, event.NodeData)
	assert.Equal(t, "MemoryPressure", event.NodeData.Condition)
	assert.Equal(t, "True", event.NodeData.Status)
}

// TestHandleNode_DiskPressure verifies DiskPressure event emission
func TestHandleNode_DiskPressure(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := intelligence.NewMock()
	deps := base.NewDeps(nil, emitter)

	cfg := Config{
		Clientset: clientset,
	}

	observer, err := New(cfg, deps)
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

	require.Len(t, emitter.Events(), 1)
	event := emitter.Events()[0]
	assert.Equal(t, "node", event.Type)
	assert.Equal(t, "node_disk_pressure", event.Subtype)
	assert.Equal(t, "DiskPressure", event.NodeData.Condition)
}

// TestHandleNode_PIDPressure verifies PIDPressure event emission
func TestHandleNode_PIDPressure(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := intelligence.NewMock()
	deps := base.NewDeps(nil, emitter)

	cfg := Config{
		Clientset: clientset,
	}

	observer, err := New(cfg, deps)
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

	require.Len(t, emitter.Events(), 1)
	event := emitter.Events()[0]
	assert.Equal(t, "node", event.Type)
	assert.Equal(t, "node_pid_pressure", event.Subtype)
	assert.Equal(t, "PIDPressure", event.NodeData.Condition)
}

// TDD Cycle 5: Resource tracking (capacity + allocations)

// TestCreateNodeEvent_IncludesResources verifies resource capacity and allocation tracking
func TestCreateNodeEvent_IncludesResources(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := intelligence.NewMock()
	deps := base.NewDeps(nil, emitter)

	cfg := Config{
		Clientset: clientset,
	}

	observer, err := New(cfg, deps)
	require.NoError(t, err)

	// Node with resource capacity
	newNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    *resource.NewMilliQuantity(4000, resource.DecimalSI),        // 4 CPUs
				corev1.ResourceMemory: *resource.NewQuantity(16*1024*1024*1024, resource.BinarySI), // 16GB
				corev1.ResourcePods:   *resource.NewQuantity(110, resource.DecimalSI),              // 110 pods
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    *resource.NewMilliQuantity(3800, resource.DecimalSI),        // 3.8 CPUs
				corev1.ResourceMemory: *resource.NewQuantity(15*1024*1024*1024, resource.BinarySI), // 15GB
				corev1.ResourcePods:   *resource.NewQuantity(110, resource.DecimalSI),              // 110 pods
			},
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	ctx := context.Background()
	observer.handleNode(ctx, nil, newNode)

	require.Len(t, emitter.Events(), 1)
	event := emitter.Events()[0]
	require.NotNil(t, event.NodeData)

	// Verify capacity
	assert.Equal(t, int64(4000), event.NodeData.CPUCapacity)                 // 4000 milliCPU
	assert.Equal(t, int64(16*1024*1024*1024), event.NodeData.MemoryCapacity) // 16GB
	assert.Equal(t, int64(110), event.NodeData.PodCapacity)                  // 110 pods
}
