package extraction

import (
	"context"
	"fmt"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
)

// K8sExtractionStage implements the ProcessingStage interface for K8s context extraction
type K8sExtractionStage struct {
	extractor *K8sContextExtractor
	logger    *zap.Logger
}

// NewK8sExtractionStage creates a new K8s extraction stage
func NewK8sExtractionStage(k8sClient kubernetes.Interface, logger *zap.Logger) (*K8sExtractionStage, error) {
	extractor, err := NewK8sContextExtractor(k8sClient, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create K8s context extractor: %w", err)
	}

	return &K8sExtractionStage{
		extractor: extractor,
		logger:    logger,
	}, nil
}

// Name returns the stage name
func (s *K8sExtractionStage) Name() string {
	return "k8s-extraction"
}

// Process enriches the event with K8s context
func (s *K8sExtractionStage) Process(ctx context.Context, event *domain.UnifiedEvent) error {
	return s.extractor.Process(ctx, event)
}

// GetMetrics returns extraction metrics
func (s *K8sExtractionStage) GetMetrics() map[string]*ExtractionMetrics {
	return s.extractor.GetMetrics()
}
