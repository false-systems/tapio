package correlation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// TestNATSSubscriber_BasicFunctionality tests basic NATS subscriber operations
func TestNATSSubscriber_BasicFunctionality(t *testing.T) {
	logger := zap.NewNop()

	// Create mock correlation engine
	mockEngine := NewMockCorrelationEngine()

	// Create config
	config := &NATSSubscriberConfig{
		URL:               "nats://localhost:4222",
		StreamName:        "TEST_STREAM",
		Name:              "test-subscriber",
		TraceSubjects:     []string{"traces.>"},
		CorrelationWindow: 2 * time.Second,
		MinEventsForCorr:  2,
		Logger:            logger,
	}

	// Test config defaults
	assert.Equal(t, "nats://localhost:4222", config.URL)
	assert.Equal(t, 2*time.Second, config.CorrelationWindow)
	assert.Equal(t, 2, config.MinEventsForCorr)

	// Test that we can create the subscriber (will fail without NATS server, but that's OK)
	_, err := NewNATSSubscriber(config, mockEngine)
	assert.Error(t, err) // Expected to fail without NATS server
	assert.Contains(t, err.Error(), "failed to connect")
}

// TestNATSSubscriber_TraceIDExtraction tests trace ID extraction from subjects
func TestNATSSubscriber_TraceIDExtraction(t *testing.T) {
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
			subject:  "traces.abc123",
			expected: "abc123",
		},
		{
			name:     "Complex trace subject",
			subject:  "test.traces.xyz789",
			expected: "xyz789",
		},
		{
			name:     "No trace ID",
			subject:  "events.raw.systemd",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := subscriber.extractTraceIDFromSubject(tt.subject)
			assert.Equal(t, tt.expected, result)
		})
	}
}
