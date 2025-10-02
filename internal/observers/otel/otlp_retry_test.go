package otel

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// MockFailingExporter simulates transient failures
type MockFailingExporter struct {
	attempts      int
	failUntil     int
	exportedSpans [][]*domain.OTELSpanData
}

func (m *MockFailingExporter) ExportSpans(_ context.Context, spans []*domain.OTELSpanData) error {
	m.attempts++
	if m.attempts <= m.failUntil {
		return errors.New("temporary network error")
	}
	m.exportedSpans = append(m.exportedSpans, spans)
	return nil
}

func (m *MockFailingExporter) Shutdown(_ context.Context) error {
	return nil
}

func TestExportWithRetry_SuccessFirstAttempt(t *testing.T) {
	exporter := &MockFailingExporter{failUntil: 0} // Succeeds immediately
	logger := zap.NewNop()

	spans := []*domain.OTELSpanData{
		{
			TraceID:     "1234567890abcdef1234567890abcdef",
			SpanID:      "1234567890abcdef",
			ServiceName: "test",
			Name:        "operation",
		},
	}

	config := DefaultRetryConfig()
	ctx := context.Background()

	err := ExportWithRetry(ctx, exporter, spans, config, logger)
	require.NoError(t, err)
	assert.Equal(t, 1, exporter.attempts)
	assert.Len(t, exporter.exportedSpans, 1)
}

func TestExportWithRetry_SuccessAfterRetries(t *testing.T) {
	exporter := &MockFailingExporter{failUntil: 2} // Fails twice, succeeds on 3rd attempt
	logger := zap.NewNop()

	spans := []*domain.OTELSpanData{
		{
			TraceID:     "1234567890abcdef1234567890abcdef",
			SpanID:      "1234567890abcdef",
			ServiceName: "test",
			Name:        "operation",
		},
	}

	config := DefaultRetryConfig()
	ctx := context.Background()

	start := time.Now()
	err := ExportWithRetry(ctx, exporter, spans, config, logger)
	duration := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, 3, exporter.attempts)
	assert.Len(t, exporter.exportedSpans, 1)

	// Should have delays from retries (100ms + 200ms = 300ms minimum)
	assert.Greater(t, duration, 250*time.Millisecond)
}

func TestExportWithRetry_ExhaustsRetries(t *testing.T) {
	exporter := &MockFailingExporter{failUntil: 10} // Always fails
	logger := zap.NewNop()

	spans := []*domain.OTELSpanData{
		{
			TraceID:     "1234567890abcdef1234567890abcdef",
			SpanID:      "1234567890abcdef",
			ServiceName: "test",
			Name:        "operation",
		},
	}

	config := DefaultRetryConfig()
	ctx := context.Background()

	err := ExportWithRetry(ctx, exporter, spans, config, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed after")
	assert.Equal(t, config.MaxRetries+1, exporter.attempts)
	assert.Empty(t, exporter.exportedSpans)
}

func TestExportWithRetry_EmptySpans(t *testing.T) {
	exporter := &MockFailingExporter{failUntil: 0}
	logger := zap.NewNop()

	config := DefaultRetryConfig()
	ctx := context.Background()

	err := ExportWithRetry(ctx, exporter, nil, config, logger)
	assert.NoError(t, err)
	assert.Equal(t, 0, exporter.attempts)
}

func TestExportWithRetry_ContextCancelled(t *testing.T) {
	exporter := &MockFailingExporter{failUntil: 10} // Would retry forever
	logger := zap.NewNop()

	spans := []*domain.OTELSpanData{
		{
			TraceID:     "1234567890abcdef1234567890abcdef",
			SpanID:      "1234567890abcdef",
			ServiceName: "test",
			Name:        "operation",
		},
	}

	config := RetryConfig{
		MaxRetries:   10,
		InitialDelay: 500 * time.Millisecond, // Longer delay
		MaxDelay:     2 * time.Second,
		Multiplier:   2.0,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := ExportWithRetry(ctx, exporter, spans, config, logger)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	// Should have failed fast when context cancelled
	assert.LessOrEqual(t, exporter.attempts, 2)
}

func TestDefaultRetryConfig(t *testing.T) {
	config := DefaultRetryConfig()

	assert.Equal(t, 3, config.MaxRetries)
	assert.Equal(t, 100*time.Millisecond, config.InitialDelay)
	assert.Equal(t, 5*time.Second, config.MaxDelay)
	assert.Equal(t, 2.0, config.Multiplier)
}

func TestCalculateBackoff(t *testing.T) {
	config := DefaultRetryConfig()

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{0, 100 * time.Millisecond},
		{1, 200 * time.Millisecond},
		{2, 400 * time.Millisecond},
		{3, 800 * time.Millisecond},
		{4, 1600 * time.Millisecond},
		{5, 3200 * time.Millisecond},
		{6, 5000 * time.Millisecond},  // Capped at MaxDelay
		{10, 5000 * time.Millisecond}, // Still capped
	}

	for _, tt := range tests {
		t.Run(tt.expected.String(), func(t *testing.T) {
			result := calculateBackoff(tt.attempt, config)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExportWithRetry_CustomConfig(t *testing.T) {
	exporter := &MockFailingExporter{failUntil: 1} // Fails once
	logger := zap.NewNop()

	spans := []*domain.OTELSpanData{
		{
			TraceID:     "1234567890abcdef1234567890abcdef",
			SpanID:      "1234567890abcdef",
			ServiceName: "test",
			Name:        "operation",
		},
	}

	config := RetryConfig{
		MaxRetries:   1,
		InitialDelay: 50 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Multiplier:   3.0,
	}

	ctx := context.Background()
	start := time.Now()
	err := ExportWithRetry(ctx, exporter, spans, config, logger)
	duration := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, 2, exporter.attempts)

	// Should have one retry delay (~50ms)
	assert.Greater(t, duration, 40*time.Millisecond)
	assert.Less(t, duration, 200*time.Millisecond)
}
