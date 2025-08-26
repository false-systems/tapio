package oom

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfigFromMapWithParsing tests the improved ConfigFromMap with actual parsing
func TestConfigFromMapWithParsing(t *testing.T) {
	tests := []struct {
		name      string
		configMap map[string]string
		verify    func(t *testing.T, cfg *Config)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "valid numeric values",
			configMap: map[string]string{
				"enable_prediction":               "true",
				"prediction_threshold_percent":    "85",
				"ring_buffer_size":                "4096",
				"high_pressure_threshold_percent": "90",
				"event_batch_size":                "100",
				"max_events_per_second":           "1000",
			},
			verify: func(t *testing.T, cfg *Config) {
				assert.True(t, cfg.EnablePrediction)
				assert.Equal(t, uint32(85), cfg.PredictionThresholdPct)
				assert.Equal(t, uint32(4096), cfg.RingBufferSize)
				assert.Equal(t, uint32(90), cfg.HighPressureThresholdPct)
				assert.Equal(t, uint32(100), cfg.EventBatchSize)
				assert.Equal(t, uint32(1000), cfg.MaxEventsPerSecond)
			},
		},
		{
			name: "boolean flags",
			configMap: map[string]string{
				"collect_cmdline":          "true",
				"collect_environment":      "false",
				"collect_memory_details":   "true",
				"exclude_system_processes": "false",
			},
			verify: func(t *testing.T, cfg *Config) {
				assert.True(t, cfg.CollectCmdline)
				assert.False(t, cfg.CollectEnvironment)
				assert.True(t, cfg.CollectMemoryDetails)
				assert.False(t, cfg.ExcludeSystemProcesses)
			},
		},
		{
			name: "invalid numeric value",
			configMap: map[string]string{
				"prediction_threshold_percent": "not_a_number",
			},
			wantErr: true,
			errMsg:  "parsing prediction_threshold_percent",
		},
		{
			name: "overflow value",
			configMap: map[string]string{
				"ring_buffer_size": "999999999999999999",
			},
			wantErr: true,
			errMsg:  "parsing ring_buffer_size",
		},
		{
			name: "negative value",
			configMap: map[string]string{
				"event_batch_size": "-100",
			},
			wantErr: true,
			errMsg:  "parsing event_batch_size",
		},
		{
			name: "unknown config key",
			configMap: map[string]string{
				"unknown_key": "value",
			},
			wantErr: true,
			errMsg:  "unknown config key",
		},
		{
			name:      "empty config map",
			configMap: map[string]string{},
			verify: func(t *testing.T, cfg *Config) {
				// Should have default values
				assert.NotNil(t, cfg.OOMConfig)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ConfigFromMap(tt.configMap)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, cfg)
			} else {
				require.NoError(t, err)
				require.NotNil(t, cfg)
				if tt.verify != nil {
					tt.verify(t, cfg)
				}
			}
		})
	}
}

// TestSetConfigValue tests individual configuration value parsing
func TestSetConfigValue(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		check   func(t *testing.T, cfg *Config)
		wantErr bool
	}{
		{
			name:  "enable_prediction true",
			key:   "enable_prediction",
			value: "true",
			check: func(t *testing.T, cfg *Config) {
				assert.True(t, cfg.EnablePrediction)
			},
		},
		{
			name:  "enable_prediction false",
			key:   "enable_prediction",
			value: "false",
			check: func(t *testing.T, cfg *Config) {
				assert.False(t, cfg.EnablePrediction)
			},
		},
		{
			name:  "enable_prediction any other value",
			key:   "enable_prediction",
			value: "yes",
			check: func(t *testing.T, cfg *Config) {
				assert.False(t, cfg.EnablePrediction, "non-'true' values should be false")
			},
		},
		{
			name:  "prediction_threshold_percent valid",
			key:   "prediction_threshold_percent",
			value: "75",
			check: func(t *testing.T, cfg *Config) {
				assert.Equal(t, uint32(75), cfg.PredictionThresholdPct)
			},
		},
		{
			name:    "prediction_threshold_percent invalid",
			key:     "prediction_threshold_percent",
			value:   "abc",
			wantErr: true,
		},
		{
			name:  "ring_buffer_size max uint32",
			key:   "ring_buffer_size",
			value: "4294967295", // max uint32
			check: func(t *testing.T, cfg *Config) {
				assert.Equal(t, uint32(4294967295), cfg.RingBufferSize)
			},
		},
		{
			name:    "ring_buffer_size overflow",
			key:     "ring_buffer_size",
			value:   "4294967296", // max uint32 + 1
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewConfig()
			err := setConfigValue(cfg, tt.key, tt.value)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				if tt.check != nil {
					tt.check(t, cfg)
				}
			}
		})
	}
}

// TestCreateCollectorErrorHandling tests error handling in CreateCollector
func TestCreateCollectorErrorHandling(t *testing.T) {
	t.Run("nil logger returns error immediately", func(t *testing.T) {
		collector, err := CreateCollector(nil, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "logger cannot be nil")
		assert.Nil(t, collector)
	})

	t.Run("nil config uses default", func(t *testing.T) {
		// This test would require a mock logger
		// Since we can't easily create a real logger in tests,
		// we're just verifying the function signature works
		t.Skip("Requires mock logger implementation")
	})
}
