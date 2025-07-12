package unified

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnifiedSystem_NewSystem(t *testing.T) {
	tests := []struct {
		name   string
		config *SystemConfig
		want   bool
	}{
		{
			name:   "default config",
			config: nil,
			want:   true,
		},
		{
			name:   "custom config",
			config: DefaultSystemConfig(),
			want:   true,
		},
		{
			name: "minimal config",
			config: &SystemConfig{
				EnableNetworkMonitoring: false,
				EnableSystemd:           false,
				EnableJournald:          false,
				EventBufferSize:         1000,
				MaxEventsPerSecond:      1000,
				BatchSize:               100,
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			system, err := NewUnifiedSystem(tt.config)
			if tt.want {
				require.NoError(t, err)
				assert.NotNil(t, system)
				assert.NotNil(t, system.config)
				assert.NotNil(t, system.ctx)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestUnifiedSystem_StartStop(t *testing.T) {
	system, err := NewUnifiedSystem(nil)
	require.NoError(t, err)

	// Test start
	err = system.Start()
	assert.NoError(t, err)
	assert.True(t, system.isRunning)

	// Test double start
	err = system.Start()
	assert.Error(t, err)

	// Test stop
	err = system.Stop()
	assert.NoError(t, err)
	assert.False(t, system.isRunning)

	// Test double stop
	err = system.Stop()
	assert.NoError(t, err)
}

func TestUnifiedSystem_GetMetrics(t *testing.T) {
	system, err := NewUnifiedSystem(nil)
	require.NoError(t, err)

	// Get metrics before starting
	metrics := system.GetMetrics()
	assert.NotZero(t, metrics.Uptime)
	assert.False(t, metrics.IsRunning)

	// Start and get metrics
	err = system.Start()
	require.NoError(t, err)
	defer system.Stop()

	// Wait a bit for system to initialize
	time.Sleep(100 * time.Millisecond)

	metrics = system.GetMetrics()
	assert.True(t, metrics.IsRunning)
	assert.NotZero(t, metrics.Uptime)
	assert.GreaterOrEqual(t, metrics.CPUUsage, 0.0)
	assert.GreaterOrEqual(t, metrics.MemoryUsage, 0.0)
}

func TestUnifiedSystem_ProcessEvents(t *testing.T) {
	config := &SystemConfig{
		EnableNetworkMonitoring: false,
		EnableSystemd:           false,
		EnableJournald:          false,
		EventBufferSize:         100,
		MaxEventsPerSecond:      1000,
		BatchSize:               10,
		EnableCircuitBreaker:    false,
		EnableSelfHealing:       false,
		EnableLoadShedding:      false,
	}

	system, err := NewUnifiedSystem(config)
	require.NoError(t, err)

	err = system.Start()
	require.NoError(t, err)
	defer system.Stop()

	// Let it run briefly
	time.Sleep(200 * time.Millisecond)

	metrics := system.GetMetrics()
	// Should have some basic metrics even with minimal config
	assert.NotZero(t, metrics.Uptime)
}

func TestDefaultSystemConfig(t *testing.T) {
	config := DefaultSystemConfig()

	assert.True(t, config.EnableNetworkMonitoring)
	assert.True(t, config.EnableDNSMonitoring)
	assert.True(t, config.EnableProtocolAnalysis)
	assert.True(t, config.EnableSystemd)
	assert.True(t, config.EnableJournald)
	assert.True(t, config.EnableCircuitBreaker)
	assert.True(t, config.EnableSelfHealing)
	assert.True(t, config.EnableLoadShedding)
	assert.True(t, config.EnablePatternAnalysis)

	assert.Equal(t, 100000, config.EventBufferSize)
	assert.Equal(t, 165000, config.MaxEventsPerSecond)
	assert.Equal(t, 1000, config.BatchSize)
	assert.Equal(t, 64*1024, config.PerCPUBufferSize)
	assert.Equal(t, uint32(5), config.MaxFailures)
	assert.Equal(t, 5*time.Minute, config.CorrelationWindow)
	assert.Equal(t, 100, config.MaxMemoryMB)
	assert.Equal(t, 50, config.MaxCPUPercent)
}

func TestSystemEvent_Priority(t *testing.T) {
	system, err := NewUnifiedSystem(nil)
	require.NoError(t, err)

	tests := []struct {
		eventType string
		expected  int
	}{
		{"error", 5},
		{"oom", 5},
		{"crash", 5},
		{"warning", 3},
		{"throttle", 3},
		{"info", 1},
		{"normal", 1},
		{"unknown", 1},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			event := SystemEvent{Type: tt.eventType}
			priority := system.mapEventPriority(event)
			assert.Equal(t, tt.expected, priority)
		})
	}
}

func TestUnifiedSystem_ComponentHealth(t *testing.T) {
	config := &SystemConfig{
		EnableSelfHealing: true,
		EnableSystemd:     true,
		EnableJournald:    true,
	}

	system, err := NewUnifiedSystem(config)
	require.NoError(t, err)

	err = system.Start()
	require.NoError(t, err)
	defer system.Stop()

	// Wait for components to register
	time.Sleep(100 * time.Millisecond)

	// Check that self-healing is working
	if system.selfHealing != nil {
		components := system.selfHealing.GetAllComponentStatus()
		assert.NotEmpty(t, components)

		// Should have at least the eBPF collector
		assert.Contains(t, components, "ebpf_collector")
	}
}

// Benchmark tests
func BenchmarkUnifiedSystem_Start(b *testing.B) {
	for i := 0; i < b.N; i++ {
		system, err := NewUnifiedSystem(nil)
		if err != nil {
			b.Fatal(err)
		}

		err = system.Start()
		if err != nil {
			b.Fatal(err)
		}

		system.Stop()
	}
}

func BenchmarkUnifiedSystem_GetMetrics(b *testing.B) {
	system, err := NewUnifiedSystem(nil)
	if err != nil {
		b.Fatal(err)
	}

	err = system.Start()
	if err != nil {
		b.Fatal(err)
	}
	defer system.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = system.GetMetrics()
	}
}

func BenchmarkSystemEvent_Processing(b *testing.B) {
	config := &SystemConfig{
		EnableNetworkMonitoring: false,
		EnableSystemd:           false,
		EnableJournald:          false,
		EventBufferSize:         10000,
		MaxEventsPerSecond:      1000000, // High limit for benchmarking
		BatchSize:               1000,
		EnableCircuitBreaker:    false,
		EnableSelfHealing:       false,
		EnableLoadShedding:      false,
	}

	system, err := NewUnifiedSystem(config)
	if err != nil {
		b.Fatal(err)
	}

	err = system.Start()
	if err != nil {
		b.Fatal(err)
	}
	defer system.Stop()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			event := SystemEvent{
				Source:    "benchmark",
				Type:      "test_event",
				Timestamp: time.Now(),
				Data:      "benchmark data",
				Priority:  1,
			}
			system.mapEventPriority(event)
		}
	})
}
