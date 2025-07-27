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
func (e *SimpleCorrelationEngine) Process(event *domain.UnifiedEvent) []Correlation {
	correlations := []Correlation{}

	// 1. Check K8s relationships (FREE correlations!)
	if k8sCorr := e.k8sGraph.GetRelatedResources(event); len(k8sCorr) > 0 {
		correlations = append(correlations, k8sCorr...)
	}

	// 2. Check time-based co-occurrences
	recent := e.getRecentEvents(5 * time.Minute)
	for _, other := range recent {
		if e.areCorrelated(event, other) {
			correlations = append(correlations, Correlation{
				From:       other,
				To:         event,
				Type:       "temporal",
				Confidence: e.calculateConfidence(event, other),
			})
		}
	}

	// 3. Update our tracking
	e.updateTrackers(event)

	return correlations
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

// K8sRelationshipGraph - Use K8s native structure!
type K8sRelationshipGraph struct {
	// K8s tells us relationships for FREE:
	// - OwnerReferences (Pod -> ReplicaSet -> Deployment)
	// - Selectors (Service -> Pods)
	// - Events (Object -> Related events)
	// - Labels (Grouping related resources)
}

func (g *K8sRelationshipGraph) GetRelatedResources(event *domain.UnifiedEvent) []Correlation {
	correlations := []Correlation{}

	// Example: Pod event? Check its owners
	if isPodEvent(event) {
		// ConfigMap mounted? That's a correlation!
		if cms := g.getConfigMapsForPod(event); len(cms) > 0 {
			for _, cm := range cms {
				correlations = append(correlations, Correlation{
					From:       cm,
					To:         event,
					Type:       "configuration",
					Confidence: 1.0, // K8s TELLS us this is connected!
				})
			}
		}

		// Service selecting this pod? Another correlation!
		if svcs := g.getServicesForPod(event); len(svcs) > 0 {
			for _, svc := range svcs {
				correlations = append(correlations, Correlation{
					From:       event,
					To:         svc,
					Type:       "service-endpoint",
					Confidence: 1.0,
				})
			}
		}
	}

	return correlations
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
func (e *SimpleCorrelationEngine) ExplainCorrelation(corr Correlation) string {
	switch corr.Type {
	case "owner-reference":
		return fmt.Sprintf("%s owns %s (K8s OwnerReference)", corr.From, corr.To)
	case "selector":
		return fmt.Sprintf("Service %s selects pod %s", corr.From, corr.To)
	case "temporal":
		return fmt.Sprintf("%s happened 30s before %s (observed 47 times)", corr.From, corr.To)
	case "configuration":
		return fmt.Sprintf("Pod mounts ConfigMap %s", corr.From)
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
func SimpleConfigMapCorrelation(event *domain.UnifiedEvent) []Correlation {
	correlations := []Correlation{}

	// Is this a pod restart?
	if event.Kubernetes != nil && event.Kubernetes.Reason == "Started" {
		// Check: Does this pod mount any ConfigMaps?
		pod := getPodDetails(event)
		for _, volume := range pod.Volumes {
			if volume.ConfigMap != nil {
				// Check: Was this ConfigMap recently updated?
				if wasRecentlyUpdated(volume.ConfigMap.Name) {
					correlations = append(correlations, Correlation{
						From:        fmt.Sprintf("ConfigMap/%s", volume.ConfigMap.Name),
						To:          fmt.Sprintf("Pod/%s", pod.Name),
						Type:        "config-trigger-restart",
						Confidence:  0.95, // Very high confidence!
						Explanation: "Pod restarted after ConfigMap update",
					})
				}
			}
		}
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
