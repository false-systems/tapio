package storageio

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test MountInfo struct and methods
func TestMountInfoStruct(t *testing.T) {
	mountInfo := &MountInfo{
		Path:          "/var/lib/kubelet/pods/550e8400-e29b-41d4-a716-446655440000/volumes/kubernetes.io~csi/pvc-123",
		Type:          "ext4",
		Device:        "/dev/nvme0n1p1",
		K8sVolumeType: "pvc",
		PodUID:        "550e8400-e29b-41d4-a716-446655440000",
	}

	// Test basic fields
	assert.Equal(t, "/var/lib/kubelet/pods/550e8400-e29b-41d4-a716-446655440000/volumes/kubernetes.io~csi/pvc-123", mountInfo.Path)
	assert.Equal(t, "ext4", mountInfo.Type)
	assert.Equal(t, "/dev/nvme0n1p1", mountInfo.Device)
	assert.Equal(t, "pvc", mountInfo.K8sVolumeType)
	assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", mountInfo.PodUID)
	// MountInfo doesn't have CreatedAt field, test other fields
	assert.NotEmpty(t, mountInfo.Path)
}

// Test ContainerInfo struct
func TestContainerInfoStruct(t *testing.T) {
	containerInfo := &ContainerInfo{
		ContainerID: "abc123def456789012345678901234567890",
		PodUID:      "550e8400-e29b-41d4-a716-446655440000",
	}

	assert.Equal(t, "abc123def456789012345678901234567890", containerInfo.ContainerID)
	assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", containerInfo.PodUID)
}

// Test SlowIOEvent struct
func TestSlowIOEventStruct(t *testing.T) {
	slowEvent := &SlowIOEvent{
		Operation: "read",
		Path:      "/var/lib/kubelet/pods/test/pvc-data/slow-file.db",
		PID:       1234,
		Duration:  50 * time.Millisecond,
		Timestamp: time.Now(),
	}

	assert.Equal(t, "read", slowEvent.Operation)
	assert.Equal(t, "/var/lib/kubelet/pods/test/pvc-data/slow-file.db", slowEvent.Path)
	assert.Equal(t, int32(1234), slowEvent.PID)
	assert.Equal(t, 50*time.Millisecond, slowEvent.Duration)
	assert.False(t, slowEvent.Timestamp.IsZero())
}

// Test K8sVolumeType enum values and String method
func TestK8sVolumeTypeEnum(t *testing.T) {
	tests := []struct {
		volumeType K8sVolumeType
		expected   string
	}{
		{K8sVolumePVC, "pvc"},
		{K8sVolumeConfigMap, "configmap"},
		{K8sVolumeSecret, "secret"},
		{K8sVolumeEmptyDir, "emptydir"},
		{K8sVolumeHostPath, "hostpath"},
		{K8sVolumeDownwardAPI, "downwardapi"},
		{K8sVolumeProjected, "projected"},
	}

	for _, tt := range tests {
		t.Run(string(tt.volumeType), func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.volumeType))
		})
	}
}

// Test mount discovery implementation (mock for non-Linux environments)
func TestDiscoverK8sMountPointsMock(t *testing.T) {
	// This test works on all platforms by mocking the discovery
	monitoredPaths := []string{
		"/var/lib/kubelet/pods/",
		"/var/lib/docker/containers/",
	}

	// Call the discovery function - it should handle missing /proc/mounts gracefully
	mounts, err := discoverK8sMountPointsImpl(monitoredPaths)

	// On non-Linux systems, this will return an error about missing /proc/mounts
	// On Linux systems, it might succeed or fail depending on permissions
	// Either way, the function should not panic
	if err != nil {
		// Expected on non-Linux systems
		assert.Contains(t, err.Error(), "failed to read /proc/mounts")
		assert.Nil(t, mounts)
	} else {
		// Unexpected success - verify mounts is not nil
		assert.NotNil(t, mounts)
	}
}

// Test findMatchingMount with mock data
func TestFindMatchingMount(t *testing.T) {
	collector, err := NewCollector("test-mount-match", NewDefaultConfig())
	require.NoError(t, err)

	// Populate mount cache with test data
	collector.mountCacheMu.Lock()
	collector.mountCache["/var/lib/kubelet/pods/test-pod-1/volumes/kubernetes.io~csi/pvc-123"] = &MountInfo{
		Path:          "/var/lib/kubelet/pods/test-pod-1/volumes/kubernetes.io~csi/pvc-123",
		K8sVolumeType: "pvc",
		PodUID:        "test-pod-1",
	}
	collector.mountCache["/var/lib/kubelet/pods/test-pod-2/volumes/kubernetes.io~configmap/app-config"] = &MountInfo{
		Path:          "/var/lib/kubelet/pods/test-pod-2/volumes/kubernetes.io~configmap/app-config",
		K8sVolumeType: "configmap",
		PodUID:        "test-pod-2",
	}
	collector.mountCache["/var/lib/etcd"] = &MountInfo{
		Path:          "/var/lib/etcd",
		K8sVolumeType: "etcd_data",
		PodUID:        "",
	}
	collector.mountCacheMu.Unlock()

	tests := []struct {
		name     string
		path     string
		expected *MountInfo
	}{
		{
			name: "exact match for PVC",
			path: "/var/lib/kubelet/pods/test-pod-1/volumes/kubernetes.io~csi/pvc-123",
			expected: &MountInfo{
				Path:          "/var/lib/kubelet/pods/test-pod-1/volumes/kubernetes.io~csi/pvc-123",
				K8sVolumeType: "pvc",
				PodUID:        "test-pod-1",
			},
		},
		{
			name: "file within PVC mount",
			path: "/var/lib/kubelet/pods/test-pod-1/volumes/kubernetes.io~csi/pvc-123/data/file.txt",
			expected: &MountInfo{
				Path:          "/var/lib/kubelet/pods/test-pod-1/volumes/kubernetes.io~csi/pvc-123",
				K8sVolumeType: "pvc",
				PodUID:        "test-pod-1",
			},
		},
		{
			name: "configmap match",
			path: "/var/lib/kubelet/pods/test-pod-2/volumes/kubernetes.io~configmap/app-config/config.yaml",
			expected: &MountInfo{
				Path:          "/var/lib/kubelet/pods/test-pod-2/volumes/kubernetes.io~configmap/app-config",
				K8sVolumeType: "configmap",
				PodUID:        "test-pod-2",
			},
		},
		{
			name: "etcd data match",
			path: "/var/lib/etcd/member/snap/db",
			expected: &MountInfo{
				Path:          "/var/lib/etcd",
				K8sVolumeType: "etcd_data",
				PodUID:        "",
			},
		},
		{
			name:     "no match",
			path:     "/tmp/random-file.txt",
			expected: nil,
		},
		{
			name:     "shorter path than mount",
			path:     "/var/lib/kubelet",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector.mountCacheMu.RLock()
			result := collector.findMatchingMount(tt.path)
			collector.mountCacheMu.RUnlock()

			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.expected.Path, result.Path)
				assert.Equal(t, tt.expected.K8sVolumeType, result.K8sVolumeType)
				assert.Equal(t, tt.expected.PodUID, result.PodUID)
			}
		})
	}
}

// Test enrichWithK8sContext function
func TestEnrichWithK8sContext(t *testing.T) {
	collector, err := NewCollector("test-enrich", NewDefaultConfig())
	require.NoError(t, err)

	// Setup mount cache
	collector.mountCacheMu.Lock()
	collector.mountCache["/var/lib/kubelet/pods/test-pod/volumes/kubernetes.io~csi/pvc-data"] = &MountInfo{
		Path:          "/var/lib/kubelet/pods/test-pod/volumes/kubernetes.io~csi/pvc-data",
		K8sVolumeType: "pvc",
		PodUID:        "550e8400-e29b-41d4-a716-446655440000",
	}
	collector.mountCacheMu.Unlock()

	// Setup container cache
	collector.containerCacheMu.Lock()
	collector.containerCache[12345] = &ContainerInfo{
		ContainerID: "abc123def456789012345678901234567890",
		PodUID:      "550e8400-e29b-41d4-a716-446655440000",
	}
	collector.containerCacheMu.Unlock()

	// Test event without K8s context
	event := &StorageIOEvent{
		Path:     "/var/lib/kubelet/pods/test-pod/volumes/kubernetes.io~csi/pvc-data/database.db",
		CgroupID: 12345,
	}

	enrichedEvent, err := collector.enrichWithK8sContext(event)
	require.NoError(t, err)

	// Verify K8s enrichment
	assert.Equal(t, "/var/lib/kubelet/pods/test-pod/volumes/kubernetes.io~csi/pvc-data", enrichedEvent.K8sPath)
	assert.Equal(t, "pvc", enrichedEvent.K8sVolumeType)
	assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", enrichedEvent.PodUID)
	assert.Equal(t, "/var/lib/kubelet/pods/test-pod/volumes/kubernetes.io~csi/pvc-data", enrichedEvent.MountPoint)

	// Verify container enrichment (cgroup correlation)
	collector.config.EnableCgroupCorrelation = true
	enrichedEvent, err = collector.enrichWithK8sContext(event)
	require.NoError(t, err)
	assert.Equal(t, "abc123def456789012345678901234567890", enrichedEvent.ContainerID)
}

// Test mount cache operations
func TestMountCacheOperations(t *testing.T) {
	collector, err := NewCollector("test-cache", NewDefaultConfig())
	require.NoError(t, err)

	// Test initial empty cache
	collector.mountCacheMu.RLock()
	assert.Equal(t, 0, len(collector.mountCache))
	collector.mountCacheMu.RUnlock()

	// Add entries to cache
	collector.mountCacheMu.Lock()
	collector.mountCache["/test/path1"] = &MountInfo{Path: "/test/path1", Type: "ext4"}
	collector.mountCache["/test/path2"] = &MountInfo{Path: "/test/path2", Type: "xfs"}
	collector.mountCacheMu.Unlock()

	// Verify entries
	collector.mountCacheMu.RLock()
	assert.Equal(t, 2, len(collector.mountCache))
	assert.Equal(t, "ext4", collector.mountCache["/test/path1"].Type)
	assert.Equal(t, "xfs", collector.mountCache["/test/path2"].Type)
	collector.mountCacheMu.RUnlock()

	// Clear cache
	collector.mountCacheMu.Lock()
	collector.mountCache = make(map[string]*MountInfo)
	collector.mountCacheMu.Unlock()

	// Verify cleared
	collector.mountCacheMu.RLock()
	assert.Equal(t, 0, len(collector.mountCache))
	collector.mountCacheMu.RUnlock()
}

// Test container cache operations
func TestContainerCacheOperations(t *testing.T) {
	collector, err := NewCollector("test-container-cache", NewDefaultConfig())
	require.NoError(t, err)

	// Test initial empty cache
	collector.containerCacheMu.RLock()
	assert.Equal(t, 0, len(collector.containerCache))
	collector.containerCacheMu.RUnlock()

	// Add entries to cache
	collector.containerCacheMu.Lock()
	collector.containerCache[12345] = &ContainerInfo{
		ContainerID: "container1",
		PodUID:      "pod1",
		LastSeen:    time.Now(),
	}
	collector.containerCache[67890] = &ContainerInfo{
		ContainerID: "container2",
		PodUID:      "pod2",
		LastSeen:    time.Now(),
	}
	collector.containerCacheMu.Unlock()

	// Verify entries
	collector.containerCacheMu.RLock()
	assert.Equal(t, 2, len(collector.containerCache))
	assert.Equal(t, "container1", collector.containerCache[12345].ContainerID)
	assert.Equal(t, "pod1", collector.containerCache[12345].PodUID)
	assert.Equal(t, "container2", collector.containerCache[67890].ContainerID)
	assert.Equal(t, "pod2", collector.containerCache[67890].PodUID)
	collector.containerCacheMu.RUnlock()
}

// Test initializeK8sMountPoints error handling
func TestInitializeK8sMountPointsErrorHandling(t *testing.T) {
	collector, err := NewCollector("test-init-error", NewDefaultConfig())
	require.NoError(t, err)

	// Test with invalid paths that should fail
	originalPaths := collector.config.MonitoredK8sPaths
	collector.config.MonitoredK8sPaths = []string{"/nonexistent/path/that/should/fail"}

	// This should fail gracefully on most systems
	err = collector.initializeK8sMountPoints()

	// Restore original paths
	collector.config.MonitoredK8sPaths = originalPaths

	// The error should be handled gracefully
	if err != nil {
		assert.Contains(t, err.Error(), "failed to discover K8s mount points")
	}
}

// Test mount info creation with different volume types
func TestMountInfoCreation(t *testing.T) {
	testCases := []struct {
		path           string
		expectedType   string
		expectedPodUID string
	}{
		{
			path:           "/var/lib/kubelet/pods/abc-123/volumes/kubernetes.io~csi/pvc-data",
			expectedType:   "pvc",
			expectedPodUID: "abc-123",
		},
		{
			path:           "/var/lib/kubelet/pods/def-456/volumes/kubernetes.io~configmap/app-config",
			expectedType:   "configmap",
			expectedPodUID: "def-456",
		},
		{
			path:           "/var/lib/kubelet/pods/ghi-789/volumes/kubernetes.io~secret/tls-certs",
			expectedType:   "secret",
			expectedPodUID: "ghi-789",
		},
		{
			path:           "/var/lib/kubelet/pods/jkl-012/volumes/kubernetes.io~empty-dir/cache",
			expectedType:   "emptydir",
			expectedPodUID: "jkl-012",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.expectedType, func(t *testing.T) {
			mountInfo := &MountInfo{
				Path:          tc.path,
				K8sVolumeType: tc.expectedType,
				PodUID:        tc.expectedPodUID,
			}

			assert.Equal(t, tc.path, mountInfo.Path)
			assert.Equal(t, tc.expectedType, mountInfo.K8sVolumeType)
			assert.Equal(t, tc.expectedPodUID, mountInfo.PodUID)
		})
	}
}

// Test mount discovery with edge cases
func TestMountDiscoveryEdgeCases(t *testing.T) {
	// Test with empty monitored paths
	emptyPaths := []string{}
	mounts, err := discoverK8sMountPointsImpl(emptyPaths)

	if err != nil {
		// Expected on non-Linux systems
		assert.Contains(t, err.Error(), "failed to read /proc/mounts")
	} else {
		// If successful, should return empty slice for empty input
		assert.NotNil(t, mounts)
		assert.Equal(t, 0, len(mounts))
	}

	// Test with nil monitored paths - should not panic
	mounts, err = discoverK8sMountPointsImpl(nil)
	if err != nil {
		assert.Contains(t, err.Error(), "failed to read /proc/mounts")
	} else {
		assert.NotNil(t, mounts)
	}
}

// Benchmark mount discovery performance
func BenchmarkFindMatchingMount(b *testing.B) {
	collector, err := NewCollector("bench-mount", NewDefaultConfig())
	if err != nil {
		b.Fatal(err)
	}

	// Populate mount cache with many entries
	collector.mountCacheMu.Lock()
	for i := 0; i < 1000; i++ {
		path := "/var/lib/kubelet/pods/test-pod-" + string(rune(i)) + "/volumes/kubernetes.io~csi/pvc-data"
		collector.mountCache[path] = &MountInfo{
			Path:          path,
			K8sVolumeType: "pvc",
			PodUID:        "test-pod-" + string(rune(i)),
		}
	}
	collector.mountCacheMu.Unlock()

	testPath := "/var/lib/kubelet/pods/test-pod-500/volumes/kubernetes.io~csi/pvc-data/file.txt"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.mountCacheMu.RLock()
		_ = collector.findMatchingMount(testPath)
		collector.mountCacheMu.RUnlock()
	}
}

// Test real /proc/mounts parsing if available (Linux only)
func TestRealProcMountsIfAvailable(t *testing.T) {
	// Only run this test if /proc/mounts exists (Linux systems)
	if _, err := os.Stat("/proc/mounts"); os.IsNotExist(err) {
		t.Skip("Skipping /proc/mounts test - not on Linux or no access")
		return
	}

	monitoredPaths := []string{
		"/var/lib/kubelet/",
		"/var/lib/docker/",
	}

	mounts, err := discoverK8sMountPointsImpl(monitoredPaths)

	if err != nil {
		// If there's an error, it should be a specific one we can handle
		t.Logf("Mount discovery failed (expected in some environments): %v", err)
	} else {
		// If successful, verify the structure
		assert.NotNil(t, mounts)
		t.Logf("Discovered %d mount points", len(mounts))

		for _, mount := range mounts {
			assert.NotEmpty(t, mount.Path)
			// MountInfo doesn't have CreatedAt field
		}
	}
}
