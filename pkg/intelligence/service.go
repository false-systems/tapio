package intelligence

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/rs/zerolog/log"
	"github.com/yairfalse/tapio/pkg/domain"
)

// Tier determines the output behavior of the intelligence service.
type Tier string

const (
	// TierDebug outputs to stdout only (for development/debugging).
	TierDebug Tier = "debug"

	// TierPolku streams events to POLKU gateway (production).
	// Use NewPolkuService(PolkuConfig) to create this tier.
	TierPolku Tier = "polku"
)

// Config configures the intelligence service.
type Config struct {
	// Tier determines output behavior (currently only debug supported)
	Tier Tier

	// Critical determines if failures should block event processing.
	// If true, Emit() errors will cause upstream failures.
	// If false, errors are logged but event processing continues.
	Critical bool
}

// Service is the universal event gateway for TAPIO.
// All observers emit events through this single interface.
type Service interface {
	// Emit processes and routes an event based on tier configuration.
	// This is the single entry point for all observer events.
	Emit(ctx context.Context, event *domain.ObserverEvent) error

	// Name returns the service identifier (for logging and metrics).
	Name() string

	// IsCritical returns whether this service is critical.
	// If true, Emit failures should stop event processing.
	// If false, failures are logged but processing continues.
	IsCritical() bool

	// Close gracefully shuts down the service.
	Close() error
}

// New creates an intelligence service for the given configuration.
func New(cfg Config) (Service, error) {
	switch cfg.Tier {
	case TierDebug, "": // Default to debug tier
		return newDebugService(cfg)
	default:
		return nil, fmt.Errorf("unknown tier: %s", cfg.Tier)
	}
}

// debugService implements Service for debug tier (stdout only).
type debugService struct {
	critical bool
	mu       sync.Mutex
	closed   bool
}

func newDebugService(cfg Config) (*debugService, error) {
	return &debugService{
		critical: cfg.Critical,
	}, nil
}

func (s *debugService) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	if event == nil {
		return fmt.Errorf("nil event")
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("service is closed")
	}
	s.mu.Unlock()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Pretty print to stdout for debugging
	data, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	log.Debug().
		Str("type", event.Type).
		Str("subtype", event.Subtype).
		Str("id", event.ID).
		RawJSON("event", data).
		Msg("debug event")

	return nil
}

func (s *debugService) Name() string {
	return "intelligence-debug"
}

func (s *debugService) IsCritical() bool {
	return s.critical
}

func (s *debugService) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}
