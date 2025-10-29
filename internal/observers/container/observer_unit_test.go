package container

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
