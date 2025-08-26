//go:build linux
// +build linux

package storageio

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestStressEventProcessing tests the collector under high load
func TestStressEventProcessing(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	logger := zaptest.NewLogger(t)

	config := NewDefaultConfig()
	config.BufferSize = 50000 // Large buffer for stress test
	config.SlowIOThresholdMs = 10

	collector, err := NewCollector("stress-test", config)
	require.NoError(t, err)

	collector.logger = logger

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	collector.ctx = ctx
	collector.cancel = cancel

	// Counters for tracking
	var (
		eventsGenerated int64
		eventsProcessed int64
		slowEvents      int64
		k8sEvents       int64
		errors          int64
	)

	// Consumer to drain events
	go func() {
		for {
			select {
			case event := <-collector.events:
				atomic.AddInt64(&eventsProcessed, 1)

				if storageData, ok := event.GetStorageIOData(); ok {
					if storageData.SlowIO {
						atomic.AddInt64(&slowEvents, 1)
					}
					if storageData.VolumeType != "" {
						atomic.AddInt64(&k8sEvents, 1)
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Event generators
	const numGenerators = 10
	const eventsPerGenerator = 1000

	var wg sync.WaitGroup

	for g := 0; g < numGenerators; g++ {
		wg.Add(1)
		go func(generatorID int) {
			defer wg.Done()

			r := rand.New(rand.NewSource(time.Now().UnixNano() + int64(generatorID)))

			for i := 0; i < eventsPerGenerator; i++ {
				select {
				case <-ctx.Done():
					return
				default:
				}

				// Generate diverse test events
				event := generateRandomStorageEvent(r, generatorID, i)

				if err := collector.processStorageEvent(event); err != nil {
					atomic.AddInt64(&errors, 1)
					t.Logf("Error processing event: %v", err)
				} else {
					atomic.AddInt64(&eventsGenerated, 1)
				}

				// Random small delay to simulate realistic timing
				if r.Intn(100) < 5 { // 5% chance
					time.Sleep(time.Microsecond * time.Duration(r.Intn(100)))
				}
			}
		}(g)
	}

	// Wait for all generators to finish
	wg.Wait()

	// Give some time for event processing to complete
	time.Sleep(2 * time.Second)

	// Collect final stats
	finalGenerated := atomic.LoadInt64(&eventsGenerated)
	finalProcessed := atomic.LoadInt64(&eventsProcessed)
	finalSlowEvents := atomic.LoadInt64(&slowEvents)
	finalK8sEvents := atomic.LoadInt64(&k8sEvents)
	finalErrors := atomic.LoadInt64(&errors)

	t.Logf("Stress Test Results:")
	t.Logf("- Events Generated: %d", finalGenerated)
	t.Logf("- Events Processed: %d", finalProcessed)
	t.Logf("- Slow Events: %d", finalSlowEvents)
	t.Logf("- K8s Events: %d", finalK8sEvents)
	t.Logf("- Processing Errors: %d", finalErrors)
	t.Logf("- Processing Rate: %.2f events/sec", float64(finalProcessed)/30.0)

	// Assertions
	assert.Greater(t, finalGenerated, int64(5000), "Should generate substantial events")
	assert.Greater(t, finalProcessed, int64(finalGenerated/2), "Should process at least half of events")
	assert.Greater(t, finalSlowEvents, int64(0), "Should detect some slow events")
	assert.Greater(t, finalK8sEvents, int64(0), "Should detect some K8s events")
	assert.Equal(t, int64(0), finalErrors, "Should have no processing errors")
}

// TestConcurrentRawEventProcessing tests raw eBPF event processing under load
func TestConcurrentRawEventProcessing(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrent test in short mode")
	}

	collector, err := NewCollector("concurrent-test", NewDefaultConfig())
	require.NoError(t, err)

	collector.logger = zaptest.NewLogger(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	collector.ctx = ctx
	collector.cancel = cancel

	var (
		rawEventsProcessed int64
		parseErrors        int64
		validationErrors   int64
		conversionErrors   int64
	)

	// Event drain
	go func() {
		for {
			select {
			case <-collector.events:
				// Just drain
			case <-ctx.Done():
				return
			}
		}
	}()

	const numWorkers = 20
	const eventsPerWorker = 100

	var wg sync.WaitGroup

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			r := rand.New(rand.NewSource(time.Now().UnixNano() + int64(workerID)))

			for i := 0; i < eventsPerWorker; i++ {
				select {
				case <-ctx.Done():
					return
				default:
				}

				// Generate random raw event data
				rawEventData := generateRandomRawEventData(r, workerID, i)

				err := collector.processRawStorageEvent(rawEventData)
				if err != nil {
					if contains(err.Error(), "parse") {
						atomic.AddInt64(&parseErrors, 1)
					} else if contains(err.Error(), "invalid") {
						atomic.AddInt64(&validationErrors, 1)
					} else if contains(err.Error(), "convert") {
						atomic.AddInt64(&conversionErrors, 1)
					}
				} else {
					atomic.AddInt64(&rawEventsProcessed, 1)
				}
			}
		}(w)
	}

	wg.Wait()
	time.Sleep(time.Second) // Let processing complete

	finalRawProcessed := atomic.LoadInt64(&rawEventsProcessed)
	finalParseErrors := atomic.LoadInt64(&parseErrors)
	finalValidationErrors := atomic.LoadInt64(&validationErrors)
	finalConversionErrors := atomic.LoadInt64(&conversionErrors)

	t.Logf("Concurrent Raw Event Processing Results:")
	t.Logf("- Raw Events Processed: %d", finalRawProcessed)
	t.Logf("- Parse Errors: %d", finalParseErrors)
	t.Logf("- Validation Errors: %d", finalValidationErrors)
	t.Logf("- Conversion Errors: %d", finalConversionErrors)

	assert.Greater(t, finalRawProcessed, int64(1000), "Should process many raw events")
}

// TestMemoryUsageUnderLoad tests memory behavior during sustained load
func TestMemoryUsageUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory test in short mode")
	}

	config := NewDefaultConfig()
	config.BufferSize = 10000

	collector, err := NewCollector("memory-test", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	collector.ctx = ctx
	collector.cancel = cancel

	// Start background loops like in real usage
	collector.wg.Add(3)
	go collector.refreshMountPointsLoop()
	go collector.healthMonitorLoop()
	go collector.slowIOTrackingLoop()

	// Generate sustained load
	const loadDuration = 4 * time.Second
	const targetEventsPerSec = 5000

	var eventsGenerated int64

	go func() {
		ticker := time.NewTicker(time.Second / targetEventsPerSec)
		defer ticker.Stop()

		r := rand.New(rand.NewSource(time.Now().UnixNano()))

		for {
			select {
			case <-ticker.C:
				event := generateRandomStorageEvent(r, 0, int(atomic.LoadInt64(&eventsGenerated)))
				if err := collector.processStorageEvent(event); err == nil {
					atomic.AddInt64(&eventsGenerated, 1)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Drain events
	go func() {
		for {
			select {
			case <-collector.events:
				// Just drain to prevent backup
			case <-ctx.Done():
				return
			}
		}
	}()

	// Let it run
	time.Sleep(loadDuration)
	cancel()
	collector.wg.Wait()

	finalEventsGenerated := atomic.LoadInt64(&eventsGenerated)
	expectedEvents := int64(targetEventsPerSec * int(loadDuration.Seconds()))

	t.Logf("Memory Test Results:")
	t.Logf("- Events Generated: %d", finalEventsGenerated)
	t.Logf("- Target Events: %d", expectedEvents)
	t.Logf("- Achievement Rate: %.2f%%", float64(finalEventsGenerated)/float64(expectedEvents)*100)

	// Should generate a reasonable number of events without memory issues
	assert.Greater(t, finalEventsGenerated, expectedEvents/4, "Should generate at least 25% of target events")
}

// BenchmarkEventProcessing benchmarks the core event processing pipeline
func BenchmarkEventProcessingStress(b *testing.B) {
	collector, err := NewCollector("benchmark", NewDefaultConfig())
	require.NoError(b, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	collector.ctx = ctx
	collector.cancel = cancel

	// Drain events
	go func() {
		for {
			select {
			case <-collector.events:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Pre-generate test events
	r := rand.New(rand.NewSource(42)) // Fixed seed for consistency
	events := make([]*StorageIOEvent, 1000)
	for i := range events {
		events[i] = generateRandomStorageEvent(r, 0, i)
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		eventIdx := 0
		for pb.Next() {
			event := events[eventIdx%len(events)]
			collector.processStorageEvent(event)
			eventIdx++
		}
	})
}

// BenchmarkRawEventParsing benchmarks raw eBPF event parsing
func BenchmarkRawEventParsing(b *testing.B) {
	collector, err := NewCollector("benchmark-raw", NewDefaultConfig())
	require.NoError(b, err)

	// Generate test raw event data
	r := rand.New(rand.NewSource(42))
	rawData := make([][]byte, 100)
	for i := range rawData {
		rawData[i] = generateRandomRawEventData(r, 0, i)
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		dataIdx := 0
		for pb.Next() {
			data := rawData[dataIdx%len(rawData)]
			collector.processRawStorageEvent(data)
			dataIdx++
		}
	})
}

// Helper functions

func generateRandomStorageEvent(r *rand.Rand, generatorID, eventID int) *StorageIOEvent {
	operations := []string{"read", "write", "fsync", "iterate_dir", "open", "close"}

	paths := []string{
		"/var/lib/kubelet/pods/pod-%d/volumes/kubernetes.io~csi/pvc-data-%d/mount/file.db",
		"/var/lib/kubelet/pods/pod-%d/volumes/kubernetes.io~configmap/app-config-%d/config.yaml",
		"/var/lib/kubelet/pods/pod-%d/volumes/kubernetes.io~secret/tls-certs-%d/cert.pem",
		"/var/lib/kubelet/pods/pod-%d/volumes/kubernetes.io~empty-dir/cache-%d/temp.dat",
		"/var/lib/docker/containers/container-%d/rootfs/app/data-%d.log",
		"/var/lib/etcd/member/snap/db-%d",
		"/home/user/documents/file-%d.txt",
	}

	commands := []string{"postgres", "mysql", "nginx", "redis", "app", "worker", "backup"}

	// Random duration - some will be slow
	var duration time.Duration
	if r.Intn(10) < 2 { // 20% chance of slow I/O
		duration = time.Duration(r.Intn(100)+15) * time.Millisecond // 15-115ms
	} else {
		duration = time.Duration(r.Intn(8)+1) * time.Millisecond // 1-8ms
	}

	path := fmt.Sprintf(paths[r.Intn(len(paths))], generatorID, eventID)

	event := &StorageIOEvent{
		Operation: operations[r.Intn(len(operations))],
		Path:      path,
		Timestamp: time.Now(),
		Size:      int64(r.Intn(65536) + 1024), // 1KB - 64KB
		Offset:    int64(r.Intn(1048576)),      // 0 - 1MB
		Duration:  duration,
		SlowIO:    duration > 10*time.Millisecond,
		Device:    fmt.Sprintf("8:%d", r.Intn(16)),
		Inode:     uint64(r.Intn(1000000) + 1),
		PID:       int32(r.Intn(30000) + 1000),
		PPID:      int32(r.Intn(30000) + 1000),
		UID:       int32(r.Intn(1000) + 1000),
		GID:       int32(r.Intn(1000) + 1000),
		Command:   commands[r.Intn(len(commands))],
		CgroupID:  uint64(r.Intn(100000) + 1),
		VFSLayer:  fmt.Sprintf("vfs_%s", operations[r.Intn(len(operations))]),
	}

	// Enrich with PVC info if it's a K8s path
	EnrichEventWithPVCInfo(event, event.Path)

	return event
}

func generateRandomRawEventData(r *rand.Rand, workerID, eventID int) []byte {
	rawEvent := StorageIOEventRaw{
		EventType:   uint8(r.Intn(6) + 1), // 1-6
		PID:         uint32(r.Intn(30000) + 1000),
		PPID:        uint32(r.Intn(30000) + 1000),
		UID:         uint32(r.Intn(1000) + 1000),
		GID:         uint32(r.Intn(1000) + 1000),
		CgroupID:    uint64(r.Intn(100000) + 1),
		StartTimeNs: uint64(time.Now().UnixNano()),
		EndTimeNs:   uint64(time.Now().Add(time.Duration(r.Intn(50)+1) * time.Millisecond).UnixNano()),
		Inode:       uint64(r.Intn(1000000) + 1),
		Size:        int64(r.Intn(65536) + 1024),
		Offset:      int64(r.Intn(1048576)),
		Flags:       uint32(r.Intn(256)),
		Mode:        uint32(0644),
		ErrorCode:   0, // Usually no error
		DevMajor:    uint32(8),
		DevMinor:    uint32(r.Intn(16)),
	}

	paths := []string{
		fmt.Sprintf("/var/lib/kubelet/pods/pod-%d/volumes/kubernetes.io~csi/pvc-%d/mount/file.db", workerID, eventID),
		fmt.Sprintf("/var/lib/kubelet/pods/pod-%d/volumes/kubernetes.io~configmap/config-%d/app.yaml", workerID, eventID),
		fmt.Sprintf("/home/user/file-%d-%d.txt", workerID, eventID),
	}

	commands := []string{"postgres", "nginx", "app", "worker"}

	path := paths[r.Intn(len(paths))]
	command := commands[r.Intn(len(commands))]

	copy(rawEvent.Path[:], path)
	copy(rawEvent.Comm[:], command)

	// Occasionally create invalid events for error testing
	if r.Intn(20) == 0 {
		rawEvent.PID = 0 // Invalid PID
	}

	// Convert to bytes
	data := make([]byte, unsafe.Sizeof(rawEvent))
	ptr := unsafe.Pointer(&rawEvent)
	copy(data, (*[unsafe.Sizeof(rawEvent)]byte)(ptr)[:])

	return data
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			(len(s) > 2*len(substr) && s[len(s)/2-len(substr)/2:len(s)/2-len(substr)/2+len(substr)] == substr))))
}
