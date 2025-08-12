package correlation

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

// BenchmarkProductionScenario_WithoutPooling simulates production usage without pooling
func BenchmarkProductionScenario_WithoutPooling(b *testing.B) {
	// Simulate production scenario: process events in batches
	const batchSize = 100

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Process a batch of correlation results
		batch := make([]*CorrelationResult, 0, batchSize)

		for j := 0; j < batchSize; j++ {
			result := &CorrelationResult{
				ID:         "correlation-" + string(rune(j)),
				Type:       "k8s_ownership",
				Confidence: 0.85,
				Message:    "Kubernetes ownership correlation found",
				Summary:    "Pod ownership traced to deployment",
				Events:     []string{"event1", "event2", "event3"},
				Related: []*domain.UnifiedEvent{
					{ID: "pod-event", Type: domain.EventTypeKubernetes},
					{ID: "deployment-event", Type: domain.EventTypeKubernetes},
				},
				StartTime: time.Now(),
				EndTime:   time.Now().Add(time.Second),
				Details: CorrelationDetails{
					Pattern:        "ownership",
					Algorithm:      "k8s-correlator",
					ProcessingTime: time.Millisecond * 5,
					DataPoints:     3,
				},
				Evidence: EvidenceData{
					EventIDs:    []string{"event1", "event2"},
					ResourceIDs: []string{"pod/test", "deployment/test"},
					Attributes:  map[string]string{"owner": "deployment/test"},
				},
				ConfigData: &ConfigChangeData{
					ResourceType:  "Deployment",
					ResourceName:  "test-deployment",
					Namespace:     "default",
					ChangeType:    "UPDATE",
					ChangedFields: map[string]string{"image": "v2.0.0"},
				},
				Impact: &Impact{
					Severity:   domain.EventSeverityMedium,
					Scope:      "service",
					UserImpact: "minimal downtime",
					Resources:  []string{"pod/test-123", "service/test-svc"},
					Services:   []ServiceReference{{Name: "test", Namespace: "default"}},
				},
			}

			// Simulate processing the result
			processCorrelationResult(result)
			batch = append(batch, result)
		}

		// Simulate batch processing completion - results become eligible for GC
		for j := range batch {
			batch[j] = nil
		}
		batch = nil

		// Periodically force GC to simulate production memory pressure
		if i%10 == 0 {
			runtime.GC()
		}
	}
}

// BenchmarkProductionScenario_WithPooling simulates production usage with pooling
func BenchmarkProductionScenario_WithPooling(b *testing.B) {
	logger := zaptest.NewLogger(b)
	pool := NewCorrelationResultPool(logger, 200) // Pool size for realistic reuse
	ctx := context.Background()

	// Simulate production scenario: process events in batches
	const batchSize = 100

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Process a batch of correlation results using pool
		batch := make([]*CorrelationResult, 0, batchSize)

		for j := 0; j < batchSize; j++ {
			// Get from pool instead of allocating
			result := pool.Get(ctx)

			// Populate with same data
			result.ID = "correlation-" + string(rune(j))
			result.Type = "k8s_ownership"
			result.Confidence = 0.85
			result.Message = "Kubernetes ownership correlation found"
			result.Summary = "Pod ownership traced to deployment"
			result.Events = pool.GetStringSlice()
			result.Events = append(result.Events, "event1", "event2", "event3")
			result.Related = pool.GetEventSlice()
			result.Related = append(result.Related,
				&domain.UnifiedEvent{ID: "pod-event", Type: domain.EventTypeKubernetes},
				&domain.UnifiedEvent{ID: "deployment-event", Type: domain.EventTypeKubernetes},
			)
			result.StartTime = time.Now()
			result.EndTime = time.Now().Add(time.Second)

			result.Details.Pattern = "ownership"
			result.Details.Algorithm = "k8s-correlator"
			result.Details.ProcessingTime = time.Millisecond * 5
			result.Details.DataPoints = 3

			result.Evidence.EventIDs = pool.GetStringSlice()
			result.Evidence.EventIDs = append(result.Evidence.EventIDs, "event1", "event2")
			result.Evidence.ResourceIDs = pool.GetStringSlice()
			result.Evidence.ResourceIDs = append(result.Evidence.ResourceIDs, "pod/test", "deployment/test")
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
			result.Impact.UserImpact = "minimal downtime"
			result.Impact.Resources = pool.GetStringSlice()
			result.Impact.Resources = append(result.Impact.Resources, "pod/test-123", "service/test-svc")
			result.Impact.Services = make([]ServiceReference, 1)
			result.Impact.Services[0] = ServiceReference{Name: "test", Namespace: "default"}

			// Simulate processing the result
			processCorrelationResult(result)
			batch = append(batch, result)
		}

		// Return all results to pool when batch processing is done
		for _, result := range batch {
			pool.Put(ctx, result)
		}
		batch = nil

		// Periodically force GC to simulate production memory pressure
		if i%10 == 0 {
			runtime.GC()
		}
	}
}

// BenchmarkGCPressure_WithoutPooling measures GC overhead without pooling
func BenchmarkGCPressure_WithoutPooling(b *testing.B) {
	const (
		goroutines = 10
		iterations = 100
	)

	b.ResetTimer()
	b.ReportAllocs()

	var wg sync.WaitGroup

	for i := 0; i < b.N; i++ {
		wg.Add(goroutines)

		for g := 0; g < goroutines; g++ {
			go func() {
				defer wg.Done()

				for j := 0; j < iterations; j++ {
					result := createComplexCorrelationResult(j)
					processCorrelationResult(result)
					// Let result go out of scope for GC
				}
			}()
		}

		wg.Wait()

		// Force GC to measure pressure
		runtime.GC()
	}
}

// BenchmarkGCPressure_WithPooling measures GC overhead with pooling
func BenchmarkGCPressure_WithPooling(b *testing.B) {
	logger := zaptest.NewLogger(b)
	pool := NewCorrelationResultPool(logger, 100)
	ctx := context.Background()

	const (
		goroutines = 10
		iterations = 100
	)

	b.ResetTimer()
	b.ReportAllocs()

	var wg sync.WaitGroup

	for i := 0; i < b.N; i++ {
		wg.Add(goroutines)

		for g := 0; g < goroutines; g++ {
			go func() {
				defer wg.Done()

				for j := 0; j < iterations; j++ {
					result := pool.Get(ctx)
					populateCorrelationResultFromPool(pool, result, j)
					processCorrelationResult(result)
					pool.Put(ctx, result)
				}
			}()
		}

		wg.Wait()

		// Force GC to measure pressure
		runtime.GC()
	}
}

// Helper function to simulate realistic correlation result processing
func processCorrelationResult(result *CorrelationResult) {
	// Simulate some processing work that accesses the data
	_ = len(result.ID)
	_ = result.Confidence > 0.5
	_ = len(result.Events)
	_ = len(result.Related)

	if result.ConfigData != nil {
		_ = result.ConfigData.ResourceType
	}
	if result.Impact != nil {
		_ = len(result.Impact.Resources)
	}
}

// Helper function to create complex correlation result without pool
func createComplexCorrelationResult(id int) *CorrelationResult {
	return &CorrelationResult{
		ID:         "correlation-" + string(rune(id)),
		Type:       "complex_correlation",
		Confidence: 0.9,
		Message:    "Complex correlation with nested data",
		Summary:    "Multi-resource correlation found",
		Events:     []string{"event1", "event2", "event3", "event4"},
		Related: []*domain.UnifiedEvent{
			{ID: "event-" + string(rune(id)), Type: domain.EventTypeKubernetes},
			{ID: "related-" + string(rune(id)), Type: domain.EventTypeSystem},
		},
		StartTime: time.Now(),
		EndTime:   time.Now().Add(time.Minute),
		Details: CorrelationDetails{
			Pattern:        "complex-pattern",
			Algorithm:      "multi-correlator",
			ProcessingTime: time.Millisecond * 10,
			DataPoints:     4,
		},
		Evidence: EvidenceData{
			EventIDs:    []string{"event1", "event2", "event3"},
			ResourceIDs: []string{"pod/test", "service/test", "deployment/test"},
			Attributes: map[string]string{
				"correlation_type": "complex",
				"severity":         "high",
				"namespace":        "default",
			},
		},
		ConfigData: &ConfigChangeData{
			ResourceType:  "Service",
			ResourceName:  "complex-service",
			Namespace:     "production",
			ChangeType:    "UPDATE",
			ChangedFields: map[string]string{"replicas": "3", "image": "v1.5.0"},
		},
		DependencyData: &DependencyData{
			SourceService:   ServiceReference{Name: "frontend", Namespace: "default"},
			TargetService:   ServiceReference{Name: "backend", Namespace: "default"},
			DependencyType:  "http",
			Direction:       "outbound",
			Strength:        0.95,
			ObservedLatency: time.Millisecond * 50,
		},
		Impact: &Impact{
			Severity:    domain.EventSeverityHigh,
			Scope:       "cluster",
			UserImpact:  "service degradation",
			Degradation: "25%",
			Resources:   []string{"pod/frontend-123", "pod/backend-456", "service/api"},
			Services:    []ServiceReference{{Name: "api", Namespace: "production"}},
		},
	}
}

// Helper function to populate correlation result from pool
func populateCorrelationResultFromPool(pool *CorrelationResultPool, result *CorrelationResult, id int) {
	result.ID = "correlation-" + string(rune(id))
	result.Type = "complex_correlation"
	result.Confidence = 0.9
	result.Message = "Complex correlation with nested data"
	result.Summary = "Multi-resource correlation found"
	result.Events = pool.GetStringSlice()
	result.Events = append(result.Events, "event1", "event2", "event3", "event4")
	result.Related = pool.GetEventSlice()
	result.Related = append(result.Related,
		&domain.UnifiedEvent{ID: "event-" + string(rune(id)), Type: domain.EventTypeKubernetes},
		&domain.UnifiedEvent{ID: "related-" + string(rune(id)), Type: domain.EventTypeSystem},
	)
	result.StartTime = time.Now()
	result.EndTime = time.Now().Add(time.Minute)

	result.Details.Pattern = "complex-pattern"
	result.Details.Algorithm = "multi-correlator"
	result.Details.ProcessingTime = time.Millisecond * 10
	result.Details.DataPoints = 4

	result.Evidence.EventIDs = pool.GetStringSlice()
	result.Evidence.EventIDs = append(result.Evidence.EventIDs, "event1", "event2", "event3")
	result.Evidence.ResourceIDs = pool.GetStringSlice()
	result.Evidence.ResourceIDs = append(result.Evidence.ResourceIDs, "pod/test", "service/test", "deployment/test")
	result.Evidence.Attributes = pool.GetStringMap()
	result.Evidence.Attributes["correlation_type"] = "complex"
	result.Evidence.Attributes["severity"] = "high"
	result.Evidence.Attributes["namespace"] = "default"

	result.ConfigData = pool.GetConfigData()
	result.ConfigData.ResourceType = "Service"
	result.ConfigData.ResourceName = "complex-service"
	result.ConfigData.Namespace = "production"
	result.ConfigData.ChangeType = "UPDATE"
	result.ConfigData.ChangedFields = pool.GetStringMap()
	result.ConfigData.ChangedFields["replicas"] = "3"
	result.ConfigData.ChangedFields["image"] = "v1.5.0"

	result.DependencyData = pool.GetDependencyData()
	result.DependencyData.SourceService = ServiceReference{Name: "frontend", Namespace: "default"}
	result.DependencyData.TargetService = ServiceReference{Name: "backend", Namespace: "default"}
	result.DependencyData.DependencyType = "http"
	result.DependencyData.Direction = "outbound"
	result.DependencyData.Strength = 0.95
	result.DependencyData.ObservedLatency = time.Millisecond * 50

	result.Impact = pool.GetImpact()
	result.Impact.Severity = domain.EventSeverityHigh
	result.Impact.Scope = "cluster"
	result.Impact.UserImpact = "service degradation"
	result.Impact.Degradation = "25%"
	result.Impact.Resources = pool.GetStringSlice()
	result.Impact.Resources = append(result.Impact.Resources, "pod/frontend-123", "pod/backend-456", "service/api")
	result.Impact.Services = make([]ServiceReference, 1)
	result.Impact.Services[0] = ServiceReference{Name: "api", Namespace: "production"}
}
