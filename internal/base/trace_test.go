package base

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	otelTrace "go.opentelemetry.io/otel/trace"
)

func TestExtractTraceContext(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(provider)
	defer otel.SetTracerProvider(nil)

	tracer := provider.Tracer("test")

	t.Run("extracts valid trace context", func(t *testing.T) {
		ctx, span := tracer.Start(context.Background(), "test-span")
		defer span.End()

		event := &domain.ObserverEvent{
			ID:     "test-1",
			Type:   "tcp_connect",
			Source: "test",
		}

		ExtractTraceContext(ctx, event)

		sc := span.SpanContext()
		assert.Equal(t, sc.TraceID().String(), event.TraceID)
		assert.Equal(t, sc.SpanID().String(), event.SpanID)
		assert.Equal(t, byte(sc.TraceFlags()), event.TraceFlags)
	})

	t.Run("handles nil event", func(t *testing.T) {
		ctx, span := tracer.Start(context.Background(), "test-span")
		defer span.End()

		ExtractTraceContext(ctx, nil)
		// Should not panic
	})

	t.Run("handles invalid span context", func(t *testing.T) {
		ctx := context.Background()
		event := &domain.ObserverEvent{
			ID:     "test-2",
			Type:   "dns_query",
			Source: "test",
		}

		ExtractTraceContext(ctx, event)

		assert.Empty(t, event.TraceID)
		assert.Empty(t, event.SpanID)
		assert.Zero(t, event.TraceFlags)
	})
}

func TestInjectTraceContext(t *testing.T) {
	t.Run("injects valid trace context", func(t *testing.T) {
		event := &domain.ObserverEvent{
			ID:         "test-1",
			Type:       "tcp_connect",
			Source:     "test",
			TraceID:    "0af7651916cd43dd8448eb211c80319c",
			SpanID:     "b9c7c989f97918e1",
			TraceFlags: 1,
		}

		ctx := InjectTraceContext(event)
		require.NotNil(t, ctx)

		// Verify span context was injected
		sc := otelTrace.SpanContextFromContext(ctx)
		assert.True(t, sc.IsValid())
		assert.Equal(t, event.TraceID, sc.TraceID().String())
		assert.Equal(t, event.SpanID, sc.SpanID().String())
		assert.Equal(t, event.TraceFlags, byte(sc.TraceFlags()))
		assert.True(t, sc.IsRemote())
	})

	t.Run("handles nil event", func(t *testing.T) {
		ctx := InjectTraceContext(nil)
		require.NotNil(t, ctx)

		sc := otelTrace.SpanContextFromContext(ctx)
		assert.False(t, sc.IsValid())
	})

	t.Run("handles empty trace ID", func(t *testing.T) {
		event := &domain.ObserverEvent{
			ID:      "test-2",
			Type:    "dns_query",
			Source:  "test",
			TraceID: "",
			SpanID:  "b9c7c989f97918e1",
		}

		ctx := InjectTraceContext(event)
		require.NotNil(t, ctx)

		sc := otelTrace.SpanContextFromContext(ctx)
		assert.False(t, sc.IsValid())
	})

	t.Run("handles invalid trace ID hex", func(t *testing.T) {
		event := &domain.ObserverEvent{
			ID:      "test-3",
			Type:    "tcp_connect",
			Source:  "test",
			TraceID: "invalid-hex-string",
			SpanID:  "b9c7c989f97918e1",
		}

		ctx := InjectTraceContext(event)
		require.NotNil(t, ctx)

		sc := otelTrace.SpanContextFromContext(ctx)
		assert.False(t, sc.IsValid())
	})

	t.Run("handles invalid span ID hex", func(t *testing.T) {
		event := &domain.ObserverEvent{
			ID:      "test-4",
			Type:    "tcp_connect",
			Source:  "test",
			TraceID: "0af7651916cd43dd8448eb211c80319c",
			SpanID:  "invalid-hex",
		}

		ctx := InjectTraceContext(event)
		require.NotNil(t, ctx)

		sc := otelTrace.SpanContextFromContext(ctx)
		assert.False(t, sc.IsValid())
	})
}

func TestHasTraceContext(t *testing.T) {
	t.Run("returns true for valid trace context", func(t *testing.T) {
		event := &domain.ObserverEvent{
			ID:      "test-1",
			Type:    "tcp_connect",
			Source:  "test",
			TraceID: "0af7651916cd43dd8448eb211c80319c",
			SpanID:  "b9c7c989f97918e1",
		}

		assert.True(t, HasTraceContext(event))
	})

	t.Run("returns false for nil event", func(t *testing.T) {
		assert.False(t, HasTraceContext(nil))
	})

	t.Run("returns false for empty trace ID", func(t *testing.T) {
		event := &domain.ObserverEvent{
			ID:      "test-2",
			Type:    "dns_query",
			Source:  "test",
			TraceID: "",
			SpanID:  "b9c7c989f97918e1",
		}

		assert.False(t, HasTraceContext(event))
	})

	t.Run("returns false for empty span ID", func(t *testing.T) {
		event := &domain.ObserverEvent{
			ID:      "test-3",
			Type:    "tcp_connect",
			Source:  "test",
			TraceID: "0af7651916cd43dd8448eb211c80319c",
			SpanID:  "",
		}

		assert.False(t, HasTraceContext(event))
	})

	t.Run("returns false for both empty", func(t *testing.T) {
		event := &domain.ObserverEvent{
			ID:      "test-4",
			Type:    "oom_kill",
			Source:  "test",
			TraceID: "",
			SpanID:  "",
		}

		assert.False(t, HasTraceContext(event))
	})
}

func TestContextPropagationRoundTrip(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(provider)
	defer otel.SetTracerProvider(nil)

	tracer := provider.Tracer("test")

	t.Run("extracts and injects trace context", func(t *testing.T) {
		// Create span with trace context
		ctx, span := tracer.Start(context.Background(), "test-span")
		defer span.End()

		originalSC := span.SpanContext()

		// Extract into event
		event := &domain.ObserverEvent{
			ID:     "test-1",
			Type:   "tcp_connect",
			Source: "test",
		}
		ExtractTraceContext(ctx, event)

		// Verify extraction
		assert.True(t, HasTraceContext(event))

		// Inject back into context
		newCtx := InjectTraceContext(event)
		newSC := otelTrace.SpanContextFromContext(newCtx)

		// Verify injection preserved trace context
		assert.True(t, newSC.IsValid())
		assert.Equal(t, originalSC.TraceID(), newSC.TraceID())
		assert.Equal(t, originalSC.SpanID(), newSC.SpanID())
		assert.Equal(t, originalSC.TraceFlags(), newSC.TraceFlags())
		assert.True(t, newSC.IsRemote())
	})
}
