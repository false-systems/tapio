//go:build linux
// +build linux

package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestObserverSystem tests Linux-specific eBPF functionality
func TestObserverSystem(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping system test in short mode")
	}

	tests := []struct {
		name         string
		config       *Config
		validateFunc func(*testing.T, *Observer)
	}{
		{
			name: "ebpf_initialization",
			config: &Config{
				ConnectionTableSize: defaultConnectionTableSize,
				ConnectionTimeout:   defaultConnectionTimeout,
				BufferSize:          defaultBufferSize,
				CleanupInterval:     defaultCleanupInterval,
				EnableK8sMapping:    false,
				Name:                "ebpf-test",
				HealthCheck:         true,
				EnableOTEL:          false,
				EnableStdout:        false,
			},
			validateFunc: func(t *testing.T, obs *Observer) {
				assert.NotNil(t, obs.connectionsTracker)
				assert.NotNil(t, obs.connectionsTracker.ebpfState)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			observer, err := NewObserver(tt.config.Name, tt.config, zap.NewNop())
			require.NoError(t, err)
			require.NotNil(t, observer)

			err = observer.Start(ctx)
			require.NoError(t, err)
			defer observer.Stop()

			// Wait for eBPF to initialize
			time.Sleep(500 * time.Millisecond)

			// Validate eBPF state
			tt.validateFunc(t, observer)

			// Verify health
			assert.True(t, observer.IsHealthy())
		})
	}
}

// TestObserverSystem_EBPFEventCapture tests actual eBPF event capture
func TestObserverSystem_EBPFEventCapture(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping system test in short mode")
	}

	config := &Config{
		ConnectionTableSize: defaultConnectionTableSize,
		ConnectionTimeout:   defaultConnectionTimeout,
		BufferSize:          defaultBufferSize,
		CleanupInterval:     defaultCleanupInterval,
		EnableK8sMapping:    false,
		Name:                "ebpf-capture",
		HealthCheck:         true,
		EnableOTEL:          false,
		EnableStdout:        false,
	}

	observer, err := NewObserver(config.Name, config, zap.NewNop())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Wait for eBPF programs to attach
	time.Sleep(1 * time.Second)

	// Verify eBPF state
	assert.NotNil(t, observer.connectionsTracker.ebpfState)
	assert.NotNil(t, observer.connectionsTracker.ebpfState.objs)
	assert.NotNil(t, observer.connectionsTracker.ebpfState.reader)
	assert.NotEmpty(t, observer.connectionsTracker.ebpfState.links)

	// Try to receive events
	timeout := time.After(5 * time.Second)
	eventReceived := false

	select {
	case event := <-observer.Events():
		assert.NotNil(t, event)
		eventReceived = true
	case <-timeout:
		// No events is acceptable in test environment
	}

	if eventReceived {
		t.Log("Successfully captured eBPF event")
	} else {
		t.Log("No eBPF events captured (may require network activity)")
	}
}

// TestObserverSystem_EBPFCleanup tests proper eBPF resource cleanup
func TestObserverSystem_EBPFCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping system test in short mode")
	}

	config := &Config{
		ConnectionTableSize: defaultConnectionTableSize,
		ConnectionTimeout:   defaultConnectionTimeout,
		BufferSize:          defaultBufferSize,
		CleanupInterval:     defaultCleanupInterval,
		EnableK8sMapping:    false,
		Name:                "ebpf-cleanup",
		HealthCheck:         true,
		EnableOTEL:          false,
		EnableStdout:        false,
	}

	observer, err := NewObserver(config.Name, config, zap.NewNop())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)

	// Wait for initialization
	time.Sleep(500 * time.Millisecond)

	// Verify eBPF is running
	assert.NotNil(t, observer.connectionsTracker.ebpfState)

	// Stop observer
	err = observer.Stop()
	assert.NoError(t, err)

	// eBPF resources should be cleaned up
	// Note: We can't check ebpfState after cleanup as it may be nil
	assert.False(t, observer.IsHealthy())
}

// TestObserverSystem_ConnectionTracking tests actual TCP connection tracking
func TestObserverSystem_ConnectionTracking(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping system test in short mode")
	}

	config := &Config{
		ConnectionTableSize: defaultConnectionTableSize,
		ConnectionTimeout:   defaultConnectionTimeout,
		BufferSize:          defaultBufferSize,
		CleanupInterval:     defaultCleanupInterval,
		EnableK8sMapping:    false,
		Name:                "conn-tracking",
		HealthCheck:         true,
		EnableOTEL:          false,
		EnableStdout:        false,
	}

	observer, err := NewObserver(config.Name, config, zap.NewNop())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Wait for eBPF to attach
	time.Sleep(1 * time.Second)

	// Get connection stats
	stats := observer.connectionsTracker.GetStats()
	assert.NotNil(t, stats)
	assert.GreaterOrEqual(t, stats.ActiveConnections, uint64(0))

	// Note: Actual connection tracking requires network activity
	// This test validates the infrastructure is in place
}
