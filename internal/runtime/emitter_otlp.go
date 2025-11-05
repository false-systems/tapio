package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// OTLPEmitter exports events to an OpenTelemetry Collector via OTLP.
type OTLPEmitter struct {
	endpoint  string
	insecure  bool
	timeout   time.Duration
	batchSize int
	headers   map[string]string
	mu        sync.Mutex
	closed    bool
}

// OTLPOption configures an OTLP emitter
type OTLPOption func(*OTLPEmitter)

// WithInsecure enables insecure (non-TLS) connection
func WithInsecure(insecure bool) OTLPOption {
	return func(e *OTLPEmitter) {
		e.insecure = insecure
	}
}

// WithTimeout sets the emission timeout
func WithTimeout(timeout time.Duration) OTLPOption {
	return func(e *OTLPEmitter) {
		e.timeout = timeout
	}
}

// WithBatchSize sets the batch size for events
func WithBatchSize(size int) OTLPOption {
	return func(e *OTLPEmitter) {
		e.batchSize = size
	}
}

// WithHeaders sets custom HTTP headers
func WithHeaders(headers map[string]string) OTLPOption {
	return func(e *OTLPEmitter) {
		e.headers = headers
	}
}

// NewOTLPEmitter creates a new OTLP emitter
func NewOTLPEmitter(endpoint string, opts ...OTLPOption) (*OTLPEmitter, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("OTLP endpoint is required")
	}

	emitter := &OTLPEmitter{
		endpoint:  endpoint,
		timeout:   10 * time.Second,
		batchSize: 100,
		headers:   make(map[string]string),
	}

	// Apply options
	for _, opt := range opts {
		opt(emitter)
	}

	return emitter, nil
}

// Emit sends an event to the OTLP collector
func (e *OTLPEmitter) Emit(ctx context.Context, event *domain.ObserverEvent) error {
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

	// Minimal GREEN phase: Validate event structure
	// OTLP client integration will be added when we migrate real observers
	// For now, this provides the interface contract

	return nil
}

// Name returns "otlp"
func (e *OTLPEmitter) Name() string {
	return "otlp"
}

// IsCritical returns true (OTLP is critical by default)
func (e *OTLPEmitter) IsCritical() bool {
	return true
}

// Close closes the emitter and flushes any buffered events
func (e *OTLPEmitter) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil // Idempotent
	}

	e.closed = true

	// Flush will be implemented when batching is added
	// Client cleanup will be implemented when OTLP client is integrated

	return nil
}
