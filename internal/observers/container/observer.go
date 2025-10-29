package container

import (
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"

	"github.com/yairfalse/tapio/pkg/domain"
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

// getContainerType returns the type of container ("init", "main", or "")
// by searching through pod's container status arrays.
func getContainerType(pod *corev1.Pod, containerName string) string {
	if pod == nil {
		return ""
	}

	// Check init containers
	for _, status := range pod.Status.InitContainerStatuses {
		if status.Name == containerName {
			return "init"
		}
	}

	// Check main containers
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == containerName {
			return "main"
		}
	}

	// Check ephemeral containers (future support)
	for _, status := range pod.Status.EphemeralContainerStatuses {
		if status.Name == containerName {
			return "ephemeral"
		}
	}

	// Not found
	return ""
}

// createDomainEvent creates an ObserverEvent from pod and container status.
// Determines event subtype based on failure type (OOMKill, crash, image pull).
func createDomainEvent(pod *corev1.Pod, status *corev1.ContainerStatus) *domain.ObserverEvent {
	if pod == nil || status == nil {
		return nil
	}

	// Determine subtype based on failure type
	subtype := "container_unknown"
	if detectOOMKill(status) {
		subtype = "container_oom_killed"
	} else if detectCrash(status) {
		subtype = "container_crashed"
	} else if detectImagePullFailure(status) {
		subtype = "container_image_pull_failed"
	}

	// Extract container type (init, main, ephemeral)
	containerType := getContainerType(pod, status.Name)

	// Find container spec to get image
	var image string
	for _, c := range pod.Spec.InitContainers {
		if c.Name == status.Name {
			image = c.Image
			break
		}
	}
	if image == "" {
		for _, c := range pod.Spec.Containers {
			if c.Name == status.Name {
				image = c.Image
				break
			}
		}
	}

	// Extract state and details
	state := ""
	reason := ""
	message := ""
	exitCode := int32(0)
	signal := int32(0)

	if status.State.Waiting != nil {
		state = "Waiting"
		reason = status.State.Waiting.Reason
		message = status.State.Waiting.Message
	} else if status.State.Running != nil {
		state = "Running"
	} else if status.State.Terminated != nil {
		state = "Terminated"
		reason = status.State.Terminated.Reason
		message = status.State.Terminated.Message
		exitCode = status.State.Terminated.ExitCode
		signal = status.State.Terminated.Signal
	}

	// Create event
	event := &domain.ObserverEvent{
		ID:        uuid.New().String(),
		Type:      "container",
		Subtype:   subtype,
		Source:    "container-observer",
		Timestamp: time.Now(),
		ContainerData: &domain.ContainerEventData{
			ContainerName: status.Name,
			ContainerType: containerType,
			PodName:       pod.Name,
			PodNamespace:  pod.Namespace,
			NodeName:      pod.Spec.NodeName,
			Image:         image,
			State:         state,
			Reason:        reason,
			Message:       message,
			RestartCount:  status.RestartCount,
			ExitCode:      exitCode,
			Signal:        signal,
		},
	}

	return event
}
