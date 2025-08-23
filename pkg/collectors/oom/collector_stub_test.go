//go:build !linux

package oom

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestStubCollector(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultOOMConfig()

	collector, err := NewCollector("test-oom-stub", config, logger)
	require.NoError(t, err)
	require.NotNil(t, collector)

	assert.Equal(t, "test-oom-stub", collector.Name())
	assert.True(t, collector.IsHealthy()) // Stub is always healthy

	// Test start/stop
	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)

	err = collector.Stop()
	require.NoError(t, err)

	// Test events channel (should be closed)
	events := collector.Events()
	select {
	case _, ok := <-events:
		assert.False(t, ok, "Events channel should be closed for stub collector")
	default:
		t.Log("Events channel closed immediately as expected")
	}
}

func TestFactoryFunctionsStub(t *testing.T) {
	logger := zap.NewNop()

	// Test CreateCollector
	config := NewConfig()
	collector, err := CreateCollector(config, logger)
	require.NoError(t, err)
	require.NotNil(t, collector)

	// Should be a stub collector on non-Linux platforms
	assert.Equal(t, "oom-collector", collector.Name())
	assert.True(t, collector.IsHealthy())
}

func TestConfigValidationStub(t *testing.T) {
	// Config validation should work regardless of platform
	config := DefaultOOMConfig()
	err := config.Validate()
	assert.NoError(t, err)

	// Test invalid config
	invalidConfig := &OOMConfig{
		PredictionThresholdPct: 150, // > 100%
	}
	err = invalidConfig.Validate()
	assert.Error(t, err)
}

func TestOOMEventTypesStub(t *testing.T) {
	// Event type validation should work regardless of platform
	assert.True(t, OOMKillVictim.IsCritical())
	assert.False(t, OOMKillVictim.IsPredictive())
	assert.Equal(t, "oom_kill_victim", OOMKillVictim.String())

	assert.False(t, MemoryPressureHigh.IsCritical())
	assert.True(t, MemoryPressureHigh.IsPredictive())
	assert.Equal(t, "memory_pressure_high", MemoryPressureHigh.String())
}

func TestConfigFromMapStub(t *testing.T) {
	configMap := map[string]string{
		"enable_prediction": "true",
	}

	config, err := ConfigFromMap(configMap)
	require.NoError(t, err)
	require.NotNil(t, config)
	assert.True(t, config.EnablePrediction)
}
