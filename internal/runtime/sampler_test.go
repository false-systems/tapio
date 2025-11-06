package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yairfalse/tapio/pkg/domain"
)

// RED: Test sampler creation
func TestNewSampler(t *testing.T) {
	config := SamplingConfig{
		Enabled:     true,
		DefaultRate: 0.5,
	}

	sampler := NewSampler(config)
	assert.NotNil(t, sampler)
}

// RED: Test default sampling rate
func TestSampler_DefaultRate(t *testing.T) {
	config := SamplingConfig{
		Enabled:     true,
		DefaultRate: 1.0, // Keep all events
	}

	sampler := NewSampler(config)

	event := &domain.ObserverEvent{
		Type:    "network",
		Subtype: "dns_query",
	}

	// With rate 1.0, should always keep
	for i := 0; i < 100; i++ {
		assert.True(t, sampler.ShouldSample(event))
	}
}

// RED: Test sampling drops events
func TestSampler_DropsEvents(t *testing.T) {
	config := SamplingConfig{
		Enabled:     true,
		DefaultRate: 0.0, // Drop all events
	}

	sampler := NewSampler(config)

	event := &domain.ObserverEvent{
		Type:    "network",
		Subtype: "dns_query",
	}

	// With rate 0.0, should always drop
	for i := 0; i < 100; i++ {
		assert.False(t, sampler.ShouldSample(event))
	}
}

// RED: Test KeepAll rule
func TestSampler_KeepAllRule(t *testing.T) {
	config := SamplingConfig{
		Enabled:     true,
		DefaultRate: 0.0, // Drop by default
		Rules: []SamplingRule{
			{
				EventType: "network",
				Subtype:   "link_failure",
				KeepAll:   true, // Always keep link failures
			},
		},
	}

	sampler := NewSampler(config)

	// link_failure should always be kept
	linkFailure := &domain.ObserverEvent{
		Type:    "network",
		Subtype: "link_failure",
	}

	for i := 0; i < 100; i++ {
		assert.True(t, sampler.ShouldSample(linkFailure))
	}

	// dns_query should be dropped (default rate 0.0)
	dnsQuery := &domain.ObserverEvent{
		Type:    "network",
		Subtype: "dns_query",
	}

	for i := 0; i < 100; i++ {
		assert.False(t, sampler.ShouldSample(dnsQuery))
	}
}

// RED: Test rule override rate
func TestSampler_RuleOverride(t *testing.T) {
	config := SamplingConfig{
		Enabled:     true,
		DefaultRate: 0.5, // 50% by default
		Rules: []SamplingRule{
			{
				EventType: "network",
				Subtype:   "dns_query",
				Rate:      1.0, // 100% for DNS
			},
		},
	}

	sampler := NewSampler(config)

	// DNS should be kept (rule rate 1.0)
	dns := &domain.ObserverEvent{
		Type:    "network",
		Subtype: "dns_query",
	}

	for i := 0; i < 100; i++ {
		assert.True(t, sampler.ShouldSample(dns))
	}
}

// RED: Test wildcard subtype (empty string matches all)
func TestSampler_WildcardSubtype(t *testing.T) {
	config := SamplingConfig{
		Enabled:     true,
		DefaultRate: 0.0, // Drop by default
		Rules: []SamplingRule{
			{
				EventType: "network",
				Subtype:   "", // Match all network subtypes
				Rate:      1.0,
			},
		},
	}

	sampler := NewSampler(config)

	// All network events should be kept
	dnsQuery := &domain.ObserverEvent{
		Type:    "network",
		Subtype: "dns_query",
	}
	linkFailure := &domain.ObserverEvent{
		Type:    "network",
		Subtype: "link_failure",
	}

	for i := 0; i < 10; i++ {
		assert.True(t, sampler.ShouldSample(dnsQuery))
		assert.True(t, sampler.ShouldSample(linkFailure))
	}

	// Non-network events should be dropped
	nodeEvent := &domain.ObserverEvent{
		Type:    "node",
		Subtype: "ipc_degradation",
	}

	for i := 0; i < 10; i++ {
		assert.False(t, sampler.ShouldSample(nodeEvent))
	}
}

// RED: Test nil event
func TestSampler_NilEvent(t *testing.T) {
	config := SamplingConfig{
		Enabled:     true,
		DefaultRate: 1.0,
	}

	sampler := NewSampler(config)
	assert.False(t, sampler.ShouldSample(nil))
}

// Test that sampling disabled keeps all events
func TestSampler_DisabledKeepsAll(t *testing.T) {
	config := SamplingConfig{
		Enabled:     false, // Sampling disabled
		DefaultRate: 0.0,   // Would drop all if enabled
	}

	sampler := NewSampler(config)

	event := &domain.ObserverEvent{
		Type:    "network",
		Subtype: "dns_query",
	}

	// Should keep all events when disabled
	for i := 0; i < 100; i++ {
		assert.True(t, sampler.ShouldSample(event), "Should keep event when sampling disabled")
	}
}
