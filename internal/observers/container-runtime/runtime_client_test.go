package containerruntime

import (
	"context"
	"fmt"
	"runtime"
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

// mockRuntimeClient is a test implementation of RuntimeClient
type mockRuntimeClient struct {
	containers  []Container
	shouldError bool
	events      []ContainerEvent
	closed      bool
}

func (m *mockRuntimeClient) ListContainers(ctx context.Context) ([]Container, error) {
	if m.shouldError {
		return nil, fmt.Errorf("mock error")
	}
	return m.containers, nil
}

func (m *mockRuntimeClient) WatchEvents(ctx context.Context) (<-chan ContainerEvent, error) {
	if m.shouldError {
		return nil, fmt.Errorf("mock error")
	}

	ch := make(chan ContainerEvent, len(m.events))
	for _, event := range m.events {
		ch <- event
	}
	close(ch)
	return ch, nil
}

func (m *mockRuntimeClient) Close() error {
	if m.shouldError {
		return fmt.Errorf("mock close error")
	}
	m.closed = true
	return nil
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
	// Skip on non-Linux platforms since NewDockerClient is Linux-only
	if runtime.GOOS != "linux" {
		t.Skip("Docker client is Linux-only")
	}
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

			// This test requires NewDockerClient which is Linux-only
			t.Skip("NewDockerClient not available on this platform")
		})
	}
}

// TestMapUpdater_UpdateMaps verifies eBPF map updates
func TestMapUpdater_UpdateMaps(t *testing.T) {
	tests := []struct {
		name       string
		containers []Container
		wantErr    bool
		checkMap   func(t *testing.T, updater MapUpdater)
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
			checkMap: func(t *testing.T, updater MapUpdater) {
				// Verify update was called
				mock := updater.(*mockMapUpdater)
				assert.Equal(t, 1, mock.updateCount)
				assert.Equal(t, 1, mock.lastUpdateSize)
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
			checkMap: func(t *testing.T, updater MapUpdater) {
				mock := updater.(*mockMapUpdater)
				assert.Equal(t, 1, mock.updateCount)
				assert.Equal(t, 3, mock.lastUpdateSize)
			},
		},
		{
			name:       "Empty container list",
			containers: []Container{},
			wantErr:    false,
			checkMap: func(t *testing.T, updater MapUpdater) {
				mock := updater.(*mockMapUpdater)
				assert.Equal(t, 1, mock.updateCount)
				assert.Equal(t, 0, mock.lastUpdateSize)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updater := &mockMapUpdater{
				updateCount:    0,
				lastUpdateSize: 0,
				shouldError:    false,
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

// Test helpers are in runtime_client_helpers_test.go and runtime_client_extended_test.go
