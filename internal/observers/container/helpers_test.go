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
