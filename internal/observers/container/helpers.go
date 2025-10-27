//go:build linux
// +build linux

package container

import "strings"

// nullTerminatedString converts C-style null-terminated byte array to Go string
func nullTerminatedString(b []byte) string {
	n := 0
	for n < len(b) && b[n] != 0 {
		n++
	}
	return string(b[:n])
}

// parseContainerID extracts container ID from cgroup path
// Strips common prefixes: cri-containerd-, docker-, containerd-
func parseContainerID(cgroupPath string) string {
	if cgroupPath == "" {
		return ""
	}

	// Get last path segment
	parts := strings.Split(cgroupPath, "/")
	if len(parts) == 0 {
		return ""
	}
	lastPart := parts[len(parts)-1]

	// Strip common container runtime prefixes
	prefixes := []string{"cri-containerd-", "docker-", "containerd-"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(lastPart, prefix) {
			return strings.TrimPrefix(lastPart, prefix)
		}
	}

	return lastPart
}
