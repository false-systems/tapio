package internal
import (
	"math"
	"time"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation/core"
)
// confidenceCalculator implements core.ConfidenceCalculator
type confidenceCalculator struct{}
// NewConfidenceCalculator creates a new confidence calculator
func NewConfidenceCalculator() core.ConfidenceCalculator {
	return &confidenceCalculator{}
}
// ComputeConfidence computes overall confidence for a correlation
func (c *confidenceCalculator) ComputeConfidence(correlation domain.Correlation) float64 {
	factors := c.GetConfidenceFactors(correlation)
	return c.WeightFactors(factors)
}
// ComputeEventConfidence computes confidence for a single event
func (c *confidenceCalculator) ComputeEventConfidence(event domain.Event) float64 {
	baseConfidence := event.Confidence
	// Adjust based on various factors
	factors := []core.ConfidenceFactor{
		{
			Name:        "base_confidence",
			Value:       baseConfidence,
			Weight:      0.4,
			Description: "Original event confidence",
			Source:      "event",
		},
		{
			Name:        "source_reliability",
			Value:       c.getSourceReliability(event.Source),
			Weight:      0.2,
			Description: "Source reliability factor",
			Source:      "source_analysis",
		},
		{
			Name:        "severity_factor",
			Value:       c.getSeverityFactor(event.Severity),
			Weight:      0.15,
			Description: "Event severity factor",
			Source:      "severity_analysis",
		},
		{
			Name:        "context_completeness",
			Value:       c.getContextCompleteness(event),
			Weight:      0.15,
			Description: "Context information completeness",
			Source:      "context_analysis",
		},
		{
			Name:        "temporal_freshness",
			Value:       c.getTemporalFreshness(event.Timestamp),
			Weight:      0.1,
			Description: "Event temporal freshness",
			Source:      "temporal_analysis",
		},
	}
	return c.WeightFactors(factors)
}
// ComputePatternConfidence computes confidence for a pattern match
func (c *confidenceCalculator) ComputePatternConfidence(pattern core.CorrelationPattern, events []domain.Event) float64 {
	if len(events) == 0 {
		return 0.0
	}
	factors := []core.ConfidenceFactor{
		{
			Name:        "pattern_specificity",
			Value:       c.getPatternSpecificity(pattern),
			Weight:      0.25,
			Description: "Pattern specificity and selectivity",
			Source:      "pattern_analysis",
		},
		{
			Name:        "event_quality",
			Value:       c.getAverageEventQuality(events),
			Weight:      0.25,
			Description: "Average quality of matched events",
			Source:      "event_analysis",
		},
		{
			Name:        "temporal_consistency",
			Value:       c.getTemporalConsistency(events, pattern.TimeWindow()),
			Weight:      0.2,
			Description: "Temporal consistency of events",
			Source:      "temporal_analysis",
		},
		{
			Name:        "source_diversity",
			Value:       c.getSourceDiversity(events),
			Weight:      0.15,
			Description: "Diversity of event sources",
			Source:      "source_analysis",
		},
		{
			Name:        "pattern_priority",
			Value:       c.getPriorityFactor(pattern.Priority()),
			Weight:      0.15,
			Description: "Pattern priority weighting",
			Source:      "priority_analysis",
		},
	}
	return c.WeightFactors(factors)
}
// GetConfidenceFactors returns detailed confidence factors for a correlation
func (c *confidenceCalculator) GetConfidenceFactors(correlation domain.Correlation) []core.ConfidenceFactor {
	var factors []core.ConfidenceFactor
	// Base correlation confidence
	factors = append(factors, core.ConfidenceFactor{
		Name:        "base_confidence",
		Value:       correlation.Confidence.Overall,
		Weight:      0.3,
		Description: "Base correlation confidence",
		Source:      "correlation",
	})
	// Event count factor
	eventCountFactor := c.getEventCountFactor(len(correlation.Events))
	factors = append(factors, core.ConfidenceFactor{
		Name:        "event_count",
		Value:       eventCountFactor,
		Weight:      0.2,
		Description: "Number of events in correlation",
		Source:      "event_analysis",
	})
	// Temporal span factor
	temporalSpanFactor := c.getTemporalSpanFactor(correlation)
	factors = append(factors, core.ConfidenceFactor{
		Name:        "temporal_span",
		Value:       temporalSpanFactor,
		Weight:      0.15,
		Description: "Temporal span appropriateness",
		Source:      "temporal_analysis",
	})
	// Correlation type factor
	typeFactor := c.getCorrelationTypeFactor(correlation.Type)
	factors = append(factors, core.ConfidenceFactor{
		Name:        "correlation_type",
		Value:       typeFactor,
		Weight:      0.15,
		Description: "Correlation type reliability",
		Source:      "type_analysis",
	})
	// Finding quality factor
	findingsFactor := c.getFindingsQualityFactor(correlation.Findings)
	factors = append(factors, core.ConfidenceFactor{
		Name:        "findings_quality",
		Value:       findingsFactor,
		Weight:      0.1,
		Description: "Quality of associated findings",
		Source:      "findings_analysis",
	})
	// Metadata completeness factor
	metadataFactor := c.getMetadataCompletenessFactor(correlation.Metadata)
	factors = append(factors, core.ConfidenceFactor{
		Name:        "metadata_completeness",
		Value:       metadataFactor,
		Weight:      0.1,
		Description: "Metadata completeness",
		Source:      "metadata_analysis",
	})
	return factors
}
// WeightFactors combines confidence factors using their weights
func (c *confidenceCalculator) WeightFactors(factors []core.ConfidenceFactor) float64 {
	if len(factors) == 0 {
		return 0.0
	}
	var weightedSum, totalWeight float64
	for _, factor := range factors {
		// Clamp factor value to [0, 1]
		value := math.Max(0, math.Min(1, factor.Value))
		weightedSum += value * factor.Weight
		totalWeight += factor.Weight
	}
	if totalWeight == 0 {
		return 0.0
	}
	result := weightedSum / totalWeight
	// Apply confidence bounds
	return math.Max(0, math.Min(1, result))
}
// Helper methods for computing confidence factors
// getSourceReliability returns reliability score for different sources
func (c *confidenceCalculator) getSourceReliability(source domain.Source) float64 {
	reliability := map[domain.Source]float64{
		domain.SourceEBPF:       0.95, // eBPF is very reliable
		domain.SourceKubernetes: 0.90, // K8s API is quite reliable
		domain.SourceSystemd:    0.85, // systemd is generally reliable
		domain.SourceJournald:   0.80, // Logs can be less structured
	}
	if score, exists := reliability[source]; exists {
		return score
	}
	return 0.70 // Default moderate reliability
}
// getSeverityFactor returns confidence factor based on event severity
func (c *confidenceCalculator) getSeverityFactor(severity domain.Severity) float64 {
	switch severity {
	case domain.SeverityCritical:
		return 1.0
	case domain.SeverityError:
		return 0.9
	case domain.SeverityWarn:
		return 0.8
	case domain.SeverityInfo:
		return 0.7
	case domain.SeverityDebug:
		return 0.6
	default:
		return 0.5
	}
}
// getContextCompleteness calculates completeness of event context
func (c *confidenceCalculator) getContextCompleteness(event domain.Event) float64 {
	score := 0.0
	maxScore := 5.0
	// Check for various context elements
	if event.Context.Host != "" {
		score += 1.0
	}
	if event.Context.Container != "" {
		score += 1.0
	}
	if event.Context.PID != nil {
		score += 1.0
	}
	if len(event.Context.Labels) > 0 {
		score += 1.0
	}
	if len(event.Context.Tags) > 0 {
		score += 1.0
	}
	return score / maxScore
}
// getTemporalFreshness calculates freshness factor based on event age
func (c *confidenceCalculator) getTemporalFreshness(timestamp time.Time) float64 {
	age := time.Since(timestamp)
	// Fresher events have higher confidence
	if age < time.Minute {
		return 1.0
	} else if age < time.Hour {
		return 0.9
	} else if age < 24*time.Hour {
		return 0.8
	} else if age < 7*24*time.Hour {
		return 0.6
	} else {
		return 0.4
	}
}
// getPatternSpecificity calculates pattern specificity factor
func (c *confidenceCalculator) getPatternSpecificity(pattern core.CorrelationPattern) float64 {
	// More specific patterns (requiring specific sources, having strict criteria) get higher scores
	score := 0.5 // Base score
	// Boost for specific source requirements
	if len(pattern.RequiredSources()) > 0 {
		score += 0.2
	}
	// Boost for higher minimum confidence
	if pattern.MinConfidence() > 0.8 {
		score += 0.2
	} else if pattern.MinConfidence() > 0.6 {
		score += 0.1
	}
	// Boost for reasonable time windows (not too broad)
	if pattern.TimeWindow() < 10*time.Minute {
		score += 0.1
	}
	return math.Min(1.0, score)
}
// getAverageEventQuality calculates average quality of events
func (c *confidenceCalculator) getAverageEventQuality(events []domain.Event) float64 {
	if len(events) == 0 {
		return 0.0
	}
	var totalQuality float64
	for _, event := range events {
		quality := c.ComputeEventConfidence(event)
		totalQuality += quality
	}
	return totalQuality / float64(len(events))
}
// getTemporalConsistency calculates temporal consistency of events
func (c *confidenceCalculator) getTemporalConsistency(events []domain.Event, timeWindow time.Duration) float64 {
	if len(events) < 2 {
		return 1.0
	}
	// Find temporal span of events
	var earliest, latest time.Time
	for i, event := range events {
		if i == 0 {
			earliest = event.Timestamp
			latest = event.Timestamp
		} else {
			if event.Timestamp.Before(earliest) {
				earliest = event.Timestamp
			}
			if event.Timestamp.After(latest) {
				latest = event.Timestamp
			}
		}
	}
	span := latest.Sub(earliest)
	// Events within the time window get higher scores
	if span <= timeWindow {
		return 1.0
	} else if span <= 2*timeWindow {
		return 0.8
	} else if span <= 5*timeWindow {
		return 0.6
	} else {
		return 0.4
	}
}
// getSourceDiversity calculates diversity of event sources
func (c *confidenceCalculator) getSourceDiversity(events []domain.Event) float64 {
	if len(events) == 0 {
		return 0.0
	}
	sources := make(map[domain.Source]bool)
	for _, event := range events {
		sources[event.Source] = true
	}
	diversity := float64(len(sources))
	maxDiversity := 4.0 // Maximum expected number of sources
	return math.Min(1.0, diversity/maxDiversity)
}
// getPriorityFactor converts pattern priority to confidence factor
func (c *confidenceCalculator) getPriorityFactor(priority core.PatternPriority) float64 {
	switch priority {
	case core.PatternPriorityCritical:
		return 1.0
	case core.PatternPriorityHigh:
		return 0.9
	case core.PatternPriorityMedium:
		return 0.8
	case core.PatternPriorityLow:
		return 0.7
	default:
		return 0.5
	}
}
// getEventCountFactor calculates factor based on number of events
func (c *confidenceCalculator) getEventCountFactor(count int) float64 {
	// Optimal range is 2-5 events
	if count >= 2 && count <= 5 {
		return 1.0
	} else if count > 5 && count <= 10 {
		return 0.9
	} else if count > 10 {
		return 0.8 // Too many events might be noisy
	} else {
		return 0.5 // Single event correlations are less reliable
	}
}
// getTemporalSpanFactor calculates factor based on temporal span of correlation
func (c *confidenceCalculator) getTemporalSpanFactor(correlation domain.Correlation) float64 {
	// This would require access to the events to calculate span
	// For now, return a default moderate value
	return 0.8
}
// getCorrelationTypeFactor returns reliability factor for correlation types
func (c *confidenceCalculator) getCorrelationTypeFactor(corrType domain.CorrelationType) float64 {
	reliability := map[domain.CorrelationType]float64{
		domain.CorrelationTypeCausal:      0.95, // Causal correlations are most reliable
		domain.CorrelationTypeTemporal:    0.90, // Temporal correlations are quite reliable
		domain.CorrelationTypeResource:    0.85, // Resource correlations are good
		domain.CorrelationTypeNetwork:     0.85, // Network correlations are good
		domain.CorrelationTypeService:     0.80, // Service correlations are decent
		domain.CorrelationTypeSecurity:    0.80, // Security correlations are decent
		domain.CorrelationTypePerformance: 0.75, // Performance correlations can be noisy
		domain.CorrelationTypeCascade:     0.85, // Cascade correlations are quite reliable
		domain.CorrelationTypePredictive:  0.70, // Predictive correlations are less certain
		domain.CorrelationTypeStatistical: 0.70, // Statistical correlations can be spurious
		domain.CorrelationTypeGeneral:     0.60, // General correlations are least specific
	}
	if score, exists := reliability[corrType]; exists {
		return score
	}
	return 0.60 // Default moderate reliability
}
// getFindingsQualityFactor calculates quality factor from findings
func (c *confidenceCalculator) getFindingsQualityFactor(findings []domain.Finding) float64 {
	if len(findings) == 0 {
		return 0.5 // Neutral if no findings
	}
	var totalQuality float64
	for _, finding := range findings {
		// Base quality from finding confidence
		quality := finding.Confidence.Overall
		// Adjust based on finding severity
		severityBoost := c.getSeverityFactor(finding.Severity)
		quality = (quality + severityBoost) / 2.0
		// Adjust based on evidence count
		evidenceBoost := math.Min(1.0, float64(len(finding.Evidence))/3.0)
		quality = (quality + evidenceBoost) / 2.0
		totalQuality += quality
	}
	return totalQuality / float64(len(findings))
}
// getMetadataCompletenessFactor calculates completeness of metadata
func (c *confidenceCalculator) getMetadataCompletenessFactor(metadata domain.CorrelationMetadata) float64 {
	score := 0.0
	maxScore := 4.0
	// Check for metadata completeness
	if metadata.SchemaVersion != "" {
		score += 1.0
	}
	if !metadata.ProcessedAt.IsZero() {
		score += 1.0
	}
	if metadata.ProcessedBy != "" {
		score += 1.0
	}
	if len(metadata.Annotations) > 0 {
		score += 1.0
	}
	return score / maxScore
}