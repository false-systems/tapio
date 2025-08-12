package correlation

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

// BenchmarkMemoryUsage_WithoutPooling measures actual memory allocation patterns without pooling
func BenchmarkMemoryUsage_WithoutPooling(b *testing.B) {
	// Run GC and get baseline memory stats
	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	b.ResetTimer()

	// Simulate creating many correlation results over time
	for i := 0; i < b.N; i++ {
		// Create batch of correlations
		correlations := make([]*CorrelationResult, 50)

		for j := range correlations {
			correlations[j] = &CorrelationResult{
				ID:         "correlation-" + string(rune(i*50+j)),
				Type:       "test-correlation",
				Confidence: 0.85,
				Message:    "Test correlation message",
				Summary:    "Test summary",
				Events:     []string{"event1", "event2", "event3"},
				Related: []*domain.UnifiedEvent{
					{ID: "related-1", Type: domain.EventTypeKubernetes},
					{ID: "related-2", Type: domain.EventTypeSystem},
				},
				StartTime: time.Now(),
				EndTime:   time.Now().Add(time.Second),
				Details: CorrelationDetails{
					Pattern:        "test-pattern",
					Algorithm:      "test-algorithm",
					ProcessingTime: time.Millisecond,
					DataPoints:     3,
				},
				Evidence: EvidenceData{
					EventIDs:    []string{"event1", "event2"},
					ResourceIDs: []string{"resource1", "resource2"},
					Attributes:  map[string]string{"key": "value"},
				},
				ConfigData: &ConfigChangeData{
					ResourceType:  "TestResource",
					ResourceName:  "test-resource",
					Namespace:     "test",
					ChangeType:    "UPDATE",
					ChangedFields: map[string]string{"field": "value"},
				},
			}
		}

		// Simulate some processing on the correlations
		for _, corr := range correlations {
			_ = len(corr.ID)
			_ = corr.Confidence > 0.5
		}

		// Clear references to make eligible for GC
		for j := range correlations {
			correlations[j] = nil
		}

		// Force GC every 10 iterations
		if i%10 == 0 {
			runtime.GC()
		}
	}

	b.StopTimer()

	// Final GC and memory measurement
	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	// Report memory usage
	b.ReportMetric(float64(m2.TotalAlloc-m1.TotalAlloc), "total-bytes")
	b.ReportMetric(float64(m2.Mallocs-m1.Mallocs), "mallocs")
	b.ReportMetric(float64(m2.NumGC-m1.NumGC), "gc-cycles")
}

// BenchmarkMemoryUsage_WithPooling measures actual memory allocation patterns with pooling
func BenchmarkMemoryUsage_WithPooling(b *testing.B) {
	logger := zaptest.NewLogger(b)
	pool := NewCorrelationResultPool(logger, 100)
	ctx := context.Background()

	// Run GC and get baseline memory stats
	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	b.ResetTimer()

	// Simulate creating many correlation results over time using pool
	for i := 0; i < b.N; i++ {
		// Create batch of correlations using pool
		correlations := make([]*CorrelationResult, 50)

		for j := range correlations {
			result := pool.Get(ctx)

			// Populate with same data as non-pooled version
			result.ID = "correlation-" + string(rune(i*50+j))
			result.Type = "test-correlation"
			result.Confidence = 0.85
			result.Message = "Test correlation message"
			result.Summary = "Test summary"
			result.Events = pool.GetStringSlice()
			result.Events = append(result.Events, "event1", "event2", "event3")
			result.Related = pool.GetEventSlice()
			result.Related = append(result.Related,
				&domain.UnifiedEvent{ID: "related-1", Type: domain.EventTypeKubernetes},
				&domain.UnifiedEvent{ID: "related-2", Type: domain.EventTypeSystem},
			)
			result.StartTime = time.Now()
			result.EndTime = time.Now().Add(time.Second)

			result.Details.Pattern = "test-pattern"
			result.Details.Algorithm = "test-algorithm"
			result.Details.ProcessingTime = time.Millisecond
			result.Details.DataPoints = 3

			result.Evidence.EventIDs = pool.GetStringSlice()
			result.Evidence.EventIDs = append(result.Evidence.EventIDs, "event1", "event2")
			result.Evidence.ResourceIDs = pool.GetStringSlice()
			result.Evidence.ResourceIDs = append(result.Evidence.ResourceIDs, "resource1", "resource2")
			result.Evidence.Attributes = pool.GetStringMap()
			result.Evidence.Attributes["key"] = "value"

			result.ConfigData = pool.GetConfigData()
			result.ConfigData.ResourceType = "TestResource"
			result.ConfigData.ResourceName = "test-resource"
			result.ConfigData.Namespace = "test"
			result.ConfigData.ChangeType = "UPDATE"
			result.ConfigData.ChangedFields = pool.GetStringMap()
			result.ConfigData.ChangedFields["field"] = "value"

			correlations[j] = result
		}

		// Simulate some processing on the correlations
		for _, corr := range correlations {
			_ = len(corr.ID)
			_ = corr.Confidence > 0.5
		}

		// Return all objects to pool
		for _, corr := range correlations {
			pool.Put(ctx, corr)
		}

		// Force GC every 10 iterations
		if i%10 == 0 {
			runtime.GC()
		}
	}

	b.StopTimer()

	// Final GC and memory measurement
	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	// Report memory usage
	b.ReportMetric(float64(m2.TotalAlloc-m1.TotalAlloc), "total-bytes")
	b.ReportMetric(float64(m2.Mallocs-m1.Mallocs), "mallocs")
	b.ReportMetric(float64(m2.NumGC-m1.NumGC), "gc-cycles")

	// Report pool statistics
	stats := pool.GetStats()
	b.ReportMetric(stats.HitRate*100, "hit-rate-%")
	b.ReportMetric(float64(stats.PoolHits), "pool-hits")
}

// BenchmarkRealWorldScenario_WithoutPooling simulates a realistic correlation engine workload
func BenchmarkRealWorldScenario_WithoutPooling(b *testing.B) {
	b.StopTimer()

	// Simulate correlation engine processing events continuously
	for i := 0; i < b.N; i++ {
		b.StartTimer()

		// Process 1000 events, creating correlations for some of them
		eventCount := 1000
		correlationCount := 0

		for eventID := 0; eventID < eventCount; eventID++ {
			// Simulate: 20% of events result in correlations
			if eventID%5 == 0 {
				correlation := &CorrelationResult{
					ID:         "correlation-" + string(rune(correlationCount)),
					Type:       "pattern-match",
					Confidence: 0.75 + float64(eventID%25)/100, // Vary confidence
					Message:    "Pattern-based correlation detected",
					Summary:    "System behavior pattern identified",
					Events:     []string{"event-" + string(rune(eventID)), "event-" + string(rune(eventID-1))},
					Related: []*domain.UnifiedEvent{
						{ID: "event-" + string(rune(eventID)), Type: domain.EventTypeSystem},
					},
					StartTime: time.Now().Add(-time.Minute),
					EndTime:   time.Now(),
					Details: CorrelationDetails{
						Pattern:        "behavioral-pattern",
						Algorithm:      "pattern-matcher",
						ProcessingTime: time.Millisecond * time.Duration(1+eventID%10),
						DataPoints:     2,
					},
					Evidence: EvidenceData{
						EventIDs:    []string{"event-" + string(rune(eventID))},
						ResourceIDs: []string{"resource-" + string(rune(eventID/10))},
						Attributes: map[string]string{
							"pattern_type": "behavior",
							"confidence":   "high",
						},
					},
				}

				// Simulate correlation processing
				_ = len(correlation.ID)
				_ = correlation.Confidence > 0.7
				_ = len(correlation.Events)

				correlationCount++

				// Correlation goes out of scope and becomes eligible for GC
			}
		}

		b.StopTimer()

		// Simulate periodic GC pressure in real system
		runtime.GC()

		b.StartTimer()
	}
}

// BenchmarkRealWorldScenario_WithPooling simulates realistic workload with pooling
func BenchmarkRealWorldScenario_WithPooling(b *testing.B) {
	logger := zaptest.NewLogger(b)
	pool := NewCorrelationResultPool(logger, 50) // Realistic pool size
	ctx := context.Background()

	b.StopTimer()

	// Simulate correlation engine processing events continuously
	for i := 0; i < b.N; i++ {
		b.StartTimer()

		// Process 1000 events, creating correlations for some of them
		eventCount := 1000
		correlationCount := 0

		for eventID := 0; eventID < eventCount; eventID++ {
			// Simulate: 20% of events result in correlations
			if eventID%5 == 0 {
				correlation := pool.Get(ctx)

				// Populate correlation
				correlation.ID = "correlation-" + string(rune(correlationCount))
				correlation.Type = "pattern-match"
				correlation.Confidence = 0.75 + float64(eventID%25)/100 // Vary confidence
				correlation.Message = "Pattern-based correlation detected"
				correlation.Summary = "System behavior pattern identified"
				correlation.Events = pool.GetStringSlice()
				correlation.Events = append(correlation.Events,
					"event-"+string(rune(eventID)), "event-"+string(rune(eventID-1)))
				correlation.Related = pool.GetEventSlice()
				correlation.Related = append(correlation.Related,
					&domain.UnifiedEvent{ID: "event-" + string(rune(eventID)), Type: domain.EventTypeSystem},
				)
				correlation.StartTime = time.Now().Add(-time.Minute)
				correlation.EndTime = time.Now()

				correlation.Details.Pattern = "behavioral-pattern"
				correlation.Details.Algorithm = "pattern-matcher"
				correlation.Details.ProcessingTime = time.Millisecond * time.Duration(1+eventID%10)
				correlation.Details.DataPoints = 2

				correlation.Evidence.EventIDs = pool.GetStringSlice()
				correlation.Evidence.EventIDs = append(correlation.Evidence.EventIDs, "event-"+string(rune(eventID)))
				correlation.Evidence.ResourceIDs = pool.GetStringSlice()
				correlation.Evidence.ResourceIDs = append(correlation.Evidence.ResourceIDs, "resource-"+string(rune(eventID/10)))
				correlation.Evidence.Attributes = pool.GetStringMap()
				correlation.Evidence.Attributes["pattern_type"] = "behavior"
				correlation.Evidence.Attributes["confidence"] = "high"

				// Simulate correlation processing
				_ = len(correlation.ID)
				_ = correlation.Confidence > 0.7
				_ = len(correlation.Events)

				// Return to pool when done processing
				pool.Put(ctx, correlation)

				correlationCount++
			}
		}

		b.StopTimer()

		// Simulate periodic GC pressure in real system
		runtime.GC()

		b.StartTimer()
	}
}
