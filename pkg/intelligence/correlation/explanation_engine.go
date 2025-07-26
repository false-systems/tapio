package correlation

import (
	"fmt"
	"strings"
	"time"
)

// ExplanationEngine generates human-readable explanations for correlations
type ExplanationEngine struct {
	// Template store for different explanation types
	templates *ExplanationTemplates
}

// ExplanationTemplates contains templates for different correlation types
type ExplanationTemplates struct {
	K8sNative   map[string]string
	Temporal    map[string]string
	Sequence    map[string]string
	Statistical map[string]string
	Network     map[string]string
}

// CorrelationExplanation contains the explanation details
type CorrelationExplanation struct {
	Summary    string            // One-line summary
	Details    string            // Detailed explanation
	Evidence   []string          // Supporting evidence
	Confidence string            // Confidence explanation
	Actionable []string          // Suggested actions
	Related    []string          // Related correlations
	Metadata   map[string]string // Additional context
}

// NewExplanationEngine creates a new explanation engine
func NewExplanationEngine() *ExplanationEngine {
	return &ExplanationEngine{
		templates: initializeTemplates(),
	}
}

// ExplainK8sCorrelation explains K8s-native correlations
func (e *ExplanationEngine) ExplainK8sCorrelation(correlation K8sCorrelation) CorrelationExplanation {
	template := e.templates.K8sNative[correlation.Type]
	if template == "" {
		template = e.templates.K8sNative["default"]
	}

	summary := e.formatTemplate(template, map[string]string{
		"source":    correlation.Source.Name,
		"target":    correlation.Target.Name,
		"reason":    correlation.Reason,
		"direction": correlation.Direction,
	})

	details := e.buildK8sDetails(correlation)
	evidence := e.buildK8sEvidence(correlation)
	actionable := e.buildK8sActions(correlation)

	return CorrelationExplanation{
		Summary:    summary,
		Details:    details,
		Evidence:   evidence,
		Confidence: "100% - Direct K8s relationship",
		Actionable: actionable,
		Metadata: map[string]string{
			"type":       "k8s-native",
			"subtype":    correlation.Type,
			"confidence": "1.0",
		},
	}
}

// ExplainTemporalCorrelation explains time-based correlations
func (e *ExplanationEngine) ExplainTemporalCorrelation(correlation TemporalCorrelation) CorrelationExplanation {
	template := e.templates.Temporal[correlation.Pattern]
	if template == "" {
		template = e.templates.Temporal["default"]
	}

	summary := e.formatTemplate(template, map[string]string{
		"sourceType":  correlation.SourceEvent.EventType,
		"targetType":  correlation.TargetEvent.EventType,
		"timeDelta":   e.formatDuration(correlation.TimeDelta),
		"occurrences": fmt.Sprintf("%d", correlation.Occurrences),
		"confidence":  fmt.Sprintf("%.0f%%", correlation.Confidence*100),
	})

	details := e.buildTemporalDetails(correlation)
	evidence := e.buildTemporalEvidence(correlation)
	actionable := e.buildTemporalActions(correlation)

	return CorrelationExplanation{
		Summary:    summary,
		Details:    details,
		Evidence:   evidence,
		Confidence: e.formatConfidence(correlation.Confidence),
		Actionable: actionable,
		Metadata: map[string]string{
			"type":        "temporal",
			"pattern":     correlation.Pattern,
			"confidence":  fmt.Sprintf("%.2f", correlation.Confidence),
			"occurrences": fmt.Sprintf("%d", correlation.Occurrences),
		},
	}
}

// ExplainSequenceCorrelation explains sequence-based correlations
func (e *ExplanationEngine) ExplainSequenceCorrelation(correlation SequenceCorrelation) CorrelationExplanation {
	patternStr := strings.Join(correlation.Pattern.Pattern, " → ")

	summary := fmt.Sprintf("Event is part of sequence: %s (observed %d times)",
		patternStr, correlation.Pattern.Occurrences)

	details := e.buildSequenceDetails(correlation)
	evidence := e.buildSequenceEvidence(correlation)
	actionable := e.buildSequenceActions(correlation)

	return CorrelationExplanation{
		Summary:    summary,
		Details:    details,
		Evidence:   evidence,
		Confidence: e.formatConfidence(correlation.Confidence),
		Actionable: actionable,
		Metadata: map[string]string{
			"type":       "sequence",
			"pattern":    patternStr,
			"confidence": fmt.Sprintf("%.2f", correlation.Confidence),
			"duration":   correlation.Duration.String(),
		},
	}
}

// ExplainCorrelationFeatures explains how confidence was calculated
func (e *ExplanationEngine) ExplainCorrelationFeatures(features CorrelationFeatures, score float64) CorrelationExplanation {
	summary := fmt.Sprintf("Confidence score: %.0f%% based on multiple factors", score*100)

	details := e.buildFeatureDetails(features)
	evidence := e.buildFeatureEvidence(features)

	return CorrelationExplanation{
		Summary:    summary,
		Details:    details,
		Evidence:   evidence,
		Confidence: e.formatConfidence(score),
		Metadata: map[string]string{
			"type":  "confidence-explanation",
			"score": fmt.Sprintf("%.2f", score),
		},
	}
}

// Build detailed explanations

func (e *ExplanationEngine) buildK8sDetails(correlation K8sCorrelation) string {
	switch correlation.Type {
	case "ownership":
		return fmt.Sprintf(
			"This correlation is based on Kubernetes OwnerReference. "+
				"Resource %s/%s owns %s/%s, creating a direct dependency relationship. "+
				"When the owner changes, it typically affects the owned resource.",
			correlation.Source.Kind, correlation.Source.Name,
			correlation.Target.Kind, correlation.Target.Name)

	case "selector":
		return fmt.Sprintf(
			"This correlation exists because Service %s has a selector that matches Pod %s. "+
				"The service routes traffic to this pod, so pod health directly affects service availability.",
			correlation.Source.Name, correlation.Target.Name)

	case "label":
		return fmt.Sprintf(
			"These resources share important labels, indicating they're part of the same logical grouping. " +
				"Resources with matching labels often experience similar issues.")

	case "event-sequence":
		return fmt.Sprintf(
			"These events occurred on the same Kubernetes object within a short time window, " +
				"suggesting a causal relationship.")

	case "network":
		return fmt.Sprintf(
			"This correlation is based on network connectivity. "+
				"Resource %s connects to %s, creating a dependency.",
			correlation.Source.Name, correlation.Target.Name)

	default:
		return "This is a Kubernetes-native correlation based on cluster topology."
	}
}

func (e *ExplanationEngine) buildTemporalDetails(correlation TemporalCorrelation) string {
	return fmt.Sprintf(
		"This correlation was discovered by observing that %s consistently %s %s. "+
			"Over %d observations, this pattern occurred with an average time difference of %s. "+
			"The consistency of this timing suggests a causal or reactive relationship.",
		correlation.SourceEvent.EventType,
		correlation.Pattern,
		correlation.TargetEvent.EventType,
		correlation.Occurrences,
		e.formatDuration(correlation.TimeDelta))
}

func (e *ExplanationEngine) buildSequenceDetails(correlation SequenceCorrelation) string {
	pattern := strings.Join(correlation.Pattern.Pattern, " → ")
	return fmt.Sprintf(
		"This event is part of a learned sequence pattern: %s. "+
			"This sequence has been observed %d times with an average duration of %s. "+
			"Understanding this sequence helps predict what might happen next.",
		pattern,
		correlation.Pattern.Occurrences,
		e.formatDuration(correlation.Pattern.AvgDuration))
}

func (e *ExplanationEngine) buildFeatureDetails(features CorrelationFeatures) string {
	details := []string{}

	if features.HasOwnerReference {
		details = append(details, "Strong structural relationship (OwnerReference)")
	}
	if features.HasSelector {
		details = append(details, "Service selector relationship")
	}
	if features.TimeDelta < time.Minute {
		details = append(details, "Events occurred very close in time")
	}
	if features.Occurrences > 10 {
		details = append(details, fmt.Sprintf("Pattern observed %d times", features.Occurrences))
	}
	if features.MessageSimilarity > 0.8 {
		details = append(details, "Event messages are very similar")
	}

	return "Confidence calculated from: " + strings.Join(details, ", ")
}

// Build evidence lists

func (e *ExplanationEngine) buildK8sEvidence(correlation K8sCorrelation) []string {
	evidence := []string{
		fmt.Sprintf("Correlation type: %s", correlation.Type),
		fmt.Sprintf("Source: %s/%s", correlation.Source.Kind, correlation.Source.Name),
		fmt.Sprintf("Target: %s/%s", correlation.Target.Kind, correlation.Target.Name),
	}

	if correlation.Source.Namespace != "" {
		evidence = append(evidence, fmt.Sprintf("Namespace: %s", correlation.Source.Namespace))
	}

	return evidence
}

func (e *ExplanationEngine) buildTemporalEvidence(correlation TemporalCorrelation) []string {
	return []string{
		fmt.Sprintf("Observed %d times", correlation.Occurrences),
		fmt.Sprintf("Average time delta: %s", e.formatDuration(correlation.TimeDelta)),
		fmt.Sprintf("Pattern: %s", correlation.Pattern),
		fmt.Sprintf("Source entity: %s", correlation.SourceEvent.Entity),
		fmt.Sprintf("Target entity: %s", correlation.TargetEvent.Entity),
	}
}

func (e *ExplanationEngine) buildSequenceEvidence(correlation SequenceCorrelation) []string {
	evidence := []string{
		fmt.Sprintf("Sequence pattern: %s", strings.Join(correlation.Pattern.Pattern, " → ")),
		fmt.Sprintf("Observed %d times", correlation.Pattern.Occurrences),
		fmt.Sprintf("Average duration: %s", e.formatDuration(correlation.Pattern.AvgDuration)),
		fmt.Sprintf("Sequence duration: %s", e.formatDuration(correlation.Duration)),
	}

	// Add entities involved
	entities := make(map[string]int)
	for _, event := range correlation.Events {
		entities[event.Entity]++
	}

	if len(entities) > 1 {
		entityList := make([]string, 0, len(entities))
		for entity := range entities {
			entityList = append(entityList, entity)
		}
		evidence = append(evidence, fmt.Sprintf("Entities involved: %s", strings.Join(entityList, ", ")))
	}

	return evidence
}

func (e *ExplanationEngine) buildFeatureEvidence(features CorrelationFeatures) []string {
	evidence := []string{}

	if features.HasOwnerReference {
		evidence = append(evidence, "Has OwnerReference relationship")
	}
	if features.HasSelector {
		evidence = append(evidence, "Has selector relationship")
	}
	if features.HasLabelMatch {
		evidence = append(evidence, "Shares important labels")
	}
	if features.CoOccurrenceRate > 0.5 {
		evidence = append(evidence, fmt.Sprintf("Co-occurrence rate: %.0f%%", features.CoOccurrenceRate*100))
	}
	if features.ConditionalProb > 0.7 {
		evidence = append(evidence, fmt.Sprintf("Conditional probability: %.0f%%", features.ConditionalProb*100))
	}

	return evidence
}

// Build actionable suggestions

func (e *ExplanationEngine) buildK8sActions(correlation K8sCorrelation) []string {
	switch correlation.Type {
	case "ownership":
		return []string{
			"Check the owner resource for configuration issues",
			"Verify owner resource is in healthy state",
			"Review recent changes to the owner resource",
		}
	case "selector":
		return []string{
			"Verify service selector matches pod labels",
			"Check service endpoint configuration",
			"Test network connectivity to the pod",
		}
	case "network":
		return []string{
			"Check network policies affecting connectivity",
			"Verify DNS resolution",
			"Test port connectivity",
		}
	default:
		return []string{
			"Investigate both resources for related issues",
			"Check recent changes to either resource",
		}
	}
}

func (e *ExplanationEngine) buildTemporalActions(correlation TemporalCorrelation) []string {
	actions := []string{
		"Monitor for this pattern in future events",
	}

	if correlation.Pattern == "precedes" {
		actions = append(actions, "Use source event as early warning for target event")
	} else if correlation.Pattern == "follows" {
		actions = append(actions, "Investigate root cause in the preceding event")
	}

	if correlation.Confidence > 0.8 {
		actions = append(actions, "Set up alerting for this correlation pattern")
	}

	return actions
}

func (e *ExplanationEngine) buildSequenceActions(correlation SequenceCorrelation) []string {
	actions := []string{
		"Monitor for completion of this sequence",
		"Identify if sequence indicates normal operation or problem escalation",
	}

	if len(correlation.Events) > 1 {
		actions = append(actions, "Review sequence timing for performance optimization")
	}

	return actions
}

// Helper methods

func (e *ExplanationEngine) formatTemplate(template string, vars map[string]string) string {
	result := template
	for key, value := range vars {
		placeholder := "{" + key + "}"
		result = strings.ReplaceAll(result, placeholder, value)
	}
	return result
}

func (e *ExplanationEngine) formatDuration(d time.Duration) string {
	if d < time.Second {
		return "< 1 second"
	} else if d < time.Minute {
		return fmt.Sprintf("%.0f seconds", d.Seconds())
	} else if d < time.Hour {
		return fmt.Sprintf("%.1f minutes", d.Minutes())
	} else {
		return fmt.Sprintf("%.1f hours", d.Hours())
	}
}

func (e *ExplanationEngine) formatConfidence(confidence float64) string {
	percentage := confidence * 100

	if percentage >= 95 {
		return fmt.Sprintf("Very High (%.0f%%) - Strong evidence", percentage)
	} else if percentage >= 80 {
		return fmt.Sprintf("High (%.0f%%) - Good evidence", percentage)
	} else if percentage >= 60 {
		return fmt.Sprintf("Medium (%.0f%%) - Some evidence", percentage)
	} else if percentage >= 40 {
		return fmt.Sprintf("Low (%.0f%%) - Weak evidence", percentage)
	} else {
		return fmt.Sprintf("Very Low (%.0f%%) - Speculative", percentage)
	}
}

// Initialize explanation templates
func initializeTemplates() *ExplanationTemplates {
	return &ExplanationTemplates{
		K8sNative: map[string]string{
			"ownership":      "{source} owns {target} - direct dependency relationship",
			"selector":       "Service {source} selects Pod {target} for traffic routing",
			"label":          "{source} and {target} share important labels",
			"event-sequence": "Events on same K8s object: {reason}",
			"network":        "{source} connects to {target} over network",
			"default":        "K8s relationship between {source} and {target}",
		},
		Temporal: map[string]string{
			"precedes":   "{sourceType} consistently happens before {targetType} by ~{timeDelta}",
			"follows":    "{targetType} typically follows {sourceType} by ~{timeDelta}",
			"concurrent": "{sourceType} and {targetType} occur simultaneously",
			"default":    "{sourceType} and {targetType} are temporally correlated ({confidence} confidence)",
		},
		Sequence: map[string]string{
			"start":   "Event starts sequence pattern",
			"middle":  "Event continues sequence pattern",
			"end":     "Event completes sequence pattern",
			"default": "Event is part of learned sequence",
		},
		Statistical: map[string]string{
			"cooccurrence": "Events co-occur more often than chance",
			"conditional":  "One event predicts the other",
			"default":      "Statistical correlation detected",
		},
		Network: map[string]string{
			"connectivity": "Network connection established",
			"traffic":      "Network traffic pattern",
			"default":      "Network-based correlation",
		},
	}
}
