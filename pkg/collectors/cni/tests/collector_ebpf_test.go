//go:build linux
// +build linux

package cni

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectorWithEBPF(t *testing.T) {
	// Skip if not running as root
	if os.Geteuid() != 0 {
		t.Skip("eBPF tests require root privileges")
	}

	collector, err := NewCollector("test-cni-ebpf")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Verify eBPF components were initialized
	assert.NotNil(t, collector.ebpfState)

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	// Should still be healthy
	assert.True(t, collector.IsHealthy())
}

func TestEBPFCleanup(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("eBPF tests require root privileges")
	}

	collector, err := NewCollector("test-cni-cleanup")
	require.NoError(t, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)

	// Stop should cleanup eBPF resources
	err = collector.Stop()
	require.NoError(t, err)

	// eBPF state should be cleaned up
	assert.Nil(t, collector.ebpfState)
	assert.False(t, collector.IsHealthy())
}
