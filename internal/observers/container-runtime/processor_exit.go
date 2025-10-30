//go:build linux

package containerruntime

import (
	"context"
	"time"

	"github.com/yairfalse/tapio/internal/observers/container"
	"github.com/yairfalse/tapio/pkg/domain"
)

// ExitProcessor handles container exit events from eBPF
type ExitProcessor struct{}

// NewExitProcessor creates a new exit event processor
func NewExitProcessor() *ExitProcessor {
	return &ExitProcessor{}
}

// Process checks if event is container exit and creates domain event
// Returns nil if event is OOM kill (handled by OOMProcessor)
func (p *ExitProcessor) Process(ctx context.Context, evt container.ContainerEventBPF) *domain.ObserverEvent {
	// Only process exit events (not OOM kills)
	if evt.Type != container.EventTypeExit {
		return nil
	}

	// Extract cgroup path from eBPF event
	cgroupPath := container.NullTerminatedString(evt.CgroupPath[:])
	containerID := container.ParseContainerID(cgroupPath)

	// Classify exit (not OOM for this processor)
	classification := container.ClassifyExit(evt.ExitCode, evt.Signal, false)

	// Create container event data
	containerData := &domain.ContainerEventData{
		ContainerID: containerID,
		PID:         evt.PID,
		ExitCode:    evt.ExitCode,
		Signal:      evt.Signal,
		Category:    string(classification.Category),
		Evidence:    classification.Evidence,
		MemoryLimit: int64(evt.MemoryLimit),
		MemoryUsage: int64(evt.MemoryUsage),
		CgroupPath:  cgroupPath,
	}

	// Create domain event
	return &domain.ObserverEvent{
		Type:          string(domain.EventTypeContainer),
		Subtype:       "container_exit",
		Timestamp:     time.Unix(0, int64(evt.TimestampNs)),
		ContainerData: containerData,
	}
}
