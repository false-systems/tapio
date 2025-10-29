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

// detectCrash returns true if the container crashed (non-zero exit code).
// Excludes OOMKills (exit code 137) as they are handled separately.
func detectCrash(status *corev1.ContainerStatus) bool {
	if status == nil {
		return false
	}

	// Check if container is terminated
	if status.State.Terminated == nil {
		return false
	}

	terminated := status.State.Terminated

	// Exit code 0 = success, not a crash
	if terminated.ExitCode == 0 {
		return false
	}

	// Exit code 137 = OOMKill, handled separately
	if terminated.ExitCode == 137 {
		return false
	}

	// Any other non-zero exit code = crash
	return true
}

// detectImagePullFailure returns true if the container failed to pull its image.
// Checks for:
// - Reason "ErrImagePull"
// - Reason "ImagePullBackOff"
func detectImagePullFailure(status *corev1.ContainerStatus) bool {
	if status == nil {
		return false
	}

	// Image pull failures occur in Waiting state
	if status.State.Waiting == nil {
		return false
	}

	waiting := status.State.Waiting

	// Check for image pull error reasons
	if waiting.Reason == "ErrImagePull" || waiting.Reason == "ImagePullBackOff" {
		return true
	}

	return false
}
