package interfaces

import (
	"context"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// EventPipeline defines the interface for high-performance event processing
type EventPipeline interface {
	// Start initializes the event pipeline
	Start() error

	// Stop gracefully shuts down the pipeline
	Stop() error

	// Submit submits an event for processing
	Submit(event *PipelineEvent) error

	// GetOutput retrieves a processed event
	GetOutput() (*PipelineEvent, error)

	// GetMetrics returns pipeline performance metrics
	GetMetrics() *PipelineMetrics

	// GetEvent gets an event from the object pool
	GetEvent() *PipelineEvent

	// PutEvent returns an event to the object pool
	PutEvent(event *PipelineEvent)
}

// BatchProcessor defines the interface for batch processing capabilities
type BatchProcessor interface {
	// ProcessBatch processes multiple events in a batch
	ProcessBatch(ctx context.Context, events []*domain.UnifiedEvent) error

	// GetBatchSize returns the optimal batch size
	GetBatchSize() int

	// SetBatchSize sets the batch size
	SetBatchSize(size int)
}

// PipelineEvent represents an event in the processing pipeline
type PipelineEvent struct {
	ID        uint64
	Type      string
	Timestamp int64
	Priority  uint8
	Metadata  [8]uint64
}

// PipelineMetrics contains performance metrics for the pipeline
type PipelineMetrics struct {
	Throughput      uint64
	AverageLatency  time.Duration
	QueueDepth      int
	ErrorRate       float64
	EventsProcessed uint64
	EventsDropped   uint64
}
