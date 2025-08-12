package transformer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
)

// EventTransformer converts RawEvents to UnifiedEvents with OTEL context
type EventTransformer struct {
	mu sync.RWMutex
}

// NewEventTransformer creates a new event transformer
func NewEventTransformer() *EventTransformer {
	return &EventTransformer{}
}

// Transform converts a RawEvent to UnifiedEvent with full context
func (t *EventTransformer) Transform(ctx context.Context, raw collectors.RawEvent) (*domain.UnifiedEvent, error) {
	// Create base unified event
	event := &domain.UnifiedEvent{
		ID:        domain.GenerateEventID(),
		Timestamp: raw.Timestamp,
		Type:      mapCollectorType(raw.Type),
		Source:    raw.Type,
		Category:  getCategoryFromType(raw.Type),
		Severity:  domain.EventSeverityInfo, // Default, will be enriched
		Tags:      []string{raw.Type},
	}

	// Extract OTEL context from both RawEvent fields and metadata
	event.TraceContext = t.extractOTELContext(raw)

	// Add entity context
	event.Entity = &domain.EntityContext{
		Type:       raw.Type,
		Name:       raw.Metadata["name"],
		Namespace:  raw.Metadata["namespace"],
		UID:        raw.Metadata["id"],
		Attributes: make(map[string]string),
	}

	// Copy metadata to entity attributes
	for k, v := range raw.Metadata {
		if !strings.HasPrefix(k, "trace_") && !strings.HasPrefix(k, "span_") && !strings.HasPrefix(k, "baggage.") {
			event.Entity.Attributes[k] = v
		}
	}

	// Parse raw data based on collector type
	t.parseRawData(event, raw)

	// Infer semantic context (merge with existing)
	inferred := t.inferSemantics(ctx, event)
	if event.Semantic == nil {
		event.Semantic = inferred
	} else {
		// Merge inferred with existing
		if event.Semantic.Intent == "" {
			event.Semantic.Intent = inferred.Intent
		}
		if event.Semantic.Category == "" {
			event.Semantic.Category = inferred.Category
		}
		if event.Semantic.Confidence == 0 {
			event.Semantic.Confidence = inferred.Confidence
		}
		// Append tags
		event.Semantic.Tags = append(event.Semantic.Tags, inferred.Tags...)
	}

	return event, nil
}

// extractOTELContext extracts OTEL trace context from RawEvent fields and metadata
func (t *EventTransformer) extractOTELContext(raw collectors.RawEvent) *domain.TraceContext {
	// Check if we have any trace information
	if raw.TraceID == "" && raw.Metadata["trace_id"] == "" {
		return nil
	}

	ctx := &domain.TraceContext{
		// Prefer RawEvent fields over metadata
		TraceID:      raw.TraceID,
		SpanID:       raw.SpanID,
		ParentSpanID: raw.Metadata["parent_span_id"],
		TraceState:   raw.Metadata["trace_state"],
		Baggage:      make(map[string]string),
	}

	// Fallback to metadata if RawEvent fields are empty
	if ctx.TraceID == "" {
		ctx.TraceID = raw.Metadata["trace_id"]
	}
	if ctx.SpanID == "" {
		ctx.SpanID = raw.Metadata["span_id"]
	}

	// Extract baggage
	for k, v := range raw.Metadata {
		if strings.HasPrefix(k, "baggage.") {
			baggageKey := strings.TrimPrefix(k, "baggage.")
			ctx.Baggage[baggageKey] = v
		}
	}

	return ctx
}

// parseRawData parses collector-specific data
func (t *EventTransformer) parseRawData(event *domain.UnifiedEvent, raw collectors.RawEvent) {
	switch raw.Type {
	case "systemd":
		t.parseSystemdData(event, raw.Data)
	case "ebpf":
		t.parseEBPFData(event, raw.Data)
	case "kubeapi":
		t.parseKubeAPIData(event, raw.Data)
	case "etcd":
		t.parseEtcdData(event, raw.Data)
	case "cni":
		t.parseCNIData(event, raw.Data)
	default:
		event.Message = string(raw.Data)
	}
}

// parseSystemdData parses systemd collector data
func (t *EventTransformer) parseSystemdData(event *domain.UnifiedEvent, data []byte) {
	// Try JSON first
	var jsonData map[string]interface{}
	if err := json.Unmarshal(data, &jsonData); err == nil {
		if msg, ok := jsonData["message"].(string); ok {
			event.Message = msg
		}
		if level, ok := jsonData["level"].(string); ok {
			event.Severity = mapLogLevel(level)
		}

		// Add to application data
		event.Application = &domain.ApplicationData{
			Level:   string(event.Severity),
			Message: event.Message,
			Custom: &domain.ApplicationCustomData{
				Tags:    []string{},
				Labels:  make(map[string]string),
				Metrics: make(map[string]float64),
			},
		}

		// Map common JSON fields to structured data
		if method, ok := jsonData["method"].(string); ok {
			event.Application.Custom.HTTPMethod = method
		}
		if path, ok := jsonData["path"].(string); ok {
			event.Application.Custom.HTTPPath = path
		}
		if status, ok := jsonData["status"].(float64); ok {
			event.Application.Custom.HTTPStatusCode = int(status)
		}

		// Store remaining fields as labels
		for k, v := range jsonData {
			if k != "method" && k != "path" && k != "status" {
				event.Application.Custom.Labels[k] = fmt.Sprintf("%v", v)
			}
		}
	} else {
		// Raw log line
		event.Message = string(data)

		// Try to detect severity from common log patterns
		if strings.Contains(event.Message, "E1225") || strings.Contains(strings.ToLower(event.Message), "error") {
			event.Severity = domain.EventSeverityError
		} else if strings.Contains(strings.ToLower(event.Message), "warn") {
			event.Severity = domain.EventSeverityWarning
		}
	}
}

// parseEBPFData parses eBPF collector data
func (t *EventTransformer) parseEBPFData(event *domain.UnifiedEvent, data []byte) {
	event.Kernel = &domain.KernelData{}

	var jsonData map[string]interface{}
	if err := json.Unmarshal(data, &jsonData); err == nil {
		if syscall, ok := jsonData["syscall"].(string); ok {
			event.Kernel.Syscall = syscall
		}
		if pid, ok := jsonData["pid"].(float64); ok {
			event.Kernel.PID = uint32(pid)
		}
		if comm, ok := jsonData["comm"].(string); ok {
			event.Kernel.Comm = comm
		}

		event.Message = fmt.Sprintf("eBPF: %s syscall by %s (PID: %v)",
			event.Kernel.Syscall, event.Kernel.Comm, event.Kernel.PID)
	} else {
		event.Message = string(data)
	}
}

// parseK8sData parses Kubernetes collector data
func (t *EventTransformer) parseKubeAPIData(event *domain.UnifiedEvent, data []byte) {
	event.Kubernetes = &domain.KubernetesData{}

	var jsonData map[string]interface{}
	if err := json.Unmarshal(data, &jsonData); err == nil {
		if eventType, ok := jsonData["type"].(string); ok {
			event.Kubernetes.EventType = eventType
			if eventType == "Warning" {
				event.Severity = domain.EventSeverityWarning
			}
		}

		if reason, ok := jsonData["reason"].(string); ok {
			event.Kubernetes.Reason = reason
		}

		if msg, ok := jsonData["message"].(string); ok {
			event.Message = msg
		}

		// Parse object details
		if obj, ok := jsonData["object"].(map[string]interface{}); ok {
			if kind, ok := obj["kind"].(string); ok {
				event.Kubernetes.ObjectKind = kind
			}
			if name, ok := obj["name"].(string); ok {
				// Store in Object field as "kind/name" format
				event.Kubernetes.Object = fmt.Sprintf("%s/%s", event.Kubernetes.ObjectKind, name)
				event.Entity.Name = name
			}
			if ns, ok := obj["namespace"].(string); ok {
				event.Entity.Namespace = ns
			}
		}

		// Set entity type from K8s object kind
		if event.Kubernetes.ObjectKind != "" {
			event.Entity.Type = strings.ToLower(event.Kubernetes.ObjectKind)
		}
	} else {
		event.Message = string(data)
	}
}

// parseEtcdData parses etcd collector data
func (t *EventTransformer) parseEtcdData(event *domain.UnifiedEvent, data []byte) {
	var jsonData map[string]interface{}
	if err := json.Unmarshal(data, &jsonData); err == nil {
		if op, ok := jsonData["operation"].(string); ok {
			event.Message = fmt.Sprintf("etcd %s operation", op)
		}

		if key, ok := jsonData["key"].(string); ok {
			// Check if it's a K8s resource
			if strings.Contains(key, "/registry/") {
				event.Tags = append(event.Tags, "k8s-resource-update")
				if event.Semantic == nil {
					event.Semantic = &domain.SemanticContext{
						Tags: []string{"k8s-resource-update"},
					}
				} else {
					event.Semantic.Tags = append(event.Semantic.Tags, "k8s-resource-update")
				}
			}
		}
	} else {
		event.Message = string(data)
	}
}

// parseCNIData parses CNI collector data
func (t *EventTransformer) parseCNIData(event *domain.UnifiedEvent, data []byte) {
	event.Network = &domain.NetworkData{}

	var jsonData map[string]interface{}
	if err := json.Unmarshal(data, &jsonData); err == nil {
		if action, ok := jsonData["action"].(string); ok {
			event.Message = fmt.Sprintf("CNI %s action", action)
			// Store action in entity attributes
			if event.Entity.Attributes == nil {
				event.Entity.Attributes = make(map[string]string)
			}
			event.Entity.Attributes["action"] = action
		}

		// Map specific fields to NetworkData
		if ip, ok := jsonData["ip"].(string); ok {
			event.Network.SourceIP = ip
			if event.Entity.Attributes == nil {
				event.Entity.Attributes = make(map[string]string)
			}
			event.Entity.Attributes["ip"] = ip
		}

		if iface, ok := jsonData["interface"].(string); ok {
			event.Network.InterfaceName = iface
		}

		// Store pod/namespace info in entity
		if pod, ok := jsonData["pod"].(string); ok {
			event.Entity.Name = pod
		}
		if ns, ok := jsonData["namespace"].(string); ok {
			event.Entity.Namespace = ns
		}
	} else {
		event.Message = string(data)
	}
}

// inferSemantics infers semantic context from the event
func (t *EventTransformer) inferSemantics(ctx context.Context, event *domain.UnifiedEvent) *domain.SemanticContext {
	semantic := &domain.SemanticContext{
		Tags:       []string{},
		Confidence: 0.5, // Default confidence
	}

	// K8s OOM specific
	if event.Kubernetes != nil && event.Kubernetes.Reason == "OOMKilling" {
		semantic.Intent = "memory_exhaustion"
		semantic.Category = "resource_management"
		semantic.Tags = append(semantic.Tags, "oom", "pod-failure", "critical")
		semantic.Confidence = 0.95
		event.Severity = domain.EventSeverityCritical
		return semantic
	}

	// Network failures
	if event.Network != nil && event.Network.StatusCode == 504 {
		semantic.Intent = "network_failure"
		semantic.Category = "connectivity"
		semantic.Tags = append(semantic.Tags, "timeout", "network", "availability")
		semantic.Confidence = 0.85
		return semantic
	}

	// Disk issues
	if strings.Contains(strings.ToLower(event.Message), "no space left on device") {
		semantic.Intent = "disk_exhaustion"
		semantic.Category = "storage"
		semantic.Tags = append(semantic.Tags, "disk-full", "storage", "critical")
		semantic.Confidence = 0.9
		event.Severity = domain.EventSeverityCritical
		return semantic
	}

	// Default semantic based on event type
	switch event.Type {
	case domain.EventTypeSystem:
		semantic.Category = "system"
		semantic.Tags = append(semantic.Tags, "system")
	case domain.EventTypeNetwork:
		semantic.Category = "network"
		semantic.Tags = append(semantic.Tags, "network")
	case domain.EventTypeKubernetes:
		semantic.Category = "orchestration"
		semantic.Tags = append(semantic.Tags, "k8s")
	case domain.EventTypeLog:
		semantic.Category = "logging"
		semantic.Tags = append(semantic.Tags, "log")
	}

	return semantic
}

// mapCollectorType maps collector type to domain event type
func mapCollectorType(collectorType string) domain.EventType {
	switch collectorType {
	case "systemd":
		return domain.EventTypeLog
	case "ebpf":
		return domain.EventTypeSystem
	case "kubeapi":
		return domain.EventTypeKubernetes
	case "etcd":
		return domain.EventTypeSystem
	case "cni":
		return domain.EventTypeNetwork
	default:
		return domain.EventTypeSystem
	}
}

// getCategoryFromType returns category based on collector type
func getCategoryFromType(collectorType string) string {
	switch collectorType {
	case "systemd":
		return "system"
	case "ebpf":
		return "kernel"
	case "kubeapi":
		return "orchestration"
	case "etcd":
		return "storage"
	case "cni":
		return "network"
	default:
		return "unknown"
	}
}

// mapLogLevel maps log levels to severity
func mapLogLevel(level string) domain.EventSeverity {
	switch strings.ToLower(level) {
	case "debug":
		return domain.EventSeverityDebug
	case "info":
		return domain.EventSeverityInfo
	case "warn", "warning":
		return domain.EventSeverityWarning
	case "error":
		return domain.EventSeverityError
	case "critical", "crit", "fatal":
		return domain.EventSeverityCritical
	default:
		return domain.EventSeverityInfo
	}
}
