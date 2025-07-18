package correlation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation/foundation"
	"github.com/yairfalse/tapio/pkg/correlation/types"
	"github.com/yairfalse/tapio/pkg/domain"
)

// ============================================================================
// INTERFACE ALIASES FOR BACKWARD COMPATIBILITY
// ============================================================================

// Core engine interfaces
type Engine = foundation.Engine
type Rule = foundation.Rule

// Data source interfaces
type EventStore = foundation.EventStore
type DataSource = foundation.DataSource
type DataHandler = foundation.DataHandler

// Pattern detection interfaces
type PatternDetector = foundation.PatternDetector
type PatternRegistry = foundation.PatternRegistry

// AutoFix interfaces
type AutoFixEngine = foundation.AutoFixEngine

// Performance interfaces
type RulePerformance = foundation.RulePerformance
type RuleExecution = foundation.RuleExecution

// EngineStats contains statistics about the correlation engine
type EngineStats struct {
	EventsProcessed    uint64
	CorrelationsFound  uint64
	InsightsGenerated  uint64
	ErrorCount         uint64
	LastProcessedTime  time.Time
}

// CorrelationEngine defines the interface for correlation processing
type CorrelationEngine interface {
	// ProcessEvents processes a batch of events through the correlation engine
	ProcessEvents(ctx context.Context, events []*types.Event) error

	// Start starts the correlation engine
	Start(ctx context.Context) error

	// Stop stops the correlation engine
	Stop() error

	// GetStats returns engine statistics
	GetStats() *EngineStats
}


// InsightGenerator generates actionable insights
type InsightGenerator interface {
	// GenerateInsights creates insights from correlations and patterns
	GenerateInsights(ctx context.Context, correlations []domain.Correlation, patterns []domain.Pattern) ([]domain.Insight, error)
}

// CorrelationStore persists correlation data
type CorrelationStore interface {
	// StoreCorrelation persists a correlation
	StoreCorrelation(ctx context.Context, correlation domain.Correlation) error
	// GetCorrelation retrieves a correlation by ID
	GetCorrelation(ctx context.Context, id string) (*domain.Correlation, error)
	// QueryCorrelations queries correlations
	QueryCorrelations(ctx context.Context, window domain.TimeWindow, types []string) ([]domain.Correlation, error)
	// DeleteCorrelation removes a correlation
	DeleteCorrelation(ctx context.Context, id string) error
}

// RuleRepository manages correlation rules
type RuleRepository interface {
	// Save persists a rule
	Save(ctx context.Context, rule domain.Rule) error
	// Get retrieves a rule by ID
	Get(ctx context.Context, id string) (*domain.Rule, error)
	// List lists all rules
	List(ctx context.Context) ([]domain.Rule, error)
	// Delete removes a rule
	Delete(ctx context.Context, id string) error
}

// CorrelationResult represents the result of correlation analysis
type CorrelationResult struct {
	Correlations []domain.Correlation `json:"correlations"`
	Insights     []domain.Insight     `json:"insights"`
	Patterns     []domain.Pattern     `json:"patterns"`
	ProcessingTime time.Duration      `json:"processing_time"`
	EventsAnalyzed int               `json:"events_analyzed"`
}

// AnalysisOptions configures correlation analysis
type AnalysisOptions struct {
	TimeWindow      domain.TimeWindow
	EventTypes      []string
	MinConfidence   float64
	MaxCorrelations int
	IncludePatterns bool
	IncludeInsights bool
}

// InsightStore defines the interface for storing and retrieving insights
type InsightStore interface {
	// Store stores an insight
	Store(insight *domain.Insight) error

	// Get retrieves an insight by ID
	Get(id string) (*domain.Insight, error)

	// GetInsights retrieves insights for a specific resource
	GetInsights(resourceName, namespace string) []*domain.Insight

	// GetAllInsights retrieves all insights
	GetAllInsights() []*domain.Insight

	// Delete removes an insight by ID
	Delete(id string) error

	// DeleteOlderThan removes insights older than the specified time
	DeleteOlderThan(cutoff time.Time) error
}

// InMemoryInsightStore implements InsightStore using memory storage
type InMemoryInsightStore struct {
	insights map[string]*domain.Insight
	mu       sync.RWMutex
}

// NewInMemoryInsightStore creates a new in-memory insight store
func NewInMemoryInsightStore() *InMemoryInsightStore {
	return &InMemoryInsightStore{
		insights: make(map[string]*domain.Insight),
	}
}

// Store stores an insight
func (s *InMemoryInsightStore) Store(insight *domain.Insight) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.insights[insight.ID] = insight
	return nil
}

// Get retrieves an insight by ID
func (s *InMemoryInsightStore) Get(id string) (*domain.Insight, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	insight, exists := s.insights[id]
	if !exists {
		return nil, fmt.Errorf("insight not found: %s", id)
	}

	return insight, nil
}

// GetInsights retrieves insights for a specific resource
func (s *InMemoryInsightStore) GetInsights(resourceName, namespace string) []*domain.Insight {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*domain.Insight
	for _, insight := range s.insights {
		if insight.ResourceName == resourceName && insight.Namespace == namespace {
			results = append(results, insight)
		}
	}

	return results
}

// GetAllInsights retrieves all insights
func (s *InMemoryInsightStore) GetAllInsights() []*domain.Insight {
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]*domain.Insight, 0, len(s.insights))
	for _, insight := range s.insights {
		results = append(results, insight)
	}

	return results
}

// Delete removes an insight by ID
func (s *InMemoryInsightStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.insights, id)
	return nil
}

// DeleteOlderThan removes insights older than the specified time
func (s *InMemoryInsightStore) DeleteOlderThan(cutoff time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, insight := range s.insights {
		if insight.Timestamp.Before(cutoff) {
			delete(s.insights, id)
		}
	}

	return nil
}