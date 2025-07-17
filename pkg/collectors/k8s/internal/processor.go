package internal

import (
	"context"
	"fmt"
	"time"

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
	// Create Kubernetes event payload
	payload := p.createKubernetesPayload(raw)
	
	// Determine severity based on event type and content
	severity := p.determineSeverity(raw)
	
	// Create the domain event
	event := domain.Event{
		ID:        domain.EventID(fmt.Sprintf("k8s_%s_%s_%s_%d", raw.ResourceKind, raw.Namespace, raw.Name, raw.Timestamp.UnixNano())),
		Type:      domain.EventTypeKubernetes,
		Source:    domain.SourceK8s,
		Timestamp: raw.Timestamp,
		Payload:   payload,
		Context:   p.createContext(raw),
		Metadata:  p.createMetadata(raw),
		Severity:  severity,
		Confidence: 1.0, // K8s events are direct observations
		Fingerprint: domain.EventFingerprint{
			Hash:      p.generateHash(raw),
			Signature: fmt.Sprintf("%s:%s", raw.ResourceKind, string(raw.Type)),
			Fields: map[string]string{
				"kind":      raw.ResourceKind,
				"namespace": raw.Namespace,
				"name":      raw.Name,
				"type":      string(raw.Type),
			},
		},
	}
	
	return event, nil
}

// createKubernetesPayload creates a Kubernetes event payload
func (p *eventProcessor) createKubernetesPayload(raw core.RawEvent) domain.KubernetesEventPayload {
	payload := domain.KubernetesEventPayload{
		Resource: domain.ResourceRef{
			APIVersion: p.extractAPIVersion(raw),
			Kind:       raw.ResourceKind,
			Namespace:  raw.Namespace,
			Name:       raw.Name,
		},
		EventType: string(raw.Type),
		Count:     1,
	}
	
	// Extract additional information based on resource type
	switch raw.ResourceKind {
	case "Pod":
		p.enrichPodPayload(&payload, raw)
	case "Node":
		p.enrichNodePayload(&payload, raw)
	case "Service":
		p.enrichServicePayload(&payload, raw)
	case "Event":
		p.enrichEventPayload(&payload, raw)
	}
	
	// Handle state changes for MODIFIED events
	if raw.Type == core.EventTypeModified && raw.OldObject != nil {
		payload.OldState = p.extractState(raw.OldObject)
		payload.NewState = p.extractState(raw.Object)
	}
	
	return payload
}

// enrichPodPayload adds Pod-specific information
func (p *eventProcessor) enrichPodPayload(payload *domain.KubernetesEventPayload, raw core.RawEvent) {
	if pod, ok := raw.Object.(*corev1.Pod); ok {
		payload.Message = fmt.Sprintf("Pod %s in namespace %s: %s", pod.Name, pod.Namespace, pod.Status.Phase)
		payload.Reason = string(pod.Status.Phase)
		
		// Extract resource version
		payload.ResourceVersion = pod.ResourceVersion
		
		// Add container information to message if available
		if len(pod.Status.ContainerStatuses) > 0 {
			status := pod.Status.ContainerStatuses[0]
			if status.State.Waiting != nil {
				payload.Message = fmt.Sprintf("%s - Container waiting: %s", payload.Message, status.State.Waiting.Reason)
			} else if status.State.Terminated != nil {
				payload.Message = fmt.Sprintf("%s - Container terminated: %s", payload.Message, status.State.Terminated.Reason)
			}
		}
	}
}

// enrichNodePayload adds Node-specific information
func (p *eventProcessor) enrichNodePayload(payload *domain.KubernetesEventPayload, raw core.RawEvent) {
	if node, ok := raw.Object.(*corev1.Node); ok {
		// Extract node conditions
		conditions := []string{}
		for _, cond := range node.Status.Conditions {
			if cond.Status == corev1.ConditionTrue {
				conditions = append(conditions, string(cond.Type))
			}
		}
		
		payload.Message = fmt.Sprintf("Node %s conditions: %v", node.Name, conditions)
		payload.ResourceVersion = node.ResourceVersion
		
		// Check for important conditions
		for _, cond := range node.Status.Conditions {
			if cond.Type == corev1.NodeReady && cond.Status != corev1.ConditionTrue {
				payload.Reason = "NodeNotReady"
				payload.Message = fmt.Sprintf("Node %s is not ready: %s", node.Name, cond.Message)
				break
			}
		}
	}
}

// enrichServicePayload adds Service-specific information
func (p *eventProcessor) enrichServicePayload(payload *domain.KubernetesEventPayload, raw core.RawEvent) {
	if svc, ok := raw.Object.(*corev1.Service); ok {
		payload.Message = fmt.Sprintf("Service %s in namespace %s", svc.Name, svc.Namespace)
		payload.ResourceVersion = svc.ResourceVersion
		
		if svc.Spec.Type == corev1.ServiceTypeLoadBalancer && len(svc.Status.LoadBalancer.Ingress) > 0 {
			payload.Message = fmt.Sprintf("%s - LoadBalancer IP: %s", payload.Message, svc.Status.LoadBalancer.Ingress[0].IP)
		}
	}
}

// enrichEventPayload adds Event-specific information
func (p *eventProcessor) enrichEventPayload(payload *domain.KubernetesEventPayload, raw core.RawEvent) {
	if event, ok := raw.Object.(*corev1.Event); ok {
		payload.Message = event.Message
		payload.Reason = event.Reason
		payload.Count = event.Count
		payload.Resource = domain.ResourceRef{
			APIVersion: event.InvolvedObject.APIVersion,
			Kind:       event.InvolvedObject.Kind,
			Namespace:  event.InvolvedObject.Namespace,
			Name:       event.InvolvedObject.Name,
			UID:        string(event.InvolvedObject.UID),
		}
		payload.FieldPath = event.InvolvedObject.FieldPath
		
		// Override event type with the K8s event type
		payload.EventType = event.Type
	}
}

// createContext creates the event context
func (p *eventProcessor) createContext(raw core.RawEvent) domain.EventContext {
	labels := domain.Labels{
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
	
	return domain.EventContext{
		Resource: &domain.ResourceRef{
			Kind:      raw.ResourceKind,
			Namespace: raw.Namespace,
			Name:      raw.Name,
		},
		Cluster:   "", // Would be set from cluster info
		Namespace: raw.Namespace,
		Labels:    labels,
		Tags: domain.Tags{
			"kubernetes",
			raw.ResourceKind,
			string(raw.Type),
		},
	}
}

// createMetadata creates the event metadata
func (p *eventProcessor) createMetadata(raw core.RawEvent) domain.EventMetadata {
	annotations := map[string]string{
		"event_type":    string(raw.Type),
		"resource_kind": raw.ResourceKind,
	}
	
	// Extract annotations from the object if available
	if obj, ok := raw.Object.(metav1.Object); ok {
		for k, v := range obj.GetAnnotations() {
			annotations[k] = v
		}
	}
	
	return domain.EventMetadata{
		SchemaVersion: "1.0",
		ProcessedAt:   time.Now(),
		ProcessedBy:   "k8s-collector",
		Annotations:   annotations,
	}
}

// determineSeverity determines the event severity
func (p *eventProcessor) determineSeverity(raw core.RawEvent) domain.Severity {
	// Handle K8s Event objects specially
	if event, ok := raw.Object.(*corev1.Event); ok {
		switch event.Type {
		case corev1.EventTypeWarning:
			return domain.SeverityWarn
		case corev1.EventTypeNormal:
			return domain.SeverityInfo
		}
		
		// Check specific reasons
		switch event.Reason {
		case "Failed", "FailedCreate", "FailedDelete", "FailedScheduling":
			return domain.SeverityError
		case "Killing", "Evicted", "NodeNotReady":
			return domain.SeverityCritical
		case "BackOff", "Unhealthy":
			return domain.SeverityWarn
		}
	}
	
	// For resource events
	switch raw.Type {
	case core.EventTypeDeleted:
		if raw.ResourceKind == "Pod" || raw.ResourceKind == "Node" {
			return domain.SeverityWarn
		}
		return domain.SeverityInfo
		
	case core.EventTypeError:
		return domain.SeverityError
		
	default:
		return domain.SeverityInfo
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