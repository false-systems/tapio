//go:build linux
// +build linux

package containerruntime

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestRuntimeClient_ListContainers verifies container enumeration
func TestRuntimeClient_ListContainers(t *testing.T) {
	tests := []struct {
		name          string
		setupMock     func() RuntimeClient
		wantCount     int
		wantContainer *Container
		wantErr       bool
	}{
		{
			name: "List running containers",
			setupMock: func() RuntimeClient {
				return &mockRuntimeClient{
					containers: []Container{
						{
							ID:       "abc123",
							PID:      1234,
							CgroupID: 567890,
							Labels: map[string]string{
								"app": "nginx",
							},
						},
						{
							ID:       "def456",
							PID:      5678,
							CgroupID: 123456,
							Labels: map[string]string{
								"app": "redis",
							},
						},
					},
				}
			},
			wantCount: 2,
			wantContainer: &Container{
				ID:  "abc123",
				PID: 1234,
			},
			wantErr: false,
		},
		{
			name: "No containers running",
			setupMock: func() RuntimeClient {
				return &mockRuntimeClient{
					containers: []Container{},
				}
			},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name: "Docker daemon unavailable",
			setupMock: func() RuntimeClient {
				return &mockRuntimeClient{
					shouldError: true,
				}
			},
			wantCount: 0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := tt.setupMock()
			ctx := context.Background()

			containers, err := client.ListContainers(ctx)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Len(t, containers, tt.wantCount)

				if tt.wantContainer != nil && len(containers) > 0 {
					assert.Equal(t, tt.wantContainer.ID, containers[0].ID)
					assert.Equal(t, tt.wantContainer.PID, containers[0].PID)
				}
			}
		})
	}
}

// TestRuntimeClient_WatchEvents verifies event streaming
func TestRuntimeClient_WatchEvents(t *testing.T) {
	tests := []struct {
		name       string
		setupMock  func() RuntimeClient
		wantEvents []ContainerEventType
		wantErr    bool
	}{
		{
			name: "Container start and stop events",
			setupMock: func() RuntimeClient {
				return &mockRuntimeClient{
					events: []ContainerEvent{
						{
							Type: ContainerEventStart,
							Container: Container{
								ID:  "test123",
								PID: 9999,
							},
						},
						{
							Type: ContainerEventStop,
							Container: Container{
								ID: "test123",
							},
						},
					},
				}
			},
			wantEvents: []ContainerEventType{
				ContainerEventStart,
				ContainerEventStop,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := tt.setupMock()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			eventCh, err := client.WatchEvents(ctx)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, eventCh)

				var receivedEvents []ContainerEventType
				for event := range eventCh {
					receivedEvents = append(receivedEvents, event.Type)
					if len(receivedEvents) >= len(tt.wantEvents) {
						break
					}
				}

				assert.Equal(t, tt.wantEvents, receivedEvents)
			}
		})
	}
}

// TestDockerRuntimeClient_Connect verifies Docker connection
func TestDockerRuntimeClient_Connect(t *testing.T) {
	tests := []struct {
		name        string
		socketPath  string
		wantErr     bool
		errContains string
	}{
		{
			name:       "Valid socket path",
			socketPath: "/var/run/docker.sock",
			wantErr:    false,
		},
		{
			name:        "Invalid socket path",
			socketPath:  "/nonexistent/docker.sock",
			wantErr:     true,
			errContains: "failed to connect",
		},
		{
			name:       "Empty socket path uses default",
			socketPath: "",
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Skip if Docker not available
			if !isDockerAvailable() {
				t.Skip("Docker not available")
			}

			client, err := NewDockerClient(tt.socketPath)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, client)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, client)
				if client != nil {
					client.Close()
				}
			}
		})
	}
}

// TestMapUpdater_UpdateMaps verifies eBPF map updates
func TestMapUpdater_UpdateMaps(t *testing.T) {
	tests := []struct {
		name       string
		containers []Container
		wantErr    bool
		checkMap   func(t *testing.T, updater *MapUpdater)
	}{
		{
			name: "Update single container",
			containers: []Container{
				{
					ID:       "test123",
					PID:      1234,
					CgroupID: 5678,
				},
			},
			wantErr: false,
			checkMap: func(t *testing.T, updater *MapUpdater) {
				// Verify PID was added to map
				assert.Contains(t, updater.(*mockMapUpdater).pids, uint32(1234))
				assert.Contains(t, updater.(*mockMapUpdater).cgroupIDs, uint64(5678))
			},
		},
		{
			name: "Update multiple containers",
			containers: []Container{
				{ID: "c1", PID: 100, CgroupID: 1000},
				{ID: "c2", PID: 200, CgroupID: 2000},
				{ID: "c3", PID: 300, CgroupID: 3000},
			},
			wantErr: false,
			checkMap: func(t *testing.T, updater *MapUpdater) {
				mock := updater.(*mockMapUpdater)
				assert.Len(t, mock.pids, 3)
				assert.Contains(t, mock.pids, uint32(100))
				assert.Contains(t, mock.pids, uint32(200))
				assert.Contains(t, mock.pids, uint32(300))
			},
		},
		{
			name:       "Empty container list",
			containers: []Container{},
			wantErr:    false,
			checkMap: func(t *testing.T, updater *MapUpdater) {
				mock := updater.(*mockMapUpdater)
				assert.Empty(t, mock.pids)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updater := &mockMapUpdater{
				pids:      make(map[uint32]bool),
				cgroupIDs: make(map[uint64]bool),
			}

			err := updater.UpdateMaps(tt.containers)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.checkMap != nil {
					tt.checkMap(t, updater)
				}
			}
		})
	}
}

// Test helpers

type mockRuntimeClient struct {
	containers  []Container
	events      []ContainerEvent
	shouldError bool
}

func (m *mockRuntimeClient) ListContainers(ctx context.Context) ([]Container, error) {
	if m.shouldError {
		return nil, assert.AnError
	}
	return m.containers, nil
}

func (m *mockRuntimeClient) WatchEvents(ctx context.Context) (<-chan ContainerEvent, error) {
	if m.shouldError {
		return nil, assert.AnError
	}
	ch := make(chan ContainerEvent)
	go func() {
		defer close(ch)
		for _, event := range m.events {
			select {
			case ch <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

func (m *mockRuntimeClient) Close() error {
	return nil
}

type mockMapUpdater struct {
	pids      map[uint32]bool
	cgroupIDs map[uint64]bool
}

func (m *mockMapUpdater) UpdateMaps(containers []Container) error {
	for _, c := range containers {
		m.pids[c.PID] = true
		m.cgroupIDs[c.CgroupID] = true
	}
	return nil
}

func isDockerAvailable() bool {
	// Check if Docker socket exists
	_, err := os.Stat("/var/run/docker.sock")
	return err == nil
}
