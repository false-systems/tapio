package container

import (
	corev1 "k8s.io/api/core/v1"
)

// detectOOMKill returns true if the container was killed due to out-of-memory.
// Checks for:
// - Reason "OOMKilled"
// - Exit code 137 (SIGKILL for memory limits)
func detectOOMKill(status *corev1.ContainerStatus) bool {
	if status == nil {
		return false
	}

	// Check if container is terminated
	if status.State.Terminated == nil {
		return false
	}

	terminated := status.State.Terminated

	// Check explicit OOMKilled reason
	if terminated.Reason == "OOMKilled" {
		return true
	}

	// Check exit code 137 (SIGKILL for memory)
	if terminated.ExitCode == 137 {
		return true
	}

	return false
}
