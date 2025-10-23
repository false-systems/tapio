package domain

import (
	"fmt"
)

// K8sContext provides Kubernetes context for event enrichment
type K8sContext struct {
	ClusterID string

	// Pod context
	PodName      string
	PodNamespace string
	PodLabels    map[string]string
	PodIP        string
	HostIP       string

	// Owner context (Deployment/StatefulSet/DaemonSet)
	OwnerKind string
	OwnerName string

	// Service context (if event involves service)
	ServiceName string
	ServiceIP   string

	// Node context
	NodeName string
	Zone     string
	Region   string
}

// EnrichWithK8sContext transforms ObserverEvent to TapioEvent with graph entities
// This is the bridge between Community (raw data) and Enterprise (graph correlation)
func EnrichWithK8sContext(event *ObserverEvent, ctx *K8sContext) (*TapioEvent, error) {
	if event == nil {
		return nil, fmt.Errorf("nil event")
	}
	if ctx == nil {
		return nil, fmt.Errorf("nil K8s context")
	}

	// Map event type string to EventType enum
	eventType := mapToEventType(event.Type)

	// Determine severity and outcome from event data
	severity := determineSeverity(event)
	outcome := determineOutcome(event)

	// Extract graph entities from event + K8s context
	entities := extractEntities(event, ctx)

	// Extract relationships between entities
	relationships := extractRelationships(event, ctx, entities)

	return &TapioEvent{
		ID:        event.ID,
		Type:      eventType,
		Subtype:   event.Subtype,
		Severity:  severity,
		Outcome:   outcome,
		Timestamp: event.Timestamp,

		// Graph correlation (THE VALUE-ADD for Enterprise)
		Entities:      entities,
		Relationships: relationships,

		// Same typed data as ObserverEvent
		NetworkData:   event.NetworkData,
		KernelData:    event.KernelData,
		ContainerData: event.ContainerData,
		K8sData:       event.K8sData,
		ProcessData:   event.ProcessData,

		// Multi-cluster support
		ClusterID: ctx.ClusterID,
		Namespace: ctx.PodNamespace,
		Labels:    ctx.PodLabels,
	}, nil
}

// mapToEventType converts string event type to EventType enum
func mapToEventType(eventType string) EventType {
	// Extract base type from event type (e.g., "tcp_connect" → "network")
	// Check specific patterns first, then broader categories
	switch {
	// Performance patterns (check before network patterns)
	case contains(eventType, "performance_", "rtt_", "latency_", "degradation"):
		return EventTypePerformance
	case contains(eventType, "resource_", "cpu_", "memory_"):
		return EventTypeResource
	// Kernel patterns
	case contains(eventType, "oom_", "syscall_", "signal_"):
		return EventTypeKernel
	// Container patterns
	case contains(eventType, "container_", "docker_"):
		return EventTypeContainer
	// K8s resource patterns
	case contains(eventType, "deployment_"):
		return EventTypeDeployment
	case contains(eventType, "pod_"):
		return EventTypePod
	case contains(eventType, "service_"):
		return EventTypeService
	case contains(eventType, "volume_", "pvc_"):
		return EventTypeVolume
	case contains(eventType, "config_", "configmap_", "secret_"):
		return EventTypeConfig
	case contains(eventType, "health_"):
		return EventTypeHealth
	// Network patterns (broader, check last)
	case contains(eventType, "tcp_", "udp_", "http_", "dns_", "connection_"):
		return EventTypeNetwork
	default:
		return EventTypeNetwork // Default fallback
	}
}

// determineSeverity classifies event severity from event data
func determineSeverity(event *ObserverEvent) Severity {
	// High severity events
	if contains(event.Type,
		"oom_kill", "crash", "failed", "timeout", "refused") {
		return SeverityError
	}

	// HTTP 5xx errors
	if event.NetworkData != nil && event.NetworkData.HTTPStatusCode >= 500 {
		return SeverityError
	}

	// HTTP 4xx warnings
	if event.NetworkData != nil && event.NetworkData.HTTPStatusCode >= 400 {
		return SeverityWarning
	}

	// Performance degradation
	if event.NetworkData != nil && event.NetworkData.RTTDegradation > 50.0 {
		return SeverityWarning
	}

	// Default info level
	return SeverityInfo
}

// determineOutcome classifies event outcome
func determineOutcome(event *ObserverEvent) Outcome {
	// Check HTTP status first (most specific)
	if event.NetworkData != nil && event.NetworkData.HTTPStatusCode > 0 {
		if event.NetworkData.HTTPStatusCode >= 200 && event.NetworkData.HTTPStatusCode < 300 {
			return OutcomeSuccess
		}
		if event.NetworkData.HTTPStatusCode >= 400 {
			return OutcomeFailure
		}
	}

	// Failure patterns
	if contains(event.Type,
		"failed", "error", "timeout", "refused", "crash", "oom_kill") {
		return OutcomeFailure
	}

	// Success patterns
	if contains(event.Type,
		"established", "success", "completed", "ready") {
		return OutcomeSuccess
	}

	return OutcomeUnknown
}

// extractEntities creates graph nodes from event + K8s context
func extractEntities(event *ObserverEvent, ctx *K8sContext) []Entity {
	entities := make([]Entity, 0, 4)

	// Always include source pod entity (if available)
	if ctx.PodName != "" {
		entities = append(entities, Entity{
			Type:      EntityTypePod,
			ID:        fmt.Sprintf("%s/%s", ctx.PodNamespace, ctx.PodName),
			Name:      ctx.PodName,
			ClusterID: ctx.ClusterID,
			Namespace: ctx.PodNamespace,
			Labels:    ctx.PodLabels,
		})
	}

	// Add owner entity (Deployment/StatefulSet/DaemonSet)
	if ctx.OwnerKind != "" && ctx.OwnerName != "" {
		entityType := mapOwnerToEntityType(ctx.OwnerKind)
		entities = append(entities, Entity{
			Type:      entityType,
			ID:        fmt.Sprintf("%s/%s", ctx.PodNamespace, ctx.OwnerName),
			Name:      ctx.OwnerName,
			ClusterID: ctx.ClusterID,
			Namespace: ctx.PodNamespace,
		})
	}

	// Add node entity
	if ctx.NodeName != "" {
		entities = append(entities, Entity{
			Type:      EntityTypeNode,
			ID:        ctx.NodeName,
			Name:      ctx.NodeName,
			ClusterID: ctx.ClusterID,
			Attributes: map[string]string{
				"zone":   ctx.Zone,
				"region": ctx.Region,
			},
		})
	}

	// Network event: Add destination service entity
	if event.NetworkData != nil && ctx.ServiceName != "" {
		entities = append(entities, Entity{
			Type:      EntityTypeService,
			ID:        fmt.Sprintf("%s/%s", ctx.PodNamespace, ctx.ServiceName),
			Name:      ctx.ServiceName,
			ClusterID: ctx.ClusterID,
			Namespace: ctx.PodNamespace,
		})
	}

	return entities
}

// extractRelationships creates graph edges between entities
func extractRelationships(event *ObserverEvent, ctx *K8sContext, entities []Entity) []Relationship {
	relationships := make([]Relationship, 0, 3)

	// Find entities by type
	var podEntity, ownerEntity, nodeEntity, serviceEntity *Entity
	for i := range entities {
		switch entities[i].Type {
		case EntityTypePod:
			podEntity = &entities[i]
		case EntityTypeDeployment, EntityTypeStatefulSet, EntityTypeDaemonSet:
			ownerEntity = &entities[i]
		case EntityTypeNode:
			nodeEntity = &entities[i]
		case EntityTypeService:
			serviceEntity = &entities[i]
		}
	}

	// Owner → manages → Pod
	if ownerEntity != nil && podEntity != nil {
		relationships = append(relationships, Relationship{
			Type:   RelationshipManages,
			Source: *ownerEntity,
			Target: *podEntity,
		})
	}

	// Node → contains → Pod
	if nodeEntity != nil && podEntity != nil {
		relationships = append(relationships, Relationship{
			Type:   RelationshipContains,
			Source: *nodeEntity,
			Target: *podEntity,
		})
	}

	// Pod → connects_to → Service (network events)
	if event.NetworkData != nil && podEntity != nil && serviceEntity != nil {
		relationships = append(relationships, Relationship{
			Type:   RelationshipConnectsTo,
			Source: *podEntity,
			Target: *serviceEntity,
			Labels: map[string]string{
				"protocol": event.NetworkData.Protocol,
			},
		})
	}

	return relationships
}

// mapOwnerToEntityType converts K8s owner kind to EntityType
func mapOwnerToEntityType(ownerKind string) EntityType {
	switch ownerKind {
	case "Deployment":
		return EntityTypeDeployment
	case "StatefulSet":
		return EntityTypeStatefulSet
	case "DaemonSet":
		return EntityTypeDaemonSet
	default:
		return EntityTypePod // Fallback
	}
}

// contains checks if string contains any of the substrings
func contains(s string, substrings ...string) bool {
	for _, sub := range substrings {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
