package intelligence

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yairfalse/tapio/pkg/domain"
)

// Test buildSubject with different event types and subtypes
func TestBuildSubject(t *testing.T) {
	tests := []struct {
		name     string
		event    *domain.ObserverEvent
		expected string
	}{
		{
			name: "network event with subtype",
			event: &domain.ObserverEvent{
				Type:    string(domain.EventTypeNetwork),
				Subtype: "dns_query",
			},
			expected: "tapio.events.network.dns_query",
		},
		{
			name: "network event without subtype",
			event: &domain.ObserverEvent{
				Type:    string(domain.EventTypeNetwork),
				Subtype: "",
			},
			expected: "tapio.events.network",
		},
		{
			name: "container event with subtype",
			event: &domain.ObserverEvent{
				Type:    string(domain.EventTypeContainer),
				Subtype: "oom_kill",
			},
			expected: "tapio.events.container.oom_kill",
		},
		{
			name: "subtype with special characters",
			event: &domain.ObserverEvent{
				Type:    "network",
				Subtype: "tcp:syn timeout",
			},
			expected: "tapio.events.network.tcp_syn_timeout",
		},
		{
			name: "subtype with slashes",
			event: &domain.ObserverEvent{
				Type:    "deployment",
				Subtype: "rollout/failed",
			},
			expected: "tapio.events.deployment.rollout_failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subject := buildSubject(tt.event)
			assert.Equal(t, tt.expected, subject)
		})
	}
}

// Test sanitizeSubjectToken edge cases
func TestSanitizeSubjectToken(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "simple string",
			input:    "dns_query",
			expected: "dns_query",
		},
		{
			name:     "spaces",
			input:    "syn timeout",
			expected: "syn_timeout",
		},
		{
			name:     "colons",
			input:    "tcp:syn",
			expected: "tcp_syn",
		},
		{
			name:     "slashes",
			input:    "rollout/failed",
			expected: "rollout_failed",
		},
		{
			name:     "multiple special chars",
			input:    "tcp:syn timeout/retry",
			expected: "tcp_syn_timeout_retry",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeSubjectToken(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
