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
