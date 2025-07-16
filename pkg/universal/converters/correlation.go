package converters

import (
	"fmt"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/yairfalse/tapio/pkg/universal"
)

// CorrelationConverter converts correlation engine output to universal format
type CorrelationConverter struct {
	sourceID string
	version  string
}

// NewCorrelationConverter creates a new correlation converter
func NewCorrelationConverter(sourceID, version string) *CorrelationConverter {
	return &CorrelationConverter{
		sourceID: sourceID,
		version:  version,
	}
}

// ConvertFinding converts a correlation finding to universal prediction
func (c *CorrelationConverter) ConvertFinding(finding *correlation.Finding) (*universal.UniversalPrediction, error) {
	if finding == nil {
		return nil, fmt.Errorf("nil finding")
	}

	prediction := universal.GetPrediction()

	// Set core fields
	prediction.ID = fmt.Sprintf("correlation_%s_%d", finding.RuleID, time.Now().UnixNano())
	prediction.Timestamp = finding.CreatedAt

	// Convert resource reference to target
	if finding.Resource.Name != "" {
		resourceRef := &correlation.ResourceReference{
			Kind:      finding.Resource.Type,
			Name:      finding.Resource.Name,
			Namespace: finding.Resource.Namespace,
		}
		prediction.Target = c.convertResourceToTarget(resourceRef)
	}

	// Convert finding type to prediction type
	prediction.Type = c.mapFindingToPredictionType(finding.Title)

	// Set prediction data
	if finding.Prediction != nil {
		prediction.TimeToEvent = finding.Prediction.TimeToEvent
		prediction.Probability = finding.Prediction.Confidence
		prediction.Impact = c.mapSeverityLevelToImpact(finding.Severity)
		prediction.Description = finding.Prediction.Event

		// Add factors
		prediction.Factors = finding.Prediction.Factors

		// Convert mitigations
		for _, mitigation := range finding.Prediction.Mitigation {
			prediction.Mitigations = append(prediction.Mitigations, universal.Mitigation{
				Action:      mitigation,
				Description: mitigation,
				Urgency:     finding.Severity.String(),
				Risk:        "low", // Default risk level
			})
		}
	} else {
		// Create prediction from finding data
		prediction.TimeToEvent = 0 // Immediate issue
		prediction.Probability = finding.Confidence
		prediction.Impact = c.mapSeverityLevelToImpact(finding.Severity)
		prediction.Description = finding.Description
	}

	// Convert evidence
	for _, ev := range finding.Evidence {
		prediction.Evidence = append(prediction.Evidence, c.convertRuleEvidence(ev))
	}

	// Set quality
	prediction.Quality = universal.DataQuality{
		Confidence: finding.Confidence,
		Source:     c.sourceID,
		Version:    c.version,
		Tags: map[string]string{
			"rule_id": finding.RuleID,
			"engine":  "correlation",
		},
		Metadata: finding.Metadata,
	}

	return prediction, nil
}

// ConvertFindings converts multiple findings to predictions
func (c *CorrelationConverter) ConvertFindings(findings []correlation.Finding) ([]*universal.UniversalPrediction, error) {
	predictions := make([]*universal.UniversalPrediction, 0, len(findings))

	for _, finding := range findings {
		prediction, err := c.ConvertFinding(&finding)
		if err != nil {
			// Log error but continue processing
			continue
		}
		predictions = append(predictions, prediction)
	}

	return predictions, nil
}

// ConvertOOMPrediction converts OOM-specific prediction data
func (c *CorrelationConverter) ConvertOOMPrediction(
	target *universal.Target,
	timeToOOM time.Duration,
	confidence float64,
	memoryUsage, memoryLimit uint64,
	growthRate float64,
) (*universal.UniversalPrediction, error) {

	prediction := universal.GetPrediction()

	// Set core fields
	prediction.ID = fmt.Sprintf("oom_prediction_%s_%d", target.Name, time.Now().UnixNano())
	prediction.Timestamp = time.Now()
	prediction.Target = *target

	// Set prediction data
	prediction.Type = universal.PredictionTypeOOM
	prediction.TimeToEvent = timeToOOM
	prediction.Probability = confidence
	prediction.Impact = c.calculateOOMImpact(timeToOOM)
	prediction.Description = fmt.Sprintf("Out of Memory predicted in %v", timeToOOM.Round(time.Minute))

	// Add evidence
	prediction.Evidence = append(prediction.Evidence, universal.Evidence{
		Type:        "memory_usage",
		Description: fmt.Sprintf("Current usage: %.2f%% of limit", float64(memoryUsage)/float64(memoryLimit)*100),
		Data: map[string]interface{}{
			"current_usage": memoryUsage,
			"memory_limit":  memoryLimit,
			"usage_ratio":   float64(memoryUsage) / float64(memoryLimit),
		},
		Confidence: 0.95,
		Source:     "memory_metrics",
	})

	prediction.Evidence = append(prediction.Evidence, universal.Evidence{
		Type:        "growth_rate",
		Description: fmt.Sprintf("Memory growing at %.2f MB/minute", growthRate/1024/1024),
		Data: map[string]interface{}{
			"growth_rate_bytes_per_minute": growthRate,
		},
		Confidence: confidence,
		Source:     "trend_analysis",
	})

	// Add factors
	prediction.Factors = []string{
		fmt.Sprintf("Memory usage at %.1f%%", float64(memoryUsage)/float64(memoryLimit)*100),
		fmt.Sprintf("Growth rate: %.2f MB/min", growthRate/1024/1024),
		"Consistent upward trend detected",
	}

	// Add mitigations
	prediction.Mitigations = c.generateOOMMitigations(target, timeToOOM)

	// Set quality
	prediction.Quality = universal.DataQuality{
		Confidence: confidence,
		Source:     c.sourceID,
		Version:    c.version,
		Tags: map[string]string{
			"prediction": "oom",
			"algorithm":  "linear_projection",
		},
	}

	return prediction, nil
}

// convertResourceToTarget converts correlation resource reference to universal target
func (c *CorrelationConverter) convertResourceToTarget(resource *correlation.ResourceReference) universal.Target {
	target := universal.Target{
		Name:      resource.Name,
		Namespace: resource.Namespace,
	}

	switch strings.ToLower(resource.Kind) {
	case "pod":
		target.Type = universal.TargetTypePod
		target.Pod = resource.Name
	case "deployment", "statefulset", "daemonset":
		target.Type = universal.TargetTypeService
	case "node":
		target.Type = universal.TargetTypeNode
		target.Node = resource.Name
	default:
		target.Type = universal.TargetTypeService
	}

	return target
}

// mapFindingToPredictionType maps finding titles to prediction types
func (c *CorrelationConverter) mapFindingToPredictionType(title string) universal.PredictionType {
	titleLower := strings.ToLower(title)

	switch {
	case strings.Contains(titleLower, "oom") || strings.Contains(titleLower, "memory"):
		return universal.PredictionTypeOOM
	case strings.Contains(titleLower, "crash") || strings.Contains(titleLower, "restart"):
		return universal.PredictionTypeCrash
	case strings.Contains(titleLower, "disk") || strings.Contains(titleLower, "storage"):
		return universal.PredictionTypeDiskFull
	case strings.Contains(titleLower, "performance") || strings.Contains(titleLower, "cpu"):
		return universal.PredictionTypePerformance
	default:
		return universal.PredictionTypeCustom
	}
}

// mapSeverityLevelToImpact maps correlation severity level to impact level
func (c *CorrelationConverter) mapSeverityLevelToImpact(severity correlation.SeverityLevel) universal.ImpactLevel {
	switch severity {
	case correlation.SeverityLevelCritical:
		return universal.ImpactLevelCritical
	case correlation.SeverityLevelError:
		return universal.ImpactLevelHigh
	case correlation.SeverityLevelWarning:
		return universal.ImpactLevelMedium
	case correlation.SeverityLevelInfo:
		return universal.ImpactLevelLow
	default:
		return universal.ImpactLevelMedium
	}
}

// mapSeverityToImpact maps correlation severity to impact level
func (c *CorrelationConverter) mapSeverityToImpact(severity correlation.Severity) universal.ImpactLevel {
	switch severity {
	case correlation.SeverityCritical:
		return universal.ImpactLevelCritical
	case correlation.SeverityHigh:
		return universal.ImpactLevelHigh
	case correlation.SeverityMedium:
		return universal.ImpactLevelMedium
	case correlation.SeverityLow:
		return universal.ImpactLevelLow
	default:
		return universal.ImpactLevelMedium
	}
}

// convertRuleEvidence converts correlation rule evidence to universal evidence
func (c *CorrelationConverter) convertRuleEvidence(ev correlation.RuleEvidence) universal.Evidence {
	return universal.Evidence{
		Type:        ev.Type,
		Description: ev.Description,
		Data:        ev.Data,
		Confidence:  ev.Confidence,
		Source:      string(ev.Source),
	}
}

// convertEvidence converts correlation evidence to universal evidence
func (c *CorrelationConverter) convertEvidence(ev correlation.Evidence) universal.Evidence {
	return universal.Evidence{
		Type:        "generic",
		Description: fmt.Sprintf("%v", ev),
		Data:        map[string]interface{}{"evidence": ev},
		Confidence:  0.5,
		Source:      c.sourceID,
	}
}

// calculateOOMImpact determines impact level based on time to OOM
func (c *CorrelationConverter) calculateOOMImpact(timeToOOM time.Duration) universal.ImpactLevel {
	switch {
	case timeToOOM < 5*time.Minute:
		return universal.ImpactLevelCritical
	case timeToOOM < 15*time.Minute:
		return universal.ImpactLevelHigh
	case timeToOOM < 30*time.Minute:
		return universal.ImpactLevelMedium
	default:
		return universal.ImpactLevelLow
	}
}

// generateOOMMitigations creates mitigation suggestions for OOM predictions
func (c *CorrelationConverter) generateOOMMitigations(target *universal.Target, timeToOOM time.Duration) []universal.Mitigation {
	mitigations := []universal.Mitigation{}

	// Immediate actions for critical situations
	if timeToOOM < 10*time.Minute {
		mitigations = append(mitigations, universal.Mitigation{
			Action:      "Increase memory limit immediately",
			Description: "Prevent OOM by increasing container memory limit",
			Commands: []string{
				fmt.Sprintf("kubectl set resources deployment %s --limits=memory=2Gi", target.Name),
			},
			Urgency: "critical",
			Risk:    "low",
		})

		mitigations = append(mitigations, universal.Mitigation{
			Action:      "Scale horizontally",
			Description: "Distribute load by scaling out",
			Commands: []string{
				fmt.Sprintf("kubectl scale deployment %s --replicas=3", target.Name),
			},
			Urgency: "high",
			Risk:    "medium",
		})
	}

	// General mitigations
	mitigations = append(mitigations, universal.Mitigation{
		Action:      "Investigate memory leak",
		Description: "Check for memory leaks in the application",
		Commands: []string{
			fmt.Sprintf("kubectl exec -it %s -- /bin/sh", target.Pod),
			"# Run memory profiler or heap dump",
		},
		Urgency: "medium",
		Risk:    "low",
	})

	mitigations = append(mitigations, universal.Mitigation{
		Action:      "Review memory requests and limits",
		Description: "Ensure proper resource allocation",
		Commands: []string{
			fmt.Sprintf("kubectl describe pod %s", target.Pod),
			fmt.Sprintf("kubectl top pod %s", target.Pod),
		},
		Urgency: "medium",
		Risk:    "low",
	})

	return mitigations
}

// ConvertCorrelationResult converts generic correlation analysis result
func (c *CorrelationConverter) ConvertCorrelationResult(result map[string]interface{}) (*universal.UniversalDataset, error) {
	dataset := &universal.UniversalDataset{
		ID:        fmt.Sprintf("correlation_%d", time.Now().UnixNano()),
		Version:   c.version,
		Timestamp: time.Now(),
		Source:    c.sourceID,
		Tags:      make(map[string]string),
		Metadata:  result,
	}

	// Extract patterns if available
	if patterns, ok := result["patterns"].(map[string]int); ok {
		for pattern, count := range patterns {
			dataset.Tags[fmt.Sprintf("pattern_%s", pattern)] = fmt.Sprintf("%d", count)
		}
	}

	// Extract insights if available
	if insights, ok := result["insights"].([]string); ok {
		dataset.Metadata["insights"] = insights
	}

	// Set quality based on confidence
	confidence := 0.8 // Default confidence
	if conf, ok := result["confidence"].(float64); ok {
		confidence = conf
	}

	dataset.OverallQuality = universal.DataQuality{
		Confidence: confidence,
		Source:     c.sourceID,
		Version:    c.version,
		Tags: map[string]string{
			"type": "correlation_analysis",
		},
	}

	return dataset, nil
}
