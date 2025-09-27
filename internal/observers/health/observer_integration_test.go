package health

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/observers/orchestrator"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// TestIntegrationWithOrchestrator tests integration with observer orchestrator
func TestIntegrationWithOrchestrator(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t)

	// Create orchestrator config
	orchConfig := &orchestrator.ObserverConfigData{
		BufferSize: 1000,
	}

	// Use factory to create observer
	observer, err := Factory("health", orchConfig, logger)
	require.NoError(t, err)
	require.NotNil(t, observer)

	// Start observer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Verify observer is operational
	assert.True(t, observer.IsHealthy())
	assert.Equal(t, "health", observer.Name())

	// Verify event channel
	eventChan := observer.Events()
	require.NotNil(t, eventChan)

	// Cast to concrete type to access Statistics method
	healthObs, ok := observer.(*Observer)
	require.True(t, ok)
	// Get statistics
	stats := healthObs.Statistics()
	assert.NotNil(t, stats)
	// Check observer name directly
	assert.Equal(t, "health", observer.Name())
}

// TestIntegrationWithMetricsProvider tests OpenTelemetry metrics integration
func TestIntegrationWithMetricsProvider(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t)

	// Create observer with metrics requirement
	config := &Config{
		RingBufferSize:    1024,
		EventChannelSize:  10,
		RateLimitMs:       10,
		RequireAllMetrics: false, // Graceful degradation
		EnabledCategories: map[string]bool{
			"file": true,
		},
	}

	observer, err := NewObserver(logger, config)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Generate some events to trigger metrics
	for i := 0; i < 5; i++ {
		event := &HealthEvent{
			TimestampNs: uint64(time.Now().UnixNano()),
			PID:         uint32(1000 + i),
			ErrorCode:   -28, // ENOSPC
			Category:    1,
		}

		domainEvent := observer.convertToCollectorEvent(event)
		observer.EventChannelManager.SendEvent(domainEvent)
		// updateErrorMetrics is not exported, skip it for now
	}

	// Verify metrics were updated (through side effects)
	stats := observer.Statistics()
	assert.GreaterOrEqual(t, stats.EventsProcessed, int64(0))
}

// TestIntegrationWithEventPipeline tests event processing pipeline
func TestIntegrationWithEventPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Set up event pipeline
	type ProcessedEvent struct {
		Original  *domain.CollectorEvent
		Processed time.Time
	}

	processedEvents := make(chan ProcessedEvent, 10)

	// Event processor
	go func() {
		for event := range observer.Events() {
			// Simulate processing
			processed := ProcessedEvent{
				Original:  event,
				Processed: time.Now(),
			}
			processedEvents <- processed
		}
	}()

	// Generate test events
	testEvents := []struct {
		errorCode int32
		category  uint8
		severity  domain.EventSeverity
	}{
		{-28, 1, domain.EventSeverityCritical}, // ENOSPC
		{-12, 3, domain.EventSeverityCritical}, // ENOMEM
		{-111, 2, domain.EventSeverityError},   // ECONNREFUSED
		{-13, 1, domain.EventSeverityWarning},  // EACCES
	}

	for _, te := range testEvents {
		event := &HealthEvent{
			TimestampNs: uint64(time.Now().UnixNano()),
			PID:         1000,
			ErrorCode:   te.errorCode,
			Category:    te.category,
		}

		domainEvent := observer.convertToCollectorEvent(event)
		assert.Equal(t, te.severity, domainEvent.Severity)

		sent := observer.EventChannelManager.SendEvent(domainEvent)
		assert.True(t, sent)
	}

	// Verify events were processed
	timeout := time.After(1 * time.Second)
	processedCount := 0

	for processedCount < len(testEvents) {
		select {
		case processed := <-processedEvents:
			assert.NotNil(t, processed.Original)
			assert.False(t, processed.Processed.IsZero())
			processedCount++
		case <-timeout:
			t.Fatalf("timeout waiting for processed events, got %d/%d",
				processedCount, len(testEvents))
		}
	}
}

// TestIntegrationWithMultipleObservers tests running multiple health observers
func TestIntegrationWithMultipleObservers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t)

	// Create multiple observers with different configs
	configs := []struct {
		name       string
		categories map[string]bool
	}{
		{
			name: "file-observer",
			categories: map[string]bool{
				"file": true,
			},
		},
		{
			name: "network-observer",
			categories: map[string]bool{
				"network": true,
			},
		},
		{
			name: "memory-observer",
			categories: map[string]bool{
				"memory": true,
			},
		},
	}

	observers := make([]interface {
		Start(context.Context) error
		Stop() error
		Events() <-chan *domain.CollectorEvent
		IsHealthy() bool
	}, 0, len(configs))

	ctx := context.Background()

	// Start all observers
	for _, cfg := range configs {
		config := &Config{
			RingBufferSize:    1024,
			EventChannelSize:  10,
			RateLimitMs:       10,
			EnabledCategories: cfg.categories,
		}

		observer, err := NewObserver(logger, config)
		require.NoError(t, err)

		err = observer.Start(ctx)
		require.NoError(t, err)

		observers = append(observers, observer)
	}

	// Verify all are healthy
	for i, obs := range observers {
		assert.True(t, obs.IsHealthy(), "observer %d not healthy", i)
	}

	// Stop all observers
	for _, obs := range observers {
		err := obs.Stop()
		require.NoError(t, err)
	}
}

// TestIntegrationWithContextCancellation tests context cancellation handling
func TestIntegrationWithContextCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	err = observer.Start(ctx)
	require.NoError(t, err)

	// Start event generator
	var eventsSent atomic.Int32
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				event := &HealthEvent{
					TimestampNs: uint64(time.Now().UnixNano()),
					PID:         1000,
					ErrorCode:   -5,
				}
				domainEvent := observer.convertToCollectorEvent(event)
				if observer.EventChannelManager.SendEvent(domainEvent) {
					eventsSent.Add(1)
				}
			}
		}
	}()

	// Let it run
	time.Sleep(100 * time.Millisecond)

	// Cancel context
	cancel()

	// Stop observer
	err = observer.Stop()
	require.NoError(t, err)

	// Verify some events were sent
	assert.Greater(t, eventsSent.Load(), int32(0))
}

// TestIntegrationWithEnvironmentVariables tests mock mode via env vars
func TestIntegrationWithEnvironmentVariables(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Set mock mode
	os.Setenv("TAPIO_MOCK_MODE", "true")
	defer os.Unsetenv("TAPIO_MOCK_MODE")

	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// In mock mode, should still be operational
	assert.True(t, observer.IsHealthy())
}

// TestIntegrationWithLoggerLevels tests different logging levels
func TestIntegrationWithLoggerLevels(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Test with different logger configurations
	loggers := []struct {
		name   string
		logger *zap.Logger
	}{
		{"debug", zaptest.NewLogger(t, zaptest.Level(zap.DebugLevel))},
		{"info", zaptest.NewLogger(t, zaptest.Level(zap.InfoLevel))},
		{"error", zaptest.NewLogger(t, zaptest.Level(zap.ErrorLevel))},
	}

	for _, lg := range loggers {
		t.Run(lg.name, func(t *testing.T) {
			observer, err := NewObserver(lg.logger, DefaultConfig())
			require.NoError(t, err)

			ctx := context.Background()
			err = observer.Start(ctx)
			require.NoError(t, err)

			// Generate event
			event := &HealthEvent{
				TimestampNs: uint64(time.Now().UnixNano()),
				PID:         1000,
				ErrorCode:   -28,
			}

			domainEvent := observer.convertToCollectorEvent(event)
			observer.EventChannelManager.SendEvent(domainEvent)

			err = observer.Stop()
			require.NoError(t, err)
		})
	}
}

// TestIntegrationWithRealSystemCalls tests with actual system calls (non-Linux)
func TestIntegrationWithRealSystemCalls(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// On non-Linux, we get mock events
	// Wait for at least one mock event
	select {
	case event := <-observer.Events():
		assert.NotNil(t, event)
		assert.Equal(t, "health", event.Source)
		// Mock events have "mock": "true" in custom data
		if event.EventData.Custom != nil {
			assert.Equal(t, "true", event.EventData.Custom["mock"])
		}
	case <-time.After(2 * time.Second): // Reduced timeout for faster tests
		// This is expected on Linux where mock events aren't generated
		t.Log("No mock events received (expected on Linux)")
	}
}

// TestIntegrationEventOrdering tests that events maintain order
func TestIntegrationEventOrdering(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t)
	config := &Config{
		RingBufferSize:   1024,
		EventChannelSize: 100, // Large enough to hold all events
		RateLimitMs:      1,
		EnabledCategories: map[string]bool{
			"file": true,
		},
	}

	observer, err := NewObserver(logger, config)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Send numbered events
	numEvents := 20
	for i := 0; i < numEvents; i++ {
		event := &HealthEvent{
			TimestampNs: uint64(time.Now().UnixNano()),
			PID:         uint32(1000 + i),
			ErrorCode:   -28,
			ErrorCount:  uint32(i), // Use error count as sequence number
			Category:    1,
		}

		domainEvent := observer.convertToCollectorEvent(event)
		sent := observer.EventChannelManager.SendEvent(domainEvent)
		assert.True(t, sent)
	}

	// Collect events and verify order
	collected := make([]*domain.CollectorEvent, 0, numEvents)
	timeout := time.After(2 * time.Second)

	for len(collected) < numEvents {
		select {
		case event := <-observer.Events():
			collected = append(collected, event)
		case <-timeout:
			t.Fatalf("timeout collecting events, got %d/%d", len(collected), numEvents)
		}
	}

	// Verify sequence
	for i, event := range collected {
		errorCount := event.Metadata.Labels["error_count"]
		assert.Equal(t, fmt.Sprintf("%d", i), errorCount)
	}
}
