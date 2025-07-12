package sources

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/correlation"
)

func TestNewEBPFSource(t *testing.T) {
	source := NewEBPFSource()
	
	assert.NotNil(t, source)
	assert.Equal(t, "ebpf", source.name)
	assert.NotNil(t, source.platform)
	assert.NotNil(t, source.collector)
	assert.False(t, source.started)
}

func TestEBPFSource_Name(t *testing.T) {
	source := NewEBPFSource()
	assert.Equal(t, "ebpf", source.Name())
}

func TestEBPFSource_GetType(t *testing.T) {
	source := NewEBPFSource()
	assert.Equal(t, correlation.SourceEBPF, source.GetType())
}

func TestEBPFSource_IsAvailable(t *testing.T) {
	source := NewEBPFSource()
	
	// Since we're using mock collector, it should be available
	available := source.IsAvailable()
	assert.True(t, available)
}

func TestEBPFSource_IsAvailableWithContext(t *testing.T) {
	source := NewEBPFSource()
	ctx := context.Background()
	
	available := source.IsAvailableWithContext(ctx)
	assert.True(t, available)
}

func TestEBPFSource_StartStop(t *testing.T) {
	source := NewEBPFSource()
	ctx := context.Background()

	// Test start
	err := source.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, source.started)

	// Test double start
	err = source.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already started")

	// Test stop
	err = source.Stop(ctx)
	assert.NoError(t, err)
	assert.False(t, source.started)

	// Test double stop is safe
	err = source.Stop(ctx)
	assert.NoError(t, err)
}

func TestEBPFSource_Collect(t *testing.T) {
	source := NewEBPFSource()
	ctx := context.Background()

	// Should fail when not started
	_, err := source.Collect(ctx, []collectors.Target{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not started")

	// Start the source
	err = source.Start(ctx)
	require.NoError(t, err)
	defer source.Stop(ctx)

	// Create test targets
	targets := []collectors.Target{
		{
			ID:   "test-target",
			Type: "pod",
			Name: "test-pod",
			PID:  1234,
		},
	}

	// Should succeed when started
	dataset, err := source.Collect(ctx, targets)
	assert.NoError(t, err)
	assert.NotEmpty(t, dataset.Source)
}

func TestEBPFSource_SupportsTarget(t *testing.T) {
	source := NewEBPFSource()

	tests := []struct {
		target   collectors.Target
		expected bool
	}{
		{
			target:   collectors.Target{Type: "pod"},
			expected: true,
		},
		{
			target:   collectors.Target{Type: "container"},
			expected: true,
		},
		{
			target:   collectors.Target{Type: "process"},
			expected: true,
		},
		{
			target:   collectors.Target{Type: "service", PID: 1234},
			expected: true,
		},
		{
			target:   collectors.Target{Type: "service", PID: 0},
			expected: false,
		},
		{
			target:   collectors.Target{Type: "node"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.target.Type, func(t *testing.T) {
			result := source.SupportsTarget(tt.target)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEBPFSource_GetPlatformInfo(t *testing.T) {
	source := NewEBPFSource()
	
	platform := source.GetPlatformInfo()
	assert.NotNil(t, platform)
	assert.NotEmpty(t, platform.OS)
	assert.NotEmpty(t, platform.Arch)
}

func TestEBPFSource_GetCapabilities(t *testing.T) {
	source := NewEBPFSource()
	ctx := context.Background()
	
	capabilities := source.GetCapabilities(ctx)
	assert.NotNil(t, capabilities)
}

func TestEBPFSource_IsUsingMock(t *testing.T) {
	source := NewEBPFSource()
	
	// Should be using mock collector on non-Linux platforms
	assert.True(t, source.IsUsingMock())
}

func TestEBPFSource_MockScenarios(t *testing.T) {
	source := NewEBPFSource()
	
	// Should be able to get available scenarios
	scenarios := source.GetAvailableMockScenarios()
	assert.NotEmpty(t, scenarios)
	
	// Should be able to set scenario
	err := source.SetMockScenario("high_memory_usage")
	assert.NoError(t, err)
}

func TestEBPFSource_GetData(t *testing.T) {
	source := NewEBPFSource()
	ctx := context.Background()

	// Should fail when not started
	_, err := source.GetData(ctx, "process_stats", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not started")

	// Start the source
	err = source.Start(ctx)
	require.NoError(t, err)
	defer source.Stop(ctx)

	// Should return eBPF data when started
	data, err := source.GetData(ctx, "process_stats", nil)
	assert.NoError(t, err)
	assert.NotNil(t, data)
	
	// Should be eBPFData type
	ebpfData, ok := data.(*correlation.EBPFData)
	assert.True(t, ok)
	assert.NotNil(t, ebpfData.ProcessStats)
	assert.NotNil(t, ebpfData.SystemMetrics)
	assert.NotZero(t, ebpfData.Timestamp)
}

func TestEBPFSource_Lifecycle(t *testing.T) {
	source := NewEBPFSource()
	ctx := context.Background()

	// Initial state
	assert.False(t, source.started)
	assert.True(t, source.IsUsingMock())

	// Start
	err := source.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, source.started)

	// Collect data
	targets := []collectors.Target{
		{
			ID:   "lifecycle-test",
			Type: "process",
			PID:  5678,
		},
	}

	dataset, err := source.Collect(ctx, targets)
	assert.NoError(t, err)
	assert.Contains(t, dataset.Source, "mock")

	// Get data
	data, err := source.GetData(ctx, "memory_stats", nil)
	assert.NoError(t, err)
	assert.NotNil(t, data)

	// Stop
	err = source.Stop(ctx)
	assert.NoError(t, err)
	assert.False(t, source.started)

	// Should fail after stop
	_, err = source.Collect(ctx, targets)
	assert.Error(t, err)
}

func TestEBPFSource_ConcurrentAccess(t *testing.T) {
	source := NewEBPFSource()
	ctx := context.Background()

	err := source.Start(ctx)
	require.NoError(t, err)
	defer source.Stop(ctx)

	targets := []collectors.Target{
		{ID: "concurrent-test", Type: "pod", PID: 9999},
	}

	// Test concurrent access
	results := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func() {
			_, err := source.Collect(ctx, targets)
			results <- err
		}()
	}

	// Collect results
	for i := 0; i < 10; i++ {
		err := <-results
		assert.NoError(t, err)
	}
}

// Benchmark tests
func BenchmarkEBPFSource_Collect(b *testing.B) {
	source := NewEBPFSource()
	ctx := context.Background()

	err := source.Start(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer source.Stop(ctx)

	targets := []collectors.Target{
		{ID: "bench-target", Type: "process", PID: 1234},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := source.Collect(ctx, targets)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEBPFSource_GetData(b *testing.B) {
	source := NewEBPFSource()
	ctx := context.Background()

	err := source.Start(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer source.Stop(ctx)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := source.GetData(ctx, "process_stats", nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEBPFSource_SupportsTarget(b *testing.B) {
	source := NewEBPFSource()
	target := collectors.Target{
		Type: "pod",
		PID:  1234,
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		source.SupportsTarget(target)
	}
}

func BenchmarkEBPFSource_StartStop(b *testing.B) {
	ctx := context.Background()

	for i := 0; i < b.N; i++ {
		source := NewEBPFSource()
		source.Start(ctx)
		source.Stop(ctx)
	}
}