package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/performance"
)

// SimplifiedValidationStage validates events without metadata tracking
type SimplifiedValidationStage struct {
	name string
}

// NewSimplifiedValidationStage creates a new validation stage
func NewSimplifiedValidationStage(name string) *SimplifiedValidationStage {
	return &SimplifiedValidationStage{name: name}
}

func (s *SimplifiedValidationStage) Name() string {
	return s.name
}

func (s *SimplifiedValidationStage) Process(ctx context.Context, event *performance.Event) (*performance.Event, error) {
	// Validate required fields
	if event.ID == "" {
		return nil, fmt.Errorf("event ID is required")
	}
	if event.Timestamp.IsZero() {
		return nil, fmt.Errorf("event timestamp is required")
	}
	if event.Type == "" {
		return nil, fmt.Errorf("event type is required")
	}
	if event.Source == "" {
		return nil, fmt.Errorf("event source is required")
	}

	// Validate layer-specific data
	layerCount := 0
	if event.IsKernelEvent() {
		layerCount++
	}
	if event.IsNetworkEvent() {
		layerCount++
	}
	if event.IsApplicationEvent() {
		layerCount++
	}
	if event.IsKubernetesEvent() {
		layerCount++
	}

	if layerCount == 0 {
		return nil, fmt.Errorf("event must have at least one layer-specific data")
	}

	return event, nil
}

// SimplifiedEnrichmentStage enriches events without metadata tracking
type SimplifiedEnrichmentStage struct {
	name string
}

// NewSimplifiedEnrichmentStage creates a new enrichment stage
func NewSimplifiedEnrichmentStage(name string) *SimplifiedEnrichmentStage {
	return &SimplifiedEnrichmentStage{name: name}
}

func (s *SimplifiedEnrichmentStage) Name() string {
	return s.name
}

func (s *SimplifiedEnrichmentStage) Process(ctx context.Context, event *performance.Event) (*performance.Event, error) {
	// Add semantic context if missing
	if event.Semantic == nil {
		event.Semantic = &domain.SemanticContext{
			Category:   inferCategory(event),
			Confidence: 0.8,
		}
	}

	// Add impact context if missing
	if event.Impact == nil {
		event.Impact = &domain.ImpactContext{
			Severity: inferSeverity(event),
		}
	}

	// Add entity context if missing
	if event.Entity == nil && event.Kubernetes != nil {
		event.Entity = &domain.EntityContext{
			Type:      event.Kubernetes.ObjectKind,
			Name:      event.Kubernetes.Object,
			Namespace: extractNamespace(event),
		}
	}

	return event, nil
}

// SimplifiedCorrelationStage performs correlation without metadata tracking
type SimplifiedCorrelationStage struct {
	name string
}

// NewSimplifiedCorrelationStage creates a new correlation stage
func NewSimplifiedCorrelationStage(name string) *SimplifiedCorrelationStage {
	return &SimplifiedCorrelationStage{name: name}
}

func (s *SimplifiedCorrelationStage) Name() string {
	return s.name
}

func (s *SimplifiedCorrelationStage) Process(ctx context.Context, event *performance.Event) (*performance.Event, error) {
	// Simple correlation logic
	if event.Correlation == nil {
		event.Correlation = &domain.CorrelationContext{}
	}

	// Set correlation hints based on event type
	if event.IsKernelEvent() && event.Kernel.Syscall == "oom_kill" {
		event.Correlation.Pattern = "memory-exhaustion"
		event.CorrelationHints = append(event.CorrelationHints, "check-memory-limits")
	}

	if event.IsNetworkEvent() && event.Network.StatusCode >= 500 {
		event.Correlation.Pattern = "service-error"
		event.CorrelationHints = append(event.CorrelationHints, "check-service-health")
	}

	return event, nil
}

// SimplifiedAnalyticsStage performs analytics without metadata tracking
type SimplifiedAnalyticsStage struct {
	name string
}

// NewSimplifiedAnalyticsStage creates a new analytics stage
func NewSimplifiedAnalyticsStage(name string) *SimplifiedAnalyticsStage {
	return &SimplifiedAnalyticsStage{name: name}
}

func (s *SimplifiedAnalyticsStage) Name() string {
	return s.name
}

func (s *SimplifiedAnalyticsStage) Process(ctx context.Context, event *performance.Event) (*performance.Event, error) {
	start := time.Now()

	// Simple anomaly detection
	score := calculateAnomalyScore(event)
	if score > 0.7 {
		if event.Anomaly == nil {
			event.Anomaly = &domain.AnomalyInfo{}
		}
		event.Anomaly.Score = score
		event.Anomaly.Type = "statistical"
		event.Anomaly.Description = "Event deviates from normal patterns"
		event.Anomaly.Confidence = score
	}

	// Track processing time
	processingTime := time.Since(start)
	if event.Attributes == nil {
		event.Attributes = make(map[string]interface{})
	}
	event.Attributes["analytics_processing_time_ms"] = processingTime.Milliseconds()

	return event, nil
}

// Helper functions

func inferCategory(event *performance.Event) string {
	if event.IsKernelEvent() {
		return "system"
	}
	if event.IsNetworkEvent() {
		return "network"
	}
	if event.IsApplicationEvent() {
		return "application"
	}
	if event.IsKubernetesEvent() {
		return "infrastructure"
	}
	return "unknown"
}

func inferSeverity(event *performance.Event) string {
	// Simple severity inference
	if event.Application != nil && event.Application.Level == "error" {
		return "high"
	}
	if event.Kubernetes != nil && event.Kubernetes.EventType == "Warning" {
		return "medium"
	}
	return "low"
}

func extractNamespace(event *performance.Event) string {
	if event.Entity != nil && event.Entity.Namespace != "" {
		return event.Entity.Namespace
	}
	if event.Kubernetes != nil {
		if labels := event.Kubernetes.Labels; labels != nil {
			if ns, ok := labels["namespace"]; ok {
				return ns
			}
		}
	}
	return "default"
}

func calculateAnomalyScore(event *performance.Event) float64 {
	// Placeholder anomaly scoring
	score := 0.0

	// High severity increases anomaly score
	if event.Impact != nil && event.Impact.Severity == "critical" {
		score += 0.3
	}

	// Kernel events are less common
	if event.IsKernelEvent() {
		score += 0.2
	}

	// Events without trace context might be anomalous
	if !event.HasTraceContext() {
		score += 0.1
	}

	return score
}
