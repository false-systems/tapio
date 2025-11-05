package runtime

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test 1: Valid default config passes validation
func TestConfig_Validate_DefaultConfig(t *testing.T) {
	cfg := DefaultConfig("test-observer")
	err := cfg.Validate()
	require.NoError(t, err)
}

// Test 2: Empty name fails validation
func TestConfig_Validate_EmptyName(t *testing.T) {
	cfg := DefaultConfig("test")
	cfg.Name = ""

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config.Name is required")
}

// Test 3: Sampling rate < 0 fails
func TestConfig_Validate_InvalidSamplingRate_Negative(t *testing.T) {
	cfg := DefaultConfig("test")
	cfg.Sampling.Enabled = true
	cfg.Sampling.DefaultRate = -0.1

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config.Sampling.DefaultRate must be between 0.0 and 1.0")
}

// Test 4: Sampling rate > 1 fails
func TestConfig_Validate_InvalidSamplingRate_TooHigh(t *testing.T) {
	cfg := DefaultConfig("test")
	cfg.Sampling.Enabled = true
	cfg.Sampling.DefaultRate = 1.5

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config.Sampling.DefaultRate must be between 0.0 and 1.0")
}

// Test 5: Sampling rule without EventType fails
func TestConfig_Validate_SamplingRule_MissingEventType(t *testing.T) {
	cfg := DefaultConfig("test")
	cfg.Sampling.Enabled = true
	cfg.Sampling.Rules = []SamplingRule{
		{EventType: "", Rate: 0.5},
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config.Sampling.Rules[0].EventType is required")
}

// Test 6: Sampling rule with invalid rate fails
func TestConfig_Validate_SamplingRule_InvalidRate(t *testing.T) {
	cfg := DefaultConfig("test")
	cfg.Sampling.Enabled = true
	cfg.Sampling.Rules = []SamplingRule{
		{EventType: "network", Rate: 2.0, KeepAll: false},
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config.Sampling.Rules[0].Rate must be between 0.0 and 1.0")
}

// Test 7: Queue size <= 0 fails
func TestConfig_Validate_InvalidQueueSize(t *testing.T) {
	cfg := DefaultConfig("test")
	cfg.Backpressure.QueueSize = 0

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config.Backpressure.QueueSize must be > 0")
}

// Test 8: Invalid drop policy fails
func TestConfig_Validate_InvalidDropPolicy(t *testing.T) {
	cfg := DefaultConfig("test")
	cfg.Backpressure.DropPolicy = DropPolicy("invalid")

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config.Backpressure.DropPolicy must be 'oldest', 'newest', or 'random'")
}

// Test 9: Health check interval <= 0 when enabled fails
func TestConfig_Validate_InvalidHealthCheckInterval(t *testing.T) {
	cfg := DefaultConfig("test")
	cfg.Health.Enabled = true
	cfg.Health.CheckInterval = 0

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config.Health.CheckInterval must be > 0 when health checks enabled")
}

// Test 10: Invalid failure policy fails
func TestConfig_Validate_InvalidFailurePolicy(t *testing.T) {
	cfg := DefaultConfig("test")
	cfg.Failure.Policy = FailurePolicy("invalid")

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config.Failure.Policy must be 'isolate', 'restart', or 'fail_fast'")
}

// Test 11: Restart policy with invalid MaxAttempts fails
func TestConfig_Validate_RestartPolicy_InvalidMaxAttempts(t *testing.T) {
	cfg := DefaultConfig("test")
	cfg.Failure.Policy = FailPolicyRestart
	cfg.Failure.Retry.MaxAttempts = 0

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config.Failure.Retry.MaxAttempts must be > 0 for restart policy")
}

// Test 12: Restart policy with invalid InitialDelay fails
func TestConfig_Validate_RestartPolicy_InvalidInitialDelay(t *testing.T) {
	cfg := DefaultConfig("test")
	cfg.Failure.Policy = FailPolicyRestart
	cfg.Failure.Retry.MaxAttempts = 3
	cfg.Failure.Retry.InitialDelay = 0

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config.Failure.Retry.InitialDelay must be > 0 for restart policy")
}

// Test 13: Restart policy with invalid Multiplier fails
func TestConfig_Validate_RestartPolicy_InvalidMultiplier(t *testing.T) {
	cfg := DefaultConfig("test")
	cfg.Failure.Policy = FailPolicyRestart
	cfg.Failure.Retry.MaxAttempts = 3
	cfg.Failure.Retry.InitialDelay = 1 * time.Second
	cfg.Failure.Retry.Multiplier = 1.0

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config.Failure.Retry.Multiplier must be > 1.0 for exponential backoff")
}

// Test 14: Valid custom config passes
func TestConfig_Validate_ValidCustomConfig(t *testing.T) {
	cfg := Config{
		Name: "custom-observer",
		Sampling: SamplingConfig{
			Enabled:     true,
			DefaultRate: 0.5,
			Rules: []SamplingRule{
				{EventType: "network", Subtype: "dns", KeepAll: true},
				{EventType: "node", Rate: 0.8},
			},
		},
		Backpressure: BackpressureConfig{
			QueueSize:  5000,
			DropPolicy: DropNewest,
		},
		Health: HealthConfig{
			Enabled:       true,
			CheckInterval: 15 * time.Second,
		},
		Failure: FailureConfig{
			Policy: FailPolicyRestart,
			Retry: RetryConfig{
				MaxAttempts:  5,
				InitialDelay: 2 * time.Second,
				MaxDelay:     30 * time.Second,
				Multiplier:   2.5,
			},
		},
		Metrics: MetricsConfig{
			Enabled:       true,
			AllowedLabels: []string{"namespace", "pod"},
		},
	}

	err := cfg.Validate()
	require.NoError(t, err)
}

// Test 15: Sampling disabled skips rate validation
func TestConfig_Validate_SamplingDisabled_SkipsValidation(t *testing.T) {
	cfg := DefaultConfig("test")
	cfg.Sampling.Enabled = false
	cfg.Sampling.DefaultRate = -1.0 // Invalid but should be ignored

	err := cfg.Validate()
	require.NoError(t, err) // Should pass because sampling is disabled
}
