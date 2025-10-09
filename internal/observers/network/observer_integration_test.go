//go:build linux

package network

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/base"
)

func TestNetworkObserver_LoadeBPF(t *testing.T) {
	setupOTEL(t)

	config := Config{
		Output: base.OutputConfig{Stdout: true},
	}

	observer, err := NewNetworkObserver("test-network", config)
	require.NoError(t, err)

	// Load eBPF program
	ctx := context.Background()
	err = observer.loadeBPF(ctx)
	require.NoError(t, err, "eBPF program should load successfully")

	// Cleanup
	if observer.ebpfManager != nil {
		observer.ebpfManager.Close()
	}
}

func TestNetworkObserver_Lifecycle(t *testing.T) {
	setupOTEL(t)

	config := Config{
		Output: base.OutputConfig{Stdout: true},
	}

	observer, err := NewNetworkObserver("test-network", config)
	require.NoError(t, err)

	// Start observer
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	go func() {
		_ = observer.Start(ctx)
	}()

	// Wait for observer to be running
	time.Sleep(10 * time.Millisecond)
	assert.True(t, observer.IsHealthy())

	// Stop observer
	err = observer.Stop()
	require.NoError(t, err)
	assert.False(t, observer.IsHealthy())
}
