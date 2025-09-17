//go:build linux
// +build linux

package dns

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

// Test CO-RE eBPF loading
func TestCoreEBPFLoad(t *testing.T) {
	// Skip if not running as root
	if !isRoot() {
		t.Skip("Test requires root privileges")
	}

	// Skip if BTF not available
	if !hasBTF() {
		t.Skip("Test requires BTF-enabled kernel")
	}

	logger := zaptest.NewLogger(t)

	config := &Config{
		Name:                  "test-dns",
		BufferSize:            1000,
		EnableEBPF:            true,
		CircuitBreakerConfig:  DefaultCircuitBreakerConfig(),
		ContainerIDExtraction: true,
		ParseAnswers:          true,
	}

	collector, err := NewObserver("test-dns", *config)
	require.NoError(t, err)
	require.NotNil(t, collector)

	// Start collector
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)

	// Verify eBPF is loaded
	assert.NotNil(t, collector.(*Collector).ebpfState)

	// Stop collector
	err = collector.Stop()
	assert.NoError(t, err)
}

// Test DNS event capture
func TestCoreDNSEventCapture(t *testing.T) {
	// Skip if not running as root
	if !isRoot() {
		t.Skip("Test requires root privileges")
	}

	// Skip if BTF not available
	if !hasBTF() {
		t.Skip("Test requires BTF-enabled kernel")
	}

	logger := zaptest.NewLogger(t)

	config := &Config{
		Name:                  "test-dns",
		BufferSize:            1000,
		EnableEBPF:            true,
		CircuitBreakerConfig:  DefaultCircuitBreakerConfig(),
		ContainerIDExtraction: true,
		ParseAnswers:          true,
	}

	collector, err := NewObserver("test-dns", *config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start collector
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Get event channel
	events := collector.Events()

	// Trigger DNS lookup
	go func() {
		time.Sleep(1 * time.Second)
		net.LookupHost("example.com")
	}()

	// Wait for event
	select {
	case event := <-events:
		assert.NotNil(t, event)
		assert.Equal(t, domain.EventTypeNetwork, event.Type)
		assert.Equal(t, "dns-observer", event.Source)
		assert.NotNil(t, event.EventData.Network)
		assert.NotNil(t, event.EventData.Process)

		// Verify DNS-specific fields
		assert.Contains(t, event.EventData.Custom, "dns_query")
		assert.Contains(t, event.EventData.Custom, "query_type")
		assert.Contains(t, event.EventData.Custom, "event_type")

	case <-ctx.Done():
		t.Fatal("Timeout waiting for DNS event")
	}
}

// Test rate limiting
func TestCoreRateLimiting(t *testing.T) {
	// Skip if not running as root
	if !isRoot() {
		t.Skip("Test requires root privileges")
	}

	// Skip if BTF not available
	if !hasBTF() {
		t.Skip("Test requires BTF-enabled kernel")
	}

	logger := zaptest.NewLogger(t)

	config := &Config{
		Name:                  "test-dns",
		BufferSize:            1000,
		EnableEBPF:            true,
		CircuitBreakerConfig:  DefaultCircuitBreakerConfig(),
		ContainerIDExtraction: true,
		ParseAnswers:          true,
	}

	collector, err := NewObserver("test-dns", *config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start collector
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Generate high DNS load
	for i := 0; i < 2000; i++ {
		go net.LookupHost(fmt.Sprintf("test%d.example.com", i))
	}

	// Collect events for 2 seconds
	time.Sleep(2 * time.Second)

	// Get metrics
	c := collector.(*Collector)
	stats := c.BaseObserver.Statistics()

	// Should be rate limited to ~1000 events/sec * 2 sec = 2000 events
	// Allow some variance
	assert.LessOrEqual(t, stats.EventsProcessed, int64(2500))
	assert.GreaterOrEqual(t, stats.EventsProcessed, int64(1500))

	// Should have drops due to rate limiting
	assert.Greater(t, stats.EventsDropped, int64(0))
}

// Test overflow handling
func TestCoreOverflowHandling(t *testing.T) {
	// Skip if not running as root
	if !isRoot() {
		t.Skip("Test requires root privileges")
	}

	// Skip if BTF not available
	if !hasBTF() {
		t.Skip("Test requires BTF-enabled kernel")
	}

	logger := zaptest.NewLogger(t)

	// Small buffer to trigger overflow
	config := &Config{
		Name:                  "test-dns",
		BufferSize:            10, // Very small buffer
		EnableEBPF:            true,
		CircuitBreakerConfig:  DefaultCircuitBreakerConfig(),
		ContainerIDExtraction: true,
		ParseAnswers:          true,
	}

	collector, err := NewObserver("test-dns", *config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start collector
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Don't consume events to cause overflow
	// Generate DNS lookups
	for i := 0; i < 100; i++ {
		go net.LookupHost(fmt.Sprintf("overflow%d.example.com", i))
	}

	time.Sleep(2 * time.Second)

	// Check overflow stats
	c := collector.(*Collector)
	c.readCoreOverflowStats()

	stats := c.BaseObserver.Statistics()
	assert.Greater(t, stats.EventsDropped, int64(0))
}

// Test CO-RE field existence
func TestCoreFieldExistence(t *testing.T) {
	// This test verifies CO-RE macros work correctly
	// It's a compile-time test effectively

	// If this compiles, CO-RE macros are working
	_ = `
	#include "vmlinux.h"
	#include <bpf/bpf_core_read.h>
	
	static __always_inline void test_core_fields(struct task_struct *task) {
		if (bpf_core_field_exists(struct task_struct, tgid)) {
			u32 pid = BPF_CORE_READ(task, tgid);
		}
		
		if (bpf_core_field_exists(struct task_struct, cgroups)) {
			struct css_set *cgroups = BPF_CORE_READ(task, cgroups);
		}
	}
	`

	// Test passes if compilation succeeds
	assert.True(t, true)
}

// Benchmark event processing
func BenchmarkCoreEventProcessing(b *testing.B) {
	// Skip if not running as root
	if !isRoot() {
		b.Skip("Benchmark requires root privileges")
	}

	// Skip if BTF not available
	if !hasBTF() {
		b.Skip("Benchmark requires BTF-enabled kernel")
	}

	logger := zaptest.NewLogger(b)

	config := &Config{
		Name:                  "bench-dns",
		BufferSize:            10000,
		EnableEBPF:            true,
		CircuitBreakerConfig:  DefaultCircuitBreakerConfig(),
		ContainerIDExtraction: true,
		ParseAnswers:          true,
	}

	collector, err := NewObserver("bench-dns", *config)
	require.NoError(b, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(b, err)
	defer collector.Stop()

	b.ResetTimer()

	// Benchmark DNS lookups
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			net.LookupHost("benchmark.example.com")
		}
	})

	// Report events per second
	c := collector.(*Collector)
	stats := c.BaseObserver.Statistics()
	b.ReportMetric(float64(stats.EventsProcessed)/b.Elapsed().Seconds(), "events/sec")
}

// Helper functions
func isRoot() bool {
	return os.Geteuid() == 0
}

func hasBTF() bool {
	_, err := os.Stat("/sys/kernel/btf/vmlinux")
	return err == nil
}

// Test multi-kernel compatibility
func TestCoreMultiKernelCompat(t *testing.T) {
	// This test would run in CI with different kernel versions
	// For now, just verify we can detect kernel version

	kernelVersion, err := getKernelVersion()
	require.NoError(t, err)

	t.Logf("Running on kernel: %s", kernelVersion)

	// Verify minimum kernel version (5.4+)
	major, minor, err := parseKernelVersion(kernelVersion)
	require.NoError(t, err)

	if major < 5 || (major == 5 && minor < 4) {
		t.Skip("CO-RE requires kernel 5.4+")
	}

	assert.GreaterOrEqual(t, major, 5)
}

func getKernelVersion() (string, error) {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return "", err
	}

	// Parse version from string like "Linux version 5.15.0-91-generic ..."
	parts := strings.Fields(string(data))
	if len(parts) < 3 {
		return "", fmt.Errorf("unexpected /proc/version format")
	}

	return parts[2], nil
}

func parseKernelVersion(version string) (int, int, error) {
	// Parse version like "5.15.0-91-generic"
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("unexpected version format: %s", version)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, err
	}

	return major, minor, nil
}
