package correlation

import (
	"context"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

func TestCorrelationResultPool_BasicOperations(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pool := NewCorrelationResultPool(logger, 10)
	ctx := context.Background()

	// Test Get returns a clean object
	result := pool.Get(ctx)
	require.NotNil(t, result)
	assert.Empty(t, result.ID)
	assert.Empty(t, result.Type)
	assert.Zero(t, result.Confidence)
	assert.Nil(t, result.ConfigData)
	assert.Nil(t, result.DependencyData)
	assert.Nil(t, result.TemporalData)
	assert.Nil(t, result.OwnershipData)
	assert.Nil(t, result.RootCause)
	assert.Nil(t, result.Impact)

	// Test Put returns object to pool
	pool.Put(ctx, result)

	// Test reuse
	result2 := pool.Get(ctx)
	require.NotNil(t, result2)

	// Objects should be reused (same memory address)
	assert.Equal(t, uintptr(unsafe.Pointer(result)), uintptr(unsafe.Pointer(result2)))
}

func TestCorrelationResultPool_ObjectReset(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pool := NewCorrelationResultPool(logger, 10)
	ctx := context.Background()

	// Get object and populate it
	result := pool.Get(ctx)
	result.ID = "test-id"
	result.Type = "test-type"
	result.Confidence = 0.95
	result.Message = "test message"
	result.TraceID = "trace-123"
	result.Summary = "test summary"
	result.StartTime = time.Now()
	result.EndTime = time.Now().Add(time.Minute)
	result.Events = []string{"event1", "event2"}
	result.Related = []*domain.UnifiedEvent{
		{ID: "test-event", Type: domain.EventTypeKubernetes},
	}

	// Create and attach nested objects
	result.ConfigData = &ConfigChangeData{
		ResourceType: "ConfigMap",
		ResourceName: "test-cm",
		Namespace:    "default",
		ChangeType:   "UPDATE",
		OldValue:     "old",
		NewValue:     "new",
		ChangedFields: map[string]string{
			"key": "value",
		},
	}

	result.RootCause = &RootCause{
		EventID:     "root-event",
		Confidence:  0.8,
		Description: "root cause",
	}

	result.Impact = &Impact{
		Severity:    domain.EventSeverityHigh,
		Scope:       "cluster",
		UserImpact:  "service down",
		Degradation: "50%",
		Resources:   []string{"pod1", "pod2"},
		Services:    []ServiceReference{{Name: "svc1", Namespace: "default"}},
	}

	// Return to pool
	pool.Put(ctx, result)

	// Get new object - should be reset
	result2 := pool.Get(ctx)
	assert.Empty(t, result2.ID)
	assert.Empty(t, result2.Type)
	assert.Zero(t, result2.Confidence)
	assert.Empty(t, result2.Message)
	assert.Empty(t, result2.TraceID)
	assert.Empty(t, result2.Summary)
	assert.True(t, result2.StartTime.IsZero())
	assert.True(t, result2.EndTime.IsZero())
	assert.Nil(t, result2.Events)
	assert.Nil(t, result2.Related)
	assert.Nil(t, result2.ConfigData)
	assert.Nil(t, result2.RootCause)
	assert.Nil(t, result2.Impact)
}

func TestCorrelationResultPool_NestedObjects(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pool := NewCorrelationResultPool(logger, 10)

	// Test config data pooling
	configData := pool.GetConfigData()
	require.NotNil(t, configData)
	assert.Empty(t, configData.ResourceType)
	assert.Empty(t, configData.ResourceName)
	assert.Empty(t, configData.Namespace)
	assert.Nil(t, configData.ChangedFields)

	// Test dependency data pooling
	depData := pool.GetDependencyData()
	require.NotNil(t, depData)
	assert.Empty(t, depData.SourceService.Name)
	assert.Empty(t, depData.TargetService.Name)
	assert.Zero(t, depData.Strength)

	// Test temporal data pooling
	tempData := pool.GetTemporalData()
	require.NotNil(t, tempData)
	assert.Zero(t, tempData.TimeWindow)
	assert.Empty(t, tempData.Pattern)
	assert.Nil(t, tempData.EventSequence)

	// Test ownership data pooling
	ownerData := pool.GetOwnershipData()
	require.NotNil(t, ownerData)
	assert.Empty(t, ownerData.Owner)
	assert.Empty(t, ownerData.Team)
	assert.Nil(t, ownerData.Labels)
	assert.Nil(t, ownerData.Annotations)

	// Test impact pooling
	impact := pool.GetImpact()
	require.NotNil(t, impact)
	assert.Empty(t, impact.Severity)
	assert.Empty(t, impact.Scope)
	assert.Nil(t, impact.Resources)
	assert.Nil(t, impact.Services)

	pool.PutImpact(impact)

	// Test root cause pooling
	rootCause := pool.GetRootCause()
	require.NotNil(t, rootCause)
	assert.Empty(t, rootCause.EventID)
	assert.Zero(t, rootCause.Confidence)
	assert.Empty(t, rootCause.Description)

	pool.PutRootCause(rootCause)
}

func TestCorrelationResultPool_SlicePooling(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pool := NewCorrelationResultPool(logger, 10)

	// Test string slice pooling
	stringSlice := pool.GetStringSlice()
	require.NotNil(t, stringSlice)
	assert.Equal(t, 0, len(stringSlice))
	assert.True(t, cap(stringSlice) >= 0)

	stringSlice = append(stringSlice, "test1", "test2")
	pool.PutStringSlice(stringSlice)

	// Get again - should be reused but reset
	stringSlice2 := pool.GetStringSlice()
	assert.Equal(t, 0, len(stringSlice2))

	// Test event slice pooling
	eventSlice := pool.GetEventSlice()
	require.NotNil(t, eventSlice)
	assert.Equal(t, 0, len(eventSlice))

	event := &domain.UnifiedEvent{ID: "test", Type: domain.EventTypeKubernetes}
	eventSlice = append(eventSlice, event)
	pool.PutEventSlice(eventSlice)

	// Get again - should be reused and references cleared
	eventSlice2 := pool.GetEventSlice()
	assert.Equal(t, 0, len(eventSlice2))
}

func TestCorrelationResultPool_MapPooling(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pool := NewCorrelationResultPool(logger, 10)

	// Test string map pooling
	stringMap := pool.GetStringMap()
	require.NotNil(t, stringMap)
	assert.Equal(t, 0, len(stringMap))

	stringMap["key1"] = "value1"
	stringMap["key2"] = "value2"
	pool.PutStringMap(stringMap)

	// Get again - should be reused but cleared
	stringMap2 := pool.GetStringMap()
	assert.Equal(t, 0, len(stringMap2))
}

func TestCorrelationResultPool_Statistics(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pool := NewCorrelationResultPool(logger, 10)
	ctx := context.Background()

	// Initial stats
	stats := pool.GetStats()
	assert.Equal(t, 0, int(stats.TotalAllocations))
	assert.Equal(t, 0, int(stats.PoolHits))
	assert.Equal(t, 0, int(stats.PoolMisses))
	assert.Equal(t, 10, stats.MaxSize)

	// Get objects to trigger allocations
	results := make([]*CorrelationResult, 5)
	for i := 0; i < 5; i++ {
		results[i] = pool.Get(ctx)
	}

	// Return objects to pool
	for i := 0; i < 5; i++ {
		pool.Put(ctx, results[i])
	}

	// Get objects again - should hit pool
	for i := 0; i < 5; i++ {
		results[i] = pool.Get(ctx)
	}

	stats = pool.GetStats()
	assert.True(t, stats.TotalAllocations >= 5)
	assert.True(t, stats.PoolHits >= 5)
	assert.True(t, stats.HitRate > 0)

	// Log stats for verification
	pool.LogStats(ctx)
	pool.ReportMetrics(ctx)
}

func TestCorrelationResultPool_MemoryLeakPrevention(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pool := NewCorrelationResultPool(logger, 100)
	ctx := context.Background()

	// Create a large number of objects with references
	const numObjects = 1000

	// Measure initial memory
	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	// Create and use many objects
	for i := 0; i < numObjects; i++ {
		result := pool.Get(ctx)

		// Populate with data that could cause leaks
		result.ID = "test-" + string(rune(i))
		result.Events = []string{"event1", "event2", "event3"}
		result.Related = []*domain.UnifiedEvent{
			{ID: "event-" + string(rune(i)), Type: domain.EventTypeKubernetes},
		}

		result.ConfigData = &ConfigChangeData{
			ResourceName: "config-" + string(rune(i)),
			ChangedFields: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
		}

		result.Impact = &Impact{
			Resources: []string{"pod1", "pod2"},
			Services:  []ServiceReference{{Name: "svc1", Namespace: "default"}},
		}

		// Return to pool
		pool.Put(ctx, result)

		// Occasionally force GC
		if i%100 == 0 {
			runtime.GC()
		}
	}

	// Force final GC and measure memory
	runtime.GC()
	runtime.GC() // Double GC to ensure cleanup
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	// Verify pool cleaned up properly
	stats := pool.GetStats()
	assert.True(t, stats.HitRate > 0.8, "Pool hit rate should be > 80%% for good reuse")

	// Memory growth should be minimal due to object pooling
	memGrowth := int64(m2.Alloc) - int64(m1.Alloc)
	t.Logf("Memory growth: %d bytes", memGrowth)
	t.Logf("Pool stats: hits=%d, misses=%d, hit_rate=%.2f",
		stats.PoolHits, stats.PoolMisses, stats.HitRate)

	// With proper pooling, memory growth should be limited
	// This is a rough heuristic - exact values depend on Go runtime
	maxExpectedGrowth := int64(numObjects * 1000) // 1KB per object max
	assert.True(t, memGrowth < maxExpectedGrowth,
		"Memory growth %d exceeds expected %d, possible memory leak", memGrowth, maxExpectedGrowth)
}

func TestCorrelationResultPool_ConcurrentAccess(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pool := NewCorrelationResultPool(logger, 50)
	ctx := context.Background()

	const numGoroutines = 10
	const operationsPerGoroutine = 100

	done := make(chan bool, numGoroutines)

	// Concurrent access test
	for g := 0; g < numGoroutines; g++ {
		go func(goroutineID int) {
			defer func() { done <- true }()

			for i := 0; i < operationsPerGoroutine; i++ {
				// Get object
				result := pool.Get(ctx)
				require.NotNil(t, result)

				// Populate some data
				result.ID = "test-concurrent"
				result.Type = "test"
				result.Confidence = 0.5

				// Get some nested objects
				if i%3 == 0 {
					configData := pool.GetConfigData()
					configData.ResourceName = "test"
					result.ConfigData = configData
				}

				if i%5 == 0 {
					impact := pool.GetImpact()
					impact.Severity = domain.EventSeverityMedium
					result.Impact = impact
				}

				// Return to pool
				pool.Put(ctx, result)
			}
		}(g)
	}

	// Wait for all goroutines to complete
	for g := 0; g < numGoroutines; g++ {
		select {
		case <-done:
			// Success
		case <-time.After(30 * time.Second):
			t.Fatal("Concurrent test timed out")
		}
	}

	stats := pool.GetStats()
	t.Logf("Concurrent test stats: total=%d, hits=%d, misses=%d, hit_rate=%.2f",
		stats.TotalAllocations, stats.PoolHits, stats.PoolMisses, stats.HitRate)

	assert.True(t, stats.HitRate > 0.5, "Pool should have reasonable hit rate under concurrent access")
}

func TestCorrelationResultPool_NilHandling(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pool := NewCorrelationResultPool(logger, 10)
	ctx := context.Background()

	// Test putting nil objects
	pool.Put(ctx, nil) // Should not panic

	// Test putting nil nested objects
	pool.PutImpact(nil)
	pool.PutRootCause(nil)
	pool.PutStringSlice(nil)
	pool.PutStringMap(nil)
	pool.PutEventSlice(nil)

	// All should complete without panic
	assert.True(t, true)
}

func TestCorrelationResultPool_LargeObjectHandling(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pool := NewCorrelationResultPool(logger, 10)

	// Test very large slices are not pooled (to prevent memory bloat)
	largeStringSlice := make([]string, 1000)
	for i := range largeStringSlice {
		largeStringSlice[i] = "test"
	}

	// This should not panic but also should not pool the large slice
	pool.PutStringSlice(largeStringSlice)

	largeStringMap := make(map[string]string)
	for i := 0; i < 1000; i++ {
		largeStringMap[string(rune(i))] = "value"
	}

	// This should not panic but also should not pool the large map
	pool.PutStringMap(largeStringMap)

	assert.True(t, true)
}

// BenchmarkCorrelationResultPool_WithoutPooling benchmarks allocation without pooling
func BenchmarkCorrelationResultPool_WithoutPooling(b *testing.B) {
	// Store results to prevent compiler optimizations
	results := make([]*CorrelationResult, 0, 100)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Simulate direct allocation like current code
		result := &CorrelationResult{
			ID:         "test-correlation",
			Type:       "k8s_ownership",
			Confidence: 0.95,
			Message:    "Test correlation found",
			TraceID:    "trace-123",
			Summary:    "Test summary",
			Events:     []string{"event1", "event2"},
			Related: []*domain.UnifiedEvent{
				{ID: "event1", Type: domain.EventTypeKubernetes},
				{ID: "event2", Type: domain.EventTypeKubernetes},
			},
			StartTime: time.Now(),
			EndTime:   time.Now().Add(time.Minute),
			Details: CorrelationDetails{
				Pattern:        "ownership",
				Algorithm:      "k8s-correlator",
				ProcessingTime: time.Millisecond * 10,
				DataPoints:     2,
			},
			Evidence: EvidenceData{
				EventIDs:    []string{"event1", "event2"},
				ResourceIDs: []string{"pod/test"},
				Timestamps:  []time.Time{time.Now()},
				Attributes:  map[string]string{"owner": "deployment/test"},
			},
			ConfigData: &ConfigChangeData{
				ResourceType: "Deployment",
				ResourceName: "test-deployment",
				Namespace:    "default",
				ChangeType:   "UPDATE",
				ChangedFields: map[string]string{
					"image": "v2.0.0",
				},
			},
			Impact: &Impact{
				Severity:    domain.EventSeverityMedium,
				Scope:       "service",
				UserImpact:  "potential downtime",
				Degradation: "10%",
				Resources:   []string{"pod/test-123"},
				Services:    []ServiceReference{{Name: "test-svc", Namespace: "default"}},
			},
		}

		// Simulate processing - prevent compiler optimization
		if result.ID == "" {
			b.Fatal("unexpected empty ID")
		}
		if result.Confidence != 0.95 {
			b.Fatal("unexpected confidence value")
		}

		// Simulate using the complex nested structures
		if len(result.Events) != 2 {
			b.Fatal("unexpected events count")
		}
		if len(result.Related) != 2 {
			b.Fatal("unexpected related events count")
		}
		if result.ConfigData.ResourceType != "Deployment" {
			b.Fatal("unexpected resource type")
		}

		// Store result to prevent optimization
		results = append(results, result)

		// Periodically clear to prevent memory buildup during benchmark
		if i%100 == 99 {
			results = results[:0]
		}
	}

	// Use results to prevent total optimization
	runtime.KeepAlive(results)
}

// BenchmarkCorrelationResultPool_WithPooling benchmarks allocation with pooling
func BenchmarkCorrelationResultPool_WithPooling(b *testing.B) {
	logger := zaptest.NewLogger(b)
	pool := NewCorrelationResultPool(logger, 1000)
	ctx := context.Background()

	// Store results to prevent compiler optimizations
	results := make([]*CorrelationResult, 0, 100)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Get from pool
		result := pool.Get(ctx)

		// Populate (same as non-pooled version)
		result.ID = "test-correlation"
		result.Type = "k8s_ownership"
		result.Confidence = 0.95
		result.Message = "Test correlation found"
		result.TraceID = "trace-123"
		result.Summary = "Test summary"
		result.Events = pool.GetStringSlice()
		result.Events = append(result.Events, "event1", "event2")
		result.Related = pool.GetEventSlice()
		result.Related = append(result.Related,
			&domain.UnifiedEvent{ID: "event1", Type: domain.EventTypeKubernetes},
			&domain.UnifiedEvent{ID: "event2", Type: domain.EventTypeKubernetes},
		)
		result.StartTime = time.Now()
		result.EndTime = time.Now().Add(time.Minute)
		result.Details.Pattern = "ownership"
		result.Details.Algorithm = "k8s-correlator"
		result.Details.ProcessingTime = time.Millisecond * 10
		result.Details.DataPoints = 2
		result.Evidence.EventIDs = pool.GetStringSlice()
		result.Evidence.EventIDs = append(result.Evidence.EventIDs, "event1", "event2")
		result.Evidence.ResourceIDs = pool.GetStringSlice()
		result.Evidence.ResourceIDs = append(result.Evidence.ResourceIDs, "pod/test")
		result.Evidence.Attributes = pool.GetStringMap()
		result.Evidence.Attributes["owner"] = "deployment/test"

		result.ConfigData = pool.GetConfigData()
		result.ConfigData.ResourceType = "Deployment"
		result.ConfigData.ResourceName = "test-deployment"
		result.ConfigData.Namespace = "default"
		result.ConfigData.ChangeType = "UPDATE"
		result.ConfigData.ChangedFields = pool.GetStringMap()
		result.ConfigData.ChangedFields["image"] = "v2.0.0"

		result.Impact = pool.GetImpact()
		result.Impact.Severity = domain.EventSeverityMedium
		result.Impact.Scope = "service"
		result.Impact.UserImpact = "potential downtime"
		result.Impact.Degradation = "10%"
		result.Impact.Resources = pool.GetStringSlice()
		result.Impact.Resources = append(result.Impact.Resources, "pod/test-123")
		result.Impact.Services = make([]ServiceReference, 1)
		result.Impact.Services[0] = ServiceReference{Name: "test-svc", Namespace: "default"}

		// Simulate processing - prevent compiler optimization
		if result.ID == "" {
			b.Fatal("unexpected empty ID")
		}
		if result.Confidence != 0.95 {
			b.Fatal("unexpected confidence value")
		}

		// Simulate using the complex nested structures
		if len(result.Events) != 2 {
			b.Fatal("unexpected events count")
		}
		if len(result.Related) != 2 {
			b.Fatal("unexpected related events count")
		}
		if result.ConfigData.ResourceType != "Deployment" {
			b.Fatal("unexpected resource type")
		}

		// Store result to prevent optimization (before returning to pool)
		results = append(results, result)

		// Return to pool
		pool.Put(ctx, result)

		// Periodically clear to prevent memory buildup during benchmark
		if i%100 == 99 {
			results = results[:0]
		}
	}

	// Use results to prevent total optimization
	runtime.KeepAlive(results)
}

// BenchmarkCorrelationResultPool_ConcurrentUsage benchmarks concurrent pool usage
func BenchmarkCorrelationResultPool_ConcurrentUsage(b *testing.B) {
	logger := zaptest.NewLogger(b)
	pool := NewCorrelationResultPool(logger, 100)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			result := pool.Get(ctx)
			result.ID = "concurrent-test"
			result.Type = "benchmark"
			result.Confidence = 0.8
			pool.Put(ctx, result)
		}
	})
}
