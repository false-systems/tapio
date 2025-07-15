package correlation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation/foundation"
	"github.com/yairfalse/tapio/pkg/correlation/types"
)

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

// Type aliases for backward compatibility
type Event = types.Event
type Severity = types.Severity
type Filter = foundation.Filter
type TimeWindow = foundation.TimeWindow

// Severity constants for backward compatibility
const (
	SeverityCritical = types.SeverityCritical
	SeverityHigh     = types.SeverityHigh
	SeverityMedium   = types.SeverityMedium
	SeverityLow      = types.SeverityLow
)

// EngineStats provides statistics about the correlation engine
type EngineStats struct {
	EventsProcessed    uint64
	CorrelationsFound  uint64
	InsightsGenerated  uint64
	ActiveRules        int
	ActiveCorrelations int
	ProcessingRate     float64
}

// Evidence represents evidence supporting an insight
type Evidence struct {
	EventID     string
	Description string
	Timestamp   time.Time
	Source      string
}

// RootCause represents identified root cause
type RootCause struct {
	EventID     string
	Description string
	Confidence  float64
}

// ActionableItem represents a recommended action
type ActionableItem struct {
	Description string
	Command     string
	Impact      string
	Risk        string
}

// Prediction represents a future event prediction
type Prediction struct {
	Type        string
	Probability float64
	Confidence  float64
	TimeToEvent time.Duration
	Description string
}

// Insight represents a correlated insight from multiple events
type Insight struct {
	ID              string
	Title           string
	Description     string
	Severity        string
	Category        string
	ResourceName    string
	Namespace       string
	Timestamp       time.Time
	Evidence        []*Evidence
	RootCause       *RootCause
	Prediction      *Prediction
	ActionableItems []*ActionableItem
}

// InsightStore defines the interface for storing and retrieving insights
type InsightStore interface {
	// Store stores an insight
	Store(insight *Insight) error

	// Get retrieves an insight by ID
	Get(id string) (*Insight, error)

	// GetInsights retrieves insights for a specific resource
	GetInsights(resourceName, namespace string) []*Insight

	// GetAllInsights retrieves all insights
	GetAllInsights() []*Insight

	// Delete removes an insight by ID
	Delete(id string) error

	// DeleteOlderThan removes insights older than the specified time
	DeleteOlderThan(cutoff time.Time) error
}

// InMemoryInsightStore implements InsightStore using memory storage
type InMemoryInsightStore struct {
	insights map[string]*Insight
	mu       sync.RWMutex
}

// NewInMemoryInsightStore creates a new in-memory insight store
func NewInMemoryInsightStore() *InMemoryInsightStore {
	return &InMemoryInsightStore{
		insights: make(map[string]*Insight),
	}
}

// Store stores an insight
func (s *InMemoryInsightStore) Store(insight *Insight) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.insights[insight.ID] = insight
	return nil
}

// Get retrieves an insight by ID
func (s *InMemoryInsightStore) Get(id string) (*Insight, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	insight, exists := s.insights[id]
	if !exists {
		return nil, fmt.Errorf("insight not found: %s", id)
	}

	return insight, nil
}

// GetInsights retrieves insights for a specific resource
func (s *InMemoryInsightStore) GetInsights(resourceName, namespace string) []*Insight {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*Insight
	for _, insight := range s.insights {
		if insight.ResourceName == resourceName && insight.Namespace == namespace {
			results = append(results, insight)
		}
	}

	return results
}

// GetAllInsights retrieves all insights
func (s *InMemoryInsightStore) GetAllInsights() []*Insight {
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]*Insight, 0, len(s.insights))
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
