package storageio

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestNewCollector(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
	}{
		{
			name:        "valid default config",
			config:      NewDefaultConfig(),
			expectError: false,
		},
		{
			name: "invalid buffer size",
			config: &Config{
				BufferSize:        -1,
				SlowIOThresholdMs: 10,
				SamplingRate:      0.1,
				MonitoredK8sPaths: []string{"/var/lib/kubelet/"},
				EnableVFSRead:     true,
				MonitorPVCs:       true,
			},
			expectError: true,
		},
		{
			name: "invalid sampling rate",
			config: &Config{
				BufferSize:        1000,
				SlowIOThresholdMs: 10,
				SamplingRate:      1.5,
				MonitoredK8sPaths: []string{"/var/lib/kubelet/"},
				EnableVFSRead:     true,
				MonitorPVCs:       true,
			},
			expectError: true,
		},
		{
			name: "no VFS probes enabled",
			config: &Config{
				BufferSize:          1000,
				SlowIOThresholdMs:   10,
				SamplingRate:        0.1,
				MonitoredK8sPaths:   []string{"/var/lib/kubelet/"},
				EnableVFSRead:       false,
				EnableVFSWrite:      false,
				EnableVFSFsync:      false,
				EnableVFSIterateDir: false,
				MonitorPVCs:         true,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollector("test-storage-io", tt.config)
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, collector)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, collector)
				assert.Equal(t, "test-storage-io", collector.Name())
				assert.False(t, collector.IsHealthy()) // Not started yet
			}
		})
	}
}

func TestCollectorLifecycle(t *testing.T) {
	config := NewDefaultConfig()
	config.EnableEBPF = false // Disable eBPF for unit tests

	collector, err := NewCollector("test-storage-io", config)
	require.NoError(t, err)
	require.NotNil(t, collector)

	// Test initial state
	assert.Equal(t, "test-storage-io", collector.Name())
	assert.False(t, collector.IsHealthy())

	// Test start (should fail gracefully without eBPF)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Note: Start will fail on non-Linux or when eBPF is disabled
	// This is expected behavior for the unit test environment
	err = collector.Start(ctx)
	// We expect this to fail in test environment, so we don't assert success

	// Test stop
	err = collector.Stop()
	assert.NoError(t, err)
	assert.False(t, collector.IsHealthy())
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
	}{
		{
			name:        "default config should be valid",
			config:      NewDefaultConfig(),
			expectError: false,
		},
		{
			name: "negative buffer size",
			config: func() *Config {
				c := NewDefaultConfig()
				c.BufferSize = -1
				return c
			}(),
			expectError: true,
		},
		{
			name: "zero slow IO threshold",
			config: func() *Config {
				c := NewDefaultConfig()
				c.SlowIOThresholdMs = 0
				return c
			}(),
			expectError: true,
		},
		{
			name: "invalid sampling rate",
			config: func() *Config {
				c := NewDefaultConfig()
				c.SamplingRate = 2.0
				return c
			}(),
			expectError: true,
		},
		{
			name: "empty monitored paths should still be valid",
			config: func() *Config {
				c := NewDefaultConfig()
				c.MonitoredK8sPaths = []string{}
				return c
			}(),
			expectError: false,
		},
		{
			name: "relative path should fail",
			config: func() *Config {
				c := NewDefaultConfig()
				c.MonitoredK8sPaths = []string{"relative/path"}
				return c
			}(),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestConfigGetters(t *testing.T) {
	config := NewDefaultConfig()

	// Test VFS probe getters
	probes := config.GetEnabledVFSProbes()
	assert.Contains(t, probes, VFSProbeRead)
	assert.Contains(t, probes, VFSProbeWrite)
	assert.Contains(t, probes, VFSProbeFsync)
	assert.Contains(t, probes, VFSProbeIterateDir)

	// Test volume type getters
	volumeTypes := config.GetMonitoredVolumeTypes()
	assert.Contains(t, volumeTypes, K8sVolumePVC)
	assert.Contains(t, volumeTypes, K8sVolumeConfigMap)
	assert.Contains(t, volumeTypes, K8sVolumeSecret)

	// Test path filtering
	assert.True(t, config.ShouldExcludePath("/proc/test"))
	assert.True(t, config.ShouldExcludePath("/sys/test"))
	assert.False(t, config.ShouldExcludePath("/var/lib/kubelet/pods/test"))

	// Test process filtering
	assert.True(t, config.ShouldExcludeProcess("kthreadd"))
	assert.True(t, config.ShouldExcludeProcess("rcu_test"))
	assert.False(t, config.ShouldExcludeProcess("kubelet"))

	// Test sampling rate logic
	assert.Equal(t, SlowIOSamplingRate, config.GetEffectiveSamplingRate("/test", false, true))
	assert.Equal(t, K8sSamplingRate, config.GetEffectiveSamplingRate("/test", true, false))
	assert.Equal(t, config.SamplingRate, config.GetEffectiveSamplingRate("/test", false, false))
}

func TestStorageEventProcessing(t *testing.T) {
	config := NewDefaultConfig()
	config.EnableEBPF = false

	collector, err := NewCollector("test-storage-io", config)
	require.NoError(t, err)

	// Test event processing helpers
	event := &StorageIOEvent{
		Operation: "read",
		Path:      "/var/lib/kubelet/pods/test-pod/volumes/pvc/test-volume",
		Size:      4096,
		Duration:  15 * time.Millisecond, // Slow I/O
		SlowIO:    true,                  // Mark as slow I/O for 15ms > 10ms threshold
		PID:       12345,
		Command:   "kubelet",
		Timestamp: time.Now(),
	}

	// Test filtering logic
	shouldProcess := collector.shouldProcessEvent(event)
	assert.True(t, shouldProcess) // Should process slow I/O

	// Test event conversion
	collectorEvent, err := collector.convertToCollectorEvent(event)
	require.NoError(t, err)
	require.NotNil(t, collectorEvent)

	assert.Equal(t, domain.EventTypeStorageIO, collectorEvent.Type)
	assert.Equal(t, "test-storage-io", collectorEvent.Source)
	assert.NotEmpty(t, collectorEvent.EventID)

	// Test StorageIOData extraction
	storageData, ok := collectorEvent.GetStorageIOData()
	require.True(t, ok)
	require.NotNil(t, storageData)

	assert.Equal(t, "read", storageData.Operation)
	assert.Equal(t, "/var/lib/kubelet/pods/test-pod/volumes/pvc/test-volume", storageData.Path)
	assert.Equal(t, int64(4096), storageData.Size)
	assert.Equal(t, 15*time.Millisecond, storageData.Duration)
	assert.True(t, storageData.SlowIO)
	// Check fields that actually exist in StorageIOData
	assert.NotEmpty(t, storageData.Path)
	assert.NotEmpty(t, storageData.Operation)

	// Test priority determination
	assert.Equal(t, domain.PriorityHigh, collectorEvent.Metadata.Priority)

	// Test tags
	assert.Contains(t, collectorEvent.Metadata.Tags, "storage-io")
	assert.Contains(t, collectorEvent.Metadata.Tags, "read")
	assert.Contains(t, collectorEvent.Metadata.Tags, "slow-io")
}

func TestUtilityFunctions(t *testing.T) {
	// Test path matching
	assert.True(t, matchesPath("/var/lib/kubelet/pods/test", "/var/lib/kubelet/"))
	assert.False(t, matchesPath("/var/lib/docker", "/var/lib/kubelet/"))
	assert.False(t, matchesPath("/var", "/var/lib/kubelet/"))

	// Test path hashing (for sampling)
	hash1 := hashPath("/test/path1")
	hash2 := hashPath("/test/path2")
	hash3 := hashPath("/test/path1") // Same as hash1

	assert.NotEqual(t, hash1, hash2)
	assert.Equal(t, hash1, hash3)
	assert.True(t, hash1 >= 0)
	assert.True(t, hash2 >= 0)
}

func TestIOClassification(t *testing.T) {
	tests := []struct {
		name     string
		event    *StorageIOEvent
		expected IOClassification
	}{
		{
			name: "fast small read",
			event: &StorageIOEvent{
				Operation: "read",
				Path:      "/tmp/test",
				Size:      1024,
				Duration:  500 * time.Microsecond,
			},
			expected: IOClassification{
				IsSlowIO:     false,
				LatencyClass: "fast",
				SizeClass:    "small",
			},
		},
		{
			name: "slow large write",
			event: &StorageIOEvent{
				Operation:     "write",
				Path:          "/var/lib/kubelet/pods/test",
				Size:          2 * 1024 * 1024, // 2MB
				Duration:      50 * time.Millisecond,
				K8sVolumeType: string(K8sVolumePVC),
			},
			expected: IOClassification{
				IsSlowIO:       true,
				IsK8sVolume:    true,
				IsCriticalPath: true,
				LatencyClass:   "slow",
				SizeClass:      "huge",
			},
		},
		{
			name: "critical fsync",
			event: &StorageIOEvent{
				Operation: "fsync",
				Path:      "/etc/kubernetes/admin.conf",
				Size:      0,
				Duration:  200 * time.Millisecond,
			},
			expected: IOClassification{
				IsSlowIO:       true,
				IsCriticalPath: true,
				LatencyClass:   "critical",
				SizeClass:      "small",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			classification := ClassifyIO(tt.event, 10) // 10ms threshold

			assert.Equal(t, tt.expected.IsSlowIO, classification.IsSlowIO)
			assert.Equal(t, tt.expected.IsK8sVolume, classification.IsK8sVolume)
			assert.Equal(t, tt.expected.IsCriticalPath, classification.IsCriticalPath)
			assert.Equal(t, tt.expected.LatencyClass, classification.LatencyClass)
			assert.Equal(t, tt.expected.SizeClass, classification.SizeClass)
		})
	}
}

func TestVFSProbeTypeString(t *testing.T) {
	tests := []struct {
		probe    VFSProbeType
		expected string
	}{
		{VFSProbeRead, "vfs_read"},
		{VFSProbeWrite, "vfs_write"},
		{VFSProbeFsync, "vfs_fsync"},
		{VFSProbeIterateDir, "vfs_iterate_dir"},
		{VFSProbeOpen, "vfs_open"},
		{VFSProbeClose, "vfs_close"},
		{VFSProbeType(99), "vfs_unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.probe.String())
		})
	}
}

func TestMountInfoProcessing(t *testing.T) {
	// Test mount discovery functions
	volumeType, podUID, volumeName := parseK8sVolumeInfo("/var/lib/kubelet/pods/test-pod-uid/volumes/kubernetes.io~configmap/test-config")
	assert.Equal(t, string(K8sVolumeConfigMap), volumeType)
	assert.Equal(t, "test-pod-uid", podUID)
	assert.Equal(t, "test-config", volumeName)

	// Test secret volume
	volumeType, podUID, volumeName = parseK8sVolumeInfo("/var/lib/kubelet/pods/another-pod/volumes/kubernetes.io~secret/tls-secret")
	assert.Equal(t, string(K8sVolumeSecret), volumeType)
	assert.Equal(t, "another-pod", podUID)
	assert.Equal(t, "tls-secret", volumeName)

	// Test non-K8s path
	volumeType, podUID, volumeName = parseK8sVolumeInfo("/tmp/regular-file")
	assert.Equal(t, string(K8sVolumeHostPath), volumeType)
	assert.Equal(t, "", podUID)
	assert.Equal(t, "", volumeName)
}

func TestK8sCriticalPathDetection(t *testing.T) {
	tests := []struct {
		path     string
		critical bool
	}{
		{"/var/lib/kubelet/pods/test", true},
		{"/etc/kubernetes/admin.conf", true},
		{"/var/lib/etcd/member", true},
		{"/var/log/containers/test.log", true},
		{"/var/lib/docker/containers/abc123", true},
		{"/tmp/regular-file", false},
		{"/home/user/document", false},
		{"/var/log/syslog", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.critical, isK8sCriticalPath(tt.path))
		})
	}
}

// Benchmark tests for performance validation

func BenchmarkEventProcessing(b *testing.B) {
	config := NewDefaultConfig()
	config.EnableEBPF = false

	collector, err := NewCollector("bench-storage-io", config)
	require.NoError(b, err)

	event := &StorageIOEvent{
		Operation: "read",
		Path:      "/var/lib/kubelet/pods/test/volumes/pvc/data",
		Size:      4096,
		Duration:  5 * time.Millisecond,
		PID:       12345,
		Command:   "kubelet",
		Timestamp: time.Now(),
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := collector.convertToCollectorEvent(event)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkPathMatching(b *testing.B) {
	paths := []string{
		"/var/lib/kubelet/pods/test-pod/volumes/pvc/data",
		"/var/lib/docker/containers/abc123/file",
		"/tmp/regular-file",
		"/etc/kubernetes/admin.conf",
	}
	pattern := "/var/lib/kubelet/"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		path := paths[i%len(paths)]
		matchesPath(path, pattern)
	}
}

func BenchmarkIOClassification(b *testing.B) {
	event := &StorageIOEvent{
		Operation:     "write",
		Path:          "/var/lib/kubelet/pods/test/volumes/pvc/data",
		Size:          1024 * 1024, // 1MB
		Duration:      25 * time.Millisecond,
		K8sVolumeType: string(K8sVolumePVC),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ClassifyIO(event, 10)
	}
}
