package otel

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestNoopExporter(t *testing.T) {
	exporter := &NoopExporter{}

	t.Run("implements interface", func(t *testing.T) {
		var _ OTLPExporter = exporter
	})

	t.Run("ExportSpans succeeds", func(t *testing.T) {
		ctx := context.Background()
		spans := []*domain.OTELSpanData{
			{
				TraceID:     "trace-123",
				SpanID:      "span-456",
				ServiceName: "test-service",
				StartTime:   time.Now(),
				EndTime:     time.Now().Add(100 * time.Millisecond),
			},
		}

		err := exporter.ExportSpans(ctx, spans)
		assert.NoError(t, err)
	})

	t.Run("ExportSpans handles nil spans", func(t *testing.T) {
		ctx := context.Background()
		err := exporter.ExportSpans(ctx, nil)
		assert.NoError(t, err)
	})

	t.Run("ExportSpans handles empty batch", func(t *testing.T) {
		ctx := context.Background()
		spans := []*domain.OTELSpanData{}
		err := exporter.ExportSpans(ctx, spans)
		assert.NoError(t, err)
	})

	t.Run("Shutdown succeeds", func(t *testing.T) {
		ctx := context.Background()
		err := exporter.Shutdown(ctx)
		assert.NoError(t, err)
	})

	t.Run("Shutdown handles cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := exporter.Shutdown(ctx)
		assert.NoError(t, err)
	})
}

func TestNoopExporter_Concurrency(t *testing.T) {
	exporter := &NoopExporter{}
	ctx := context.Background()

	// Concurrent exports
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			spans := []*domain.OTELSpanData{
				{
					TraceID:     "trace-123",
					SpanID:      "span-456",
					ServiceName: "test-service",
				},
			}
			err := exporter.ExportSpans(ctx, spans)
			require.NoError(t, err)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}
