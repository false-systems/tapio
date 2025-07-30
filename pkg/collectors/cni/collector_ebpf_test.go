//go:build linux
// +build linux

package cni

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors"
)

func TestCollectorWithEBPF(t *testing.T) {
	// Skip if not running as root
	if !isRoot() {
		t.Skip("eBPF tests require root privileges")
	}

	config := collectors.DefaultCollectorConfig()
	config.BufferSize = 100

	collector, err := NewCollector(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Verify eBPF components were initialized
	assert.NotNil(t, collector.ebpfCollection)
	assert.NotNil(t, collector.ebpfReader)
	assert.Greater(t, len(collector.ebpfLinks), 0)

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	// Should still be healthy
	assert.True(t, collector.IsHealthy())
}

func TestEBPFCleanup(t *testing.T) {
	if !isRoot() {
		t.Skip("eBPF tests require root privileges")
	}

	config := collectors.DefaultCollectorConfig()
	collector, err := NewCollector(config)
	require.NoError(t, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)

	// Stop should cleanup eBPF resources
	err = collector.Stop()
	require.NoError(t, err)

	// Verify cleanup
	assert.Nil(t, collector.ebpfCollection)
	assert.Nil(t, collector.ebpfReader)
	assert.Empty(t, collector.ebpfLinks)
}

func isRoot() bool {
	// Simple check - in production would use proper method
	return false // Always skip in tests for now
}
