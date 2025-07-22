package cni

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors/common"
	"github.com/yairfalse/tapio/pkg/domain"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
)

// CNIAdapter implements CollectorAdapter for CNI/network events
type CNIAdapter struct{}

// NewCNIAdapter creates a new CNI collector adapter
func NewCNIAdapter() *CNIAdapter {
	return &CNIAdapter{}
}

// GetCollectorID returns the collector identifier
func (a *CNIAdapter) GetCollectorID() string {
	return "cni-collector"
}

// GetTracerName returns the OTEL tracer name
func (a *CNIAdapter) GetTracerName() string {
	return "tapio.cni.collector"
}

// GetBatchIDPrefix returns the batch ID prefix
func (a *CNIAdapter) GetBatchIDPrefix() string {
	return "cni-batch"
}

// MapEventType maps domain event types to protobuf types for CNI
func (a *CNIAdapter) MapEventType(eventType domain.EventType) pb.EventType {
	switch eventType {
	case domain.EventTypeNetwork:
		return pb.EventType_EVENT_TYPE_NETWORK
	case domain.EventTypeKubernetes:
		return pb.EventType_EVENT_TYPE_KUBERNETES
	case domain.EventTypeSystem:
		return pb.EventType_EVENT_TYPE_SYSCALL
	case domain.EventTypeProcess:
		return pb.EventType_EVENT_TYPE_PROCESS
	case domain.EventTypeService:
		return pb.EventType_EVENT_TYPE_HTTP
	case domain.EventTypeMemory:
		return pb.EventType_EVENT_TYPE_RESOURCE_USAGE
	case domain.EventTypeCPU:
		return pb.EventType_EVENT_TYPE_RESOURCE_USAGE
	case domain.EventTypeDisk:
		return pb.EventType_EVENT_TYPE_FILE_SYSTEM
	default:
		return pb.EventType_EVENT_TYPE_NETWORK // Default for CNI events
	}
}

// MapSourceType maps domain source types to protobuf types for CNI
func (a *CNIAdapter) MapSourceType(source domain.SourceType) pb.SourceType {
	switch source {
	case domain.SourceCNI:
		return pb.SourceType_SOURCE_TYPE_KUBERNETES_API
	case domain.SourceK8s:
		return pb.SourceType_SOURCE_TYPE_KUBERNETES_API
	case domain.SourceEBPF:
		return pb.SourceType_SOURCE_TYPE_EBPF
	case domain.SourceSystemd:
		return pb.SourceType_SOURCE_TYPE_JOURNALD
	default:
		return pb.SourceType_SOURCE_TYPE_KUBERNETES_API // Default for CNI
	}
}

// ExtractMessage extracts the message from a UnifiedEvent for CNI
func (a *CNIAdapter) ExtractMessage(event *domain.UnifiedEvent) string {
	if event.Kubernetes != nil && event.Kubernetes.Message != "" {
		return event.Kubernetes.Message
	}
	if event.Application != nil && event.Application.Message != "" {
		return event.Application.Message
	}
	if event.Semantic != nil && event.Semantic.Narrative != "" {
		return event.Semantic.Narrative
	}
	// Construct message from network data if available
	if event.Network != nil {
		if event.Network.Protocol != "" && event.Network.SourceIP != "" {
			return fmt.Sprintf("Network traffic: %s from %s to %s",
				event.Network.Protocol, event.Network.SourceIP, event.Network.DestIP)
		}
		if event.Network.Direction != "" {
			return fmt.Sprintf("Network %s traffic on interface", event.Network.Direction)
		}
	}
	return fmt.Sprintf("CNI event %s from %s", event.Type, event.Source)
}

// CreateEventContext creates CNI-specific event context
func (a *CNIAdapter) CreateEventContext(event *domain.UnifiedEvent) *pb.EventContext {
	if event.Entity == nil {
		return nil
	}

	clusterName := ""
	nodeName := ""
	interfaceName := ""
	if event.Entity.Labels != nil {
		clusterName = event.Entity.Labels["cluster"]
		nodeName = event.Entity.Labels["node"]
		interfaceName = event.Entity.Labels["interface"]
	}

	// Extract network interface info
	if event.Network != nil && event.Network.InterfaceName != "" {
		interfaceName = event.Network.InterfaceName
	}

	return &pb.EventContext{
		Cluster:   clusterName,
		Namespace: event.Entity.Namespace,
		Node:      nodeName,
		Pod:       event.Entity.Name, // For CNI, the entity often represents a pod
		Service:   interfaceName,     // Use service field for network interface
		Labels:    event.Entity.Labels,
	}
}

// ExtractAttributes extracts CNI-specific attributes from the event
func (a *CNIAdapter) ExtractAttributes(event *domain.UnifiedEvent) map[string]string {
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

	// Add network data (CNI-specific focus)
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
		if event.Network.SourcePort != 0 {
			attributes["network.source_port"] = fmt.Sprintf("%d", event.Network.SourcePort)
		}
		if event.Network.DestPort != 0 {
			attributes["network.dest_port"] = fmt.Sprintf("%d", event.Network.DestPort)
		}
		if event.Network.BytesSent != 0 {
			attributes["network.bytes_sent"] = fmt.Sprintf("%d", event.Network.BytesSent)
		}
		if event.Network.BytesRecv != 0 {
			attributes["network.bytes_recv"] = fmt.Sprintf("%d", event.Network.BytesRecv)
		}
		if event.Network.Latency != 0 {
			attributes["network.latency_ns"] = fmt.Sprintf("%d", event.Network.Latency)
		}
		if event.Network.Method != "" {
			attributes["network.method"] = event.Network.Method
		}
		if event.Network.Path != "" {
			attributes["network.path"] = event.Network.Path
		}
		if event.Network.InterfaceName != "" {
			attributes["network.interface"] = event.Network.InterfaceName
		}
		if event.Network.VirtualNetwork != "" {
			attributes["network.virtual_network"] = event.Network.VirtualNetwork
		}
		if event.Network.ContainerID != "" {
			attributes["network.container_id"] = event.Network.ContainerID
		}

		// Add network headers
		for k, v := range event.Network.Headers {
			attributes["network.header."+k] = v
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
		if event.Kubernetes.ClusterName != "" {
			attributes["k8s.cluster_name"] = event.Kubernetes.ClusterName
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

		// Add application custom data
		for k, v := range event.Application.Custom {
			attributes["app."+k] = fmt.Sprintf("%v", v)
		}
	}

	// Add kernel data if present (for eBPF network events)
	if event.Kernel != nil {
		if event.Kernel.Syscall != "" {
			attributes["kernel.syscall"] = event.Kernel.Syscall
		}
		if event.Kernel.PID != 0 {
			attributes["kernel.pid"] = fmt.Sprintf("%d", event.Kernel.PID)
		}
		if event.Kernel.Comm != "" {
			attributes["kernel.comm"] = event.Kernel.Comm
		}
	}

	return attributes
}

// NewTapioGRPCClient creates a new CNI Tapio gRPC client
func NewTapioGRPCClient(serverAddr string) (*common.TapioGRPCClient, error) {
	adapter := NewCNIAdapter()
	return common.NewTapioGRPCClient(serverAddr, adapter)
}

// NewTapioGRPCClientWithConfig creates a new CNI Tapio gRPC client with custom config
func NewTapioGRPCClientWithConfig(config *common.TapioClientConfig) (*common.TapioGRPCClient, error) {
	adapter := NewCNIAdapter()
	return common.NewTapioGRPCClientWithConfig(config, adapter)
}
