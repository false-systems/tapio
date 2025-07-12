package installer

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// pipeline implements the Pipeline interface with generics
type pipeline[T any] struct {
	steps           []Step[T]
	rollbackEnabled bool
	metrics         MetricsCollector
	executedSteps   []Step[T]
	mu              sync.Mutex
}

// NewPipeline creates a new pipeline
func NewPipeline[T any]() Pipeline[T] {
	return &pipeline[T]{
		steps:         make([]Step[T], 0),
		executedSteps: make([]Step[T], 0),
	}
}

// AddStep adds a step to the pipeline
func (p *pipeline[T]) AddStep(step Step[T]) Pipeline[T] {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.steps = append(p.steps, step)
	return p
}

// Execute runs all steps in order
func (p *pipeline[T]) Execute(ctx context.Context, initial T) (T, error) {
	p.mu.Lock()
	steps := make([]Step[T], len(p.steps))
	copy(steps, p.steps)
	p.executedSteps = make([]Step[T], 0, len(steps))
	p.mu.Unlock()

	current := initial

	for i, step := range steps {
		select {
		case <-ctx.Done():
			err := ctx.Err()
			if p.rollbackEnabled {
				p.rollback(ctx, current)
			}
			return current, fmt.Errorf("pipeline cancelled at step %d (%s): %w", i+1, step.Name(), err)
		default:
		}

		startTime := time.Now()

		// Execute step
		result, err := step.Execute(ctx, current)

		// Record metrics
		if p.metrics != nil {
			duration := time.Since(startTime)
			p.metrics.RecordDuration(step.Name(), duration)

			if err != nil {
				p.metrics.RecordError(step.Name(), err)
			} else {
				p.metrics.RecordSuccess(step.Name())
			}
		}

		if err != nil {
			// Rollback if enabled
			if p.rollbackEnabled {
				if rollbackErr := p.rollback(ctx, current); rollbackErr != nil {
					return current, fmt.Errorf("step %s failed: %w (rollback also failed: %v)",
						step.Name(), err, rollbackErr)
				}
			}
			return current, fmt.Errorf("step %s failed: %w", step.Name(), err)
		}

		// Validate step if possible
		if err := step.Validate(ctx, result); err != nil {
			if p.rollbackEnabled {
				p.rollback(ctx, current)
			}
			return current, fmt.Errorf("step %s validation failed: %w", step.Name(), err)
		}

		// Record executed step for potential rollback
		p.mu.Lock()
		p.executedSteps = append(p.executedSteps, step)
		p.mu.Unlock()

		current = result
	}

	return current, nil
}

// WithRollback enables automatic rollback on failure
func (p *pipeline[T]) WithRollback(enabled bool) Pipeline[T] {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.rollbackEnabled = enabled
	return p
}

// WithMetrics enables metrics collection
func (p *pipeline[T]) WithMetrics(collector MetricsCollector) Pipeline[T] {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.metrics = collector
	return p
}

// rollback performs rollback of executed steps in reverse order
func (p *pipeline[T]) rollback(ctx context.Context, data T) error {
	p.mu.Lock()
	steps := make([]Step[T], len(p.executedSteps))
	copy(steps, p.executedSteps)
	p.mu.Unlock()

	// Rollback in reverse order
	for i := len(steps) - 1; i >= 0; i-- {
		step := steps[i]

		// Create a context with timeout for rollback
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		if err := step.Rollback(rollbackCtx, data); err != nil {
			// Log error but continue with other rollbacks
			fmt.Printf("Warning: rollback failed for step %s: %v\n", step.Name(), err)
		}
	}

	return nil
}

// ParallelPipeline runs multiple pipelines in parallel
type ParallelPipeline[T any] struct {
	pipelines []Pipeline[T]
	strategy  ParallelStrategy
}

// ParallelStrategy defines how parallel pipelines are executed
type ParallelStrategy int

const (
	// AllMustSucceed requires all pipelines to succeed
	AllMustSucceed ParallelStrategy = iota
	// AnyCanSucceed allows some pipelines to fail
	AnyCanSucceed
	// FirstToSucceed stops when first pipeline succeeds
	FirstToSucceed
)

// NewParallelPipeline creates a new parallel pipeline
func NewParallelPipeline[T any](strategy ParallelStrategy) *ParallelPipeline[T] {
	return &ParallelPipeline[T]{
		pipelines: make([]Pipeline[T], 0),
		strategy:  strategy,
	}
}

// AddPipeline adds a pipeline to run in parallel
func (pp *ParallelPipeline[T]) AddPipeline(p Pipeline[T]) *ParallelPipeline[T] {
	pp.pipelines = append(pp.pipelines, p)
	return pp
}

// Execute runs all pipelines according to the strategy
func (pp *ParallelPipeline[T]) Execute(ctx context.Context, initial T) ([]T, error) {
	if len(pp.pipelines) == 0 {
		return nil, fmt.Errorf("no pipelines to execute")
	}

	results := make([]T, len(pp.pipelines))
	errors := make([]error, len(pp.pipelines))

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for i, pipeline := range pp.pipelines {
		wg.Add(1)
		go func(idx int, p Pipeline[T]) {
			defer wg.Done()

			result, err := p.Execute(ctx, initial)
			results[idx] = result
			errors[idx] = err

			// Handle different strategies
			if pp.strategy == FirstToSucceed && err == nil {
				cancel() // Cancel other pipelines
			}
		}(i, pipeline)
	}

	wg.Wait()

	// Evaluate results based on strategy
	switch pp.strategy {
	case AllMustSucceed:
		for i, err := range errors {
			if err != nil {
				return results, fmt.Errorf("pipeline %d failed: %w", i, err)
			}
		}
		return results, nil

	case AnyCanSucceed:
		successCount := 0
		for _, err := range errors {
			if err == nil {
				successCount++
			}
		}
		if successCount == 0 {
			return results, fmt.Errorf("all pipelines failed")
		}
		return results, nil

	case FirstToSucceed:
		for i, err := range errors {
			if err == nil {
				return []T{results[i]}, nil
			}
		}
		return results, fmt.Errorf("no pipeline succeeded")

	default:
		return results, fmt.Errorf("unknown parallel strategy")
	}
}

// ConditionalStep wraps a step with a condition
type ConditionalStep[T any] struct {
	step      Step[T]
	condition func(ctx context.Context, data T) bool
}

// NewConditionalStep creates a new conditional step
func NewConditionalStep[T any](step Step[T], condition func(context.Context, T) bool) Step[T] {
	return &ConditionalStep[T]{
		step:      step,
		condition: condition,
	}
}

func (c *ConditionalStep[T]) Name() string {
	return fmt.Sprintf("conditional[%s]", c.step.Name())
}

func (c *ConditionalStep[T]) Execute(ctx context.Context, data T) (T, error) {
	if c.condition(ctx, data) {
		return c.step.Execute(ctx, data)
	}
	// Skip execution if condition is false
	return data, nil
}

func (c *ConditionalStep[T]) Rollback(ctx context.Context, data T) error {
	if c.condition(ctx, data) {
		return c.step.Rollback(ctx, data)
	}
	return nil
}

func (c *ConditionalStep[T]) Validate(ctx context.Context, data T) error {
	if c.condition(ctx, data) {
		return c.step.Validate(ctx, data)
	}
	return nil
}

// RetryStep wraps a step with retry logic
type RetryStep[T any] struct {
	step       Step[T]
	maxRetries int
	delay      time.Duration
	backoff    BackoffStrategy
}

// BackoffStrategy defines retry backoff behavior
type BackoffStrategy func(attempt int, delay time.Duration) time.Duration

// ExponentialBackoff doubles the delay for each retry
func ExponentialBackoff(attempt int, delay time.Duration) time.Duration {
	return delay * time.Duration(1<<uint(attempt-1))
}

// LinearBackoff adds a fixed delay for each retry
func LinearBackoff(attempt int, delay time.Duration) time.Duration {
	return delay * time.Duration(attempt)
}

// NewRetryStep creates a new retry step
func NewRetryStep[T any](step Step[T], maxRetries int, delay time.Duration) Step[T] {
	return &RetryStep[T]{
		step:       step,
		maxRetries: maxRetries,
		delay:      delay,
		backoff:    ExponentialBackoff,
	}
}

func (r *RetryStep[T]) Name() string {
	return fmt.Sprintf("retry[%s]", r.step.Name())
}

func (r *RetryStep[T]) Execute(ctx context.Context, data T) (T, error) {
	var lastErr error

	for attempt := 1; attempt <= r.maxRetries; attempt++ {
		result, err := r.step.Execute(ctx, data)
		if err == nil {
			return result, nil
		}

		lastErr = err

		if attempt < r.maxRetries {
			waitTime := r.backoff(attempt, r.delay)
			select {
			case <-ctx.Done():
				return data, ctx.Err()
			case <-time.After(waitTime):
				// Continue to next retry
			}
		}
	}

	return data, fmt.Errorf("failed after %d attempts: %w", r.maxRetries, lastErr)
}

func (r *RetryStep[T]) Rollback(ctx context.Context, data T) error {
	return r.step.Rollback(ctx, data)
}

func (r *RetryStep[T]) Validate(ctx context.Context, data T) error {
	return r.step.Validate(ctx, data)
}
