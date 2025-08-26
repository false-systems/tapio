//go:build linux

package oom

import (
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/yairfalse/tapio/pkg/collectors"
)

func TestNewCollector(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		name          string
		collectorName string
		config        *OOMConfig
		logger        *zap.Logger
		wantErr       bool
		errContains   string
	}{
		{
			name:          "valid configuration",
			collectorName: "test-oom",
			config:        DefaultOOMConfig(),
			logger:        logger,
			wantErr:       false,
		},
		{
			name:          "nil config uses default",
			collectorName: "test-oom",
			config:        nil,
			logger:        logger,
			wantErr:       false,
		},
		{
			name:          "empty name",
			collectorName: "",
			config:        DefaultOOMConfig(),
			logger:        logger,
			wantErr:       true,
			errContains:   "name cannot be empty",
		},
		{
			name:          "nil logger",
			collectorName: "test-oom",
			config:        DefaultOOMConfig(),
			logger:        nil,
			wantErr:       true,
			errContains:   "logger cannot be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollector(tt.collectorName, tt.config, tt.logger)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, collector)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, collector)
				assert.Equal(t, tt.collectorName, collector.Name())
				assert.False(t, collector.IsHealthy()) // Not started yet
			}
		})
	}
}

func TestCollectorInterface(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector, err := NewCollector("test-oom", DefaultOOMConfig(), logger)
	require.NoError(t, err)

	// Test interface implementation
	var _ collectors.Collector = collector

	// Test methods
	assert.Equal(t, "test-oom", collector.Name())
	assert.NotNil(t, collector.Events())
	assert.False(t, collector.IsHealthy())
}

func TestCollectorStartStop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultOOMConfig()
	collector, err := NewCollector("test-oom", config, logger)
	require.NoError(t, err)

	// Test initial state
	assert.False(t, collector.IsHealthy())

	// Note: We can't actually test Start() without root privileges
	// In a real environment, this would require CAP_BPF capability
	// These tests verify the structure and error handling

	// Test Stop on non-started collector (should not panic)
	err = collector.Stop()
	assert.NoError(t, err)
}

func TestOOMEventValidation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector, err := NewCollector("test-oom", DefaultOOMConfig(), logger)
	require.NoError(t, err)

	tests := []struct {
		name    string
		event   *OOMEvent
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid event",
			event: &OOMEvent{
				Timestamp: 123456789,
				PID:       1234,
				EventType: uint32(OOMKillVictim),
			},
			wantErr: false,
		},
		{
			name: "invalid timestamp",
			event: &OOMEvent{
				Timestamp: 0,
				PID:       1234,
				EventType: uint32(OOMKillVictim),
			},
			wantErr: true,
			errMsg:  "invalid timestamp",
		},
		{
			name: "invalid PID",
			event: &OOMEvent{
				Timestamp: 123456789,
				PID:       0,
				EventType: uint32(OOMKillVictim),
			},
			wantErr: true,
			errMsg:  "invalid PID",
		},
		{
			name: "invalid event type",
			event: &OOMEvent{
				Timestamp: 123456789,
				PID:       1234,
				EventType: 0,
			},
			wantErr: true,
			errMsg:  "invalid event type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := collector.validateRawEvent(tt.event)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestMemoryPressureTracking(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultOOMConfig()
	config.EnablePrediction = true

	collector, err := NewCollector("test-oom", config, logger)
	require.NoError(t, err)

	// Test first update (creates new tracker)
	event1 := &ProcessedOOMEvent{
		Timestamp: time.Now(),
		KubernetesContext: KubernetesContext{
			ContainerID: "container-123",
		},
		MemoryStats: MemoryStatistics{
			UsageBytes:   1024 * 1024 * 500,  // 500MB
			LimitBytes:   1024 * 1024 * 1024, // 1GB
			UsagePercent: 50.0,
		},
	}

	collector.updateMemoryTracking(event1)

	collector.memoryTracker.mu.RLock()
	tracker, exists := collector.memoryTracker.trackers["container-123"]
	collector.memoryTracker.mu.RUnlock()

	assert.True(t, exists)
	assert.Equal(t, uint64(1024*1024*500), tracker.LastUsage)
	assert.Equal(t, 1, tracker.SampleCount)

	// Test second update (updates existing tracker)
	time.Sleep(100 * time.Millisecond)
	event2 := &ProcessedOOMEvent{
		Timestamp: time.Now(),
		EventType: MemoryPressureHigh,
		KubernetesContext: KubernetesContext{
			ContainerID: "container-123",
		},
		MemoryStats: MemoryStatistics{
			UsageBytes:   1024 * 1024 * 600,  // 600MB
			LimitBytes:   1024 * 1024 * 1024, // 1GB
			UsagePercent: 60.0,
		},
	}

	collector.updateMemoryTracking(event2)

	collector.memoryTracker.mu.RLock()
	tracker, exists = collector.memoryTracker.trackers["container-123"]
	collector.memoryTracker.mu.RUnlock()

	assert.True(t, exists)
	assert.Equal(t, uint64(1024*1024*600), tracker.LastUsage)
	assert.Equal(t, 2, tracker.SampleCount)
	assert.Greater(t, tracker.AllocationRate, 0.0) // Should have calculated rate
}

func TestMemoryPredictionGeneration(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultOOMConfig()
	config.EnablePrediction = true

	collector, err := NewCollector("test-oom", config, logger)
	require.NoError(t, err)

	tracker := &ContainerMemoryTracker{
		ContainerID:       "test-container",
		LastUsage:         1024 * 1024 * 800, // 800MB
		LastTimestamp:     time.Now(),
		AllocationRate:    10.0, // 10MB/s
		MaxObservedUsage:  1024 * 1024 * 800,
		SampleCount:       15,
		PredictionHistory: make([]MemoryPrediction, 0),
	}

	event := &ProcessedOOMEvent{
		Timestamp: time.Now(),
		EventType: MemoryPressureHigh,
		MemoryStats: MemoryStatistics{
			UsageBytes:   1024 * 1024 * 800,  // 800MB
			LimitBytes:   1024 * 1024 * 1024, // 1GB
			UsagePercent: 80.0,
		},
	}

	prediction := collector.generateMemoryPrediction(tracker, event)
	assert.NotNil(t, prediction)

	// Should predict OOM in about 20 seconds (200MB remaining / 10MB/s)
	expectedTimeToOOM := 20.0
	actualTimeToOOM := prediction.PredictedOOMTime.Sub(prediction.Timestamp).Seconds()
	assert.InDelta(t, expectedTimeToOOM, actualTimeToOOM, 3.0)

	// Confidence should be high with 15 samples and high allocation rate
	assert.Greater(t, prediction.Confidence, 0.7)
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  *OOMConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid default config",
			config:  DefaultOOMConfig(),
			wantErr: false,
		},
		{
			name: "prediction threshold too high",
			config: &OOMConfig{
				EnablePrediction:         true,
				PredictionThresholdPct:   150, // Invalid: > 100
				HighPressureThresholdPct: 80,
				RingBufferSize:           256 * 1024,
				EventBatchSize:           100,
				EnableK8sCorrelation:     true,
			},
			wantErr: true,
			errMsg:  "must be <= 100",
		},
		{
			name: "high pressure threshold too high",
			config: &OOMConfig{
				EnablePrediction:         true,
				PredictionThresholdPct:   90,
				HighPressureThresholdPct: 150, // Invalid: > 100
				RingBufferSize:           256 * 1024,
				EventBatchSize:           100,
				EnableK8sCorrelation:     true,
			},
			wantErr: true,
			errMsg:  "must be <= 100",
		},
		{
			name: "ring buffer too small",
			config: &OOMConfig{
				EnablePrediction:         true,
				PredictionThresholdPct:   90,
				HighPressureThresholdPct: 80,
				RingBufferSize:           1024, // Invalid: too small
				EventBatchSize:           100,
				EnableK8sCorrelation:     true,
			},
			wantErr: true,
			errMsg:  "must be >= 4096",
		},
		{
			name: "zero batch size",
			config: &OOMConfig{
				EnablePrediction:         true,
				PredictionThresholdPct:   90,
				HighPressureThresholdPct: 80,
				RingBufferSize:           256 * 1024,
				EventBatchSize:           0, // Invalid: zero
				EnableK8sCorrelation:     true,
			},
			wantErr: true,
			errMsg:  "must be > 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFactoryFunctions(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Test CreateCollector
	collector, err := CreateCollector(nil, logger)
	require.NoError(t, err)
	require.NotNil(t, collector)
	assert.Equal(t, "oom-collector", collector.Name())

	// Test with custom config
	config := NewConfig()
	config.EnablePrediction = false
	collector2, err := CreateCollector(config, logger)
	require.NoError(t, err)
	require.NotNil(t, collector2)

	// Test GetDefaultConfig
	defaultConfig := GetDefaultConfig()
	assert.NotNil(t, defaultConfig)
	assert.NoError(t, defaultConfig.Validate())
}

func TestStructSizes(t *testing.T) {
	// Verify struct sizes for kernel compatibility
	oomEventSize := GetOOMEventSize()
	assert.Greater(t, oomEventSize, uint32(0))
	assert.Equal(t, uint32(oomEventSize), uint32(unsafe.Sizeof(OOMEvent{})))
}

func TestConfigFromMap(t *testing.T) {
	tests := []struct {
		name    string
		input   map[string]string
		wantErr bool
		check   func(*testing.T, *Config)
	}{
		{
			name:  "empty map",
			input: map[string]string{},
			check: func(t *testing.T, c *Config) {
				// Should have defaults
				assert.True(t, c.EnablePrediction)
			},
		},
		{
			name: "valid enable_prediction",
			input: map[string]string{
				"enable_prediction": "false",
			},
			check: func(t *testing.T, c *Config) {
				assert.False(t, c.EnablePrediction)
			},
		},
		{
			name: "unknown key",
			input: map[string]string{
				"unknown_key": "value",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := ConfigFromMap(tt.input)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, config)

			if tt.check != nil {
				tt.check(t, config)
			}
		})
	}
}

// Benchmark memory prediction generation
func BenchmarkMemoryPredictionGeneration(b *testing.B) {
	logger := zap.NewNop()
	collector, err := NewCollector("bench-oom", DefaultOOMConfig(), logger)
	require.NoError(b, err)

	tracker := &ContainerMemoryTracker{
		ContainerID:       "bench-container",
		AllocationRate:    15.5,
		SampleCount:       20,
		LastUsage:         1024 * 1024 * 800,
		MaxObservedUsage:  1024 * 1024 * 800,
		PredictionHistory: make([]MemoryPrediction, 0, 100),
	}

	event := &ProcessedOOMEvent{
		EventType: MemoryPressureHigh,
		Timestamp: time.Now(),
		MemoryStats: MemoryStatistics{
			UsageBytes:   1024 * 1024 * 800,
			LimitBytes:   1024 * 1024 * 1024,
			UsagePercent: 80.0,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = collector.generateMemoryPrediction(tracker, event)
	}
}
