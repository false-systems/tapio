package containerruntime

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"time"
)

// isDockerAvailable checks if Docker daemon is running
func isDockerAvailable() bool {
	// Check if Docker socket exists
	if _, err := os.Stat("/var/run/docker.sock"); err != nil {
		return false
	}
	return true
}

// mockMapUpdater is defined in runtime_client_extended_test.go

// contains checks if a slice contains a value (generic helper)
func contains[T comparable](slice []T, item T) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}

// Additional test helpers for stress testing
type stressTestClient struct {
	callCount   int
	maxLatency  int // milliseconds
	shouldPanic bool
}

func (s *stressTestClient) ListContainers(ctx context.Context) ([]Container, error) {
	s.callCount++
	if s.shouldPanic && s.callCount > 100 {
		panic("stress test panic")
	}

	// Simulate variable latency
	if s.maxLatency > 0 {
		time.Sleep(time.Duration(rand.Intn(s.maxLatency)) * time.Millisecond)
	}

	// Return varying number of containers
	count := rand.Intn(10) + 1
	containers := make([]Container, count)
	for i := range containers {
		containers[i] = Container{
			ID:  fmt.Sprintf("stress-%d-%d", s.callCount, i),
			PID: uint32(1000 + i),
		}
	}

	return containers, nil
}

func (s *stressTestClient) WatchEvents(ctx context.Context) (<-chan ContainerEvent, error) {
	ch := make(chan ContainerEvent, 100)

	go func() {
		defer close(ch)
		for i := 0; i < 50; i++ {
			select {
			case <-ctx.Done():
				return
			case ch <- ContainerEvent{
				Type: ContainerEventStart,
				Container: Container{
					ID:  fmt.Sprintf("event-%d", i),
					PID: uint32(2000 + i),
				},
				Timestamp: time.Now(),
			}:
			}
		}
	}()

	return ch, nil
}

func (s *stressTestClient) Close() error {
	return nil
}
