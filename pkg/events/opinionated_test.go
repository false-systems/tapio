package events

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/events/opinionated"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestSemanticEnricher_Enrich(t *testing.T) {
	tests := []struct {
		name    string
		event   RawEvent
		wantErr bool
		checks  func(t *testing.T, result *opinionated.SemanticContext)
	}{
		{
			name: "memory exhaustion event",
			event: RawEvent{
				Type:      "memory",
				Source:    "kubelet",
				Entity:    "pod/frontend-xyz",
				Timestamp: time.Now(),
				Data: map[string]interface{}{
					"memory_usage_percent": 95.5,
					"namespace":            "production",
				},
			},
			wantErr: false,
			checks: func(t *testing.T, result *opinionated.SemanticContext) {
				assert.Equal(t, "resource.exhaustion.memory", result.EventType)
				assert.Contains(t, result.OntologyTags, "critical")
				assert.Contains(t, result.OntologyTags, "performance")
				assert.NotEmpty(t, result.Description)
				assert.NotEmpty(t, result.Embedding)
				assert.Greater(t, result.IntentConfidence, float32(0.5))
			},
		},
		{
			name: "pod lifecycle event",
			event: RawEvent{
				Type:      "pod",
				Source:    "kube-apiserver",
				Entity:    "pod/backend-abc",
				Timestamp: time.Now(),
				Data: map[string]interface{}{
					"action":    "created",
					"namespace": "default",
				},
			},
			wantErr: false,
			checks: func(t *testing.T, result *opinionated.SemanticContext) {
				assert.Equal(t, "lifecycle.kubernetes.pod", result.EventType)
				assert.Contains(t, result.OntologyTags, "infrastructure")
				assert.NotEmpty(t, result.Description)
			},
		},
	}

	enricher := NewSemanticEnricher()
	ctx := context.Background()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := enricher.Enrich(ctx, tt.event)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)
			tt.checks(t, result)
		})
	}
}

func TestCorrelationEngine_IndexAndCorrelate(t *testing.T) {
	engine := NewCorrelationEngine()
	ctx := context.Background()

	// Create test events
	event1 := &opinionated.OpinionatedEvent{
		Id:        "event-1",
		Timestamp: timestamppb.Now(),
		Semantic: &opinionated.SemanticContext{
			EventType: "resource.exhaustion.memory",
			Embedding: generateTestEmbedding(128),
		},
		Behavioral: &opinionated.BehavioralContext{
			Entity: &opinionated.EntityFingerprint{
				Id:   "pod:frontend-xyz",
				Type: "pod",
			},
			BehaviorDeviation: 0.9,
		},
		Correlation: &opinionated.CorrelationContext{
			Vectors: &opinionated.CorrelationVectors{
				Temporal: []float32{0.8, 0.2, 0.1},
				Spatial:  []float32{0.9, 0.7, 0.3},
			},
			Groups: []*opinionated.CorrelationGroup{
				{Id: "group-1", Type: "deployment"},
			},
		},
	}

	event2 := &opinionated.OpinionatedEvent{
		Id:        "event-2",
		Timestamp: timestamppb.New(time.Now().Add(20 * time.Second)),
		Semantic: &opinionated.SemanticContext{
			EventType: "lifecycle.kubernetes.pod.restart",
			Embedding: generateTestEmbedding(128),
		},
		Behavioral: &opinionated.BehavioralContext{
			Entity: &opinionated.EntityFingerprint{
				Id:   "pod:frontend-xyz",
				Type: "pod",
			},
		},
		Correlation: &opinionated.CorrelationContext{
			CausalLinks: []*opinionated.CausalLink{
				{
					EventId:      "event-1",
					Relationship: "caused_by",
					Confidence:   0.85,
					Lag:          durationpb.New(20 * time.Second),
				},
			},
		},
	}

	// Index events
	err := engine.IndexEvent(ctx, event1)
	require.NoError(t, err)

	err = engine.IndexEvent(ctx, event2)
	require.NoError(t, err)

	// Test correlation
	options := CorrelationOptions{
		TimeWindow:        1 * time.Minute,
		EntityDepth:       2,
		CausalDepth:       3,
		SemanticThreshold: 0.7,
		BuildGraph:        true,
		DetectPatterns:    true,
		MaxResults:        10,
	}

	result, err := engine.Correlate(ctx, event2, options)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should find temporal correlation
	assert.Greater(t, len(result.Correlations), 0)
	
	// Should find causal correlation
	hasCausal := false
	for _, corr := range result.Correlations {
		if corr.Type == "causal" {
			hasCausal = true
			break
		}
	}
	assert.True(t, hasCausal, "Should find causal correlation")

	// Should build correlation graph
	if options.BuildGraph {
		assert.NotNil(t, result.Graph)
		assert.Greater(t, len(result.Graph.Nodes), 0)
	}
}

func TestFutureProofEngine_PrepareForAI(t *testing.T) {
	config := &FutureProofConfig{
		Profile: "default",
		Features: FeatureConfig{
			EnabledFeatures: []string{"temporal", "behavioral", "semantic"},
			CacheFeatures:   true,
		},
		Models: ModelConfig{
			EnabledModels: []string{"oom_predictor"},
		},
		Optimization: OptimizationConfig{
			EnableParallelism: true,
			MaxWorkers:        2,
		},
	}

	engine := NewFutureProofEngine(config)
	ctx := context.Background()

	event := &opinionated.OpinionatedEvent{
		Id:        "test-event",
		Timestamp: timestamppb.Now(),
		Semantic: &opinionated.SemanticContext{
			EventType:        "resource.exhaustion.memory",
			Embedding:        generateTestEmbedding(128),
			Intent:           "debugging",
			IntentConfidence: 0.9,
			SemanticFeatures: map[string]float32{
				"memory_pressure": 0.95,
			},
		},
		Behavioral: &opinionated.BehavioralContext{
			Entity: &opinionated.EntityFingerprint{
				Id:         "pod:frontend-xyz",
				Type:       "pod",
				TrustScore: 0.95,
			},
			BehaviorDeviation: 0.85,
			BehaviorTrend:     "increasing",
		},
		Temporal: &opinionated.TemporalContext{
			Duration: durationpb.New(5 * time.Minute),
			Periodicity: &opinionated.Periodicity{
				Period:     durationpb.New(24 * time.Hour),
				Confidence: 0.9,
			},
		},
		AiFeatures: &opinionated.AIFeatures{
			TimeSeries: &opinionated.TimeSeriesFeatures{
				Rolling_1M: &opinionated.RollingStats{
					Mean:   75.5,
					StdDev: 10.2,
					Max:    95.0,
				},
				Trend_1H: 0.15,
			},
		},
	}

	result, err := engine.PrepareForAI(ctx, event)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Check features were generated
	assert.NotEmpty(t, result.Features.Sparse)
	assert.NotEmpty(t, result.Features.Categorical)

	// Check temporal features
	assert.Contains(t, result.Features.Sparse, "hour_of_day")
	assert.Contains(t, result.Features.Sparse, "day_of_week")
	
	// Check behavioral features
	assert.Contains(t, result.Features.Sparse, "behavior_deviation")
	assert.Equal(t, float32(0.85), result.Features.Sparse["behavior_deviation"])

	// Check semantic features
	assert.Contains(t, result.Features.Categorical, "event_type")
	assert.Equal(t, "resource.exhaustion.memory", result.Features.Categorical["event_type"])

	// Check predictions were made (if memory usage is high enough)
	if result.Features.Sparse["memory_usage"] > 0.7 {
		assert.Greater(t, len(result.Predictions), 0)
	}
}

func TestOpinionConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		config  OpinionConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: OpinionConfig{
				ImportanceWeights: map[string]float32{
					"customer_facing": 0.9,
					"system_critical": 0.8,
				},
				AnomalyThresholds: map[string]float32{
					"memory_usage": 0.85,
					"cpu_usage":    0.75,
				},
				BehavioralConfig: BehavioralOpinions{
					DeviationSensitivity: 0.8,
				},
				PredictionConfig: PredictionOpinions{
					MinConfidenceThreshold: 0.7,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid importance weight",
			config: OpinionConfig{
				ImportanceWeights: map[string]float32{
					"customer_facing": 1.5, // > 1
				},
			},
			wantErr: true,
			errMsg:  "importance weight",
		},
		{
			name: "invalid anomaly threshold",
			config: OpinionConfig{
				AnomalyThresholds: map[string]float32{
					"memory_usage": -0.1, // < 0
				},
			},
			wantErr: true,
			errMsg:  "anomaly threshold",
		},
		{
			name: "invalid deviation sensitivity",
			config: OpinionConfig{
				BehavioralConfig: BehavioralOpinions{
					DeviationSensitivity: 1.2, // > 1
				},
			},
			wantErr: true,
			errMsg:  "deviation sensitivity",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateOpinionConfig(tt.config)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCorrelationPatterns(t *testing.T) {
	tests := []struct {
		name     string
		events   []*IndexedEvent
		pattern  Pattern
		expected bool
	}{
		{
			name: "cascade failure pattern",
			events: createCascadeEvents(),
			pattern: &CascadeFailurePattern{},
			expected: true,
		},
		{
			name: "thundering herd pattern",
			events: createThunderingHerdEvents(),
			pattern: &ThunderingHerdPattern{},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detection := tt.pattern.Detect(tt.events)
			
			if tt.expected {
				assert.NotNil(t, detection)
				assert.Greater(t, detection.Confidence, float32(0.5))
				assert.Equal(t, tt.pattern.Name(), detection.PatternName)
			} else {
				assert.Nil(t, detection)
			}
		})
	}
}

func TestLSHIndex(t *testing.T) {
	lsh := newLSHIndex(128, 5)

	// Add events with embeddings
	events := make([]*IndexedEvent, 10)
	for i := range events {
		embedding := generateTestEmbedding(128)
		event := &IndexedEvent{
			Event: &opinionated.OpinionatedEvent{
				Id: fmt.Sprintf("event-%d", i),
				Semantic: &opinionated.SemanticContext{
					Embedding: embedding,
				},
			},
			Timestamp: time.Now().UnixNano(),
		}
		events[i] = event
		lsh.Add(event, embedding)
	}

	// Find similar events
	queryEmbedding := generateTestEmbedding(128)
	similar := lsh.FindSimilar(queryEmbedding, 5)

	assert.LessOrEqual(t, len(similar), 5)
	assert.Greater(t, len(similar), 0)
}

func TestFeatureCache(t *testing.T) {
	cache := newFeatureCache(1 * time.Minute)

	// Test set and get
	features := &AIFeatures{
		Sparse: map[string]float32{
			"test_feature": 0.5,
		},
	}

	cache.Set("event-1", features, 1*time.Minute)
	
	retrieved := cache.Get("event-1")
	assert.NotNil(t, retrieved)
	
	retrievedFeatures, ok := retrieved.(*AIFeatures)
	assert.True(t, ok)
	assert.Equal(t, float32(0.5), retrievedFeatures.Sparse["test_feature"])

	// Test miss
	miss := cache.Get("non-existent")
	assert.Nil(t, miss)
}

func TestBatcher(t *testing.T) {
	batchReceived := make(chan []*AIReadyEvent, 1)
	callback := func(batch []*AIReadyEvent) {
		batchReceived <- batch
	}

	batcher := newBatcher(3, 100*time.Millisecond)

	// Add events
	for i := 0; i < 3; i++ {
		event := &AIReadyEvent{
			Original: &opinionated.OpinionatedEvent{
				Id: fmt.Sprintf("event-%d", i),
			},
		}
		batcher.Add(event, callback)
	}

	// Should receive batch immediately when full
	select {
	case batch := <-batchReceived:
		assert.Len(t, batch, 3)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Batch not received")
	}
}

func TestOpinionProfiles(t *testing.T) {
	// Test default profile
	defaultProfile := OpinionProfiles["default"]
	assert.NotNil(t, defaultProfile)
	assert.Equal(t, float32(1.0), defaultProfile.ImportanceWeights["customer_facing"])
	assert.Equal(t, 30*time.Second, defaultProfile.CorrelationWindows["oom_restart"])

	// Test sensitive profile
	sensitiveProfile := OpinionProfiles["sensitive"]
	assert.NotNil(t, sensitiveProfile)
	assert.Equal(t, float32(0.75), sensitiveProfile.AnomalyThresholds["memory_usage"])

	// Test merging profiles
	config := &FutureProofConfig{
		Profile: "sensitive",
	}
	engine := NewFutureProofEngine(config)
	
	// Should have merged sensitive profile
	assert.Equal(t, float32(0.75), engine.config.Opinions.AnomalyThresholds["memory_usage"])
}

// Helper functions

func generateTestEmbedding(size int) []float32 {
	embedding := make([]float32, size)
	for i := range embedding {
		embedding[i] = float32(i) / float32(size)
	}
	return embedding
}

func createCascadeEvents() []*IndexedEvent {
	baseTime := time.Now().UnixNano()
	events := make([]*IndexedEvent, 5)
	
	for i := range events {
		events[i] = &IndexedEvent{
			Event: &opinionated.OpinionatedEvent{
				Id: fmt.Sprintf("cascade-%d", i),
			},
			Timestamp: baseTime + int64(i*10*time.Second),
		}
	}
	
	return events
}

func createThunderingHerdEvents() []*IndexedEvent {
	baseTime := time.Now().UnixNano()
	events := make([]*IndexedEvent, 15)
	
	// All events within 2 seconds
	for i := range events {
		events[i] = &IndexedEvent{
			Event: &opinionated.OpinionatedEvent{
				Id: fmt.Sprintf("herd-%d", i),
			},
			Timestamp: baseTime + int64(i*100*time.Millisecond),
		}
	}
	
	return events
}

func TestMetricsTracking(t *testing.T) {
	// Test semantic enricher metrics
	enricher := NewSemanticEnricher()
	ctx := context.Background()

	// Process some events
	for i := 0; i < 5; i++ {
		event := RawEvent{
			Type:   "test",
			Entity: fmt.Sprintf("entity-%d", i),
		}
		_, _ = enricher.Enrich(ctx, event)
	}

	metrics := enricher.Metrics()
	assert.Equal(t, uint64(5), metrics.EventsEnriched)
	assert.Greater(t, metrics.EnrichmentDuration, time.Duration(0))

	// Test correlation engine metrics
	engine := NewCorrelationEngine()
	
	event := &opinionated.OpinionatedEvent{
		Id:        "test-metrics",
		Timestamp: timestamppb.Now(),
	}
	
	_ = engine.IndexEvent(ctx, event)
	
	corrMetrics := engine.Metrics()
	assert.Equal(t, uint64(1), corrMetrics.EventsIndexed)
}

func BenchmarkSemanticEnrichment(b *testing.B) {
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
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := enricher.Enrich(ctx, event)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkCorrelationIndexing(b *testing.B) {
	engine := NewCorrelationEngine()
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			event := &opinionated.OpinionatedEvent{
				Id:        fmt.Sprintf("bench-%d", i),
				Timestamp: timestamppb.Now(),
				Semantic: &opinionated.SemanticContext{
					EventType: "test.benchmark",
					Embedding: generateTestEmbedding(128),
				},
			}
			
			err := engine.IndexEvent(ctx, event)
			if err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
}

func BenchmarkAIFeatureGeneration(b *testing.B) {
	config := &FutureProofConfig{
		Profile: "default",
		Features: FeatureConfig{
			EnabledFeatures: []string{"temporal", "behavioral", "semantic"},
		},
		Optimization: OptimizationConfig{
			EnableParallelism: true,
			MaxWorkers:        4,
		},
	}

	engine := NewFutureProofEngine(config)
	ctx := context.Background()

	event := &opinionated.OpinionatedEvent{
		Id:        "bench-event",
		Timestamp: timestamppb.Now(),
		Semantic: &opinionated.SemanticContext{
			EventType: "resource.exhaustion.memory",
			Embedding: generateTestEmbedding(128),
		},
		Behavioral: &opinionated.BehavioralContext{
			BehaviorDeviation: 0.8,
		},
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := engine.PrepareForAI(ctx, event)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}