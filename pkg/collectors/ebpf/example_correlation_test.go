package ebpf

import (
	"fmt"
	"testing"
	"time"
)

// TestManualCorrelation demonstrates container correlation manually
// Run with: go test -v -run TestManualCorrelation
func TestManualCorrelation(t *testing.T) {
	// This test demonstrates how the correlation would work
	// without requiring actual eBPF (for CI/CD compatibility)

	collector, err := NewCollector("demo")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// In a real scenario:
	// 1. K8s API watcher sees pod creation
	// 2. Container runtime starts container with PID
	// 3. We update correlation maps

	// Example pod creation event from K8s
	podUID := "frontend-pod-abc123"
	podName := "frontend-deployment-7f8b9c-xyz"
	namespace := "production"

	// Example container start event
	containerPID := uint32(12345)
	containerID := "docker://8a3b5c7d9e1f"
	containerImage := "myapp/frontend:v2.1.0"
	cgroupID := uint64(98765) // From /proc/PID/cgroup

	fmt.Printf("\n=== Container Correlation Demo ===\n")
	fmt.Printf("Pod Created: %s in namespace %s\n", podName, namespace)
	fmt.Printf("Container Started: PID=%d, ID=%s\n", containerPID, containerID)
	fmt.Printf("Cgroup ID: %d\n", cgroupID)

	// Update correlations (would be called by K8s watcher)
	if collector.objs != nil { // Only if eBPF is available
		// Update pod info
		err = collector.UpdatePodInfo(cgroupID, podUID, namespace, podName)
		if err == nil {
			fmt.Printf("✓ Updated Pod correlation: Cgroup %d → Pod %s\n", cgroupID, podUID)
		}

		// Update container info
		err = collector.UpdateContainerInfo(containerPID, containerID, podUID, containerImage)
		if err == nil {
			fmt.Printf("✓ Updated Container correlation: PID %d → Container %s\n", containerPID, containerID)
		}
	}

	// Simulate an event with correlation
	fmt.Printf("\n=== Simulated Event ===\n")
	event := KernelEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		PID:       containerPID,
		TID:       containerPID,
		EventType: 1,       // MEMORY_ALLOC
		Size:      1048576, // 1MB allocation
		CgroupID:  cgroupID,
	}
	copy(event.Comm[:], "frontend")
	copy(event.PodUID[:], podUID)

	// Show what metadata would be generated
	metadata := map[string]string{
		"collector": "ebpf",
		"pid":       fmt.Sprintf("%d", event.PID),
		"comm":      "frontend",
		"size":      fmt.Sprintf("%d", event.Size),
		"cgroup_id": fmt.Sprintf("%d", event.CgroupID),
		"pod_uid":   podUID,
		// These would be added by correlation:
		"container_id":    containerID,
		"container_image": containerImage,
		"pod_name":        podName,
		"namespace":       namespace,
	}

	fmt.Printf("Event Type: MEMORY_ALLOC\n")
	fmt.Printf("Process: %s (PID %d)\n", metadata["comm"], event.PID)
	fmt.Printf("Memory Size: %d bytes\n", event.Size)
	fmt.Printf("\nCorrelation Chain:\n")
	fmt.Printf("  PID %d → Container %s (image: %s)\n",
		event.PID, containerID, containerImage)
	fmt.Printf("  Cgroup %d → Pod %s (namespace: %s)\n",
		event.CgroupID, podUID, namespace)

	// Show the narrative this enables
	fmt.Printf("\n=== Narrative ===\n")
	fmt.Printf("Process 'frontend' (PID %d) in container %s\n", event.PID, containerID)
	fmt.Printf("running image %s as part of pod %s\n", containerImage, podName)
	fmt.Printf("in namespace %s allocated %d bytes of memory.\n", namespace, event.Size)

	// Demonstrate lookup
	if collector.objs != nil {
		if containerInfo, err := collector.GetContainerInfo(containerPID); err == nil {
			fmt.Printf("\n✓ Container lookup successful: %+v\n", containerInfo)
		}
		if podInfo, err := collector.GetPodInfo(cgroupID); err == nil {
			fmt.Printf("✓ Pod lookup successful: %+v\n", podInfo)
		}
	}
}

// ExampleCollector_UpdateContainerInfo shows the correlation in action
func ExampleCollector_UpdateContainerInfo() {
	// This would produce output like:
	//
	// Event from eBPF:
	//   Type: MEMORY_ALLOC
	//   PID: 12345
	//   Size: 1048576
	//   Cgroup: 98765
	//
	// After correlation:
	//   Container: nginx-abc123 (nginx:1.20)
	//   Pod: frontend-pod-xyz (namespace: production)
	//
	// Narrative:
	//   "Process nginx (12345) in container nginx-abc123
	//    running in pod frontend-pod-xyz allocated 1MB of memory"

	fmt.Println("See TestManualCorrelation for demonstration")
}
