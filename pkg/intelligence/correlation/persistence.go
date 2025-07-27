package correlation

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// CorrelationStore defines the interface for correlation persistence
type CorrelationStore interface {
	// Store a discovered correlation
	StoreCorrelation(ctx context.Context, correlation *StoredCorrelation) error

	// Query correlations by various criteria
	GetCorrelationsByType(ctx context.Context, correlationType string, limit int) ([]*StoredCorrelation, error)
	GetCorrelationsByTimeRange(ctx context.Context, start, end time.Time) ([]*StoredCorrelation, error)
	GetCorrelationsByPattern(ctx context.Context, pattern string) ([]*StoredCorrelation, error)
	GetCorrelationsByConfidence(ctx context.Context, minConfidence float64) ([]*StoredCorrelation, error)

	// Get correlation statistics
	GetCorrelationStats(ctx context.Context) (*CorrelationStatistics, error)

	// Learning and adaptation
	UpdateCorrelationFeedback(ctx context.Context, correlationID string, feedback CorrelationFeedback) error
	GetCorrelationPatterns(ctx context.Context) ([]*LearnedPattern, error)

	// Cleanup and maintenance
	CleanupOldCorrelations(ctx context.Context, olderThan time.Duration) error
	GetStorageStats(ctx context.Context) (*StorageStatistics, error)
}

// StoredCorrelation represents a correlation that has been persisted
type StoredCorrelation struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Timestamp   time.Time              `json:"timestamp"`
	Source      *domain.UnifiedEvent   `json:"source_event"`
	Target      *domain.UnifiedEvent   `json:"target_event,omitempty"`
	Events      []*domain.UnifiedEvent `json:"events,omitempty"` // For multi-event correlations
	Confidence  float64                `json:"confidence"`
	Explanation CorrelationExplanation `json:"explanation"`
	Metadata    map[string]interface{} `json:"metadata"`

	// Learning and feedback
	UserFeedback      []CorrelationFeedback `json:"user_feedback,omitempty"`
	ConfirmationCount int                   `json:"confirmation_count"`
	RejectionCount    int                   `json:"rejection_count"`
	LastSeen          time.Time             `json:"last_seen"`
	Frequency         int                   `json:"frequency"`

	// Context for learning
	ClusterContext   string   `json:"cluster_context,omitempty"`
	NamespaceContext string   `json:"namespace_context,omitempty"`
	ServiceContext   string   `json:"service_context,omitempty"`
	Tags             []string `json:"tags,omitempty"`
}

// CorrelationFeedback represents user feedback on a correlation
type CorrelationFeedback struct {
	UserID     string    `json:"user_id,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
	IsCorrect  bool      `json:"is_correct"`
	Confidence float64   `json:"confidence"` // User's confidence in their feedback
	Comments   string    `json:"comments,omitempty"`
	Source     string    `json:"source"` // "explicit", "implicit", "automated"
}

// LearnedPattern represents a pattern discovered through correlation analysis
type LearnedPattern struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Pattern     string                 `json:"pattern"`
	Description string                 `json:"description"`
	Confidence  float64                `json:"confidence"`
	Frequency   int                    `json:"frequency"`
	FirstSeen   time.Time              `json:"first_seen"`
	LastSeen    time.Time              `json:"last_seen"`
	Examples    []*StoredCorrelation   `json:"examples"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// CorrelationStatistics provides insights into correlation discovery
type CorrelationStatistics struct {
	TotalCorrelations     int                    `json:"total_correlations"`
	CorrelationsByType    map[string]int         `json:"correlations_by_type"`
	CorrelationsByHour    map[int]int            `json:"correlations_by_hour"`
	AverageConfidence     float64                `json:"average_confidence"`
	HighConfidenceCount   int                    `json:"high_confidence_count"`   // >0.8
	MediumConfidenceCount int                    `json:"medium_confidence_count"` // 0.5-0.8
	LowConfidenceCount    int                    `json:"low_confidence_count"`    // <0.5
	TopPatterns           []*LearnedPattern      `json:"top_patterns"`
	TrendData             []CorrelationTrend     `json:"trend_data"`
	UserFeedbackStats     UserFeedbackStatistics `json:"user_feedback_stats"`
}

// CorrelationTrend represents correlation discovery trends over time
type CorrelationTrend struct {
	Timestamp         time.Time `json:"timestamp"`
	CorrelationRate   float64   `json:"correlation_rate"` // correlations per hour
	AverageConfidence float64   `json:"average_confidence"`
	PatternDiversity  int       `json:"pattern_diversity"` // unique patterns seen
}

// UserFeedbackStatistics tracks feedback quality
type UserFeedbackStatistics struct {
	TotalFeedback      int           `json:"total_feedback"`
	PositiveFeedback   int           `json:"positive_feedback"`
	NegativeFeedback   int           `json:"negative_feedback"`
	AverageFeedbackLag time.Duration `json:"average_feedback_lag"` // time from correlation to feedback
	TopContributors    []string      `json:"top_contributors"`
}

// StorageStatistics provides storage insights
type StorageStatistics struct {
	TotalRecords      int           `json:"total_records"`
	StorageSize       int64         `json:"storage_size_bytes"`
	OldestRecord      time.Time     `json:"oldest_record"`
	NewestRecord      time.Time     `json:"newest_record"`
	AverageRecordSize int           `json:"average_record_size"`
	CompressionRatio  float64       `json:"compression_ratio"`
	QueryPerformance  time.Duration `json:"average_query_time"`
}

// InMemoryCorrelationStore provides a simple in-memory implementation
type InMemoryCorrelationStore struct {
	correlations     map[string]*StoredCorrelation
	patterns         map[string]*LearnedPattern
	stats            *CorrelationStatistics
	correlationCount int
	mu               sync.RWMutex
	logger           *zap.Logger
}

// NewInMemoryCorrelationStore creates a new in-memory correlation store
func NewInMemoryCorrelationStore(logger *zap.Logger) *InMemoryCorrelationStore {
	return &InMemoryCorrelationStore{
		correlations: make(map[string]*StoredCorrelation),
		patterns:     make(map[string]*LearnedPattern),
		stats: &CorrelationStatistics{
			CorrelationsByType: make(map[string]int),
			CorrelationsByHour: make(map[int]int),
			TopPatterns:        make([]*LearnedPattern, 0),
			TrendData:          make([]CorrelationTrend, 0),
		},
		logger: logger,
	}
}

// StoreCorrelation stores a correlation in memory
func (s *InMemoryCorrelationStore) StoreCorrelation(ctx context.Context, correlation *StoredCorrelation) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate ID if not provided
	if correlation.ID == "" {
		correlation.ID = fmt.Sprintf("corr-%d-%s", time.Now().UnixNano(), correlation.Type)
	}

	// Set timestamp if not provided
	if correlation.Timestamp.IsZero() {
		correlation.Timestamp = time.Now()
	}

	// Store the correlation
	s.correlations[correlation.ID] = correlation
	s.correlationCount++

	// Update statistics
	s.updateStatistics(correlation)

	// Learn patterns
	s.learnPattern(correlation)

	// Only log every 100th correlation to reduce test log spam
	if s.correlationCount%100 == 1 {
		s.logger.Debug("Stored correlations",
			zap.Int("total_count", s.correlationCount),
			zap.String("latest_type", correlation.Type),
			zap.Float64("latest_confidence", correlation.Confidence))
	}

	return nil
}

// GetCorrelationsByType retrieves correlations by type
func (s *InMemoryCorrelationStore) GetCorrelationsByType(ctx context.Context, correlationType string, limit int) ([]*StoredCorrelation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*StoredCorrelation
	count := 0

	for _, corr := range s.correlations {
		if correlationType == "" || corr.Type == correlationType {
			results = append(results, corr)
			count++
			if limit > 0 && count >= limit {
				break
			}
		}
	}

	return results, nil
}

// GetCorrelationsByTimeRange retrieves correlations within a time range
func (s *InMemoryCorrelationStore) GetCorrelationsByTimeRange(ctx context.Context, start, end time.Time) ([]*StoredCorrelation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*StoredCorrelation

	for _, corr := range s.correlations {
		if corr.Timestamp.After(start) && corr.Timestamp.Before(end) {
			results = append(results, corr)
		}
	}

	return results, nil
}

// GetCorrelationsByPattern retrieves correlations matching a pattern
func (s *InMemoryCorrelationStore) GetCorrelationsByPattern(ctx context.Context, pattern string) ([]*StoredCorrelation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*StoredCorrelation

	for _, corr := range s.correlations {
		// Simple pattern matching - could be enhanced with regex
		if pattern == "" ||
			fmt.Sprintf("%s", corr.Explanation.Summary) == pattern ||
			corr.Type == pattern {
			results = append(results, corr)
		}
	}

	return results, nil
}

// GetCorrelationsByConfidence retrieves correlations above minimum confidence
func (s *InMemoryCorrelationStore) GetCorrelationsByConfidence(ctx context.Context, minConfidence float64) ([]*StoredCorrelation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*StoredCorrelation

	for _, corr := range s.correlations {
		if corr.Confidence >= minConfidence {
			results = append(results, corr)
		}
	}

	return results, nil
}

// GetCorrelationStats returns correlation statistics
func (s *InMemoryCorrelationStore) GetCorrelationStats(ctx context.Context) (*CorrelationStatistics, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Return a copy of current stats
	stats := *s.stats
	stats.TotalCorrelations = len(s.correlations)

	// Calculate confidence distribution
	highConf, medConf, lowConf := 0, 0, 0
	totalConf := 0.0

	for _, corr := range s.correlations {
		totalConf += corr.Confidence
		switch {
		case corr.Confidence > 0.8:
			highConf++
		case corr.Confidence > 0.5:
			medConf++
		default:
			lowConf++
		}
	}

	if len(s.correlations) > 0 {
		stats.AverageConfidence = totalConf / float64(len(s.correlations))
	}
	stats.HighConfidenceCount = highConf
	stats.MediumConfidenceCount = medConf
	stats.LowConfidenceCount = lowConf

	// Add top patterns
	var topPatterns []*LearnedPattern
	for _, pattern := range s.patterns {
		topPatterns = append(topPatterns, pattern)
	}
	stats.TopPatterns = topPatterns

	return &stats, nil
}

// UpdateCorrelationFeedback updates feedback for a correlation
func (s *InMemoryCorrelationStore) UpdateCorrelationFeedback(ctx context.Context, correlationID string, feedback CorrelationFeedback) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	corr, exists := s.correlations[correlationID]
	if !exists {
		return fmt.Errorf("correlation not found: %s", correlationID)
	}

	// Add feedback
	corr.UserFeedback = append(corr.UserFeedback, feedback)

	// Update counters
	if feedback.IsCorrect {
		corr.ConfirmationCount++
	} else {
		corr.RejectionCount++
	}

	s.logger.Info("Updated correlation feedback",
		zap.String("correlation_id", correlationID),
		zap.Bool("is_correct", feedback.IsCorrect))

	return nil
}

// GetCorrelationPatterns returns learned patterns
func (s *InMemoryCorrelationStore) GetCorrelationPatterns(ctx context.Context) ([]*LearnedPattern, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var patterns []*LearnedPattern
	for _, pattern := range s.patterns {
		patterns = append(patterns, pattern)
	}

	return patterns, nil
}

// CleanupOldCorrelations removes correlations older than specified duration
func (s *InMemoryCorrelationStore) CleanupOldCorrelations(ctx context.Context, olderThan time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)
	removed := 0

	for id, corr := range s.correlations {
		if corr.Timestamp.Before(cutoff) {
			delete(s.correlations, id)
			removed++
		}
	}

	s.logger.Info("Cleaned up old correlations",
		zap.Int("removed_count", removed),
		zap.Duration("older_than", olderThan))

	return nil
}

// GetStorageStats returns storage statistics
func (s *InMemoryCorrelationStore) GetStorageStats(ctx context.Context) (*StorageStatistics, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := &StorageStatistics{
		TotalRecords: len(s.correlations),
	}

	// Calculate storage size (approximate)
	totalSize := int64(0)
	var oldest, newest time.Time

	for _, corr := range s.correlations {
		// Rough estimation of record size
		data, _ := json.Marshal(corr)
		totalSize += int64(len(data))

		if oldest.IsZero() || corr.Timestamp.Before(oldest) {
			oldest = corr.Timestamp
		}
		if newest.IsZero() || corr.Timestamp.After(newest) {
			newest = corr.Timestamp
		}
	}

	stats.StorageSize = totalSize
	stats.OldestRecord = oldest
	stats.NewestRecord = newest
	if len(s.correlations) > 0 {
		stats.AverageRecordSize = int(totalSize / int64(len(s.correlations)))
	}
	stats.CompressionRatio = 1.0              // No compression in memory
	stats.QueryPerformance = time.Millisecond // Fast in-memory queries

	return stats, nil
}

// Helper methods

func (s *InMemoryCorrelationStore) updateStatistics(correlation *StoredCorrelation) {
	// Update type counts
	s.stats.CorrelationsByType[correlation.Type]++

	// Update hourly distribution
	hour := correlation.Timestamp.Hour()
	s.stats.CorrelationsByHour[hour]++

	// Add trend data point
	trend := CorrelationTrend{
		Timestamp:         correlation.Timestamp,
		CorrelationRate:   1.0, // Simplified - would calculate actual rate
		AverageConfidence: correlation.Confidence,
		PatternDiversity:  len(s.patterns),
	}
	s.stats.TrendData = append(s.stats.TrendData, trend)

	// Keep only recent trend data (last 24 hours)
	cutoff := time.Now().Add(-24 * time.Hour)
	var recentTrends []CorrelationTrend
	for _, trend := range s.stats.TrendData {
		if trend.Timestamp.After(cutoff) {
			recentTrends = append(recentTrends, trend)
		}
	}
	s.stats.TrendData = recentTrends
}

func (s *InMemoryCorrelationStore) learnPattern(correlation *StoredCorrelation) {
	// Extract pattern key from correlation
	patternKey := fmt.Sprintf("%s_%s", correlation.Type, correlation.Explanation.Summary)

	pattern, exists := s.patterns[patternKey]
	if !exists {
		// Create new pattern
		pattern = &LearnedPattern{
			ID:          patternKey,
			Type:        correlation.Type,
			Pattern:     correlation.Explanation.Summary,
			Description: correlation.Explanation.Details,
			FirstSeen:   correlation.Timestamp,
			Examples:    make([]*StoredCorrelation, 0),
			Metadata:    make(map[string]interface{}),
		}
		s.patterns[patternKey] = pattern
	}

	// Update pattern
	pattern.Frequency++
	pattern.LastSeen = correlation.Timestamp
	pattern.Confidence = (pattern.Confidence*float64(pattern.Frequency-1) + correlation.Confidence) / float64(pattern.Frequency)

	// Add example (keep only recent ones)
	pattern.Examples = append(pattern.Examples, correlation)
	if len(pattern.Examples) > 10 {
		pattern.Examples = pattern.Examples[1:] // Keep last 10 examples
	}
}

// CorrelationPersistenceService integrates persistence with correlation system
type CorrelationPersistenceService struct {
	store  CorrelationStore
	logger *zap.Logger
}

// NewCorrelationPersistenceService creates a new persistence service
func NewCorrelationPersistenceService(store CorrelationStore, logger *zap.Logger) *CorrelationPersistenceService {
	return &CorrelationPersistenceService{
		store:  store,
		logger: logger,
	}
}

// PersistCorrelation converts and stores a correlation
func (s *CorrelationPersistenceService) PersistCorrelation(ctx context.Context,
	sourceEvent *domain.UnifiedEvent,
	correlation interface{},
	explanation CorrelationExplanation,
	confidence float64) error {

	storedCorr := &StoredCorrelation{
		Timestamp:   time.Now(),
		Source:      sourceEvent,
		Confidence:  confidence,
		Explanation: explanation,
		Metadata:    make(map[string]interface{}),
		LastSeen:    time.Now(),
		Frequency:   1,
	}

	// Type-specific handling
	switch corr := correlation.(type) {
	case K8sCorrelation:
		storedCorr.Type = "k8s_correlation"
		storedCorr.Metadata["k8s_type"] = corr.Type
		storedCorr.Metadata["source_resource"] = corr.Source
		storedCorr.Metadata["target_resource"] = corr.Target

	case TemporalCorrelation:
		storedCorr.Type = "temporal_correlation"
		storedCorr.Metadata["time_delta"] = corr.TimeDelta
		storedCorr.Metadata["occurrences"] = corr.Occurrences

	case SequenceCorrelation:
		storedCorr.Type = "sequence_correlation"
		storedCorr.Metadata["pattern"] = corr.Pattern
		storedCorr.Metadata["sequence_length"] = len(corr.Events)
		// Convert EventReference to UnifiedEvent references (simplified)
		storedCorr.Metadata["event_references"] = corr.Events

	default:
		storedCorr.Type = "unknown_correlation"
	}

	return s.store.StoreCorrelation(ctx, storedCorr)
}

// GetHistoricalCorrelations retrieves correlations for analysis
func (s *CorrelationPersistenceService) GetHistoricalCorrelations(ctx context.Context,
	correlationType string,
	timeRange time.Duration) ([]*StoredCorrelation, error) {

	end := time.Now()
	start := end.Add(-timeRange)

	// Get correlations by time range
	timeFilteredCorrelations, err := s.store.GetCorrelationsByTimeRange(ctx, start, end)
	if err != nil {
		return nil, err
	}

	// If no type filter specified, return all time-filtered correlations
	if correlationType == "" {
		return timeFilteredCorrelations, nil
	}

	// Filter by type
	var results []*StoredCorrelation
	for _, corr := range timeFilteredCorrelations {
		if corr.Type == correlationType {
			results = append(results, corr)
		}
	}

	return results, nil
}

// GetCorrelationInsights provides analytical insights
func (s *CorrelationPersistenceService) GetCorrelationInsights(ctx context.Context) (*CorrelationStatistics, error) {
	return s.store.GetCorrelationStats(ctx)
}
