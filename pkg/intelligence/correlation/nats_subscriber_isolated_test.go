package correlation

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// Test trace ID extraction without any external dependencies
func TestNATSSubscriber_ExtractTraceID_Isolated(t *testing.T) {
	subscriber := &NATSSubscriber{
		config: &NATSSubscriberConfig{Logger: zap.NewNop()},
		logger: zap.NewNop(),
	}

	tests := []struct {
		name     string
		subject  string
		expected string
	}{
		{
			name:     "Simple trace subject",
			subject:  "traces.abc123def456",
			expected: "abc123def456",
		},
		{
			name:     "Test stream trace subject",
			subject:  "test.stream.123.traces.xyz789",
			expected: "xyz789",
		},
		{
			name:     "No trace ID",
			subject:  "events.raw.systemd",
			expected: "",
		},
		{
			name:     "Traces without ID",
			subject:  "traces",
			expected: "",
		},
		{
			name:     "Complex trace subject",
			subject:  "prod.nats.traces.trace123.extra",
			expected: "trace123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := subscriber.extractTraceIDFromSubject(tt.subject)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test config defaults
func TestNATSSubscriberConfig_Defaults(t *testing.T) {
	config := &NATSSubscriberConfig{
		URL:        "nats://localhost:4222",
		StreamName: "TEST",
		Name:       "test-sub",
	}

	// Create mock engine
	mockEngine := NewMockCorrelationEngine()

	// This will apply defaults
	sub, err := NewNATSSubscriber(config, mockEngine)
	assert.Error(t, err) // Will fail to connect, but config should be set

	assert.Equal(t, 10, config.BatchSize)
	assert.Equal(t, 5*time.Second, config.BatchTimeout)
	assert.Equal(t, 4, config.WorkerCount)
	assert.Equal(t, 1000, config.MaxPending)
	assert.Equal(t, 30*time.Second, config.CorrelationWindow)
	assert.Equal(t, 2, config.MinEventsForCorr)
	assert.NotNil(t, config.Logger)

	_ = sub // avoid unused variable warning
}

// Test subscriber lifecycle without NATS
func TestNATSSubscriber_Lifecycle_Mock(t *testing.T) {
	// Test that we handle the started flag correctly
	subscriber := &NATSSubscriber{
		config:  &NATSSubscriberConfig{Logger: zap.NewNop()},
		logger:  zap.NewNop(),
		started: false,
	}

	// Stop should be safe on non-started subscriber
	err := subscriber.Stop()
	assert.NoError(t, err)

	// Results channel should be accessible
	results := subscriber.Results()
	assert.NotNil(t, results)
}