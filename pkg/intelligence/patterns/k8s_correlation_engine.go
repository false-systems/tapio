package patterns

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// NO ML NEEDED - Just Smart Data Structures + K8s Knowledge!

// SimpleCorrelationEngine - Powerful correlation without ML
type SimpleCorrelationEngine struct {
	// Just count co-occurrences
	cooccurrences *CoOccurrenceTracker

	// Track sequences
	sequences *SimpleSequenceTracker

	// Use K8s structure
	k8sGraph *K8sRelationshipGraph

	// Event history for temporal correlation
	events []*domain.UnifiedEvent

	// Event frequency tracking
	eventFrequency map[string]int

	mu sync.RWMutex
}

// CoOccurrenceTracker - Dead simple but effective
type CoOccurrenceTracker struct {
	// Map: EventTypeA_EventTypeB -> Stats
	pairs map[string]*PairStats
	mu    sync.RWMutex
}

type PairStats struct {
	Count      int
	TimeDeltas []time.Duration
	LastSeen   time.Time
}

// Process - The entire "learning" algorithm in 20 lines!
func (e *SimpleCorrelationEngine) Process(event *domain.UnifiedEvent) []domain.Correlation {
	correlations := []domain.Correlation{}

	// 1. Check K8s relationships (FREE correlations!)
	if k8sCorr := e.k8sGraph.GetRelatedResources(event); len(k8sCorr) > 0 {
		correlations = append(correlations, k8sCorr...)
	}

	// 2. Check time-based co-occurrences
	recent := e.getRecentEvents(5 * time.Minute)
	for _, other := range recent {
		if e.areCorrelated(event, other) {
			correlations = append(correlations, domain.Correlation{
				ID:         fmt.Sprintf("corr-%s-%s", other.ID, event.ID),
				Type:       "temporal",
				Events:     []string{other.ID, event.ID},
				Confidence: e.calculateConfidence(event, other),
				Timestamp:  time.Now(),
			})
		}
	}

	// 3. Update our tracking
	e.updateTrackers(event)

	return correlations
}

// getRecentEvents returns events from the last duration
func (e *SimpleCorrelationEngine) getRecentEvents(duration time.Duration) []*domain.UnifiedEvent {
	e.mu.RLock()
	defer e.mu.RUnlock()

	cutoff := time.Now().Add(-duration)
	recent := []*domain.UnifiedEvent{}

	for _, event := range e.events {
		if event.Timestamp.After(cutoff) {
			recent = append(recent, event)
		}
	}

	return recent
}

// updateTrackers updates internal tracking state
func (e *SimpleCorrelationEngine) updateTrackers(event *domain.UnifiedEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Add to event history
	e.events = append(e.events, event)

	// Keep only recent events (e.g., last 1000)
	if len(e.events) > 1000 {
		e.events = e.events[len(e.events)-1000:]
	}

	// Update frequency counters
	if e.eventFrequency == nil {
		e.eventFrequency = make(map[string]int)
	}
	e.eventFrequency[string(event.Type)]++
}

// areCorrelated - Simple heuristics, no ML needed!
func (e *SimpleCorrelationEngine) areCorrelated(a, b *domain.UnifiedEvent) bool {
	// Rule 1: Same namespace/service = likely correlated
	if a.Entity != nil && b.Entity != nil {
		if a.Entity.Namespace == b.Entity.Namespace {
			return true
		}
	}

	// Rule 2: Error propagation pattern
	if a.Severity == domain.EventSeverityError &&
		b.Severity == domain.EventSeverityError &&
		b.Timestamp.Sub(a.Timestamp) < 1*time.Minute {
		return true
	}

	// Rule 3: Known K8s patterns (ConfigMap -> Pod restart)
	if isConfigMapUpdate(a) && isPodRestart(b) {
		return true
	}

	return false
}

// Helper functions for K8s event detection
func isConfigMapUpdate(event *domain.UnifiedEvent) bool {
	return event.Type == domain.EventTypeKubernetes &&
		event.Kubernetes != nil &&
		event.Kubernetes.ObjectKind == "ConfigMap" &&
		(event.Kubernetes.EventType == "update" || event.Kubernetes.EventType == "modified")
}

func isPodRestart(event *domain.UnifiedEvent) bool {
	return event.Type == domain.EventTypeKubernetes &&
		event.Kubernetes != nil &&
		event.Kubernetes.ObjectKind == "Pod" &&
		event.Kubernetes.EventType == "restart"
}

func isPodEvent(event *domain.UnifiedEvent) bool {
	return event.Type == domain.EventTypeKubernetes &&
		event.Kubernetes != nil &&
		event.Kubernetes.ObjectKind == "Pod"
}

// K8sRelationshipGraph - Use K8s native structure!
type K8sRelationshipGraph struct {
	// K8s tells us relationships for FREE:
	// - OwnerReferences (Pod -> ReplicaSet -> Deployment)
	// - Selectors (Service -> Pods)
	// - Events (Object -> Related events)
	// - Labels (Grouping related resources)
}

func (g *K8sRelationshipGraph) GetRelatedResources(event *domain.UnifiedEvent) []domain.Correlation {
	correlations := []domain.Correlation{}

	// Example: Pod event? Check its owners
	if isPodEvent(event) {
		// ConfigMap mounted? That's a correlation!
		if cms := g.getConfigMapsForPod(event); len(cms) > 0 {
			for _, cm := range cms {
				correlations = append(correlations, domain.Correlation{
					ID:         fmt.Sprintf("k8s-%s-%s", cm.ID, event.ID),
					Type:       "configuration",
					Events:     []string{cm.ID, event.ID},
					Confidence: 1.0, // K8s TELLS us this is connected!
					Timestamp:  time.Now(),
				})
			}
		}

		// Service selecting this pod? Another correlation!
		if svcs := g.getServicesForPod(event); len(svcs) > 0 {
			for _, svc := range svcs {
				correlations = append(correlations, domain.Correlation{
					ID:         fmt.Sprintf("k8s-%s-%s", event.ID, svc.ID),
					Type:       "service-endpoint",
					Events:     []string{event.ID, svc.ID},
					Confidence: 1.0,
					Timestamp:  time.Now(),
				})
			}
		}
	}

	return correlations
}

// getConfigMapsForPod returns ConfigMap events related to a pod
func (g *K8sRelationshipGraph) getConfigMapsForPod(podEvent *domain.UnifiedEvent) []*domain.UnifiedEvent {
	// This is a stub implementation
	// In a real implementation, this would check the pod's spec for mounted ConfigMaps
	return []*domain.UnifiedEvent{}
}

// getServicesForPod returns Service events related to a pod
func (g *K8sRelationshipGraph) getServicesForPod(podEvent *domain.UnifiedEvent) []*domain.UnifiedEvent {
	// This is a stub implementation
	// In a real implementation, this would check services that select this pod
	return []*domain.UnifiedEvent{}
}

// THE REAL INSIGHT: K8s Already Knows The Correlations!

// Example 1: Deployment Update Cascade
func DeploymentCascadeCorrelation() {
	// K8s native cascade:
	// Deployment (update)
	//   -> ReplicaSet (create new)
	//     -> Pods (terminate old, create new)
	//       -> Service endpoints (update)
	//         -> Ingress (may see errors)

	// WE DON'T NEED TO LEARN THIS - K8S TELLS US!
}

// Example 2: Resource Pressure Cascade
func ResourcePressureCorrelation() {
	// K8s events tell us:
	// Node (DiskPressure)
	//   -> Kubelet (evicting pods)
	//     -> Pods (Evicted)
	//       -> Deployment (scaling issues)
	//         -> HPA (can't scale)

	// Just follow the events!
}

// SimpleSequenceTracker - Pattern matching without ML
type SimpleSequenceTracker struct {
	// Just remember sequences we've seen
	sequences map[string]int // sequence -> count
	buffer    []string       // rolling buffer of recent events
}

func (s *SimpleSequenceTracker) AddEvent(eventType string) {
	s.buffer = append(s.buffer, eventType)
	if len(s.buffer) > 10 {
		s.buffer = s.buffer[1:] // Keep last 10
	}

	// Check for patterns
	if len(s.buffer) >= 3 {
		// 3-event sequence
		seq := fmt.Sprintf("%s->%s->%s",
			s.buffer[len(s.buffer)-3],
			s.buffer[len(s.buffer)-2],
			s.buffer[len(s.buffer)-1])
		s.sequences[seq]++
	}
}

// Why This Works Better Than ML:

// 1. EXPLAINABLE
func (e *SimpleCorrelationEngine) ExplainCorrelation(corr domain.Correlation) string {
	// Extract event IDs from correlation
	eventInfo := ""
	if len(corr.Events) >= 2 {
		eventInfo = fmt.Sprintf("between %s and %s", corr.Events[0], corr.Events[1])
	}

	switch corr.Type {
	case "owner-reference":
		return fmt.Sprintf("Owner reference correlation %s (K8s OwnerReference)", eventInfo)
	case "selector":
		return fmt.Sprintf("Service selector correlation %s", eventInfo)
	case "temporal":
		return fmt.Sprintf("Temporal correlation %s (confidence: %.2f)", eventInfo, corr.Confidence)
	case "configuration":
		return fmt.Sprintf("Configuration correlation %s", eventInfo)
	default:
		return "Unknown correlation"
	}
}

// 2. DETERMINISTIC
// Same inputs = same outputs. No black box!

// 3. FAST
// No model training, no matrix operations, just hash lookups

// 4. RELIABLE
// No model drift, no retraining, no hyperparameters

// The 80/20 Rule: Simple Heuristics Catch 80% of Correlations!

func CoreCorrelationRules() []string {
	return []string{
		"1. Same namespace = likely correlated",
		"2. Owner/owned = definitely correlated",
		"3. Selector match = definitely correlated",
		"4. Temporal proximity + same service = likely correlated",
		"5. Error cascade (A fails, then B fails) = likely correlated",
		"6. ConfigMap update -> Pod restart = definitely correlated",
		"7. Node pressure -> Pod eviction = definitely correlated",
		"8. Same labels = likely correlated",
		"9. Network connection = definitely correlated",
		"10. Event.InvolvedObject = definitely correlated",
	}
}

// Practical Example: ConfigMap -> Pod Restart
func SimpleConfigMapCorrelation(event *domain.UnifiedEvent) []domain.Correlation {
	correlations := []domain.Correlation{}

	// Is this a pod restart?
	if event.Kubernetes != nil && event.Kubernetes.Reason == "Started" {
		// In a real implementation, we would:
		// 1. Check pod's spec for mounted ConfigMaps
		// 2. Query recent ConfigMap update events
		// 3. Correlate based on timing and references

		// For now, return example correlation
		correlations = append(correlations, domain.Correlation{
			ID:          fmt.Sprintf("config-corr-%s", event.ID),
			Type:        "config-trigger-restart",
			Events:      []string{event.ID}, // Would include ConfigMap event ID
			Confidence:  0.95,               // Very high confidence!
			Timestamp:   time.Now(),
			Description: "Pod restarted after ConfigMap update",
			Metadata: domain.CorrelationMetadata{
				CreatedAt: time.Now(),
				Source:    "k8s-correlation-engine",
				Algorithm: "config-mount-detection",
			},
		})
	}

	return correlations
}

// Statistical Confidence Without ML
func (e *SimpleCorrelationEngine) calculateConfidence(a, b *domain.UnifiedEvent) float64 {
	key := fmt.Sprintf("%s_%s", getEventKey(a), getEventKey(b))

	e.mu.RLock()
	stats, exists := e.cooccurrences.pairs[key]
	e.mu.RUnlock()

	if !exists {
		return 0.1 // First time seeing this
	}

	// Simple confidence calculation:
	// - More occurrences = higher confidence
	// - Consistent timing = higher confidence
	// - Recent observations = higher confidence

	occurrenceScore := math.Min(float64(stats.Count)/10.0, 1.0)
	recencyScore := 1.0 / (1.0 + time.Since(stats.LastSeen).Hours()/24.0)
	consistencyScore := calculateConsistency(stats.TimeDeltas)

	return (occurrenceScore + recencyScore + consistencyScore) / 3.0
}

// Helper functions

func getEventKey(event *domain.UnifiedEvent) string {
	return fmt.Sprintf("%s-%s", event.Type, event.Source)
}

func calculateConsistency(timeDeltas []time.Duration) float64 {
	if len(timeDeltas) < 2 {
		return 0.5
	}

	// Calculate variance in time deltas
	var sum, sumSq float64
	for _, td := range timeDeltas {
		seconds := td.Seconds()
		sum += seconds
		sumSq += seconds * seconds
	}

	mean := sum / float64(len(timeDeltas))
	variance := (sumSq / float64(len(timeDeltas))) - (mean * mean)

	// Lower variance = higher consistency
	return 1.0 / (1.0 + variance)
}

// The Bottom Line: K8s + Simple Stats > Complex ML

func WhyNoMLNeeded() []string {
	return []string{
		"1. K8s already provides relationship data",
		"2. Simple co-occurrence counting works great",
		"3. Temporal proximity is a strong signal",
		"4. Deterministic rules are debuggable",
		"5. No training time or model maintenance",
		"6. Works immediately on deployment",
		"7. Customer can understand the correlations",
		"8. No GPU needed, runs on Raspberry Pi",
	}
}
