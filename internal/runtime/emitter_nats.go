package runtime

import (
	"context"
	"fmt"
	"sync"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence"
)

// NATSEmitter wraps IntelligenceService as an Emitter.
// This bridges Level 2 (Intelligence) with Level 4 (Runtime).
//
// Thread-Safety: This implementation is thread-safe. The Emit() method can be called
// concurrently from multiple goroutines.
type NATSEmitter struct {
	svc intelligence.IntelligenceService

	mu     sync.Mutex
	closed bool
}

// NewNATSEmitter creates an emitter that sends events to Intelligence Service.
// The Intelligence Service publishes events to NATS subjects.
// url: NATS server URL (e.g., "nats://localhost:4222")
func NewNATSEmitter(url string) (*NATSEmitter, error) {
	svc, err := intelligence.NewIntelligenceService(url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}
	return &NATSEmitter{svc: svc}, nil
}

// Emit sends an observer event to NATS via Intelligence Service.
func (e *NATSEmitter) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	if event == nil {
		return fmt.Errorf("nil event")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return fmt.Errorf("emitter is closed")
	}
	svc := e.svc
	e.mu.Unlock()

	return svc.ProcessEvent(ctx, event)
}

// Name returns the emitter name for logging and metrics.
func (e *NATSEmitter) Name() string {
	return "nats"
}

// IsCritical returns false - NATS is not critical.
// If NATS is down, OTLP should still work.
func (e *NATSEmitter) IsCritical() bool {
	return false
}

// Close shuts down the Intelligence Service connection.
// This method is idempotent - multiple calls are safe.
func (e *NATSEmitter) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil
	}

	if e.svc != nil {
		if err := e.svc.Shutdown(context.Background()); err != nil {
			return fmt.Errorf("failed to shutdown intelligence service: %w", err)
		}
	}

	e.closed = true
	return nil
}
