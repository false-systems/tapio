package containerruntime

import (
	"errors"
	"regexp"
	"strings"
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
