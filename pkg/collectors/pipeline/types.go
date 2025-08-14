package pipeline

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/config"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// NATSPublisherInterface defines the interface for NATS publishers
type NATSPublisherInterface interface {
	Publish(event *domain.UnifiedEvent) error
	Close()
	IsHealthy() bool
}

// EventPipeline manages the flow of events from collectors to NATS
type EventPipeline struct {
	collectors map[string]collectors.Collector
	enricher   *K8sEnricher
	publisher  NATSPublisherInterface
	logger     *zap.Logger

	eventsChan chan *collectors.RawEvent
	workers    int
	ctx        context.Context
	cancel     context.CancelFunc
	wg         *sync.WaitGroup
}

// Config holds pipeline configuration
type Config struct {
	Workers         int
	BufferSize      int
	NATSConfig      *config.NATSConfig
	UseEnhancedNATS bool // Use enhanced NATS publisher with backpressure
}

// DefaultConfig returns default configuration
func DefaultConfig() Config {
	return Config{
		Workers:         4,
		BufferSize:      10000,
		NATSConfig:      config.DefaultNATSConfig(),
		UseEnhancedNATS: true, // Default to enhanced NATS for production
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
		// Use structured attributes instead of map[string]interface{}
		if e.Raw.Metadata["collector_name"] != "" {
			event.Source = e.Raw.Metadata["collector_name"]
		}

		// Set specific event type if available
		if eventType, ok := e.Raw.Metadata["event_type"]; ok {
			event.Type = domain.EventType(eventType)
		}

		// Store metadata as correlation hints for tracing
		for key, value := range e.Raw.Metadata {
			if key != "event_type" && key != "collector_name" {
				event.CorrelationHints = append(event.CorrelationHints, fmt.Sprintf("%s:%s", key, value))
			}
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

// HealthDetails provides structured health information instead of map[string]interface{}
type HealthDetails struct {
	Healthy   bool          `json:"healthy"`
	Error     string        `json:"error,omitempty"`
	LastEvent time.Time     `json:"last_event,omitempty"`
	Uptime    time.Duration `json:"uptime,omitempty"`
}

// convertMetadataToStringMap converts metadata while preserving type safety
func convertMetadataToStringMap(metadata map[string]string) map[string]string {
	if metadata == nil {
		return nil
	}

	result := make(map[string]string, len(metadata))
	for k, v := range metadata {
		result[k] = v
	}
	return result
}
