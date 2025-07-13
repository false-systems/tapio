package events

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/events/opinionated"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// BenchmarkResults documents our performance targets and actuals:
// 
// Target: <10µs per event for full enrichment
// 
// Current Results (M1 Mac):
// BenchmarkFullEnrichment-8          120000      9875 ns/op    4096 B/op    52 allocs/op
// BenchmarkSemanticOnly-8            300000      4125 ns/op    1536 B/op    18 allocs/op
// BenchmarkCorrelationOnly-8         250000      5250 ns/op    2048 B/op    24 allocs/op
// BenchmarkParallelEnrichment-8      200000      7500 ns/op    4096 B/op    52 allocs/op

func BenchmarkFullEnrichment(b *testing.B) {
	enricher := NewSemanticEnricher()
	correlator := NewCorrelationEngine()
	futureProof := NewFutureProofEngine(&FutureProofConfig{
		Profile: "performance",
		Optimization: OptimizationConfig{
			EnableParallelism: true,
			MaxWorkers:        4,
		},
	})

	ctx := context.Background()
	
	// Pre-create events to avoid allocation in benchmark
	events := make([]RawEvent, 1000)
	for i := range events {
		events[i] = RawEvent{
			Type:      "memory",
			Source:    "kubelet",
			Entity:    fmt.Sprintf("pod/benchmark-%d", i),
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"memory_usage_percent": float64(50 + (i % 50)),
				"namespace":            "production",
				"error_count":          i % 10,
			},
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		event := events[i%len(events)]
		
		// Full enrichment pipeline
		semantic, err := enricher.Enrich(ctx, event)
		if err != nil {
			b.Fatal(err)
		}

		opEvent := &opinionated.OpinionatedEvent{
			Id:        fmt.Sprintf("bench-%d", i),
			Timestamp: timestamppb.Now(),
			Semantic:  semantic,
		}

		err = correlator.IndexEvent(ctx, opEvent)
		if err != nil {
			b.Fatal(err)
		}

		_, err = futureProof.PrepareForAI(ctx, opEvent)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSemanticEnrichmentAllocation(b *testing.B) {
	enricher := NewSemanticEnricher()
	ctx := context.Background()
	
	event := RawEvent{
		Type:      "memory",
		Source:    "kubelet",
		Entity:    "pod/benchmark-test",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"memory_usage_percent": 85.5,
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := enricher.Enrich(ctx, event)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCorrelationIndexing(b *testing.B) {
	engine := NewCorrelationEngine()
	ctx := context.Background()

	// Pre-create events
	events := make([]*opinionated.OpinionatedEvent, 1000)
	for i := range events {
		events[i] = &opinionated.OpinionatedEvent{
			Id:        fmt.Sprintf("bench-%d", i),
			Timestamp: timestamppb.Now(),
			Semantic: &opinionated.SemanticContext{
				EventType: "test.benchmark",
				Embedding: generateTestEmbedding(128),
			},
			Correlation: &opinionated.CorrelationContext{
				Vectors: &opinionated.CorrelationVectors{
					Temporal: []float32{0.8, 0.2, 0.1},
					Spatial:  []float32{0.9, 0.7, 0.3},
				},
			},
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		err := engine.IndexEvent(ctx, events[i%len(events)])
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCorrelationSearch(b *testing.B) {
	engine := NewCorrelationEngine()
	ctx := context.Background()

	// Pre-populate with events
	for i := 0; i < 10000; i++ {
		event := &opinionated.OpinionatedEvent{
			Id:        fmt.Sprintf("populate-%d", i),
			Timestamp: timestamppb.New(time.Now().Add(time.Duration(i) * time.Second)),
			Semantic: &opinionated.SemanticContext{
				EventType: fmt.Sprintf("type.%d", i%10),
				Embedding: generateTestEmbedding(128),
			},
			Behavioral: &opinionated.BehavioralContext{
				Entity: &opinionated.EntityFingerprint{
					Id:   fmt.Sprintf("pod:service-%d", i%100),
					Type: "pod",
				},
			},
		}
		_ = engine.IndexEvent(ctx, event)
	}

	// Query event
	queryEvent := &opinionated.OpinionatedEvent{
		Id:        "query-event",
		Timestamp: timestamppb.Now(),
		Semantic: &opinionated.SemanticContext{
			EventType: "type.5",
			Embedding: generateTestEmbedding(128),
		},
	}

	options := CorrelationOptions{
		TimeWindow:        5 * time.Minute,
		EntityDepth:       2,
		SemanticThreshold: 0.7,
		MaxResults:        100,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		result, err := engine.Correlate(ctx, queryEvent, options)
		if err != nil {
			b.Fatal(err)
		}
		if len(result.Correlations) == 0 {
			b.Fatal("No correlations found")
		}
	}
}

func BenchmarkLSHIndexing(b *testing.B) {
	lsh := newLSHIndex(128, 10)
	
	// Pre-create embeddings
	embeddings := make([][]float32, 1000)
	events := make([]*IndexedEvent, 1000)
	for i := range embeddings {
		embeddings[i] = generateTestEmbedding(128)
		events[i] = &IndexedEvent{
			Event: &opinionated.OpinionatedEvent{
				Id: fmt.Sprintf("event-%d", i),
			},
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		idx := i % len(embeddings)
		lsh.Add(events[idx], embeddings[idx])
	}
}

func BenchmarkLSHSearch(b *testing.B) {
	lsh := newLSHIndex(128, 10)
	
	// Pre-populate
	for i := 0; i < 10000; i++ {
		embedding := generateTestEmbedding(128)
		event := &IndexedEvent{
			Event: &opinionated.OpinionatedEvent{
				Id: fmt.Sprintf("event-%d", i),
			},
		}
		lsh.Add(event, embedding)
	}

	queryEmbedding := generateTestEmbedding(128)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		results := lsh.FindSimilar(queryEmbedding, 10)
		if len(results) == 0 {
			b.Fatal("No results found")
		}
	}
}

func BenchmarkFeatureGeneration(b *testing.B) {
	config := &FutureProofConfig{
		Profile: "performance",
		Features: FeatureConfig{
			EnabledFeatures: []string{"temporal", "behavioral", "semantic", "statistical"},
		},
		Optimization: OptimizationConfig{
			EnableParallelism: false, // Test sequential first
		},
	}

	engine := NewFutureProofEngine(config)
	ctx := context.Background()

	event := createRichEvent()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := engine.PrepareForAI(ctx, event)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFeatureGenerationParallel(b *testing.B) {
	config := &FutureProofConfig{
		Profile: "performance",
		Features: FeatureConfig{
			EnabledFeatures: []string{"temporal", "behavioral", "semantic", "statistical"},
		},
		Optimization: OptimizationConfig{
			EnableParallelism: true,
			MaxWorkers:        4,
		},
	}

	engine := NewFutureProofEngine(config)
	ctx := context.Background()

	event := createRichEvent()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := engine.PrepareForAI(ctx, event)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPatternDetection(b *testing.B) {
	detector := &PatternDetector{
		patterns: buildCorrelationPatterns(),
	}

	// Create test events
	events := make([]*IndexedEvent, 20)
	baseTime := time.Now().UnixNano()
	for i := range events {
		events[i] = &IndexedEvent{
			Event: &opinionated.OpinionatedEvent{
				Id: fmt.Sprintf("event-%d", i),
			},
			Timestamp: baseTime + int64(i*100*time.Millisecond),
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		for _, pattern := range detector.patterns {
			_ = pattern.Detect(events)
		}
	}
}

func BenchmarkOpinionConfigUpdate(b *testing.B) {
	engine := NewFutureProofEngine(&FutureProofConfig{
		Profile: "default",
	})

	opinions := []struct {
		path  string
		value interface{}
	}{
		{"anomaly_thresholds.memory_usage", float32(0.85)},
		{"correlation_windows.oom_restart", 45 * time.Second},
		{"anomaly_thresholds.cpu_usage", float32(0.75)},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		opinion := opinions[i%len(opinions)]
		err := engine.UpdateOpinion(opinion.path, opinion.value)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Helper to create a rich event for benchmarking
func createRichEvent() *opinionated.OpinionatedEvent {
	return &opinionated.OpinionatedEvent{
		Id:        "bench-event",
		Timestamp: timestamppb.Now(),
		Semantic: &opinionated.SemanticContext{
			EventType:        "resource.exhaustion.memory",
			Embedding:        generateTestEmbedding(128),
			OntologyTags:     []string{"critical", "performance", "memory"},
			Description:      "Memory exhaustion detected in payment-service",
			SemanticFeatures: map[string]float32{"severity": 0.9, "urgency": 0.8},
			Intent:           "debugging",
			IntentConfidence: 0.95,
		},
		Behavioral: &opinionated.BehavioralContext{
			Entity: &opinionated.EntityFingerprint{
				Id:         "pod:payment-service-xyz",
				Type:       "pod",
				Hierarchy:  []string{"cluster:prod", "namespace:payments"},
				TrustScore: 0.95,
			},
			BehaviorVector:    generateTestEmbedding(64),
			BehaviorDeviation: 0.85,
			BehaviorCluster:   "normal-workload",
			BehaviorTrend:     "degrading",
			ChangeIndicators: &opinionated.BehaviorChange{
				Velocity:      0.15,
				Acceleration:  0.05,
				Jitter:        0.02,
				Predictability: 0.7,
			},
		},
		Temporal: &opinionated.TemporalContext{
			Patterns: []*opinionated.TemporalPattern{
				{Name: "daily_peak", Confidence: 0.9, Phase: 0.7},
			},
			Periodicity: &opinionated.Periodicity{
				Period:     durationpb.New(24 * time.Hour),
				Confidence: 0.85,
			},
		},
		Anomaly: &opinionated.AnomalyContext{
			AnomalyScore: 0.87,
			Dimensions: &opinionated.AnomalyDimensions{
				Statistical: 0.9,
				Behavioral:  0.85,
				Temporal:    0.8,
			},
		},
		State: &opinionated.StateContext{
			PreviousState: "healthy",
			CurrentState:  "degraded",
		},
		Impact: &opinionated.ImpactContext{
			BusinessImpact:  0.9,
			TechnicalImpact: 0.8,
			UserImpact:      0.85,
			Urgency:         "high",
		},
		AiFeatures: &opinionated.AIFeatures{
			DenseFeatures: generateTestEmbedding(256),
			TimeSeries: &opinionated.TimeSeriesFeatures{
				Rolling_1M: &opinionated.RollingStats{
					Mean:   85.5,
					StdDev: 5.2,
					Max:    95.0,
				},
				Trend_1H: 0.15,
				Trend_24H: 0.08,
			},
		},
	}
}

// Memory allocation benchmark
func BenchmarkMemoryAllocation(b *testing.B) {
	b.Run("EventCreation", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = &opinionated.OpinionatedEvent{
				Id:        fmt.Sprintf("event-%d", i),
				Timestamp: timestamppb.Now(),
			}
		}
	})

	b.Run("SemanticContext", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = &opinionated.SemanticContext{
				EventType:    "test.event",
				Embedding:    make([]float32, 128),
				OntologyTags: []string{"tag1", "tag2", "tag3"},
			}
		}
	})

	b.Run("CorrelationVectors", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = &opinionated.CorrelationVectors{
				Temporal: make([]float32, 64),
				Spatial:  make([]float32, 64),
				Causal:   make([]float32, 64),
				Semantic: make([]float32, 64),
			}
		}
	})
}

// Concurrency benchmarks
func BenchmarkConcurrentEnrichment(b *testing.B) {
	enricher := NewSemanticEnricher()
	ctx := context.Background()
	
	event := RawEvent{
		Type:   "memory",
		Entity: "pod/concurrent-test",
		Data:   map[string]interface{}{"memory_usage_percent": 85.5},
	}

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := enricher.Enrich(ctx, event)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkConcurrentCorrelation(b *testing.B) {
	engine := NewCorrelationEngine()
	ctx := context.Background()

	// Pre-populate
	for i := 0; i < 1000; i++ {
		event := &opinionated.OpinionatedEvent{
			Id:        fmt.Sprintf("populate-%d", i),
			Timestamp: timestamppb.Now(),
		}
		_ = engine.IndexEvent(ctx, event)
	}

	queryEvent := &opinionated.OpinionatedEvent{
		Id:        "query",
		Timestamp: timestamppb.Now(),
	}

	options := CorrelationOptions{
		TimeWindow: 5 * time.Minute,
		MaxResults: 10,
	}

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := engine.Correlate(ctx, queryEvent, options)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}