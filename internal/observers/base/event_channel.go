package base

import (
	"os"
	"sync"
	"sync/atomic"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// EventChannelManager handles event channel operations with drop counting
type EventChannelManager struct {
	mu            sync.RWMutex
	channel       chan *domain.CollectorEvent
	closed        atomic.Bool
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
	// Check if already closed
	if ecm.closed.Load() {
		return false
	}

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

	// Try to send with proper synchronization
	ecm.mu.RLock()
	defer ecm.mu.RUnlock()

	// Double-check closed status while holding lock
	if ecm.closed.Load() || ecm.channel == nil {
		ecm.droppedCount.Add(1)
		return false
	}

	// Use non-blocking send with panic recovery
	defer func() {
		if r := recover(); r != nil {
			// Channel was closed while we were trying to send
			ecm.droppedCount.Add(1)
		}
	}()

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
	ecm.mu.RLock()
	defer ecm.mu.RUnlock()
	return ecm.channel
}

// Close closes the event channel
func (ecm *EventChannelManager) Close() {
	// Use atomic compare-and-swap to ensure Close is called only once
	if !ecm.closed.CompareAndSwap(false, true) {
		return // Already closed
	}

	// Lock for write to safely close channel
	ecm.mu.Lock()
	defer ecm.mu.Unlock()

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
	ecm.mu.RLock()
	defer ecm.mu.RUnlock()

	if ecm.channel == nil {
		return false
	}
	return len(ecm.channel) == cap(ecm.channel)
}

// GetChannelUtilization returns the percentage of channel capacity used
func (ecm *EventChannelManager) GetChannelUtilization() float64 {
	ecm.mu.RLock()
	defer ecm.mu.RUnlock()

	if ecm.channel == nil || cap(ecm.channel) == 0 {
		return 0
	}
	return float64(len(ecm.channel)) / float64(cap(ecm.channel)) * 100
}
