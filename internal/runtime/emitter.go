package runtime

import (
	"context"

	"github.com/yairfalse/tapio/pkg/domain"
)

// Emitter is the interface for exporting observer events to external systems.
// Implementations handle the protocol-specific details of sending events.
//
// Examples:
//   - OTLPEmitter: Exports to OpenTelemetry Collector (OSS)
//   - NATSEmitter: Publishes to NATS JetStream (Enterprise)
//   - FileEmitter: Writes to file (Debug/Testing)
//
// Design: Best-effort delivery
//   - ObserverRuntime can have multiple emitters
//   - If one emitter fails, others continue
//   - Failures are logged and metriced, not propagated
type Emitter interface {
	// Emit sends an observer event to the destination.
	// Returns error if the event could not be sent.
	//
	// Thread-safety: Emit may be called concurrently.
	// Implementations must be thread-safe.
	//
	// Timeout: Emit should respect ctx timeout/cancellation.
	// Long-running emits should be avoided (use buffering).
	Emit(ctx context.Context, event *domain.ObserverEvent) error

	// Name returns the emitter name for logging and metrics.
	// Should be lowercase (e.g., "otlp", "nats", "file").
	Name() string

	// Close releases any resources held by the emitter.
	// Called during runtime shutdown.
	// Implementations should flush any buffered events.
	Close() error
}

// EmitterPolicy defines how multiple emitters are handled.
type EmitterPolicy int

const (
	// EmitterPolicyBestEffort continues if some emitters fail.
	// This is the default and recommended policy.
	// Use case: NATS down shouldn't break OTLP export.
	EmitterPolicyBestEffort EmitterPolicy = iota

	// EmitterPolicyAllOrNothing fails entire event if any emitter fails.
	// Use case: Critical events that must reach all destinations.
	// Not recommended for production (availability > consistency).
	EmitterPolicyAllOrNothing
)
