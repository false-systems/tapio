package internal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/cni/core"
	"github.com/yairfalse/tapio/pkg/domain"
)

// cniEventProcessor implements core.EventProcessor
type cniEventProcessor struct {
	nodeHostname string
}

// newCNIEventProcessor creates a new CNI event processor
func newCNIEventProcessor() core.EventProcessor {
	return &cniEventProcessor{
		nodeHostname: getNodeHostname(),
	}
}

// ProcessEvent converts a CNI raw event to a UnifiedEvent with rich semantic context
func (p *cniEventProcessor) ProcessEvent(ctx context.Context, raw core.CNIRawEvent) (*domain.UnifiedEvent, error) {
	// Generate cryptographically secure event ID like K8s collector
	eventID := p.generateSecureEventID()

	// Determine event type and severity with semantic intelligence
	severity := p.determineSeverity(raw)

	// Create the UnifiedEvent with rich semantic context
	unifiedEvent := &domain.UnifiedEvent{
		// Core event identification
		ID:        eventID,
		Timestamp: raw.Timestamp,
		Source:    string(domain.SourceCNI),
		Type:      domain.EventTypeNetwork,

		// Rich semantic correlation context - CRITICAL
		Semantic: p.createSemanticContext(raw),
		Entity:   p.createEntityContext(raw),

		// Network-specific data (NOT Application!)
		Network: p.createNetworkContext(raw),

		// Kubernetes correlation context
		Kubernetes: p.createKubernetesContext(raw),

		// Distributed tracing correlation context
		TraceContext: p.extractTraceContext(raw),

		// Complete impact context with all fields
		Impact: p.createImpactContext(raw, severity),
	}

	return unifiedEvent, nil
}

// generateSecureEventID generates cryptographically secure event ID like K8s
func (p *cniEventProcessor) generateSecureEventID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based ID
		return fmt.Sprintf("cni-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("cni-%s", hex.EncodeToString(bytes))
}

// createSemanticContext creates rich semantic context for CNI events
func (p *cniEventProcessor) createSemanticContext(raw core.CNIRawEvent) *domain.SemanticContext {
	intent := p.determineSemanticIntent(raw)
	category := p.determineSemanticCategory(raw)
	tags := p.generateSemanticTags(raw)
	narrative := p.generateNarrative(raw)
	confidence := p.calculateSemanticConfidence(raw)

	return &domain.SemanticContext{
		Intent:     intent,
		Category:   category,
		Tags:       tags,
		Narrative:  narrative,
		Confidence: confidence,
	}
}

// createEntityContext creates entity context for the affected pod/container
func (p *cniEventProcessor) createEntityContext(raw core.CNIRawEvent) *domain.EntityContext {
	return &domain.EntityContext{
		Type:      "Pod",
		Name:      raw.PodName,
		Namespace: raw.PodNamespace,
		UID:       raw.PodUID,
		Labels:    raw.Labels,
		Attributes: map[string]string{
			"container_id":   raw.ContainerID,
			"node_name":      raw.NodeName,
			"interface_name": raw.InterfaceName,
			"cni_plugin":     raw.PluginName,
			"operation":      string(raw.Operation),
		},
	}
}

// createNetworkContext creates network-specific context
func (p *cniEventProcessor) createNetworkContext(raw core.CNIRawEvent) *domain.NetworkData {
	if raw.AssignedIP == "" && raw.InterfaceName == "" {
		return nil
	}

	plugin := core.DetectCNIPlugin(raw.PluginName, raw.Command, raw.RawConfig)

	return &domain.NetworkData{
		Protocol:  "CNI",
		SourceIP:  raw.AssignedIP,
		Direction: p.determineNetworkDirection(raw),
		Latency:   raw.Duration.Nanoseconds(),
		Headers: map[string]string{
			"cni_plugin":     string(plugin),
			"cni_version":    raw.CNIVersion,
			"interface_name": raw.InterfaceName,
			"subnet":         raw.Subnet,
			"gateway":        raw.Gateway,
			"command":        raw.Command,
		},
	}
}

// createKubernetesContext creates Kubernetes correlation context
func (p *cniEventProcessor) createKubernetesContext(raw core.CNIRawEvent) *domain.KubernetesData {
	if raw.PodUID == "" && raw.PodName == "" {
		return nil
	}

	eventType := "Normal"
	reason := p.determineK8sReason(raw)
	if !raw.Success {
		eventType = "Warning"
	}

	return &domain.KubernetesData{
		EventType:       eventType,
		Reason:          reason,
		Object:          fmt.Sprintf("Pod/%s", raw.PodName),
		ObjectKind:      "Pod",
		Message:         p.generateK8sMessage(raw),
		Action:          string(raw.Operation),
		Labels:          raw.Labels,
		Annotations:     raw.Annotations,
		ResourceVersion: "", // CNI doesn't have resource versions
	}
}

// createImpactContext creates complete impact context with all fields
func (p *cniEventProcessor) createImpactContext(raw core.CNIRawEvent, severity string) *domain.ImpactContext {
	businessImpact := p.calculateBusinessImpact(raw, severity)
	affectedServices := p.determineAffectedServices(raw)
	customerFacing := p.isCustomerFacing(raw)
	sloImpact := p.hasSLOImpact(raw, severity)
	revenueImpacting := p.isRevenueImpacting(raw)
	affectedUsers := p.estimateAffectedUsers(raw)

	return &domain.ImpactContext{
		Severity:         severity,
		BusinessImpact:   businessImpact,
		AffectedServices: affectedServices,
		CustomerFacing:   customerFacing,
		SLOImpact:        sloImpact,
		RevenueImpacting: revenueImpacting,
		AffectedUsers:    int(affectedUsers),
	}
}

// determineSemanticIntent determines the semantic intent of CNI operations
func (p *cniEventProcessor) determineSemanticIntent(raw core.CNIRawEvent) string {
	switch raw.Operation {
	case core.CNIOperationAdd:
		if !raw.Success {
			return "network-setup-failed"
		}
		if raw.AssignedIP != "" {
			return "pod-network-attached"
		}
		return "network-interface-created"

	case core.CNIOperationDel:
		if !raw.Success {
			return "network-cleanup-failed"
		}
		return "pod-network-detached"

	case core.CNIOperationCheck:
		if !raw.Success {
			return "network-connectivity-lost"
		}
		return "network-health-verified"

	default:
		return fmt.Sprintf("cni-%s-operation", strings.ToLower(string(raw.Operation)))
	}
}

// determineSemanticCategory determines the semantic category
func (p *cniEventProcessor) determineSemanticCategory(raw core.CNIRawEvent) string {
	if !raw.Success {
		if raw.Duration > 30*time.Second {
			return "performance"
		}
		return "reliability"
	}

	switch raw.Operation {
	case core.CNIOperationAdd, core.CNIOperationDel:
		return "lifecycle"
	case core.CNIOperationCheck:
		return "health"
	default:
		return "operations"
	}
}

// generateSemanticTags generates semantic tags for correlation
func (p *cniEventProcessor) generateSemanticTags(raw core.CNIRawEvent) []string {
	tags := []string{"cni", "networking", raw.PluginName}

	// Add operation tags
	switch raw.Operation {
	case core.CNIOperationAdd:
		tags = append(tags, "pod-startup", "network-setup")
	case core.CNIOperationDel:
		tags = append(tags, "pod-teardown", "network-cleanup")
	case core.CNIOperationCheck:
		tags = append(tags, "health-check", "connectivity")
	}

	// Add plugin-specific tags
	plugin := core.DetectCNIPlugin(raw.PluginName, raw.Command, raw.RawConfig)
	switch plugin {
	case core.CNIPluginCalico:
		tags = append(tags, "calico", "network-policy")
	case core.CNIPluginFlannel:
		tags = append(tags, "flannel", "overlay-network")
	case core.CNIPluginWeave:
		tags = append(tags, "weave", "mesh-network")
	case core.CNIPluginCilium:
		tags = append(tags, "cilium", "ebpf", "network-policy")
	}

	// Add failure tags
	if !raw.Success {
		tags = append(tags, "failure", "network-error")
		if raw.Duration > 10*time.Second {
			tags = append(tags, "slow-operation")
		}
	}

	// Add namespace tags
	if raw.PodNamespace != "" {
		if raw.PodNamespace == "kube-system" {
			tags = append(tags, "system-critical")
		} else if raw.PodNamespace == "production" || raw.PodNamespace == "prod" {
			tags = append(tags, "production", "customer-facing")
		}
	}

	return tags
}

// generateNarrative creates human-readable description
func (p *cniEventProcessor) generateNarrative(raw core.CNIRawEvent) string {
	plugin := core.DetectCNIPlugin(raw.PluginName, raw.Command, raw.RawConfig)

	if !raw.Success {
		return fmt.Sprintf("CNI %s operation failed for pod %s in namespace %s: %s",
			raw.Operation, raw.PodName, raw.PodNamespace, raw.ErrorMessage)
	}

	switch raw.Operation {
	case core.CNIOperationAdd:
		if raw.AssignedIP != "" {
			return fmt.Sprintf("Network interface successfully configured for pod %s with IP %s using %s plugin",
				raw.PodName, raw.AssignedIP, plugin)
		}
		return fmt.Sprintf("Network setup initiated for pod %s using %s plugin", raw.PodName, plugin)

	case core.CNIOperationDel:
		return fmt.Sprintf("Network interface removed for pod %s using %s plugin", raw.PodName, plugin)

	case core.CNIOperationCheck:
		return fmt.Sprintf("Network connectivity verified for pod %s using %s plugin", raw.PodName, plugin)

	default:
		return fmt.Sprintf("CNI %s operation completed for pod %s", raw.Operation, raw.PodName)
	}
}

// calculateSemanticConfidence calculates confidence in semantic classification
func (p *cniEventProcessor) calculateSemanticConfidence(raw core.CNIRawEvent) float64 {
	confidence := 0.85 // Base confidence for CNI events

	// Higher confidence for well-known plugins
	plugin := core.DetectCNIPlugin(raw.PluginName, raw.Command, raw.RawConfig)
	switch plugin {
	case core.CNIPluginCalico, core.CNIPluginFlannel, core.CNIPluginWeave, core.CNIPluginCilium:
		confidence += 0.1
	}

	// Lower confidence for errors without clear messages
	if !raw.Success && raw.ErrorMessage == "" {
		confidence -= 0.2
	}

	// Higher confidence for complete pod information
	if raw.PodUID != "" && raw.PodName != "" && raw.PodNamespace != "" {
		confidence += 0.05
	}

	if confidence > 1.0 {
		confidence = 1.0
	} else if confidence < 0.0 {
		confidence = 0.0
	}

	return confidence
}

// determineSeverity determines event severity based on operation and outcome
func (p *cniEventProcessor) determineSeverity(raw core.CNIRawEvent) string {
	if !raw.Success {
		// Critical for system namespaces
		if raw.PodNamespace == "kube-system" || raw.PodNamespace == "kube-public" {
			return "critical"
		}

		// High for production namespaces
		if raw.PodNamespace == "production" || raw.PodNamespace == "prod" {
			return "high"
		}

		// Warning for other failures
		return "warning"
	}

	// Slow operations are warnings
	if raw.Duration > 10*time.Second {
		return "warning"
	}

	// Normal operations are info
	return "info"
}

// calculateBusinessImpact calculates business impact score
func (p *cniEventProcessor) calculateBusinessImpact(raw core.CNIRawEvent, severity string) float64 {
	base := 0.1

	// Adjust based on severity
	switch severity {
	case "critical":
		base = 0.9
	case "high":
		base = 0.7
	case "warning":
		base = 0.4
	case "info":
		base = 0.1
	}

	// Adjust based on namespace
	if raw.PodNamespace == "production" || raw.PodNamespace == "prod" {
		base += 0.3
	} else if raw.PodNamespace == "kube-system" {
		base += 0.2
	}

	// Failed operations have higher impact
	if !raw.Success {
		base += 0.2
	}

	// Long-running operations indicate performance issues
	if raw.Duration > 30*time.Second {
		base += 0.1
	}

	if base > 1.0 {
		base = 1.0
	}

	return base
}

// determineAffectedServices determines which services might be affected
func (p *cniEventProcessor) determineAffectedServices(raw core.CNIRawEvent) []string {
	services := []string{}

	// Network connectivity affects all pod services
	if raw.PodName != "" {
		services = append(services, fmt.Sprintf("%s-pod-network", raw.PodNamespace))
	}

	// System namespace affects core services
	if raw.PodNamespace == "kube-system" {
		services = append(services, "kubernetes-networking", "cluster-dns", "cluster-connectivity")
	}

	// Production namespace affects customer services
	if raw.PodNamespace == "production" || raw.PodNamespace == "prod" {
		services = append(services, "customer-applications", "api-gateway")
	}

	// CNI failures affect specific network services
	if !raw.Success {
		plugin := core.DetectCNIPlugin(raw.PluginName, raw.Command, raw.RawConfig)
		switch plugin {
		case core.CNIPluginCalico:
			services = append(services, "network-policy-enforcement")
		case core.CNIPluginCilium:
			services = append(services, "network-policy-enforcement", "ebpf-dataplane")
		}
	}

	return services
}

// isCustomerFacing determines if the event affects customer-facing services
func (p *cniEventProcessor) isCustomerFacing(raw core.CNIRawEvent) bool {
	// Production namespaces are customer-facing
	if raw.PodNamespace == "production" || raw.PodNamespace == "prod" {
		return true
	}

	// Default namespace often contains user workloads
	if raw.PodNamespace == "default" {
		return true
	}

	// Check labels for customer-facing indicators
	for k, v := range raw.Labels {
		if k == "tier" && (v == "frontend" || v == "api") {
			return true
		}
		if k == "customer-facing" && v == "true" {
			return true
		}
	}

	return false
}

// hasSLOImpact determines if the event impacts SLOs
func (p *cniEventProcessor) hasSLOImpact(raw core.CNIRawEvent, severity string) bool {
	// Critical and high severity events impact SLOs
	if severity == "critical" || severity == "high" {
		return true
	}

	// Network failures for customer-facing services impact SLOs
	if !raw.Success && p.isCustomerFacing(raw) {
		return true
	}

	// Long network setup times impact latency SLOs
	if raw.Duration > 5*time.Second && (raw.PodNamespace == "production" || raw.PodNamespace == "prod") {
		return true
	}

	return false
}

// isRevenueImpacting determines if the event impacts revenue
func (p *cniEventProcessor) isRevenueImpacting(raw core.CNIRawEvent) bool {
	// Check for revenue-critical labels
	for k, v := range raw.Labels {
		if k == "revenue-critical" && v == "true" {
			return true
		}
		if k == "service" && (v == "payment" || v == "checkout" || v == "billing") {
			return true
		}
	}

	// Network failures in production can impact revenue
	if !raw.Success && (raw.PodNamespace == "production" || raw.PodNamespace == "prod") {
		return true
	}

	return false
}

// estimateAffectedUsers estimates number of affected users
func (p *cniEventProcessor) estimateAffectedUsers(raw core.CNIRawEvent) int64 {
	// No users affected for successful operations
	if raw.Success && raw.Duration < 5*time.Second {
		return 0
	}

	// System namespace doesn't directly affect users
	if raw.PodNamespace == "kube-system" {
		return 0
	}

	// Production failures affect many users
	if !raw.Success && (raw.PodNamespace == "production" || raw.PodNamespace == "prod") {
		return 1000 // Estimate based on production impact
	}

	// Slow operations in production affect some users
	if raw.Duration > 10*time.Second && (raw.PodNamespace == "production" || raw.PodNamespace == "prod") {
		return 100
	}

	// Default namespace failures affect few users
	if !raw.Success && raw.PodNamespace == "default" {
		return 10
	}

	return 0
}

// Helper functions

func (p *cniEventProcessor) determineNetworkDirection(raw core.CNIRawEvent) string {
	switch raw.Operation {
	case core.CNIOperationAdd:
		return "ingress"
	case core.CNIOperationDel:
		return "egress"
	default:
		return "internal"
	}
}

func (p *cniEventProcessor) determineK8sReason(raw core.CNIRawEvent) string {
	if !raw.Success {
		switch raw.Operation {
		case core.CNIOperationAdd:
			return "NetworkSetupFailed"
		case core.CNIOperationDel:
			return "NetworkCleanupFailed"
		case core.CNIOperationCheck:
			return "NetworkCheckFailed"
		default:
			return "CNIOperationFailed"
		}
	}

	switch raw.Operation {
	case core.CNIOperationAdd:
		return "NetworkAttached"
	case core.CNIOperationDel:
		return "NetworkDetached"
	case core.CNIOperationCheck:
		return "NetworkHealthy"
	default:
		return "CNIOperation"
	}
}

func (p *cniEventProcessor) generateK8sMessage(raw core.CNIRawEvent) string {
	plugin := core.DetectCNIPlugin(raw.PluginName, raw.Command, raw.RawConfig)

	if !raw.Success {
		return fmt.Sprintf("CNI %s operation failed for pod using %s plugin: %s",
			raw.Operation, plugin, raw.ErrorMessage)
	}

	switch raw.Operation {
	case core.CNIOperationAdd:
		if raw.AssignedIP != "" {
			return fmt.Sprintf("Successfully attached network to pod with IP %s using %s plugin",
				raw.AssignedIP, plugin)
		}
		return fmt.Sprintf("Initiated network attachment for pod using %s plugin", plugin)

	case core.CNIOperationDel:
		return fmt.Sprintf("Successfully detached network from pod using %s plugin", plugin)

	case core.CNIOperationCheck:
		return fmt.Sprintf("Network connectivity healthy for pod using %s plugin", plugin)

	default:
		return fmt.Sprintf("CNI %s operation completed using %s plugin", raw.Operation, plugin)
	}
}

// extractTraceContext extracts distributed tracing context from CNI event annotations
func (p *cniEventProcessor) extractTraceContext(raw core.CNIRawEvent) *domain.TraceContext {
	// Look for trace context in pod annotations or environment
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

// extractTraceID extracts trace ID from various annotation keys
func extractTraceID(annotations map[string]string) string {
	traceKeys := []string{
		"trace-id",
		"traceId",
		"traceid",
		"x-trace-id",
		"opentelemetry.io/trace-id",
		"jaeger.trace-id",
	}

	for _, key := range traceKeys {
		if value, ok := annotations[key]; ok && value != "" {
			return value
		}
	}
	return ""
}

// extractSpanID extracts span ID from various annotation keys
func extractSpanID(annotations map[string]string) string {
	spanKeys := []string{
		"span-id",
		"spanId",
		"spanid",
		"x-span-id",
		"opentelemetry.io/span-id",
		"jaeger.span-id",
	}

	for _, key := range spanKeys {
		if value, ok := annotations[key]; ok && value != "" {
			return value
		}
	}
	return ""
}
