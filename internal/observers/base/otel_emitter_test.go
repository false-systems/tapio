package base

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

func TestNewOTELEmitter(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		name      string
		logger    *zap.Logger
		meterName string
		wantErr   bool
	}{
		{
			name:      "valid creation",
			logger:    logger,
			meterName: "test-observer",
			wantErr:   false,
		},
		{
			name:      "nil logger",
			logger:    nil,
			meterName: "test-observer",
			wantErr:   true,
		},
		{
			name:      "empty meter name",
			logger:    logger,
			meterName: "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emitter, err := NewOTELEmitter(tt.logger, tt.meterName)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, emitter)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, emitter)
				assert.NotNil(t, emitter.meter)
				assert.NotNil(t, emitter.cache)
			}
		})
	}
}

func TestOTELEmitter_EmitDomainMetric(t *testing.T) {
	logger := zap.NewNop()
	emitter, err := NewOTELEmitter(logger, "test-observer")
	require.NoError(t, err)

	ctx := context.Background()

	tests := []struct {
		name    string
		metric  DomainMetric
		wantErr bool
	}{
		{
			name: "valid counter metric",
			metric: DomainMetric{
				Name:  "deployment_changes_total",
				Value: 1,
				Attributes: []attribute.KeyValue{
					attribute.String("namespace", "production"),
					attribute.String("deployment", "web-app"),
				},
			},
			wantErr: false,
		},
		{
			name: "empty metric name",
			metric: DomainMetric{
				Name:  "",
				Value: 1,
			},
			wantErr: true,
		},
		{
			name: "metric with zero value",
			metric: DomainMetric{
				Name:  "test_metric",
				Value: 0,
			},
			wantErr: false,
		},
		{
			name: "metric without attributes",
			metric: DomainMetric{
				Name:       "simple_counter",
				Value:      5,
				Attributes: []attribute.KeyValue{},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := emitter.EmitDomainMetric(ctx, tt.metric)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestOTELEmitter_EmitDomainGauge(t *testing.T) {
	logger := zap.NewNop()
	emitter, err := NewOTELEmitter(logger, "test-observer")
	require.NoError(t, err)

	ctx := context.Background()

	tests := []struct {
		name    string
		gauge   DomainGauge
		wantErr bool
	}{
		{
			name: "valid gauge metric",
			gauge: DomainGauge{
				Name:  "deployment_replicas",
				Value: 5,
				Attributes: []attribute.KeyValue{
					attribute.String("namespace", "production"),
					attribute.String("deployment", "web-app"),
				},
			},
			wantErr: false,
		},
		{
			name: "empty gauge name",
			gauge: DomainGauge{
				Name:  "",
				Value: 3,
			},
			wantErr: true,
		},
		{
			name: "gauge with negative value",
			gauge: DomainGauge{
				Name:  "test_gauge",
				Value: -1,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := emitter.EmitDomainGauge(ctx, tt.gauge)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestOTELEmitter_MetricCaching(t *testing.T) {
	logger := zap.NewNop()
	emitter, err := NewOTELEmitter(logger, "test-observer")
	require.NoError(t, err)

	ctx := context.Background()

	// Initial cache should be empty
	counters, gauges := emitter.GetCacheSize()
	assert.Equal(t, 0, counters)
	assert.Equal(t, 0, gauges)

	// Emit first counter
	err = emitter.EmitDomainMetric(ctx, DomainMetric{
		Name:  "test_counter",
		Value: 1,
	})
	require.NoError(t, err)

	// Cache should have 1 counter
	counters, gauges = emitter.GetCacheSize()
	assert.Equal(t, 1, counters)
	assert.Equal(t, 0, gauges)

	// Emit same counter again (should reuse cached)
	err = emitter.EmitDomainMetric(ctx, DomainMetric{
		Name:  "test_counter",
		Value: 2,
	})
	require.NoError(t, err)

	// Cache should still have 1 counter
	counters, gauges = emitter.GetCacheSize()
	assert.Equal(t, 1, counters)
	assert.Equal(t, 0, gauges)

	// Emit different counter
	err = emitter.EmitDomainMetric(ctx, DomainMetric{
		Name:  "another_counter",
		Value: 1,
	})
	require.NoError(t, err)

	// Cache should have 2 counters
	counters, gauges = emitter.GetCacheSize()
	assert.Equal(t, 2, counters)
	assert.Equal(t, 0, gauges)

	// Emit gauge
	err = emitter.EmitDomainGauge(ctx, DomainGauge{
		Name:  "test_gauge",
		Value: 5,
	})
	require.NoError(t, err)

	// Cache should have 2 counters and 1 gauge
	counters, gauges = emitter.GetCacheSize()
	assert.Equal(t, 2, counters)
	assert.Equal(t, 1, gauges)
}

func TestOTELEmitter_ConcurrentAccess(t *testing.T) {
	logger := zap.NewNop()
	emitter, err := NewOTELEmitter(logger, "test-observer")
	require.NoError(t, err)

	ctx := context.Background()

	// Emit same metric concurrently
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			err := emitter.EmitDomainMetric(ctx, DomainMetric{
				Name:  "concurrent_counter",
				Value: 1,
				Attributes: []attribute.KeyValue{
					attribute.Int("worker_id", id),
				},
			})
			assert.NoError(t, err)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should have exactly 1 counter in cache (shared across goroutines)
	counters, _ := emitter.GetCacheSize()
	assert.Equal(t, 1, counters)
}

func TestOTELEmitter_Close(t *testing.T) {
	logger := zap.NewNop()
	emitter, err := NewOTELEmitter(logger, "test-observer")
	require.NoError(t, err)

	// Close should succeed
	err = emitter.Close()
	assert.NoError(t, err)

	// Should be idempotent
	err = emitter.Close()
	assert.NoError(t, err)
}

func TestOTELEmitter_EmitEvent(t *testing.T) {
	logger := zap.NewNop()
	emitter, err := NewOTELEmitter(logger, "test-observer")
	require.NoError(t, err)

	ctx := context.Background()

	// EmitEvent should be a no-op for OTEL emitter
	err = emitter.EmitEvent(ctx, nil)
	assert.NoError(t, err, "EmitEvent should not fail even with nil event")
}
