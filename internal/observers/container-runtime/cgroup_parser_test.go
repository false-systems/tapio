package containerruntime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestExtractContainerIDFromCgroup(t *testing.T) {
	tests := []struct {
		name       string
		cgroupPath string
		want       string
	}{
		{
			name:       "Docker path",
			cgroupPath: "/docker/abcdef123456789abcdef123456789abcdef123456789abcdef123456789",
			want:       "abcdef123456", // First 12 chars
		},
		{
			name:       "Containerd path",
			cgroupPath: "/containerd/abcdef123456789abcdef123456789abcdef123456789abcdef123456789",
			want:       "abcdef123456",
		},
		{
			name:       "CRI-O path",
			cgroupPath: "/crio/fedcba987654321fedcba987654321fedcba987654321fedcba987654321",
			want:       "fedcba987654",
		},
		{
			name:       "Kubernetes pods path",
			cgroupPath: "/kubepods/burstable/pod12345678-1234-5678-9012-123456789012/abcdef123456789abcdef123456789abcdef123456789abcdef123456789",
			want:       "abcdef123456",
		},
		{
			name:       "Systemd service path",
			cgroupPath: "/system.slice/docker-container.service",
			want:       "docker-conta", // First 12 chars of service
		},
		{
			name:       "Complex Kubernetes path",
			cgroupPath: "/kubepods-burstable/pod12345678-1234-5678-9012-123456789012/fedcba987654321fedcba987654321fedcba987654321fedcba987654321",
			want:       "fedcba987654",
		},
		{
			name:       "Unknown path with hex",
			cgroupPath: "/some/other/path/1234567890ab1234567890ab",
			want:       "1234567890ab",
		},
		{
			name:       "Invalid path",
			cgroupPath: "/invalid/path",
			want:       "",
		},
		{
			name:       "Empty path",
			cgroupPath: "",
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractContainerIDFromCgroup(tt.cgroupPath)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractPodUIDFromCgroup(t *testing.T) {
	tests := []struct {
		name       string
		cgroupPath string
		want       string
	}{
		{
			name:       "Kubernetes pod UID in path",
			cgroupPath: "/kubepods/burstable/pod12345678-1234-5678-9012-123456789012/container",
			want:       "12345678-1234-5678-9012-123456789012",
		},
		{
			name:       "Pod UID with underscore",
			cgroupPath: "/kubepods/pod_abcdef12-3456-7890-abcd-ef1234567890/container",
			want:       "abcdef12-3456-7890-abcd-ef1234567890",
		},
		{
			name:       "No pod UID",
			cgroupPath: "/docker/container123",
			want:       "",
		},
		{
			name:       "Empty path",
			cgroupPath: "",
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractPodUIDFromCgroup(tt.cgroupPath)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetContainerRuntime(t *testing.T) {
	tests := []struct {
		name       string
		cgroupPath string
		want       string
	}{
		{
			name:       "Docker runtime",
			cgroupPath: "/docker/abcdef123456",
			want:       "docker",
		},
		{
			name:       "Containerd runtime",
			cgroupPath: "/containerd/abcdef123456",
			want:       "containerd",
		},
		{
			name:       "CRI-O runtime",
			cgroupPath: "/crio/abcdef123456",
			want:       "crio",
		},
		{
			name:       "Unknown runtime",
			cgroupPath: "/unknown/path",
			want:       "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetContainerRuntime(tt.cgroupPath)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractContainerIDFromPID(t *testing.T) {
	// Create a temporary cgroup file for testing
	tmpDir := t.TempDir()
	procDir := filepath.Join(tmpDir, "proc", "1234")
	require.NoError(t, os.MkdirAll(procDir, 0755))

	cgroupFile := filepath.Join(procDir, "cgroup")
	cgroupContent := `12:memory:/docker/abcdef123456789abcdef123456789abcdef123456789abcdef123456789
11:cpu:/docker/abcdef123456789abcdef123456789abcdef123456789abcdef123456789
`
	require.NoError(t, os.WriteFile(cgroupFile, []byte(cgroupContent), 0644))

	// Mock the /proc path for testing (would need refactoring to make testable)
	// For now, we'll test the error case
	t.Run("PID not found", func(t *testing.T) {
		_, err := ExtractContainerIDFromPID(99999)
		assert.Error(t, err)
	})
}

func TestIsHexString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"Valid hex", "abcdef123456", true},
		{"Uppercase hex", "ABCDEF123456", true},
		{"Mixed case hex", "AbCdEf123456", true},
		{"Invalid chars", "ghijkl123456", false},
		{"Special chars", "abcd-ef12-3456", false},
		{"Empty string", "", true}, // Empty string is technically valid hex
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isHexString(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestEnrichEventWithContainerInfo tests enriching events with container metadata
func TestEnrichEventWithContainerInfo(t *testing.T) {
	// This requires an Observer instance, so we test the basic case
	logger, _ := zap.NewProduction()
	config := NewDefaultConfig("test")
	observer, err := NewObserver("test", config)
	require.NoError(t, err)
	observer.logger = logger

	t.Run("Invalid PID", func(t *testing.T) {
		// PID 0 is invalid
		metadata, err := observer.EnrichEventWithContainerInfo(0)
		assert.Error(t, err)
		assert.Nil(t, metadata)
	})

	t.Run("Non-existent PID", func(t *testing.T) {
		// Very high PID unlikely to exist
		metadata, err := observer.EnrichEventWithContainerInfo(999999)
		assert.Error(t, err)
		assert.Nil(t, metadata)
	})
}

// TestGetMemoryLimitFromCgroup tests memory limit extraction
func TestGetMemoryLimitFromCgroup(t *testing.T) {
	logger, _ := zap.NewProduction()
	config := NewDefaultConfig("test")
	observer, err := NewObserver("test", config)
	require.NoError(t, err)
	observer.logger = logger

	t.Run("Invalid PID", func(t *testing.T) {
		limit := observer.getMemoryLimitFromCgroup(0)
		assert.Equal(t, uint64(0), limit)
	})

	t.Run("Non-existent PID", func(t *testing.T) {
		limit := observer.getMemoryLimitFromCgroup(999999)
		assert.Equal(t, uint64(0), limit)
	})
}
