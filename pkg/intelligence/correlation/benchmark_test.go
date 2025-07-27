package correlation

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

// BenchmarkCorrelationSystem measures performance of correlation system
func BenchmarkCorrelationSystem(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultSimpleSystemConfig()
	config.EventBufferSize = 10000
	config.MaxConcurrency = 8

	system := NewSimpleCorrelationSystem(logger, config)
	err := system.Start()
	if err != nil {
		b.Fatalf("Failed to start correlation system: %v", err)
	}
	defer system.Stop()

	ctx := context.Background()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		event := createBenchmarkEvent(i)
		err := system.ProcessEvent(ctx, event)
		if err != nil {
			b.Fatalf("Failed to process event: %v", err)
		}
	}
}

// BenchmarkK8sNativeCorrelator measures K8s correlation performance
func BenchmarkK8sNativeCorrelator(b *testing.B) {
	logger := zaptest.NewLogger(b)
	correlator := NewK8sNativeCorrelator(logger)

	events := make([]*domain.UnifiedEvent, 1000)
	for i := 0; i < 1000; i++ {
		events[i] = createK8sBenchmarkEvent(i)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		event := events[i%1000]
		correlations := correlator.FindCorrelations(event)
		_ = correlations // Prevent optimization
	}
}

// BenchmarkTemporalCorrelator measures temporal correlation performance
func BenchmarkTemporalCorrelator(b *testing.B) {
	logger := zaptest.NewLogger(b)
	correlator := NewTemporalCorrelator(logger, DefaultTemporalConfig())

	events := make([]*domain.UnifiedEvent, 1000)
	for i := 0; i < 1000; i++ {
		events[i] = createTemporalBenchmarkEvent(i)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		event := events[i%1000]
		correlations := correlator.Process(event)
		_ = correlations // Prevent optimization
	}
}

// BenchmarkSequenceDetector measures sequence detection performance
func BenchmarkSequenceDetector(b *testing.B) {
	logger := zaptest.NewLogger(b)
	detector := NewSequenceDetector(logger, DefaultSequenceConfig())

	events := make([]*domain.UnifiedEvent, 1000)
	for i := 0; i < 1000; i++ {
		events[i] = createSequenceBenchmarkEvent(i)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		event := events[i%1000]
		correlations := detector.Process(event)
		_ = correlations // Prevent optimization
	}
}

// BenchmarkConfidenceScorer measures confidence scoring performance
func BenchmarkConfidenceScorer(b *testing.B) {
	logger := zaptest.NewLogger(b)
	scorer := NewConfidenceScorer(logger, DefaultScorerConfig())

	events := make([]*domain.UnifiedEvent, 100)
	for i := 0; i < 100; i++ {
		events[i] = createBenchmarkEvent(i)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		event := events[i%100]
		features := CorrelationFeatures{
			HasOwnerReference: true,
			TimeDelta:         time.Second,
			Occurrences:       5,
		}
		score := scorer.ScoreCorrelation(event, event, "k8s_native", features)
		_ = score // Prevent optimization
	}
}

// BenchmarkExplanationEngine measures explanation generation performance
func BenchmarkExplanationEngine(b *testing.B) {
	engine := NewExplanationEngine()

	correlation := K8sCorrelation{
		Type: "owner_reference",
		Source: ResourceRef{
			Name:      "myapp",
			Kind:      "Deployment",
			Namespace: "default",
		},
		Target: ResourceRef{
			Name:      "myapp-abc123",
			Kind:      "ReplicaSet",
			Namespace: "default",
		},
		Confidence: 0.95,
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		explanation := engine.ExplainK8sCorrelation(correlation)
		_ = explanation // Prevent optimization
	}
}

// BenchmarkConcurrentEventProcessing measures concurrent processing performance
func BenchmarkConcurrentEventProcessing(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultSimpleSystemConfig()
	config.EventBufferSize = 10000
	config.MaxConcurrency = 8

	system := NewSimpleCorrelationSystem(logger, config)
	err := system.Start()
	if err != nil {
		b.Fatalf("Failed to start correlation system: %v", err)
	}
	defer system.Stop()

	ctx := context.Background()

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			event := createBenchmarkEvent(i)
			err := system.ProcessEvent(ctx, event)
			if err != nil {
				b.Fatalf("Failed to process event: %v", err)
			}
			i++
		}
	})
}

// BenchmarkHighVolumeEventStream simulates high-volume event processing
func BenchmarkHighVolumeEventStream(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultSimpleSystemConfig()
	config.EventBufferSize = 50000
	config.MaxConcurrency = 16

	system := NewSimpleCorrelationSystem(logger, config)
	err := system.Start()
	if err != nil {
		b.Fatalf("Failed to start correlation system: %v", err)
	}
	defer system.Stop()

	ctx := context.Background()

	// Pre-generate events for consistent benchmarking
	events := make([]*domain.UnifiedEvent, 10000)
	for i := 0; i < 10000; i++ {
		events[i] = createRealisticBenchmarkEvent(i)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		event := events[i%10000]
		err := system.ProcessEvent(ctx, event)
		if err != nil {
			b.Fatalf("Failed to process event: %v", err)
		}
	}
}

// BenchmarkMemoryUsage measures memory usage during correlation processing
func BenchmarkMemoryUsage(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultSimpleSystemConfig()
	config.EventBufferSize = 1000

	system := NewSimpleCorrelationSystem(logger, config)
	err := system.Start()
	if err != nil {
		b.Fatalf("Failed to start correlation system: %v", err)
	}
	defer system.Stop()

	ctx := context.Background()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Process a batch of events
		for j := 0; j < 100; j++ {
			event := createBenchmarkEvent(i*100 + j)
			err := system.ProcessEvent(ctx, event)
			if err != nil {
				b.Fatalf("Failed to process event: %v", err)
			}
		}

		// Simulate some processing time
		time.Sleep(1 * time.Millisecond)
	}
}

// Performance test scenarios with different load patterns

// BenchmarkBurstLoad simulates burst traffic patterns
func BenchmarkBurstLoad(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultSimpleSystemConfig()
	config.EventBufferSize = 10000

	system := NewSimpleCorrelationSystem(logger, config)
	err := system.Start()
	if err != nil {
		b.Fatalf("Failed to start correlation system: %v", err)
	}
	defer system.Stop()

	ctx := context.Background()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Simulate burst: 100 events in quick succession
		for j := 0; j < 100; j++ {
			event := createBenchmarkEvent(i*100 + j)
			err := system.ProcessEvent(ctx, event)
			if err != nil {
				b.Fatalf("Failed to process event: %v", err)
			}
		}

		// Brief pause between bursts
		time.Sleep(10 * time.Millisecond)
	}
}

// BenchmarkSustainedLoad simulates sustained traffic patterns
func BenchmarkSustainedLoad(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultSimpleSystemConfig()
	config.EventBufferSize = 5000

	system := NewSimpleCorrelationSystem(logger, config)
	err := system.Start()
	if err != nil {
		b.Fatalf("Failed to start correlation system: %v", err)
	}
	defer system.Stop()

	ctx := context.Background()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		event := createBenchmarkEvent(i)
		err := system.ProcessEvent(ctx, event)
		if err != nil {
			b.Fatalf("Failed to process event: %v", err)
		}

		// Simulate sustained load with consistent intervals
		if i%1000 == 0 {
			time.Sleep(1 * time.Millisecond)
		}
	}
}

// Helper functions for creating benchmark events

func createBenchmarkEvent(id int) *domain.UnifiedEvent {
	return &domain.UnifiedEvent{
		ID:        fmt.Sprintf("bench-event-%d", id),
		Timestamp: time.Now(),
		Type:      domain.EventTypeKubernetes,
		Source:    "benchmark",
		Severity:  domain.EventSeverityInfo,
		Kubernetes: &domain.KubernetesData{
			Object:     fmt.Sprintf("test-pod-%d", id%100),
			ObjectKind: "Pod",
			Reason:     "Started",
			APIVersion: "v1",
		},
	}
}

func createK8sBenchmarkEvent(id int) *domain.UnifiedEvent {
	kinds := []string{"Pod", "Service", "Deployment", "ReplicaSet", "ConfigMap"}
	reasons := []string{"Created", "Updated", "Deleted", "Failed", "Started"}

	return &domain.UnifiedEvent{
		ID:        fmt.Sprintf("k8s-bench-%d", id),
		Timestamp: time.Now(),
		Type:      domain.EventTypeKubernetes,
		Source:    "k8s-api",
		Severity:  domain.EventSeverityInfo,
		Kubernetes: &domain.KubernetesData{
			Object:     fmt.Sprintf("resource-%d", id),
			ObjectKind: kinds[id%len(kinds)],
			Reason:     reasons[id%len(reasons)],
			APIVersion: "v1",
			Labels: map[string]string{
				"app":     fmt.Sprintf("app-%d", id%20),
				"version": fmt.Sprintf("v%d", id%3),
			},
		},
	}
}

func createTemporalBenchmarkEvent(id int) *domain.UnifiedEvent {
	// Create events with varying timestamps for temporal correlation testing
	baseTime := time.Now().Add(-time.Duration(id) * time.Second)

	return &domain.UnifiedEvent{
		ID:        fmt.Sprintf("temporal-bench-%d", id),
		Timestamp: baseTime,
		Type:      domain.EventTypeKubernetes,
		Source:    "temporal-test",
		Severity:  domain.EventSeverityInfo,
		Kubernetes: &domain.KubernetesData{
			Object:     fmt.Sprintf("pod-%d", id%50),
			ObjectKind: "Pod",
			Reason:     []string{"Started", "Failed", "Restarted"}[id%3],
			APIVersion: "v1",
		},
	}
}

func createSequenceBenchmarkEvent(id int) *domain.UnifiedEvent {
	// Create events that form sequences for sequence detection testing
	sequences := [][]string{
		{"ScalingReplicaSet", "SuccessfulCreate", "Scheduled", "Pulling", "Started"},
		{"Failed", "BackOff", "Pulled", "Created", "Started"},
		{"Killing", "Preempting", "FailedMount", "Created", "Started"},
	}

	sequence := sequences[id%len(sequences)]
	step := sequence[id%len(sequence)]

	return &domain.UnifiedEvent{
		ID:        fmt.Sprintf("seq-bench-%d", id),
		Timestamp: time.Now(),
		Type:      domain.EventTypeKubernetes,
		Source:    "sequence-test",
		Severity:  domain.EventSeverityInfo,
		Kubernetes: &domain.KubernetesData{
			Object:     fmt.Sprintf("workload-%d", id/len(sequence)),
			ObjectKind: "Pod",
			Reason:     step,
			APIVersion: "v1",
		},
	}
}

func createRealisticBenchmarkEvent(id int) *domain.UnifiedEvent {
	// Create realistic events that would trigger multiple correlation types
	event := createK8sBenchmarkEvent(id)

	// Add realistic metadata
	event.Kubernetes.Message = fmt.Sprintf("Event %d: %s %s",
		id, event.Kubernetes.Reason, event.Kubernetes.Object)

	// Add network data for some events
	if id%7 == 0 {
		event.Network = &domain.NetworkData{
			Protocol:   "TCP",
			SourceIP:   fmt.Sprintf("10.0.%d.%d", (id/256)%256, id%256),
			SourcePort: uint16(30000 + id%10000),
			DestIP:     fmt.Sprintf("10.1.%d.%d", (id/256)%256, id%256),
			DestPort:   uint16(80 + id%100),
		}
	}

	return event
}
