package performance

import (
	"encoding/json"
	"errors"

	"github.com/yairfalse/tapio/pkg/domain"
)

// PerCPUEventBuffer provides a type-safe per-CPU buffer for UnifiedEvent processing.
// This combines the performance benefits of per-CPU buffers with the convenience
// of working directly with domain events.
type PerCPUEventBuffer struct {
	buffer    *PerCPUBuffer
	eventPool *UnifiedEventPool
	bytePool  *ByteSlicePool
}

// PerCPUEventBufferConfig configures the per-CPU event buffer
type PerCPUEventBufferConfig struct {
	BufferSizePerCPU int    // Size of each CPU buffer in bytes (default: 256KB)
	OverflowSize     uint64 // Size of overflow buffer (default: 4MB)
	EnablePooling    bool   // Use object pooling for events
}

// NewPerCPUEventBuffer creates a new per-CPU event buffer
func NewPerCPUEventBuffer(config PerCPUEventBufferConfig) (*PerCPUEventBuffer, error) {
	if config.BufferSizePerCPU == 0 {
		config.BufferSizePerCPU = 256 * 1024 // 256KB per CPU for events
	}
	if config.OverflowSize == 0 {
		config.OverflowSize = 4 * 1024 * 1024 // 4MB overflow
	}

	// Create per-CPU buffer with event aggregator
	buffer, err := NewPerCPUBuffer(PerCPUBufferConfig{
		BufferSize:   config.BufferSizePerCPU,
		OverflowSize: config.OverflowSize,
		Aggregator:   &EventAggregator{},
	})
	if err != nil {
		return nil, err
	}

	pceb := &PerCPUEventBuffer{
		buffer: buffer,
	}

	if config.EnablePooling {
		pceb.eventPool = NewUnifiedEventPool()
		pceb.bytePool = NewByteSlicePool()
	}

	return pceb, nil
}

// Put adds an event to the current CPU's buffer
func (pceb *PerCPUEventBuffer) Put(event *domain.UnifiedEvent) error {
	if event == nil {
		return errors.New("cannot put nil event")
	}

	// Serialize event to bytes
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	// Write to per-CPU buffer
	return pceb.buffer.Write(data)
}

// PutBatch adds multiple events efficiently
func (pceb *PerCPUEventBuffer) PutBatch(events []*domain.UnifiedEvent) (int, error) {
	added := 0
	for _, event := range events {
		if err := pceb.Put(event); err != nil {
			if added == 0 {
				return 0, err
			}
			break
		}
		added++
	}
	return added, nil
}

// Get retrieves all events from all CPU buffers
func (pceb *PerCPUEventBuffer) Get() ([]*domain.UnifiedEvent, error) {
	// Read all data
	dataBuffers, err := pceb.buffer.Read()
	if err != nil {
		return nil, err
	}

	events := make([]*domain.UnifiedEvent, 0, len(dataBuffers))

	// Deserialize events
	for _, data := range dataBuffers {
		var event *domain.UnifiedEvent

		if pceb.eventPool != nil {
			event = pceb.eventPool.Get()
		} else {
			event = &domain.UnifiedEvent{}
		}

		if err := json.Unmarshal(data, event); err != nil {
			if pceb.eventPool != nil {
				pceb.eventPool.Put(event)
			}
			continue // Skip corrupted events
		}

		events = append(events, event)
	}

	return events, nil
}

// GetFromCPU retrieves events from a specific CPU's buffer
func (pceb *PerCPUEventBuffer) GetFromCPU(cpu int) ([]*domain.UnifiedEvent, error) {
	if cpu < 0 || cpu >= pceb.buffer.numCPU {
		return nil, errors.New("invalid CPU index")
	}

	// Read from specific CPU
	dataBuffers := pceb.buffer.readFromBuffer(&pceb.buffer.buffers[cpu])

	events := make([]*domain.UnifiedEvent, 0, len(dataBuffers))

	for _, data := range dataBuffers {
		var event *domain.UnifiedEvent

		if pceb.eventPool != nil {
			event = pceb.eventPool.Get()
		} else {
			event = &domain.UnifiedEvent{}
		}

		if err := json.Unmarshal(data, event); err != nil {
			if pceb.eventPool != nil {
				pceb.eventPool.Put(event)
			}
			continue
		}

		events = append(events, event)
	}

	return events, nil
}

// Process applies a function to all events without removing them
func (pceb *PerCPUEventBuffer) Process(fn func(*domain.UnifiedEvent) error) error {
	events, err := pceb.Get()
	if err != nil {
		return err
	}

	for _, event := range events {
		if err := fn(event); err != nil {
			// Return events to pool if enabled
			if pceb.eventPool != nil {
				for _, e := range events {
					pceb.eventPool.Put(e)
				}
			}
			return err
		}
	}

	// Return events to pool if enabled
	if pceb.eventPool != nil {
		for _, event := range events {
			pceb.eventPool.Put(event)
		}
	}

	return nil
}

// Reset clears all buffers
func (pceb *PerCPUEventBuffer) Reset() {
	pceb.buffer.Reset()
}

// GetMetrics returns buffer metrics
func (pceb *PerCPUEventBuffer) GetMetrics() PerCPUBufferMetrics {
	return pceb.buffer.GetMetrics()
}

// GetStats returns simplified statistics
func (pceb *PerCPUEventBuffer) GetStats() PerCPUEventStats {
	metrics := pceb.buffer.GetMetrics()

	var totalUsed uint32
	var totalCapacity uint32
	var maxUtilization float64

	for _, cpu := range metrics.CPUMetrics {
		totalUsed += cpu.Used
		totalCapacity += cpu.Capacity
		if cpu.Utilization > maxUtilization {
			maxUtilization = cpu.Utilization
		}
	}

	return PerCPUEventStats{
		TotalWrites:    metrics.Writes,
		TotalReads:     metrics.Reads,
		Overflows:      metrics.Overflows,
		AvgUtilization: float64(totalUsed) / float64(totalCapacity),
		MaxUtilization: maxUtilization,
		NumCPUs:        len(metrics.CPUMetrics),
		OverflowInUse:  metrics.OverflowSize,
	}
}

// PerCPUEventStats provides simplified statistics
type PerCPUEventStats struct {
	TotalWrites    uint64
	TotalReads     uint64
	Overflows      uint64
	AvgUtilization float64
	MaxUtilization float64
	NumCPUs        int
	OverflowInUse  uint64
}

// EventAggregator aggregates event data
type EventAggregator struct{}

// Aggregate combines event data (for now, simple concatenation)
func (a *EventAggregator) Aggregate(buffers [][]byte) ([]byte, error) {
	// For events, we'll return a JSON array
	result := []byte("[")

	for i, buf := range buffers {
		if i > 0 {
			result = append(result, ',')
		}
		result = append(result, buf...)
	}

	result = append(result, ']')
	return result, nil
}
