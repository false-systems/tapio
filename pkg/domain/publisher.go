package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/nats-io/nats.go"
)

// EventPublisher publishes Tapio domain events to external systems
// Implementations: NATS (enterprise), NoOp (open source), Stdout (dev/testing)
type EventPublisher interface {
	// Publish sends an event to the configured backend
	Publish(ctx context.Context, subject string, event any) error

	// Close gracefully shuts down the publisher
	Close() error
}

// NoOpPublisher is a null implementation that discards all events
// Used in open source builds where event streaming is not enabled
type NoOpPublisher struct{}

func (n *NoOpPublisher) Publish(ctx context.Context, subject string, event any) error {
	return nil
}

func (n *NoOpPublisher) Close() error {
	return nil
}

// NATSPublisher publishes TapioEvent to NATS JetStream for Ahti correlation
// Enterprise feature - sends graph-enriched events to correlation engine
type NATSPublisher struct {
	conn   *nats.Conn
	js     nats.JetStreamContext
	closed atomic.Bool
}

// NewNATSPublisher creates a NATS publisher for TapioEvent streaming
func NewNATSPublisher(conn *nats.Conn) (*NATSPublisher, error) {
	if conn == nil {
		return nil, fmt.Errorf("NATS connection is required")
	}

	// Get JetStream context
	js, err := conn.JetStream()
	if err != nil {
		return nil, fmt.Errorf("failed to get JetStream context: %w", err)
	}

	return &NATSPublisher{
		conn: conn,
		js:   js,
	}, nil
}

// Publish sends TapioEvent to NATS JetStream subject
func (p *NATSPublisher) Publish(ctx context.Context, subject string, event any) error {
	if p.closed.Load() {
		return fmt.Errorf("publisher is closed")
	}

	if event == nil {
		return fmt.Errorf("nil event")
	}

	// Marshal event to JSON
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Publish to JetStream
	// Note: We don't use ctx for publish timeout here because NATS has its own timeouts
	// If we need context-aware publishing, we can add js.PublishAsync with ctx
	_, err = p.js.Publish(subject, data)
	if err != nil {
		return fmt.Errorf("failed to publish to NATS subject %s: %w", subject, err)
	}

	return nil
}

// Close closes the NATS publisher
func (p *NATSPublisher) Close() error {
	p.closed.Store(true)
	// Note: We don't close the NATS connection here because it might be shared
	// The connection should be closed by whoever created it
	return nil
}
