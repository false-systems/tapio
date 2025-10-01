package base

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

func TestBaseObserver_InitializeOutputs(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		name           string
		config         BaseObserverConfig
		expectOTEL     bool
		expectStdout   bool
		expectNoOutput bool
	}{
		{
			name: "OTEL output enabled",
			config: BaseObserverConfig{
				Name:   "test-observer",
				Logger: logger,
				OutputTargets: OutputTargets{
					OTEL: true,
				},
			},
			expectOTEL: true,
		},
		{
			name: "Stdout output enabled",
			config: BaseObserverConfig{
				Name:   "test-observer",
				Logger: logger,
				OutputTargets: OutputTargets{
					Stdout: true,
				},
				StdoutConfig: &StdoutEmitterConfig{
					Pretty: true,
				},
			},
			expectStdout: true,
		},
		{
			name: "both outputs enabled",
			config: BaseObserverConfig{
				Name:   "test-observer",
				Logger: logger,
				OutputTargets: OutputTargets{
					OTEL:   true,
					Stdout: true,
				},
			},
			expectOTEL:   true,
			expectStdout: true,
		},
		{
			name: "no outputs enabled",
			config: BaseObserverConfig{
				Name:   "test-observer",
				Logger: logger,
				OutputTargets: OutputTargets{
					OTEL:   false,
					Stdout: false,
				},
			},
			expectNoOutput: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bc := NewBaseObserverWithConfig(tt.config)
			require.NotNil(t, bc)

			if tt.expectOTEL {
				assert.NotNil(t, bc.otelEmitter, "OTEL emitter should be initialized")
			} else {
				assert.Nil(t, bc.otelEmitter, "OTEL emitter should not be initialized")
			}

			if tt.expectStdout {
				assert.NotNil(t, bc.stdoutEmitter, "Stdout emitter should be initialized")
			} else {
				assert.Nil(t, bc.stdoutEmitter, "Stdout emitter should not be initialized")
			}
		})
	}
}

func TestBaseObserver_EmitEvent(t *testing.T) {
	logger := zap.NewNop()
	buf := &bytes.Buffer{}

	t.Run("emit to channel only (backward compat)", func(t *testing.T) {
		config := BaseObserverConfig{
			Name:   "test-observer",
			Logger: logger,
			OutputTargets: OutputTargets{
				Channel: true,
			},
			HealthCheckTimeout: 5 * time.Second,
		}
		bc := NewBaseObserverWithConfig(config)

		eventCh := make(chan *domain.CollectorEvent, 1)
		event := &domain.CollectorEvent{
			EventID: "test-123",
			Source:  "test-observer",
		}

		ctx := context.Background()
		bc.EmitEvent(ctx, event, eventCh)

		// Should receive on channel
		select {
		case received := <-eventCh:
			assert.Equal(t, "test-123", received.EventID)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("expected event on channel")
		}
	})

	t.Run("emit to stdout only", func(t *testing.T) {
		buf.Reset()
		config := BaseObserverConfig{
			Name:   "test-observer",
			Logger: logger,
			OutputTargets: OutputTargets{
				Stdout: true,
			},
			StdoutConfig: &StdoutEmitterConfig{
				Pretty: false,
				Writer: buf,
			},
			HealthCheckTimeout: 5 * time.Second,
		}
		bc := NewBaseObserverWithConfig(config)

		event := &domain.CollectorEvent{
			EventID: "stdout-123",
			Source:  "test-observer",
		}

		ctx := context.Background()
		bc.EmitEvent(ctx, event, nil)

		// Should have written to buffer
		output := buf.String()
		assert.Contains(t, output, "stdout-123")
	})

	t.Run("emit to both channel and stdout", func(t *testing.T) {
		buf.Reset()
		config := BaseObserverConfig{
			Name:   "test-observer",
			Logger: logger,
			OutputTargets: OutputTargets{
				Channel: true,
				Stdout:  true,
			},
			StdoutConfig: &StdoutEmitterConfig{
				Pretty: false,
				Writer: buf,
			},
			HealthCheckTimeout: 5 * time.Second,
		}
		bc := NewBaseObserverWithConfig(config)

		eventCh := make(chan *domain.CollectorEvent, 1)
		event := &domain.CollectorEvent{
			EventID: "multi-123",
			Source:  "test-observer",
		}

		ctx := context.Background()
		bc.EmitEvent(ctx, event, eventCh)

		// Should receive on channel
		select {
		case received := <-eventCh:
			assert.Equal(t, "multi-123", received.EventID)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("expected event on channel")
		}

		// Should also have written to stdout
		output := buf.String()
		assert.Contains(t, output, "multi-123")
	})

	t.Run("nil event handling", func(t *testing.T) {
		config := BaseObserverConfig{
			Name:               "test-observer",
			Logger:             logger,
			HealthCheckTimeout: 5 * time.Second,
		}
		bc := NewBaseObserverWithConfig(config)

		ctx := context.Background()
		// Should not panic
		bc.EmitEvent(ctx, nil, nil)
	})
}

func TestBaseObserver_EmitDomainMetric(t *testing.T) {
	logger := zap.NewNop()

	t.Run("OTEL enabled", func(t *testing.T) {
		config := BaseObserverConfig{
			Name:   "test-observer",
			Logger: logger,
			OutputTargets: OutputTargets{
				OTEL: true,
			},
			HealthCheckTimeout: 5 * time.Second,
		}
		bc := NewBaseObserverWithConfig(config)
		require.NotNil(t, bc.otelEmitter)

		ctx := context.Background()
		metric := DomainMetric{
			Name:  "deployment_changes_total",
			Value: 1,
			Attributes: []attribute.KeyValue{
				attribute.String("namespace", "production"),
			},
		}

		err := bc.EmitDomainMetric(ctx, metric)
		assert.NoError(t, err)
	})

	t.Run("OTEL disabled", func(t *testing.T) {
		config := BaseObserverConfig{
			Name:   "test-observer",
			Logger: logger,
			OutputTargets: OutputTargets{
				OTEL: false,
			},
			HealthCheckTimeout: 5 * time.Second,
		}
		bc := NewBaseObserverWithConfig(config)
		require.Nil(t, bc.otelEmitter)

		ctx := context.Background()
		metric := DomainMetric{
			Name:  "test_metric",
			Value: 1,
		}

		err := bc.EmitDomainMetric(ctx, metric)
		assert.NoError(t, err, "should silently skip when OTEL disabled")
	})
}

func TestBaseObserver_EmitDomainGauge(t *testing.T) {
	logger := zap.NewNop()

	t.Run("OTEL enabled", func(t *testing.T) {
		config := BaseObserverConfig{
			Name:   "test-observer",
			Logger: logger,
			OutputTargets: OutputTargets{
				OTEL: true,
			},
			HealthCheckTimeout: 5 * time.Second,
		}
		bc := NewBaseObserverWithConfig(config)
		require.NotNil(t, bc.otelEmitter)

		ctx := context.Background()
		gauge := DomainGauge{
			Name:  "deployment_replicas",
			Value: 5,
			Attributes: []attribute.KeyValue{
				attribute.String("namespace", "production"),
			},
		}

		err := bc.EmitDomainGauge(ctx, gauge)
		assert.NoError(t, err)
	})

	t.Run("OTEL disabled", func(t *testing.T) {
		config := BaseObserverConfig{
			Name:   "test-observer",
			Logger: logger,
			OutputTargets: OutputTargets{
				OTEL: false,
			},
			HealthCheckTimeout: 5 * time.Second,
		}
		bc := NewBaseObserverWithConfig(config)
		require.Nil(t, bc.otelEmitter)

		ctx := context.Background()
		gauge := DomainGauge{
			Name:  "test_gauge",
			Value: 3,
		}

		err := bc.EmitDomainGauge(ctx, gauge)
		assert.NoError(t, err, "should silently skip when OTEL disabled")
	})
}

func TestBaseObserver_CloseOutputs(t *testing.T) {
	logger := zap.NewNop()

	t.Run("close all outputs", func(t *testing.T) {
		config := BaseObserverConfig{
			Name:   "test-observer",
			Logger: logger,
			OutputTargets: OutputTargets{
				OTEL:   true,
				Stdout: true,
			},
			HealthCheckTimeout: 5 * time.Second,
		}
		bc := NewBaseObserverWithConfig(config)

		err := bc.CloseOutputs()
		assert.NoError(t, err)

		// Should be idempotent
		err = bc.CloseOutputs()
		assert.NoError(t, err)
	})

	t.Run("close with no outputs", func(t *testing.T) {
		config := BaseObserverConfig{
			Name:               "test-observer",
			Logger:             logger,
			HealthCheckTimeout: 5 * time.Second,
		}
		bc := NewBaseObserverWithConfig(config)

		err := bc.CloseOutputs()
		assert.NoError(t, err)
	})
}

func TestBaseObserver_MultiOutputIntegration(t *testing.T) {
	logger := zap.NewNop()
	buf := &bytes.Buffer{}

	// Full integration: OTEL + Stdout + Channel
	config := BaseObserverConfig{
		Name:   "integration-test",
		Logger: logger,
		OutputTargets: OutputTargets{
			OTEL:    true,
			Stdout:  true,
			Channel: true,
		},
		StdoutConfig: &StdoutEmitterConfig{
			Pretty: false,
			Writer: buf,
		},
		HealthCheckTimeout: 5 * time.Second,
	}
	bc := NewBaseObserverWithConfig(config)

	// Verify all emitters initialized
	assert.NotNil(t, bc.otelEmitter, "OTEL emitter should be initialized")
	assert.NotNil(t, bc.stdoutEmitter, "Stdout emitter should be initialized")

	ctx := context.Background()
	eventCh := make(chan *domain.CollectorEvent, 1)

	// Emit event
	event := &domain.CollectorEvent{
		EventID: "integration-123",
		Source:  "integration-test",
		Type:    domain.EventTypeK8sDeployment,
	}
	bc.EmitEvent(ctx, event, eventCh)

	// Verify channel received event
	select {
	case received := <-eventCh:
		assert.Equal(t, "integration-123", received.EventID)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected event on channel")
	}

	// Verify stdout received event
	output := buf.String()
	assert.Contains(t, output, "integration-123")

	// Emit domain metric (OTEL only)
	metric := DomainMetric{
		Name:  "integration_test_total",
		Value: 1,
		Attributes: []attribute.KeyValue{
			attribute.String("test", "integration"),
		},
	}
	err := bc.EmitDomainMetric(ctx, metric)
	assert.NoError(t, err)

	// Emit domain gauge (OTEL only)
	gauge := DomainGauge{
		Name:  "integration_test_gauge",
		Value: 42,
		Attributes: []attribute.KeyValue{
			attribute.String("test", "integration"),
		},
	}
	err = bc.EmitDomainGauge(ctx, gauge)
	assert.NoError(t, err)

	// Clean shutdown
	err = bc.CloseOutputs()
	assert.NoError(t, err)
}
