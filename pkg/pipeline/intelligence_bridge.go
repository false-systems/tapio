package pipeline

import (
	"context"
	"fmt"
	"sync"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
	intelligencePipeline "github.com/yairfalse/tapio/pkg/intelligence/pipeline"
)

// IntelligenceBridge connects the collector pipeline to the intelligence pipeline
type IntelligenceBridge struct {
	collectorPipeline    Pipeline
	intelligencePipeline intelligencePipeline.IntelligencePipeline

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu      sync.RWMutex
	started bool
}

// NewIntelligenceBridge creates a bridge between collectors and intelligence
func NewIntelligenceBridge(
	collectorPipeline Pipeline,
	intelligencePipeline intelligencePipeline.IntelligencePipeline,
) *IntelligenceBridge {
	return &IntelligenceBridge{
		collectorPipeline:    collectorPipeline,
		intelligencePipeline: intelligencePipeline,
	}
}

// Start begins forwarding events from collectors to intelligence
func (b *IntelligenceBridge) Start(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.started {
		return fmt.Errorf("bridge already started")
	}

	b.ctx, b.cancel = context.WithCancel(ctx)
	b.started = true

	// Start the intelligence pipeline
	if err := b.intelligencePipeline.Start(ctx); err != nil {
		return fmt.Errorf("failed to start intelligence pipeline: %w", err)
	}

	// Start forwarding events
	b.wg.Add(1)
	go b.forwardEvents()

	return nil
}

// Stop gracefully shuts down the bridge
func (b *IntelligenceBridge) Stop() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.started {
		return nil
	}

	b.cancel()
	b.wg.Wait()

	// Stop the intelligence pipeline
	if err := b.intelligencePipeline.Stop(); err != nil {
		return fmt.Errorf("failed to stop intelligence pipeline: %w", err)
	}

	b.started = false
	return nil
}

// ProcessRawEvent processes a raw event through both pipelines
func (b *IntelligenceBridge) ProcessRawEvent(ctx context.Context, event collectors.RawEvent) error {
	// Process through collector pipeline first
	if err := b.collectorPipeline.Process(ctx, event); err != nil {
		return fmt.Errorf("collector pipeline error: %w", err)
	}

	// The forwardEvents goroutine will handle the rest
	return nil
}

// forwardEvents reads from collector pipeline and forwards to intelligence
func (b *IntelligenceBridge) forwardEvents() {
	defer b.wg.Done()

	for {
		select {
		case event, ok := <-b.collectorPipeline.Output():
			if !ok {
				// Channel closed
				return
			}

			// Forward to intelligence pipeline
			if err := b.intelligencePipeline.ProcessEvent(event); err != nil {
				// Log error but continue
				// In production, use proper logging
			}

		case <-b.ctx.Done():
			return
		}
	}
}

// GetMetrics returns combined metrics from both pipelines
func (b *IntelligenceBridge) GetMetrics() BridgeMetrics {
	return BridgeMetrics{
		CollectorPipelineHealthy:    b.collectorPipeline.IsHealthy(),
		IntelligencePipelineRunning: b.intelligencePipeline.IsRunning(),
		IntelligenceMetrics:         b.intelligencePipeline.GetMetrics(),
	}
}

// BridgeMetrics contains metrics from the bridge
type BridgeMetrics struct {
	CollectorPipelineHealthy    bool
	IntelligencePipelineRunning bool
	IntelligenceMetrics         intelligencePipeline.PipelineMetrics
}

// CreateDefaultBridge creates a bridge with default configuration
func CreateDefaultBridge(config BridgeConfig) (*IntelligenceBridge, error) {
	// Create collector pipeline
	collectorPipeline := NewPipeline(PipelineConfig{
		OutputBufferSize:    config.CollectorBufferSize,
		Workers:             config.CollectorWorkers,
		EnableK8sEnrichment: config.EnableK8sEnrichment,
		EnableTracing:       config.EnableTracing,
	})

	// Register converters
	RegisterConverters(collectorPipeline.(*CollectorPipeline))

	// Add enrichers if enabled
	if config.EnableK8sEnrichment && config.KubeConfig != "" {
		k8sEnricher, err := NewK8sEnricher(config.KubeConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create K8s enricher: %w", err)
		}
		collectorPipeline.(*CollectorPipeline).AddEnricher(k8sEnricher)
	}

	if config.EnableTracing {
		collectorPipeline.(*CollectorPipeline).AddEnricher(NewTraceEnricher())
	}

	// Create intelligence pipeline
	// Note: In production, you would create the actual intelligence pipeline here
	// For now, we'll return an error indicating this needs to be implemented
	// when integrating with the actual intelligence pipeline
	return nil, fmt.Errorf("intelligence pipeline integration not yet implemented")
}

// BridgeConfig configures the bridge between collectors and intelligence
type BridgeConfig struct {
	// Collector pipeline config
	CollectorBufferSize int
	CollectorWorkers    int
	EnableK8sEnrichment bool
	EnableTracing       bool
	KubeConfig          string

	// Intelligence pipeline config
	IntelligenceMode       string
	IntelligenceWorkers    int
	IntelligenceBufferSize int
}
