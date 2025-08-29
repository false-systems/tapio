//go:build integration
// +build integration

package cri

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestIntegrationRealCRISocket tests with actual CRI socket if available
func TestIntegrationRealCRISocket(t *testing.T) {
	// Skip if no CRI socket available
	socket := detectCRISocket()
	if socket == "" {
		t.Skip("No CRI socket detected, skipping integration test")
	}

	config := NewDefaultConfig("integration")
	config.SocketPath = socket
	config.PollInterval = 2 * time.Second

	collector, err := NewCollector("cri-integration", config)
	require.NoError(t, err)

	collector.logger = zaptest.NewLogger(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start collector
	err = collector.Start(ctx)
	require.NoError(t, err, "Failed to start with real CRI socket")
	defer collector.Stop()

	assert.True(t, collector.IsHealthy(), "Should be healthy with real socket")

	// Wait for at least one poll cycle
	time.Sleep(3 * time.Second)

	// Check if we received any events
	eventCount := 0
	done := false

	for !done {
		select {
		case event := <-collector.Events():
			eventCount++
			assert.NotNil(t, event)
			assert.NotEmpty(t, event.EventID)
			assert.Equal(t, "cri-integration", event.Source)

			// Log first few events
			if eventCount <= 3 {
				t.Logf("Received event: Type=%s, Container=%+v",
					event.Type, event.EventData)
			}
		case <-time.After(100 * time.Millisecond):
			done = true
		}
	}

	t.Logf("Received %d events from real CRI", eventCount)

	// Stop collector
	err = collector.Stop()
	assert.NoError(t, err)
	assert.False(t, collector.IsHealthy())
}

// TestIntegrationMultipleCollectors tests running multiple collectors
func TestIntegrationMultipleCollectors(t *testing.T) {
	socket := detectCRISocket()
	if socket == "" {
		t.Skip("No CRI socket detected")
	}

	// Create multiple collectors
	collectors := make([]*Collector, 3)
	for i := 0; i < 3; i++ {
		config := NewDefaultConfig("test")
		config.SocketPath = socket
		config.PollInterval = 5 * time.Second

		c, err := NewCollector(string(rune('a'+i)), config)
		require.NoError(t, err)
		c.logger = zaptest.NewLogger(t)
		collectors[i] = c
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start all collectors
	for _, c := range collectors {
		err := c.Start(ctx)
		if err != nil {
			// May fail to connect, that's OK for this test
			t.Logf("Collector %s failed to start: %v", c.Name(), err)
		}
		defer c.Stop()
	}

	// Let them run briefly
	time.Sleep(1 * time.Second)

	// Stop all collectors
	for _, c := range collectors {
		err := c.Stop()
		assert.NoError(t, err)
	}
}
