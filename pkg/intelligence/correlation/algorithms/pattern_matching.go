package algorithms
import (
	"context"
	"fmt"
	"sort"
	"time"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation/core"
)
// patternMatcher implements pattern-based correlation analysis
type patternMatcher struct {
	config core.AlgorithmConfig
}
// NewPatternMatcher creates a new pattern matcher
func NewPatternMatcher(config core.AlgorithmConfig) core.PatternMatcher {
	return &patternMatcher{
		config: config,
	}
}
// FindPatterns finds all matching patterns in the given events
func (p *patternMatcher) FindPatterns(ctx context.Context, events []domain.Event, patterns []core.CorrelationPattern) ([]domain.Correlation, error) {
	var allCorrelations []domain.Correlation
	// Optimize patterns for better performance
	optimizedPatterns := p.OptimizePatterns(patterns)
	// Process each pattern
	for _, pattern := range optimizedPatterns {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		// Skip disabled patterns
		if !pattern.Enabled() {
			continue
		}
		// Find correlations for this pattern
		correlations, err := p.MatchPattern(ctx, events, pattern)
		if err != nil {
			// Log error but continue with other patterns
			continue
		}
		allCorrelations = append(allCorrelations, correlations...)
	}
	// Sort correlations by confidence
	sort.Slice(allCorrelations, func(i, j int) bool {
		return allCorrelations[i].Confidence.Overall > allCorrelations[j].Confidence.Overall
	})
	return allCorrelations, nil
}
// MatchPattern matches a specific pattern against events
func (p *patternMatcher) MatchPattern(ctx context.Context, events []domain.Event, pattern core.CorrelationPattern) ([]domain.Correlation, error) {
	// Filter events relevant to this pattern
	relevantEvents := p.FilterRelevantEvents(events, pattern)
	if len(relevantEvents) == 0 {
		return nil, nil
	}
	// Check if we have sufficient events
	if len(relevantEvents) < 2 {
		return nil, nil
	}
	// Apply pattern-specific matching with timeout
	matchCtx, cancel := context.WithTimeout(ctx, p.config.TimeWindow)
	defer cancel()
	correlations, err := pattern.Match(matchCtx, relevantEvents)
	if err != nil {
		return nil, fmt.Errorf("pattern match failed: %w", err)
	}
	// Post-process correlations
	var validCorrelations []domain.Correlation
	for _, correlation := range correlations {
		// Validate correlation meets minimum confidence
		if correlation.Confidence.Overall >= pattern.MinConfidence() {
			// Enrich correlation with pattern metadata
			enrichedCorrelation := p.enrichCorrelation(correlation, pattern)
			validCorrelations = append(validCorrelations, enrichedCorrelation)
		}
	}
	return validCorrelations, nil
}
// OptimizePatterns optimizes pattern order for better performance
func (p *patternMatcher) OptimizePatterns(patterns []core.CorrelationPattern) []core.CorrelationPattern {
	// Create a copy to avoid modifying the original slice
	optimized := make([]core.CorrelationPattern, len(patterns))
	copy(optimized, patterns)
	// Sort patterns by priority (higher priority first)
	sort.Slice(optimized, func(i, j int) bool {
		return optimized[i].Priority() > optimized[j].Priority()
	})
	// Further optimize by grouping patterns with similar requirements
	return p.groupSimilarPatterns(optimized)
}
// FilterRelevantEvents filters events relevant to a specific pattern
func (p *patternMatcher) FilterRelevantEvents(events []domain.Event, pattern core.CorrelationPattern) []domain.Event {
	var relevant []domain.Event
	requiredSources := pattern.RequiredSources()
	timeWindow := pattern.TimeWindow()
	// Get the latest event time for time window calculation
	var latestTime time.Time
	for _, event := range events {
		if event.Timestamp.After(latestTime) {
			latestTime = event.Timestamp
		}
	}
	windowStart := latestTime.Add(-timeWindow)
	for _, event := range events {
		// Check if event can match this pattern
		if !pattern.CanMatch(event) {
			continue
		}
		// Check if event source is required
		if len(requiredSources) > 0 {
			sourceRequired := false
			for _, source := range requiredSources {
				if event.Source == source {
					sourceRequired = true
					break
				}
			}
			if !sourceRequired {
				continue
			}
		}
		// Check if event is within time window
		if event.Timestamp.Before(windowStart) {
			continue
		}
		relevant = append(relevant, event)
	}
	// Sort relevant events by timestamp for pattern analysis
	sort.Slice(relevant, func(i, j int) bool {
		return relevant[i].Timestamp.Before(relevant[j].Timestamp)
	})
	// Limit to maximum events if specified
	maxEvents := pattern.MaxEvents()
	if maxEvents > 0 && len(relevant) > maxEvents {
		// Keep the most recent events
		relevant = relevant[len(relevant)-maxEvents:]
	}
	return relevant
}
// Helper methods
// enrichCorrelation enriches a correlation with pattern metadata
func (p *patternMatcher) enrichCorrelation(correlation domain.Correlation, pattern core.CorrelationPattern) domain.Correlation {
	// Add pattern information to correlation metadata
	if correlation.Metadata.Annotations == nil {
		correlation.Metadata.Annotations = make(map[string]string)
	}
	correlation.Metadata.Annotations["pattern_id"] = pattern.ID()
	correlation.Metadata.Annotations["pattern_name"] = pattern.Name()
	correlation.Metadata.Annotations["pattern_category"] = string(pattern.Category())
	correlation.Metadata.Annotations["pattern_priority"] = fmt.Sprintf("%d", pattern.Priority())
	// Add pattern tags to correlation
	for _, tag := range pattern.Tags() {
		correlation.Context.Tags = append(correlation.Context.Tags, tag)
	}
	// Enhance description with pattern information
	if correlation.Description == "" {
		correlation.Description = pattern.Description()
	} else {
		correlation.Description = fmt.Sprintf("%s (Pattern: %s)", correlation.Description, pattern.Name())
	}
	return correlation
}
// groupSimilarPatterns groups patterns with similar requirements for batch processing
func (p *patternMatcher) groupSimilarPatterns(patterns []core.CorrelationPattern) []core.CorrelationPattern {
	// For now, just return the sorted patterns
	// In a more sophisticated implementation, we could group patterns that:
	// - Require the same sources
	// - Have similar time windows
	// - Share common event types
	// This would allow for more efficient batch processing
	return patterns
}
// CorrelationAlgorithm implementation for pattern matcher
type patternMatchingAlgorithm struct {
	matcher core.PatternMatcher
	patterns []core.CorrelationPattern
}
// NewPatternMatchingAlgorithm creates a new pattern matching algorithm
func NewPatternMatchingAlgorithm(patterns []core.CorrelationPattern, config core.AlgorithmConfig) core.CorrelationAlgorithm {
	matcher := NewPatternMatcher(config)
	return &patternMatchingAlgorithm{
		matcher:  matcher,
		patterns: patterns,
	}
}
// Name returns the algorithm name
func (p *patternMatchingAlgorithm) Name() string {
	return "pattern_matching"
}
// Type returns the algorithm type
func (p *patternMatchingAlgorithm) Type() core.AlgorithmType {
	return core.AlgorithmTypePattern
}
// Correlate performs correlation using pattern matching
func (p *patternMatchingAlgorithm) Correlate(ctx context.Context, events []domain.Event, config core.AlgorithmConfig) ([]domain.Correlation, error) {
	return p.matcher.FindPatterns(ctx, events, p.patterns)
}
// ComputeConfidence computes confidence for a correlation
func (p *patternMatchingAlgorithm) ComputeConfidence(events []domain.Event, correlation domain.Correlation) float64 {
	// Base confidence from the correlation itself
	baseConfidence := correlation.Confidence.Overall
	// Adjust based on event quality
	eventQualityFactor := p.computeEventQualityFactor(events)
	// Adjust based on correlation completeness
	completenessFactor := p.computeCompletenessFactor(correlation)
	// Combine factors
	adjustedConfidence := baseConfidence * eventQualityFactor * completenessFactor
	// Ensure confidence is within valid range
	if adjustedConfidence > 1.0 {
		adjustedConfidence = 1.0
	}
	if adjustedConfidence < 0.0 {
		adjustedConfidence = 0.0
	}
	return adjustedConfidence
}
// SupportedSources returns supported event sources
func (p *patternMatchingAlgorithm) SupportedSources() []domain.Source {
	return []domain.Source{
		domain.SourceEBPF,
		domain.SourceKubernetes,
		domain.SourceSystemd,
		domain.SourceJournald,
	}
}
// SupportedEventTypes returns supported event types
func (p *patternMatchingAlgorithm) SupportedEventTypes() []domain.EventType {
	return []domain.EventType{
		domain.EventTypeMemory,
		domain.EventTypeNetwork,
		domain.EventTypeCPU,
		domain.EventTypeDisk,
		domain.EventTypeService,
		domain.EventTypeSystem,
		domain.EventTypeLog,
	}
}
// RequiredParameters returns required algorithm parameters
func (p *patternMatchingAlgorithm) RequiredParameters() []string {
	return []string{
		"time_window",
		"min_confidence",
		"max_events",
	}
}
// Helper methods for confidence computation
func (p *patternMatchingAlgorithm) computeEventQualityFactor(events []domain.Event) float64 {
	if len(events) == 0 {
		return 0.0
	}
	qualitySum := 0.0
	for _, event := range events {
		// Quality based on event confidence and completeness
		eventQuality := event.Confidence
		// Boost quality for events with rich context
		if p.hasRichContext(event) {
			eventQuality *= 1.1
		}
		// Boost quality for high-severity events
		if event.Severity >= domain.SeverityError {
			eventQuality *= 1.2
		}
		qualitySum += eventQuality
	}
	averageQuality := qualitySum / float64(len(events))
	// Normalize to reasonable range
	if averageQuality > 1.0 {
		return 1.0
	}
	return averageQuality
}
func (p *patternMatchingAlgorithm) computeCompletenessFactor(correlation domain.Correlation) float64 {
	completeness := 0.0
	// Check if correlation has description
	if correlation.Description != "" {
		completeness += 0.2
	}
	// Check if correlation has context
	if len(correlation.Context.Labels) > 0 {
		completeness += 0.2
	}
	// Check if correlation has multiple events
	if len(correlation.Events) > 1 {
		completeness += 0.3
	}
	// Check if correlation has findings
	if len(correlation.Findings) > 0 {
		completeness += 0.3
	}
	return completeness
}
func (p *patternMatchingAlgorithm) hasRichContext(event domain.Event) bool {
	// Consider context rich if it has multiple pieces of information
	contextPieces := 0
	if event.Context.Host != "" {
		contextPieces++
	}
	if event.Context.Container != "" {
		contextPieces++
	}
	if event.Context.PID != nil {
		contextPieces++
	}
	if len(event.Context.Labels) > 0 {
		contextPieces++
	}
	if len(event.Context.Tags) > 0 {
		contextPieces++
	}
	return contextPieces >= 3
}
// Statistical correlation algorithm
type statisticalAlgorithm struct {
	config core.AlgorithmConfig
}
// NewStatisticalAlgorithm creates a new statistical correlation algorithm
func NewStatisticalAlgorithm(config core.AlgorithmConfig) core.CorrelationAlgorithm {
	return &statisticalAlgorithm{
		config: config,
	}
}
// Name returns the algorithm name
func (s *statisticalAlgorithm) Name() string {
	return "statistical"
}
// Type returns the algorithm type
func (s *statisticalAlgorithm) Type() core.AlgorithmType {
	return core.AlgorithmTypeStatistical
}
// Correlate performs statistical correlation analysis
func (s *statisticalAlgorithm) Correlate(ctx context.Context, events []domain.Event, config core.AlgorithmConfig) ([]domain.Correlation, error) {
	if len(events) < 2 {
		return nil, core.ErrInsufficientData
	}
	var correlations []domain.Correlation
	// Group events by time windows
	windows := s.groupEventsByTimeWindows(events, config.TimeWindow)
	// Analyze correlations within each window
	for _, window := range windows {
		if len(window) < 2 {
			continue
		}
		// Find statistical correlations within the window
		windowCorrelations := s.findStatisticalCorrelations(window, config)
		correlations = append(correlations, windowCorrelations...)
	}
	return correlations, nil
}
// ComputeConfidence computes statistical confidence
func (s *statisticalAlgorithm) ComputeConfidence(events []domain.Event, correlation domain.Correlation) float64 {
	// Implement statistical confidence computation
	// This could include correlation coefficients, p-values, etc.
	// For now, use a simplified approach based on event frequency and timing
	return s.computeFrequencyBasedConfidence(events, correlation)
}
// SupportedSources returns supported sources for statistical analysis
func (s *statisticalAlgorithm) SupportedSources() []domain.Source {
	return []domain.Source{
		domain.SourceEBPF,
		domain.SourceKubernetes,
		domain.SourceSystemd,
		domain.SourceJournald,
	}
}
// SupportedEventTypes returns supported event types for statistical analysis
func (s *statisticalAlgorithm) SupportedEventTypes() []domain.EventType {
	return []domain.EventType{
		domain.EventTypeMemory,
		domain.EventTypeNetwork,
		domain.EventTypeCPU,
		domain.EventTypeDisk,
		domain.EventTypeService,
		domain.EventTypeSystem,
		domain.EventTypeLog,
	}
}
// RequiredParameters returns required parameters for statistical analysis
func (s *statisticalAlgorithm) RequiredParameters() []string {
	return []string{
		"time_window",
		"min_confidence",
		"correlation_threshold",
	}
}
// Helper methods for statistical algorithm
func (s *statisticalAlgorithm) groupEventsByTimeWindows(events []domain.Event, windowSize time.Duration) [][]domain.Event {
	if len(events) == 0 {
		return nil
	}
	// Sort events by timestamp
	sortedEvents := make([]domain.Event, len(events))
	copy(sortedEvents, events)
	sort.Slice(sortedEvents, func(i, j int) bool {
		return sortedEvents[i].Timestamp.Before(sortedEvents[j].Timestamp)
	})
	var windows [][]domain.Event
	var currentWindow []domain.Event
	var windowStart time.Time
	for _, event := range sortedEvents {
		if len(currentWindow) == 0 {
			windowStart = event.Timestamp
			currentWindow = []domain.Event{event}
			continue
		}
		if event.Timestamp.Sub(windowStart) <= windowSize {
			currentWindow = append(currentWindow, event)
		} else {
			windows = append(windows, currentWindow)
			windowStart = event.Timestamp
			currentWindow = []domain.Event{event}
		}
	}
	if len(currentWindow) > 0 {
		windows = append(windows, currentWindow)
	}
	return windows
}
func (s *statisticalAlgorithm) findStatisticalCorrelations(events []domain.Event, config core.AlgorithmConfig) []domain.Correlation {
	var correlations []domain.Correlation
	// Analyze event co-occurrence patterns
	coOccurrenceMatrix := s.buildCoOccurrenceMatrix(events)
	// Find significant correlations based on co-occurrence
	for sourceType1, targets := range coOccurrenceMatrix {
		for sourceType2, count := range targets {
			if sourceType1 == sourceType2 {
				continue
			}
			// Compute correlation strength
			strength := s.computeCoOccurrenceStrength(sourceType1, sourceType2, count, len(events))
			if strength >= config.MinConfidence {
				correlation := s.createStatisticalCorrelation(sourceType1, sourceType2, strength, events)
				correlations = append(correlations, correlation)
			}
		}
	}
	return correlations
}
func (s *statisticalAlgorithm) buildCoOccurrenceMatrix(events []domain.Event) map[string]map[string]int {
	matrix := make(map[string]map[string]int)
	// Group events by source and type
	for i, event1 := range events {
		key1 := fmt.Sprintf("%s_%s", event1.Source, event1.Type)
		if matrix[key1] == nil {
			matrix[key1] = make(map[string]int)
		}
		for j, event2 := range events {
			if i == j {
				continue
			}
			key2 := fmt.Sprintf("%s_%s", event2.Source, event2.Type)
			// Check if events co-occur (within reasonable time window)
			timeDiff := event1.Timestamp.Sub(event2.Timestamp)
			if timeDiff < 0 {
				timeDiff = -timeDiff
			}
			if timeDiff <= 5*time.Minute { // Co-occurrence window
				matrix[key1][key2]++
			}
		}
	}
	return matrix
}
func (s *statisticalAlgorithm) computeCoOccurrenceStrength(type1, type2 string, coOccurrences, totalEvents int) float64 {
	if totalEvents == 0 {
		return 0.0
	}
	// Simple frequency-based strength
	frequency := float64(coOccurrences) / float64(totalEvents)
	// Apply some normalization and thresholding
	strength := frequency * 2.0 // Amplify signal
	if strength > 1.0 {
		strength = 1.0
	}
	return strength
}
func (s *statisticalAlgorithm) createStatisticalCorrelation(type1, type2 string, strength float64, events []domain.Event) domain.Correlation {
	// Find representative events of each type
	var events1, events2 []domain.EventID
	for _, event := range events {
		eventType := fmt.Sprintf("%s_%s", event.Source, event.Type)
		if eventType == type1 {
			events1 = append(events1, event.ID)
		}
		if eventType == type2 {
			events2 = append(events2, event.ID)
		}
	}
	allEventIDs := append(events1, events2...)
	// Convert to EventReferences
	eventRefs := make([]domain.EventReference, len(allEventIDs))
	for i, eventID := range allEventIDs {
		eventRefs[i] = domain.EventReference{
			EventID:      eventID,
			Role:         "participant",
			Relationship: "statistical",
			Weight:       1.0,
		}
	}
	// Convert strength to ConfidenceScore
	confidenceScore := domain.ConfidenceScore{
		Overall:     strength,
		Temporal:    strength,
		Causal:      strength * 0.5, // Lower causal confidence for statistical correlations
		Pattern:     strength,
		Statistical: strength,
	}
	correlation := domain.Correlation{
		ID:          domain.CorrelationID(fmt.Sprintf("stat_corr_%s_%s", type1, type2)),
		Type:        domain.CorrelationTypeStatistical,
		Events:      eventRefs,
		Confidence:  confidenceScore,
		Description: fmt.Sprintf("Statistical correlation between %s and %s", type1, type2),
		Timestamp:   time.Now(),
		Metadata: domain.CorrelationMetadata{
			SchemaVersion: "1.0",
			ProcessedAt:   time.Now(),
			ProcessedBy:   "statistical_algorithm",
			Annotations: map[string]string{
				"algorithm":     "statistical",
				"type1":         type1,
				"type2":         type2,
				"strength":      fmt.Sprintf("%.3f", strength),
				"method":        "co_occurrence",
			},
		},
	}
	return correlation
}
func (s *statisticalAlgorithm) computeFrequencyBasedConfidence(events []domain.Event, correlation domain.Correlation) float64 {
	// Count how many events in the correlation vs total events
	correlationEventCount := len(correlation.Events)
	totalEventCount := len(events)
	if totalEventCount == 0 {
		return 0.0
	}
	// Base confidence on the ratio, but apply some normalization
	ratio := float64(correlationEventCount) / float64(totalEventCount)
	// Apply logarithmic scaling to prevent very small correlations from dominating
	confidence := ratio * 2.0 // Amplify
	if confidence > 1.0 {
		confidence = 1.0
	}
	return confidence
}