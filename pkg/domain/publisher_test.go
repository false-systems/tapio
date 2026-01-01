package domain

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNoOpPublisher_Publish verifies NoOp implementation discards events
func TestNoOpPublisher_Publish(t *testing.T) {
	publisher := &NoOpPublisher{}

	event := &TapioEvent{
		ID:   "test-123",
		Type: EventTypeNetwork,
	}

	err := publisher.Publish(context.Background(), "tapio.events.test", event)
	assert.NoError(t, err, "NoOp should never error")
}

// TestNoOpPublisher_Close verifies NoOp close
func TestNoOpPublisher_Close(t *testing.T) {
	publisher := &NoOpPublisher{}
	err := publisher.Close()
	assert.NoError(t, err, "NoOp close should never error")
}
