//go:build experimental
// +build experimental

package correlation

import (
	"fmt"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// OwnershipDimension correlates events by K8s ownership hierarchy
type OwnershipDimension struct {
	logger *zap.Logger
}

func NewOwnershipDimension(logger *zap.Logger) *OwnershipDimension {
	return &OwnershipDimension{logger: logger}
}

func (d *OwnershipDimension) FindCorrelations(event *domain.UnifiedEvent, graph *CorrelationGraph) []DimensionMatch {
	var matches []DimensionMatch

	if event.K8sContext == nil {
		return matches
	}

	k8s := event.K8sContext

	// Find events from same workload
	if k8s.WorkloadName != "" {
		related := graph.FindByWorkload(k8s.WorkloadKind, k8s.WorkloadName, k8s.Namespace)
		if len(related) > 1 { // More than just this event
			matches = append(matches, DimensionMatch{
				Dimension:  "ownership",
				Type:       "same_workload",
				Confidence: 0.9,
				Evidence: []string{
					fmt.Sprintf("All events from %s/%s", k8s.WorkloadKind, k8s.WorkloadName),
					fmt.Sprintf("%d related events found", len(related)),
				},
				Metadata: map[string]interface{}{
					"workload": fmt.Sprintf("%s/%s", k8s.WorkloadKind, k8s.WorkloadName),
					"events":   getEventIDs(related),
				},
			})
		}
	}

	// Find events from owner chain
	for _, owner := range k8s.OwnerReferences {
		related := graph.FindByOwner(owner.Kind, owner.Name, k8s.Namespace)
		if len(related) > 1 {
			matches = append(matches, DimensionMatch{
				Dimension:  "ownership",
				Type:       "owner_hierarchy",
				Confidence: 0.85,
				Evidence: []string{
					fmt.Sprintf("Common owner: %s/%s", owner.Kind, owner.Name),
					fmt.Sprintf("%d sibling resources affected", len(related)),
				},
				Metadata: map[string]interface{}{
					"owner":  fmt.Sprintf("%s/%s", owner.Kind, owner.Name),
					"events": getEventIDs(related),
				},
			})
		}
	}

	// Find events from pods with same labels
	if len(k8s.Labels) > 0 {
		// Check for app label
		if appLabel, ok := k8s.Labels["app"]; ok {
			related := graph.FindByLabel("app", appLabel, k8s.Namespace)
			if len(related) > 1 {
				matches = append(matches, DimensionMatch{
					Dimension:  "ownership",
					Type:       "same_application",
					Confidence: 0.8,
					Evidence: []string{
						fmt.Sprintf("Same app label: %s", appLabel),
						fmt.Sprintf("%d pods in application affected", len(related)),
					},
					Metadata: map[string]interface{}{
						"app":    appLabel,
						"events": getEventIDs(related),
					},
				})
			}
		}
	}

	return matches
}

// SpatialDimension correlates events by K8s topology (nodes, zones, namespaces)
type SpatialDimension struct {
	logger *zap.Logger
}

func NewSpatialDimension(logger *zap.Logger) *SpatialDimension {
	return &SpatialDimension{logger: logger}
}

func (d *SpatialDimension) FindCorrelations(event *domain.UnifiedEvent, graph *CorrelationGraph) []DimensionMatch {
	var matches []DimensionMatch

	if event.K8sContext == nil {
		return matches
	}

	k8s := event.K8sContext

	// Node-level correlation
	if k8s.NodeName != "" {
		related := graph.FindByNode(k8s.NodeName)
		if len(related) > 2 { // Multiple events on same node
			matches = append(matches, DimensionMatch{
				Dimension:  "spatial",
				Type:       "same_node",
				Confidence: 0.85,
				Evidence: []string{
					fmt.Sprintf("All events on node: %s", k8s.NodeName),
					fmt.Sprintf("%d events on this node", len(related)),
				},
				Metadata: map[string]interface{}{
					"node":   k8s.NodeName,
					"events": getEventIDs(related),
				},
			})
		}
	}

	// Zone-level correlation
	if k8s.Zone != "" {
		related := graph.FindByZone(k8s.Zone)
		if len(related) > 3 { // Multiple events in same zone
			matches = append(matches, DimensionMatch{
				Dimension:  "spatial",
				Type:       "same_zone",
				Confidence: 0.7,
				Evidence: []string{
					fmt.Sprintf("Events clustered in zone: %s", k8s.Zone),
					fmt.Sprintf("%d events in this zone", len(related)),
				},
				Metadata: map[string]interface{}{
					"zone":   k8s.Zone,
					"events": getEventIDs(related),
				},
			})
		}
	}

	// Namespace correlation
	related := graph.FindByNamespace(k8s.Namespace)
	if len(related) > 5 { // Many events in namespace
		matches = append(matches, DimensionMatch{
			Dimension:  "spatial",
			Type:       "namespace_wide",
			Confidence: 0.75,
			Evidence: []string{
				fmt.Sprintf("Multiple events in namespace: %s", k8s.Namespace),
				fmt.Sprintf("%d events in namespace", len(related)),
			},
			Metadata: map[string]interface{}{
				"namespace": k8s.Namespace,
				"events":    getEventIDs(related),
			},
		})
	}

	// Cross-namespace correlation (for network events)
	if event.Network != nil {
		// Check correlation hints for cross-namespace
		for _, hint := range event.CorrelationHints {
			if strings.Contains(hint, "dest_pod:") {
				parts := strings.Split(hint, ":")
				if len(parts) == 2 {
					destParts := strings.Split(parts[1], "/")
					if len(destParts) == 2 && destParts[0] != k8s.Namespace {
						matches = append(matches, DimensionMatch{
							Dimension:  "spatial",
							Type:       "cross_namespace",
							Confidence: 0.8,
							Evidence: []string{
								fmt.Sprintf("Cross-namespace traffic: %s -> %s", k8s.Namespace, destParts[0]),
								"Network flow between namespaces detected",
							},
							Metadata: map[string]interface{}{
								"source_ns": k8s.Namespace,
								"dest_ns":   destParts[0],
							},
						})
					}
				}
			}
		}
	}

	return matches
}

// TemporalDimension correlates events by time patterns
type TemporalDimension struct {
	logger *zap.Logger
	window time.Duration
}

func NewTemporalDimension(logger *zap.Logger, window time.Duration) *TemporalDimension {
	return &TemporalDimension{
		logger: logger,
		window: window,
	}
}

func (d *TemporalDimension) FindCorrelations(event *domain.UnifiedEvent, graph *CorrelationGraph) []DimensionMatch {
	var matches []DimensionMatch

	// Find events in time window
	startTime := event.Timestamp.Add(-d.window)
	endTime := event.Timestamp.Add(d.window)

	related := graph.FindInTimeRange(startTime, endTime)

	// Burst detection - many events in short time
	if len(related) > 10 {
		matches = append(matches, DimensionMatch{
			Dimension:  "temporal",
			Type:       "event_burst",
			Confidence: 0.9,
			Evidence: []string{
				fmt.Sprintf("%d events within %s", len(related), d.window),
				fmt.Sprintf("Burst detected around %s", event.Timestamp.Format(time.RFC3339)),
			},
			Metadata: map[string]interface{}{
				"burst_size": len(related),
				"time_range": d.window.String(),
				"events":     getEventIDs(related),
			},
		})
	}

	// Periodic pattern detection
	if pattern := d.detectPeriodicPattern(event, graph); pattern != nil {
		matches = append(matches, *pattern)
	}

	// Cascade detection - rapid succession
	cascade := d.detectCascade(event, graph)
	if len(cascade) > 2 {
		matches = append(matches, DimensionMatch{
			Dimension:  "temporal",
			Type:       "rapid_cascade",
			Confidence: 0.85,
			Evidence: []string{
				fmt.Sprintf("Cascade of %d events detected", len(cascade)),
				"Events occurring in rapid succession",
			},
			Metadata: map[string]interface{}{
				"cascade_length": len(cascade),
				"events":         getEventIDs(cascade),
			},
		})
	}

	return matches
}

func (d *TemporalDimension) detectPeriodicPattern(event *domain.UnifiedEvent, graph *CorrelationGraph) *DimensionMatch {
	// Look for similar events at regular intervals
	if event.Semantic == nil || event.Semantic.Intent == "" {
		return nil
	}

	similar := graph.FindBySemantic(event.Semantic.Intent)
	if len(similar) < 3 {
		return nil
	}

	// Check for regular intervals
	intervals := d.calculateIntervals(similar)
	if d.isPerodic(intervals) {
		avgInterval := d.averageInterval(intervals)
		return &DimensionMatch{
			Dimension:  "temporal",
			Type:       "periodic_pattern",
			Confidence: 0.8,
			Evidence: []string{
				fmt.Sprintf("Event occurs every ~%s", avgInterval),
				fmt.Sprintf("%d occurrences detected", len(similar)),
			},
			Metadata: map[string]interface{}{
				"period":     avgInterval.String(),
				"occurences": len(similar),
				"events":     getEventIDs(similar),
			},
		}
	}

	return nil
}

func (d *TemporalDimension) detectCascade(event *domain.UnifiedEvent, graph *CorrelationGraph) []*domain.UnifiedEvent {
	// Find events immediately after this one
	cascade := []*domain.UnifiedEvent{event}

	nextWindow := 10 * time.Second
	currentTime := event.Timestamp

	for i := 0; i < 10; i++ { // Max cascade length
		next := graph.FindInTimeRange(currentTime, currentTime.Add(nextWindow))
		if len(next) == 0 {
			break
		}

		// Find the closest event
		var closest *domain.UnifiedEvent
		minDiff := nextWindow

		for _, e := range next {
			if e.ID == event.ID {
				continue
			}
			diff := e.Timestamp.Sub(currentTime)
			if diff > 0 && diff < minDiff {
				minDiff = diff
				closest = e
			}
		}

		if closest != nil {
			cascade = append(cascade, closest)
			currentTime = closest.Timestamp
		} else {
			break
		}
	}

	return cascade
}

func (d *TemporalDimension) calculateIntervals(events []*domain.UnifiedEvent) []time.Duration {
	if len(events) < 2 {
		return nil
	}

	// Sort by time
	sorted := sortByTime(events)

	var intervals []time.Duration
	for i := 1; i < len(sorted); i++ {
		interval := sorted[i].Timestamp.Sub(sorted[i-1].Timestamp)
		intervals = append(intervals, interval)
	}

	return intervals
}

func (d *TemporalDimension) isPerodic(intervals []time.Duration) bool {
	if len(intervals) < 2 {
		return false
	}

	avg := d.averageInterval(intervals)
	threshold := avg / 10 // 10% variance allowed

	for _, interval := range intervals {
		diff := interval - avg
		if diff < 0 {
			diff = -diff
		}
		if diff > threshold {
			return false
		}
	}

	return true
}

func (d *TemporalDimension) averageInterval(intervals []time.Duration) time.Duration {
	if len(intervals) == 0 {
		return 0
	}

	var sum time.Duration
	for _, interval := range intervals {
		sum += interval
	}

	return sum / time.Duration(len(intervals))
}

// CausalDimension identifies cause-effect relationships
type CausalDimension struct {
	logger *zap.Logger
	window time.Duration
}

func NewCausalDimension(logger *zap.Logger, window time.Duration) *CausalDimension {
	return &CausalDimension{
		logger: logger,
		window: window,
	}
}

func (d *CausalDimension) FindCorrelations(event *domain.UnifiedEvent, graph *CorrelationGraph) []DimensionMatch {
	var matches []DimensionMatch

	// Resource exhaustion causality
	if match := d.findResourceExhaustion(event, graph); match != nil {
		matches = append(matches, *match)
	}

	// Error propagation
	if match := d.findErrorPropagation(event, graph); match != nil {
		matches = append(matches, *match)
	}

	// Service dependency failures
	if match := d.findDependencyFailure(event, graph); match != nil {
		matches = append(matches, *match)
	}

	// Configuration change impact
	if match := d.findConfigChangeImpact(event, graph); match != nil {
		matches = append(matches, *match)
	}

	return matches
}

func (d *CausalDimension) findResourceExhaustion(event *domain.UnifiedEvent, graph *CorrelationGraph) *DimensionMatch {
	// Check if this is an OOM or resource event
	isResourceEvent := false

	if event.Kubernetes != nil && event.Kubernetes.Reason == "OOMKilling" {
		isResourceEvent = true
	}

	if event.Metrics != nil && strings.Contains(event.Metrics.MetricName, "memory") {
		if event.Metrics.Value > 0.9 { // 90% usage
			isResourceEvent = true
		}
	}

	if !isResourceEvent {
		return nil
	}

	// Find subsequent failures
	afterEvents := graph.FindAfter(event.Timestamp, d.window)

	// Look for restarts, failures
	var consequences []*domain.UnifiedEvent
	for _, e := range afterEvents {
		if e.K8sContext != nil && e.K8sContext.WorkloadName == event.K8sContext.WorkloadName {
			if e.Kubernetes != nil && (e.Kubernetes.Reason == "BackOff" || e.Kubernetes.Reason == "Failed") {
				consequences = append(consequences, e)
			}
		}
	}

	if len(consequences) > 0 {
		return &DimensionMatch{
			Dimension:  "causal",
			Type:       "resource_exhaustion",
			Confidence: 0.9,
			Evidence: []string{
				"Resource exhaustion detected",
				fmt.Sprintf("%d subsequent failures found", len(consequences)),
			},
			Metadata: map[string]interface{}{
				"cause":        event.ID,
				"consequences": getEventIDs(consequences),
			},
		}
	}

	return nil
}

func (d *CausalDimension) findErrorPropagation(event *domain.UnifiedEvent, graph *CorrelationGraph) *DimensionMatch {
	// Check if this is an error event
	if event.GetSeverity() != "error" && event.GetSeverity() != "critical" {
		return nil
	}

	// Find related services that errored after this
	afterEvents := graph.FindAfter(event.Timestamp, d.window)

	var propagated []*domain.UnifiedEvent
	for _, e := range afterEvents {
		// Check if error and related (same namespace or connected service)
		if e.GetSeverity() == "error" || e.GetSeverity() == "critical" {
			if d.areServicesRelated(event, e) {
				propagated = append(propagated, e)
			}
		}
	}

	if len(propagated) > 0 {
		return &DimensionMatch{
			Dimension:  "causal",
			Type:       "error_propagation",
			Confidence: 0.85,
			Evidence: []string{
				fmt.Sprintf("Error propagated to %d services", len(propagated)),
				"Downstream services affected",
			},
			Metadata: map[string]interface{}{
				"source_error": event.ID,
				"propagated":   getEventIDs(propagated),
			},
		}
	}

	return nil
}

func (d *CausalDimension) findDependencyFailure(event *domain.UnifiedEvent, graph *CorrelationGraph) *DimensionMatch {
	// Check network failures to dependencies
	if event.Network == nil || event.Network.StatusCode < 500 {
		return nil
	}

	// Find events from consumers of this service
	if event.K8sContext == nil || len(event.K8sContext.Consumers) == 0 {
		return nil
	}

	afterEvents := graph.FindAfter(event.Timestamp, d.window)

	var affected []*domain.UnifiedEvent
	for _, consumer := range event.K8sContext.Consumers {
		for _, e := range afterEvents {
			if e.K8sContext != nil && e.K8sContext.Name == consumer.Name {
				affected = append(affected, e)
			}
		}
	}

	if len(affected) > 0 {
		return &DimensionMatch{
			Dimension:  "causal",
			Type:       "dependency_failure",
			Confidence: 0.8,
			Evidence: []string{
				"Service failure affected dependent services",
				fmt.Sprintf("%d consumers impacted", len(affected)),
			},
			Metadata: map[string]interface{}{
				"failed_service": fmt.Sprintf("%s/%s", event.K8sContext.Namespace, event.K8sContext.Name),
				"affected":       getEventIDs(affected),
			},
		}
	}

	return nil
}

func (d *CausalDimension) findConfigChangeImpact(event *domain.UnifiedEvent, graph *CorrelationGraph) *DimensionMatch {
	// Check if this is a config change event
	if event.Kubernetes == nil || event.Kubernetes.Action != "MODIFIED" {
		return nil
	}

	isConfigChange := false
	if strings.Contains(event.Kubernetes.ObjectKind, "ConfigMap") ||
		strings.Contains(event.Kubernetes.ObjectKind, "Secret") {
		isConfigChange = true
	}

	if !isConfigChange {
		return nil
	}

	// Find pods that restarted after this
	afterEvents := graph.FindAfter(event.Timestamp, d.window)

	var restarts []*domain.UnifiedEvent
	for _, e := range afterEvents {
		if e.Kubernetes != nil && e.Kubernetes.Reason == "Started" {
			// Check if pod uses this config
			if e.K8sContext != nil {
				for _, dep := range e.K8sContext.Dependencies {
					if dep.Name == event.Kubernetes.Object {
						restarts = append(restarts, e)
					}
				}
			}
		}
	}

	if len(restarts) > 0 {
		return &DimensionMatch{
			Dimension:  "causal",
			Type:       "config_change_impact",
			Confidence: 0.9,
			Evidence: []string{
				"Configuration change triggered restarts",
				fmt.Sprintf("%d pods restarted", len(restarts)),
			},
			Metadata: map[string]interface{}{
				"config_change": event.ID,
				"restarts":      getEventIDs(restarts),
			},
		}
	}

	return nil
}

func (d *CausalDimension) areServicesRelated(e1, e2 *domain.UnifiedEvent) bool {
	if e1.K8sContext == nil || e2.K8sContext == nil {
		return false
	}

	// Same namespace
	if e1.K8sContext.Namespace == e2.K8sContext.Namespace {
		return true
	}

	// Check if one consumes the other
	for _, consumer := range e1.K8sContext.Consumers {
		if consumer.Name == e2.K8sContext.Name {
			return true
		}
	}

	for _, consumer := range e2.K8sContext.Consumers {
		if consumer.Name == e1.K8sContext.Name {
			return true
		}
	}

	return false
}

// SemanticDimension correlates by meaning and intent
type SemanticDimension struct {
	logger *zap.Logger
}

func NewSemanticDimension(logger *zap.Logger) *SemanticDimension {
	return &SemanticDimension{logger: logger}
}

func (d *SemanticDimension) FindCorrelations(event *domain.UnifiedEvent, graph *CorrelationGraph) []DimensionMatch {
	var matches []DimensionMatch

	if event.Semantic == nil || event.Semantic.Intent == "" {
		return matches
	}

	// Find semantically similar events
	similar := graph.FindBySemantic(event.Semantic.Intent)
	if len(similar) > 1 {
		matches = append(matches, DimensionMatch{
			Dimension:  "semantic",
			Type:       "similar_intent",
			Confidence: 0.85,
			Evidence: []string{
				fmt.Sprintf("Same semantic intent: %s", event.Semantic.Intent),
				fmt.Sprintf("%d similar events found", len(similar)),
			},
			Metadata: map[string]interface{}{
				"intent": event.Semantic.Intent,
				"events": getEventIDs(similar),
			},
		})
	}

	// Category clustering
	if event.Semantic.Category != "" {
		categoryEvents := graph.FindByCategory(event.Semantic.Category)
		if len(categoryEvents) > 3 {
			matches = append(matches, DimensionMatch{
				Dimension:  "semantic",
				Type:       "category_cluster",
				Confidence: 0.75,
				Evidence: []string{
					fmt.Sprintf("Events in category: %s", event.Semantic.Category),
					fmt.Sprintf("%d events in this category", len(categoryEvents)),
				},
				Metadata: map[string]interface{}{
					"category": event.Semantic.Category,
					"events":   getEventIDs(categoryEvents),
				},
			})
		}
	}

	// Domain-specific patterns
	if event.Semantic.Domain != "" {
		domainPattern := d.detectDomainPattern(event, graph)
		if domainPattern != nil {
			matches = append(matches, *domainPattern)
		}
	}

	return matches
}

func (d *SemanticDimension) detectDomainPattern(event *domain.UnifiedEvent, graph *CorrelationGraph) *DimensionMatch {
	// Domain-specific patterns (e.g., "authentication", "payment", "inventory")
	domainEvents := graph.FindByDomain(event.Semantic.Domain)

	if len(domainEvents) < 3 {
		return nil
	}

	// Look for common patterns in domain
	patterns := map[string]int{
		"failure_pattern":   0,
		"overload_pattern":  0,
		"security_pattern":  0,
		"performance_issue": 0,
	}

	for _, e := range domainEvents {
		if e.GetSeverity() == "error" || e.GetSeverity() == "critical" {
			patterns["failure_pattern"]++
		}
		if e.Semantic != nil && strings.Contains(e.Semantic.Intent, "overload") {
			patterns["overload_pattern"]++
		}
		if e.Semantic != nil && e.Semantic.Category == "security" {
			patterns["security_pattern"]++
		}
		if e.Network != nil && e.Network.Latency > 100000000 { // 100ms
			patterns["performance_issue"]++
		}
	}

	// Find dominant pattern
	maxPattern := ""
	maxCount := 0
	for pattern, count := range patterns {
		if count > maxCount && count >= 2 {
			maxPattern = pattern
			maxCount = count
		}
	}

	if maxPattern != "" {
		return &DimensionMatch{
			Dimension:  "semantic",
			Type:       maxPattern,
			Confidence: 0.8,
			Evidence: []string{
				fmt.Sprintf("Domain pattern detected in %s", event.Semantic.Domain),
				fmt.Sprintf("%d occurrences of %s", maxCount, maxPattern),
			},
			Metadata: map[string]interface{}{
				"domain":  event.Semantic.Domain,
				"pattern": maxPattern,
				"count":   maxCount,
				"events":  getEventIDs(domainEvents),
			},
		}
	}

	return nil
}

// DependencyDimension correlates by K8s resource dependencies
type DependencyDimension struct {
	logger *zap.Logger
}

func NewDependencyDimension(logger *zap.Logger) *DependencyDimension {
	return &DependencyDimension{logger: logger}
}

func (d *DependencyDimension) FindCorrelations(event *domain.UnifiedEvent, graph *CorrelationGraph) []DimensionMatch {
	var matches []DimensionMatch

	if event.K8sContext == nil {
		return matches
	}

	// ConfigMap/Secret dependency correlation
	if len(event.K8sContext.Dependencies) > 0 {
		for _, dep := range event.K8sContext.Dependencies {
			related := graph.FindByDependency(dep.Kind, dep.Name, dep.Namespace)
			if len(related) > 1 {
				matches = append(matches, DimensionMatch{
					Dimension:  "dependency",
					Type:       "shared_config",
					Confidence: 0.85,
					Evidence: []string{
						fmt.Sprintf("Shared dependency: %s/%s", dep.Kind, dep.Name),
						fmt.Sprintf("%d pods using this config", len(related)),
					},
					Metadata: map[string]interface{}{
						"dependency": fmt.Sprintf("%s/%s", dep.Kind, dep.Name),
						"events":     getEventIDs(related),
					},
				})
			}
		}
	}

	// Service dependency through consumers
	if len(event.K8sContext.Consumers) > 0 {
		// This pod is consumed by services
		var consumerEvents []*domain.UnifiedEvent
		for _, consumer := range event.K8sContext.Consumers {
			events := graph.FindByService(consumer.Name, consumer.Namespace)
			consumerEvents = append(consumerEvents, events...)
		}

		if len(consumerEvents) > 0 {
			matches = append(matches, DimensionMatch{
				Dimension:  "dependency",
				Type:       "service_dependency",
				Confidence: 0.8,
				Evidence: []string{
					"Events from dependent services",
					fmt.Sprintf("%d consumer events found", len(consumerEvents)),
				},
				Metadata: map[string]interface{}{
					"consumers": event.K8sContext.Consumers,
					"events":    getEventIDs(consumerEvents),
				},
			})
		}
	}

	// PVC dependency correlation
	pvcDeps := d.findPVCDependencies(event.K8sContext.Dependencies)
	if len(pvcDeps) > 0 {
		for _, pvc := range pvcDeps {
			related := graph.FindByDependency("PersistentVolumeClaim", pvc.Name, pvc.Namespace)
			if len(related) > 1 {
				matches = append(matches, DimensionMatch{
					Dimension:  "dependency",
					Type:       "shared_storage",
					Confidence: 0.9,
					Evidence: []string{
						fmt.Sprintf("Shared PVC: %s", pvc.Name),
						fmt.Sprintf("%d pods mounting this volume", len(related)),
						"Potential storage contention",
					},
					Metadata: map[string]interface{}{
						"pvc":    pvc.Name,
						"events": getEventIDs(related),
					},
				})
			}
		}
	}

	return matches
}

func (d *DependencyDimension) findPVCDependencies(deps []domain.ResourceDependency) []domain.ResourceDependency {
	var pvcs []domain.ResourceDependency
	for _, dep := range deps {
		if dep.Kind == "PersistentVolumeClaim" {
			pvcs = append(pvcs, dep)
		}
	}
	return pvcs
}

// Helper function to extract event IDs
func getEventIDs(events []*domain.UnifiedEvent) []string {
	ids := make([]string, len(events))
	for i, e := range events {
		ids[i] = e.ID
	}
	return ids
}
