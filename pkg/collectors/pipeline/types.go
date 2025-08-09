package pipeline

import (
	"context"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/config"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// EventPipeline manages the flow of events from collectors to NATS
type EventPipeline struct {
	collectors map[string]collectors.Collector
	enricher   *K8sEnricher
	publisher  *NATSPublisher
	logger     *zap.Logger

	eventsChan chan *collectors.RawEvent
	workers    int
	ctx        context.Context
	cancel     context.CancelFunc
}

// Config holds pipeline configuration
type Config struct {
	Workers    int
	BufferSize int
	NATSConfig *config.NATSConfig
}

// DefaultConfig returns default configuration
func DefaultConfig() Config {
	return Config{
		Workers:    4,
		BufferSize: 10000,
		NATSConfig: config.DefaultNATSConfig(),
	}
}

// EnrichedEvent represents an event with added context
type EnrichedEvent struct {
	Raw       *collectors.RawEvent
	K8sObject *K8sObjectInfo
	TraceID   string
	SpanID    string
	ParentID  string
}

// K8sObjectInfo contains Kubernetes object information
type K8sObjectInfo struct {
	Kind      string
	Name      string
	Namespace string
	UID       string
	Labels    map[string]string
}

// ConvertToUnified converts enriched event to domain.UnifiedEvent
func (e *EnrichedEvent) ConvertToUnified() *domain.UnifiedEvent {
	event := &domain.UnifiedEvent{
		ID:        domain.GenerateEventID(),
		Timestamp: e.Raw.Timestamp,
		Type:      mapCollectorTypeToDomain(e.Raw.Type),
		Severity:  domain.EventSeverityInfo,
		Source:    e.Raw.Type,
		Message:   extractMessage(e.Raw),
	}

	// Add K8s context
	if e.K8sObject != nil {
		event.K8sContext = &domain.K8sContext{
			Kind:      e.K8sObject.Kind,
			Name:      e.K8sObject.Name,
			Namespace: e.K8sObject.Namespace,
			UID:       e.K8sObject.UID,
			Labels:    e.K8sObject.Labels,
		}
	}

	// Add trace context
	if e.TraceID != "" {
		event.TraceContext = &domain.TraceContext{
			TraceID: e.TraceID,
			SpanID:  e.SpanID,
		}
	}

	// Copy metadata to attributes with type safety, preserving event_type
	if len(e.Raw.Metadata) > 0 {
		event.Attributes = convertMetadataToAttributes(e.Raw.Metadata)

		// Also set specific event type if available
		if eventType, ok := e.Raw.Metadata["event_type"]; ok {
			event.Type = domain.EventType(eventType)
		}
	}

	return event
}

func mapCollectorTypeToDomain(collectorType string) domain.EventType {
	switch collectorType {
	case "kubeapi":
		return domain.EventTypeKubernetes
	case "etcd":
		return domain.EventTypeSystem // etcd is system-level
	case "kernel":
		return domain.EventTypeProcess // kernel events are process-level
	case "cni":
		return domain.EventTypeNetwork
	case "systemd":
		return domain.EventTypeSystem
	default:
		return domain.EventTypeSystem // default to system
	}
}

func extractMessage(raw *collectors.RawEvent) string {
	if msg, ok := raw.Metadata["message"]; ok {
		return msg
	}
	if event, ok := raw.Metadata["event"]; ok {
		return event
	}
	return "Event from " + raw.Type
}

// CollectorHealthStatus represents the health status of a collector
type CollectorHealthStatus struct {
	Healthy   bool
	Error     string
	LastEvent time.Time
}

// convertMetadataToAttributes converts string metadata to interface{} attributes safely
func convertMetadataToAttributes(metadata map[string]string) map[string]interface{} {
	if metadata == nil {
		return nil
	}

	attributes := make(map[string]interface{}, len(metadata))
	for k, v := range metadata {
		attributes[k] = v
	}
	return attributes
}
