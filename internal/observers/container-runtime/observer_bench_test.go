package containerruntime

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

func BenchmarkObserver_ProcessEvent(b *testing.B) {
	observer, err := NewObserver("bench", nil)
	if err != nil {
		b.Fatal(err)
	}
	defer observer.Stop()

	ctx := context.Background()
	if err := observer.Start(ctx); err != nil {
		b.Fatal(err)
	}

	// Create sample event
	event := &domain.CollectorEvent{
		EventID:   "bench-1",
		Timestamp: time.Now(),
		Type:      domain.EventTypeContainerOOM,
		Source:    "container-runtime-bench",
		Severity:  domain.EventSeverityError,
		EventData: domain.EventDataContainer{
			Container: &domain.ContainerData{
				ContainerID: "abc123",
				ImageName:   "test:latest",
				Runtime:     "containerd",
				State:       "running",
				Labels: map[string]string{
					"pod-uid":   "pod-123",
					"namespace": "default",
					"pod-name":  "test-pod",
				},
			},
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer": "container-runtime",
				"version":  "1.0.0",
			},
		},
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			observer.SendEvent(event)
		}
	})

	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "events/sec")
}

func BenchmarkCgroupParser(b *testing.B) {
	cgroupPaths := []string{
		"/kubepods/burstable/pod12345678-1234-5678-9012-123456789012/docker-abcdef123456789",
		"/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod12345678-1234-5678-9012-123456789012.slice/docker-abcdef123456.scope",
		"/docker/abcdef123456789",
		"/system.slice/containerd.service/kubepods-burstable-pod12345678-1234-5678-9012-123456789012.slice/cri-containerd-abcdef123456789.scope",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		path := cgroupPaths[i%len(cgroupPaths)]
		_, _, _ = parseCgroupPath(path)
	}
}

func BenchmarkEventConversion(b *testing.B) {
	// Simulate BPF event conversion
	bpfEvent := BPFContainerExitEvent{
		Timestamp:   uint64(time.Now().UnixNano()),
		PID:         1234,
		TGID:        1234,
		ExitCode:    0,
		CgroupID:    12345,
		MemoryUsage: 1024 * 1024 * 100, // 100MB
		MemoryLimit: 1024 * 1024 * 512, // 512MB
		OOMKilled:   0,
	}

	// Set container ID (as byte array)
	copy(bpfEvent.ContainerID[:], []byte("abcdef123456789"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Just test struct creation, convertBPFEventToDomain was removed
		_ = &bpfEvent
	}
}

// Stress test - high volume event processing
func TestObserver_StressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	observer, err := NewObserver("stress", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Stop()

	ctx := context.Background()
	if err := observer.Start(ctx); err != nil {
		t.Fatal(err)
	}

	const numEvents = 10000
	const numGoroutines = 10

	// Start consumer
	events := observer.Events()
	received := 0
	done := make(chan bool)

	go func() {
		for range events {
			received++
			if received >= numEvents {
				done <- true
				return
			}
		}
	}()

	// Send events concurrently
	start := time.Now()
	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			for i := 0; i < numEvents/numGoroutines; i++ {
				event := &domain.CollectorEvent{
					EventID:   fmt.Sprintf("stress-%d-%d", id, i),
					Timestamp: time.Now(),
					Type:      domain.EventTypeContainerOOM,
					Source:    "container-runtime-stress",
					Severity:  domain.EventSeverityError,
					Metadata: domain.EventMetadata{
						Labels: map[string]string{
							"observer": "container-runtime",
							"version":  "1.0.0",
							"test":     "stress",
						},
					},
				}
				observer.SendEvent(event)
			}
		}(g)
	}

	// Wait for completion or timeout
	select {
	case <-done:
		duration := time.Since(start)
		rate := float64(numEvents) / duration.Seconds()
		t.Logf("Processed %d events in %v (%.2f events/sec)", numEvents, duration, rate)
	case <-time.After(30 * time.Second):
		t.Fatalf("Timeout: only received %d/%d events", received, numEvents)
	}
}
