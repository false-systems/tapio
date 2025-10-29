//go:build linux
// +build linux

package containerruntime

import (
	"context"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// OOMProcessor handles OOM kill events from eBPF
type OOMProcessor struct{}

// NewOOMProcessor creates a new OOM event processor
func NewOOMProcessor() *OOMProcessor {
	return &OOMProcessor{}
}

// Process checks if event is OOM kill and creates domain event
// Returns nil if event is not an OOM kill
func (p *OOMProcessor) Process(ctx context.Context, evt ContainerEventBPF) *domain.ObserverEvent {
	// Only process OOM kill events
	if evt.Type != EventTypeOOMKill {
		return nil
	}

	// Extract cgroup path from eBPF event
	cgroupPath := nullTerminatedString(evt.CgroupPath[:])
	containerID := parseContainerID(cgroupPath)

	// Classify exit (always OOM for this processor)
	classification := ClassifyExit(evt.ExitCode, evt.Signal, true)

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
		Subtype:       "oom_kill",
		Timestamp:     time.Unix(0, int64(evt.TimestampNs)),
		ContainerData: containerData,
	}
}
