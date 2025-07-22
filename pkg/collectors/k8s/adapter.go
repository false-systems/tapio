package k8s

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors/common"
	"github.com/yairfalse/tapio/pkg/domain"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
)

// K8sAdapter implements CollectorAdapter for Kubernetes events
type K8sAdapter struct{}

// NewK8sAdapter creates a new K8s collector adapter
func NewK8sAdapter() *K8sAdapter {
	return &K8sAdapter{}
}

// GetCollectorID returns the collector identifier
func (a *K8sAdapter) GetCollectorID() string {
	return "k8s-collector"
}

// GetTracerName returns the OTEL tracer name
func (a *K8sAdapter) GetTracerName() string {
	return "tapio.k8s.collector"
}

// GetBatchIDPrefix returns the batch ID prefix
func (a *K8sAdapter) GetBatchIDPrefix() string {
	return "k8s-batch"
}

// MapEventType maps domain event types to protobuf types for K8s
func (a *K8sAdapter) MapEventType(eventType domain.EventType) pb.EventType {
	switch eventType {
	case domain.EventTypeKubernetes:
		return pb.EventType_EVENT_TYPE_KUBERNETES
	case domain.EventTypeSystem:
		return pb.EventType_EVENT_TYPE_SYSCALL
	case domain.EventTypeNetwork:
		return pb.EventType_EVENT_TYPE_NETWORK
	case domain.EventTypeProcess:
		return pb.EventType_EVENT_TYPE_PROCESS
	case domain.EventTypeMemory:
		return pb.EventType_EVENT_TYPE_RESOURCE_USAGE
	case domain.EventTypeCPU:
		return pb.EventType_EVENT_TYPE_RESOURCE_USAGE
	case domain.EventTypeDisk:
		return pb.EventType_EVENT_TYPE_FILE_SYSTEM
	case domain.EventTypeService:
		return pb.EventType_EVENT_TYPE_HTTP // Services often use HTTP
	default:
		return pb.EventType_EVENT_TYPE_KUBERNETES
	}
}

// MapSourceType maps domain source types to protobuf types for K8s
func (a *K8sAdapter) MapSourceType(source domain.SourceType) pb.SourceType {
	switch source {
	case domain.SourceK8s:
		return pb.SourceType_SOURCE_TYPE_KUBERNETES_API
	case domain.SourceEBPF:
		return pb.SourceType_SOURCE_TYPE_EBPF
	case domain.SourceSystemd:
		return pb.SourceType_SOURCE_TYPE_JOURNALD
	case domain.SourceCNI:
		return pb.SourceType_SOURCE_TYPE_KUBERNETES_API
	default:
		return pb.SourceType_SOURCE_TYPE_KUBERNETES_API
	}
}

// ExtractMessage extracts the message from a UnifiedEvent for K8s
func (a *K8sAdapter) ExtractMessage(event *domain.UnifiedEvent) string {
	if event.Kubernetes != nil && event.Kubernetes.Message != "" {
		return event.Kubernetes.Message
	}
	if event.Application != nil && event.Application.Message != "" {
		return event.Application.Message
	}
	if event.Semantic != nil && event.Semantic.Narrative != "" {
		return event.Semantic.Narrative
	}
	return fmt.Sprintf("Event %s from %s", event.Type, event.Source)
}

// CreateEventContext creates K8s-specific event context
func (a *K8sAdapter) CreateEventContext(event *domain.UnifiedEvent) *pb.EventContext {
	if event.Entity == nil {
		return nil
	}

	clusterName := ""
	nodeName := ""
	if event.Entity.Labels != nil {
		clusterName = event.Entity.Labels["cluster"]
		nodeName = event.Entity.Labels["node"]
	}

	return &pb.EventContext{
		Cluster:   clusterName,
		Namespace: event.Entity.Namespace,
		Node:      nodeName,
		Pod:       event.Entity.Name, // For K8s, the entity name is often the pod name
		Labels:    event.Entity.Labels,
	}
}

// ExtractAttributes extracts K8s-specific attributes from the event
func (a *K8sAdapter) ExtractAttributes(event *domain.UnifiedEvent) map[string]string {
	attributes := make(map[string]string)

	// Add entity attributes
	if event.Entity != nil {
		if event.Entity.UID != "" {
			attributes["entity.uid"] = event.Entity.UID
		}
		if event.Entity.Type != "" {
			attributes["entity.type"] = event.Entity.Type
		}
		if event.Entity.Name != "" {
			attributes["entity.name"] = event.Entity.Name
		}
		if event.Entity.Namespace != "" {
			attributes["entity.namespace"] = event.Entity.Namespace
		}

		// Add entity labels with prefix
		for k, v := range event.Entity.Labels {
			attributes["entity.label."+k] = v
		}

		// Add entity attributes with prefix
		for k, v := range event.Entity.Attributes {
			attributes["entity."+k] = v
		}
	}

	// Add Kubernetes-specific attributes
	if event.Kubernetes != nil {
		if event.Kubernetes.EventType != "" {
			attributes["k8s.event_type"] = event.Kubernetes.EventType
		}
		if event.Kubernetes.Reason != "" {
			attributes["k8s.reason"] = event.Kubernetes.Reason
		}
		if event.Kubernetes.Object != "" {
			attributes["k8s.object"] = event.Kubernetes.Object
		}
		if event.Kubernetes.ObjectKind != "" {
			attributes["k8s.object_kind"] = event.Kubernetes.ObjectKind
		}
		if event.Kubernetes.Action != "" {
			attributes["k8s.action"] = event.Kubernetes.Action
		}
		if event.Kubernetes.APIVersion != "" {
			attributes["k8s.api_version"] = event.Kubernetes.APIVersion
		}
		if event.Kubernetes.ResourceVersion != "" {
			attributes["k8s.resource_version"] = event.Kubernetes.ResourceVersion
		}

		// Add K8s labels with prefix
		for k, v := range event.Kubernetes.Labels {
			attributes["k8s.label."+k] = v
		}

		// Add K8s annotations with prefix
		for k, v := range event.Kubernetes.Annotations {
			attributes["k8s.annotation."+k] = v
		}
	}

	// Add network data if present
	if event.Network != nil {
		if event.Network.Protocol != "" {
			attributes["network.protocol"] = event.Network.Protocol
		}
		if event.Network.SourceIP != "" {
			attributes["network.source_ip"] = event.Network.SourceIP
		}
		if event.Network.DestIP != "" {
			attributes["network.dest_ip"] = event.Network.DestIP
		}
		if event.Network.Direction != "" {
			attributes["network.direction"] = event.Network.Direction
		}
		if event.Network.StatusCode != 0 {
			attributes["network.status_code"] = fmt.Sprintf("%d", event.Network.StatusCode)
		}
	}

	// Add application data if present
	if event.Application != nil {
		if event.Application.Level != "" {
			attributes["app.level"] = event.Application.Level
		}
		if event.Application.Logger != "" {
			attributes["app.logger"] = event.Application.Logger
		}
		if event.Application.ErrorType != "" {
			attributes["app.error_type"] = event.Application.ErrorType
		}
		if event.Application.UserID != "" {
			attributes["app.user_id"] = event.Application.UserID
		}
		if event.Application.SessionID != "" {
			attributes["app.session_id"] = event.Application.SessionID
		}
		if event.Application.RequestID != "" {
			attributes["app.request_id"] = event.Application.RequestID
		}
	}

	return attributes
}

// NewTapioGRPCClient creates a new K8s Tapio gRPC client
func NewTapioGRPCClient(serverAddr string) (*common.TapioGRPCClient, error) {
	adapter := NewK8sAdapter()
	return common.NewTapioGRPCClient(serverAddr, adapter)
}

// NewTapioGRPCClientWithConfig creates a new K8s Tapio gRPC client with custom config
func NewTapioGRPCClientWithConfig(config *common.TapioClientConfig) (*common.TapioGRPCClient, error) {
	adapter := NewK8sAdapter()
	return common.NewTapioGRPCClientWithConfig(config, adapter)
}
