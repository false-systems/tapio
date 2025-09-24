package base

import (
	"context"
	"fmt"

	"github.com/yairfalse/tapio/pkg/domain"
)

// EnhancedEventManager combines ring buffer with channel for maximum flexibility
// This can replace EventChannelManager in collectors that want ring buffer benefits
type EnhancedEventManager struct {
	*EventChannelManager // Embed for backward compatibility
	ringBuffer           *RingBuffer
	useRingBuffer        bool
}

// NewEnhancedEventManager creates a manager with both channel and ring buffer
func NewEnhancedEventManager(config RingBufferConfig) (*EnhancedEventManager, error) {
	// Create traditional channel manager
	channelSize := config.Size
	if channelSize == 0 {
		channelSize = 1000
	}

	ecm := NewEventChannelManager(channelSize, config.CollectorName, config.Logger)

	// Create ring buffer that feeds into the channel
	config.OutputChannel = ecm.channel
	rb, err := NewRingBuffer(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create ring buffer: %w", err)
	}

	return &EnhancedEventManager{
		EventChannelManager: ecm,
		ringBuffer:          rb,
		useRingBuffer:       true,
	}, nil
}

// SendEvent sends an event through ring buffer or falls back to channel
func (eem *EnhancedEventManager) SendEvent(event *domain.CollectorEvent) bool {
	if eem.useRingBuffer && eem.ringBuffer != nil {
		return eem.ringBuffer.Write(event)
	}
	return eem.EventChannelManager.SendEvent(event)
}

// RegisterLocalConsumer adds a local consumer (only works with ring buffer)
func (eem *EnhancedEventManager) RegisterLocalConsumer(consumer LocalConsumer) {
	if eem.ringBuffer != nil {
		eem.ringBuffer.RegisterLocalConsumer(consumer)
	}
}

// Start begins processing (required for ring buffer)
func (eem *EnhancedEventManager) Start(ctx context.Context) {
	if eem.ringBuffer != nil {
		eem.ringBuffer.Start(ctx)
	}
}

// Stop gracefully shuts down
func (eem *EnhancedEventManager) Stop() {
	if eem.ringBuffer != nil {
		eem.ringBuffer.Stop()
	}
	eem.EventChannelManager.Close()
}

// EventManagerStats represents combined event manager statistics
type EventManagerStats struct {
	// Basic stats (always present)
	Sent        int64   `json:"sent"`
	Dropped     int64   `json:"dropped"`
	Utilization float64 `json:"utilization"`

	// Ring buffer stats (optional)
	RingBuffer *RingBufferStats `json:"ring_buffer,omitempty"`
}

// GetStatistics returns combined statistics with proper typing
func (eem *EnhancedEventManager) GetStatistics() EventManagerStats {
	stats := EventManagerStats{
		Sent:        eem.GetSentCount(),
		Dropped:     eem.GetDroppedCount(),
		Utilization: eem.GetChannelUtilization(),
	}

	if eem.ringBuffer != nil {
		rbStats := eem.ringBuffer.Statistics()
		stats.RingBuffer = &rbStats
	}

	return stats
}
