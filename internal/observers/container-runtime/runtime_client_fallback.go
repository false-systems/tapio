//go:build !linux
// +build !linux

package containerruntime

import (
	"context"
	"fmt"
	"time"
)

// Fallback types for non-Linux systems

// ContainerEventType represents container lifecycle events (fallback)
type ContainerEventType string

const (
	ContainerEventStart ContainerEventType = "start"
	ContainerEventStop  ContainerEventType = "stop"
	ContainerEventDie   ContainerEventType = "die"
	ContainerEventOOM   ContainerEventType = "oom"
)

// Container represents a running container with metadata (fallback)
type Container struct {
	ID        string
	PID       uint32
	CgroupID  uint64
	Labels    map[string]string
	Namespace string
	Runtime   string
}

// ContainerEvent represents a container lifecycle event (fallback)
type ContainerEvent struct {
	Type      ContainerEventType
	Container Container
	Timestamp time.Time
}

// RuntimeClient interface for container runtime operations (fallback)
type RuntimeClient interface {
	ListContainers(ctx context.Context) ([]Container, error)
	WatchEvents(ctx context.Context) (<-chan ContainerEvent, error)
	Close() error
}

// MapUpdater interface for updating eBPF maps (fallback)
type MapUpdater interface {
	UpdateMaps(containers []Container) error
}

// AutoDetectRuntime returns nil on non-Linux systems
func AutoDetectRuntime() (RuntimeClient, error) {
	return nil, fmt.Errorf("container runtime detection not supported on this platform")
}
