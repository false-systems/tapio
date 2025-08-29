package resourcestarvation

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDefaultConfig(t *testing.T) {
	config := NewDefaultConfig()
	require.NotNil(t, config)

	assert.Equal(t, "resource-starvation", config.Name)
	assert.True(t, config.Enabled)
	assert.Equal(t, 100, config.StarvationThresholdMS)
	assert.Equal(t, 500, config.SevereThresholdMS)
	assert.Equal(t, 2000, config.CriticalThresholdMS)
	assert.Equal(t, 0.1, config.SampleRate)
	assert.Equal(t, 1024, config.RingBufferSizeKB)
	assert.True(t, config.EnableK8sEnrichment)
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid default config",
			config:      NewDefaultConfig(),
			expectError: false,
		},
		{
			name: "empty name",
			config: &Config{
				Name:                  "",
				StarvationThresholdMS: 100,
				SevereThresholdMS:     500,
				CriticalThresholdMS:   2000,
			},
			expectError: true,
			errorMsg:    "collector name cannot be empty",
		},
		{
			name: "negative starvation threshold",
			config: &Config{
				Name:                  "test",
				StarvationThresholdMS: -100,
			},
			expectError: true,
			errorMsg:    "starvation_threshold_ms must be positive",
		},
		{
			name: "invalid threshold order",
			config: &Config{
				Name:                  "test",
				StarvationThresholdMS: 500,
				SevereThresholdMS:     100,
				CriticalThresholdMS:   2000,
			},
			expectError: true,
			errorMsg:    "severe_threshold_ms (100) must be greater than starvation_threshold_ms (500)",
		},
		{
			name: "invalid sample rate too high",
			config: &Config{
				Name:                  "test",
				StarvationThresholdMS: 100,
				SevereThresholdMS:     500,
				CriticalThresholdMS:   2000,
				SampleRate:            1.5,
			},
			expectError: true,
			errorMsg:    "sample_rate must be between 0.0 and 1.0",
		},
		{
			name: "invalid sample rate negative",
			config: &Config{
				Name:                  "test",
				StarvationThresholdMS: 100,
				SevereThresholdMS:     500,
				CriticalThresholdMS:   2000,
				SampleRate:            -0.1,
			},
			expectError: true,
			errorMsg:    "sample_rate must be between 0.0 and 1.0",
		},
		{
			name: "invalid ring buffer size",
			config: &Config{
				Name:                  "test",
				StarvationThresholdMS: 100,
				SevereThresholdMS:     500,
				CriticalThresholdMS:   2000,
				SampleRate:            0.5,
				RingBufferSizeKB:      0,
			},
			expectError: true,
			errorMsg:    "ring_buffer_size_kb must be between 1 and 16384",
		},
		{
			name: "ring buffer too large",
			config: &Config{
				Name:                  "test",
				StarvationThresholdMS: 100,
				SevereThresholdMS:     500,
				CriticalThresholdMS:   2000,
				SampleRate:            0.5,
				RingBufferSizeKB:      20000,
			},
			expectError: true,
			errorMsg:    "ring_buffer_size_kb must be between 1 and 16384",
		},
		{
			name: "invalid max events per sec",
			config: &Config{
				Name:                  "test",
				StarvationThresholdMS: 100,
				SevereThresholdMS:     500,
				CriticalThresholdMS:   2000,
				SampleRate:            0.5,
				RingBufferSizeKB:      1024,
				MaxEventsPerSec:       0,
			},
			expectError: true,
			errorMsg:    "max_events_per_sec must be positive",
		},
		{
			name: "invalid pattern confidence",
			config: &Config{
				Name:                   "test",
				StarvationThresholdMS:  100,
				SevereThresholdMS:      500,
				CriticalThresholdMS:    2000,
				SampleRate:             0.5,
				RingBufferSizeKB:       1024,
				MaxEventsPerSec:        1000,
				EnablePatternDetection: true,
				PatternWindowSec:       60,
				MinPatternConfidence:   1.5,
			},
			expectError: true,
			errorMsg:    "min_pattern_confidence must be between 0.0 and 1.0",
		},
		{
			name: "invalid memory limit",
			config: &Config{
				Name:                  "test",
				StarvationThresholdMS: 100,
				SevereThresholdMS:     500,
				CriticalThresholdMS:   2000,
				SampleRate:            0.5,
				RingBufferSizeKB:      1024,
				MaxEventsPerSec:       1000,
				MaxMemoryMB:           0,
			},
			expectError: true,
			errorMsg:    "max_memory_mb must be between 1 and 8192",
		},
		{
			name: "memory limit too high",
			config: &Config{
				Name:                  "test",
				StarvationThresholdMS: 100,
				SevereThresholdMS:     500,
				CriticalThresholdMS:   2000,
				SampleRate:            0.5,
				RingBufferSizeKB:      1024,
				MaxEventsPerSec:       1000,
				MaxMemoryMB:           10000,
			},
			expectError: true,
			errorMsg:    "max_memory_mb must be between 1 and 8192",
		},
		{
			name: "invalid processing timeout",
			config: &Config{
				Name:                  "test",
				StarvationThresholdMS: 100,
				SevereThresholdMS:     500,
				CriticalThresholdMS:   2000,
				SampleRate:            0.5,
				RingBufferSizeKB:      1024,
				MaxEventsPerSec:       1000,
				MaxMemoryMB:           512,
				ProcessingTimeout:     -1 * time.Second,
			},
			expectError: true,
			errorMsg:    "processing_timeout must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestConfigApplyDefaults(t *testing.T) {
	config := &Config{
		Name: "custom",
	}

	config.ApplyDefaults()

	assert.Equal(t, "custom", config.Name)
	assert.Equal(t, 100, config.StarvationThresholdMS)
	assert.Equal(t, 500, config.SevereThresholdMS)
	assert.Equal(t, 2000, config.CriticalThresholdMS)
	assert.Equal(t, 0.1, config.SampleRate)
}

func TestConfigIsSafeMode(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		expected bool
	}{
		{
			name:     "default config is safe",
			config:   NewDefaultConfig(),
			expected: true,
		},
		{
			name: "high sample rate not safe",
			config: &Config{
				SampleRate:            0.8,
				MaxEventsPerSec:       1000,
				MaxMemoryMB:           512,
				CircuitBreakerEnabled: true,
				DebugMode:             false,
			},
			expected: false,
		},
		{
			name: "high event rate not safe",
			config: &Config{
				SampleRate:            0.3,
				MaxEventsPerSec:       3000,
				MaxMemoryMB:           512,
				CircuitBreakerEnabled: true,
				DebugMode:             false,
			},
			expected: false,
		},
		{
			name: "debug mode not safe",
			config: &Config{
				SampleRate:            0.3,
				MaxEventsPerSec:       1000,
				MaxMemoryMB:           512,
				CircuitBreakerEnabled: true,
				DebugMode:             true,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.IsSafeMode()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfigGetters(t *testing.T) {
	config := &Config{
		RingBufferSizeKB:    2048,
		GracefulShutdownSec: 45,
	}

	assert.Equal(t, 2048*1024, config.GetEffectiveRingBufferSize())
	assert.Equal(t, 45*time.Second, config.GetEffectiveShutdownTimeout())
}

func TestConfigString(t *testing.T) {
	config := NewDefaultConfig()
	str := config.String()

	assert.Contains(t, str, "resource-starvation")
	assert.Contains(t, str, "enabled=true")
	assert.Contains(t, str, "starvation_threshold=100ms")
	assert.Contains(t, str, "sample_rate=0.10")
	assert.Contains(t, str, "safe_mode=true")
}

func TestNodeNameFromEnvironment(t *testing.T) {
	originalNodeName := os.Getenv("NODE_NAME")
	defer os.Setenv("NODE_NAME", originalNodeName)

	t.Run("with NODE_NAME set", func(t *testing.T) {
		os.Setenv("NODE_NAME", "test-node-123")
		config := NewDefaultConfig()
		assert.Equal(t, "test-node-123", config.NodeName)
	})

	t.Run("without NODE_NAME", func(t *testing.T) {
		os.Unsetenv("NODE_NAME")
		config := NewDefaultConfig()
		assert.NotEmpty(t, config.NodeName)
		assert.NotEqual(t, "", config.NodeName)
	})
}
