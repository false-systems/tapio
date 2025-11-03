package runtime

import (
	"math/rand"

	"github.com/yairfalse/tapio/pkg/domain"
)

// Sampler implements event sampling based on configuration rules.
type Sampler struct {
	config SamplingConfig
	rng    *rand.Rand
}

// NewSampler creates a new sampler with the given configuration
func NewSampler(config SamplingConfig) *Sampler {
	return &Sampler{
		config: config,
		rng:    rand.New(rand.NewSource(rand.Int63())),
	}
}

// ShouldSample determines if an event should be kept or dropped.
// Returns true if event should be kept, false if it should be sampled out.
func (s *Sampler) ShouldSample(event *domain.ObserverEvent) bool {
	if event == nil {
		return false
	}

	// Find matching rule
	for _, rule := range s.config.Rules {
		if rule.Matches(event.Type, event.Subtype) {
			// Rule matches - use its settings
			if rule.KeepAll {
				return true // Always keep
			}
			return s.rng.Float64() < rule.Rate
		}
	}

	// No matching rule - use default rate
	return s.rng.Float64() < s.config.DefaultRate
}
