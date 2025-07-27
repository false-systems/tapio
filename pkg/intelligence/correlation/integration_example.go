//go:build experimental
// +build experimental

package correlation

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/analytics/engine"
	"github.com/yairfalse/tapio/pkg/intelligence/interfaces"
	"go.uber.org/zap"
)

// Example shows how to integrate the correlation system with analytics engine
func ExampleIntegration() {
	// Create logger
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	// 1. Create the correlation system
	correlationConfig := DefaultSimpleSystemConfig()
	correlationConfig.EnableK8sNative = true
	correlationConfig.EnableTemporal = true
	correlationConfig.EnableSequence = true

	correlationSystem := NewSimpleCorrelationSystem(logger, correlationConfig)

	// 2. Create the analytics adapter
	adapter := NewAnalyticsCorrelationAdapter(correlationSystem, logger)

	// 3. Create mock pipeline and tracer for example
	mockPipeline := &mockEventPipeline{}
	mockTracer := &mockSemanticTracer{}

	// 4. Create analytics engine with our correlation adapter
	analyticsConfig := engine.DefaultConfig()
	analyticsEngine, err := engine.NewAnalyticsEngine(
		analyticsConfig,
		logger,
		mockPipeline,
		adapter, // Our correlation adapter implements CorrelationEngine
		mockTracer,
	)
	if err != nil {
		panic(err)
	}

	// 5. Start the system
	if err := analyticsEngine.Start(); err != nil {
		panic(err)
	}

	// 6. Process events with OTEL context
	ctx := context.Background()

	// Example: OOM Kill event with enhanced OTEL trace context
	event1 := &domain.UnifiedEvent{
		ID:        "evt-oom-123",
		Type:      domain.EventTypeKubernetes,
		Timestamp: time.Now(),
		Source:    "kubelet",
		TraceContext: &domain.TraceContext{
			TraceID:      "trace-deployment-456",
			SpanID:       "span-oom-789",
			ParentSpanID: "span-deploy-001", // Links to deployment span
			Sampled:      true,
			Baggage: map[string]string{
				"deployment.name": "api-server",
				"user.id":         "deploy-bot",
			},
		},
		Semantic: &domain.SemanticContext{
			Intent:     "memory_exhaustion",
			Category:   "resource_management",
			Tags:       []string{"oom", "pod-failure", "critical"},
			Narrative:  "API server pod exceeded memory limit and was killed by the kernel",
			Confidence: 0.95,
			Concepts:   []string{"resource-limits", "container-lifecycle", "kubernetes-scheduler"},
		},
		KubernetesData: &domain.KubernetesData{
			EventType:  "pod_oom_killed",
			ObjectKind: "Pod",
			Object:     "api-server-abc",
			Namespace:  "default",
			Reason:     "OOMKilled",
			Message:    "Container api exceeded memory limit",
			Labels: map[string]string{
				"app":     "api-server",
				"version": "v1.2.3",
			},
		},
		Impact: &domain.ImpactContext{
			Severity:         "critical",
			BusinessImpact:   0.8,
			AffectedServices: []string{"api-gateway", "user-service"},
			CustomerFacing:   true,
			SLOImpact:        true,
		},
	}

	// Process the event
	result, err := analyticsEngine.ProcessEvent(ctx, event1)
	if err != nil {
		logger.Error("Failed to process event", zap.Error(err))
	} else {
		logger.Info("Event processed",
			zap.String("event_id", result.EventID),
			zap.Float64("confidence", result.ConfidenceScore),
			zap.String("correlation_id", result.CorrelationID),
		)
	}

	// Example: Related deployment event (same trace, parent span)
	event2 := &domain.UnifiedEvent{
		ID:        "evt-deploy-124",
		Type:      domain.EventTypeKubernetes,
		Timestamp: time.Now().Add(-5 * time.Minute),
		Source:    "kube-controller",
		TraceContext: &domain.TraceContext{
			TraceID: "trace-deployment-456", // Same trace!
			SpanID:  "span-deploy-001",      // Parent of the OOM event
			Sampled: true,
			Baggage: map[string]string{
				"deployment.name": "api-server",
				"user.id":         "deploy-bot",
			},
		},
		Semantic: &domain.SemanticContext{
			Intent:     "scaling_operation",
			Category:   "deployment",
			Tags:       []string{"deployment", "scaling", "automated"},
			Narrative:  "Deployment scaled up to handle increased load",
			Confidence: 0.9,
		},
		KubernetesData: &domain.KubernetesData{
			EventType:  "deployment_updated",
			ObjectKind: "Deployment",
			Object:     "api-server",
			Namespace:  "default",
			Reason:     "ScalingReplicaSet",
			Message:    "Scaled up replica set api-server-abc to 3",
		},
		Correlation: &domain.CorrelationContext{
			CorrelationID: "corr-deployment-456",
			GroupID:       "deployment-lifecycle",
			Pattern:       "scaling-cascade",
		},
	}

	_, _ = analyticsEngine.ProcessEvent(ctx, event2)

	// 7. Check correlation findings
	time.Sleep(100 * time.Millisecond) // Allow async processing

	if findings := adapter.GetLatestFindings(); findings != nil {
		fmt.Printf("Latest Finding:\n")
		fmt.Printf("  Pattern: %s\n", findings.PatternType)
		fmt.Printf("  Confidence: %.2f\n", findings.Confidence)
		fmt.Printf("  Description: %s\n", findings.Description)
		fmt.Printf("  Semantic Group: %+v\n", findings.SemanticGroup)
	}

	// 8. Check semantic groups (OTEL trace grouping)
	groups := adapter.GetSemanticGroups()
	fmt.Printf("\nSemantic Groups: %d\n", len(groups))
	for _, group := range groups {
		fmt.Printf("  Group %s: Intent=%s, Type=%s\n",
			group.ID, group.Intent, group.Type)
	}

	// 9. Get correlation statistics
	stats := adapter.GetStats()
	fmt.Printf("\nCorrelation Statistics:\n")
	for key, value := range stats {
		fmt.Printf("  %s: %v\n", key, value)
	}

	// 10. Stop the system
	_ = analyticsEngine.Stop()
}

// Mock implementations for the example

type mockEventPipeline struct{}

func (m *mockEventPipeline) Start() error                                 { return nil }
func (m *mockEventPipeline) Stop() error                                  { return nil }
func (m *mockEventPipeline) Submit(event *interfaces.PipelineEvent) error { return nil }
func (m *mockEventPipeline) GetEvent() *interfaces.PipelineEvent {
	return &interfaces.PipelineEvent{}
}
func (m *mockEventPipeline) PutEvent(event *interfaces.PipelineEvent) {}
func (m *mockEventPipeline) GetOutput() (*interfaces.PipelineEvent, error) {
	return nil, fmt.Errorf("no events")
}
func (m *mockEventPipeline) GetMetrics() *interfaces.PipelineMetrics {
	return &interfaces.PipelineMetrics{}
}

type mockSemanticTracer struct {
	groups []*interfaces.SemanticGroup
}

func (m *mockSemanticTracer) TraceEvent(ctx context.Context, event *domain.UnifiedEvent) error {
	return nil
}
func (m *mockSemanticTracer) GetSemanticGroups() []*interfaces.SemanticGroup {
	return m.groups
}
func (m *mockSemanticTracer) GetTraceContext(eventID string) *interfaces.TraceContext {
	return nil
}

// Example output:
/*
Event processed event_id=evt-oom-123 confidence=0.85 correlation_id=k8s-corr-123

Latest Finding:
  Pattern: owner_reference
  Confidence: 0.95
  Description: Pod api-server-abc is owned by Deployment api-server
  Semantic Group: &{ID:trace-deployment-456 Intent:resource_exhaustion Type:k8s_Pod}

Semantic Groups: 1
  Group trace-deployment-456: Intent=resource_exhaustion, Type=k8s_Pod

Correlation Statistics:
  running: true
  events_processed: 2
  k8s_correlations_found: 1
  temporal_correlations: 0
  sequence_correlations: 0
  total_insights_generated: 1
  semantic_groups_count: 1
  event_buffer_size: 2
*/
