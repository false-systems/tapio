package context

import (
	"math"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// ConfidenceScorer calculates confidence scores for UnifiedEvents
type ConfidenceScorer struct {
	weights       map[string]float64
	knownSources  map[string]bool
	maxEventAge   time.Duration
	minFieldRatio float64
}

// NewConfidenceScorer creates a new confidence scorer with default weights
func NewConfidenceScorer() *ConfidenceScorer {
	return &ConfidenceScorer{
		weights: map[string]float64{
			"complete_data":      0.3,
			"trace_context":      0.2,
			"entity_context":     0.2,
			"timestamp_accuracy": 0.15,
			"known_source":       0.15,
		},
		knownSources: map[string]bool{
			"ebpf":        true,
			"k8s":         true,
			"kubernetes":  true,
			"cni":         true,
			"systemd":     true,
			"application": true,
			"system":      true,
		},
		maxEventAge:   5 * time.Minute,
		minFieldRatio: 0.7, // Event needs 70% of expected fields to be "complete"
	}
}

// NewConfidenceScorerWithConfig creates a scorer with custom configuration
func NewConfidenceScorerWithConfig(weights map[string]float64, knownSources []string) *ConfidenceScorer {
	// Validate and normalize weights
	totalWeight := 0.0
	for _, weight := range weights {
		totalWeight += weight
	}

	// Normalize weights to sum to 1.0
	if totalWeight > 0 {
		for key, weight := range weights {
			weights[key] = weight / totalWeight
		}
	}

	// Convert known sources slice to map
	sourcesMap := make(map[string]bool, len(knownSources))
	for _, source := range knownSources {
		sourcesMap[strings.ToLower(source)] = true
	}

	return &ConfidenceScorer{
		weights:       weights,
		knownSources:  sourcesMap,
		maxEventAge:   5 * time.Minute,
		minFieldRatio: 0.7,
	}
}

// CalculateConfidence calculates the confidence score for a UnifiedEvent
func (cs *ConfidenceScorer) CalculateConfidence(ue *domain.UnifiedEvent) float64 {
	if ue == nil {
		return 0.0
	}

	confidence := 0.0

	// Data completeness
	if cs.hasCompleteData(ue) {
		confidence += cs.weights["complete_data"]
	}

	// Trace context presence
	if cs.hasTraceContext(ue) {
		confidence += cs.weights["trace_context"]
	}

	// Entity context quality
	if cs.hasEntityContext(ue) {
		confidence += cs.weights["entity_context"]
	}

	// Timestamp accuracy
	if cs.hasAccurateTimestamp(ue) {
		confidence += cs.weights["timestamp_accuracy"]
	}

	// Known source
	if cs.isKnownSource(ue.Source) {
		confidence += cs.weights["known_source"]
	}

	// Ensure confidence is between 0.0 and 1.0
	return math.Min(math.Max(confidence, 0.0), 1.0)
}

// hasCompleteData checks if the event has sufficient data for its type
func (cs *ConfidenceScorer) hasCompleteData(ue *domain.UnifiedEvent) bool {
	// Base requirements
	if ue.ID == "" || ue.Timestamp.IsZero() || ue.Type == "" || ue.Source == "" {
		return false
	}

	// Type-specific completeness checks
	switch ue.Type {
	case domain.EventTypeSystem, domain.EventTypeCPU, domain.EventTypeDisk:
		return cs.hasCompleteKernelData(ue)
	case domain.EventTypeMemory:
		return cs.hasCompleteMemoryData(ue)
	case domain.EventTypeNetwork:
		return cs.hasCompleteNetworkData(ue)
	case domain.EventTypeLog:
		return cs.hasCompleteApplicationData(ue)
	case domain.EventTypeKubernetes:
		return cs.hasCompleteKubernetesData(ue)
	case domain.EventTypeProcess:
		return cs.hasCompleteProcessData(ue)
	case domain.EventTypeService:
		// Service events are more flexible
		return ue.Entity != nil || ue.Kubernetes != nil
	default:
		// Unknown types get partial credit if they have any layer data
		return ue.Kernel != nil || ue.Network != nil || ue.Application != nil || ue.Kubernetes != nil
	}
}

// hasCompleteKernelData checks kernel data completeness
func (cs *ConfidenceScorer) hasCompleteKernelData(ue *domain.UnifiedEvent) bool {
	if ue.Kernel == nil {
		return false
	}

	// Count filled fields
	filledFields := 0
	totalFields := 6

	if ue.Kernel.Syscall != "" {
		filledFields++
	}
	if ue.Kernel.PID > 0 {
		filledFields++
	}
	if ue.Kernel.TID > 0 {
		filledFields++
	}
	if ue.Kernel.Comm != "" {
		filledFields++
	}
	if ue.Kernel.UID > 0 {
		filledFields++
	}
	if ue.Kernel.GID > 0 {
		filledFields++
	}

	return float64(filledFields)/float64(totalFields) >= cs.minFieldRatio
}

// hasCompleteMemoryData checks memory event data completeness
func (cs *ConfidenceScorer) hasCompleteMemoryData(ue *domain.UnifiedEvent) bool {
	// Memory events can have kernel OR application data
	if ue.Kernel != nil {
		return cs.hasCompleteKernelData(ue)
	}
	if ue.Application != nil {
		return cs.hasCompleteApplicationData(ue)
	}
	return false
}

// hasCompleteNetworkData checks network data completeness
func (cs *ConfidenceScorer) hasCompleteNetworkData(ue *domain.UnifiedEvent) bool {
	if ue.Network == nil {
		return false
	}

	// Essential network fields
	if ue.Network.Protocol == "" {
		return false
	}

	// For TCP/UDP, we expect more complete data
	if ue.Network.Protocol == "TCP" || ue.Network.Protocol == "UDP" {
		hasSource := ue.Network.SourceIP != "" || ue.Network.SourcePort > 0
		hasDest := ue.Network.DestIP != "" || ue.Network.DestPort > 0
		return hasSource && hasDest
	}

	// For other protocols, having protocol is sufficient
	return true
}

// hasCompleteApplicationData checks application data completeness
func (cs *ConfidenceScorer) hasCompleteApplicationData(ue *domain.UnifiedEvent) bool {
	if ue.Application == nil {
		return false
	}

	// Required fields for application events
	hasLevel := ue.Application.Level != ""
	hasMessage := ue.Application.Message != ""
	hasLogger := ue.Application.Logger != ""

	// At least 2 out of 3 required fields
	filledCount := 0
	if hasLevel {
		filledCount++
	}
	if hasMessage {
		filledCount++
	}
	if hasLogger {
		filledCount++
	}

	return filledCount >= 2
}

// hasCompleteKubernetesData checks Kubernetes data completeness
func (cs *ConfidenceScorer) hasCompleteKubernetesData(ue *domain.UnifiedEvent) bool {
	if ue.Kubernetes == nil {
		return false
	}

	// Essential Kubernetes fields
	hasEventType := ue.Kubernetes.EventType != ""
	hasObject := ue.Kubernetes.Object != ""
	hasReason := ue.Kubernetes.Reason != ""

	// At least 2 out of 3 required
	filledCount := 0
	if hasEventType {
		filledCount++
	}
	if hasObject {
		filledCount++
	}
	if hasReason {
		filledCount++
	}

	return filledCount >= 2
}

// hasCompleteProcessData checks process data completeness
func (cs *ConfidenceScorer) hasCompleteProcessData(ue *domain.UnifiedEvent) bool {
	if ue.Kernel == nil {
		return false
	}

	// Process events need PID and either comm or syscall
	hasPID := ue.Kernel.PID > 0
	hasComm := ue.Kernel.Comm != ""
	hasSyscall := ue.Kernel.Syscall != ""

	return hasPID && (hasComm || hasSyscall)
}

// hasTraceContext checks if the event has distributed tracing context
func (cs *ConfidenceScorer) hasTraceContext(ue *domain.UnifiedEvent) bool {
	return ue.TraceContext != nil && ue.TraceContext.TraceID != "" && ue.TraceContext.SpanID != ""
}

// hasEntityContext checks if the event has entity context
func (cs *ConfidenceScorer) hasEntityContext(ue *domain.UnifiedEvent) bool {
	if ue.Entity == nil {
		return false
	}

	// Strong entity context has type, name, and either namespace or UID
	hasType := ue.Entity.Type != ""
	hasName := ue.Entity.Name != ""
	hasNamespace := ue.Entity.Namespace != ""
	hasUID := ue.Entity.UID != ""

	return hasType && hasName && (hasNamespace || hasUID)
}

// hasAccurateTimestamp checks if the timestamp is recent and reasonable
func (cs *ConfidenceScorer) hasAccurateTimestamp(ue *domain.UnifiedEvent) bool {
	age := time.Since(ue.Timestamp)

	// Check for future timestamps (clock skew)
	// Allow up to 1 minute in the future
	if age < -1*time.Minute {
		return false
	}

	// Check if event is recent (allow slight negative age for clock skew)
	return age >= -1*time.Minute && age <= cs.maxEventAge
}

// isKnownSource checks if the event source is recognized
func (cs *ConfidenceScorer) isKnownSource(source string) bool {
	if source == "" {
		return false
	}

	// Normalize source to lowercase for comparison
	normalizedSource := strings.ToLower(strings.TrimSpace(source))

	// Check exact match
	if cs.knownSources[normalizedSource] {
		return true
	}

	// Check for common prefixes/suffixes
	for known := range cs.knownSources {
		if strings.HasPrefix(normalizedSource, known) || strings.HasSuffix(normalizedSource, known) {
			return true
		}
		// Also check if known source is contained in the source string
		if strings.Contains(normalizedSource, known) {
			return true
		}
	}

	return false
}

// GetWeights returns the current weight configuration
func (cs *ConfidenceScorer) GetWeights() map[string]float64 {
	// Return a copy to prevent external modification
	weights := make(map[string]float64, len(cs.weights))
	for k, v := range cs.weights {
		weights[k] = v
	}
	return weights
}

// SetWeight updates a specific weight value
func (cs *ConfidenceScorer) SetWeight(key string, value float64) error {
	if _, exists := cs.weights[key]; !exists {
		return nil // Silently ignore unknown weights
	}

	// Ensure value is between 0 and 1
	cs.weights[key] = math.Min(math.Max(value, 0.0), 1.0)

	// Re-normalize weights to sum to 1.0
	totalWeight := 0.0
	for _, weight := range cs.weights {
		totalWeight += weight
	}

	if totalWeight > 0 {
		for key, weight := range cs.weights {
			cs.weights[key] = weight / totalWeight
		}
	}

	return nil
}

// AddKnownSource adds a new source to the known sources list
func (cs *ConfidenceScorer) AddKnownSource(source string) {
	cs.knownSources[strings.ToLower(strings.TrimSpace(source))] = true
}

// RemoveKnownSource removes a source from the known sources list
func (cs *ConfidenceScorer) RemoveKnownSource(source string) {
	delete(cs.knownSources, strings.ToLower(strings.TrimSpace(source)))
}

// GetKnownSources returns the list of known sources
func (cs *ConfidenceScorer) GetKnownSources() []string {
	sources := make([]string, 0, len(cs.knownSources))
	for source := range cs.knownSources {
		sources = append(sources, source)
	}
	return sources
}
