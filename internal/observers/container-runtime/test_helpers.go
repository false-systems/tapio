package containerruntime

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

var (
	// ErrInvalidCgroupPath indicates the cgroup path could not be parsed
	ErrInvalidCgroupPath = errors.New("invalid cgroup path")
)

// Test helper functions that need to be implemented for tests

// parseCgroupPath parses a cgroup path to extract container ID and pod UID
func parseCgroupPath(cgroupPath string) (containerID, podUID string, err error) {
	if cgroupPath == "" {
		return "", "", ErrInvalidCgroupPath
	}

	// Extract container ID
	containerID = extractContainerID(cgroupPath)
	if containerID == "" {
		return "", "", ErrInvalidCgroupPath
	}

	// Extract pod UID
	podUID = extractPodUID(cgroupPath)

	return containerID, podUID, nil
}

// extractContainerID extracts container ID from various formats
func extractContainerID(path string) string {
	// Pattern for docker/containerd IDs
	patterns := []string{
		`docker-([a-f0-9]{12,64})`,
		`cri-containerd-([a-f0-9]{12,64})`,
		`([a-f0-9]{12,64})\.scope`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(path); len(matches) > 1 {
			return matches[1]
		}
	}

	// Check if the last segment is a container ID
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		lastPart := parts[len(parts)-1]
		// Remove common prefixes
		lastPart = strings.TrimPrefix(lastPart, "docker-")
		lastPart = strings.TrimPrefix(lastPart, "cri-containerd-")
		lastPart = strings.TrimSuffix(lastPart, ".scope")

		// Check if it looks like a container ID
		if len(lastPart) >= 12 && isHex(lastPart) {
			return lastPart
		}
	}

	return ""
}

// extractPodUID extracts pod UID from cgroup path
func extractPodUID(path string) string {
	// Pattern for pod UIDs
	re := regexp.MustCompile(`pod[_-]?([a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12})`)
	if matches := re.FindStringSubmatch(path); len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// isHex checks if a string contains only hexadecimal characters
func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// BPFContainerExitEvent for testing (simplified version)
type BPFContainerExitEvent struct {
	Timestamp   uint64   // ns since boot
	Pid         uint32   // Process ID
	Ppid        uint32   // Parent Process ID
	ExitCode    int32    // Exit code
	Signal      int32    // Signal that caused exit
	CgroupId    uint64   // Cgroup ID
	ContainerId [64]byte // Container ID string
}

// convertBPFEventToDomain converts a BPF event to domain event
func convertBPFEventToDomain(bpfEvent *BPFContainerExitEvent) *domain.CollectorEvent {
	containerID := string(bpfEvent.ContainerId[:])
	containerID = strings.TrimRight(containerID, "\x00")

	exitCode := int32(bpfEvent.ExitCode)

	return &domain.CollectorEvent{
		EventID:   fmt.Sprintf("bpf-%d", bpfEvent.Pid),
		Timestamp: time.Unix(0, int64(bpfEvent.Timestamp)),
		Type:      domain.EventTypeContainerExit,
		Source:    "container-runtime",
		Severity:  domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			Container: &domain.ContainerData{
				ContainerID: containerID,
				ExitCode:    &exitCode,
			},
			Process: &domain.ProcessData{
				PID:  int32(bpfEvent.Pid),
				PPID: int32(bpfEvent.Ppid),
			},
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer": "container-runtime",
				"version":  "1.0.0",
				"source":   "bpf",
			},
		},
	}
}
