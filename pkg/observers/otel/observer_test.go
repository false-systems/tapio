package otel

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestNewObserver(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name:    "default config",
			config:  nil,
			wantErr: false,
		},
		{
			name: "custom config",
			config: &Config{
				Name:         "test-otel",
				GRPCEndpoint: ":5317",
				BufferSize:   5000,
				SamplingRate: 0.5,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observer, err := NewObserver("test", tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, observer)
			assert.Equal(t, "test", observer.Name())
		})
	}
}

func TestObserverLifecycle(t *testing.T) {
	observer, err := NewObserver("test-otel", nil)
	require.NoError(t, err)
	require.NotNil(t, observer)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start observer
	err = observer.Start(ctx)
	if err != nil {
		// On non-Linux platforms, Start returns an error
		t.Skipf("Skipping lifecycle test on non-Linux platform: %v", err)
	}

	// Check health
	assert.True(t, observer.IsHealthy())

	// Get events channel
	events := observer.Events()
	assert.NotNil(t, events)

	// Stop observer
	err = observer.Stop()
	assert.NoError(t, err)

	// Should no longer be healthy
	assert.False(t, observer.IsHealthy())
}

func TestConfig(t *testing.T) {
	t.Run("default config", func(t *testing.T) {
		config := DefaultConfig()
		assert.NotNil(t, config)
		assert.Equal(t, "otel-observer", config.Name)
		assert.Equal(t, ":4317", config.GRPCEndpoint)
		assert.Equal(t, ":4318", config.HTTPEndpoint)
		assert.Equal(t, 10000, config.BufferSize)
		assert.Equal(t, 1.0, config.SamplingRate)
		assert.True(t, config.AlwaysSampleErrors)
		assert.True(t, config.EnableDependencies)
	})

	t.Run("validate config", func(t *testing.T) {
		config := &Config{
			SamplingRate:       -1.0, // Invalid
			BufferSize:         0,    // Invalid
			MaxTracesPerSecond: 0,    // Invalid
		}

		err := config.Validate()
		assert.NoError(t, err)

		// Should fix invalid values
		assert.Equal(t, 1.0, config.SamplingRate)
		assert.Equal(t, 10000, config.BufferSize)
		assert.Equal(t, 1000, config.MaxTracesPerSecond)
	})
}

func TestSampling(t *testing.T) {
	observer, err := NewObserver("test", &Config{
		SamplingRate:       0.5,
		AlwaysSampleErrors: true,
	})
	require.NoError(t, err)

	t.Run("always sample errors", func(t *testing.T) {
		errorSpan := &domain.OTELSpanData{
			StatusCode: "ERROR",
		}
		// Should always sample error spans
		for i := 0; i < 100; i++ {
			assert.True(t, observer.ShouldSample(errorSpan))
		}
	})

	t.Run("sample rate 1.0", func(t *testing.T) {
		observer.config.SamplingRate = 1.0
		okSpan := &domain.OTELSpanData{
			StatusCode: "OK",
		}
		// Should always sample when rate is 1.0
		for i := 0; i < 100; i++ {
			assert.True(t, observer.ShouldSample(okSpan))
		}
	})

	t.Run("sample rate 0.0", func(t *testing.T) {
		observer.config.SamplingRate = 0.0
		observer.config.AlwaysSampleErrors = false
		okSpan := &domain.OTELSpanData{
			StatusCode: "OK",
		}
		// Should never sample when rate is 0.0
		sampled := 0
		for i := 0; i < 100; i++ {
			if observer.ShouldSample(okSpan) {
				sampled++
			}
		}
		assert.Equal(t, 0, sampled)
	})
}

func TestServiceDependency(t *testing.T) {
	observer, err := NewObserver("test", &Config{
		EnableDependencies: true,
	})
	require.NoError(t, err)

	// Record some dependencies
	observer.RecordServiceDependency("frontend", "backend")
	observer.RecordServiceDependency("frontend", "backend")
	observer.RecordServiceDependency("backend", "database")
	observer.RecordServiceDependency("backend", "cache")

	// Check recorded dependencies using GetServiceDependencies
	deps := observer.GetServiceDependencies()
	assert.Equal(t, int64(2), deps["frontend"]["backend"])
	assert.Equal(t, int64(1), deps["backend"]["database"])
	assert.Equal(t, int64(1), deps["backend"]["cache"])

	// Test edge cases
	observer.RecordServiceDependency("", "backend")          // Empty from
	observer.RecordServiceDependency("frontend", "")         // Empty to
	observer.RecordServiceDependency("frontend", "frontend") // Self-reference

	// These should not be recorded
	deps = observer.GetServiceDependencies()
	assert.Empty(t, deps[""])
	if frontendDeps, exists := deps["frontend"]; exists {
		assert.Empty(t, frontendDeps[""])
		assert.Empty(t, frontendDeps["frontend"])
	}
}

func TestEventEmission(t *testing.T) {
	observer, err := NewObserver("test", &Config{
		BufferSize: 10,
	})
	require.NoError(t, err)

	ctx := context.Background()

	// Start observer (may fail on non-Linux)
	err = observer.Start(ctx)
	if err != nil {
		t.Skipf("Skipping event emission test on non-Linux platform: %v", err)
	}
	defer observer.Stop()

	// Wait for test span to be emitted
	select {
	case event := <-observer.Events():
		assert.NotNil(t, event)
		assert.Equal(t, domain.EventTypeOTELSpan, event.Type)
		assert.NotNil(t, event.EventData.OTELSpan)
		assert.Equal(t, "test-service", event.EventData.OTELSpan.ServiceName)
	case <-time.After(15 * time.Second):
		t.Fatal("Timeout waiting for test span")
	}
}

func BenchmarkSampling(b *testing.B) {
	observer, _ := NewObserver("bench", &Config{
		SamplingRate:       0.5,
		AlwaysSampleErrors: true,
	})

	span := &domain.OTELSpanData{
		StatusCode: "OK",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = observer.ShouldSample(span)
	}
}

func BenchmarkServiceDependency(b *testing.B) {
	observer, _ := NewObserver("bench", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		observer.RecordServiceDependency("service-a", "service-b")
	}
}
