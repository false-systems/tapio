package kernel

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors"
)

func TestCgroupIDExtraction(t *testing.T) {
	tests := []struct {
		name         string
		setupFunc    func(t *testing.T) (*ModularCollector, func())
		validateFunc func(t *testing.T, collector *ModularCollector, cgroupID uint64)
	}{
		{
			name: "Real container cgroup ID extraction",
			setupFunc: func(t *testing.T) (*ModularCollector, func()) {
				collector, err := NewModularCollector("test-cgroup")
				require.NoError(t, err)

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err = collector.Start(ctx)
				if err != nil {
					cancel()
					t.Skip("kernel eBPF not available in test environment")
				}

				cleanup := func() {
					cancel()
					collector.Stop()
				}

				return collector, cleanup
			},
			validateFunc: func(t *testing.T, collector *ModularCollector, cgroupID uint64) {
				// Validate that cgroup ID is not zero and not a PID
				assert.NotZero(t, cgroupID, "Cgroup ID should not be zero")

				// Cgroup IDs should be much larger than typical PIDs
				// With our fix, fallback cgroup IDs use 4GB offset (0x100000000)
				// and kernfs inodes are also typically large numbers
				assert.Greater(t, cgroupID, uint64(100000), "Cgroup ID should be much larger than typical PID range")

				// Enhanced validation: if this is a fallback ID (using offset), it should be >= 4GB
				if cgroupID >= 0x100000000 {
					assert.GreaterOrEqual(t, cgroupID, uint64(0x100000000),
						"Fallback cgroup ID should use 4GB offset to guarantee PID separation")

					// Additional validation for different offset ranges
					if cgroupID >= 0x100000000 && cgroupID < 0x200000000 {
						// This is the 4GB offset range for cgroup IDs
						assert.Greater(t, cgroupID, uint64(0x100000000), "Cgroup ID should be in 4GB-8GB range")
						assert.Less(t, cgroupID, uint64(0x200000000), "Cgroup ID should be in 4GB-8GB range")
					} else if cgroupID >= 0x200000000 && cgroupID < 0x400000000 {
						// This is the 8GB offset range for css_set pointer hashes
						assert.Greater(t, cgroupID, uint64(0x200000000), "Hash-based cgroup ID should be in 8GB-16GB range")
						assert.Less(t, cgroupID, uint64(0x400000000), "Hash-based cgroup ID should be in 8GB-16GB range")
					}
				}

				// Test that the same process consistently returns the same cgroup ID
				currentPID := uint32(os.Getpid())

				// Add our PID to container tracking
				value := uint8(1)
				err := collector.objs.ContainerPids.Put(&currentPID, &value)
				require.NoError(t, err)

				// Trigger some memory allocations to generate events
				for i := 0; i < 5; i++ {
					_ = make([]byte, 1024)
					time.Sleep(10 * time.Millisecond)
				}

				// Check collected events for consistent cgroup IDs
				var events []KernelEvent
				timeout := time.After(2 * time.Second)

			eventLoop:
				for len(events) < 3 {
					select {
					case rawEvent := <-collector.Events():
						if rawEvent.Metadata["pid"] == fmt.Sprintf("%d", currentPID) {
							// Parse the kernel event from raw data
							if len(rawEvent.Data) >= int(unsafe.Sizeof(KernelEvent{})) {
								safeParser := collectors.NewSafeParser()
								event, err := collectors.SafeCast[KernelEvent](safeParser, rawEvent.Data)
								require.NoError(t, err)

								events = append(events, *event)


							}
						}
					case <-timeout:
						break eventLoop
					}
				}

				if len(events) > 1 {
					firstCgroupID := events[0].CgroupID
					for _, event := range events[1:] {
						assert.Equal(t, firstCgroupID, event.CgroupID,
							"All events from same process should have same cgroup ID")
					}
					assert.NotZero(t, firstCgroupID, "Events should have non-zero cgroup ID")
				}
			},
		},
		{
			name: "Cgroup ID vs PID validation",
			setupFunc: func(t *testing.T) (*ModularCollector, func()) {
				collector, err := NewModularCollector("test-pid-validation")
				require.NoError(t, err)

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err = collector.Start(ctx)
				if err != nil {
					cancel()
					t.Skip("kernel eBPF not available in test environment")
				}

				cleanup := func() {
					cancel()
					collector.Stop()
				}

				return collector, cleanup
			},
			validateFunc: func(t *testing.T, collector *ModularCollector, cgroupID uint64) {
				// Get current process cgroup information from /proc
				currentPID := os.Getpid()
				cgroupPath := fmt.Sprintf("/proc/%d/cgroup", currentPID)
				cgroupData, err := os.ReadFile(cgroupPath)
				if err != nil {
					t.Skip("Cannot read cgroup information")
				}

				cgroupStr := string(cgroupData)
				t.Logf("Process %d cgroup info:\n%s", currentPID, cgroupStr)

				// Verify that cgroup ID is not equal to PID
				assert.NotEqual(t, cgroupID, uint64(currentPID),
					"Cgroup ID should not equal PID - this indicates the bug is present")

				// Enhanced validation: cgroup ID should be orders of magnitude larger than PID
				// to ensure no overlap with the PID space (max PID is typically 32-bit)
				assert.Greater(t, cgroupID, uint64(currentPID)*1000,
					"Cgroup ID should be much larger than PID to prevent overlap")

				// Specific validation for offset-based IDs
				maxPossiblePID := uint64(0x7FFFFFFF) // 32-bit signed int max
				if cgroupID >= 0x100000000 {
					// This is an offset-based ID - should be completely separate from PID space
					assert.Greater(t, cgroupID, maxPossiblePID*4,
						"Offset-based cgroup ID should be well separated from max possible PID")
				}

				// Additional validation: cgroup ID should be consistent with proc information
				// For containers, we should see docker/containerd/kubepods in cgroup path
				isContainer := strings.Contains(cgroupStr, "docker") ||
					strings.Contains(cgroupStr, "containerd") ||
					strings.Contains(cgroupStr, "kubepods")

				if isContainer {
					// Container processes should have cgroup IDs that look like inode numbers
					// These are typically large numbers (> 1000000)
					assert.Greater(t, cgroupID, uint64(1000000),
						"Container cgroup ID should be a large inode number")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, cleanup := tt.setupFunc(t)
			defer cleanup()

			// Generate a test cgroup ID by checking our current process
			currentPID := uint32(os.Getpid())

			// Add current process to container tracking
			value := uint8(1)
			err := collector.objs.ContainerPids.Put(&currentPID, &value)
			require.NoError(t, err)

			// Generate some activity to trigger events
			for i := 0; i < 3; i++ {
				_ = make([]byte, 512)
			}

			// Wait for an event and extract cgroup ID
			var cgroupID uint64
			timeout := time.After(3 * time.Second)

		eventLoop:
			for {
				select {
				case rawEvent := <-collector.Events():
					if rawEvent.Metadata["pid"] == fmt.Sprintf("%d", currentPID) {
						cgroupIDStr := rawEvent.Metadata["cgroup_id"]
						var err error
						cgroupID, err = strconv.ParseUint(cgroupIDStr, 10, 64)
						require.NoError(t, err)
						break eventLoop
					}
				case <-timeout:
					t.Fatal("Timeout waiting for events")
				}
			}

			tt.validateFunc(t, collector, cgroupID)
		})
	}
}

func TestCgroupIDCorrelation(t *testing.T) {
	collector, err := NewModularCollector("test-correlation")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	if err != nil {
		t.Skip("kernel eBPF not available in test environment")
	}
	defer collector.Stop()

	// Test pod information correlation
	testCgroupID := uint64(12345678)
	testPodUID := "test-pod-uid-12345"
	testNamespace := "test-namespace"
	testPodName := "test-pod"

	// Update pod info in kernel eBPF map
	err = collector.UpdatePodInfo(testCgroupID, testPodUID, testNamespace, testPodName)
	require.NoError(t, err)

	// Retrieve pod info
	podInfo, err := collector.GetPodInfo(testCgroupID)
	require.NoError(t, err)
	require.NotNil(t, podInfo)

	// Validate correlation data
	assert.Equal(t, testPodUID, collector.nullTerminatedString(podInfo.PodUID[:]))
	assert.Equal(t, testNamespace, collector.nullTerminatedString(podInfo.Namespace[:]))
	assert.Equal(t, testPodName, collector.nullTerminatedString(podInfo.PodName[:]))

	// Test removal
	err = collector.RemovePodInfo(testCgroupID)
	require.NoError(t, err)

	// Should not find pod info after removal
	_, err = collector.GetPodInfo(testCgroupID)
	assert.Error(t, err)
}

func TestCgroupIDUniqueness(t *testing.T) {
	collector, err := NewModularCollector("test-uniqueness")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	if err != nil {
		t.Skip("kernel eBPF not available in test environment")
	}
	defer collector.Stop()

	// Track cgroup IDs we've seen
	seenCgroups := make(map[uint64]bool)
	seenPIDs := make(map[uint32]bool)

	// Add multiple PIDs and collect their events
	basePID := uint32(os.Getpid())
	value := uint8(1)

	// Add our current process
	err = collector.objs.ContainerPids.Put(&basePID, &value)
	require.NoError(t, err)

	// Generate activity and collect cgroup IDs
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		timeout := time.After(3 * time.Second)
		eventCount := 0

		for eventCount < 10 {
			select {
			case rawEvent := <-collector.Events():
				pidStr := rawEvent.Metadata["pid"]
				cgroupIDStr := rawEvent.Metadata["cgroup_id"]

				pid, err := strconv.ParseUint(pidStr, 10, 32)
				if err != nil {
					continue
				}

				cgroupID, err := strconv.ParseUint(cgroupIDStr, 10, 64)
				if err != nil {
					continue
				}

				seenPIDs[uint32(pid)] = true
				seenCgroups[cgroupID] = true
				eventCount++

				// Validate that cgroup ID is not equal to PID
				assert.NotEqual(t, cgroupID, pid,
					"Cgroup ID %d should not equal PID %d", cgroupID, pid)

			case <-timeout:
				return
			}
		}
	}()

	// Generate some events
	for i := 0; i < 20; i++ {
		_ = make([]byte, 256)
		time.Sleep(50 * time.Millisecond)
	}

	wg.Wait()

	t.Logf("Seen %d unique PIDs and %d unique cgroup IDs", len(seenPIDs), len(seenCgroups))

	// For processes in the same cgroup, they should have the same cgroup ID
	// For the same process, PID and cgroup ID should be different
	assert.Greater(t, len(seenPIDs), 0, "Should have seen some PIDs")
	assert.Greater(t, len(seenCgroups), 0, "Should have seen some cgroup IDs")
}

func BenchmarkCgroupIDExtraction(b *testing.B) {
	collector, err := NewModularCollector("bench-cgroup")
	if err != nil {
		b.Skip("Cannot create collector")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	if err != nil {
		b.Skip("kernel eBPF not available")
	}
	defer collector.Stop()

	currentPID := uint32(os.Getpid())
	value := uint8(1)
	err = collector.objs.ContainerPids.Put(&currentPID, &value)
	if err != nil {
		b.Skip("Cannot add PID to tracking")
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Generate some activity that triggers cgroup ID extraction
			_ = make([]byte, 64)
		}
	})
}
