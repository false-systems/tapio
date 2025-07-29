package pipeline

import (
	"context"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
)

// Pipeline processes raw events from collectors into UnifiedEvents
type Pipeline interface {
	// Process a raw event from collectors
	Process(ctx context.Context, event collectors.RawEvent) error
	
	// Start the pipeline
	Start(ctx context.Context) error
	
	// Stop the pipeline
	Stop() error
	
	// Get output channel of UnifiedEvents
	Output() <-chan *domain.UnifiedEvent
	
	// Health check
	IsHealthy() bool
}

// EventConverter converts raw events to UnifiedEvents based on source
type EventConverter interface {
	// Convert raw event to UnifiedEvent
	Convert(ctx context.Context, raw collectors.RawEvent) (*domain.UnifiedEvent, error)
	
	// Supported source type
	SourceType() string
}

// Enricher adds context to events
type Enricher interface {
	// Enrich event with additional context
	Enrich(ctx context.Context, event *domain.UnifiedEvent) error
}

// PipelineConfig configures the pipeline
type PipelineConfig struct {
	// Buffer size for output channel
	OutputBufferSize int
	
	// Number of workers for processing
	Workers int
	
	// Enable K8s enrichment
	EnableK8sEnrichment bool
	
	// Enable OTEL trace context
	EnableTracing bool
	
	// Batch size for processing
	BatchSize int
	
	// Flush interval for batches
	FlushInterval string
}