package otel

import (
	"context"

	"github.com/yairfalse/tapio/pkg/domain"
)

// OTLPExporter exports spans to an OTLP/gRPC endpoint
type OTLPExporter interface {
	// ExportSpans sends a batch of spans to the OTLP endpoint
	ExportSpans(ctx context.Context, spans []*domain.OTELSpanData) error

	// Shutdown gracefully closes the exporter
	Shutdown(ctx context.Context) error
}

// NoopExporter is a no-op implementation for when OTLP is disabled
type NoopExporter struct{}

// ExportSpans does nothing
func (n *NoopExporter) ExportSpans(_ context.Context, _ []*domain.OTELSpanData) error {
	return nil
}

// Shutdown does nothing
func (n *NoopExporter) Shutdown(_ context.Context) error {
	return nil
}

// Verify NoopExporter implements OTLPExporter
var _ OTLPExporter = (*NoopExporter)(nil)
