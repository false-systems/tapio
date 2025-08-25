package storageio

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

// Test all parts of the convertToCollectorEvent method to increase coverage
func TestConvertToCollectorEventComplete(t *testing.T) {
	config := NewDefaultConfig()
	config.SlowIOThresholdMs = 10

	collector, err := NewCollector("test-convert-complete", config)
	require.NoError(t, err)

	testCases := []struct {
		name         string
		event        *StorageIOEvent
		expectedSev  domain.EventSeverity
		expectedPrio domain.EventPriority
		expectedTags []string
	}{
		{
			name: "critical slow IO with error",
			event: &StorageIOEvent{
				Operation:     "read",
				Path:          "/var/lib/kubelet/pods/test/pvc-data/critical.db",
				Duration:      500 * time.Millisecond, // Very slow - critical
				Size:          8192,
				Offset:        0,
				ErrorCode:     5, // I/O error
				ErrorMessage:  "Input/output error",
				Device:        "8:0",
				Inode:         12345,
				PID:           2000,
				PPID:          1999,
				UID:           1001,
				GID:           1001,
				Command:       "postgres",
				CgroupID:      55555,
				VFSLayer:      "vfs_read",
				Flags:         0x0001,
				Mode:          0644,
				SlowIO:        true,
				BlockedIO:     false,
				K8sVolumeType: "pvc",
				PVCName:       "postgres-data",
				StorageClass:  "gp3",
				Timestamp:     time.Now(),
			},
			expectedSev:  domain.EventSeverityCritical,
			expectedPrio: domain.PriorityCritical,
			expectedTags: []string{"storage-io", "read", "slow-io", "kubernetes", "volume:pvc"},
		},
		{
			name: "warning slow IO",
			event: &StorageIOEvent{
				Operation:     "write",
				Path:          "/var/lib/kubelet/pods/test/secret-vol/config.yaml",
				Duration:      25 * time.Millisecond, // Slow but not critical
				Size:          2048,
				ErrorCode:     0,
				Device:        "8:1",
				Inode:         67890,
				PID:           3000,
				Command:       "nginx",
				CgroupID:      77777,
				VFSLayer:      "vfs_write",
				SlowIO:        true,
				BlockedIO:     false,
				K8sVolumeType: "secret",
				Timestamp:     time.Now(),
			},
			expectedSev:  domain.EventSeverityWarning,
			expectedPrio: domain.PriorityHigh,
			expectedTags: []string{"storage-io", "write", "slow-io", "kubernetes", "volume:secret"},
		},
		{
			name: "normal fast IO",
			event: &StorageIOEvent{
				Operation:     "fsync",
				Path:          "/var/lib/kubelet/pods/test/emptydir/temp.log",
				Duration:      2 * time.Millisecond, // Fast
				Size:          1024,
				ErrorCode:     0,
				Device:        "8:2",
				Inode:         11111,
				PID:           4000,
				Command:       "app",
				CgroupID:      88888,
				VFSLayer:      "vfs_fsync",
				SlowIO:        false,
				BlockedIO:     false,
				K8sVolumeType: "emptydir",
				Timestamp:     time.Now(),
			},
			expectedSev:  domain.EventSeverityInfo,
			expectedPrio: domain.PriorityNormal,
			expectedTags: []string{"storage-io", "fsync", "kubernetes", "volume:emptydir"},
		},
		{
			name: "blocked IO high priority",
			event: &StorageIOEvent{
				Operation: "read",
				Path:      "/home/user/documents/file.txt", // Non-K8s path
				Duration:  8 * time.Millisecond,
				Size:      4096,
				ErrorCode: 0,
				Device:    "8:3",
				Inode:     99999,
				PID:       5000,
				Command:   "vi",
				CgroupID:  12345,
				VFSLayer:  "vfs_read",
				SlowIO:    false,
				BlockedIO: true, // Blocked I/O
				Timestamp: time.Now(),
			},
			expectedSev:  domain.EventSeverityInfo,
			expectedPrio: domain.PriorityHigh, // Blocked IO is high priority
			expectedTags: []string{"storage-io", "read", "blocked-io"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			collectorEvent, err := collector.convertToCollectorEvent(tc.event)
			require.NoError(t, err)
			require.NotNil(t, collectorEvent)

			// Check basic event structure
			assert.NotEmpty(t, collectorEvent.EventID)
			assert.Equal(t, domain.EventTypeStorageIO, collectorEvent.Type)
			assert.Equal(t, collector.name, collectorEvent.Source)
			assert.Equal(t, tc.expectedSev, collectorEvent.Severity)
			assert.Equal(t, tc.event.Timestamp, collectorEvent.Timestamp)

			// Check metadata
			assert.Equal(t, tc.expectedPrio, collectorEvent.Metadata.Priority)
			assert.Equal(t, tc.event.PID, collectorEvent.Metadata.PID)
			assert.Equal(t, tc.event.PPID, collectorEvent.Metadata.PPID)
			assert.Equal(t, tc.event.UID, collectorEvent.Metadata.UID)
			assert.Equal(t, tc.event.GID, collectorEvent.Metadata.GID)
			assert.Equal(t, tc.event.Command, collectorEvent.Metadata.Command)
			assert.Equal(t, tc.event.CgroupID, collectorEvent.Metadata.CgroupID)

			// Check tags
			for _, expectedTag := range tc.expectedTags {
				assert.Contains(t, collectorEvent.Metadata.Tags, expectedTag,
					"Expected tag '%s' not found in %v", expectedTag, collectorEvent.Metadata.Tags)
			}

			// Check correlation hints
			assert.NotEmpty(t, collectorEvent.Metadata.CorrelationHints)
			hints := strings.Join(collectorEvent.Metadata.CorrelationHints, ",")
			assert.Contains(t, hints, "cgroup:")
			assert.Contains(t, hints, "device:")

			// Check storage IO data
			storageData, ok := collectorEvent.GetStorageIOData()
			require.True(t, ok)
			require.NotNil(t, storageData)

			assert.Equal(t, tc.event.Operation, storageData.Operation)
			assert.Equal(t, tc.event.Path, storageData.Path)
			assert.Equal(t, tc.event.Size, storageData.Size)
			assert.Equal(t, tc.event.Offset, storageData.Offset)
			assert.Equal(t, tc.event.Duration, storageData.Duration)
			assert.Equal(t, tc.event.SlowIO, storageData.SlowIO)
			assert.Equal(t, tc.event.BlockedIO, storageData.BlockedIO)
			assert.Equal(t, tc.event.Device, storageData.Device)
			assert.Equal(t, tc.event.Inode, storageData.Inode)
			assert.Equal(t, tc.event.K8sVolumeType, storageData.VolumeType)
			assert.Equal(t, tc.event.PVCName, storageData.PVCName)
			assert.Equal(t, tc.event.StorageClass, storageData.StorageClass)
			assert.Equal(t, tc.event.ErrorCode, storageData.ErrorCode)
			assert.Equal(t, tc.event.ErrorMessage, storageData.ErrorMessage)
			assert.Equal(t, tc.event.VFSLayer, storageData.VFSLayer)
			assert.Equal(t, tc.event.Flags, storageData.Flags)
			assert.Equal(t, tc.event.Mode, storageData.Mode)

			// Check calculated latency
			expectedLatencyMs := float64(tc.event.Duration.Nanoseconds()) / 1e6
			assert.InDelta(t, expectedLatencyMs, storageData.LatencyMS, 0.01)
		})
	}
}

// Test all severity levels
func TestEventSeverityLevels(t *testing.T) {
	config := NewDefaultConfig()
	config.SlowIOThresholdMs = 10

	collector, err := NewCollector("test-severity", config)
	require.NoError(t, err)

	testCases := []struct {
		name      string
		duration  time.Duration
		errorCode int32
		expected  domain.EventSeverity
	}{
		{"critical - extremely slow", 200 * time.Millisecond, 0, domain.EventSeverityCritical}, // 10 * SlowIOThresholdMs
		{"critical - with error", 5 * time.Millisecond, 5, domain.EventSeverityCritical},
		{"error - very slow", 75 * time.Millisecond, 0, domain.EventSeverityError}, // 5 * SlowIOThresholdMs
		{"warning - slow", 15 * time.Millisecond, 0, domain.EventSeverityWarning},  // 1 * SlowIOThresholdMs
		{"info - normal", 2 * time.Millisecond, 0, domain.EventSeverityInfo},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			event := &StorageIOEvent{
				Operation: "read",
				Path:      "/test/file",
				Duration:  tc.duration,
				Size:      4096,
				ErrorCode: tc.errorCode,
				Timestamp: time.Now(),
			}

			collectorEvent, err := collector.convertToCollectorEvent(event)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, collectorEvent.Severity)
		})
	}
}

// Test all priority levels
func TestEventPriorityLevels(t *testing.T) {
	config := NewDefaultConfig()
	config.SlowIOThresholdMs = 10

	collector, err := NewCollector("test-priority", config)
	require.NoError(t, err)

	testCases := []struct {
		name     string
		event    *StorageIOEvent
		expected domain.EventPriority
	}{
		{
			name: "critical - very slow with error",
			event: &StorageIOEvent{
				Duration:  200 * time.Millisecond,
				ErrorCode: 5,
				Path:      "/tmp/test",
			},
			expected: domain.PriorityCritical,
		},
		{
			name: "critical - just error",
			event: &StorageIOEvent{
				Duration:  5 * time.Millisecond,
				ErrorCode: 13, // Permission denied
				Path:      "/tmp/test",
			},
			expected: domain.PriorityCritical,
		},
		{
			name: "high - slow IO",
			event: &StorageIOEvent{
				Duration: 25 * time.Millisecond,
				SlowIO:   true,
				Path:     "/tmp/test",
			},
			expected: domain.PriorityHigh,
		},
		{
			name: "high - blocked IO",
			event: &StorageIOEvent{
				Duration:  8 * time.Millisecond,
				BlockedIO: true,
				Path:      "/tmp/test",
			},
			expected: domain.PriorityHigh,
		},
		{
			name: "normal - K8s critical path",
			event: &StorageIOEvent{
				Duration: 3 * time.Millisecond,
				Path:     "/var/lib/kubelet/pods/test/pvc",
			},
			expected: domain.PriorityNormal,
		},
		{
			name: "low - regular path",
			event: &StorageIOEvent{
				Duration: 2 * time.Millisecond,
				Path:     "/home/user/document.txt",
			},
			expected: domain.PriorityLow,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			priority := collector.determinePriority(tc.event)
			assert.Equal(t, tc.expected, priority)
		})
	}
}

// Test tag generation edge cases
func TestTagGenerationEdgeCases(t *testing.T) {
	collector, err := NewCollector("test-tags-edge", NewDefaultConfig())
	require.NoError(t, err)

	testCases := []struct {
		name           string
		event          *StorageIOEvent
		expectedTags   []string
		unexpectedTags []string
	}{
		{
			name: "minimal event",
			event: &StorageIOEvent{
				Operation: "read",
				SlowIO:    false,
				BlockedIO: false,
				Path:      "/tmp/test",
			},
			expectedTags:   []string{"storage-io", "read"},
			unexpectedTags: []string{"slow-io", "blocked-io", "kubernetes"},
		},
		{
			name: "all flags set",
			event: &StorageIOEvent{
				Operation:     "write",
				SlowIO:        true,
				BlockedIO:     true,
				Path:          "/var/lib/kubelet/pods/test/pvc",
				K8sVolumeType: "configmap",
			},
			expectedTags:   []string{"storage-io", "write", "slow-io", "blocked-io", "kubernetes", "volume:configmap"},
			unexpectedTags: []string{},
		},
		{
			name: "empty volume type",
			event: &StorageIOEvent{
				Operation:     "fsync",
				SlowIO:        false,
				BlockedIO:     false,
				Path:          "/var/lib/kubelet/pods/test/unknown",
				K8sVolumeType: "", // Empty volume type
			},
			expectedTags:   []string{"storage-io", "fsync", "kubernetes"},
			unexpectedTags: []string{"volume:", "slow-io", "blocked-io"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tags := collector.generateTags(tc.event)

			for _, expectedTag := range tc.expectedTags {
				assert.Contains(t, tags, expectedTag, "Expected tag '%s' not found", expectedTag)
			}

			for _, unexpectedTag := range tc.unexpectedTags {
				assert.NotContains(t, tags, unexpectedTag, "Unexpected tag '%s' found", unexpectedTag)
			}
		})
	}
}

// Test the complete processStorageEvent pipeline
func TestProcessStorageEventPipeline(t *testing.T) {
	config := NewDefaultConfig()
	config.BufferSize = 100
	config.SamplingRate = 1.0 // Process all events

	collector, err := NewCollector("test-pipeline", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	collector.ctx = ctx
	collector.cancel = cancel

	// Add K8s mount for enrichment
	collector.mountCacheMu.Lock()
	collector.mountCache["/var/lib/kubelet/pods/test-pod/volumes/kubernetes.io~csi/pvc-data"] = &MountInfo{
		Path:          "/var/lib/kubelet/pods/test-pod/volumes/kubernetes.io~csi/pvc-data",
		K8sVolumeType: "pvc",
		PodUID:        "test-pod-uid-123",
	}
	collector.mountCacheMu.Unlock()

	testEvents := []*StorageIOEvent{
		{
			Operation: "read",
			Path:      "/var/lib/kubelet/pods/test-pod/volumes/kubernetes.io~csi/pvc-data/database.db",
			Duration:  15 * time.Millisecond, // Slow
			Size:      8192,
			PID:       1000,
			Command:   "postgres",
			CgroupID:  12345,
			Timestamp: time.Now(),
		},
		{
			Operation: "write",
			Path:      "/tmp/small-file.txt", // Should be filtered by sampling
			Duration:  2 * time.Millisecond,
			Size:      512,
			PID:       2000,
			Command:   "test",
			Timestamp: time.Now(),
		},
	}

	var processedEvents []*domain.CollectorEvent

	// Process events and collect results
	for _, event := range testEvents {
		err := collector.processStorageEvent(event)
		require.NoError(t, err)
	}

	// Collect processed events
	timeout := time.After(1 * time.Second)
	for len(processedEvents) < len(testEvents) {
		select {
		case event := <-collector.Events():
			processedEvents = append(processedEvents, event)
		case <-timeout:
			break // Timeout - some events may have been filtered
		}
	}

	// Should have processed at least the K8s event
	assert.Greater(t, len(processedEvents), 0)

	// Check first event enrichment
	if len(processedEvents) > 0 {
		firstEvent := processedEvents[0]
		assert.Equal(t, domain.EventTypeStorageIO, firstEvent.Type)
		assert.Equal(t, collector.name, firstEvent.Source)

		storageData, ok := firstEvent.GetStorageIOData()
		require.True(t, ok)
		assert.True(t, storageData.SlowIO) // 15ms > 10ms threshold
		assert.Equal(t, "pvc", storageData.VolumeType)
	}
}

// Test edge cases for K8s critical path detection
func TestK8sCriticalPathEdgeCases(t *testing.T) {
	testCases := []struct {
		path     string
		expected bool
	}{
		{"/var/lib/kubelet/pods/", false},        // Exact match to prefix, but empty pod
		{"/var/lib/kubelet/pods/test", true},     // Actual pod path
		{"/var/lib/kubelet/pods", false},         // Missing trailing slash
		{"/var/lib/kubelet", false},              // Parent path only
		{"/var/lib/docker/containers/", false},   // Empty container
		{"/var/lib/docker/containers/abc", true}, // Actual container
		{"/etc/kubernetes", false},               // Missing trailing slash in path
		{"/etc/kubernetes/", true},               // Exact directory match
		{"/etc/kubernetes/admin.conf", true},     // File in directory
		{"", false},                              // Empty path
		{"/", false},                             // Root path only
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			result := isK8sCriticalPath(tc.path)
			assert.Equal(t, tc.expected, result, "Path: %s", tc.path)
		})
	}
}

// Test processStorageEvent with buffer full scenario
func TestProcessStorageEventBufferFull(t *testing.T) {
	config := NewDefaultConfig()
	config.BufferSize = 1 // Very small buffer to test overflow
	config.SamplingRate = 1.0

	collector, err := NewCollector("test-buffer-full", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	collector.ctx = ctx
	collector.cancel = cancel

	// Fill up the buffer first
	firstEvent := &StorageIOEvent{
		Operation: "read",
		Path:      "/test/file1.txt",
		Duration:  5 * time.Millisecond,
		Size:      1024,
		Timestamp: time.Now(),
	}

	err = collector.processStorageEvent(firstEvent)
	require.NoError(t, err)

	// Buffer should now be full, second event should trigger overflow
	secondEvent := &StorageIOEvent{
		Operation: "write",
		Path:      "/test/file2.txt",
		Duration:  3 * time.Millisecond,
		Size:      2048,
		Timestamp: time.Now(),
	}

	// This should handle buffer full gracefully (no error)
	err = collector.processStorageEvent(secondEvent)
	assert.NoError(t, err)

	// Verify first event is still in channel
	select {
	case event := <-collector.Events():
		assert.NotNil(t, event)
		storageData, ok := event.GetStorageIOData()
		require.True(t, ok)
		assert.Equal(t, "read", storageData.Operation)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected event not received")
	}
}
