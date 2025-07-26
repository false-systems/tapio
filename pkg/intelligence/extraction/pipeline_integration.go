package extraction

import (
	"context"
	"fmt"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/pipeline"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
)

// K8sEnrichedPipelineBuilder extends the pipeline builder with K8s context extraction
type K8sEnrichedPipelineBuilder struct {
	*pipeline.PipelineBuilder
	k8sClient kubernetes.Interface
	logger    *zap.Logger
}

// NewK8sEnrichedPipelineBuilder creates a new pipeline builder with K8s enrichment
func NewK8sEnrichedPipelineBuilder(k8sClient kubernetes.Interface, logger *zap.Logger) *K8sEnrichedPipelineBuilder {
	return &K8sEnrichedPipelineBuilder{
		PipelineBuilder: pipeline.NewPipelineBuilder(),
		k8sClient:       k8sClient,
		logger:          logger,
	}
}

// WithK8sExtraction adds the K8s context extraction stage to the pipeline
func (pb *K8sEnrichedPipelineBuilder) WithK8sExtraction() *K8sEnrichedPipelineBuilder {
	stage, err := NewK8sExtractionStage(pb.k8sClient, pb.logger)
	if err != nil {
		pb.logger.Error("Failed to create K8s extraction stage", zap.Error(err))
		return pb
	}

	pb.AddStage(stage)
	return pb
}

// Build creates the pipeline with K8s enrichment
func (pb *K8sEnrichedPipelineBuilder) Build() (pipeline.IntelligencePipeline, error) {
	return pb.PipelineBuilder.Build()
}

// CreateK8sEnrichedPipeline creates a high-performance pipeline with K8s context extraction
func CreateK8sEnrichedPipeline(k8sClient kubernetes.Interface, logger *zap.Logger) (pipeline.IntelligencePipeline, error) {
	builder := NewK8sEnrichedPipelineBuilder(k8sClient, logger)

	// Add K8s extraction stage
	stage, err := NewK8sExtractionStage(k8sClient, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create K8s extraction stage: %w", err)
	}

	return builder.
		WithMode(pipeline.PipelineModeHighPerformance).
		EnableValidation(true).
		EnableContext(true).
		EnableCorrelation(true).
		AddStage(stage).
		Build()
}

// K8sExtractionProcessor wraps K8sContextExtractor for direct event processing
type K8sExtractionProcessor struct {
	extractor *K8sContextExtractor
}

// NewK8sExtractionProcessor creates a new K8s extraction processor
func NewK8sExtractionProcessor(k8sClient kubernetes.Interface, logger *zap.Logger) (*K8sExtractionProcessor, error) {
	extractor, err := NewK8sContextExtractor(k8sClient, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create K8s context extractor: %w", err)
	}

	return &K8sExtractionProcessor{
		extractor: extractor,
	}, nil
}

// ProcessEvent enriches a single event with K8s context
func (p *K8sExtractionProcessor) ProcessEvent(ctx context.Context, event *domain.UnifiedEvent) error {
	return p.extractor.Process(ctx, event)
}

// ProcessBatch enriches a batch of events with K8s context
func (p *K8sExtractionProcessor) ProcessBatch(ctx context.Context, events []*domain.UnifiedEvent) error {
	for _, event := range events {
		if err := p.extractor.Process(ctx, event); err != nil {
			// Log error but continue processing other events
			p.extractor.logger.Warn("Failed to enrich event with K8s context",
				zap.String("event_id", event.ID),
				zap.Error(err),
			)
		}
	}
	return nil
}

// GetMetrics returns extraction metrics
func (p *K8sExtractionProcessor) GetMetrics() map[string]*ExtractionMetrics {
	return p.extractor.GetMetrics()
}
