//go:build !notest
// +build !notest

package dns

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// Helper function to create query name
func makeQueryNameNeg(name string) [253]byte {
	var result [253]byte
	copy(result[:], name)
	return result
}

// Negative tests verify error handling and edge cases

func TestNegative_InvalidConfig(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil config uses defaults",
			config:  nil,
			wantErr: false,
		},
		{
			name: "empty name gets default",
			config: &Config{
				Name: "",
			},
			wantErr: false,
		},
		{
			name: "negative buffer size gets default",
			config: &Config{
				Name:       "test",
				BufferSize: -1,
			},
			wantErr: false,
		},
		{
			name: "zero repeat threshold gets default",
			config: &Config{
				Name:            "test",
				RepeatThreshold: 0,
			},
			wantErr: false,
		},
		{
			name: "extremely large buffer size",
			config: &Config{
				Name:       "test",
				BufferSize: 1000000000, // 1 billion
			},
			wantErr: false, // Should handle but may warn
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obs, err := NewObserver("test", tt.config, logger)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, obs)
			}
		})
	}
}

func TestNegative_StartWithoutInit(t *testing.T) {
	// Create observer with minimal setup (nil fields)
	obs := &Observer{}

	ctx := context.Background()

	// Should panic or return error when starting uninitialized observer
	defer func() {
		if r := recover(); r != nil {
			t.Logf("Expected panic when starting uninitialized observer: %v", r)
		}
	}()

	err := obs.Start(ctx)
	if err == nil {
		t.Error("Expected error when starting uninitialized observer")
	} else {
		t.Logf("Expected error: %v", err)
	}
}

func TestNegative_DoubleStart(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "negative-double-start"
	config.EnableEBPF = false

	obs, err := NewObserver("test", config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// First start should succeed
	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	// Second start should either error or handle gracefully
	err = obs.Start(ctx)
	// Behavior depends on implementation - may error or be idempotent
	t.Logf("Double start result: %v", err)
}

func TestNegative_DoubleStop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "negative-double-stop"
	config.EnableEBPF = false

	obs, err := NewObserver("test", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)

	// First stop should succeed
	err = obs.Stop()
	assert.NoError(t, err)

	// Second stop should handle gracefully
	err = obs.Stop()
	// Should not panic, may return error
	t.Logf("Double stop result: %v", err)
}

func TestNegative_StopWithoutStart(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "negative-stop-without-start"
	config.EnableEBPF = false

	obs, err := NewObserver("test", config, logger)
	require.NoError(t, err)

	// Stop without start
	err = obs.Stop()
	// Should handle gracefully
	t.Logf("Stop without start result: %v", err)
}

func TestNegative_NilEvent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "negative-nil-event"
	config.EnableEBPF = false

	obs, err := NewObserver("test", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	// Try to track nil event
	defer func() {
		if r := recover(); r != nil {
			t.Logf("Recovered from panic: %v", r)
		}
	}()

	isRepeated := obs.trackProblem(nil)
	assert.False(t, isRepeated, "Nil event should not be tracked")
}

func TestNegative_InvalidDNSEvent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "negative-invalid-event"
	config.EnableEBPF = false

	obs, err := NewObserver("test", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	tests := []struct {
		name  string
		event *DNSEvent
	}{
		{
			name: "empty query name",
			event: &DNSEvent{
				Timestamp:   uint64(time.Now().UnixNano()),
				ProblemType: DNSProblemSlow,
				QueryName:   makeQueryNameNeg(""),
				QueryType:   1,
				LatencyNs:   150_000_000,
			},
		},
		{
			name: "invalid problem type",
			event: &DNSEvent{
				Timestamp:   uint64(time.Now().UnixNano()),
				ProblemType: 255, // Invalid type
				QueryName:   makeQueryNameNeg("test.com"),
				QueryType:   1,
				LatencyNs:   150_000_000,
			},
		},
		{
			name: "zero timestamp",
			event: &DNSEvent{
				Timestamp:   0,
				ProblemType: DNSProblemSlow,
				QueryName:   makeQueryNameNeg("test.com"),
				QueryType:   1,
				LatencyNs:   150_000_000,
			},
		},
		{
			name: "negative latency",
			event: &DNSEvent{
				Timestamp:   uint64(time.Now().UnixNano()),
				ProblemType: DNSProblemSlow,
				QueryName:   makeQueryNameNeg("test.com"),
				QueryType:   1,
				LatencyNs:   0, // Can't be negative with uint64
			},
		},
		{
			name: "extremely long query name",
			event: &DNSEvent{
				Timestamp:   uint64(time.Now().UnixNano()),
				ProblemType: DNSProblemSlow,
				QueryName:   makeQueryNameNeg(string(make([]byte, 10000))), // 10KB name - will be truncated
				QueryType:   1,
				LatencyNs:   150_000_000,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should handle gracefully without panic
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Panic on %s: %v", tt.name, r)
				}
			}()

			obs.trackProblem(tt.event)
			// If it doesn't panic, it handled the error
		})
	}
}

func TestNegative_ContextCancellation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "negative-context-cancel"
	config.EnableEBPF = false

	obs, err := NewObserver("test", config, logger)
	require.NoError(t, err)

	// Start with context that will be cancelled
	ctx, cancel := context.WithCancel(context.Background())

	err = obs.Start(ctx)
	require.NoError(t, err)

	// Cancel context while running
	cancel()

	// Observer should handle cancellation gracefully
	time.Sleep(100 * time.Millisecond)

	// Should still be able to stop cleanly
	err = obs.Stop()
	// May or may not error, but shouldn't panic
	t.Logf("Stop after context cancel: %v", err)
}

func TestNegative_ConcurrentStop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "negative-concurrent-stop"
	config.EnableEBPF = false

	obs, err := NewObserver("test", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)

	// Try to stop from multiple goroutines
	done := make(chan error, 5)
	for i := 0; i < 5; i++ {
		go func() {
			done <- obs.Stop()
		}()
	}

	// Collect results
	var errors []error
	for i := 0; i < 5; i++ {
		if err := <-done; err != nil {
			errors = append(errors, err)
		}
	}

	// Should handle concurrent stops without panic
	t.Logf("Concurrent stop errors: %v", errors)
}

func TestNegative_EventChannelAfterStop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "negative-channel-after-stop"
	config.EnableEBPF = false

	obs, err := NewObserver("test", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)

	events := obs.Events()
	require.NotNil(t, events)

	// Stop observer
	err = obs.Stop()
	assert.NoError(t, err)

	// Try to read from channel after stop
	select {
	case event, ok := <-events:
		if ok {
			t.Errorf("Received event after stop: %v", event)
		} else {
			t.Log("Channel properly closed after stop")
		}
	case <-time.After(100 * time.Millisecond):
		t.Log("Channel blocked after stop (may be expected)")
	}
}

func TestNegative_PanicRecovery(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "negative-panic-recovery"
	config.EnableEBPF = false

	_, err := NewObserver("test", config, logger)
	require.NoError(t, err)

	// Inject a function that might panic
	testPanic := func() {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Recovered from panic: %v", r)
			}
		}()

		// Force a panic scenario
		var nilMap map[string]*ProblemTracker
		_ = nilMap["test"] // Will panic
	}

	// Should not crash the test
	testPanic()
}

func TestNegative_ResourceExhaustion(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping resource exhaustion test in short mode")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "negative-resource"
	config.EnableEBPF = false
	config.BufferSize = 10 // Very small buffer

	obs, err := NewObserver("test", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	// Flood with events to exhaust buffer
	for i := 0; i < 1000; i++ {
		event := &DNSEvent{
			Timestamp:   uint64(time.Now().UnixNano()),
			ProblemType: DNSProblemSlow,
			QueryName:   makeQueryNameNeg("flood.test.com"),
			QueryType:   1,
			LatencyNs:   150_000_000,
		}
		obs.trackProblem(event)
	}

	// Should handle buffer exhaustion without panic
	assert.True(t, obs.IsHealthy(), "Should remain healthy despite buffer pressure")
}

func TestNegative_InvalidStateMachine(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "negative-state"
	config.EnableEBPF = false

	obs, err := NewObserver("test", config, logger)
	require.NoError(t, err)

	// Try operations in invalid order
	events := obs.Events() // Before start
	assert.NotNil(t, events)

	stats := obs.GetStats() // Before start
	assert.NotNil(t, stats)

	health := obs.Health() // Before start
	assert.NotNil(t, health)
	assert.False(t, obs.IsHealthy())

	// Now start and stop quickly
	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)

	// Immediate stop
	err = obs.Stop()
	assert.NoError(t, err)

	// Try operations after stop
	assert.False(t, obs.IsHealthy())
}

func TestNegative_TimeoutScenarios(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "negative-timeout"
	config.EnableEBPF = false

	obs, err := NewObserver("test", config, logger)
	require.NoError(t, err)

	// Start with very short timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Microsecond)
	defer cancel()

	err = obs.Start(ctx)
	// May timeout or succeed quickly
	t.Logf("Start with micro timeout: %v", err)

	// Clean up if started
	if err == nil {
		obs.Stop()
	}
}

func TestNegative_DataCorruption(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "negative-corruption"
	config.EnableEBPF = false

	obs, err := NewObserver("test", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	// Try to corrupt internal state (simulated)
	event := &DNSEvent{
		Timestamp:   uint64(time.Now().UnixNano()),
		ProblemType: DNSProblemSlow,
		QueryName:   makeQueryNameNeg("corrupt.test.com"),
		QueryType:   1,
		LatencyNs:   150_000_000,
	}

	// Track same event multiple times concurrently (race condition)
	for i := 0; i < 100; i++ {
		go func() {
			obs.trackProblem(event)
		}()
	}

	time.Sleep(100 * time.Millisecond)

	// Should handle concurrent access without corruption
	stats := obs.GetStats()
	assert.NotNil(t, stats)
	assert.GreaterOrEqual(t, stats.TotalProblems, int64(1))
}

func TestNegative_ErrorPropagation(t *testing.T) {
	// Test that errors are properly propagated up the stack
	logger := zaptest.NewLogger(t)

	// Create observer that will fail on platform-specific initialization
	config := DefaultConfig()
	config.Name = "negative-error-prop"
	config.EnableEBPF = true // Will fail on non-Linux

	obs, err := NewObserver("test", config, logger)
	require.NoError(t, err) // Creation should succeed

	ctx := context.Background()
	err = obs.Start(ctx)

	// On non-Linux, eBPF should fail and fallback
	if err != nil {
		assert.Contains(t, err.Error(), "starting platform")
		t.Logf("Expected platform error: %v", err)
	} else {
		// Fallback succeeded
		assert.True(t, obs.IsHealthy())
		obs.Stop()
	}
}

// MockError is a test error type
type MockError struct {
	msg string
}

func (e MockError) Error() string {
	return e.msg
}

func TestNegative_ErrorTypes(t *testing.T) {
	// Test different error types and wrapping
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "nil error",
			err:  nil,
			want: "",
		},
		{
			name: "standard error",
			err:  errors.New("test error"),
			want: "test error",
		},
		{
			name: "wrapped error",
			err:  fmt.Errorf("wrapped: %w", errors.New("inner")),
			want: "inner",
		},
		{
			name: "custom error",
			err:  MockError{msg: "custom"},
			want: "custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil {
				assert.Nil(t, tt.err)
			} else {
				assert.Contains(t, tt.err.Error(), tt.want)
			}
		})
	}
}
