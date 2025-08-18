package intelligence

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"

	"github.com/yairfalse/tapio/pkg/domain"
)

// ProcessorStats provides strongly-typed processor statistics
// Replaces typed structs stats
type ProcessorStats struct {
	ChannelSize     int     `json:"channel_size"`
	ChannelCapacity int     `json:"channel_capacity"`
	ChannelUsage    float64 `json:"channel_usage"`
	ProcessedTotal  int64   `json:"processed_total,omitempty"`
	ErrorsTotal     int64   `json:"errors_total,omitempty"`
	AvgProcessingMS float64 `json:"avg_processing_ms,omitempty"`
}

// CalculateUsage calculates channel usage percentage
func (s *ProcessorStats) CalculateUsage() float64 {
	if s.ChannelCapacity == 0 {
		return 0.0
	}
	s.ChannelUsage = float64(s.ChannelSize) / float64(s.ChannelCapacity)
	return s.ChannelUsage
}

// DeploymentProcessor processes Kubernetes deployment events
type DeploymentProcessor struct {
	logger *zap.Logger

	// OTEL instrumentation
	tracer             trace.Tracer
	deploymentsTracked metric.Int64Counter
	eventsProcessed    metric.Int64Counter
	processingTime     metric.Float64Histogram

	// Output channel for processed deployment events
	deploymentEvents chan *domain.DeploymentEvent
}

// NewDeploymentProcessor creates a new deployment event processor
func NewDeploymentProcessor(logger *zap.Logger) (*DeploymentProcessor, error) {
	tracer := otel.Tracer("tapio.intelligence.deployment_processor")
	meter := otel.Meter("tapio.intelligence.deployment_processor")

	deploymentsTracked, err := meter.Int64Counter(
		"deployment_processor_deployments_tracked_total",
		metric.WithDescription("Total number of deployments being tracked"),
	)
	if err != nil {
		logger.Warn("Failed to create deployments tracked counter", zap.Error(err))
	}

	eventsProcessed, err := meter.Int64Counter(
		"deployment_processor_events_processed_total",
		metric.WithDescription("Total deployment events processed"),
	)
	if err != nil {
		logger.Warn("Failed to create events processed counter", zap.Error(err))
	}

	processingTime, err := meter.Float64Histogram(
		"deployment_processor_processing_duration_ms",
		metric.WithDescription("Processing duration in milliseconds"),
	)
	if err != nil {
		logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	return &DeploymentProcessor{
		logger:             logger,
		tracer:             tracer,
		deploymentsTracked: deploymentsTracked,
		eventsProcessed:    eventsProcessed,
		processingTime:     processingTime,
		deploymentEvents:   make(chan *domain.DeploymentEvent, 1000),
	}, nil
}

// ProcessRawEvent processes a raw event from the kubeapi collector
func (p *DeploymentProcessor) ProcessRawEvent(ctx context.Context, event domain.RawEvent) error {
	ctx, span := p.tracer.Start(ctx, "deployment_processor.process_raw_event")
	defer span.End()

	start := time.Now()
	defer func() {
		if p.processingTime != nil {
			duration := time.Since(start).Seconds() * 1000
			p.processingTime.Record(ctx, duration)
		}
	}()

	// Only process events from kubeapi collector
	if event.Type != "kubeapi" {
		return nil
	}

	// Parse the ResourceEvent from raw data
	var resourceEvent domain.ResourceEvent
	if err := json.Unmarshal(event.Data, &resourceEvent); err != nil {
		span.SetAttributes(attribute.String("error", err.Error()))
		return fmt.Errorf("failed to unmarshal resource event: %w", err)
	}

	// Only process Deployment events
	if resourceEvent.Kind != "Deployment" {
		return nil
	}

	span.SetAttributes(
		attribute.String("deployment.name", resourceEvent.Name),
		attribute.String("deployment.namespace", resourceEvent.Namespace),
		attribute.String("event.type", resourceEvent.EventType),
	)

	// Convert to DeploymentEvent
	deploymentEvent, err := p.convertToDeploymentEvent(ctx, &resourceEvent)
	if err != nil {
		span.SetAttributes(attribute.String("error", err.Error()))
		return fmt.Errorf("failed to convert to deployment event: %w", err)
	}

	// Send to output channel
	select {
	case p.deploymentEvents <- deploymentEvent:
		if p.eventsProcessed != nil {
			p.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("action", string(deploymentEvent.Action)),
				attribute.String("namespace", deploymentEvent.Namespace),
			))
		}
	case <-ctx.Done():
		return ctx.Err()
	default:
		p.logger.Warn("Deployment event channel full, dropping event",
			zap.String("deployment", deploymentEvent.Name),
			zap.String("namespace", deploymentEvent.Namespace),
		)
	}

	return nil
}

// convertToDeploymentEvent converts a ResourceEvent to a DeploymentEvent
func (p *DeploymentProcessor) convertToDeploymentEvent(ctx context.Context, event *domain.ResourceEvent) (*domain.DeploymentEvent, error) {
	ctx, span := p.tracer.Start(ctx, "deployment_processor.convert_event")
	defer span.End()

	// Determine action based on event type and conditions
	var action domain.DeploymentAction
	metadata := domain.DeploymentMetadata{
		Labels: event.Labels,
	}

	// Default values for replicas
	var newReplicas int32 = 1
	var oldReplicas int32 = 0

	// If we have the actual deployment object, extract more details
	if event.Object != nil {
		if deployment, ok := event.Object.(*appsv1.Deployment); ok {
			// Extract replicas
			if deployment.Spec.Replicas != nil {
				newReplicas = *deployment.Spec.Replicas
			}

			// Extract image information (use first container as primary)
			if len(deployment.Spec.Template.Spec.Containers) > 0 {
				metadata.NewImage = deployment.Spec.Template.Spec.Containers[0].Image
			}

			// Extract strategy
			if deployment.Spec.Strategy.Type != "" {
				metadata.Strategy = string(deployment.Spec.Strategy.Type)
			}

			// Check for updates when we have old object
			if event.OldObject != nil {
				if oldDeployment, ok := event.OldObject.(*appsv1.Deployment); ok {
					// Check for image changes
					if len(oldDeployment.Spec.Template.Spec.Containers) > 0 {
						metadata.OldImage = oldDeployment.Spec.Template.Spec.Containers[0].Image
					}

					// Check for scale changes
					if oldDeployment.Spec.Replicas != nil {
						oldReplicas = *oldDeployment.Spec.Replicas
					}
				}
			}
		}
	}

	// Set replicas
	metadata.NewReplicas = newReplicas
	metadata.OldReplicas = oldReplicas

	// Determine the specific action based on event type and changes
	switch event.EventType {
	case "ADDED":
		action = domain.DeploymentCreated
	case "MODIFIED":
		// Determine specific type of modification
		if metadata.OldImage != "" && metadata.NewImage != "" && metadata.OldImage != metadata.NewImage {
			action = domain.DeploymentUpdated
		} else if metadata.OldReplicas != metadata.NewReplicas {
			action = domain.DeploymentScaled
		} else {
			action = domain.DeploymentUpdated
		}
	case "DELETED":
		// Use scaled to 0 as a proxy for deletion
		action = domain.DeploymentScaled
		metadata.NewReplicas = 0
	default:
		return nil, fmt.Errorf("unknown event type: %s", event.EventType)
	}

	deploymentEvent := &domain.DeploymentEvent{
		Timestamp: event.Timestamp,
		Namespace: event.Namespace,
		Name:      event.Name,
		Action:    action,
		Metadata:  metadata,
	}

	// Validate the event
	if err := deploymentEvent.Validate(); err != nil {
		span.SetAttributes(attribute.String("validation_error", err.Error()))
		return nil, fmt.Errorf("invalid deployment event: %w", err)
	}

	span.SetAttributes(
		attribute.Bool("has_image_change", deploymentEvent.HasImageChange()),
		attribute.Bool("has_scale_change", deploymentEvent.HasScaleChange()),
		attribute.String("action", string(action)),
	)

	return deploymentEvent, nil
}

// Events returns the channel of processed deployment events
func (p *DeploymentProcessor) Events() <-chan *domain.DeploymentEvent {
	return p.deploymentEvents
}

// Start begins processing events from a raw event channel
func (p *DeploymentProcessor) Start(ctx context.Context, rawEvents <-chan domain.RawEvent) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				close(p.deploymentEvents)
				return
			case event, ok := <-rawEvents:
				if !ok {
					close(p.deploymentEvents)
					return
				}
				if err := p.ProcessRawEvent(ctx, event); err != nil {
					p.logger.Error("Failed to process raw event",
						zap.Error(err),
						zap.String("event_type", event.Type),
					)
				}
			}
		}
	}()
}

// GetStats returns processor statistics
func (p *DeploymentProcessor) GetStats() *ProcessorStats {
	stats := &ProcessorStats{
		ChannelSize:     len(p.deploymentEvents),
		ChannelCapacity: cap(p.deploymentEvents),
	}
	stats.CalculateUsage()
	return stats
}
