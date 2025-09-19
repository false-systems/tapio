package base

import (
	"os"
	"sync/atomic"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// EventChannelManager handles event channel operations with drop counting
type EventChannelManager struct {
	channel       chan *domain.CollectorEvent
	droppedCount  atomic.Int64
	sentCount     atomic.Int64
	logger        *zap.Logger
	collectorName string
	validator     *EventValidator // Event validation
}

// NewEventChannelManager creates a new event channel manager
func NewEventChannelManager(size int, collectorName string, logger *zap.Logger) *EventChannelManager {
	// Enable strict validation in development
	strictMode := false
	if env := os.Getenv("TAPIO_STRICT_VALIDATION"); env == "true" {
		strictMode = true
	}

	return &EventChannelManager{
		channel:       make(chan *domain.CollectorEvent, size),
		logger:        logger,
		collectorName: collectorName,
		validator:     NewEventValidator(collectorName, logger, strictMode),
	}
}

// SendEvent attempts to send an event through the channel
// Returns true if sent successfully, false if dropped
func (ecm *EventChannelManager) SendEvent(event *domain.CollectorEvent) bool {
	// Validate event structure before sending
	if ecm.validator != nil {
		if err := ecm.validator.ValidateEvent(event); err != nil {
			// Validation failed - count as dropped
			ecm.droppedCount.Add(1)
			if ecm.logger != nil {
				ecm.logger.Error("Event validation failed, dropping event",
					zap.String("collector", ecm.collectorName),
					zap.String("event_id", event.EventID),
					zap.Error(err),
				)
			}
			return false
		}
	}

	select {
	case ecm.channel <- event:
		ecm.sentCount.Add(1)
		return true
	default:
		// Channel full, drop event
		ecm.droppedCount.Add(1)
		if ecm.logger != nil {
			ecm.logger.Debug("Event channel full, dropping event",
				zap.String("collector", ecm.collectorName),
				zap.String("event_id", event.EventID),
				zap.String("event_type", string(event.Type)),
			)
		}
		return false
	}
}

// GetChannel returns the event channel for reading
func (ecm *EventChannelManager) GetChannel() <-chan *domain.CollectorEvent {
	return ecm.channel
}

// Close closes the event channel
func (ecm *EventChannelManager) Close() {
	if ecm.channel != nil {
		close(ecm.channel)
		ecm.channel = nil
	}
}

// GetDroppedCount returns the number of dropped events
func (ecm *EventChannelManager) GetDroppedCount() int64 {
	return ecm.droppedCount.Load()
}

// GetSentCount returns the number of successfully sent events
func (ecm *EventChannelManager) GetSentCount() int64 {
	return ecm.sentCount.Load()
}

// IsChannelFull checks if the channel is at capacity
func (ecm *EventChannelManager) IsChannelFull() bool {
	return len(ecm.channel) == cap(ecm.channel)
}

// GetChannelUtilization returns the percentage of channel capacity used
func (ecm *EventChannelManager) GetChannelUtilization() float64 {
	if cap(ecm.channel) == 0 {
		return 0
	}
	return float64(len(ecm.channel)) / float64(cap(ecm.channel)) * 100
}
