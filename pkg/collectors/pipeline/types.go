package pipeline

import (
	"context"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
)

// EventPipeline manages the flow of events from collectors to NATS
type EventPipeline struct {
	collectors map[string]collectors.Collector
	enricher   *K8sEnricher
	publisher  *NATSPublisher

	eventsChan chan *collectors.RawEvent
	workers    int
	ctx        context.Context
	cancel     context.CancelFunc
}

// Config holds pipeline configuration
type Config struct {
	Workers     int
	BufferSize  int
	NATSURL     string
	NATSSubject string
}

// DefaultConfig returns default configuration
func DefaultConfig() Config {
	return Config{
		Workers:     4,
		BufferSize:  10000,
		NATSURL:     "nats://localhost:4222",
		NATSSubject: "traces",
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

	return event
}

func mapCollectorTypeToDomain(collectorType string) domain.EventType {
	switch collectorType {
	case "kubeapi":
		return domain.EventTypeKubernetes
	case "etcd":
		return domain.EventTypeSystem // etcd is system-level
	case "ebpf":
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
