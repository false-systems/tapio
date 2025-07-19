package core

import (
	"context"
	"github.com/yairfalse/tapio/pkg/domain"
)

// Integration defines the base contract for all integrations
type Integration interface {
	// Name returns the integration identifier
	Name() string

	// Initialize sets up the integration
	Initialize(ctx context.Context, config Config) error

	// ProcessEvent handles incoming events from collectors/intelligence
	ProcessEvent(ctx context.Context, event *domain.Event) error

	// ProcessFinding handles findings from intelligence layer
	ProcessFinding(ctx context.Context, finding *domain.Finding) error

	// ProcessCorrelation handles correlations from intelligence layer
	ProcessCorrelation(ctx context.Context, correlation *domain.Correlation) error

	// Health returns the health status of this integration
	Health(ctx context.Context) (*HealthStatus, error)

	// Close cleanly shuts down the integration
	Close() error
}

// Config provides integration configuration
type Config interface {
	Validate() error
}

// HealthStatus represents integration health
type HealthStatus struct {
	Healthy bool                   `json:"healthy"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// MetricsExporter exports metrics to external systems
type MetricsExporter interface {
	Integration

	// ExportMetrics exports metrics
	ExportMetrics(ctx context.Context, metrics []Metric) error
}

// TraceExporter exports traces to external systems
type TraceExporter interface {
	Integration

	// ExportTraces exports traces
	ExportTraces(ctx context.Context, traces []Trace) error
}

// WebhookSender sends webhooks to external endpoints
type WebhookSender interface {
	Integration

	// SendWebhook sends a webhook
	SendWebhook(ctx context.Context, webhook Webhook) error
}
