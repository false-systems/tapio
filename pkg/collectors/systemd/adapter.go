package systemd

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors/common"
	"github.com/yairfalse/tapio/pkg/domain"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
)

// SystemdAdapter implements CollectorAdapter for systemd/journald events
type SystemdAdapter struct{}

// NewSystemdAdapter creates a new systemd collector adapter
func NewSystemdAdapter() *SystemdAdapter {
	return &SystemdAdapter{}
}

// GetCollectorID returns the collector identifier
func (a *SystemdAdapter) GetCollectorID() string {
	return "systemd-collector"
}

// GetTracerName returns the OTEL tracer name
func (a *SystemdAdapter) GetTracerName() string {
	return "tapio.systemd.collector"
}

// GetBatchIDPrefix returns the batch ID prefix
func (a *SystemdAdapter) GetBatchIDPrefix() string {
	return "systemd-batch"
}

// MapEventType maps domain event types to protobuf types for systemd
func (a *SystemdAdapter) MapEventType(eventType domain.EventType) pb.EventType {
	switch eventType {
	case domain.EventTypeSystem:
		return pb.EventType_EVENT_TYPE_SYSCALL
	case domain.EventTypeProcess:
		return pb.EventType_EVENT_TYPE_PROCESS
	case domain.EventTypeService:
		return pb.EventType_EVENT_TYPE_HTTP // Services may have HTTP components
	case domain.EventTypeKubernetes:
		return pb.EventType_EVENT_TYPE_KUBERNETES
	case domain.EventTypeNetwork:
		return pb.EventType_EVENT_TYPE_NETWORK
	case domain.EventTypeMemory:
		return pb.EventType_EVENT_TYPE_RESOURCE_USAGE
	case domain.EventTypeCPU:
		return pb.EventType_EVENT_TYPE_RESOURCE_USAGE
	case domain.EventTypeDisk:
		return pb.EventType_EVENT_TYPE_FILE_SYSTEM
	default:
		return pb.EventType_EVENT_TYPE_SYSCALL // Default for systemd events
	}
}

// MapSourceType maps domain source types to protobuf types for systemd
func (a *SystemdAdapter) MapSourceType(source domain.SourceType) pb.SourceType {
	switch source {
	case domain.SourceSystemd:
		return pb.SourceType_SOURCE_TYPE_JOURNALD
	case domain.SourceEBPF:
		return pb.SourceType_SOURCE_TYPE_EBPF
	case domain.SourceK8s:
		return pb.SourceType_SOURCE_TYPE_KUBERNETES_API
	case domain.SourceCNI:
		return pb.SourceType_SOURCE_TYPE_KUBERNETES_API
	default:
		return pb.SourceType_SOURCE_TYPE_JOURNALD
	}
}

// ExtractMessage extracts the message from a UnifiedEvent for systemd
func (a *SystemdAdapter) ExtractMessage(event *domain.UnifiedEvent) string {
	if event.Application != nil && event.Application.Message != "" {
		return event.Application.Message
	}
	if event.Semantic != nil && event.Semantic.Narrative != "" {
		return event.Semantic.Narrative
	}
	if event.Kubernetes != nil && event.Kubernetes.Message != "" {
		return event.Kubernetes.Message
	}
	// Construct message from kernel/system data if available
	if event.Kernel != nil {
		if event.Kernel.Syscall != "" {
			return fmt.Sprintf("System call: %s (PID: %d)", event.Kernel.Syscall, event.Kernel.PID)
		}
		if event.Kernel.Comm != "" {
			return fmt.Sprintf("Process: %s (PID: %d)", event.Kernel.Comm, event.Kernel.PID)
		}
	}
	return fmt.Sprintf("Systemd event %s from %s", event.Type, event.Source)
}

// CreateEventContext creates systemd-specific event context
func (a *SystemdAdapter) CreateEventContext(event *domain.UnifiedEvent) *pb.EventContext {
	if event.Entity == nil {
		return nil
	}

	hostname := ""
	unitName := ""
	if event.Entity.Labels != nil {
		hostname = event.Entity.Labels["hostname"]
		unitName = event.Entity.Labels["unit"]
	}

	return &pb.EventContext{
		Node:      hostname,
		Service:   unitName, // For systemd, service represents the systemd unit
		Namespace: event.Entity.Namespace,
		Labels:    event.Entity.Labels,
	}
}

// ExtractAttributes extracts systemd-specific attributes from the event
func (a *SystemdAdapter) ExtractAttributes(event *domain.UnifiedEvent) map[string]string {
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

	// Add kernel/system-specific attributes
	if event.Kernel != nil {
		if event.Kernel.Syscall != "" {
			attributes["kernel.syscall"] = event.Kernel.Syscall
		}
		if event.Kernel.PID != 0 {
			attributes["kernel.pid"] = fmt.Sprintf("%d", event.Kernel.PID)
		}
		if event.Kernel.TID != 0 {
			attributes["kernel.tid"] = fmt.Sprintf("%d", event.Kernel.TID)
		}
		if event.Kernel.UID != 0 {
			attributes["kernel.uid"] = fmt.Sprintf("%d", event.Kernel.UID)
		}
		if event.Kernel.GID != 0 {
			attributes["kernel.gid"] = fmt.Sprintf("%d", event.Kernel.GID)
		}
		if event.Kernel.Comm != "" {
			attributes["kernel.comm"] = event.Kernel.Comm
		}
		if event.Kernel.ReturnCode != 0 {
			attributes["kernel.return_code"] = fmt.Sprintf("%d", event.Kernel.ReturnCode)
		}
		if event.Kernel.CPUCore != 0 {
			attributes["kernel.cpu_core"] = fmt.Sprintf("%d", event.Kernel.CPUCore)
		}

		// Add syscall arguments
		for k, v := range event.Kernel.Args {
			attributes["kernel.arg."+k] = v
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

	// Add Kubernetes-specific attributes if present
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

		// Add K8s labels with prefix
		for k, v := range event.Kubernetes.Labels {
			attributes["k8s.label."+k] = v
		}

		// Add K8s annotations with prefix
		for k, v := range event.Kubernetes.Annotations {
			attributes["k8s.annotation."+k] = v
		}
	}

	return attributes
}

// NewTapioGRPCClient creates a new systemd Tapio gRPC client
func NewTapioGRPCClient(serverAddr string) (*common.TapioGRPCClient, error) {
	adapter := NewSystemdAdapter()
	return common.NewTapioGRPCClient(serverAddr, adapter)
}

// NewTapioGRPCClientWithConfig creates a new systemd Tapio gRPC client with custom config
func NewTapioGRPCClientWithConfig(config *common.TapioClientConfig) (*common.TapioGRPCClient, error) {
	adapter := NewSystemdAdapter()
	return common.NewTapioGRPCClientWithConfig(config, adapter)
}
