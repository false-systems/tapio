package containerruntime

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseCgroupPath(t *testing.T) {
	tests := []struct {
		name            string
		cgroupPath      string
		wantContainerID string
		wantPodUID      string
		wantErr         bool
	}{
		{
			name:            "Kubernetes cgroup v1 path",
			cgroupPath:      "/kubepods/burstable/pod12345678-1234-5678-9012-123456789012/docker-abcdef123456789",
			wantContainerID: "abcdef123456789",
			wantPodUID:      "12345678-1234-5678-9012-123456789012",
			wantErr:         false,
		},
		{
			name:            "Kubernetes cgroup v2 path",
			cgroupPath:      "/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod12345678-1234-5678-9012-123456789012.slice/docker-abcdef123456.scope",
			wantContainerID: "abcdef123456",
			wantPodUID:      "12345678-1234-5678-9012-123456789012",
			wantErr:         false,
		},
		{
			name:            "Docker only path",
			cgroupPath:      "/docker/abcdef123456789",
			wantContainerID: "abcdef123456789",
			wantPodUID:      "",
			wantErr:         false,
		},
		{
			name:            "Containerd path",
			cgroupPath:      "/system.slice/containerd.service/kubepods-burstable-pod12345678-1234-5678-9012-123456789012.slice/cri-containerd-abcdef123456789.scope",
			wantContainerID: "abcdef123456789",
			wantPodUID:      "12345678-1234-5678-9012-123456789012",
			wantErr:         false,
		},
		{
			name:            "Invalid path",
			cgroupPath:      "/invalid/path",
			wantContainerID: "",
			wantPodUID:      "",
			wantErr:         true,
		},
		{
			name:            "Empty path",
			cgroupPath:      "",
			wantContainerID: "",
			wantPodUID:      "",
			wantErr:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			containerID, podUID, err := parseCgroupPath(tt.cgroupPath)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantContainerID, containerID)
				assert.Equal(t, tt.wantPodUID, podUID)
			}
		})
	}
}

func TestExtractContainerID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Docker ID with prefix",
			input:    "docker-abcdef123456789",
			expected: "abcdef123456789",
		},
		{
			name:     "Containerd ID with prefix",
			input:    "cri-containerd-abcdef123456789",
			expected: "abcdef123456789",
		},
		{
			name:     "ID with .scope suffix",
			input:    "docker-abcdef123456789.scope",
			expected: "abcdef123456789",
		},
		{
			name:     "Plain container ID",
			input:    "abcdef123456789",
			expected: "abcdef123456789",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractContainerID(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractPodUID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Standard pod UID",
			input:    "pod12345678-1234-5678-9012-123456789012",
			expected: "12345678-1234-5678-9012-123456789012",
		},
		{
			name:     "Pod UID in slice",
			input:    "kubepods-burstable-pod12345678-1234-5678-9012-123456789012.slice",
			expected: "12345678-1234-5678-9012-123456789012",
		},
		{
			name:     "Pod UID with underscore",
			input:    "pod_12345678-1234-5678-9012-123456789012",
			expected: "12345678-1234-5678-9012-123456789012",
		},
		{
			name:     "No pod UID",
			input:    "some-other-path",
			expected: "",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractPodUID(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
