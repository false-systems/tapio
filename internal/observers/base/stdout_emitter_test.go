package base

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

func TestNewStdoutEmitter(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		name    string
		logger  *zap.Logger
		config  StdoutEmitterConfig
		wantErr bool
	}{
		{
			name:   "valid creation with defaults",
			logger: logger,
			config: StdoutEmitterConfig{
				Pretty: true,
			},
			wantErr: false,
		},
		{
			name:   "valid creation with custom writer",
			logger: logger,
			config: StdoutEmitterConfig{
				Pretty: false,
				Writer: &bytes.Buffer{},
			},
			wantErr: false,
		},
		{
			name:    "nil logger",
			logger:  nil,
			config:  StdoutEmitterConfig{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emitter, err := NewStdoutEmitter(tt.logger, tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, emitter)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, emitter)
				assert.NotNil(t, emitter.writer)
			}
		})
	}
}

func TestStdoutEmitter_EmitEvent(t *testing.T) {
	logger := zap.NewNop()

	t.Run("emit event pretty print", func(t *testing.T) {
		buf := &bytes.Buffer{}
		emitter, err := NewStdoutEmitter(logger, StdoutEmitterConfig{
			Pretty: true,
			Writer: buf,
		})
		require.NoError(t, err)

		event := &domain.CollectorEvent{
			EventID: "test-123",
			Source:  "test-observer",
			Type:    domain.EventTypeK8sDeployment,
		}

		ctx := context.Background()
		err = emitter.EmitEvent(ctx, event)
		require.NoError(t, err)

		// Should be pretty-printed JSON
		output := buf.String()
		assert.Contains(t, output, "test-123")
		assert.Contains(t, output, "test-observer")
		// Pretty print has newlines and indentation
		assert.True(t, strings.Contains(output, "\n"))
	})

	t.Run("emit event compact", func(t *testing.T) {
		buf := &bytes.Buffer{}
		emitter, err := NewStdoutEmitter(logger, StdoutEmitterConfig{
			Pretty: false,
			Writer: buf,
		})
		require.NoError(t, err)

		event := &domain.CollectorEvent{
			EventID: "compact-123",
			Source:  "compact-observer",
			Type:    domain.EventTypeK8sDeployment,
		}

		ctx := context.Background()
		err = emitter.EmitEvent(ctx, event)
		require.NoError(t, err)

		// Should be compact JSON (single line)
		output := buf.String()
		assert.Contains(t, output, "compact-123")

		// Parse to verify valid JSON
		var parsed domain.CollectorEvent
		// Trim newline
		jsonStr := strings.TrimSpace(output)
		err = json.Unmarshal([]byte(jsonStr), &parsed)
		require.NoError(t, err)
		assert.Equal(t, "compact-123", parsed.EventID)
	})

	t.Run("nil event", func(t *testing.T) {
		buf := &bytes.Buffer{}
		emitter, err := NewStdoutEmitter(logger, StdoutEmitterConfig{
			Writer: buf,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = emitter.EmitEvent(ctx, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nil")
	})
}

func TestStdoutEmitter_EmitDomainMetric(t *testing.T) {
	logger := zap.NewNop()
	buf := &bytes.Buffer{}
	emitter, err := NewStdoutEmitter(logger, StdoutEmitterConfig{
		Writer: buf,
	})
	require.NoError(t, err)

	ctx := context.Background()

	// Should be no-op
	err = emitter.EmitDomainMetric(ctx, DomainMetric{
		Name:  "test_metric",
		Value: 1,
	})
	assert.NoError(t, err)
	assert.Empty(t, buf.String(), "Should not write metrics to stdout")
}

func TestStdoutEmitter_EmitDomainGauge(t *testing.T) {
	logger := zap.NewNop()
	buf := &bytes.Buffer{}
	emitter, err := NewStdoutEmitter(logger, StdoutEmitterConfig{
		Writer: buf,
	})
	require.NoError(t, err)

	ctx := context.Background()

	// Should be no-op
	err = emitter.EmitDomainGauge(ctx, DomainGauge{
		Name:  "test_gauge",
		Value: 5,
	})
	assert.NoError(t, err)
	assert.Empty(t, buf.String(), "Should not write gauges to stdout")
}

func TestStdoutEmitter_MultipleEvents(t *testing.T) {
	logger := zap.NewNop()
	buf := &bytes.Buffer{}
	emitter, err := NewStdoutEmitter(logger, StdoutEmitterConfig{
		Pretty: false,
		Writer: buf,
	})
	require.NoError(t, err)

	ctx := context.Background()

	// Emit multiple events
	for i := 0; i < 3; i++ {
		event := &domain.CollectorEvent{
			EventID: "event-" + string(rune(i+48)), // 0, 1, 2
			Source:  "multi-test",
		}
		err = emitter.EmitEvent(ctx, event)
		require.NoError(t, err)
	}

	// Should have 3 lines
	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	assert.Len(t, lines, 3)

	// Each line should be valid JSON
	for _, line := range lines {
		var event domain.CollectorEvent
		err := json.Unmarshal([]byte(line), &event)
		require.NoError(t, err)
		assert.Equal(t, "multi-test", event.Source)
	}
}

func TestStdoutEmitter_Close(t *testing.T) {
	logger := zap.NewNop()
	emitter, err := NewStdoutEmitter(logger, StdoutEmitterConfig{})
	require.NoError(t, err)

	// Close should succeed
	err = emitter.Close()
	assert.NoError(t, err)

	// Should be idempotent
	err = emitter.Close()
	assert.NoError(t, err)
}
