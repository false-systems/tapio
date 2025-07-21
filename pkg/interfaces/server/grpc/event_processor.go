package grpc

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"go.uber.org/zap"
)

// EventProcessor processes events with enrichment and validation
type EventProcessor struct {
	logger *zap.Logger
}

// NewEventProcessor creates a new event processor
func NewEventProcessor(logger *zap.Logger) *EventProcessor {
	return &EventProcessor{
		logger: logger,
	}
}

// ProcessEvent processes a single raw event
func (p *EventProcessor) ProcessEvent(ctx context.Context, raw interface{}) (domain.Event, error) {
	// Type assertion based on raw event type
	switch e := raw.(type) {
	case *pb.Event:
		return p.processProtoEvent(ctx, e)
	case map[string]interface{}:
		return p.processMapEvent(ctx, e)
	default:
		return domain.Event{}, fmt.Errorf("unsupported event type: %T", raw)
	}
}

// processProtoEvent processes a proto event
func (p *EventProcessor) processProtoEvent(ctx context.Context, event *pb.Event) (domain.Event, error) {
	domainEvent := domain.Event{
		ID:        domain.EventID(event.Id),
		Type:      domain.EventType(event.Type.String()),
		Timestamp: event.Timestamp.AsTime(),
		Source:    domain.SourceGRPC,
		Context: domain.EventContext{
			Service:   event.Service,
			Component: event.Component,
			Metadata:  make(map[string]interface{}),
		},
	}

	// Add severity mapping
	switch event.Severity {
	case pb.EventSeverity_EVENT_SEVERITY_INFO:
		domainEvent.Severity = domain.SeverityInfo
	case pb.EventSeverity_EVENT_SEVERITY_WARNING:
		domainEvent.Severity = domain.SeverityWarning
	case pb.EventSeverity_EVENT_SEVERITY_ERROR:
		domainEvent.Severity = domain.SeverityError
	case pb.EventSeverity_EVENT_SEVERITY_CRITICAL:
		domainEvent.Severity = domain.SeverityCritical
	default:
		domainEvent.Severity = domain.SeverityInfo
	}

	// Copy metadata
	for k, v := range event.Metadata {
		domainEvent.Context.Metadata[k] = v
	}

	// Add processing metadata
	domainEvent.Context.Metadata["processed_at"] = time.Now()
	domainEvent.Context.Metadata["processor"] = "grpc_event_processor"

	return domainEvent, nil
}

// processMapEvent processes a map-based event (from REST/JSON)
func (p *EventProcessor) processMapEvent(ctx context.Context, data map[string]interface{}) (domain.Event, error) {
	event := domain.Event{
		Source: domain.SourceREST,
		Context: domain.EventContext{
			Metadata: make(map[string]interface{}),
		},
	}

	// Extract required fields
	if id, ok := data["id"].(string); ok {
		event.ID = domain.EventID(id)
	} else {
		return event, fmt.Errorf("missing event ID")
	}

	if eventType, ok := data["type"].(string); ok {
		event.Type = domain.EventType(eventType)
	} else {
		event.Type = domain.EventTypeOther
	}

	// Parse timestamp
	if ts, ok := data["timestamp"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
			event.Timestamp = parsed
		} else {
			event.Timestamp = time.Now()
		}
	} else {
		event.Timestamp = time.Now()
	}

	// Extract severity
	if severity, ok := data["severity"].(string); ok {
		switch severity {
		case "info":
			event.Severity = domain.SeverityInfo
		case "warning":
			event.Severity = domain.SeverityWarning
		case "error":
			event.Severity = domain.SeverityError
		case "critical":
			event.Severity = domain.SeverityCritical
		default:
			event.Severity = domain.SeverityInfo
		}
	}

	// Extract context fields
	if service, ok := data["service"].(string); ok {
		event.Context.Service = service
	}

	if component, ok := data["component"].(string); ok {
		event.Context.Component = component
	}

	// Copy all data as metadata
	for k, v := range data {
		if k != "id" && k != "type" && k != "timestamp" && k != "severity" && k != "service" && k != "component" {
			event.Context.Metadata[k] = v
		}
	}

	// Add processing metadata
	event.Context.Metadata["processed_at"] = time.Now()
	event.Context.Metadata["processor"] = "rest_event_processor"

	return event, nil
}

// EnrichEvent adds additional context to an event
func (p *EventProcessor) EnrichEvent(ctx context.Context, event domain.Event) domain.Event {
	// Add environment information
	if event.Context.Metadata == nil {
		event.Context.Metadata = make(map[string]interface{})
	}

	// Add processing context
	event.Context.Metadata["enriched_at"] = time.Now()
	event.Context.Metadata["enrichment_version"] = "1.0"

	// Add derived fields based on event type
	switch event.Type {
	case domain.EventTypeNetwork:
		p.enrichNetworkEvent(&event)
	case domain.EventTypeKubernetes:
		p.enrichKubernetesEvent(&event)
	case domain.EventTypeSystem:
		p.enrichSystemEvent(&event)
	}

	return event
}

func (p *EventProcessor) enrichNetworkEvent(event *domain.Event) {
	// Add network-specific enrichment
	event.Context.Metadata["network_enriched"] = true

	// Extract IPs if present
	if srcIP, ok := event.Context.Metadata["source_ip"].(string); ok {
		event.Context.Metadata["source_location"] = p.geoIPLookup(srcIP)
	}

	if dstIP, ok := event.Context.Metadata["dest_ip"].(string); ok {
		event.Context.Metadata["dest_location"] = p.geoIPLookup(dstIP)
	}
}

func (p *EventProcessor) enrichKubernetesEvent(event *domain.Event) {
	// Add Kubernetes-specific enrichment
	event.Context.Metadata["k8s_enriched"] = true

	// Add cluster context if available
	if namespace, ok := event.Context.Metadata["namespace"].(string); ok {
		event.Context.Metadata["cluster_context"] = fmt.Sprintf("k8s/%s", namespace)
	}
}

func (p *EventProcessor) enrichSystemEvent(event *domain.Event) {
	// Add system-specific enrichment
	event.Context.Metadata["system_enriched"] = true

	// Add host context
	if hostname, ok := event.Context.Metadata["hostname"].(string); ok {
		event.Context.Metadata["host_context"] = fmt.Sprintf("system/%s", hostname)
	}
}

func (p *EventProcessor) geoIPLookup(ip string) string {
	// Placeholder for GeoIP lookup
	// In production, this would use a real GeoIP database
	return "location_unknown"
}

// ValidateEvent validates an event structure
func (p *EventProcessor) ValidateEvent(event domain.Event) error {
	if event.ID == "" {
		return fmt.Errorf("event ID is required")
	}

	if event.Timestamp.IsZero() {
		return fmt.Errorf("event timestamp is required")
	}

	if event.Type == "" {
		return fmt.Errorf("event type is required")
	}

	return nil
}
