package internal

import (
	"context"
	"fmt"
	"strings"

	"github.com/yairfalse/tapio/pkg/collectors/cni/core"
	"github.com/yairfalse/tapio/pkg/domain"
)

// cniEventProcessor implements core.EventProcessor
// This is the key component that creates UnifiedEvent directly from CNI sources,
// enabling rich semantic correlation and eliminating conversion overhead.
type cniEventProcessor struct {
	nodeHostname string
}

// newCNIEventProcessor creates a new CNI event processor
func newCNIEventProcessor() core.EventProcessor {
	return &cniEventProcessor{
		nodeHostname: getNodeHostname(),
	}
}

// ProcessEvent converts a CNI raw event to a UnifiedEvent
// This is where the magic happens - we create rich semantic correlation context
// directly at the source, enabling the analytics engine to perform sophisticated
// correlation analysis without downstream conversion overhead.
func (p *cniEventProcessor) ProcessEvent(ctx context.Context, raw core.CNIRawEvent) (*domain.UnifiedEvent, error) {
	// Generate unique event ID
	eventID := fmt.Sprintf("cni_%s_%s_%d", raw.PluginName, string(raw.Operation), raw.Timestamp.UnixNano())

	// Determine event type and severity
	cniPlugin := core.DetectCNIPlugin(raw.PluginName, raw.Command, raw.RawConfig)
	eventType := core.MapOperationToEventType(raw.Operation, raw.Success, raw.AssignedIP != "")
	severity := p.mapSeverityToDomain(core.DetermineCNISeverity(raw.Operation, raw.Success, raw.Duration, cniPlugin))

	// Create the UnifiedEvent with rich semantic context
	unifiedEvent := &domain.UnifiedEvent{
		// Core event identification
		ID:        eventID,
		Timestamp: raw.Timestamp,
		Source:    string(domain.SourceCNI),
		Type:      p.mapEventTypeToDomain(eventType),

		// Kubernetes correlation context
		Kubernetes: p.createKubernetesContext(raw),

		// Network correlation context
		Network: p.createNetworkContext(raw, cniPlugin),

		// Application context for service correlation
		Application: p.createApplicationContext(raw),

		// Trace correlation
		TraceContext: p.extractTraceContext(raw),

		// Impact context
		Impact: &domain.ImpactContext{
			Severity:       string(severity),
			BusinessImpact: p.calculateBusinessImpact(raw),
			CustomerFacing: raw.PodNamespace == "production" || raw.PodNamespace == "default",
		},

		// Original data for debugging and detailed analysis
		RawData: []byte(p.generateMessage(raw, eventType)),
	}

	return unifiedEvent, nil
}

// createKubernetesContext creates rich Kubernetes correlation context
func (p *cniEventProcessor) createKubernetesContext(raw core.CNIRawEvent) *domain.KubernetesData {
	if raw.PodUID == "" && raw.PodName == "" {
		return nil
	}

	return &domain.KubernetesData{
		EventType:   "Normal",
		Reason:      "CNIOperation",
		Object:      fmt.Sprintf("pod/%s", raw.PodName),
		ObjectKind:  "Pod",
		Message:     fmt.Sprintf("CNI %s operation for pod %s", raw.Operation, raw.PodName),
		Labels:      raw.Labels,
		Annotations: raw.Annotations,
	}
}

// createNetworkContext creates comprehensive network correlation context
func (p *cniEventProcessor) createNetworkContext(raw core.CNIRawEvent, plugin core.CNIPlugin) *domain.NetworkData {
	if raw.AssignedIP == "" && raw.InterfaceName == "" {
		return nil
	}

	netData := &domain.NetworkData{
		Protocol:  "CNI",
		SourceIP:  raw.AssignedIP,
		Direction: "setup",
		Latency:   raw.Duration.Nanoseconds(),
		Headers: map[string]string{
			"cni-plugin":     string(plugin),
			"cni-version":    raw.CNIVersion,
			"interface-name": raw.InterfaceName,
			"subnet":         raw.Subnet,
			"gateway":        raw.Gateway,
		},
	}

	return netData
}

// createApplicationContext creates application correlation context
func (p *cniEventProcessor) createApplicationContext(raw core.CNIRawEvent) *domain.ApplicationData {
	if raw.PodName == "" {
		return nil
	}

	level := "info"
	if !raw.Success {
		level = "error"
	}

	// Extract application information from pod name and labels
	appName := extractApplicationName(raw.PodName, raw.Labels)

	return &domain.ApplicationData{
		Level:   level,
		Message: fmt.Sprintf("CNI operation %s for application %s", raw.Operation, appName),
		Logger:  "cni-collector",
		Custom: map[string]interface{}{
			"pod_name":     raw.PodName,
			"pod_uid":      raw.PodUID,
			"container_id": raw.ContainerID,
			"node_name":    raw.NodeName,
		},
	}
}

// calculateBusinessImpact calculates business impact score
func (p *cniEventProcessor) calculateBusinessImpact(raw core.CNIRawEvent) float64 {
	impact := 0.1 // Base impact

	// Higher impact for production namespaces
	if raw.PodNamespace == "production" {
		impact += 0.7
	} else if raw.PodNamespace == "default" {
		impact += 0.5
	}

	// Higher impact for failures
	if !raw.Success {
		impact += 0.3
	}

	if impact > 1.0 {
		impact = 1.0
	}

	return impact
}

// extractTraceContext extracts distributed tracing context
func (p *cniEventProcessor) extractTraceContext(raw core.CNIRawEvent) *domain.TraceContext {
	// Look for trace context in annotations or environment
	traceID := extractTraceID(raw.Annotations)
	spanID := extractSpanID(raw.Annotations)

	if traceID == "" && spanID == "" {
		return nil
	}

	return &domain.TraceContext{
		TraceID: traceID,
		SpanID:  spanID,
	}
}

// Mapping helper functions

func (p *cniEventProcessor) mapEventTypeToDomain(eventType core.CNIEventType) domain.EventType {
	switch eventType {
	case core.CNIEventTypeIPAllocation, core.CNIEventTypeIPDeallocation:
		return domain.EventTypeNetwork
	case core.CNIEventTypeInterfaceSetup, core.CNIEventTypeInterfaceTeardown:
		return domain.EventTypeNetwork
	case core.CNIEventTypePolicyApply, core.CNIEventTypePolicyRemove:
		return domain.EventTypeSystem
	case core.CNIEventTypeError:
		return domain.EventTypeSystem
	default:
		return domain.EventTypeNetwork
	}
}

func (p *cniEventProcessor) mapSeverityToDomain(severity core.CNISeverity) string {
	switch severity {
	case core.CNISeverityInfo:
		return "info"
	case core.CNISeverityWarning:
		return "warning"
	case core.CNISeverityError:
		return "error"
	case core.CNISeverityCritical:
		return "critical"
	default:
		return "info"
	}
}

func (p *cniEventProcessor) generateMessage(raw core.CNIRawEvent, eventType core.CNIEventType) string {
	if !raw.Success {
		return fmt.Sprintf("CNI %s operation failed for pod %s: %s",
			raw.Operation, raw.PodName, raw.ErrorMessage)
	}

	switch eventType {
	case core.CNIEventTypeIPAllocation:
		return fmt.Sprintf("IP %s allocated to pod %s via %s",
			raw.AssignedIP, raw.PodName, raw.PluginName)
	case core.CNIEventTypeIPDeallocation:
		return fmt.Sprintf("IP %s released from pod %s via %s",
			raw.AssignedIP, raw.PodName, raw.PluginName)
	case core.CNIEventTypeInterfaceSetup:
		return fmt.Sprintf("Network interface %s configured for pod %s",
			raw.InterfaceName, raw.PodName)
	case core.CNIEventTypeInterfaceTeardown:
		return fmt.Sprintf("Network interface %s removed for pod %s",
			raw.InterfaceName, raw.PodName)
	default:
		return fmt.Sprintf("CNI %s operation completed for pod %s",
			raw.Operation, raw.PodName)
	}
}

// Helper extraction functions

func extractApplicationName(podName string, labels map[string]string) string {
	// Try standard Kubernetes labels first
	if app, ok := labels["app.kubernetes.io/name"]; ok {
		return app
	}
	if app, ok := labels["app"]; ok {
		return app
	}

	// Extract from pod name (common patterns)
	if idx := strings.LastIndex(podName, "-"); idx > 0 {
		// Remove replica suffix (e.g., "nginx-deployment-5c6c8d7f9-abc123" -> "nginx-deployment")
		name := podName[:idx]
		if idx2 := strings.LastIndex(name, "-"); idx2 > 0 {
			return name[:idx2]
		}
		return name
	}

	return podName
}

func extractApplicationVersion(labels, annotations map[string]string) string {
	if version, ok := labels["app.kubernetes.io/version"]; ok {
		return version
	}
	if version, ok := labels["version"]; ok {
		return version
	}
	if version, ok := annotations["app.version"]; ok {
		return version
	}
	return ""
}

func extractTraceID(annotations map[string]string) string {
	traceKeys := []string{"trace-id", "traceId", "traceid", "x-trace-id"}
	for _, key := range traceKeys {
		if value, ok := annotations[key]; ok {
			return value
		}
	}
	return ""
}

func extractSpanID(annotations map[string]string) string {
	spanKeys := []string{"span-id", "spanId", "spanid", "x-span-id"}
	for _, key := range spanKeys {
		if value, ok := annotations[key]; ok {
			return value
		}
	}
	return ""
}
