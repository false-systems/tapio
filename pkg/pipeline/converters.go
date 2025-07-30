package pipeline

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// EBPFConverter converts eBPF events to UnifiedEvents
type EBPFConverter struct{}

func NewEBPFConverter() EventConverter {
	return &EBPFConverter{}
}

func (c *EBPFConverter) SourceType() string {
	return "ebpf"
}

func (c *EBPFConverter) Convert(ctx context.Context, raw collectors.RawEvent) (*domain.UnifiedEvent, error) {
	// Parse event type from metadata
	eventType := raw.Metadata["event_type"]

	// Build base UnifiedEvent
	event := domain.NewUnifiedEvent().
		WithSource("ebpf").
		WithTimestamp(raw.Timestamp).
		WithType(domain.EventTypeKernel)

	// Add semantic context based on event type
	switch eventType {
	case "memory_alloc", "memory_free":
		event = event.WithSemantic("memory-event", "resource", "memory", "info")

		// Parse memory event data
		if len(raw.Data) >= 32 {
			size := binary.LittleEndian.Uint64(raw.Data[24:32])
			event = event.WithCustomData("memory_size", size)
		}

	case "oom_kill":
		event = event.WithSemantic("oom-kill", "availability", "memory", "critical")

	case "network":
		event = event.WithType(domain.EventTypeNetwork).
			WithSemantic("network-activity", "connectivity", "network", "info")

	default:
		event = event.WithSemantic(eventType, "system", "kernel", "info")
	}

	// Add kernel data if available
	if len(raw.Data) >= 12 {
		pid := binary.LittleEndian.Uint32(raw.Data[8:12])
		tid := binary.LittleEndian.Uint32(raw.Data[12:16])

		event = event.WithKernelData(eventType, pid)
		built := event.Build()

		// Add additional kernel fields
		if built.Kernel != nil {
			built.Kernel.TID = tid
			built.Kernel.CPUCore = parseInt(raw.Metadata["cpu"])
		}

		return built, nil
	}

	return event.Build(), nil
}

// K8sConverter converts Kubernetes API events to UnifiedEvents
type K8sConverter struct{}

func NewK8sConverter() EventConverter {
	return &K8sConverter{}
}

func (c *K8sConverter) SourceType() string {
	return "k8s"
}

func (c *K8sConverter) Convert(ctx context.Context, raw collectors.RawEvent) (*domain.UnifiedEvent, error) {
	// Parse K8s object
	var obj map[string]interface{}
	if err := json.Unmarshal(raw.Data, &obj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal k8s object: %w", err)
	}

	// Extract metadata
	resource := raw.Metadata["resource"]
	eventType := raw.Metadata["type"]
	namespace := raw.Metadata["namespace"]
	name := raw.Metadata["name"]

	// Build UnifiedEvent
	event := domain.NewUnifiedEvent().
		WithSource("k8s").
		WithTimestamp(raw.Timestamp).
		WithType(domain.EventTypeKubernetes).
		WithEntity("k8s-resource", name, resource)

	// Add semantic context based on resource and event type
	switch resource {
	case "pods":
		switch eventType {
		case "ADDED":
			event = event.WithSemantic("pod-created", "lifecycle", "pod", "info")
		case "MODIFIED":
			event = event.WithSemantic("pod-updated", "lifecycle", "pod", "info")
		case "DELETED":
			event = event.WithSemantic("pod-deleted", "lifecycle", "pod", "warning")
		}

	case "nodes":
		event = event.WithSemantic("node-event", "infrastructure", "node", "info")

	case "services":
		event = event.WithSemantic("service-event", "networking", "service", "info")

	case "events":
		// K8s Event objects need special handling
		if eventObj, ok := obj["object"].(map[string]interface{}); ok {
			if reason, ok := eventObj["reason"].(string); ok {
				event = event.WithSemantic(reason, "kubernetes", "event",
					determineEventSeverity(eventObj))
			}
		}
	}

	// Add K8s context
	built := event.Build()
	built.K8s = &domain.K8sContext{
		Namespace: namespace,
		Resource:  resource,
		Name:      name,
		UID:       raw.Metadata["uid"],
	}

	// Store raw object for further processing
	built.RawData = raw.Data

	return built, nil
}

// SystemdConverter converts systemd events to UnifiedEvents
type SystemdConverter struct{}

func NewSystemdConverter() EventConverter {
	return &SystemdConverter{}
}

func (c *SystemdConverter) SourceType() string {
	return "systemd"
}

func (c *SystemdConverter) Convert(ctx context.Context, raw collectors.RawEvent) (*domain.UnifiedEvent, error) {
	// Parse systemd journal entry
	var entry map[string]interface{}
	if err := json.Unmarshal(raw.Data, &entry); err != nil {
		return nil, fmt.Errorf("failed to unmarshal systemd entry: %w", err)
	}

	// Extract unit name
	unit := ""
	if u, ok := entry["UNIT"].(string); ok {
		unit = u
	} else if u, ok := entry["_SYSTEMD_UNIT"].(string); ok {
		unit = u
	}

	// Build UnifiedEvent
	event := domain.NewUnifiedEvent().
		WithSource("systemd").
		WithTimestamp(raw.Timestamp).
		WithType(domain.EventTypeSystem).
		WithEntity("systemd-unit", unit, "service").
		WithSemantic("systemd-log", "system", "service", "info")

	// Add message if available
	if msg, ok := entry["MESSAGE"].(string); ok {
		event = event.WithApplicationData("info", msg)
	}

	return event.Build(), nil
}

// CNIConverter converts CNI events to UnifiedEvents
type CNIConverter struct{}

func NewCNIConverter() EventConverter {
	return &CNIConverter{}
}

func (c *CNIConverter) SourceType() string {
	return "cni"
}

func (c *CNIConverter) Convert(ctx context.Context, raw collectors.RawEvent) (*domain.UnifiedEvent, error) {
	// CNI events are typically log lines
	logLine := string(raw.Data)

	// Build UnifiedEvent
	event := domain.NewUnifiedEvent().
		WithSource("cni").
		WithTimestamp(raw.Timestamp).
		WithType(domain.EventTypeNetwork).
		WithSemantic("cni-event", "networking", "cni", "info").
		WithApplicationData("info", logLine)

	return event.Build(), nil
}

// Helper functions

func parseInt(s string) int {
	var i int
	fmt.Sscanf(s, "%d", &i)
	return i
}

func determineEventSeverity(event map[string]interface{}) string {
	if eventType, ok := event["type"].(string); ok {
		switch eventType {
		case "Warning":
			return "warning"
		case "Error":
			return "error"
		default:
			return "info"
		}
	}
	return "info"
}

// ConverterRegistry creates and registers all converters
func RegisterConverters(pipeline *CollectorPipeline) {
	pipeline.RegisterConverter(NewEBPFConverter())
	pipeline.RegisterConverter(NewK8sConverter())
	pipeline.RegisterConverter(NewSystemdConverter())
	pipeline.RegisterConverter(NewCNIConverter())
}
