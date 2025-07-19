package core

import (
	"context"
	"github.com/yairfalse/tapio/pkg/domain"
	integration "github.com/yairfalse/tapio/pkg/integrations/core"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// OTELExporter exports Tapio events as OTEL traces with semantic enrichment
type OTELExporter interface {
	integration.Integration

	// ExportEvent converts and exports an event as a span
	ExportEvent(ctx context.Context, event *domain.Event) error

	// ExportCorrelation exports correlated events as linked spans
	ExportCorrelation(ctx context.Context, correlation *domain.Correlation) error

	// ExportFinding exports a finding with semantic context
	ExportFinding(ctx context.Context, finding *domain.Finding) error

	// CreateTracer returns a configured tracer
	CreateTracer(name string) trace.Tracer

	// CreateMeter returns a configured meter for metrics
	CreateMeter(name string) metric.Meter
}

// SemanticEnricher adds semantic context to traces (from Agent 2's work!)
type SemanticEnricher interface {
	// EnrichSpan adds semantic attributes to a span
	EnrichSpan(span trace.Span, event *domain.Event) error

	// AddCorrelationLinks adds correlation information
	AddCorrelationLinks(span trace.Span, correlation *domain.Correlation) error

	// AddPredictiveContext adds predictive metrics context
	AddPredictiveContext(span trace.Span, prediction PredictiveMetrics) error

	// GenerateHumanReadableContext creates human-readable span descriptions
	GenerateHumanReadableContext(event *domain.Event) string
}

// PredictiveMetricsExporter handles the revolutionary predictive OTEL metrics
type PredictiveMetricsExporter interface {
	// ExportPrediction exports predictive metrics
	ExportPrediction(ctx context.Context, prediction PredictiveMetrics) error

	// RegisterPredictiveMetrics registers predictive metric instruments
	RegisterPredictiveMetrics(meter metric.Meter) error
}

// PredictiveMetrics represents predictive analysis metrics
type PredictiveMetrics struct {
	// Resource being predicted
	ResourceRef domain.ResourceRef `json:"resource"`

	// Prediction type (e.g., "oom", "cascade_failure", "performance_degradation")
	Type string `json:"type"`

	// Predicted time of event
	PredictedTime int64 `json:"predicted_time"`

	// Confidence level (0.0 - 1.0)
	Confidence float64 `json:"confidence"`

	// Contributing factors
	Factors []PredictiveFactor `json:"factors"`

	// Recommended actions
	Recommendations []string `json:"recommendations"`
}

// PredictiveFactor represents a factor contributing to a prediction
type PredictiveFactor struct {
	Name   string  `json:"name"`
	Impact float64 `json:"impact"` // 0.0 - 1.0
	Value  string  `json:"value"`
}
