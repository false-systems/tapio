package otel

import (
	"context"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/sdk/trace"
)

// SpanReader reads OTEL spans from SDK and transforms to domain types
// Implementations can read from:
//   - BatchSpanProcessor (in-process)
//   - SpanExporter (custom exporter)
//   - Test mock (for testing)
type SpanReader interface {
	// ReadSpans reads available spans and returns them as domain types
	// Returns empty slice if no spans available
	ReadSpans(ctx context.Context) ([]*domain.OTELSpanData, error)

	// Close releases resources
	Close() error
}

// MetricReader reads OTEL metrics from SDK and transforms to domain types
// Implementations can read from:
//   - PeriodicReader (in-process)
//   - MetricExporter (custom exporter)
//   - Test mock (for testing)
type MetricReader interface {
	// ReadMetrics reads available metrics and returns them as domain types
	// Returns empty slice if no metrics available
	ReadMetrics(ctx context.Context) ([]*domain.OTELMetricData, error)

	// Close releases resources
	Close() error
}

// BatchSpanReader implements SpanReader using OTEL SDK BatchSpanProcessor
// This reads spans directly from the application's OTEL SDK instrumentation
type BatchSpanReader struct {
	processor trace.SpanProcessor
	buffer    chan trace.ReadOnlySpan
	bufSize   int
}

// NewBatchSpanReader creates a reader that captures spans from SDK
func NewBatchSpanReader(bufferSize int) *BatchSpanReader {
	if bufferSize <= 0 {
		bufferSize = 1000
	}

	return &BatchSpanReader{
		buffer:  make(chan trace.ReadOnlySpan, bufferSize),
		bufSize: bufferSize,
	}
}

// ReadSpans reads buffered spans and converts to domain types
func (r *BatchSpanReader) ReadSpans(ctx context.Context) ([]*domain.OTELSpanData, error) {
	spans := make([]*domain.OTELSpanData, 0)

	// Non-blocking read of available spans
	for {
		select {
		case span := <-r.buffer:
			domainSpan := convertSpanToDomain(span)
			spans = append(spans, domainSpan)
		default:
			// No more spans available
			return spans, nil
		}
	}
}

// Close releases resources
func (r *BatchSpanReader) Close() error {
	close(r.buffer)
	return nil
}

// convertSpanToDomain transforms OTEL SDK span to domain.OTELSpanData
// Full implementation will be in span_processor.go
func convertSpanToDomain(span trace.ReadOnlySpan) *domain.OTELSpanData {
	// Minimal stub for interface testing
	// Complete transformation logic coming in next commit
	return &domain.OTELSpanData{
		TraceID: span.SpanContext().TraceID().String(),
		SpanID:  span.SpanContext().SpanID().String(),
		Name:    span.Name(),
	}
}
