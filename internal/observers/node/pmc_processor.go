//go:build linux

package node

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yairfalse/tapio/pkg/domain"
)

const (
	// maxCounter48bit is the maximum value for 48-bit PMC counters
	// Used for overflow detection and delta calculation
	maxCounter48bit uint64 = (1 << 48) - 1
)

// PMCEvent represents a single PMC sample from eBPF
type PMCEvent struct {
	CPU          uint32
	Cycles       uint64
	Instructions uint64
	StallCycles  uint64
	Timestamp    uint64
}

// PMCSample stores previous sample for delta calculation
type PMCSample struct {
	Cycles       uint64
	Instructions uint64
	StallCycles  uint64
	Timestamp    uint64
}

// PMCProcessor calculates IPC and memory stalls from PMC events.
type PMCProcessor struct {
	mu          sync.Mutex
	lastSample  map[uint32]*PMCSample
	lastEmitted map[uint32]string // Track last emitted impact per CPU
	nodeName    string
}

// NewPMCProcessor creates a new PMC processor
func NewPMCProcessor(nodeName string) *PMCProcessor {
	return &PMCProcessor{
		lastSample:  make(map[uint32]*PMCSample),
		lastEmitted: make(map[uint32]string),
		nodeName:    nodeName,
	}
}

// Process calculates IPC and stall percentage from PMC event
func (p *PMCProcessor) Process(ctx context.Context, event PMCEvent) *domain.ObserverEvent {
	p.mu.Lock()
	defer p.mu.Unlock()

	cpu := event.CPU

	// Get previous sample for this CPU
	prev := p.lastSample[cpu]
	if prev == nil {
		// First sample, just store it
		p.lastSample[cpu] = &PMCSample{
			Cycles:       event.Cycles,
			Instructions: event.Instructions,
			StallCycles:  event.StallCycles,
			Timestamp:    event.Timestamp,
		}
		return nil
	}

	// Calculate deltas (PMC counters are cumulative)
	// Handle 48-bit counter wraparound
	deltaCycles := calculateDelta(event.Cycles, prev.Cycles)
	deltaInstructions := calculateDelta(event.Instructions, prev.Instructions)
	deltaStalls := calculateDelta(event.StallCycles, prev.StallCycles)

	// Avoid division by zero
	if deltaCycles == 0 {
		return nil
	}

	// Calculate IPC (Instructions Per Cycle)
	ipc := float64(deltaInstructions) / float64(deltaCycles)

	// Calculate stall percentage
	stallPct := float64(deltaStalls) / float64(deltaCycles) * 100.0

	// Update last sample
	p.lastSample[cpu] = &PMCSample{
		Cycles:       event.Cycles,
		Instructions: event.Instructions,
		StallCycles:  event.StallCycles,
		Timestamp:    event.Timestamp,
	}

	// Classify performance impact
	impact := p.classifyImpact(ipc, stallPct)

	// Only emit event if impact state changed
	lastImpact, exists := p.lastEmitted[cpu]

	if exists {
		// Not first event - only emit if impact changed
		if lastImpact == impact {
			return nil // No state change, skip emission
		}
	} else {
		// First event for this CPU
		if impact == "" {
			// First event is healthy, skip emission but track state
			p.lastEmitted[cpu] = impact
			return nil
		}
		// First event shows degradation - emit it!
	}

	// State changed OR first degraded event - update tracking and emit
	p.lastEmitted[cpu] = impact

	// Determine subtype based on impact severity
	subtype := p.determineSubtype(impact)

	return &domain.ObserverEvent{
		ID:        uuid.NewString(),
		Type:      "node",
		Subtype:   subtype,
		Source:    p.nodeName + "-pmc",
		Timestamp: time.Now(),
		NodeData: &domain.NodeEventData{
			CPUIPC:            ipc,
			MemoryStalls:      stallPct,
			PerformanceImpact: impact,
		},
	}
}

// classifyImpact determines performance impact level
func (p *PMCProcessor) classifyImpact(ipc, stallPct float64) string {
	// Critical: IPC <= 0.2 + Stalls > 70%
	if ipc <= 0.2 && stallPct > 70.0 {
		return "critical"
	}

	// High: IPC <= 0.3 + Stalls > 50%
	if ipc <= 0.3 && stallPct > 50.0 {
		return "high"
	}

	// Medium: IPC <= 0.5 + Stalls > 30%
	if ipc <= 0.5 && stallPct > 30.0 {
		return "medium"
	}

	// Low: IPC <= 0.7 + Stalls > 20%
	if ipc <= 0.7 && stallPct > 20.0 {
		return "low"
	}

	// No significant degradation
	return ""
}

// determineSubtype maps impact level to event subtype
func (p *PMCProcessor) determineSubtype(impact string) string {
	switch impact {
	case "critical":
		return "node_critical_memory_bottleneck"
	case "high":
		return "node_memory_bottleneck"
	default:
		return "node_performance_degradation"
	}
}

// calculateDelta computes delta between current and previous counter values
// Handles 48-bit counter wraparound (PMC hardware limitation)
func calculateDelta(current, previous uint64) uint64 {
	if current >= previous {
		return current - previous
	}

	// Counter wrapped around (48-bit hardware counter)
	// Example: previous=max, current=1000 → delta = (max - previous + 1) + current = 1001
	return (maxCounter48bit - previous + 1) + current
}
