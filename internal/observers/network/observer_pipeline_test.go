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

func TestNetworkObserver_PipelineStage(t *testing.T) {
	setupOTEL(t)

	config := Config{
		Output: base.OutputConfig{Stdout: true},
	}

	observer, err := NewNetworkObserver("test-pipeline", config)
	require.NoError(t, err)

	// Load eBPF (needed for pipeline stage)
	ctx := context.Background()
	err = observer.loadeBPF(ctx)
	require.NoError(t, err)

	// Start observer which launches pipeline
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	go func() {
		_ = observer.Start(ctx)
	}()

	// Wait for pipeline to start
	time.Sleep(10 * time.Millisecond)
	assert.True(t, observer.IsHealthy(), "Observer should be healthy while pipeline runs")

	// Wait for context to cancel
	<-ctx.Done()

	// Give time for graceful shutdown
	time.Sleep(10 * time.Millisecond)
}

func TestNetworkObserver_PipelineContextCancellation(t *testing.T) {
	setupOTEL(t)

	config := Config{
		Output: base.OutputConfig{Stdout: true},
	}

	observer, err := NewNetworkObserver("test-cancel", config)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.loadeBPF(ctx)
	require.NoError(t, err)

	// Start with short-lived context
	ctx, cancel := context.WithCancel(context.Background())

	errChan := make(chan error, 1)
	go func() {
		errChan <- observer.Start(ctx)
	}()

	time.Sleep(10 * time.Millisecond)

	// Cancel context
	cancel()

	// Should exit cleanly
	select {
	case err := <-errChan:
		assert.NoError(t, err, "Pipeline should exit cleanly on context cancel")
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Pipeline did not exit after context cancellation")
	}
}
