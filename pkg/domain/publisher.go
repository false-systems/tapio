package domain

import "context"

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
