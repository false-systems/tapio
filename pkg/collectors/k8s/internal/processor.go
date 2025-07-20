package internal

import (
	"context"
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors/k8s/core"
	"github.com/yairfalse/tapio/pkg/domain"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// eventProcessor implements core.EventProcessor
type eventProcessor struct{}

func newEventProcessor() core.EventProcessor {
	return &eventProcessor{}
}

// ProcessEvent converts a raw Kubernetes event to a UnifiedEvent
// This creates rich semantic correlation context directly from K8s events
func (p *eventProcessor) ProcessEvent(ctx context.Context, raw core.RawEvent) (*domain.UnifiedEvent, error) {
	// Generate unique event ID
	eventID := domain.GenerateEventID()

	// Determine event type and severity
	eventType := p.mapEventTypeToDomain(raw.Type)
	severity := p.determineSeverity(raw)

	// Create semantic context based on K8s resource and action
	semantic := p.createSemanticContext(raw)

	// Create entity context from K8s resource
	entity := p.createEntityContext(raw)

	// Create Kubernetes-specific data
	k8sData := p.createKubernetesData(raw)

	// Create impact context based on severity and resource type
	impact := p.createImpactContext(raw, severity)

	// Build the UnifiedEvent
	event := &domain.UnifiedEvent{
		ID:        eventID,
		Timestamp: raw.Timestamp,
		Type:      eventType,
		Source:    string(domain.SourceK8s),

		// Rich semantic correlation context
		Semantic: semantic,
		Entity:   entity,

		// K8s-specific data
		Kubernetes: k8sData,

		// Impact and correlation
		Impact: impact,
	}

	return event, nil
}

// mapEventTypeToDomain maps K8s event types to domain event types
func (p *eventProcessor) mapEventTypeToDomain(k8sType core.EventType) domain.EventType {
	switch k8sType {
	case core.EventTypeAdded:
		return domain.EventTypeKubernetes
	case core.EventTypeModified:
		return domain.EventTypeKubernetes
	case core.EventTypeDeleted:
		return domain.EventTypeKubernetes
	case core.EventTypeError:
		return domain.EventTypeSystem
	default:
		return domain.EventTypeKubernetes
	}
}

// createSemanticContext creates semantic context for K8s events
func (p *eventProcessor) createSemanticContext(raw core.RawEvent) *domain.SemanticContext {
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

// createEntityContext creates entity context from K8s resource
func (p *eventProcessor) createEntityContext(raw core.RawEvent) *domain.EntityContext {
	// Extract labels and UID from the object if available
	var labels map[string]string
	var uid string

	if obj, ok := raw.Object.(metav1.Object); ok {
		labels = obj.GetLabels()
		uid = string(obj.GetUID())
	}

	return &domain.EntityContext{
		Type:      raw.ResourceKind,
		Name:      raw.Name,
		Namespace: raw.Namespace,
		UID:       uid,
		Labels:    labels,
		Attributes: map[string]string{
			"api_version": p.extractAPIVersion(raw),
			"event_type":  string(raw.Type),
		},
	}
}

// createImpactContext creates impact context based on severity and resource type
func (p *eventProcessor) createImpactContext(raw core.RawEvent, severity string) *domain.ImpactContext {
	businessImpact := p.calculateBusinessImpact(raw, severity)
	affectedServices := p.determineAffectedServices(raw)
	customerFacing := p.isCustomerFacing(raw)

	return &domain.ImpactContext{
		Severity:         severity,
		BusinessImpact:   businessImpact,
		AffectedServices: affectedServices,
		CustomerFacing:   customerFacing,
		SLOImpact:        severity == "critical" || severity == "high",
	}
}

// createKubernetesData creates Kubernetes-specific data for UnifiedEvent
func (p *eventProcessor) createKubernetesData(raw core.RawEvent) *domain.KubernetesData {
	// Extract labels and annotations from the object if available
	var labels, annotations map[string]string
	var resourceVersion string

	if obj, ok := raw.Object.(metav1.Object); ok {
		labels = obj.GetLabels()
		annotations = obj.GetAnnotations()
		resourceVersion = obj.GetResourceVersion()
	}

	// Extract message and reason based on resource type
	message, reason, eventType := p.extractMessageAndReason(raw)

	k8sData := &domain.KubernetesData{
		EventType:       eventType,
		Reason:          reason,
		Object:          fmt.Sprintf("%s/%s", raw.ResourceKind, raw.Name),
		ObjectKind:      raw.ResourceKind,
		Message:         message,
		Action:          string(raw.Type),
		APIVersion:      p.extractAPIVersion(raw),
		ResourceVersion: resourceVersion,
		Labels:          labels,
		Annotations:     annotations,
	}

	return k8sData
}

// extractMessageAndReason extracts message, reason, and event type from K8s resources
func (p *eventProcessor) extractMessageAndReason(raw core.RawEvent) (string, string, string) {
	switch raw.ResourceKind {
	case "Pod":
		if pod, ok := raw.Object.(*corev1.Pod); ok {
			return fmt.Sprintf("Pod %s: %s", pod.Name, pod.Status.Phase), string(pod.Status.Phase), "Normal"
		}
	case "Event":
		if event, ok := raw.Object.(*corev1.Event); ok {
			return event.Message, event.Reason, event.Type
		}
	case "Node":
		if node, ok := raw.Object.(*corev1.Node); ok {
			for _, cond := range node.Status.Conditions {
				if cond.Type == corev1.NodeReady {
					if cond.Status == corev1.ConditionTrue {
						return fmt.Sprintf("Node %s is ready", node.Name), "Ready", "Normal"
					} else {
						return fmt.Sprintf("Node %s is not ready: %s", node.Name, cond.Message), "NotReady", "Warning"
					}
				}
			}
		}
	}

	return fmt.Sprintf("%s %s: %s", raw.ResourceKind, raw.Name, string(raw.Type)), string(raw.Type), "Normal"
}

// determineSemanticIntent determines the semantic intent of a K8s event
func (p *eventProcessor) determineSemanticIntent(raw core.RawEvent) string {
	switch raw.ResourceKind {
	case "Pod":
		switch raw.Type {
		case core.EventTypeAdded:
			return "pod-created"
		case core.EventTypeDeleted:
			return "pod-terminated"
		case core.EventTypeModified:
			if pod, ok := raw.Object.(*corev1.Pod); ok {
				switch pod.Status.Phase {
				case corev1.PodRunning:
					return "pod-running"
				case corev1.PodFailed:
					return "pod-failed"
				case corev1.PodSucceeded:
					return "pod-completed"
				}
			}
			return "pod-state-change"
		}
	case "Node":
		switch raw.Type {
		case core.EventTypeAdded:
			return "node-joined"
		case core.EventTypeDeleted:
			return "node-removed"
		case core.EventTypeModified:
			return "node-state-change"
		}
	case "Service":
		switch raw.Type {
		case core.EventTypeAdded:
			return "service-created"
		case core.EventTypeDeleted:
			return "service-removed"
		case core.EventTypeModified:
			return "service-updated"
		}
	case "Event":
		if event, ok := raw.Object.(*corev1.Event); ok {
			switch event.Reason {
			case "Failed", "FailedCreate", "FailedDelete":
				return "operation-failed"
			case "Killing", "Evicted":
				return "pod-evicted"
			case "BackOff":
				return "backoff-restart"
			}
		}
		return "k8s-event"
	}

	return fmt.Sprintf("%s-%s", raw.ResourceKind, string(raw.Type))
}

// determineSemanticCategory determines the semantic category
func (p *eventProcessor) determineSemanticCategory(raw core.RawEvent) string {
	if event, ok := raw.Object.(*corev1.Event); ok {
		switch event.Type {
		case corev1.EventTypeWarning:
			return "availability"
		case corev1.EventTypeNormal:
			return "operations"
		}

		switch event.Reason {
		case "Failed", "FailedCreate", "FailedDelete", "FailedScheduling":
			return "reliability"
		case "Killing", "Evicted":
			return "resource-management"
		case "BackOff", "Unhealthy":
			return "performance"
		}
	}

	switch raw.Type {
	case core.EventTypeError:
		return "reliability"
	case core.EventTypeDeleted:
		return "operations"
	default:
		return "operations"
	}
}

// generateSemanticTags generates semantic tags for correlation
func (p *eventProcessor) generateSemanticTags(raw core.RawEvent) []string {
	tags := []string{"kubernetes", raw.ResourceKind}

	if raw.Namespace != "" {
		tags = append(tags, "namespaced")
	}

	// Add resource-specific tags
	switch raw.ResourceKind {
	case "Pod":
		tags = append(tags, "workload", "container")
	case "Node":
		tags = append(tags, "infrastructure", "cluster")
	case "Service":
		tags = append(tags, "networking", "discovery")
	case "Event":
		tags = append(tags, "observability", "audit")
	}

	// Add event type tags
	switch raw.Type {
	case core.EventTypeError:
		tags = append(tags, "error", "critical-path")
	case core.EventTypeDeleted:
		tags = append(tags, "lifecycle", "cleanup")
	}

	return tags
}

// generateNarrative creates human-readable description
func (p *eventProcessor) generateNarrative(raw core.RawEvent) string {
	message, _, _ := p.extractMessageAndReason(raw)
	return fmt.Sprintf("Kubernetes %s event: %s", raw.ResourceKind, message)
}

// calculateSemanticConfidence calculates confidence in semantic classification
func (p *eventProcessor) calculateSemanticConfidence(raw core.RawEvent) float64 {
	// K8s events are well-structured, so confidence is high
	switch raw.ResourceKind {
	case "Event":
		return 0.95 // K8s Event objects are very reliable
	case "Pod", "Node", "Service":
		return 0.90 // Core resources are well-defined
	default:
		return 0.80 // Other resources are still reliable
	}
}

// calculateBusinessImpact calculates business impact score
func (p *eventProcessor) calculateBusinessImpact(raw core.RawEvent, severity string) float64 {
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

	// Adjust based on resource type
	switch raw.ResourceKind {
	case "Node":
		base += 0.2 // Node issues affect multiple workloads
	case "Service":
		base += 0.1 // Service issues affect availability
	case "Pod":
		// Check if it's a system pod
		if raw.Namespace == "kube-system" || raw.Namespace == "kube-public" {
			base += 0.2
		}
	}

	if base > 1.0 {
		base = 1.0
	}

	return base
}

// determineAffectedServices determines which services might be affected
func (p *eventProcessor) determineAffectedServices(raw core.RawEvent) []string {
	services := []string{}

	switch raw.ResourceKind {
	case "Node":
		services = append(services, "cluster-scheduler", "node-management")
	case "Pod":
		if raw.Namespace == "kube-system" {
			services = append(services, "kubernetes-control-plane")
		} else {
			services = append(services, fmt.Sprintf("%s-workload", raw.Namespace))
		}
	case "Service":
		services = append(services, "service-discovery", "networking")
	}

	return services
}

// isCustomerFacing determines if the event affects customer-facing services
func (p *eventProcessor) isCustomerFacing(raw core.RawEvent) bool {
	// System namespaces are usually not customer-facing
	if raw.Namespace == "kube-system" || raw.Namespace == "kube-public" || raw.Namespace == "kube-node-lease" {
		return false
	}

	// Services are often customer-facing
	if raw.ResourceKind == "Service" {
		return true
	}

	// Pod failures in application namespaces affect customers
	if raw.ResourceKind == "Pod" && raw.Type == core.EventTypeError {
		return true
	}

	return false
}

// enrichPodData adds Pod-specific information
func (p *eventProcessor) enrichPodData(data map[string]interface{}, raw core.RawEvent) {
	if pod, ok := raw.Object.(*corev1.Pod); ok {
		data["message"] = fmt.Sprintf("Pod %s in namespace %s: %s", pod.Name, pod.Namespace, pod.Status.Phase)
		data["reason"] = string(pod.Status.Phase)
		data["resource_version"] = pod.ResourceVersion
		data["phase"] = string(pod.Status.Phase)

		// Add container information if available
		if len(pod.Status.ContainerStatuses) > 0 {
			status := pod.Status.ContainerStatuses[0]
			if status.State.Waiting != nil {
				data["message"] = fmt.Sprintf("%s - Container waiting: %s", data["message"], status.State.Waiting.Reason)
				data["container_state"] = "waiting"
				data["container_reason"] = status.State.Waiting.Reason
			} else if status.State.Terminated != nil {
				data["message"] = fmt.Sprintf("%s - Container terminated: %s", data["message"], status.State.Terminated.Reason)
				data["container_state"] = "terminated"
				data["container_reason"] = status.State.Terminated.Reason
			} else if status.State.Running != nil {
				data["container_state"] = "running"
			}
		}
	}
}

// enrichNodeData adds Node-specific information
func (p *eventProcessor) enrichNodeData(data map[string]interface{}, raw core.RawEvent) {
	if node, ok := raw.Object.(*corev1.Node); ok {
		// Extract node conditions
		conditions := []string{}
		for _, cond := range node.Status.Conditions {
			if cond.Status == corev1.ConditionTrue {
				conditions = append(conditions, string(cond.Type))
			}
		}

		data["message"] = fmt.Sprintf("Node %s conditions: %v", node.Name, conditions)
		data["resource_version"] = node.ResourceVersion
		data["conditions"] = conditions

		// Check for important conditions
		for _, cond := range node.Status.Conditions {
			if cond.Type == corev1.NodeReady && cond.Status != corev1.ConditionTrue {
				data["reason"] = "NodeNotReady"
				data["message"] = fmt.Sprintf("Node %s is not ready: %s", node.Name, cond.Message)
				data["ready"] = false
				break
			} else if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
				data["ready"] = true
			}
		}
	}
}

// enrichServiceData adds Service-specific information
func (p *eventProcessor) enrichServiceData(data map[string]interface{}, raw core.RawEvent) {
	if svc, ok := raw.Object.(*corev1.Service); ok {
		data["message"] = fmt.Sprintf("Service %s in namespace %s", svc.Name, svc.Namespace)
		data["resource_version"] = svc.ResourceVersion
		data["service_type"] = string(svc.Spec.Type)

		if svc.Spec.Type == corev1.ServiceTypeLoadBalancer && len(svc.Status.LoadBalancer.Ingress) > 0 {
			ip := svc.Status.LoadBalancer.Ingress[0].IP
			data["message"] = fmt.Sprintf("%s - LoadBalancer IP: %s", data["message"], ip)
			data["load_balancer_ip"] = ip
		}
	}
}

// enrichEventData adds Event-specific information
func (p *eventProcessor) enrichEventData(data map[string]interface{}, raw core.RawEvent) {
	if event, ok := raw.Object.(*corev1.Event); ok {
		data["message"] = event.Message
		data["reason"] = event.Reason
		data["count"] = event.Count
		data["resource"] = map[string]interface{}{
			"api_version": event.InvolvedObject.APIVersion,
			"kind":        event.InvolvedObject.Kind,
			"namespace":   event.InvolvedObject.Namespace,
			"name":        event.InvolvedObject.Name,
			"uid":         string(event.InvolvedObject.UID),
		}
		data["field_path"] = event.InvolvedObject.FieldPath
		data["event_type"] = event.Type
		data["first_timestamp"] = event.FirstTimestamp
		data["last_timestamp"] = event.LastTimestamp
	}
}

// createEventContext creates the event context
func (p *eventProcessor) createEventContext(raw core.RawEvent) domain.EventContext {
	// Create base labels
	labels := map[string]string{
		"kind":       raw.ResourceKind,
		"namespace":  raw.Namespace,
		"name":       raw.Name,
		"event_type": string(raw.Type),
	}

	// Extract labels from the object if available
	if obj, ok := raw.Object.(metav1.Object); ok {
		for k, v := range obj.GetLabels() {
			labels[k] = v
		}
	}

	// Create metadata
	metadata := map[string]interface{}{
		"resource_path": fmt.Sprintf("/%s/%s/%s", raw.ResourceKind, raw.Namespace, raw.Name),
		"api_source":    "k8s-api",
	}

	// Extract node name for pod events
	var nodeName string
	if pod, ok := raw.Object.(*corev1.Pod); ok {
		nodeName = pod.Spec.NodeName
	}

	return domain.EventContext{
		Service:   "kubernetes",
		Component: raw.ResourceKind,
		Namespace: raw.Namespace,
		Host:      nodeName,
		Node:      nodeName,
		Labels:    labels,
		Metadata:  metadata,
		TraceID:   "", // Could be extracted from annotations
		SpanID:    "", // Could be extracted from annotations
	}
}

// determineSeverity determines the event severity
func (p *eventProcessor) determineSeverity(raw core.RawEvent) string {
	// Handle K8s Event objects specially
	if event, ok := raw.Object.(*corev1.Event); ok {
		switch event.Type {
		case corev1.EventTypeWarning:
			return "warning"
		case corev1.EventTypeNormal:
			return "info"
		}

		// Check specific reasons
		switch event.Reason {
		case "Failed", "FailedCreate", "FailedDelete", "FailedScheduling":
			return "high"
		case "Killing", "Evicted", "NodeNotReady":
			return "critical"
		case "BackOff", "Unhealthy":
			return "warning"
		}
	}

	// For resource events
	switch raw.Type {
	case core.EventTypeDeleted:
		if raw.ResourceKind == "Pod" || raw.ResourceKind == "Node" {
			return "warning"
		}
		return "info"

	case core.EventTypeError:
		return "high"

	default:
		return "info"
	}
}

// extractAPIVersion attempts to extract API version from the object
func (p *eventProcessor) extractAPIVersion(raw core.RawEvent) string {
	// This is a simplified version - in production, you'd use the actual TypeMeta
	switch raw.ResourceKind {
	case "Pod", "Service", "Node", "Event", "ConfigMap", "Secret":
		return "v1"
	case "Deployment":
		return "apps/v1"
	default:
		return ""
	}
}

// extractState extracts state information from an object
func (p *eventProcessor) extractState(obj interface{}) string {
	switch o := obj.(type) {
	case *corev1.Pod:
		return string(o.Status.Phase)
	case *corev1.Node:
		for _, cond := range o.Status.Conditions {
			if cond.Type == corev1.NodeReady {
				return string(cond.Status)
			}
		}
		return "Unknown"
	default:
		return ""
	}
}

// generateHash generates a hash for event deduplication
func (p *eventProcessor) generateHash(raw core.RawEvent) string {
	// Simple hash based on resource identity and event type
	return fmt.Sprintf("%s-%s-%s-%s-%d",
		raw.ResourceKind,
		raw.Namespace,
		raw.Name,
		raw.Type,
		raw.Timestamp.Unix())
}
