//go:build linux
// +build linux

package container

import "strings"

// NullTerminatedString converts C-style null-terminated byte array to Go string
func NullTerminatedString(b []byte) string {
	n := 0
	for n < len(b) && b[n] != 0 {
		n++
	}
	return string(b[:n])
}

// ParseContainerID extracts container ID from cgroup path
// Strips common prefixes: cri-containerd-, docker-, containerd-
func ParseContainerID(cgroupPath string) string {
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
