//go:build linux
// +build linux

package containerruntime

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"go.uber.org/zap"
)

// ContainerEventType represents container lifecycle events
type ContainerEventType string

const (
	ContainerEventStart ContainerEventType = "start"
	ContainerEventStop  ContainerEventType = "stop"
	ContainerEventDie   ContainerEventType = "die"
	ContainerEventOOM   ContainerEventType = "oom"
)

// Container represents a running container with metadata
type Container struct {
	ID        string
	PID       uint32
	CgroupID  uint64
	Labels    map[string]string
	Namespace string
	Runtime   string
}

// ContainerEvent represents a container lifecycle event
type ContainerEvent struct {
	Type      ContainerEventType
	Container Container
	Timestamp time.Time
}

// RuntimeClient interface for container runtime operations
type RuntimeClient interface {
	ListContainers(ctx context.Context) ([]Container, error)
	WatchEvents(ctx context.Context) (<-chan ContainerEvent, error)
	Close() error
}

// MapUpdater interface for updating eBPF maps
type MapUpdater interface {
	UpdateMaps(containers []Container) error
}

// DockerClient implements RuntimeClient for Docker
type DockerClient struct {
	client *client.Client
	logger *zap.Logger
}

// NewDockerClient creates a new Docker runtime client
func NewDockerClient(socketPath string) (*DockerClient, error) {
	var opts []client.Opt

	if socketPath != "" {
		opts = append(opts, client.WithHost("unix://"+socketPath))
	} else {
		opts = append(opts, client.FromEnv)
	}

	opts = append(opts, client.WithAPIVersionNegotiation())

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if _, err := cli.Ping(ctx); err != nil {
		cli.Close()
		return nil, fmt.Errorf("failed to connect to Docker daemon: %w", err)
	}

	return &DockerClient{
		client: cli,
		logger: zap.NewNop(), // Will be set by observer
	}, nil
}

// SetLogger sets the logger for the client
func (d *DockerClient) SetLogger(logger *zap.Logger) {
	d.logger = logger
}

// ListContainers returns all running containers
func (d *DockerClient) ListContainers(ctx context.Context) ([]Container, error) {
	// Only list running containers
	listOpts := container.ListOptions{
		All: false, // Only running containers
		Filters: filters.NewArgs(
			filters.Arg("status", "running"),
		),
	}

	containers, err := d.client.ContainerList(ctx, listOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	result := make([]Container, 0, len(containers))

	for _, c := range containers {
		// Get detailed inspect data for PID
		inspect, err := d.client.ContainerInspect(ctx, c.ID)
		if err != nil {
			d.logger.Warn("Failed to inspect container",
				zap.String("container_id", c.ID),
				zap.Error(err))
			continue
		}

		// Skip containers without PID (not yet started)
		if inspect.State.Pid == 0 {
			continue
		}

		// Extract cgroup ID from cgroup path
		cgroupID := extractCgroupIDFromInspect(inspect)

		result = append(result, Container{
			ID:        c.ID[:12], // Short ID
			PID:       uint32(inspect.State.Pid),
			CgroupID:  cgroupID,
			Labels:    c.Labels,
			Namespace: c.Labels["io.kubernetes.pod.namespace"],
			Runtime:   "docker",
		})
	}

	return result, nil
}

// WatchEvents watches for container lifecycle events
func (d *DockerClient) WatchEvents(ctx context.Context) (<-chan ContainerEvent, error) {
	// Set up event filters
	eventFilters := filters.NewArgs(
		filters.Arg("type", "container"),
		filters.Arg("event", "start"),
		filters.Arg("event", "stop"),
		filters.Arg("event", "die"),
		filters.Arg("event", "oom"),
	)

	eventOpts := events.ListOptions{
		Filters: eventFilters,
	}

	dockerEvents, errors := d.client.Events(ctx, eventOpts)

	// Create our event channel
	eventCh := make(chan ContainerEvent, 100)

	go func() {
		defer close(eventCh)

		for {
			select {
			case event, ok := <-dockerEvents:
				if !ok {
					return
				}

				// Convert Docker event to our format
				containerEvent := d.dockerEventToContainerEvent(ctx, event)
				if containerEvent != nil {
					select {
					case eventCh <- *containerEvent:
					case <-ctx.Done():
						return
					}
				}

			case err, ok := <-errors:
				if !ok {
					return
				}
				d.logger.Error("Docker event error", zap.Error(err))

			case <-ctx.Done():
				return
			}
		}
	}()

	return eventCh, nil
}

// Close closes the Docker client connection
func (d *DockerClient) Close() error {
	if d.client != nil {
		return d.client.Close()
	}
	return nil
}

// dockerEventToContainerEvent converts Docker events to our format
func (d *DockerClient) dockerEventToContainerEvent(ctx context.Context, event events.Message) *ContainerEvent {
	var eventType ContainerEventType

	switch string(event.Action) {
	case "start":
		eventType = ContainerEventStart
	case "stop":
		eventType = ContainerEventStop
	case "die":
		eventType = ContainerEventDie
	case "oom":
		eventType = ContainerEventOOM
	default:
		return nil
	}

	containerID := event.Actor.ID[:12] // Short ID

	// For start events, get container details
	var pid uint32
	var cgroupID uint64

	if eventType == ContainerEventStart {
		inspect, err := d.client.ContainerInspect(ctx, containerID)
		if err != nil {
			d.logger.Warn("Failed to inspect container on start",
				zap.String("container_id", containerID),
				zap.Error(err))
			return nil
		}

		if inspect.State.Pid > 0 {
			pid = uint32(inspect.State.Pid)
			cgroupID = extractCgroupIDFromInspect(inspect)
		}
	}

	return &ContainerEvent{
		Type: eventType,
		Container: Container{
			ID:        containerID,
			PID:       pid,
			CgroupID:  cgroupID,
			Labels:    event.Actor.Attributes,
			Namespace: event.Actor.Attributes["io.kubernetes.pod.namespace"],
			Runtime:   "docker",
		},
		Timestamp: time.Unix(event.Time, event.TimeNano),
	}
}

// extractCgroupIDFromInspect extracts cgroup ID from container inspect data
func extractCgroupIDFromInspect(inspect types.ContainerJSON) uint64 {
	// Try to extract from CgroupParent or HostConfig
	cgroupPath := inspect.HostConfig.CgroupParent
	if cgroupPath == "" {
		// Try to construct from container ID
		cgroupPath = fmt.Sprintf("/docker/%s", inspect.ID)
	}

	// Parse cgroup path to get numeric ID
	// In production, this would use actual kernel cgroup ID
	return hashCgroupPath(cgroupPath)
}

// hashCgroupPath creates a numeric ID from cgroup path
func hashCgroupPath(path string) uint64 {
	var hash uint64
	for i, c := range path {
		hash = hash*31 + uint64(c)
		hash = hash ^ uint64(i)
	}
	return hash
}

// ContainerdClient implements RuntimeClient for containerd
// This is a placeholder for containerd support
type ContainerdClient struct {
	socketPath string
	logger     *zap.Logger
}

// NewContainerdClient creates a new containerd runtime client
func NewContainerdClient(socketPath string) (*ContainerdClient, error) {
	if socketPath == "" {
		socketPath = "/run/containerd/containerd.sock"
	}

	// Check if socket exists
	if _, err := os.Stat(socketPath); err != nil {
		return nil, fmt.Errorf("containerd socket not found: %w", err)
	}

	return &ContainerdClient{
		socketPath: socketPath,
		logger:     zap.NewNop(),
	}, nil
}

// ListContainers implements RuntimeClient for containerd
func (c *ContainerdClient) ListContainers(ctx context.Context) ([]Container, error) {
	// Full containerd implementation would go here
	// For now, return empty to satisfy interface
	return []Container{}, nil
}

// WatchEvents implements RuntimeClient for containerd
func (c *ContainerdClient) WatchEvents(ctx context.Context) (<-chan ContainerEvent, error) {
	ch := make(chan ContainerEvent)
	close(ch) // Close immediately as not implemented
	return ch, nil
}

// Close implements RuntimeClient for containerd
func (c *ContainerdClient) Close() error {
	return nil
}

// AutoDetectRuntime detects and creates appropriate runtime client
func AutoDetectRuntime() (RuntimeClient, error) {
	// Try Docker first
	if _, err := os.Stat("/var/run/docker.sock"); err == nil {
		return NewDockerClient("")
	}

	// Try containerd
	if _, err := os.Stat("/run/containerd/containerd.sock"); err == nil {
		return NewContainerdClient("")
	}

	return nil, fmt.Errorf("no container runtime detected")
}
