//go:build linux && ebpf
// +build linux,ebpf

package ebpf

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// FallbackCollector wraps the real eBPF collector with a fallback to enhanced collector
type FallbackCollector struct {
	realCollector     *Collector
	enhancedCollector *EnhancedCollector
	useEnhanced       bool
	events            chan *MemoryEvent
	processStats      map[uint32]*ProcessMemoryStats
	statsMutex        sync.RWMutex
	ctx               context.Context
	cancel            context.CancelFunc
	wg                sync.WaitGroup
}

// NewCollectorWithFallback creates a new collector that falls back to enhanced if eBPF fails
func NewCollectorWithFallback() (*FallbackCollector, error) {
	fc := &FallbackCollector{
		events:       make(chan *MemoryEvent, 1000),
		processStats: make(map[uint32]*ProcessMemoryStats),
	}

	// Try to create real eBPF collector
	realCollector, err := NewCollectorDirect()
	if err != nil {
		// Fall back to enhanced collector
		enhancedCollector, err := NewEnhancedCollector()
		if err != nil {
			return nil, fmt.Errorf("failed to create both eBPF and enhanced collectors: %w", err)
		}
		fc.enhancedCollector = enhancedCollector
		fc.useEnhanced = true
		fmt.Printf("eBPF collector initialization failed, using enhanced collector: %v\n", err)
	} else {
		fc.realCollector = realCollector
		fc.useEnhanced = false
	}

	return fc, nil
}

// Start starts the collector
func (c *FallbackCollector) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	if c.useEnhanced {
		if err := c.enhancedCollector.Start(); err != nil {
			return err
		}
		// Convert enhanced collector events to MemoryEvents
		c.wg.Add(1)
		go c.convertEnhancedEvents()
	} else {
		// Use real collector's event processing
		c.wg.Add(1)
		go c.forwardRealEvents()
	}

	return nil
}

// Close stops the collector and cleans up resources
func (c *FallbackCollector) Close() error {
	if c.cancel != nil {
		c.cancel()
	}

	c.wg.Wait()

	if c.useEnhanced {
		return c.enhancedCollector.Stop()
	} else {
		return c.realCollector.Close()
	}
}

// GetProcessStats returns current statistics for all tracked processes
func (c *FallbackCollector) GetProcessStats() map[uint32]*ProcessMemoryStats {
	if c.useEnhanced {
		return c.enhancedCollector.GetProcessStats()
	}
	return c.realCollector.GetProcessStats()
}

// GetMemoryPredictions returns OOM predictions for all tracked processes
func (c *FallbackCollector) GetMemoryPredictions(memoryLimits map[uint32]uint64) map[uint32]*OOMPrediction {
	if c.useEnhanced {
		return c.enhancedCollector.GetMemoryPredictions(memoryLimits)
	}
	return c.realCollector.GetMemoryPredictions(memoryLimits)
}

// CollectEvents triggers event collection
func (c *FallbackCollector) CollectEvents() {
	if c.useEnhanced {
		// Enhanced collector runs continuously
		return
	}
	c.realCollector.CollectEvents()
}

// convertEnhancedEvents converts SystemEvents to MemoryEvents
func (c *FallbackCollector) convertEnhancedEvents() {
	defer c.wg.Done()

	for {
		select {
		case <-c.ctx.Done():
			return
		case event := <-c.enhancedCollector.GetEventChannel():
			// Convert SystemEvent to MemoryEvent
			if memEvent := c.convertSystemEvent(event); memEvent != nil {
				select {
				case c.events <- memEvent:
				default:
					// Drop if channel full
				}
			}
		}
	}
}

// forwardRealEvents forwards events from the real collector
func (c *FallbackCollector) forwardRealEvents() {
	defer c.wg.Done()

	for {
		select {
		case <-c.ctx.Done():
			return
		case event, ok := <-c.realCollector.events:
			if !ok {
				return
			}
			select {
			case c.events <- event:
			default:
				// Drop if channel full
			}
		}
	}
}

// convertSystemEvent converts a SystemEvent to a MemoryEvent
func (c *FallbackCollector) convertSystemEvent(event SystemEvent) *MemoryEvent {
	switch event.Type {
	case "memory_spike", "memory_pressure":
		data, ok := event.Data.(map[string]interface{})
		if !ok {
			return nil
		}

		memEvent := &MemoryEvent{
			Timestamp: event.Timestamp,
			PID:       event.PID,
			EventType: EventMemoryAlloc,
		}

		if cmd, ok := data["command"].(string); ok {
			memEvent.Command = cmd
		}
		if rss, ok := data["current_rss"].(uint64); ok {
			memEvent.TotalMemory = rss
		}
		if growth, ok := data["memory_growth"].(int64); ok {
			memEvent.Size = uint64(growth)
		}
		if inContainer, ok := data["in_container"].(bool); ok {
			memEvent.InContainer = inContainer
		}

		return memEvent
	}

	return nil
}