package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cleanupEmitters registers cleanup for all emitters.
func cleanupEmitters(t *testing.T, emitters []Emitter) {
	t.Helper()
	for _, e := range emitters {
		emitter := e // capture for closure
		t.Cleanup(func() {
			if err := emitter.Close(); err != nil {
				t.Logf("failed to close emitter %s: %v", emitter.Name(), err)
			}
		})
	}
}

func TestTierConfig_BuildEmitters_SimpleTier(t *testing.T) {
	cfg := TierConfig{
		Tier:     TierSimple,
		OTLPURL:  "localhost:4317",
		Insecure: true,
		NATSURL:  "nats://localhost:4222", // Should be ignored
	}

	emitters, err := cfg.BuildEmitters()
	require.NoError(t, err)
	cleanupEmitters(t, emitters)

	// Only OTLP emitter
	assert.Len(t, emitters, 1)
	assert.Equal(t, "otlp", emitters[0].Name())
	assert.True(t, emitters[0].IsCritical())
}

func TestTierConfig_BuildEmitters_FreeTier(t *testing.T) {
	ns := startTestNATS(t)
	defer ns.Shutdown()

	cfg := TierConfig{
		Tier:     TierFree,
		OTLPURL:  "localhost:4317",
		Insecure: true,
		NATSURL:  ns.ClientURL(),
	}

	emitters, err := cfg.BuildEmitters()
	require.NoError(t, err)
	cleanupEmitters(t, emitters)

	// OTLP + NATS emitters
	assert.Len(t, emitters, 2)
	names := []string{emitters[0].Name(), emitters[1].Name()}
	assert.Contains(t, names, "otlp")
	assert.Contains(t, names, "nats")
}

func TestTierConfig_BuildEmitters_NATSDown(t *testing.T) {
	cfg := TierConfig{
		Tier:     TierFree,
		OTLPURL:  "localhost:4317",
		Insecure: true,
		NATSURL:  "nats://nonexistent:4222", // Bad URL
	}

	emitters, err := cfg.BuildEmitters()

	// Should NOT fail - NATS is non-critical
	require.NoError(t, err)
	cleanupEmitters(t, emitters)

	// Only OTLP emitter (NATS failed gracefully)
	assert.Len(t, emitters, 1)
	assert.Equal(t, "otlp", emitters[0].Name())
}

func TestTierConfig_BuildEmitters_NoOTLP(t *testing.T) {
	cfg := TierConfig{
		Tier: TierSimple,
		// No OTLPURL
	}

	emitters, err := cfg.BuildEmitters()

	// Should fail - OTLP is required
	assert.Error(t, err)
	assert.Nil(t, emitters)
	assert.Contains(t, err.Error(), "OTLPURL required")
}

func TestTierConfig_DefaultTier(t *testing.T) {
	cfg := TierConfig{
		// Tier not set - should default to Simple
		OTLPURL:  "localhost:4317",
		Insecure: true,
	}

	emitters, err := cfg.BuildEmitters()
	require.NoError(t, err)
	cleanupEmitters(t, emitters)

	// Default behavior = simple tier = OTLP only
	assert.Len(t, emitters, 1)
	assert.Equal(t, "otlp", emitters[0].Name())
}
