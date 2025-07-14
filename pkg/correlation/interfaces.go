package correlation

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// CorrelationEngine defines the interface for correlation processing
type CorrelationEngine interface {
	// ProcessEvents processes a batch of events through the correlation engine
	ProcessEvents(ctx context.Context, events []*Event) error
	
	// Start starts the correlation engine
	Start(ctx context.Context) error
	
	// Stop stops the correlation engine
	Stop() error
	
	// GetStats returns engine statistics
	GetStats() *EngineStats
}

// Event represents a generic event for correlation
type Event struct {
	ID        string
	Timestamp time.Time
	Source    string
	Type      string
	Severity  Severity
	Data      map[string]interface{}
}

// Severity levels for events
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
)

// EngineStats provides statistics about the correlation engine
type EngineStats struct {
	EventsProcessed     uint64
	CorrelationsFound   uint64
	InsightsGenerated   uint64
	ActiveRules         int
	ActiveCorrelations  int
	ProcessingRate      float64
}

// Correlation represents a correlation between events
type Correlation struct {
	ID          string
	Type        string
	Events      []string
	Confidence  float64
	Description string
	Timestamp   time.Time
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