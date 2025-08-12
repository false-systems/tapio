package neo4j

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	neo4jclient "github.com/yairfalse/tapio/pkg/integrations/neo4j"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
	"go.uber.org/zap"
)

func TestNeo4jStorage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	logger, _ := zap.NewDevelopment()
	config := neo4jclient.Config{
		URI:      "bolt://localhost:7687",
		Username: "neo4j",
		Password: "password",
		Database: "neo4j",
	}

	storage, err := NewStorage(config, logger)
	if err != nil {
		t.Skipf("Failed to connect to Neo4j: %v", err)
	}
	defer storage.Close(context.Background())

	ctx := context.Background()

	t.Run("StoreAndRetrieve", func(t *testing.T) {
		// Create test correlation
		result := &correlation.CorrelationResult{
			ID:         "test-correlation-1",
			Type:       "k8s_ownership",
			Confidence: 0.95,
			Events:     []string{"event-1", "event-2", "event-3"},
			TraceID:    "trace-123",
			RootCause: &correlation.RootCause{
				EventID:     "event-1",
				Confidence:  0.9,
				Description: "Pod OOMKilled due to memory limit",
				Evidence: correlation.EvidenceData{
					EventIDs:    []string{"event-1"},
					ResourceIDs: []string{"pod/api-service-xxx"},
					Attributes: map[string]string{
						"reason": "Memory usage exceeded limit",
						"status": "Container terminated",
					},
				},
			},
			Impact: &correlation.Impact{
				Severity:  domain.EventSeverityHigh,
				Resources: []string{"deployment/api-service", "pod/api-service-xxx"},
				Services: []correlation.ServiceReference{
					{Name: "api-service", Namespace: "default", Type: "deployment"},
					{Name: "frontend", Namespace: "default", Type: "service"},
				},
			},
			Summary: "OOMKilled pod causing service disruption",
			Details: correlation.CorrelationDetails{
				Pattern:    "memory_limit_exceeded",
				Algorithm:  "threshold_detection",
				DataPoints: 3,
			},
			Evidence: correlation.EvidenceData{
				EventIDs:    []string{"event-1", "event-2", "event-3"},
				ResourceIDs: []string{"pod/api-service-xxx"},
				Attributes: map[string]string{
					"pattern":  "High memory usage pattern",
					"restarts": "Multiple restarts",
				},
			},
			StartTime: time.Now().Add(-10 * time.Minute),
			EndTime:   time.Now(),
		}

		// Store correlation
		err := storage.Store(ctx, result)
		require.NoError(t, err)

		// Retrieve by trace ID
		results, err := storage.GetByTraceID(ctx, "trace-123")
		require.NoError(t, err)
		assert.Len(t, results, 1)

		retrieved := results[0]
		assert.Equal(t, result.ID, retrieved.ID)
		assert.Equal(t, result.Type, retrieved.Type)
		assert.Equal(t, result.Confidence, retrieved.Confidence)
		assert.Equal(t, result.TraceID, retrieved.TraceID)
		assert.Equal(t, result.Summary, retrieved.Summary)
		assert.ElementsMatch(t, result.Events, retrieved.Events)

		// Check root cause
		require.NotNil(t, retrieved.RootCause)
		assert.Equal(t, result.RootCause.EventID, retrieved.RootCause.EventID)
		assert.Equal(t, result.RootCause.Description, retrieved.RootCause.Description)

		// Check impact
		require.NotNil(t, retrieved.Impact)
		assert.Equal(t, result.Impact.Severity, retrieved.Impact.Severity)
		assert.ElementsMatch(t, result.Impact.Resources, retrieved.Impact.Resources)
		assert.ElementsMatch(t, result.Impact.Services, retrieved.Impact.Services)
	})

	t.Run("GetRecent", func(t *testing.T) {
		// Store multiple correlations
		for i := 0; i < 5; i++ {
			result := &correlation.CorrelationResult{
				ID:         fmt.Sprintf("recent-correlation-%d", i),
				Type:       "temporal_pattern",
				Confidence: 0.8 + float64(i)*0.02,
				Events:     []string{fmt.Sprintf("event-%d", i)},
				Summary:    fmt.Sprintf("Test correlation %d", i),
				Details: correlation.CorrelationDetails{
					Pattern:    "temporal",
					Algorithm:  "pattern_matching",
					DataPoints: i + 1,
				},
				Evidence: correlation.EvidenceData{
					EventIDs: []string{fmt.Sprintf("event-%d", i)},
				},
				StartTime: time.Now().Add(time.Duration(-i) * time.Minute),
				EndTime:   time.Now(),
			}
			err := storage.Store(ctx, result)
			require.NoError(t, err)
		}

		// Get recent correlations
		results, err := storage.GetRecent(ctx, 3)
		require.NoError(t, err)
		assert.Len(t, results, 3)

		// Should be ordered by start time (most recent first)
		assert.Equal(t, "recent-correlation-0", results[0].ID)
		assert.Equal(t, "recent-correlation-1", results[1].ID)
		assert.Equal(t, "recent-correlation-2", results[2].ID)
	})

	t.Run("Cleanup", func(t *testing.T) {
		// Store an old correlation
		oldResult := &correlation.CorrelationResult{
			ID:         "old-correlation-1",
			Type:       "sequence_match",
			Confidence: 0.75,
			Events:     []string{"old-event-1"},
			Summary:    "Old test correlation",
			Details: correlation.CorrelationDetails{
				Pattern:    "sequence",
				Algorithm:  "sequence_matching",
				DataPoints: 1,
			},
			Evidence: correlation.EvidenceData{
				EventIDs: []string{"old-event-1"},
			},
			StartTime: time.Now().Add(-2 * time.Hour),
			EndTime:   time.Now().Add(-2 * time.Hour),
		}
		err := storage.Store(ctx, oldResult)
		require.NoError(t, err)

		// Store a recent correlation
		recentResult := &correlation.CorrelationResult{
			ID:         "recent-correlation-cleanup",
			Type:       "k8s_ownership",
			Confidence: 0.85,
			Events:     []string{"recent-event-1"},
			Summary:    "Recent test correlation",
			Details: correlation.CorrelationDetails{
				Pattern:    "ownership",
				Algorithm:  "k8s_metadata",
				DataPoints: 1,
			},
			Evidence: correlation.EvidenceData{
				EventIDs: []string{"recent-event-1"},
			},
			StartTime: time.Now().Add(-5 * time.Minute),
			EndTime:   time.Now(),
		}
		err = storage.Store(ctx, recentResult)
		require.NoError(t, err)

		// Clean up correlations older than 1 hour
		err = storage.Cleanup(ctx, 1*time.Hour)
		require.NoError(t, err)

		// Verify old correlation is gone
		results, err := storage.GetRecent(ctx, 100)
		require.NoError(t, err)

		// Should not find the old correlation
		found := false
		for _, r := range results {
			if r.ID == "old-correlation-1" {
				found = true
				break
			}
		}
		assert.False(t, found, "Old correlation should have been cleaned up")

		// Should still find the recent correlation
		found = false
		for _, r := range results {
			if r.ID == "recent-correlation-cleanup" {
				found = true
				break
			}
		}
		assert.True(t, found, "Recent correlation should still exist")
	})

	t.Run("ComplexCorrelation", func(t *testing.T) {
		// Create a complex correlation with all fields
		result := &correlation.CorrelationResult{
			ID:         "complex-correlation-1",
			Type:       "multi_factor",
			Confidence: 0.92,
			Events:     []string{"e1", "e2", "e3", "e4", "e5"},
			TraceID:    "complex-trace-123",
			RootCause: &correlation.RootCause{
				EventID:     "e1",
				Confidence:  0.88,
				Description: "Database connection pool exhausted",
				Evidence: correlation.EvidenceData{
					EventIDs:    []string{"e1", "e2", "e3"},
					ResourceIDs: []string{"statefulset/database"},
					Attributes: map[string]string{
						"error_type":  "Connection timeout errors",
						"pool_status": "Pool size at maximum",
						"query_type":  "Slow query detected",
					},
				},
			},
			Impact: &correlation.Impact{
				Severity:  domain.EventSeverityCritical,
				Resources: []string{"statefulset/database", "pvc/database-data"},
				Services: []correlation.ServiceReference{
					{Name: "api", Namespace: "default", Type: "deployment"},
					{Name: "auth", Namespace: "default", Type: "deployment"},
					{Name: "billing", Namespace: "default", Type: "deployment"},
					{Name: "notifications", Namespace: "default", Type: "deployment"},
				},
				Scope:       "platform-wide",
				UserImpact:  "All users experiencing delays",
				Degradation: "Response times increased 10x",
			},
			Summary: "Database connection exhaustion causing platform-wide impact",
			Details: correlation.CorrelationDetails{
				Pattern:        "connection_exhaustion",
				Algorithm:      "multi_factor_analysis",
				DataPoints:     5,
				ProcessingTime: 250 * time.Millisecond,
			},
			Evidence: correlation.EvidenceData{
				EventIDs:    []string{"e1", "e2", "e3", "e4", "e5"},
				ResourceIDs: []string{"statefulset/database", "pvc/database-data"},
				Attributes: map[string]string{
					"metric_spike":    "Spike in connection wait time",
					"service_impact":  "Multiple service timeouts",
					"user_complaints": "Customer complaints increased",
				},
			},
			StartTime: time.Now().Add(-30 * time.Minute),
			EndTime:   time.Now().Add(-5 * time.Minute),
		}

		// Store and retrieve
		err := storage.Store(ctx, result)
		require.NoError(t, err)

		results, err := storage.GetByTraceID(ctx, "complex-trace-123")
		require.NoError(t, err)
		require.Len(t, results, 1)

		retrieved := results[0]
		assert.Equal(t, result.ID, retrieved.ID)
		assert.Len(t, retrieved.Events, 5)
		assert.Len(t, retrieved.Evidence.EventIDs, 5)
		assert.Len(t, retrieved.Evidence.Attributes, 3)
		assert.Len(t, retrieved.RootCause.Evidence.EventIDs, 3)
		assert.Len(t, retrieved.RootCause.Evidence.Attributes, 3)
		assert.Len(t, retrieved.Impact.Services, 4)
	})
}
