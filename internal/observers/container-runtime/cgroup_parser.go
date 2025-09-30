package containerruntime

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	// Container ID patterns for different runtimes
	dockerPattern     = regexp.MustCompile(`/docker/([a-f0-9]{64})`)
	containerdPattern = regexp.MustCompile(`/containerd/([a-f0-9]{64})`)
	crioPattern       = regexp.MustCompile(`/crio/([a-f0-9]{64})`)
	kubepodsPattern   = regexp.MustCompile(`/kubepods(?:-[a-z]+)?/(?:besteffort|burstable|pod[a-f0-9-]+)/pod[a-f0-9-]+/([a-f0-9]{64})`)
	systemdPattern    = regexp.MustCompile(`/system.slice/(.+)\.service`)
)

// ExtractContainerIDFromCgroup extracts container ID from cgroup path
// Supports multiple container runtimes: Docker, containerd, CRI-O, and Kubernetes
func ExtractContainerIDFromCgroup(cgroupPath string) string {
	// Try different patterns in order of likelihood
	patterns := []struct {
		name    string
		pattern *regexp.Regexp
	}{
		{"kubepods", kubepodsPattern},
		{"docker", dockerPattern},
		{"containerd", containerdPattern},
		{"crio", crioPattern},
		{"systemd", systemdPattern},
	}

	for _, p := range patterns {
		if matches := p.pattern.FindStringSubmatch(cgroupPath); len(matches) > 1 {
			// Extract just the container ID (first 12 chars for display)
			containerID := matches[1]
			if len(containerID) > 12 {
				return containerID[:12]
			}
			return containerID
		}
	}

	// If no pattern matches, try to extract from path segments
	parts := strings.Split(cgroupPath, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		part := parts[i]
		// Look for hex strings that could be container IDs
		if len(part) >= 12 && isHexString(part[:12]) {
			return part[:12]
		}
	}

	return ""
}

// ExtractContainerIDFromPID extracts container ID from process's cgroup
func ExtractContainerIDFromPID(pid int) (string, error) {
	cgroupPath := fmt.Sprintf("/proc/%d/cgroup", pid)
	data, err := os.ReadFile(cgroupPath)
	if err != nil {
		return "", fmt.Errorf("failed to read cgroup file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		// Parse cgroup line format: hierarchy-ID:controller-list:cgroup-path
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 3 {
			continue
		}

		cgroupPath := parts[2]
		if containerID := ExtractContainerIDFromCgroup(cgroupPath); containerID != "" {
			return containerID, nil
		}
	}

	return "", fmt.Errorf("no container ID found in cgroup paths")
}

// ExtractPodUIDFromCgroup extracts Kubernetes Pod UID from cgroup path
func ExtractPodUIDFromCgroup(cgroupPath string) string {
	// Pattern: /kubepods/besteffort/pod<POD_UID>/... or /pod_<POD_UID>/...
	podPattern := regexp.MustCompile(`/pod_?([a-f0-9-]{36})`)
	if matches := podPattern.FindStringSubmatch(cgroupPath); len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// GetContainerRuntime determines the container runtime from cgroup path
func GetContainerRuntime(cgroupPath string) string {
	if strings.Contains(cgroupPath, "/docker/") {
		return "docker"
	}
	if strings.Contains(cgroupPath, "/containerd/") {
		return "containerd"
	}
	if strings.Contains(cgroupPath, "/crio/") {
		return "crio"
	}
	if strings.Contains(cgroupPath, "/kubepods/") {
		return "kubernetes"
	}
	if strings.Contains(cgroupPath, "/system.slice/") {
		return "systemd"
	}
	return "unknown"
}

// EnrichEventWithContainerInfo enriches an event with container information from cgroup
func (c *Observer) EnrichEventWithContainerInfo(pid uint32) (*ContainerMetadata, error) {
	containerID, err := ExtractContainerIDFromPID(int(pid))
	if err != nil {
		return nil, err
	}

	// Try to get more info from cgroup
	cgroupPath := fmt.Sprintf("/proc/%d/cgroup", pid)
	data, err := os.ReadFile(cgroupPath)
	if err != nil {
		return &ContainerMetadata{
			ContainerID: containerID,
		}, nil
	}

	// Extract additional metadata
	cgroupContent := string(data)
	podUID := ""
	runtime := "unknown"

	lines := strings.Split(cgroupContent, "\n")
	for _, line := range lines {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 3 {
			continue
		}
		cgroupPath := parts[2]

		if podUID == "" {
			podUID = ExtractPodUIDFromCgroup(cgroupPath)
		}
		if runtime == "unknown" {
			runtime = GetContainerRuntime(cgroupPath)
		}
	}

	// Get memory limit from cgroup if available
	memoryLimit := c.getMemoryLimitFromCgroup(pid)

	return &ContainerMetadata{
		ContainerID: containerID,
		PodUID:      podUID,
		MemoryLimit: memoryLimit,
	}, nil
}

// getMemoryLimitFromCgroup reads memory limit from cgroup v2
func (c *Observer) getMemoryLimitFromCgroup(pid uint32) uint64 {
	// Try cgroup v2 first
	cgroupPath := fmt.Sprintf("/proc/%d/cgroup", pid)
	data, err := os.ReadFile(cgroupPath)
	if err != nil {
		return 0
	}

	// Parse cgroup v2 path (0::/...)
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "0::") {
			cgroupPath := strings.TrimPrefix(line, "0::")
			memLimitPath := filepath.Join("/sys/fs/cgroup", cgroupPath, "memory.max")

			limitData, err := os.ReadFile(memLimitPath)
			if err != nil {
				continue
			}

			limitStr := strings.TrimSpace(string(limitData))
			if limitStr == "max" {
				return 0 // No limit
			}

			var limit uint64
			fmt.Sscanf(limitStr, "%d", &limit)
			return limit
		}
	}

	return 0
}

// isHexString checks if a string contains only hexadecimal characters
func isHexString(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return len(s) > 0
}
