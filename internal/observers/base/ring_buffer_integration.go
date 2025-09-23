package base

import (
	"context"
	"fmt"

	"github.com/yairfalse/tapio/pkg/domain"
)

// StartRingBuffer starts the ring buffer processing if enabled
func (bc *BaseObserver) StartRingBuffer(ctx context.Context) {
	if bc.ringBuffer != nil {
		bc.ringBuffer.Start(ctx)
	}
}

// StopRingBuffer stops the ring buffer processing
func (bc *BaseObserver) StopRingBuffer() {
	if bc.ringBuffer != nil {
		bc.ringBuffer.Stop()
	}
}

// WriteToRingBuffer writes an event to the ring buffer if enabled
// Falls back to returning false if ring buffer is not enabled
func (bc *BaseObserver) WriteToRingBuffer(event *domain.CollectorEvent) bool {
	if bc.ringBuffer != nil {
		success := bc.ringBuffer.Write(event)
		if success {
			bc.RecordEvent()
		} else {
			bc.RecordDrop()
		}
		return success
	}
	return false
}

// RegisterLocalConsumer adds a local consumer for events
// Only works if ring buffer is enabled
func (bc *BaseObserver) RegisterLocalConsumer(consumer LocalConsumer) error {
	if bc.ringBuffer == nil {
		return fmt.Errorf("ring buffer not enabled for observer %s", bc.name)
	}
	bc.ringBuffer.RegisterLocalConsumer(consumer)
	return nil
}

// GetRingBufferStats returns ring buffer statistics if enabled
func (bc *BaseObserver) GetRingBufferStats() *RingBufferStats {
	if bc.ringBuffer != nil {
		stats := bc.ringBuffer.Statistics()
		return &stats
	}
	return nil
}

// SetRingBufferOutputChannel sets the output channel for the ring buffer
// This is useful for connecting to the orchestrator
func (bc *BaseObserver) SetRingBufferOutputChannel(ch chan *domain.CollectorEvent) {
	if bc.ringBuffer != nil {
		bc.ringBuffer.outputChan = ch
	}
}

// IsRingBufferEnabled returns true if ring buffer is enabled
func (bc *BaseObserver) IsRingBufferEnabled() bool {
	return bc.useRingBuffer && bc.ringBuffer != nil
}
