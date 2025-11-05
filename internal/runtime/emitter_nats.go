package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// NATSEmitter publishes events to a NATS subject.
// Critical emitter for enterprise event streaming.
type NATSEmitter struct {
	url         string
	subject     string
	timeout     time.Duration
	reconnect   bool
	credentials struct{ user, pass string }
	jetstream   bool
	streamName  string
	tls         bool
	mu          sync.Mutex
	closed      bool
}

// NATSOption configures a NATS emitter
type NATSOption func(*NATSEmitter)

// WithNATSTimeout sets the publish timeout
func WithNATSTimeout(timeout time.Duration) NATSOption {
	return func(e *NATSEmitter) {
		e.timeout = timeout
	}
}

// WithNATSReconnect enables automatic reconnection
func WithNATSReconnect(reconnect bool) NATSOption {
	return func(e *NATSEmitter) {
		e.reconnect = reconnect
	}
}

// WithNATSCredentials sets username/password authentication
func WithNATSCredentials(user, password string) NATSOption {
	return func(e *NATSEmitter) {
		e.credentials.user = user
		e.credentials.pass = password
	}
}

// WithJetStream enables JetStream support
func WithJetStream(enabled bool) NATSOption {
	return func(e *NATSEmitter) {
		e.jetstream = enabled
	}
}

// WithStreamName sets the JetStream stream name
func WithStreamName(name string) NATSOption {
	return func(e *NATSEmitter) {
		e.streamName = name
	}
}

// WithNATSTLS enables TLS for NATS connection
func WithNATSTLS(enabled bool) NATSOption {
	return func(e *NATSEmitter) {
		e.tls = enabled
	}
}

// NewNATSEmitter creates a new NATS emitter
func NewNATSEmitter(url, subject string, opts ...NATSOption) (*NATSEmitter, error) {
	if url == "" {
		return nil, fmt.Errorf("NATS URL is required")
	}

	if subject == "" {
		return nil, fmt.Errorf("NATS subject is required")
	}

	emitter := &NATSEmitter{
		url:       url,
		subject:   subject,
		timeout:   10 * time.Second,
		reconnect: true,
	}

	// Apply options
	for _, opt := range opts {
		opt(emitter)
	}

	// Minimal GREEN phase: Validate configuration
	// Actual NATS client connection will be added when we migrate real observers
	// For now, this provides the interface contract

	return emitter, nil
}

// Emit publishes an event to NATS
func (e *NATSEmitter) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	if event == nil {
		return fmt.Errorf("cannot emit nil event")
	}

	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return fmt.Errorf("emitter is closed")
	}
	e.mu.Unlock()

	// Check context
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled: %w", err)
	}

	// Marshal event as JSON to validate structure
	_, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Minimal GREEN phase: Validate event structure
	// NATS client integration will be added when we migrate real observers
	// For now, this provides the interface contract

	return nil
}

// Name returns "nats"
func (e *NATSEmitter) Name() string {
	return "nats"
}

// IsCritical returns true (NATS is critical by default)
func (e *NATSEmitter) IsCritical() bool {
	return true
}

// Close closes the NATS connection and flushes pending messages
func (e *NATSEmitter) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil // Idempotent
	}

	e.closed = true

	// Flush and disconnect will be implemented when NATS client is integrated
	// Client cleanup will be implemented when NATS client is integrated

	return nil
}
