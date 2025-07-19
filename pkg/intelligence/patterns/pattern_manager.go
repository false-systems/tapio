package patternrecognition

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// Manager implements the PatternRecognitionEngine interface
type Manager struct {
	config   *Config
	patterns map[string]Pattern
	stats    *statsCollector
	mu       sync.RWMutex
}

// NewManager creates a new pattern recognition manager
func NewManager(config *Config) *Manager {
	if config == nil {
		config = DefaultConfig()
	}

	m := &Manager{
		config:   config,
		patterns: make(map[string]Pattern),
		stats:    newStatsCollector(),
	}

	// Register default patterns if enabled
	m.registerDefaultPatterns()

	return m
}

// DetectPatterns analyzes events and returns detected patterns with correlations
func (m *Manager) DetectPatterns(ctx context.Context, events []domain.Event) ([]PatternMatch, error) {
	if len(events) == 0 {
		return nil, nil
	}

	// Filter events within the configured time window
	windowedEvents := m.filterEventsByTimeWindow(events)
	if len(windowedEvents) == 0 {
		return nil, nil
	}

	// Get enabled patterns
	patterns := m.getEnabledPatterns()
	if len(patterns) == 0 {
		return nil, fmt.Errorf("no patterns enabled")
	}

	// Detect patterns concurrently
	matches := make([]PatternMatch, 0)
	matchChan := make(chan PatternMatch, len(patterns))
	errChan := make(chan error, len(patterns))

	var wg sync.WaitGroup

	for _, pattern := range patterns {
		wg.Add(1)
		go func(p Pattern) {
			defer wg.Done()

			// Create pattern-specific context with timeout
			patternCtx, cancel := context.WithTimeout(ctx, m.config.PatternMatchTimeout)
			defer cancel()

			// Filter relevant events for this pattern
			relevantEvents := m.filterRelevantEvents(windowedEvents, p)
			if len(relevantEvents) == 0 {
				return
			}

			// Track timing
			start := time.Now()

			// Match pattern
			correlations, err := p.Match(patternCtx, relevantEvents)
			if err != nil {
				errChan <- fmt.Errorf("pattern %s match error: %w", p.ID(), err)
				m.stats.recordError(p.ID())
				return
			}

			// Record statistics
			duration := time.Since(start)
			m.stats.recordMatch(p.ID(), len(correlations), duration)

			// Convert correlations to pattern matches
			for _, correlation := range correlations {
				if correlation.Confidence.Overall >= m.config.MinConfidenceScore {
					match := PatternMatch{
						Pattern: PatternInfo{
							ID:          p.ID(),
							Name:        p.Name(),
							Description: p.Description(),
							Category:    p.Category(),
							Priority:    p.Priority(),
							Enabled:     p.Enabled(),
						},
						Correlation: correlation,
						Confidence:  correlation.Confidence.Overall,
						Events:      correlation.Events,
						Detected:    time.Now(),
					}
					matchChan <- match
				}
			}
		}(pattern)
	}

	// Wait for all patterns to complete
	go func() {
		wg.Wait()
		close(matchChan)
		close(errChan)
	}()

	// Collect results
	for match := range matchChan {
		matches = append(matches, match)
	}

	// Check for errors
	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		// Log errors but don't fail if some patterns succeeded
		// In production, these would be logged properly
	}

	return matches, nil
}

// RegisterPattern adds a new pattern to the recognition engine
func (m *Manager) RegisterPattern(pattern Pattern) error {
	if pattern == nil {
		return fmt.Errorf("pattern cannot be nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.patterns[pattern.ID()]; exists {
		return fmt.Errorf("pattern with ID %s already registered", pattern.ID())
	}

	m.patterns[pattern.ID()] = pattern
	m.stats.initPattern(pattern.ID())

	return nil
}

// UnregisterPattern removes a pattern from the engine
func (m *Manager) UnregisterPattern(patternID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.patterns[patternID]; !exists {
		return fmt.Errorf("pattern with ID %s not found", patternID)
	}

	delete(m.patterns, patternID)
	return nil
}

// GetSupportedPatterns returns all registered patterns
func (m *Manager) GetSupportedPatterns() []PatternInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	patterns := make([]PatternInfo, 0, len(m.patterns))

	for _, pattern := range m.patterns {
		info := PatternInfo{
			ID:          pattern.ID(),
			Name:        pattern.Name(),
			Description: pattern.Description(),
			Category:    pattern.Category(),
			Priority:    pattern.Priority(),
			Tags:        pattern.GetMetadata().Tags,
			Enabled:     pattern.Enabled(),
		}
		patterns = append(patterns, info)
	}

	return patterns
}

// GetPatternStats returns statistics about pattern matching
func (m *Manager) GetPatternStats() PatternStats {
	return m.stats.getStats()
}

// Configure updates the engine configuration
func (m *Manager) Configure(config *Config) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.config = config

	// Re-register default patterns if configuration changed
	if len(m.patterns) == 0 {
		m.registerDefaultPatterns()
	}

	return nil
}

// Private methods

func (m *Manager) registerDefaultPatterns() {
	// Register built-in patterns
	defaultPatterns := []Pattern{
		NewMemoryLeakPattern(),
		// Add more patterns as they are implemented:
		// NewCascadeFailurePattern(),
		// NewNetworkFailurePattern(),
		// NewOOMPredictionPattern(),
	}

	for _, pattern := range defaultPatterns {
		// Only register if enabled in config
		for _, enabledID := range m.config.EnabledPatterns {
			if pattern.ID() == enabledID {
				_ = m.RegisterPattern(pattern)
				break
			}
		}
	}
}

func (m *Manager) getEnabledPatterns() []Pattern {
	m.mu.RLock()
	defer m.mu.RUnlock()

	patterns := make([]Pattern, 0)

	for _, pattern := range m.patterns {
		if pattern.Enabled() {
			patterns = append(patterns, pattern)
		}
	}

	return patterns
}

func (m *Manager) filterEventsByTimeWindow(events []domain.Event) []domain.Event {
	if len(events) == 0 {
		return events
	}

	// Find the latest event
	latest := events[0].Timestamp
	for _, event := range events {
		if event.Timestamp.After(latest) {
			latest = event.Timestamp
		}
	}

	// Filter events within default time window
	cutoff := latest.Add(-m.config.DefaultTimeWindow)
	filtered := make([]domain.Event, 0)

	for _, event := range events {
		if event.Timestamp.After(cutoff) {
			filtered = append(filtered, event)
		}
	}

	return filtered
}

func (m *Manager) filterRelevantEvents(events []domain.Event, pattern Pattern) []domain.Event {
	// Limit events per pattern
	maxEvents := m.config.MaxEventsPerPattern

	// First pass: filter by CanMatch
	relevant := make([]domain.Event, 0)
	for _, event := range events {
		if pattern.CanMatch(event) {
			relevant = append(relevant, event)
			if len(relevant) >= maxEvents {
				break
			}
		}
	}

	return relevant
}

// statsCollector collects pattern matching statistics
type statsCollector struct {
	stats map[string]*patternStats
	mu    sync.RWMutex
}

type patternStats struct {
	totalMatches    int64
	totalAttempts   int64
	totalErrors     int64
	totalTime       time.Duration
	lastMatchTime   time.Time
	confidenceSum   float64
	confidenceCount int64
}

func newStatsCollector() *statsCollector {
	return &statsCollector{
		stats: make(map[string]*patternStats),
	}
}

func (sc *statsCollector) initPattern(patternID string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if _, exists := sc.stats[patternID]; !exists {
		sc.stats[patternID] = &patternStats{}
	}
}

func (sc *statsCollector) recordMatch(patternID string, matches int, duration time.Duration) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	stats, exists := sc.stats[patternID]
	if !exists {
		stats = &patternStats{}
		sc.stats[patternID] = stats
	}

	stats.totalAttempts++
	if matches > 0 {
		stats.totalMatches += int64(matches)
		stats.lastMatchTime = time.Now()
	}
	stats.totalTime += duration
}

func (sc *statsCollector) recordError(patternID string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if stats, exists := sc.stats[patternID]; exists {
		stats.totalErrors++
	}
}

func (sc *statsCollector) getStats() PatternStats {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	result := PatternStats{
		TotalMatches:      make(map[string]int64),
		MatchRate:         make(map[string]float64),
		AverageConfidence: make(map[string]float64),
		LastMatchTime:     make(map[string]time.Time),
		ProcessingTime:    make(map[string]time.Duration),
	}

	for patternID, stats := range sc.stats {
		result.TotalMatches[patternID] = stats.totalMatches

		if stats.totalAttempts > 0 {
			result.MatchRate[patternID] = float64(stats.totalMatches) / float64(stats.totalAttempts)
			result.ProcessingTime[patternID] = stats.totalTime / time.Duration(stats.totalAttempts)
		}

		if stats.confidenceCount > 0 {
			result.AverageConfidence[patternID] = stats.confidenceSum / float64(stats.confidenceCount)
		}

		result.LastMatchTime[patternID] = stats.lastMatchTime
	}

	return result
}

// Engine returns a new pattern recognition engine
func Engine(config *Config) PatternRecognitionEngine {
	return NewManager(config)
}
