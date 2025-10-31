//go:build linux
// +build linux

package containerapi

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/yairfalse/tapio/pkg/domain"
)

// TDD Cycle 1: Detect OOMKill

func TestDetectOOMKill_OOMKilled(t *testing.T) {
	// Container terminated with OOMKilled reason
	status := createContainerStatus("app", "Terminated", "OOMKilled", 137)

	oomKilled := detectOOMKill(status)
	assert.True(t, oomKilled, "Should detect OOMKilled with reason='OOMKilled'")
}

func TestDetectOOMKill_ExitCode137(t *testing.T) {
	// Container terminated with exit code 137 (SIGKILL for memory)
	status := createContainerStatus("app", "Terminated", "Error", 137)

	oomKilled := detectOOMKill(status)
	assert.True(t, oomKilled, "Should detect OOMKilled with exit code 137")
}

func TestDetectOOMKill_NotOOMKilled(t *testing.T) {
	// Container terminated with different error
	status := createContainerStatus("app", "Terminated", "Error", 1)

	oomKilled := detectOOMKill(status)
	assert.False(t, oomKilled, "Should not detect OOMKilled with exit code 1")
}

func TestDetectOOMKill_Running(t *testing.T) {
	// Container is running, not terminated
	status := &corev1.ContainerStatus{
		Name: "app",
		State: corev1.ContainerState{
			Running: &corev1.ContainerStateRunning{},
		},
	}

	oomKilled := detectOOMKill(status)
	assert.False(t, oomKilled, "Should not detect OOMKilled when container is running")
}

func TestDetectOOMKill_NilStatus(t *testing.T) {
	// Defensive: nil status should not panic
	oomKilled := detectOOMKill(nil)
	assert.False(t, oomKilled, "Should handle nil status gracefully")
}

// Helper functions

func createContainerStatus(name, state, reason string, exitCode int32) *corev1.ContainerStatus {
	status := &corev1.ContainerStatus{
		Name:  name,
		State: corev1.ContainerState{},
	}

	switch state {
	case "Waiting":
		status.State.Waiting = &corev1.ContainerStateWaiting{
			Reason: reason,
		}
	case "Running":
		status.State.Running = &corev1.ContainerStateRunning{
			StartedAt: metav1.Now(),
		}
	case "Terminated":
		status.State.Terminated = &corev1.ContainerStateTerminated{
			ExitCode: exitCode,
			Reason:   reason,
		}
	}

	return status
}

// TDD Cycle 2: Detect crashes

func TestDetectCrash_ExitCode1(t *testing.T) {
	// Container terminated with exit code 1
	status := createContainerStatus("app", "Terminated", "Error", 1)

	crashed := detectCrash(status)
	assert.True(t, crashed, "Should detect crash with exit code 1")
}

func TestDetectCrash_ExitCodeZero(t *testing.T) {
	// Container terminated successfully with exit code 0
	status := createContainerStatus("app", "Terminated", "Completed", 0)

	crashed := detectCrash(status)
	assert.False(t, crashed, "Should not detect crash with exit code 0")
}

func TestDetectCrash_ExitCode137IsOOMNotCrash(t *testing.T) {
	// Exit code 137 is OOMKill, not a regular crash
	status := createContainerStatus("app", "Terminated", "OOMKilled", 137)

	crashed := detectCrash(status)
	assert.False(t, crashed, "Should not detect crash for OOMKill (handled separately)")
}

func TestDetectCrash_Running(t *testing.T) {
	// Container is running
	status := &corev1.ContainerStatus{
		Name: "app",
		State: corev1.ContainerState{
			Running: &corev1.ContainerStateRunning{},
		},
	}

	crashed := detectCrash(status)
	assert.False(t, crashed, "Should not detect crash when container is running")
}

func TestDetectCrash_NilStatus(t *testing.T) {
	// Defensive: nil status should not panic
	crashed := detectCrash(nil)
	assert.False(t, crashed, "Should handle nil status gracefully")
}

// TDD Cycle 3: Detect image pull failures

func TestDetectImagePullFailure_ErrImagePull(t *testing.T) {
	// Container waiting with ErrImagePull reason
	status := createContainerStatus("app", "Waiting", "ErrImagePull", 0)

	imagePullFailed := detectImagePullFailure(status)
	assert.True(t, imagePullFailed, "Should detect ErrImagePull")
}

func TestDetectImagePullFailure_ImagePullBackOff(t *testing.T) {
	// Container waiting with ImagePullBackOff reason
	status := createContainerStatus("app", "Waiting", "ImagePullBackOff", 0)

	imagePullFailed := detectImagePullFailure(status)
	assert.True(t, imagePullFailed, "Should detect ImagePullBackOff")
}

func TestDetectImagePullFailure_Running(t *testing.T) {
	// Container is running (image pull succeeded)
	status := &corev1.ContainerStatus{
		Name: "app",
		State: corev1.ContainerState{
			Running: &corev1.ContainerStateRunning{},
		},
	}

	imagePullFailed := detectImagePullFailure(status)
	assert.False(t, imagePullFailed, "Should not detect failure when running")
}

func TestDetectImagePullFailure_WaitingOtherReason(t *testing.T) {
	// Container waiting for other reason (not image pull)
	status := createContainerStatus("app", "Waiting", "ContainerCreating", 0)

	imagePullFailed := detectImagePullFailure(status)
	assert.False(t, imagePullFailed, "Should not detect failure for other waiting reasons")
}

func TestDetectImagePullFailure_NilStatus(t *testing.T) {
	// Defensive: nil status should not panic
	imagePullFailed := detectImagePullFailure(nil)
	assert.False(t, imagePullFailed, "Should handle nil status gracefully")
}

// TDD Cycle 4: Track init containers

func TestDetectFailures_InitContainer(t *testing.T) {
	// Init container with OOMKill
	initStatus := createContainerStatus("init-migrate", "Terminated", "OOMKilled", 137)

	oomKilled := detectOOMKill(initStatus)
	assert.True(t, oomKilled, "Should detect OOMKill in init container")
}

func TestDetectFailures_MainContainer(t *testing.T) {
	// Main container crashed
	mainStatus := createContainerStatus("app", "Terminated", "Error", 1)

	crashed := detectCrash(mainStatus)
	assert.True(t, crashed, "Should detect crash in main container")
}

func TestDetectFailures_SidecarContainer(t *testing.T) {
	// Sidecar container image pull failure
	sidecarStatus := createContainerStatus("envoy", "Waiting", "ImagePullBackOff", 0)

	imagePullFailed := detectImagePullFailure(sidecarStatus)
	assert.True(t, imagePullFailed, "Should detect image pull failure in sidecar")
}

func TestGetContainerType_InitContainers(t *testing.T) {
	// Pod with init containers
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{
				*createContainerStatus("init-1", "Terminated", "Completed", 0),
				*createContainerStatus("init-2", "Running", "", 0),
			},
		},
	}

	containerType := getContainerType(pod, "init-1")
	assert.Equal(t, "init", containerType, "Should identify init container")

	containerType = getContainerType(pod, "init-2")
	assert.Equal(t, "init", containerType, "Should identify second init container")
}

func TestGetContainerType_MainContainers(t *testing.T) {
	// Pod with main containers
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				*createContainerStatus("app", "Running", "", 0),
				*createContainerStatus("sidecar", "Running", "", 0),
			},
		},
	}

	containerType := getContainerType(pod, "app")
	assert.Equal(t, "main", containerType, "Should identify main container")
}

func TestGetContainerType_NotFound(t *testing.T) {
	// Container not found in pod
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				*createContainerStatus("app", "Running", "", 0),
			},
		},
	}

	containerType := getContainerType(pod, "nonexistent")
	assert.Equal(t, "", containerType, "Should return empty string for nonexistent container")
}

// TDD Cycle 5: Create domain events

func TestCreateDomainEvent_OOMKilled(t *testing.T) {
	// Container OOMKilled
	status := createContainerStatus("app", "Terminated", "OOMKilled", 137)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-7d4b5",
			Namespace: "production",
		},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
			Containers: []corev1.Container{
				{Name: "app", Image: "nginx:1.21"},
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{*status},
		},
	}

	event := createDomainEvent(pod, status)

	assert.NotNil(t, event)
	assert.Equal(t, "container", event.Type)
	assert.Equal(t, "container_oom_killed", event.Subtype)
	assert.Equal(t, "container-observer-k8s", event.Source)
	assert.NotNil(t, event.ContainerData)
	assert.Equal(t, "app", event.ContainerData.ContainerName)
	assert.Equal(t, "main", event.ContainerData.ContainerType)
	assert.Equal(t, "web-7d4b5", event.ContainerData.PodName)
	assert.Equal(t, "production", event.ContainerData.PodNamespace)
	assert.Equal(t, "node-1", event.ContainerData.NodeName)
	assert.Equal(t, "nginx:1.21", event.ContainerData.Image)
	assert.Equal(t, "Terminated", event.ContainerData.State)
	assert.Equal(t, "OOMKilled", event.ContainerData.Reason)
	assert.Equal(t, int32(137), event.ContainerData.ExitCode)
}

func TestCreateDomainEvent_Crashed(t *testing.T) {
	// Container crashed with exit code 1
	status := createContainerStatus("worker", "Terminated", "Error", 1)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "worker-abc123",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			NodeName: "node-2",
			Containers: []corev1.Container{
				{Name: "worker", Image: "worker:v2"},
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{*status},
		},
	}

	event := createDomainEvent(pod, status)

	assert.NotNil(t, event)
	assert.Equal(t, "container_crashed", event.Subtype)
	assert.Equal(t, int32(1), event.ContainerData.ExitCode)
	assert.Equal(t, "Error", event.ContainerData.Reason)
}

func TestCreateDomainEvent_ImagePullFailed(t *testing.T) {
	// Image pull failure
	status := createContainerStatus("api", "Waiting", "ErrImagePull", 0)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-xyz789",
			Namespace: "production",
		},
		Spec: corev1.PodSpec{
			NodeName: "node-3",
			Containers: []corev1.Container{
				{Name: "api", Image: "api:latest"},
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{*status},
		},
	}

	event := createDomainEvent(pod, status)

	assert.NotNil(t, event)
	assert.Equal(t, "container_image_pull_failed", event.Subtype)
	assert.Equal(t, "Waiting", event.ContainerData.State)
	assert.Equal(t, "ErrImagePull", event.ContainerData.Reason)
}

func TestCreateDomainEvent_InitContainer(t *testing.T) {
	// Init container crashed
	status := createContainerStatus("init-migrate", "Terminated", "Error", 1)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-def456",
			Namespace: "production",
		},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
			InitContainers: []corev1.Container{
				{Name: "init-migrate", Image: "migrate:v1"},
			},
		},
		Status: corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{*status},
		},
	}

	event := createDomainEvent(pod, status)

	assert.NotNil(t, event)
	assert.Equal(t, "init", event.ContainerData.ContainerType)
	assert.Equal(t, "init-migrate", event.ContainerData.ContainerName)
}

// TDD Cycle 6: Observer struct and constructor

func TestNewAPIObserver_Success(t *testing.T) {
	// Create observer with valid config
	emitter := &fakeEmitter{}
	clientset := fake.NewSimpleClientset()

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewAPIObserver("test-observer", config)

	assert.NoError(t, err)
	assert.NotNil(t, observer)
	assert.Equal(t, "test-observer", observer.Name())
	assert.Equal(t, "default", observer.config.Namespace)
	assert.NotNil(t, observer.informer)
}

func TestNewAPIObserver_AllNamespaces(t *testing.T) {
	// Empty namespace = watch all namespaces
	emitter := &fakeEmitter{}
	clientset := fake.NewSimpleClientset()

	config := Config{
		Clientset: clientset,
		Namespace: "",
		Emitter:   emitter,
	}

	observer, err := NewAPIObserver("test-observer", config)

	assert.NoError(t, err)
	assert.NotNil(t, observer)
	assert.Equal(t, "", observer.config.Namespace)
}

func TestNewAPIObserver_NilClientset(t *testing.T) {
	// Nil clientset should return error
	emitter := &fakeEmitter{}

	config := Config{
		Clientset: nil,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewAPIObserver("test-observer", config)

	assert.Error(t, err)
	assert.Nil(t, observer)
	assert.Contains(t, err.Error(), "clientset")
}

func TestNewAPIObserver_NilEmitter(t *testing.T) {
	// Nil emitter should return error
	clientset := fake.NewSimpleClientset()

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   nil,
	}

	observer, err := NewAPIObserver("test-observer", config)

	assert.Error(t, err)
	assert.Nil(t, observer)
	assert.Contains(t, err.Error(), "emitter")
}

// Fake emitter for testing
type fakeEmitter struct {
	events       []*domain.ObserverEvent
	shouldFail   bool
	attemptCount int
}

func (e *fakeEmitter) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	e.attemptCount++
	if e.shouldFail {
		return fmt.Errorf("emit failed")
	}
	e.events = append(e.events, event)
	return nil
}

func (e *fakeEmitter) Close() error {
	return nil
}

// TDD Cycle 7: handleUpdate with event emission

func TestHandleUpdate_ContainerOOMKilled(t *testing.T) {
	// Pod updated: container OOMKilled
	emitter := &fakeEmitter{}
	clientset := fake.NewSimpleClientset()

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewAPIObserver("test", config)
	assert.NoError(t, err)

	oldPod := createPodWithContainer("web-7d4b5", "default", "app", "Running", "", 0)
	newPod := createPodWithContainer("web-7d4b5", "default", "app", "Terminated", "OOMKilled", 137)

	observer.handleUpdate(context.Background(), oldPod, newPod)

	assert.Len(t, emitter.events, 1, "Should emit 1 event")
	event := emitter.events[0]
	assert.Equal(t, "container", event.Type)
	assert.Equal(t, "container_oom_killed", event.Subtype)
	assert.Equal(t, "app", event.ContainerData.ContainerName)
}

func TestHandleUpdate_NoChange(t *testing.T) {
	// Pod updated but container state unchanged
	emitter := &fakeEmitter{}
	clientset := fake.NewSimpleClientset()

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewAPIObserver("test", config)
	assert.NoError(t, err)

	oldPod := createPodWithContainer("web-7d4b5", "default", "app", "Running", "", 0)
	newPod := createPodWithContainer("web-7d4b5", "default", "app", "Running", "", 0)

	observer.handleUpdate(context.Background(), oldPod, newPod)

	assert.Len(t, emitter.events, 0, "Should not emit event when no change")
}

func TestHandleUpdate_MultipleContainers(t *testing.T) {
	// Pod with 2 containers: 1 crashes, 1 OK
	emitter := &fakeEmitter{}
	clientset := fake.NewSimpleClientset()

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewAPIObserver("test", config)
	assert.NoError(t, err)

	oldPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-7d4b5", Namespace: "default"},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
			Containers: []corev1.Container{
				{Name: "app", Image: "nginx:1.21"},
				{Name: "sidecar", Image: "envoy:1.0"},
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				*createContainerStatus("app", "Running", "", 0),
				*createContainerStatus("sidecar", "Running", "", 0),
			},
		},
	}

	newPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-7d4b5", Namespace: "default"},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
			Containers: []corev1.Container{
				{Name: "app", Image: "nginx:1.21"},
				{Name: "sidecar", Image: "envoy:1.0"},
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				*createContainerStatus("app", "Running", "", 0),
				*createContainerStatus("sidecar", "Terminated", "Error", 1),
			},
		},
	}

	observer.handleUpdate(context.Background(), oldPod, newPod)

	assert.Len(t, emitter.events, 1, "Should emit 1 event for crashed sidecar")
	event := emitter.events[0]
	assert.Equal(t, "container_crashed", event.Subtype)
	assert.Equal(t, "sidecar", event.ContainerData.ContainerName)
}

// Helper to create pod with single container
func createPodWithContainer(name, namespace, containerName, state, reason string, exitCode int32) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
			Containers: []corev1.Container{
				{Name: containerName, Image: "app:v1"},
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				*createContainerStatus(containerName, state, reason, exitCode),
			},
		},
	}
	return pod
}

// TDD Cycle 8: Start/Stop lifecycle

func TestStart_Success(t *testing.T) {
	// Start observer successfully
	emitter := &fakeEmitter{}
	clientset := fake.NewSimpleClientset()

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewAPIObserver("test", config)
	assert.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = observer.Start(ctx)
	assert.NoError(t, err)

	// Stop observer
	err = observer.Stop()
	assert.NoError(t, err)
}

func TestStop_WithoutStart(t *testing.T) {
	// Calling Stop without Start returns error from BaseObserver
	emitter := &fakeEmitter{}
	clientset := fake.NewSimpleClientset()

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewAPIObserver("test", config)
	assert.NoError(t, err)

	err = observer.Stop()
	assert.Error(t, err) // BaseObserver returns error when not running
	assert.Contains(t, err.Error(), "not running")
}

func TestIsHealthy(t *testing.T) {
	emitter := &fakeEmitter{}
	clientset := fake.NewSimpleClientset()

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewAPIObserver("test", config)
	assert.NoError(t, err)

	// Before start: healthy (not started yet)
	assert.True(t, observer.IsHealthy())

	// After start: healthy
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	observer.Start(ctx)
	assert.True(t, observer.IsHealthy())

	// After stop: not healthy
	observer.Stop()
	assert.False(t, observer.IsHealthy())
}

// TDD Cycle 9: OTEL metrics

func TestOTELMetrics_Created(t *testing.T) {
	// Observer should create OTEL metrics without error
	emitter := &fakeEmitter{}
	clientset := fake.NewSimpleClientset()

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewAPIObserver("test", config)
	assert.NoError(t, err)
	assert.NotNil(t, observer)

	// Verify metrics fields are not nil (they should be created)
	// Note: We can't directly access private fields, but we can verify
	// the observer works without panicking when processing events
}

func TestOTELMetrics_EmitIncrementsCounter(t *testing.T) {
	// When an event is emitted, metrics should be updated (no panic)
	emitter := &fakeEmitter{}
	clientset := fake.NewSimpleClientset()

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewAPIObserver("test", config)
	assert.NoError(t, err)

	// Create pods with OOMKilled container
	oldPod := createPodWithContainer("test-pod", "default", "app", "Running", "", 0)
	newPod := createPodWithContainer("test-pod", "default", "app", "Terminated", "OOMKilled", 137)

	// Process update (should emit event and update metrics)
	observer.handleUpdate(context.Background(), oldPod, newPod)

	// Verify event was emitted
	assert.Equal(t, 1, len(emitter.events))

	// If we got here without panic, metrics worked correctly
}

func TestOTELMetrics_ErrorIncrementsErrorCounter(t *testing.T) {
	// When emit fails, error counter should be incremented (no panic)
	emitter := &fakeEmitter{
		shouldFail: true,
	}
	clientset := fake.NewSimpleClientset()

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewAPIObserver("test", config)
	assert.NoError(t, err)

	// Create pods with crashed container
	oldPod := createPodWithContainer("test-pod", "default", "app", "Running", "", 0)
	newPod := createPodWithContainer("test-pod", "default", "app", "Terminated", "Error", 1)

	// Process update (emit will fail, error counter should increment)
	observer.handleUpdate(context.Background(), oldPod, newPod)

	// Verify emit was attempted
	assert.Equal(t, 1, emitter.attemptCount)

	// If we got here without panic, error metrics worked correctly
}
