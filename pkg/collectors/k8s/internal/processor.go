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

// ProcessEvent converts a raw Kubernetes event to a domain event
func (p *eventProcessor) ProcessEvent(ctx context.Context, raw core.RawEvent) (domain.Event, error) {
	// Create Kubernetes event data
	eventData := p.createKubernetesData(raw)

	// Determine severity based on event type and content
	severity := p.determineSeverity(raw)

	// Create the domain event
	event := domain.Event{
		ID:         fmt.Sprintf("k8s_%s_%s_%s_%d", raw.ResourceKind, raw.Namespace, raw.Name, raw.Timestamp.UnixNano()),
		Type:       string(domain.EventTypeKubernetes),
		Source:     string(domain.SourceK8s),
		Timestamp:  raw.Timestamp,
		Data:       eventData,
		Context:    p.createContextData(raw),
		Severity:   string(severity),
		Confidence: 1.0, // K8s events are direct observations
		Attributes: map[string]interface{}{
			"hash":      p.generateHash(raw),
			"signature": fmt.Sprintf("%s:%s", raw.ResourceKind, string(raw.Type)),
			"kind":      raw.ResourceKind,
			"namespace": raw.Namespace,
			"name":      raw.Name,
			"type":      string(raw.Type),
		},
	}

	return event, nil
}

// createKubernetesData creates a Kubernetes event data map
func (p *eventProcessor) createKubernetesData(raw core.RawEvent) map[string]interface{} {
	data := map[string]interface{}{
		"resource": map[string]interface{}{
			"api_version": p.extractAPIVersion(raw),
			"kind":        raw.ResourceKind,
			"namespace":   raw.Namespace,
			"name":        raw.Name,
		},
		"event_type": string(raw.Type),
		"count":      1,
	}

	// Extract additional information based on resource type
	switch raw.ResourceKind {
	case "Pod":
		p.enrichPodData(data, raw)
	case "Node":
		p.enrichNodeData(data, raw)
	case "Service":
		p.enrichServiceData(data, raw)
	case "Event":
		p.enrichEventData(data, raw)
	}

	// Handle state changes for MODIFIED events
	if raw.Type == core.EventTypeModified && raw.OldObject != nil {
		data["old_state"] = p.extractState(raw.OldObject)
		data["new_state"] = p.extractState(raw.Object)
	}

	return data
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

// createContextData creates the event context data
func (p *eventProcessor) createContextData(raw core.RawEvent) map[string]interface{} {
	context := map[string]interface{}{
		"kind":      raw.ResourceKind,
		"namespace": raw.Namespace,
		"name":      raw.Name,
		"cluster":   "", // Would be set from cluster info
	}

	// Create base labels
	labels := map[string]string{
		"kind":      raw.ResourceKind,
		"namespace": raw.Namespace,
		"name":      raw.Name,
	}

	// Extract labels from the object if available
	if obj, ok := raw.Object.(metav1.Object); ok {
		for k, v := range obj.GetLabels() {
			labels[k] = v
		}
	}

	context["labels"] = labels
	context["tags"] = []string{
		"kubernetes",
		raw.ResourceKind,
		string(raw.Type),
	}

	// Add resource reference
	context["resource"] = map[string]string{
		"kind":      raw.ResourceKind,
		"namespace": raw.Namespace,
		"name":      raw.Name,
	}

	return context
}

// determineSeverity determines the event severity
func (p *eventProcessor) determineSeverity(raw core.RawEvent) domain.SeverityLevel {
	// Handle K8s Event objects specially
	if event, ok := raw.Object.(*corev1.Event); ok {
		switch event.Type {
		case corev1.EventTypeWarning:
			return domain.SeverityWarning
		case corev1.EventTypeNormal:
			return domain.SeverityLow
		}

		// Check specific reasons
		switch event.Reason {
		case "Failed", "FailedCreate", "FailedDelete", "FailedScheduling":
			return domain.SeverityHigh
		case "Killing", "Evicted", "NodeNotReady":
			return domain.SeverityCritical
		case "BackOff", "Unhealthy":
			return domain.SeverityWarning
		}
	}

	// For resource events
	switch raw.Type {
	case core.EventTypeDeleted:
		if raw.ResourceKind == "Pod" || raw.ResourceKind == "Node" {
			return domain.SeverityWarning
		}
		return domain.SeverityLow

	case core.EventTypeError:
		return domain.SeverityHigh

	default:
		return domain.SeverityLow
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
