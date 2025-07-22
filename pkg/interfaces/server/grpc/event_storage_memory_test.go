package grpc

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func createTestUnifiedEvent(id string, eventType domain.EventType, source string, timestamp time.Time) *domain.UnifiedEvent {
	return &domain.UnifiedEvent{
		ID:        id,
		Type:      eventType,
		Source:    source,
		Timestamp: timestamp,
		Entity: &domain.Entity{
			Type:      "service",
			Name:      "test-service",
			Namespace: "default",
		},
		Semantic: &domain.SemanticContext{
			Intent:     "test",
			Category:   "test",
			Confidence: 0.9,
		},
	}
}

func TestMemoryEventStorage_Store(t *testing.T) {
	storage := NewMemoryEventStorage(100, 24*time.Hour)
	ctx := context.Background()

	tests := []struct {
		name    string
		event   *domain.UnifiedEvent
		wantErr bool
	}{
		{
			name:    "store valid event",
			event:   createTestUnifiedEvent("event-1", domain.EventTypeProcess, "test", time.Now()),
			wantErr: false,
		},
		{
			name:    "store duplicate event (idempotent)",
			event:   createTestUnifiedEvent("event-1", domain.EventTypeProcess, "test", time.Now()),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := storage.Store(ctx, tt.event)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}

	// Verify event was stored
	event, err := storage.Get(ctx, "event-1")
	require.NoError(t, err)
	assert.Equal(t, "event-1", event.ID)
}

func TestMemoryEventStorage_StoreBatch(t *testing.T) {
	storage := NewMemoryEventStorage(100, 24*time.Hour)
	ctx := context.Background()

	events := make([]*domain.UnifiedEvent, 10)
	for i := 0; i < 10; i++ {
		events[i] = createTestUnifiedEvent(
			string(rune('a'+i)),
			domain.EventTypeProcess,
			"batch-test",
			time.Now().Add(time.Duration(i)*time.Second),
		)
	}

	err := storage.StoreBatch(ctx, events)
	require.NoError(t, err)

	// Verify all events were stored
	for i := 0; i < 10; i++ {
		event, err := storage.Get(ctx, string(rune('a'+i)))
		require.NoError(t, err)
		assert.Equal(t, string(rune('a'+i)), event.ID)
	}
}

func TestMemoryEventStorage_Eviction(t *testing.T) {
	storage := NewMemoryEventStorage(5, 24*time.Hour)
	ctx := context.Background()

	// Store more events than capacity
	for i := 0; i < 10; i++ {
		event := createTestUnifiedEvent(
			string(rune('a'+i)),
			domain.EventTypeProcess,
			"eviction-test",
			time.Now(),
		)
		err := storage.Store(ctx, event)
		require.NoError(t, err)
	}

	// Verify only last 5 events remain
	count, err := storage.Count(ctx, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(5), count)

	// First 5 events should be evicted
	for i := 0; i < 5; i++ {
		_, err := storage.Get(ctx, string(rune('a'+i)))
		assert.Error(t, err)
	}

	// Last 5 events should remain
	for i := 5; i < 10; i++ {
		event, err := storage.Get(ctx, string(rune('a'+i)))
		require.NoError(t, err)
		assert.Equal(t, string(rune('a'+i)), event.ID)
	}
}

func TestMemoryEventStorage_Query(t *testing.T) {
	storage := NewMemoryEventStorage(100, 24*time.Hour)
	ctx := context.Background()

	// Create test events with different attributes
	baseTime := time.Now()
	events := []*domain.UnifiedEvent{
		createTestUnifiedEvent("1", domain.EventTypeProcess, "source-a", baseTime.Add(-5*time.Minute)),
		createTestUnifiedEvent("2", domain.EventTypeNetwork, "source-b", baseTime.Add(-4*time.Minute)),
		createTestUnifiedEvent("3", domain.EventTypeProcess, "source-a", baseTime.Add(-3*time.Minute)),
		createTestUnifiedEvent("4", domain.EventTypeKernel, "source-c", baseTime.Add(-2*time.Minute)),
		createTestUnifiedEvent("5", domain.EventTypeProcess, "source-b", baseTime.Add(-1*time.Minute)),
	}

	// Set different namespaces
	events[0].Entity.Namespace = "namespace-a"
	events[1].Entity.Namespace = "namespace-b"
	events[2].Entity.Namespace = "namespace-a"
	events[3].Entity.Namespace = "namespace-c"
	events[4].Entity.Namespace = "namespace-b"

	// Store all events
	for _, event := range events {
		err := storage.Store(ctx, event)
		require.NoError(t, err)
	}

	tests := []struct {
		name          string
		filter        *pb.Filter
		limit         int
		expectedCount int
		expectedIDs   []string
	}{
		{
			name:          "no filter",
			filter:        nil,
			limit:         10,
			expectedCount: 5,
		},
		{
			name: "filter by event type",
			filter: &pb.Filter{
				EventTypes: []string{string(domain.EventTypeProcess)},
			},
			limit:         10,
			expectedCount: 3,
			expectedIDs:   []string{"5", "3", "1"},
		},
		{
			name: "filter by source",
			filter: &pb.Filter{
				Sources: []string{"source-a"},
			},
			limit:         10,
			expectedCount: 2,
			expectedIDs:   []string{"3", "1"},
		},
		{
			name: "filter by namespace",
			filter: &pb.Filter{
				Namespaces: []string{"namespace-b"},
			},
			limit:         10,
			expectedCount: 2,
			expectedIDs:   []string{"5", "2"},
		},
		{
			name: "filter by multiple types",
			filter: &pb.Filter{
				EventTypes: []string{string(domain.EventTypeProcess), string(domain.EventTypeNetwork)},
			},
			limit:         10,
			expectedCount: 4,
		},
		{
			name: "limit results",
			filter: &pb.Filter{
				EventTypes: []string{string(domain.EventTypeProcess)},
			},
			limit:         2,
			expectedCount: 2,
			expectedIDs:   []string{"5", "3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, _, err := storage.Query(ctx, tt.filter, nil, tt.limit, "")
			require.NoError(t, err)
			assert.Len(t, results, tt.expectedCount)

			if len(tt.expectedIDs) > 0 {
				actualIDs := make([]string, len(results))
				for i, event := range results {
					actualIDs[i] = event.ID
				}
				assert.Equal(t, tt.expectedIDs, actualIDs)
			}
		})
	}
}

func TestMemoryEventStorage_QueryWithTimeRange(t *testing.T) {
	storage := NewMemoryEventStorage(100, 24*time.Hour)
	ctx := context.Background()

	baseTime := time.Now()
	events := []*domain.UnifiedEvent{
		createTestUnifiedEvent("old", domain.EventTypeProcess, "test", baseTime.Add(-10*time.Minute)),
		createTestUnifiedEvent("mid1", domain.EventTypeProcess, "test", baseTime.Add(-5*time.Minute)),
		createTestUnifiedEvent("mid2", domain.EventTypeProcess, "test", baseTime.Add(-3*time.Minute)),
		createTestUnifiedEvent("new", domain.EventTypeProcess, "test", baseTime.Add(-1*time.Minute)),
	}

	for _, event := range events {
		err := storage.Store(ctx, event)
		require.NoError(t, err)
	}

	tests := []struct {
		name          string
		startOffset   time.Duration
		endOffset     time.Duration
		expectedCount int
		expectedIDs   []string
	}{
		{
			name:          "all events",
			startOffset:   -15 * time.Minute,
			endOffset:     0,
			expectedCount: 4,
		},
		{
			name:          "middle range",
			startOffset:   -6 * time.Minute,
			endOffset:     -2 * time.Minute,
			expectedCount: 2,
			expectedIDs:   []string{"mid2", "mid1"},
		},
		{
			name:          "recent only",
			startOffset:   -2 * time.Minute,
			endOffset:     0,
			expectedCount: 1,
			expectedIDs:   []string{"new"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			timeRange := &pb.TimeRange{
				Start: timestamppb.New(baseTime.Add(tt.startOffset)),
				End:   timestamppb.New(baseTime.Add(tt.endOffset)),
			}

			results, _, err := storage.Query(ctx, nil, timeRange, 10, "")
			require.NoError(t, err)
			assert.Len(t, results, tt.expectedCount)

			if len(tt.expectedIDs) > 0 {
				actualIDs := make([]string, len(results))
				for i, event := range results {
					actualIDs[i] = event.ID
				}
				assert.Equal(t, tt.expectedIDs, actualIDs)
			}
		})
	}
}

func TestMemoryEventStorage_Pagination(t *testing.T) {
	storage := NewMemoryEventStorage(100, 24*time.Hour)
	ctx := context.Background()

	// Store 20 events
	for i := 0; i < 20; i++ {
		event := createTestUnifiedEvent(
			string(rune('a'+i)),
			domain.EventTypeProcess,
			"pagination-test",
			time.Now().Add(time.Duration(i)*time.Second),
		)
		err := storage.Store(ctx, event)
		require.NoError(t, err)
	}

	// First page
	page1, token1, err := storage.Query(ctx, nil, nil, 5, "")
	require.NoError(t, err)
	assert.Len(t, page1, 5)
	assert.NotEmpty(t, token1)

	// Second page
	page2, token2, err := storage.Query(ctx, nil, nil, 5, token1)
	require.NoError(t, err)
	assert.Len(t, page2, 5)
	assert.NotEmpty(t, token2)

	// Verify no overlap
	page1IDs := make(map[string]bool)
	for _, event := range page1 {
		page1IDs[event.ID] = true
	}
	for _, event := range page2 {
		assert.False(t, page1IDs[event.ID], "Found duplicate event in pages")
	}
}

func TestMemoryEventStorage_Indexes(t *testing.T) {
	storage := NewMemoryEventStorage(100, 24*time.Hour)
	ctx := context.Background()

	// Create events with specific attributes
	events := []*domain.UnifiedEvent{
		createTestUnifiedEvent("1", domain.EventTypeProcess, "source-a", time.Now()),
		createTestUnifiedEvent("2", domain.EventTypeProcess, "source-b", time.Now()),
		createTestUnifiedEvent("3", domain.EventTypeNetwork, "source-a", time.Now()),
		createTestUnifiedEvent("4", domain.EventTypeKernel, "source-c", time.Now()),
	}

	for _, event := range events {
		err := storage.Store(ctx, event)
		require.NoError(t, err)
	}

	// Test type index
	assert.Len(t, storage.typeIndex[domain.EventTypeProcess], 2)
	assert.Len(t, storage.typeIndex[domain.EventTypeNetwork], 1)
	assert.Len(t, storage.typeIndex[domain.EventTypeKernel], 1)

	// Test source index
	assert.Len(t, storage.sourceIndex["source-a"], 2)
	assert.Len(t, storage.sourceIndex["source-b"], 1)
	assert.Len(t, storage.sourceIndex["source-c"], 1)

	// Test time index
	assert.Greater(t, len(storage.timeIndex.buckets), 0)
}

func TestMemoryEventStorage_RetentionCleanup(t *testing.T) {
	storage := NewMemoryEventStorage(100, 1*time.Hour)
	ctx := context.Background()

	// Store old and new events
	oldTime := time.Now().Add(-2 * time.Hour)
	newTime := time.Now()

	oldEvent := createTestUnifiedEvent("old", domain.EventTypeProcess, "test", oldTime)
	newEvent := createTestUnifiedEvent("new", domain.EventTypeProcess, "test", newTime)

	err := storage.Store(ctx, oldEvent)
	require.NoError(t, err)
	err = storage.Store(ctx, newEvent)
	require.NoError(t, err)

	// Old event should be cleaned up, new event should remain
	_, err = storage.Get(ctx, "old")
	assert.Error(t, err)

	event, err := storage.Get(ctx, "new")
	require.NoError(t, err)
	assert.Equal(t, "new", event.ID)
}

func TestMemoryEventStorage_Health(t *testing.T) {
	storage := NewMemoryEventStorage(10, 24*time.Hour)
	ctx := context.Background()

	// Initially healthy
	health := storage.Health()
	assert.Equal(t, pb.HealthStatus_STATUS_HEALTHY, health.Status)

	// Fill to near capacity
	for i := 0; i < 9; i++ {
		event := createTestUnifiedEvent(
			string(rune('a'+i)),
			domain.EventTypeProcess,
			"health-test",
			time.Now(),
		)
		err := storage.Store(ctx, event)
		require.NoError(t, err)
	}

	// Should be degraded when near capacity
	health = storage.Health()
	assert.Equal(t, pb.HealthStatus_STATUS_DEGRADED, health.Status)
	assert.Contains(t, health.Message, "utilization high")
}

func TestMemoryEventStorage_Statistics(t *testing.T) {
	storage := NewMemoryEventStorage(100, 24*time.Hour)
	ctx := context.Background()

	// Store some events
	for i := 0; i < 10; i++ {
		event := createTestUnifiedEvent(
			string(rune('a'+i)),
			domain.EventTypeProcess,
			"stats-test",
			time.Now(),
		)
		err := storage.Store(ctx, event)
		require.NoError(t, err)
	}

	// Query some events
	_, _, err := storage.Query(ctx, nil, nil, 5, "")
	require.NoError(t, err)

	stats := storage.GetStatistics()
	assert.Equal(t, 10, stats["events_count"])
	assert.Equal(t, uint64(10), stats["total_stored"])
	assert.Equal(t, uint64(1), stats["query_count"])
}

func TestCircularEventBuffer(t *testing.T) {
	buffer := NewCircularEventBuffer(5)

	// Add events
	for i := 0; i < 10; i++ {
		event := createTestUnifiedEvent(
			string(rune('a'+i)),
			domain.EventTypeProcess,
			"buffer-test",
			time.Now(),
		)
		buffer.Add(event)
	}

	// Should only have last 5 events
	recent := buffer.GetRecent(10)
	assert.Len(t, recent, 5)

	// Verify they are the most recent ones
	for i := 0; i < 5; i++ {
		assert.Equal(t, string(rune('f'+i)), recent[i].ID)
	}
}

func TestCircularEventBuffer_TimeWindow(t *testing.T) {
	buffer := NewCircularEventBuffer(10)
	baseTime := time.Now()

	// Add events at different times
	for i := 0; i < 5; i++ {
		event := createTestUnifiedEvent(
			string(rune('a'+i)),
			domain.EventTypeProcess,
			"window-test",
			baseTime.Add(time.Duration(i)*time.Minute),
		)
		buffer.Add(event)
	}

	// Get events from last 3 minutes
	windowEvents := buffer.GetTimeWindow(3 * time.Minute)

	// Should get the most recent events within window
	assert.GreaterOrEqual(t, len(windowEvents), 1)
	assert.LessOrEqual(t, len(windowEvents), 5)
}

// Benchmarks
func BenchmarkMemoryEventStorage_Store(b *testing.B) {
	storage := NewMemoryEventStorage(10000, 24*time.Hour)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		event := createTestUnifiedEvent(
			string(rune(i)),
			domain.EventTypeProcess,
			"bench",
			time.Now(),
		)
		storage.Store(ctx, event)
	}
}

func BenchmarkMemoryEventStorage_Query(b *testing.B) {
	storage := NewMemoryEventStorage(10000, 24*time.Hour)
	ctx := context.Background()

	// Pre-populate
	for i := 0; i < 1000; i++ {
		event := createTestUnifiedEvent(
			string(rune(i)),
			domain.EventTypeProcess,
			"bench",
			time.Now(),
		)
		storage.Store(ctx, event)
	}

	filter := &pb.Filter{
		EventTypes: []string{string(domain.EventTypeProcess)},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		storage.Query(ctx, filter, nil, 10, "")
	}
}

func BenchmarkMemoryEventStorage_QueryWithIndexes(b *testing.B) {
	storage := NewMemoryEventStorage(10000, 24*time.Hour)
	ctx := context.Background()

	// Pre-populate with diverse data
	types := []domain.EventType{domain.EventTypeProcess, domain.EventTypeNetwork, domain.EventTypeKernel}
	sources := []string{"source-a", "source-b", "source-c", "source-d"}

	for i := 0; i < 5000; i++ {
		event := createTestUnifiedEvent(
			string(rune(i)),
			types[i%len(types)],
			sources[i%len(sources)],
			time.Now().Add(time.Duration(i)*time.Second),
		)
		storage.Store(ctx, event)
	}

	filter := &pb.Filter{
		EventTypes: []string{string(domain.EventTypeProcess)},
		Sources:    []string{"source-a"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		storage.Query(ctx, filter, nil, 10, "")
	}
}

func TestMemoryEventStorage_ConcurrentAccess(t *testing.T) {
	storage := NewMemoryEventStorage(1000, 24*time.Hour)
	ctx := context.Background()

	// Concurrent writes
	errCh := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				event := createTestUnifiedEvent(
					string(rune(id*1000+j)),
					domain.EventTypeProcess,
					"concurrent",
					time.Now(),
				)
				if err := storage.Store(ctx, event); err != nil {
					errCh <- err
					return
				}
			}
			errCh <- nil
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		err := <-errCh
		assert.NoError(t, err)
	}

	// Verify all events were stored
	count, err := storage.Count(ctx, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(1000), count)
}

func TestMemoryEventStorage_RemoveDuplicates(t *testing.T) {
	storage := NewMemoryEventStorage(100, 24*time.Hour)

	input := []string{"a", "b", "a", "c", "b", "d", "a"}
	result := storage.removeDuplicates(input)

	// Should preserve order and remove duplicates
	expected := []string{"a", "b", "c", "d"}
	assert.Equal(t, expected, result)
}

func TestTimeIndex_BucketGeneration(t *testing.T) {
	storage := NewMemoryEventStorage(100, 24*time.Hour)
	ctx := context.Background()

	baseTime := time.Now().Truncate(time.Minute)

	// Add events across multiple minutes
	for i := 0; i < 5; i++ {
		event := createTestUnifiedEvent(
			string(rune('a'+i)),
			domain.EventTypeProcess,
			"time-test",
			baseTime.Add(time.Duration(i)*time.Minute),
		)
		err := storage.Store(ctx, event)
		require.NoError(t, err)
	}

	// Should have 5 different time buckets
	assert.Len(t, storage.timeIndex.buckets, 5)

	// Each bucket should have one event
	for _, eventIDs := range storage.timeIndex.buckets {
		assert.Len(t, eventIDs, 1)
	}
}

func TestMemoryEventStorage_QueryComplexFilter(t *testing.T) {
	storage := NewMemoryEventStorage(100, 24*time.Hour)
	ctx := context.Background()

	// Create diverse events
	events := []*domain.UnifiedEvent{
		createTestUnifiedEvent("1", domain.EventTypeProcess, "app-server", time.Now()),
		createTestUnifiedEvent("2", domain.EventTypeNetwork, "nginx", time.Now()),
		createTestUnifiedEvent("3", domain.EventTypeKernel, "kernel", time.Now()),
		createTestUnifiedEvent("4", domain.EventTypeProcess, "app-server", time.Now()),
		createTestUnifiedEvent("5", domain.EventTypeCPU, "system", time.Now()),
	}

	// Add different severities
	events[0].Application = &domain.ApplicationContext{Level: "error"}
	events[1].Network = &domain.NetworkContext{StatusCode: 500}
	events[3].Application = &domain.ApplicationContext{Level: "error"}

	// Store all events
	for _, event := range events {
		err := storage.Store(ctx, event)
		require.NoError(t, err)
	}

	// Complex filter: Process events from app-server with error severity
	filter := &pb.Filter{
		EventTypes: []string{string(domain.EventTypeProcess)},
		Sources:    []string{"app-server"},
		Severities: []string{"error"},
	}

	results, _, err := storage.Query(ctx, filter, nil, 10, "")
	require.NoError(t, err)
	assert.Len(t, results, 2)

	// Verify results match all criteria
	for _, event := range results {
		assert.Equal(t, domain.EventTypeProcess, event.Type)
		assert.Equal(t, "app-server", event.Source)
		assert.Equal(t, "error", event.GetSeverity())
	}
}

func TestMemoryEventStorage_GetCandidateIDs_Efficiency(t *testing.T) {
	storage := NewMemoryEventStorage(1000, 24*time.Hour)
	ctx := context.Background()

	// Create events with specific distribution
	// 100 process events, 900 other types
	for i := 0; i < 1000; i++ {
		eventType := domain.EventTypeNetwork
		if i < 100 {
			eventType = domain.EventTypeProcess
		}

		event := createTestUnifiedEvent(
			string(rune(i)),
			eventType,
			"test",
			time.Now(),
		)
		err := storage.Store(ctx, event)
		require.NoError(t, err)
	}

	// Query with type filter should be efficient
	filter := &pb.Filter{
		EventTypes: []string{string(domain.EventTypeProcess)},
	}

	candidates := storage.getCandidateIDs(filter, nil)

	// Should only get process event candidates
	assert.Len(t, candidates, 100)

	// All candidates should be process events
	for _, id := range candidates {
		event, err := storage.Get(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, domain.EventTypeProcess, event.Type)
	}
}

func TestMemoryEventStorage_QuerySorting(t *testing.T) {
	storage := NewMemoryEventStorage(100, 24*time.Hour)
	ctx := context.Background()

	// Store events with specific timestamps
	baseTime := time.Now()
	for i := 0; i < 5; i++ {
		event := createTestUnifiedEvent(
			string(rune('a'+i)),
			domain.EventTypeProcess,
			"sort-test",
			baseTime.Add(time.Duration(i)*time.Hour),
		)
		err := storage.Store(ctx, event)
		require.NoError(t, err)
	}

	// Query all events
	results, _, err := storage.Query(ctx, nil, nil, 10, "")
	require.NoError(t, err)
	assert.Len(t, results, 5)

	// Verify newest first ordering
	for i := 0; i < len(results)-1; i++ {
		assert.True(t, results[i].Timestamp.After(results[i+1].Timestamp),
			"Events should be sorted newest first")
	}

	// First event should be the newest
	assert.Equal(t, string('e'), results[0].ID)
	// Last event should be the oldest
	assert.Equal(t, string('a'), results[4].ID)
}
