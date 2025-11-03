package runtime

import (
	"context"

	"github.com/yairfalse/tapio/pkg/domain"
)

// EventProcessor is the core interface that all observers must implement.
// This is the ONLY observer-specific code - all infrastructure is handled by ObserverRuntime.
//
// Examples:
//   - NetworkProcessor: Parses eBPF network events, runs DNS/Link/Status detection
//   - NodeProcessor: Parses eBPF PMC events, calculates IPC and memory stalls
//   - ContainerProcessor: Parses eBPF container events, detects OOM/Exit patterns
//   - DeploymentsProcessor: Watches K8s deployments, detects rollout issues
//
// The processor should be PURE BUSINESS LOGIC with no infrastructure concerns:
//   - No eBPF loading/management
//   - No K8s informer management
//   - No OTLP/NATS export
//   - No metrics collection
//
// Just: Parse raw event → Apply observer logic → Return domain event (or nil)
type EventProcessor interface {
	// Process converts raw event bytes to a domain event.
	// Returns nil if event should be ignored (not interesting or not recognized).
	// Returns error only for unrecoverable processing errors.
	//
	// Thread-safety: Process may be called concurrently from multiple goroutines.
	// Implementations must be thread-safe if they maintain state.
	Process(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error)

	// Name returns the processor name for logging and metrics.
	// Should be lowercase with hyphens (e.g., "network", "node-pmc", "container-runtime").
	Name() string

	// Setup is called once before the runtime starts processing events.
	// Use this for initialization that can't be done in the constructor:
	//   - Loading configuration files
	//   - Establishing connections
	//   - Pre-warming caches
	//
	// If Setup returns an error, the runtime will not start.
	Setup(ctx context.Context) error

	// Teardown is called once after the runtime stops processing events.
	// Use this for cleanup:
	//   - Closing connections
	//   - Flushing buffers
	//   - Releasing resources
	//
	// Teardown is called even if Setup failed.
	// Teardown errors are logged but don't prevent shutdown.
	Teardown(ctx context.Context) error
}
