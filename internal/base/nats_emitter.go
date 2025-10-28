package base

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
	"github.com/yairfalse/tapio/pkg/domain"
)

// NATSEmitter handles event publishing to NATS with automatic subject routing
// This is separate from the base Emitter interface which handles output destinations
type NATSEmitter struct {
	nc     *nats.Conn
	logger zerolog.Logger
	prefix string // "tapio.events.{observer_name}"
}

// NewNATSEmitter creates a new NATS event emitter for an observer
// Events are published to: tapio.events.{observerName}.{eventSubtype}
func NewNATSEmitter(observerName string, nc *nats.Conn, logger zerolog.Logger) *NATSEmitter {
	return &NATSEmitter{
		nc:     nc,
		logger: logger,
		prefix: fmt.Sprintf("tapio.events.%s", observerName),
	}
}

// Emit publishes an observer event to NATS
// Subject format: tapio.events.{observer}.{subtype}
// Example: tapio.events.network.connection_reset
func (e *NATSEmitter) Emit(ctx context.Context, evt *domain.ObserverEvent) error {
	if evt == nil {
		return fmt.Errorf("cannot emit nil event")
	}

	// Marshal to JSON
	data, err := json.Marshal(evt)
	if err != nil {
		e.logger.Error().Err(err).Msg("failed to marshal event")
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Build subject: tapio.events.{observer}.{subtype}
	subject := fmt.Sprintf("%s.%s", e.prefix, evt.Subtype)

	// Publish to NATS
	if err := e.nc.Publish(subject, data); err != nil {
		e.logger.Error().
			Err(err).
			Str("subject", subject).
			Str("event_type", evt.Type).
			Msg("failed to publish event")
		return fmt.Errorf("failed to publish to %s: %w", subject, err)
	}

	e.logger.Debug().
		Str("subject", subject).
		Str("event_type", evt.Type).
		Str("subtype", evt.Subtype).
		Msg("event published")

	return nil
}

// EmitBatch publishes multiple events in a single operation
// More efficient than calling Emit() multiple times
func (e *NATSEmitter) EmitBatch(ctx context.Context, events []*domain.ObserverEvent) error {
	if len(events) == 0 {
		return nil
	}

	var firstErr error
	for _, evt := range events {
		if err := e.Emit(ctx, evt); err != nil && firstErr == nil {
			firstErr = err
			// Continue to publish remaining events
		}
	}

	return firstErr
}
