package containerruntime

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRuntimeClient_Concurrent tests concurrent access to runtime client
func TestRuntimeClient_Concurrent(t *testing.T) {
	client := &mockRuntimeClient{
		containers: []Container{
			{ID: "container-1", PID: 1001},
			{ID: "container-2", PID: 1002},
			{ID: "container-3", PID: 1003},
		},
		events: []ContainerEvent{
			{Type: ContainerEventStart, Container: Container{ID: "container-4"}},
			{Type: ContainerEventStop, Container: Container{ID: "container-1"}},
		},
	}

	ctx := context.Background()
	var wg sync.WaitGroup

	// Concurrent ListContainers calls
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			containers, err := client.ListContainers(ctx)
			assert.NoError(t, err)
			assert.Len(t, containers, 3)
		}()
	}

	// Concurrent WatchEvents calls
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			events, err := client.WatchEvents(ctx)
			assert.NoError(t, err)
			assert.NotNil(t, events)
		}()
	}

	wg.Wait()
}

// TestRuntimeClient_ContextCancellation tests proper context handling
func TestRuntimeClient_ContextCancellation(t *testing.T) {
	client := &mockRuntimeClient{
		containers: []Container{{ID: "test"}},
	}

	// Create a context that we'll cancel
	ctx, cancel := context.WithCancel(context.Background())

	// Start listing in background
	done := make(chan bool)
	go func() {
		_, _ = client.ListContainers(ctx)
		done <- true
	}()

	// Cancel context
	cancel()

	// Wait for completion with timeout
	select {
	case <-done:
		// Good, operation completed
	case <-time.After(100 * time.Millisecond):
		t.Error("Operation did not complete after context cancellation")
	}
}

// TestRuntimeClient_ErrorHandling tests error scenarios
func TestRuntimeClient_ErrorHandling(t *testing.T) {
	tests := []struct {
		name        string
		client      *mockRuntimeClient
		operation   string
		expectError bool
	}{
		{
			name: "ListContainers error",
			client: &mockRuntimeClient{
				shouldError: true,
			},
			operation:   "list",
			expectError: true,
		},
		{
			name: "WatchEvents error",
			client: &mockRuntimeClient{
				shouldError: true,
			},
			operation:   "watch",
			expectError: true,
		},
		{
			name: "Close error",
			client: &mockRuntimeClient{
				shouldError: true,
			},
			operation:   "close",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			switch tt.operation {
			case "list":
				_, err := tt.client.ListContainers(ctx)
				if tt.expectError {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
				}
			case "watch":
				_, err := tt.client.WatchEvents(ctx)
				if tt.expectError {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
				}
			case "close":
				err := tt.client.Close()
				if tt.expectError {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
				}
			}
		})
	}
}

// TestRuntimeClient_OOMDetection tests OOM event detection
func TestRuntimeClient_OOMDetection(t *testing.T) {
	client := &mockRuntimeClient{
		events: []ContainerEvent{
			{
				Type: ContainerEventOOM,
				Container: Container{
					ID:  "oom-container",
					PID: 5555,
					Labels: map[string]string{
						"app": "memory-hungry",
					},
				},
				Timestamp: time.Now(),
			},
		},
	}

	ctx := context.Background()
	events, err := client.WatchEvents(ctx)
	require.NoError(t, err)

	oomDetected := false
	for event := range events {
		if event.Type == ContainerEventOOM {
			oomDetected = true
			assert.Equal(t, "oom-container", event.Container.ID)
			assert.Equal(t, uint32(5555), event.Container.PID)
		}
	}

	assert.True(t, oomDetected, "OOM event should be detected")
}

// TestRuntimeClient_ContainerMetadata tests metadata extraction
func TestRuntimeClient_ContainerMetadata(t *testing.T) {
	client := &mockRuntimeClient{
		containers: []Container{
			{
				ID:       "k8s-container",
				PID:      7777,
				CgroupID: 123456789,
				Labels: map[string]string{
					"io.kubernetes.pod.name":      "test-pod",
					"io.kubernetes.pod.namespace": "default",
					"io.kubernetes.pod.uid":       "uid-123",
					"app":                         "test-app",
				},
				Namespace: "k8s",
				Runtime:   "containerd",
			},
		},
	}

	ctx := context.Background()
	containers, err := client.ListContainers(ctx)
	require.NoError(t, err)
	require.Len(t, containers, 1)

	container := containers[0]
	assert.Equal(t, "k8s-container", container.ID)
	assert.Equal(t, uint32(7777), container.PID)
	assert.Equal(t, uint64(123456789), container.CgroupID)
	assert.Equal(t, "test-pod", container.Labels["io.kubernetes.pod.name"])
	assert.Equal(t, "default", container.Labels["io.kubernetes.pod.namespace"])
	assert.Equal(t, "containerd", container.Runtime)
}

// TestMapUpdater tests eBPF map updates
func TestMapUpdater(t *testing.T) {
	updater := &mockMapUpdater{
		updateCount: 0,
	}

	containers := []Container{
		{ID: "c1", PID: 1001},
		{ID: "c2", PID: 1002},
		{ID: "c3", PID: 1003},
	}

	err := updater.UpdateMaps(containers)
	assert.NoError(t, err)
	assert.Equal(t, 1, updater.updateCount)
	assert.Equal(t, 3, updater.lastUpdateSize)
}

// mockMapUpdater is a test implementation of MapUpdater
type mockMapUpdater struct {
	updateCount    int
	lastUpdateSize int
	shouldError    bool
}

func (m *mockMapUpdater) UpdateMaps(containers []Container) error {
	if m.shouldError {
		return fmt.Errorf("mock update error")
	}
	m.updateCount++
	m.lastUpdateSize = len(containers)
	return nil
}

// TestRuntimeClient_Lifecycle tests client lifecycle
func TestRuntimeClient_Lifecycle(t *testing.T) {
	client := &mockRuntimeClient{
		containers: []Container{{ID: "test"}},
		closed:     false,
	}

	// Client should not be closed initially
	assert.False(t, client.closed)

	// List containers should work
	ctx := context.Background()
	containers, err := client.ListContainers(ctx)
	assert.NoError(t, err)
	assert.Len(t, containers, 1)

	// Close client
	err = client.Close()
	assert.NoError(t, err)
	assert.True(t, client.closed)
}

// TestRuntimeClient_EventTypes tests different event types
func TestRuntimeClient_EventTypes(t *testing.T) {
	allEventTypes := []ContainerEventType{
		ContainerEventStart,
		ContainerEventStop,
		ContainerEventDie,
		ContainerEventOOM,
	}

	var events []ContainerEvent
	for i, eventType := range allEventTypes {
		events = append(events, ContainerEvent{
			Type: eventType,
			Container: Container{
				ID:  fmt.Sprintf("container-%d", i),
				PID: uint32(1000 + i),
			},
			Timestamp: time.Now(),
		})
	}

	client := &mockRuntimeClient{
		events: events,
	}

	ctx := context.Background()
	eventCh, err := client.WatchEvents(ctx)
	require.NoError(t, err)

	var receivedTypes []ContainerEventType
	for event := range eventCh {
		receivedTypes = append(receivedTypes, event.Type)
	}

	assert.Equal(t, allEventTypes, receivedTypes)
}
