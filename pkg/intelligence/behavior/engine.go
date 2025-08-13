package behavior

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Engine is the main behavior correlation engine
// Level 2 - Can only import from domain (Level 0)
type Engine struct {
	logger *zap.Logger

	// Pattern management
	patternLoader  *PatternLoader
	patternMatcher *PatternMatcher
	predictor      *Predictor

	// Circuit breaker for reliability
	circuitBreaker *CircuitBreaker

	// Backpressure management
	backpressure *BackpressureManager

	// Result pooling for performance
	resultPool *sync.Pool

	// OTEL instrumentation - DIRECT USAGE, NO WRAPPERS
	tracer             trace.Tracer
	patternsLoaded     metric.Int64Gauge
	eventsProcessed    metric.Int64Counter
	predictionsCreated metric.Int64Counter
	processingDuration metric.Float64Histogram
	errorsTotal        metric.Int64Counter
}

// NewEngine creates a new behavior correlation engine
func NewEngine(logger *zap.Logger) (*Engine, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	// Initialize OTEL components - MANDATORY pattern per CLAUDE.md
	name := "behavior-engine"
	tracer := otel.Tracer(name)
	meter := otel.Meter(name)

	// Create metrics with descriptive names
	patternsLoaded, err := meter.Int64Gauge(
		fmt.Sprintf("%s_patterns_loaded", name),
		metric.WithDescription("Number of behavior patterns currently loaded"),
	)
	if err != nil {
		logger.Warn("Failed to create patterns gauge", zap.Error(err))
	}

	eventsProcessed, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription(fmt.Sprintf("Total events processed by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create events counter", zap.Error(err))
	}

	predictionsCreated, err := meter.Int64Counter(
		fmt.Sprintf("%s_predictions_created_total", name),
		metric.WithDescription("Total predictions created"),
	)
	if err != nil {
		logger.Warn("Failed to create predictions counter", zap.Error(err))
	}

	processingDuration, err := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_duration_ms", name),
		metric.WithDescription("Event processing duration in milliseconds"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		logger.Warn("Failed to create processing histogram", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription(fmt.Sprintf("Total errors in %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	// Initialize result pool for GC pressure reduction
	resultPool := &sync.Pool{
		New: func() interface{} {
			return &domain.PredictionResult{
				Context:       make(map[string]interface{}, 10),
				RelatedEvents: make([]string, 0, 10),
			}
		},
	}

	engine := &Engine{
		logger:             logger,
		resultPool:         resultPool,
		tracer:             tracer,
		patternsLoaded:     patternsLoaded,
		eventsProcessed:    eventsProcessed,
		predictionsCreated: predictionsCreated,
		processingDuration: processingDuration,
		errorsTotal:        errorsTotal,
	}

	// Initialize pattern loader
	// Single source of truth for patterns
	patternDirs := []string{
		"patterns/behavior",
	}

	var validDirs []string
	for _, dir := range patternDirs {
		if _, err := os.Stat(dir); err == nil {
			validDirs = append(validDirs, dir)
		}
	}

	// If no valid directories found, create engine without patterns
	if len(validDirs) == 0 {
		logger.Warn("No pattern directories found, starting without patterns")
		validDirs = []string{"."} // Use current directory as placeholder
	}

	patternConfig := PatternLoaderConfig{
		PatternDirs:     validDirs,
		WatchForChanges: false, // Disable watching for now
		ValidationLevel: ValidationStandard,
		ReloadDebounce:  2 * time.Second,
	}
	patternLoader, err := NewPatternLoader(logger, patternConfig)
	if err != nil {
		// Create a minimal pattern loader without patterns
		logger.Warn("Failed to create pattern loader, starting with empty patterns", zap.Error(err))
		patternLoader = &PatternLoader{
			logger:   logger,
			patterns: make(map[string]*domain.BehaviorPattern),
		}
	}
	engine.patternLoader = patternLoader

	// Initialize components
	engine.patternMatcher = NewPatternMatcher(logger)
	engine.predictor = NewPredictor(logger)
	engine.predictor.patternLoader = patternLoader // Connect pattern loader to predictor
	engine.circuitBreaker = NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:  CircuitBreakerMaxFailures,
		ResetTimeout: CircuitBreakerResetTimeout,
	})
	engine.backpressure = NewBackpressureManager(MaxQueueSize)

	// Load patterns into matcher
	patterns := patternLoader.GetAllPatterns()
	engine.patternMatcher.UpdatePatterns(convertPatternPointers(patterns))

	// Update metrics
	if patternsLoaded != nil {
		patternsLoaded.Record(context.Background(), int64(len(patterns)))
	}

	logger.Info("Behavior engine initialized",
		zap.Int("max_queue_size", MaxQueueSize),
		zap.Duration("circuit_breaker_timeout", CircuitBreakerResetTimeout),
	)

	return engine, nil
}

// ReloadPatterns reloads patterns from disk
func (e *Engine) ReloadPatterns() error {
	ctx := context.Background()
	if err := e.patternLoader.loadAllPatterns(ctx); err != nil {
		return fmt.Errorf("failed to reload patterns: %w", err)
	}

	// Update pattern matcher
	patterns := e.patternLoader.GetAllPatterns()
	e.patternMatcher.UpdatePatterns(convertPatternPointers(patterns))

	// Update metrics
	if e.patternsLoaded != nil {
		e.patternsLoaded.Record(ctx, int64(len(patterns)))
	}

	e.logger.Info("Patterns reloaded",
		zap.Int("count", len(patterns)),
	)

	return nil
}

// Process processes an event through the behavior engine
func (e *Engine) Process(ctx context.Context, event *domain.UnifiedEvent) (*domain.PredictionResult, error) {
	// Validate input
	if event == nil {
		return nil, fmt.Errorf("event cannot be nil")
	}

	// Circuit breaker protection
	return e.circuitBreaker.Execute(ctx, func() (*domain.PredictionResult, error) {
		// Timeout protection
		ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		defer cancel()

		// Backpressure check
		if !e.backpressure.TryAccept() {
			if e.errorsTotal != nil {
				e.errorsTotal.Add(ctx, 1, metric.WithAttributes(
					attribute.String("error_type", "backpressure"),
				))
			}
			return nil, fmt.Errorf("system overloaded, dropping event")
		}
		defer e.backpressure.Release()

		// Process with OTEL tracing
		ctx, span := e.tracer.Start(ctx, "behavior.engine.process")
		defer span.End()

		startTime := time.Now()
		defer func() {
			duration := time.Since(startTime).Milliseconds()
			if e.processingDuration != nil {
				e.processingDuration.Record(ctx, float64(duration))
			}
		}()

		// Set span attributes
		span.SetAttributes(
			attribute.String("event.id", event.ID),
			attribute.String("event.type", string(event.Type)),
		)

		// Get result from pool
		result := e.resultPool.Get().(*domain.PredictionResult)
		defer func() {
			// Clear and return to pool
			result.Prediction = nil
			result.RelatedEvents = result.RelatedEvents[:0]
			for k := range result.Context {
				delete(result.Context, k)
			}
			e.resultPool.Put(result)
		}()

		// Match patterns
		matches, err := e.patternMatcher.Match(ctx, event)
		if err != nil {
			span.RecordError(err)
			if e.errorsTotal != nil {
				e.errorsTotal.Add(ctx, 1, metric.WithAttributes(
					attribute.String("error_type", "pattern_match"),
				))
			}
			return nil, fmt.Errorf("pattern matching failed: %w", err)
		}

		// No matches found
		if len(matches) == 0 {
			span.AddEvent("no_patterns_matched")
			return nil, nil
		}

		// Generate prediction from best match
		bestMatch := e.selectBestMatch(matches)
		domainMatch := e.convertToDomainMatch(bestMatch)
		prediction, err := e.predictor.GeneratePrediction(ctx, domainMatch, event)
		if err != nil {
			span.RecordError(err)
			if e.errorsTotal != nil {
				e.errorsTotal.Add(ctx, 1, metric.WithAttributes(
					attribute.String("error_type", "prediction_generation"),
				))
			}
			return nil, fmt.Errorf("prediction generation failed: %w", err)
		}

		// Record success metrics
		if e.eventsProcessed != nil {
			e.eventsProcessed.Add(ctx, 1)
		}
		if e.predictionsCreated != nil && prediction != nil {
			e.predictionsCreated.Add(ctx, 1, metric.WithAttributes(
				attribute.String("pattern", prediction.PatternName),
				attribute.Float64("confidence", prediction.Confidence),
			))
		}

		// Build result
		finalResult := &domain.PredictionResult{
			Prediction:    prediction,
			Context:       make(map[string]interface{}),
			RelatedEvents: []string{event.ID},
		}

		span.AddEvent("prediction_generated", trace.WithAttributes(
			attribute.String("pattern", prediction.PatternName),
			attribute.Float64("confidence", prediction.Confidence),
		))

		return finalResult, nil
	})
}

// validatePattern validates a behavior pattern
func (e *Engine) validatePattern(pattern *domain.BehaviorPattern) error {
	if pattern.ID == "" {
		return fmt.Errorf("pattern ID is required")
	}
	if pattern.Name == "" {
		return fmt.Errorf("pattern name is required")
	}
	if len(pattern.Conditions) == 0 {
		return fmt.Errorf("pattern must have at least one condition")
	}
	if len(pattern.PredictionTemplate.PotentialImpacts) == 0 {
		return fmt.Errorf("pattern must have at least one potential impact")
	}
	if pattern.BaseConfidence <= 0 || pattern.BaseConfidence > 1 {
		return fmt.Errorf("base confidence must be between 0 and 1")
	}
	return nil
}

// convertToDomainMatch converts a BehaviorPatternMatch to domain.PatternMatch
func (e *Engine) convertToDomainMatch(match BehaviorPatternMatch) domain.PatternMatch {
	// Convert conditions
	conditions := make([]domain.ConditionMatch, 0, len(match.Conditions))
	for _, c := range match.Conditions {
		conditions = append(conditions, domain.ConditionMatch{
			Matched:       c.Matched,
			ActualValue:   fmt.Sprintf("%v", c.ActualValue),
			ExpectedValue: fmt.Sprintf("%v", c.Condition.Match.Value),
			Message:       c.Message,
		})
	}

	return domain.PatternMatch{
		PatternID:   match.PatternID,
		PatternName: match.PatternName,
		Type:        "behavior",
		Confidence:  match.Confidence,
		MatchTime:   match.MatchedAt,
		Timestamp:   match.MatchedAt,
		Conditions:  conditions,
	}
}

// selectBestMatch selects the best pattern match with consensus scoring
func (e *Engine) selectBestMatch(matches []BehaviorPatternMatch) BehaviorPatternMatch {
	if len(matches) == 1 {
		return matches[0]
	}

	// Apply consensus finding from aggregator
	consensusBoost := e.calculateConsensus(matches)

	best := matches[0]
	bestScore := best.Confidence

	for i, match := range matches {
		// Calculate adjusted score with consensus
		score := match.Confidence
		if consensusBoost > 0.5 { // Majority agreement
			score *= (1 + 0.2) // 20% boost for consensus
		}

		if i == 0 {
			bestScore = score
			continue
		}

		// Higher score wins
		if score > bestScore {
			best = match
			bestScore = score
		} else if score == bestScore && match.MatchedAt.After(best.MatchedAt) {
			// If scores equal, prefer more recent
			best = match
		}
	}
	return best
}

// calculateConsensus finds agreement between multiple pattern matches (from aggregator)
func (e *Engine) calculateConsensus(matches []BehaviorPatternMatch) float64 {
	if len(matches) < 2 {
		return 0
	}

	// Count pattern names that agree
	patternNames := make(map[string]int)
	for _, match := range matches {
		patternNames[match.PatternName]++
	}

	// Find most common pattern
	maxCount := 0
	for _, count := range patternNames {
		if count > maxCount {
			maxCount = count
		}
	}

	// Return consensus ratio
	return float64(maxCount) / float64(len(matches))
}

// GetPatterns returns currently loaded patterns
func (e *Engine) GetPatterns() []*domain.BehaviorPattern {
	return e.patternLoader.GetAllPatterns()
}

// UpdatePatternConfidence updates pattern confidence based on feedback
func (e *Engine) UpdatePatternConfidence(patternID string, adjustment float64) error {
	pattern, exists := e.patternLoader.GetPattern(patternID)
	if !exists {
		return fmt.Errorf("pattern %s not found", patternID)
	}

	// Adjust confidence with bounds [0.1, 1.0]
	newConfidence := pattern.BaseConfidence * adjustment
	if newConfidence < 0.1 {
		newConfidence = 0.1
	} else if newConfidence > 1.0 {
		newConfidence = 1.0
	}
	pattern.AdjustedConfidence = newConfidence

	e.logger.Info("Pattern confidence updated",
		zap.String("pattern_id", patternID),
		zap.Float64("base", pattern.BaseConfidence),
		zap.Float64("adjusted", newConfidence),
	)

	return nil
}

// Health returns the health status of the engine
func (e *Engine) Health(ctx context.Context) (bool, map[string]interface{}) {
	details := map[string]interface{}{
		"patterns_loaded": len(e.patternLoader.GetAllPatterns()),
		"circuit_breaker": e.circuitBreaker.State(),
		"queue_usage":     e.backpressure.Usage(),
	}

	healthy := e.circuitBreaker.State() != "open" &&
		e.backpressure.Usage() < 0.9 // Less than 90% full

	return healthy, details
}

// Stop stops the engine and all components
func (e *Engine) Stop() error {
	if e.patternLoader != nil {
		return e.patternLoader.Stop()
	}
	return nil
}

// convertPatternPointers converts pattern pointers to values
func convertPatternPointers(patterns []*domain.BehaviorPattern) []domain.BehaviorPattern {
	result := make([]domain.BehaviorPattern, len(patterns))
	for i, p := range patterns {
		result[i] = *p
	}
	return result
}
