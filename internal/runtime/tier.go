package runtime

import (
	"fmt"
	"log"
)

// Tier determines which emitters are enabled.
type Tier string

const (
	// TierSimple enables OTLP only.
	// Use for: Basic observability without Ahti correlation.
	TierSimple Tier = "simple"

	// TierFree enables OTLP + NATS.
	// Use for: Ahti correlation with ObserverEvent (no graph entities).
	TierFree Tier = "free"
)

// TierConfig holds emitter configuration based on deployment tier.
type TierConfig struct {
	// Tier determines emitter selection
	Tier Tier

	// OTLPURL is the OpenTelemetry Collector endpoint (required)
	OTLPURL string

	// Insecure disables TLS for OTLP connection
	Insecure bool

	// NATSURL for FREE tier (ignored for SIMPLE tier)
	NATSURL string
}

// BuildEmitters creates emitters based on tier configuration.
// Returns error only if critical emitter (OTLP) fails.
// Non-critical emitter (NATS) failures are logged but don't fail the call.
func (c *TierConfig) BuildEmitters() ([]Emitter, error) {
	if c.OTLPURL == "" {
		return nil, fmt.Errorf("OTLPURL required for all tiers")
	}

	var emitters []Emitter

	// OTLP always enabled (critical)
	otlp, err := NewOTLPEmitter(c.OTLPURL, c.Insecure)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP emitter: %w", err)
	}
	emitters = append(emitters, otlp)

	// NATS only for FREE tier (non-critical)
	if c.Tier == TierFree && c.NATSURL != "" {
		nats, err := NewNATSEmitter(c.NATSURL)
		if err != nil {
			// Log warning but don't fail - NATS is non-critical
			log.Printf("WARN: failed to create NATS emitter (continuing without it): %v", err)
		} else {
			emitters = append(emitters, nats)
		}
	}

	return emitters, nil
}
