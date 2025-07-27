package correlation

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

func TestInMemoryCorrelationStore_StoreCorrelation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := NewInMemoryCorrelationStore(logger)
	ctx := context.Background()

	correlation := &StoredCorrelation{
		Type:       "k8s_correlation",
		Source:     createPersistenceTestUnifiedEvent("source-1"),
		Target:     createPersistenceTestUnifiedEvent("target-1"),
		Confidence: 0.85,
		Explanation: CorrelationExplanation{
			Summary: "Pod is owned by ReplicaSet",
			Details: "Strong ownership relationship detected",
		},
	}

	err := store.StoreCorrelation(ctx, correlation)
	assert.NoError(t, err)
	assert.NotEmpty(t, correlation.ID)
	assert.False(t, correlation.Timestamp.IsZero())

	// Verify storage
	correlations, err := store.GetCorrelationsByType(ctx, "k8s_correlation", 10)
	assert.NoError(t, err)
	assert.Len(t, correlations, 1)
	assert.Equal(t, correlation.ID, correlations[0].ID)
}

func TestInMemoryCorrelationStore_GetCorrelationsByType(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := NewInMemoryCorrelationStore(logger)
	ctx := context.Background()

	// Store different types of correlations
	correlations := []*StoredCorrelation{
		{Type: "k8s_correlation", Confidence: 0.9},
		{Type: "temporal_correlation", Confidence: 0.8},
		{Type: "k8s_correlation", Confidence: 0.7},
		{Type: "sequence_correlation", Confidence: 0.6},
	}

	for _, corr := range correlations {
		err := store.StoreCorrelation(ctx, corr)
		require.NoError(t, err)
	}

	// Query by type
	k8sCorrelations, err := store.GetCorrelationsByType(ctx, "k8s_correlation", 10)
	assert.NoError(t, err)
	assert.Len(t, k8sCorrelations, 2)

	// Query all types
	allCorrelations, err := store.GetCorrelationsByType(ctx, "", 10)
	assert.NoError(t, err)
	assert.Len(t, allCorrelations, 4)

	// Test limit
	limitedCorrelations, err := store.GetCorrelationsByType(ctx, "", 2)
	assert.NoError(t, err)
	assert.Len(t, limitedCorrelations, 2)
}

func TestInMemoryCorrelationStore_GetCorrelationsByTimeRange(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := NewInMemoryCorrelationStore(logger)
	ctx := context.Background()

	now := time.Now()

	// Store correlations with different timestamps
	correlations := []*StoredCorrelation{
		{Type: "test1", Timestamp: now.Add(-2 * time.Hour)},
		{Type: "test2", Timestamp: now.Add(-1 * time.Hour)},
		{Type: "test3", Timestamp: now.Add(-30 * time.Minute)},
		{Type: "test4", Timestamp: now.Add(-10 * time.Minute)},
	}

	for _, corr := range correlations {
		err := store.StoreCorrelation(ctx, corr)
		require.NoError(t, err)
	}

	// Query last hour
	start := now.Add(-1 * time.Hour)
	end := now
	recentCorrelations, err := store.GetCorrelationsByTimeRange(ctx, start, end)
	assert.NoError(t, err)
	assert.Len(t, recentCorrelations, 2) // test3 and test4
}

func TestInMemoryCorrelationStore_GetCorrelationsByConfidence(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := NewInMemoryCorrelationStore(logger)
	ctx := context.Background()

	// Store correlations with different confidence levels
	correlations := []*StoredCorrelation{
		{Type: "high", Confidence: 0.95},
		{Type: "medium", Confidence: 0.75},
		{Type: "low", Confidence: 0.45},
		{Type: "very_high", Confidence: 0.98},
	}

	for _, corr := range correlations {
		err := store.StoreCorrelation(ctx, corr)
		require.NoError(t, err)
	}

	// Query high confidence correlations
	highConfidence, err := store.GetCorrelationsByConfidence(ctx, 0.8)
	assert.NoError(t, err)
	assert.Len(t, highConfidence, 2) // high and very_high

	// Query medium confidence correlations
	mediumConfidence, err := store.GetCorrelationsByConfidence(ctx, 0.5)
	assert.NoError(t, err)
	assert.Len(t, mediumConfidence, 3) // high, medium, very_high
}

func TestInMemoryCorrelationStore_UpdateCorrelationFeedback(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := NewInMemoryCorrelationStore(logger)
	ctx := context.Background()

	// Store a correlation
	correlation := &StoredCorrelation{
		Type:       "test_correlation",
		Confidence: 0.8,
	}
	err := store.StoreCorrelation(ctx, correlation)
	require.NoError(t, err)

	// Add positive feedback
	positiveFeedback := CorrelationFeedback{
		UserID:     "user1",
		Timestamp:  time.Now(),
		IsCorrect:  true,
		Confidence: 0.9,
		Comments:   "This correlation is accurate",
		Source:     "explicit",
	}

	err = store.UpdateCorrelationFeedback(ctx, correlation.ID, positiveFeedback)
	assert.NoError(t, err)

	// Add negative feedback
	negativeFeedback := CorrelationFeedback{
		UserID:     "user2",
		Timestamp:  time.Now(),
		IsCorrect:  false,
		Confidence: 0.3,
		Comments:   "This seems incorrect",
		Source:     "explicit",
	}

	err = store.UpdateCorrelationFeedback(ctx, correlation.ID, negativeFeedback)
	assert.NoError(t, err)

	// Verify feedback was stored
	correlations, err := store.GetCorrelationsByType(ctx, "test_correlation", 1)
	require.NoError(t, err)
	require.Len(t, correlations, 1)

	stored := correlations[0]
	assert.Len(t, stored.UserFeedback, 2)
	assert.Equal(t, 1, stored.ConfirmationCount)
	assert.Equal(t, 1, stored.RejectionCount)

	// Test feedback for non-existent correlation
	err = store.UpdateCorrelationFeedback(ctx, "non-existent", positiveFeedback)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "correlation not found")
}

func TestInMemoryCorrelationStore_GetCorrelationStats(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := NewInMemoryCorrelationStore(logger)
	ctx := context.Background()

	// Store correlations with various confidence levels
	correlations := []*StoredCorrelation{
		{Type: "k8s_correlation", Confidence: 0.95},
		{Type: "k8s_correlation", Confidence: 0.85},
		{Type: "temporal_correlation", Confidence: 0.75},
		{Type: "temporal_correlation", Confidence: 0.65},
		{Type: "sequence_correlation", Confidence: 0.45},
	}

	for _, corr := range correlations {
		err := store.StoreCorrelation(ctx, corr)
		require.NoError(t, err)
	}

	stats, err := store.GetCorrelationStats(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, stats)

	// Verify statistics
	assert.Equal(t, 5, stats.TotalCorrelations)
	assert.Equal(t, 2, stats.CorrelationsByType["k8s_correlation"])
	assert.Equal(t, 2, stats.CorrelationsByType["temporal_correlation"])
	assert.Equal(t, 1, stats.CorrelationsByType["sequence_correlation"])

	// Verify confidence distribution
	assert.Equal(t, 2, stats.HighConfidenceCount)   // 0.95, 0.85
	assert.Equal(t, 2, stats.MediumConfidenceCount) // 0.75, 0.65
	assert.Equal(t, 1, stats.LowConfidenceCount)    // 0.45

	// Verify average confidence
	expectedAvg := (0.95 + 0.85 + 0.75 + 0.65 + 0.45) / 5
	assert.InDelta(t, expectedAvg, stats.AverageConfidence, 0.01)
}

func TestInMemoryCorrelationStore_PatternLearning(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := NewInMemoryCorrelationStore(logger)
	ctx := context.Background()

	// Store multiple correlations with similar patterns
	correlations := []*StoredCorrelation{
		{
			Type:       "k8s_correlation",
			Confidence: 0.9,
			Explanation: CorrelationExplanation{
				Summary: "Pod owned by ReplicaSet",
				Details: "ownership pattern detected",
			},
		},
		{
			Type:       "k8s_correlation",
			Confidence: 0.85,
			Explanation: CorrelationExplanation{
				Summary: "Pod owned by ReplicaSet", // Same summary for pattern learning
				Details: "ownership pattern detected",
			},
		},
		{
			Type:       "temporal_correlation",
			Confidence: 0.8,
			Explanation: CorrelationExplanation{
				Summary: "Events occur in sequence",
				Details: "temporal_sequence pattern detected",
			},
		},
	}

	for _, corr := range correlations {
		err := store.StoreCorrelation(ctx, corr)
		require.NoError(t, err)
	}

	// Get learned patterns
	patterns, err := store.GetCorrelationPatterns(ctx)
	assert.NoError(t, err)
	assert.Len(t, patterns, 2) // ownership pattern and temporal_sequence pattern

	// Find ownership pattern
	var ownershipPattern *LearnedPattern
	for _, pattern := range patterns {
		if pattern.Pattern == "Pod owned by ReplicaSet" {
			ownershipPattern = pattern
			break
		}
	}

	require.NotNil(t, ownershipPattern)
	assert.Equal(t, "k8s_correlation", ownershipPattern.Type)
	assert.Equal(t, 2, ownershipPattern.Frequency)
	assert.Equal(t, "Pod owned by ReplicaSet", ownershipPattern.Pattern)
	assert.InDelta(t, 0.875, ownershipPattern.Confidence, 0.01) // (0.9 + 0.85) / 2
}

func TestInMemoryCorrelationStore_CleanupOldCorrelations(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := NewInMemoryCorrelationStore(logger)
	ctx := context.Background()

	now := time.Now()

	// Store correlations with different ages
	correlations := []*StoredCorrelation{
		{Type: "old1", Timestamp: now.Add(-25 * time.Hour)},
		{Type: "old2", Timestamp: now.Add(-26 * time.Hour)},
		{Type: "recent1", Timestamp: now.Add(-1 * time.Hour)},
		{Type: "recent2", Timestamp: now.Add(-2 * time.Hour)},
	}

	for _, corr := range correlations {
		err := store.StoreCorrelation(ctx, corr)
		require.NoError(t, err)
	}

	// Verify initial count
	all, err := store.GetCorrelationsByType(ctx, "", 10)
	assert.NoError(t, err)
	assert.Len(t, all, 4)

	// Cleanup correlations older than 24 hours
	err = store.CleanupOldCorrelations(ctx, 24*time.Hour)
	assert.NoError(t, err)

	// Verify cleanup
	remaining, err := store.GetCorrelationsByType(ctx, "", 10)
	assert.NoError(t, err)
	assert.Len(t, remaining, 2) // Only recent1 and recent2 should remain
}

func TestInMemoryCorrelationStore_GetStorageStats(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := NewInMemoryCorrelationStore(logger)
	ctx := context.Background()

	// Store some correlations
	correlations := []*StoredCorrelation{
		{Type: "test1", Confidence: 0.8},
		{Type: "test2", Confidence: 0.9},
	}

	for _, corr := range correlations {
		err := store.StoreCorrelation(ctx, corr)
		require.NoError(t, err)
	}

	stats, err := store.GetStorageStats(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, stats)

	assert.Equal(t, 2, stats.TotalRecords)
	assert.Greater(t, stats.StorageSize, int64(0))
	assert.Greater(t, stats.AverageRecordSize, 0)
	assert.Equal(t, 1.0, stats.CompressionRatio) // No compression in memory
	assert.Equal(t, time.Millisecond, stats.QueryPerformance)
}

func TestCorrelationPersistenceService_PersistCorrelation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := NewInMemoryCorrelationStore(logger)
	service := NewCorrelationPersistenceService(store, logger)
	ctx := context.Background()

	sourceEvent := createPersistenceTestUnifiedEvent("source-1")

	// Test K8s correlation persistence
	k8sCorr := K8sCorrelation{
		Type:       "ownership",
		Source:     ResourceRef{Name: "pod-1", Kind: "Pod"},
		Target:     ResourceRef{Name: "rs-1", Kind: "ReplicaSet"},
		Confidence: 0.95,
	}

	explanation := CorrelationExplanation{
		Summary: "Pod is owned by ReplicaSet",
		Details: "Strong ownership relationship",
	}

	err := service.PersistCorrelation(ctx, sourceEvent, k8sCorr, explanation, 0.95)
	assert.NoError(t, err)

	// Verify persistence
	correlations, err := store.GetCorrelationsByType(ctx, "k8s_correlation", 1)
	assert.NoError(t, err)
	assert.Len(t, correlations, 1)

	stored := correlations[0]
	assert.Equal(t, "k8s_correlation", stored.Type)
	assert.Equal(t, 0.95, stored.Confidence)
	assert.Equal(t, "ownership", stored.Metadata["k8s_type"])
}

func TestCorrelationPersistenceService_GetHistoricalCorrelations(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := NewInMemoryCorrelationStore(logger)
	service := NewCorrelationPersistenceService(store, logger)
	ctx := context.Background()

	now := time.Now()

	// Store historical correlations
	correlations := []*StoredCorrelation{
		{Type: "k8s_correlation", Timestamp: now.Add(-30 * time.Minute)},
		{Type: "k8s_correlation", Timestamp: now.Add(-2 * time.Hour)},
		{Type: "temporal_correlation", Timestamp: now.Add(-1 * time.Hour)},
		{Type: "k8s_correlation", Timestamp: now.Add(-25 * time.Hour)}, // Outside range
	}

	for _, corr := range correlations {
		err := store.StoreCorrelation(ctx, corr)
		require.NoError(t, err)
	}

	// Get historical correlations for last 4 hours
	historical, err := service.GetHistoricalCorrelations(ctx, "k8s_correlation", 4*time.Hour)
	assert.NoError(t, err)
	assert.Len(t, historical, 2) // Only correlations within 4 hours

	// Get all types within last 4 hours
	allHistorical, err := service.GetHistoricalCorrelations(ctx, "", 4*time.Hour)
	assert.NoError(t, err)
	assert.Len(t, allHistorical, 3) // k8s (2) + temporal (1)
}

func TestCorrelationPersistenceService_GetCorrelationInsights(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := NewInMemoryCorrelationStore(logger)
	service := NewCorrelationPersistenceService(store, logger)
	ctx := context.Background()

	// Store some correlations
	correlations := []*StoredCorrelation{
		{Type: "k8s_correlation", Confidence: 0.9},
		{Type: "temporal_correlation", Confidence: 0.8},
	}

	for _, corr := range correlations {
		err := store.StoreCorrelation(ctx, corr)
		require.NoError(t, err)
	}

	insights, err := service.GetCorrelationInsights(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, insights)
	assert.Equal(t, 2, insights.TotalCorrelations)
	assert.InDelta(t, 0.85, insights.AverageConfidence, 0.01)
}

// Helper function to create test unified events for persistence tests
func createPersistenceTestUnifiedEvent(id string) *domain.UnifiedEvent {
	return &domain.UnifiedEvent{
		ID:        id,
		Timestamp: time.Now(),
		Type:      domain.EventTypeKubernetes,
		Source:    "test",
		Severity:  domain.EventSeverityInfo,
		Kubernetes: &domain.KubernetesData{
			Object:     "test-object",
			ObjectKind: "Pod",
			Reason:     "Created",
			APIVersion: "v1",
		},
	}
}
