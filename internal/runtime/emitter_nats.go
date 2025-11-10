package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/yairfalse/tapio/pkg/domain"
)

// NATSEmitter publishes observer events to NATS JetStream.
// This is a NON-CRITICAL emitter for Enterprise deployments.
type NATSEmitter struct {
	conn   *nats.Conn
	mu     sync.Mutex
	closed bool
}

// NewNATSEmitter creates a NATS emitter that publishes to the given NATS server.
// url: NATS server URL (e.g., "nats://localhost:4222")
func NewNATSEmitter(url string) (*NATSEmitter, error) {
	// Connect to NATS server
	conn, err := nats.Connect(url,
		nats.MaxReconnects(-1),            // Infinite reconnects
		nats.ReconnectWait(2*time.Second), // 2 second reconnect delay
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	return &NATSEmitter{
		conn:   conn,
		closed: false,
	}, nil
}

// Emit publishes an observer event to NATS as JSON.
// Subject pattern: tapio.events.{type}.{subtype}
// Example: tapio.events.network.dns_query
func (e *NATSEmitter) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	if event == nil {
		return fmt.Errorf("event is nil")
	}

	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return fmt.Errorf("emitter is closed")
	}
	e.mu.Unlock()

	// Check context cancellation first
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Build NATS subject
	subject := e.buildSubject(event)

	// Marshal event to JSON
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Publish to NATS (core NATS, not JetStream)
	// JetStream adds complexity and we don't need durability for OSS version
	if err := e.conn.Publish(subject, data); err != nil {
		return fmt.Errorf("failed to publish to NATS: %w", err)
	}

	return nil
}

// buildSubject constructs the NATS subject for an event.
// Pattern: tapio.events.{type}.{subtype}
// If subtype is empty, pattern is: tapio.events.{type}
func (e *NATSEmitter) buildSubject(event *domain.ObserverEvent) string {
	// Sanitize type and subtype (replace invalid chars with underscore)
	eventType := sanitizeSubjectToken(event.Type)
	subtype := sanitizeSubjectToken(event.Subtype)

	if subtype == "" {
		return fmt.Sprintf("tapio.events.%s", eventType)
	}
	return fmt.Sprintf("tapio.events.%s.%s", eventType, subtype)
}

// sanitizeSubjectToken replaces invalid NATS subject characters with underscore.
// NATS subjects can only contain: A-Z, a-z, 0-9, ., -, _
func sanitizeSubjectToken(s string) string {
	if s == "" {
		return ""
	}

	// Replace spaces and special chars with underscore
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, ":", "_")

	return s
}

// Name returns the emitter name for logging and metrics.
func (e *NATSEmitter) Name() string {
	return "nats"
}

// IsCritical returns false - NATS is non-critical (Enterprise add-on).
// If NATS is down, events still go to OTLP.
func (e *NATSEmitter) IsCritical() bool {
	return false
}

// Close closes the NATS connection.
func (e *NATSEmitter) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil // Already closed
	}

	if e.conn != nil {
		e.conn.Close()
	}

	e.closed = true
	return nil
}
