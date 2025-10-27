//go:build linux
// +build linux

package container

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNullTerminatedString_Normal verifies normal C-string conversion
func TestNullTerminatedString_Normal(t *testing.T) {
	buf := []byte{'h', 'e', 'l', 'l', 'o', 0, 0, 0}
	result := nullTerminatedString(buf)
	assert.Equal(t, "hello", result)
}

// TestNullTerminatedString_Empty verifies empty string (starts with null)
func TestNullTerminatedString_Empty(t *testing.T) {
	buf := []byte{0, 0, 0, 0}
	result := nullTerminatedString(buf)
	assert.Equal(t, "", result)
}

// TestNullTerminatedString_Full verifies string without null terminator
func TestNullTerminatedString_Full(t *testing.T) {
	buf := []byte{'a', 'b', 'c', 'd'}
	result := nullTerminatedString(buf)
	assert.Equal(t, "abcd", result)
}

// TestNullTerminatedString_LongPath verifies cgroup path extraction
func TestNullTerminatedString_LongPath(t *testing.T) {
	path := "/sys/fs/cgroup/kubepods/burstable/pod-abc/cri-containerd-123"
	buf := make([]byte, 256)
	copy(buf, []byte(path))

	result := nullTerminatedString(buf)
	assert.Equal(t, path, result)
}

// TestParseContainerID_Containerd verifies containerd cgroup path parsing
func TestParseContainerID_Containerd(t *testing.T) {
	path := "/kubepods/burstable/pod-abc/cri-containerd-1234567890abcdef"
	result := parseContainerID(path)
	assert.Equal(t, "1234567890abcdef", result)
}

// TestParseContainerID_Docker verifies docker cgroup path parsing
func TestParseContainerID_Docker(t *testing.T) {
	path := "/docker/abcd1234efgh5678"
	result := parseContainerID(path)
	assert.Equal(t, "abcd1234efgh5678", result)
}

// TestParseContainerID_ContainerdPrefix verifies containerd- prefix stripping
func TestParseContainerID_ContainerdPrefix(t *testing.T) {
	path := "/containerd-xyz789"
	result := parseContainerID(path)
	assert.Equal(t, "xyz789", result)
}

// TestParseContainerID_NoPrefix verifies raw ID without prefix
func TestParseContainerID_NoPrefix(t *testing.T) {
	path := "/kubepods/plain123456"
	result := parseContainerID(path)
	assert.Equal(t, "plain123456", result)
}

// TestParseContainerID_Empty verifies empty path handling
func TestParseContainerID_Empty(t *testing.T) {
	result := parseContainerID("")
	assert.Equal(t, "", result)
}
