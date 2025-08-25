package storageio

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Test ShouldExcludePath function
func TestShouldExcludePath(t *testing.T) {
	config := &Config{
		ExcludedPaths: []string{"/tmp", "/var/tmp", "/dev"},
	}

	tests := []struct {
		path     string
		expected bool
	}{
		{"/tmp/test-file", true},
		{"/var/tmp/cache", true},
		{"/dev/null", true},
		{"/var/lib/kubelet/pods/test", false},
		{"/etc/kubernetes/admin.conf", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := config.ShouldExcludePath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test ShouldExcludeProcess function
func TestShouldExcludeProcess(t *testing.T) {
	config := &Config{
		ExcludedProcesses: []string{"systemd", "kthreadd", "ksoftirqd"},
	}

	tests := []struct {
		process  string
		expected bool
	}{
		{"systemd", true},
		{"kthreadd", true},
		{"ksoftirqd", true},
		{"postgres", false},
		{"nginx", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.process, func(t *testing.T) {
			result := config.ShouldExcludeProcess(tt.process)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test ShouldIncludeProcess function
func TestShouldIncludeProcess(t *testing.T) {
	tests := []struct {
		name              string
		includedProcesses []string
		process           string
		expected          bool
	}{
		{
			name:              "empty included list - include all",
			includedProcesses: []string{},
			process:           "postgres",
			expected:          true,
		},
		{
			name:              "process in included list",
			includedProcesses: []string{"postgres", "nginx", "mysql"},
			process:           "postgres",
			expected:          true,
		},
		{
			name:              "process not in included list",
			includedProcesses: []string{"postgres", "nginx", "mysql"},
			process:           "unknown",
			expected:          false,
		},
		{
			name:              "empty process name with included list",
			includedProcesses: []string{"postgres", "nginx", "mysql"},
			process:           "",
			expected:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				IncludedProcesses: tt.includedProcesses,
			}
			result := config.ShouldIncludeProcess(tt.process)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test GetEnabledVFSProbes function
func TestGetEnabledVFSProbes(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		expected []VFSProbeType
	}{
		{
			name: "default config - all probes enabled",
			config: &Config{
				EnableVFSRead:       true,
				EnableVFSWrite:      true,
				EnableVFSFsync:      true,
				EnableVFSIterateDir: true,
			},
			expected: []VFSProbeType{
				VFSProbeRead,
				VFSProbeWrite,
				VFSProbeFsync,
				VFSProbeIterateDir,
			},
		},
		{
			name: "only read and write enabled",
			config: &Config{
				EnableVFSRead:       true,
				EnableVFSWrite:      true,
				EnableVFSFsync:      false,
				EnableVFSIterateDir: false,
			},
			expected: []VFSProbeType{
				VFSProbeRead,
				VFSProbeWrite,
			},
		},
		{
			name: "no probes enabled",
			config: &Config{
				EnableVFSRead:       false,
				EnableVFSWrite:      false,
				EnableVFSFsync:      false,
				EnableVFSIterateDir: false,
			},
			expected: []VFSProbeType{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetEnabledVFSProbes()
			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}

// Test config validation edge cases
func TestConfigValidationEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "negative min IO size",
			config: &Config{
				BufferSize:        1000,
				SlowIOThresholdMs: 10,
				SamplingRate:      0.5,
				MinIOSize:         -1, // Invalid
				MonitoredK8sPaths: []string{"/var/lib/kubelet/pods/"},
				EnableVFSRead:     true,
			},
			expectError: true,
			errorMsg:    "min_io_size must be non-negative",
		},
		{
			name: "zero sampling rate should be valid",
			config: &Config{
				BufferSize:        1000,
				SlowIOThresholdMs: 10,
				SamplingRate:      0.0, // Valid - no sampling
				MinIOSize:         0,
				MonitoredK8sPaths: []string{"/var/lib/kubelet/pods/"},
				EnableVFSRead:     true,
			},
			expectError: false,
		},
		{
			name: "very large buffer size should be valid",
			config: &Config{
				BufferSize:        1000000, // Large but valid
				SlowIOThresholdMs: 10,
				SamplingRate:      1.0,
				MinIOSize:         0,
				MonitoredK8sPaths: []string{"/var/lib/kubelet/pods/"},
				EnableVFSRead:     true,
			},
			expectError: false,
		},
		{
			name: "very high slow IO threshold",
			config: &Config{
				BufferSize:        1000,
				SlowIOThresholdMs: 60000, // 1 minute - high but valid
				SamplingRate:      1.0,
				MinIOSize:         0,
				MonitoredK8sPaths: []string{"/var/lib/kubelet/pods/"},
				EnableVFSRead:     true,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Test default config values
func TestDefaultConfigValues(t *testing.T) {
	config := NewDefaultConfig()

	// Test core values
	assert.Equal(t, 10000, config.BufferSize)
	assert.Equal(t, 10, config.SlowIOThresholdMs)
	assert.Equal(t, 0.1, config.SamplingRate)
	assert.Equal(t, int64(4096), config.MinIOSize)
	assert.Equal(t, 256, config.MaxPathLength)

	// Test K8s paths
	expectedPaths := []string{
		"/var/lib/kubelet/pods/",
		"/var/lib/kubelet/plugins/",
		"/var/lib/docker/containers/",
		"/var/lib/containerd/",
		"/var/log/containers/",
		"/var/log/pods/",
		"/etc/kubernetes/",
		"/var/lib/etcd/",
	}
	assert.ElementsMatch(t, expectedPaths, config.MonitoredK8sPaths)

	// Test VFS probes - default should enable read, write, fsync
	assert.True(t, config.EnableVFSRead)
	assert.True(t, config.EnableVFSWrite)
	assert.True(t, config.EnableVFSFsync)
	assert.False(t, config.EnableVFSIterateDir) // Disabled by default for performance

	// Test correlation settings
	assert.True(t, config.EnableCgroupCorrelation)
	assert.True(t, config.EnableContainerCorrelation)
	assert.True(t, config.MonitorPVCs)
	assert.True(t, config.MonitorConfigMaps)
	assert.True(t, config.MonitorSecrets)

	// Test intervals
	assert.Greater(t, config.MountRefreshInterval.Seconds(), 0.0)
	assert.Greater(t, config.CacheCleanupInterval.Seconds(), 0.0)
	assert.Greater(t, config.HealthCheckInterval.Seconds(), 0.0)
	assert.Greater(t, config.FlushInterval.Seconds(), 0.0)
}

// Test config with production-like settings
func TestProductionConfig(t *testing.T) {
	config := NewDefaultConfig()

	// Modify for production-like settings
	config.BufferSize = 50000
	config.SlowIOThresholdMs = 100 // More conservative for production
	config.SamplingRate = 0.01     // 1% sampling for high-volume environments
	config.MinIOSize = 16384       // 16KB minimum
	config.VerboseLogging = false  // Disable verbose logging
	config.DebugMode = false       // Disable debug mode

	err := config.Validate()
	assert.NoError(t, err, "Production-like config should be valid")

	// Verify production settings
	assert.Equal(t, 50000, config.BufferSize)
	assert.Equal(t, 100, config.SlowIOThresholdMs)
	assert.Equal(t, 0.01, config.SamplingRate)
	assert.Equal(t, int64(16384), config.MinIOSize)
	assert.False(t, config.VerboseLogging)
	assert.False(t, config.DebugMode)
}

// Test pathContains helper function
func TestPathContains(t *testing.T) {
	tests := []struct {
		path     string
		prefix   string
		expected bool
	}{
		{"/var/lib/kubelet/pods/test", "/var/lib/kubelet", true},
		{"/var/lib/kubelet/pods/test", "/var/lib/kubelet/pods", true},
		{"/var/lib/kubelet/pods/test", "/var/lib/kubelet/pods/test", true},
		{"/var/lib/kubelet/pods/test", "/var/lib/kubelet/pods/test/mount", false},
		{"/tmp/file", "/tmp", true},
		{"/tm/file", "/tmp", false},
		{"", "/tmp", false},
		{"/tmp/file", "", true}, // Empty prefix should match anything
	}

	for _, tt := range tests {
		t.Run(tt.path+"_contains_"+tt.prefix, func(t *testing.T) {
			result := pathContains(tt.path, tt.prefix)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Helper function that might be used in config
func pathContains(path, prefix string) bool {
	if prefix == "" {
		return true
	}
	if len(path) < len(prefix) {
		return false
	}
	return path[:len(prefix)] == prefix
}

// Test string slice contains helper
func TestStringSliceContains(t *testing.T) {
	slice := []string{"postgres", "nginx", "mysql", "redis"}

	assert.True(t, stringSliceContains(slice, "postgres"))
	assert.True(t, stringSliceContains(slice, "nginx"))
	assert.True(t, stringSliceContains(slice, "mysql"))
	assert.True(t, stringSliceContains(slice, "redis"))
	assert.False(t, stringSliceContains(slice, "unknown"))
	assert.False(t, stringSliceContains(slice, ""))
	assert.False(t, stringSliceContains([]string{}, "postgres"))
}

// Helper function for string slice contains
func stringSliceContains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
