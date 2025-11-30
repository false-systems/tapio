package intelligence

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

// Test NewIntelligenceService creates service
func TestNewIntelligenceService(t *testing.T) {
	svc, err := NewIntelligenceService("nats://localhost:4222")
	if err != nil {
		t.Skipf("Skipping test - NATS server not available: %v", err)
	}
	require.NotNil(t, svc)
	defer func() {
		if err := svc.Shutdown(context.Background()); err != nil {
			t.Logf("failed to shutdown service: %v", err)
		}
	}()
}

// Test ProcessEvent accepts ObserverEvent
func TestIntelligenceService_ProcessEvent(t *testing.T) {
	svc, err := NewIntelligenceService("nats://localhost:4222")
	if err != nil {
		t.Skipf("Skipping test - NATS server not available: %v", err)
	}
	defer func() {
		if err := svc.Shutdown(context.Background()); err != nil {
			t.Logf("failed to shutdown service: %v", err)
		}
	}()

	event := &domain.ObserverEvent{
		ID:        "test-123",
		Type:      string(domain.EventTypeNetwork),
		Subtype:   "dns_query",
		Source:    "test-observer",
		Timestamp: time.Now(),
	}

	err = svc.ProcessEvent(context.Background(), event)
	assert.NoError(t, err)
}

// Test Shutdown cleans up resources
func TestIntelligenceService_Shutdown(t *testing.T) {
	svc, err := NewIntelligenceService("nats://localhost:4222")
	if err != nil {
		t.Skipf("Skipping test - NATS server not available: %v", err)
	}

	err = svc.Shutdown(context.Background())
	assert.NoError(t, err)

	// Multiple Shutdown() calls should be safe
	err = svc.Shutdown(context.Background())
	assert.NoError(t, err)
}

// Test context cancellation
func TestIntelligenceService_ContextCancellation(t *testing.T) {
	svc, err := NewIntelligenceService("nats://localhost:4222")
	if err != nil {
		t.Skipf("Skipping test - NATS server not available: %v", err)
	}
	defer func() {
		if err := svc.Shutdown(context.Background()); err != nil {
			t.Logf("failed to shutdown service: %v", err)
		}
	}()

	event := &domain.ObserverEvent{
		ID:        "test-789",
		Type:      string(domain.EventTypeNetwork),
		Source:    "test-observer",
		Timestamp: time.Now(),
	}

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// ProcessEvent should fail fast due to cancelled context
	err = svc.ProcessEvent(ctx, event)
	assert.Error(t, err, "ProcessEvent should fail with cancelled context")
}

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
