package base

import (
	"context"

	"golang.org/x/sync/errgroup"
)

// PipelineStage represents a single stage in the observer pipeline
type PipelineStage func(ctx context.Context) error

// Pipeline orchestrates parallel execution of observer stages using errgroup
type Pipeline struct {
	stages []PipelineStage
}

// NewPipeline creates a new pipeline
func NewPipeline() *Pipeline {
	return &Pipeline{
		stages: make([]PipelineStage, 0),
	}
}

// Add registers a new stage to the pipeline
func (p *Pipeline) Add(stage PipelineStage) {
	p.stages = append(p.stages, stage)
}

// Run executes all pipeline stages in parallel
// If any stage returns an error, all stages are cancelled via context
func (p *Pipeline) Run(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)

	for _, stage := range p.stages {
		stage := stage // Capture loop variable
		g.Go(func() error {
			return stage(ctx)
		})
	}

	return g.Wait()
}
