package aggregator

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/integrations/telemetry"
	"go.uber.org/zap"
)

// CorrelationAggregator combines outputs from multiple correlators into a single answer
type CorrelationAggregator struct {
	logger             *zap.Logger
	config             AggregatorConfig
	rules              []AggregationRule
	confidenceCalc     *ConfidenceCalculator
	conflictResolver   *ConflictResolver
	causalityBuilder   *CausalityBuilder
	patternMatcher     *PatternMatcher
	synthesisEngine    *SynthesisEngine
	correlatorAccuracy map[string]float64 // Track accuracy per correlator
	instrumentation    *telemetry.AggregatorInstrumentation
}

// NewCorrelationAggregator creates a new aggregator
func NewCorrelationAggregator(logger *zap.Logger, config AggregatorConfig) *CorrelationAggregator {
	// Create instrumentation
	instrumentation, err := telemetry.NewAggregatorInstrumentation(logger)
	if err != nil {
		logger.Error("Failed to create instrumentation", zap.Error(err))
		// Continue without instrumentation
		instrumentation = nil
	}

	agg := &CorrelationAggregator{
		logger:             logger,
		config:             config,
		rules:              []AggregationRule{},
		confidenceCalc:     NewConfidenceCalculator(),
		conflictResolver:   NewConflictResolver(config.ConflictResolution),
		causalityBuilder:   NewCausalityBuilder(logger),
		patternMatcher:     NewPatternMatcher(),
		synthesisEngine:    NewSynthesisEngine(logger),
		correlatorAccuracy: make(map[string]float64),
		instrumentation:    instrumentation,
	}

	// Initialize default rules
	agg.initializeDefaultRules()

	return agg
}

// Aggregate combines multiple correlator outputs into a final result
func (a *CorrelationAggregator) Aggregate(ctx context.Context, outputs []*CorrelatorOutput, event *domain.UnifiedEvent) (*FinalResult, error) {
	start := time.Now()

	// Validate inputs
	if len(outputs) == 0 {
		return nil, fmt.Errorf("no correlator outputs to aggregate")
	}

	a.logger.Info("Starting aggregation",
		zap.Int("correlator_count", len(outputs)),
		zap.String("event_id", event.ID))

	// Record correlator outputs metric
	if a.instrumentation != nil {
		a.instrumentation.CorrelatorOutputs.Record(ctx, int64(len(outputs)))
	}

	// Step 1: Check data sufficiency
	if !a.hasMinimumData(outputs) {
		a.logger.Warn("Insufficient data for full analysis")
		return a.degradedAnalysis(outputs, event), nil
	}

	// Step 2: Extract all findings
	allFindings := a.extractFindings(outputs)
	a.logger.Debug("Extracted findings", zap.Int("count", len(allFindings)))

	// Step 3: Resolve conflicts
	conflictsBefore := len(allFindings)
	resolvedFindings := a.conflictResolver.Resolve(allFindings)
	conflictsResolved := conflictsBefore - len(resolvedFindings)
	a.logger.Debug("Resolved conflicts", zap.Int("remaining", len(resolvedFindings)))

	if a.instrumentation != nil && conflictsResolved > 0 {
		a.instrumentation.ConflictsResolved.Add(ctx, int64(conflictsResolved))
		// Track conflict type separately
		if a.instrumentation.ConflictTypes != nil {
			a.instrumentation.ConflictTypes.Add(ctx, 1)
		}
	}

	// Step 4: Apply aggregation rules
	for _, rule := range a.rules {
		if rule.Condition(outputs) {
			if result := rule.Aggregate(outputs); result != nil {
				result.ProcessingTime = time.Since(start)
				result.Timestamp = time.Now()
				// Add contributing correlators
				for _, output := range outputs {
					if len(output.Findings) > 0 {
						result.Correlators = append(result.Correlators, output.CorrelatorName)
					}
				}
				a.logger.Info("Rule matched",
					zap.String("rule", rule.Name),
					zap.Float64("confidence", result.Confidence))
				return result, nil
			}
		}
	}

	// Step 5: Build causality chain
	causalChain := a.causalityBuilder.BuildChain(resolvedFindings)

	// Step 6: Match patterns
	patterns := a.patternMatcher.FindMatches(causalChain)

	if a.instrumentation != nil && len(patterns) > 0 {
		a.instrumentation.PatternMatches.Add(ctx, int64(len(patterns)))
	}

	// Step 7: Apply synthesis rules to generate higher-level insights
	syntheses := a.synthesisEngine.ApplySynthesis(ctx, resolvedFindings)
	a.logger.Debug("Applied synthesis rules", zap.Int("synthesis_count", len(syntheses)))

	// Step 8: Calculate final confidence (now includes synthesis boost)
	confidence := a.confidenceCalc.Calculate(resolvedFindings, patterns, outputs)

	// Apply synthesis confidence boost
	for _, synthesis := range syntheses {
		if synthesis.Confidence > confidence {
			confidence = synthesis.Confidence
		}
	}

	// Step 9: Build final result
	result := a.buildFinalResult(resolvedFindings, causalChain, patterns, confidence, outputs)

	// Enhance result with synthesis insights
	if len(syntheses) > 0 {
		result.Syntheses = syntheses
		// Use the highest priority synthesis for the summary
		bestSynthesis := syntheses[0]
		for _, synthesis := range syntheses {
			if synthesis.Confidence > bestSynthesis.Confidence {
				bestSynthesis = synthesis
			}
		}
		if bestSynthesis.Insight != "" {
			result.Summary = bestSynthesis.Insight
		}
	}
	result.ProcessingTime = time.Since(start)

	// Record final metrics
	if a.instrumentation != nil {
		a.instrumentation.CorrelationsAggregated.Add(ctx, 1)
		a.instrumentation.ConfidenceScores.Record(ctx, confidence)
		a.instrumentation.AggregationDuration.Record(ctx, result.ProcessingTime.Seconds())

		// Calculate and record agreement score
		agreementScore := a.calculateAgreementScore(outputs)
		a.instrumentation.AgreementScore.Record(ctx, agreementScore)
	}

	a.logger.Info("Aggregation complete",
		zap.String("root_cause", result.RootCause),
		zap.Float64("confidence", result.Confidence),
		zap.Duration("duration", result.ProcessingTime))

	return result, nil
}

// hasMinimumData checks if we have enough data for analysis
func (a *CorrelationAggregator) hasMinimumData(outputs []*CorrelatorOutput) bool {
	// Need at least one correlator with findings
	for _, output := range outputs {
		if len(output.Findings) > 0 {
			return true
		}
	}
	return false
}

// degradedAnalysis provides best-effort analysis with limited data
func (a *CorrelationAggregator) degradedAnalysis(outputs []*CorrelatorOutput, event *domain.UnifiedEvent) *FinalResult {
	result := &FinalResult{
		ID:         fmt.Sprintf("degraded-%s", event.ID),
		Summary:    "Limited analysis due to insufficient data",
		Confidence: 0.3,
		Timestamp:  time.Now(),
	}

	// Find any available findings
	for _, output := range outputs {
		if len(output.Findings) > 0 {
			// Use the highest confidence finding
			bestFinding := output.Findings[0]
			for _, finding := range output.Findings {
				if finding.Confidence > bestFinding.Confidence {
					bestFinding = finding
				}
			}
			result.RootCause = bestFinding.Message
			result.Confidence = bestFinding.Confidence * 0.6 // Reduce confidence
			result.Correlators = []string{output.CorrelatorName}
			break
		}
	}

	if result.RootCause == "" {
		result.RootCause = "Unable to determine root cause with available data"
	}

	return result
}

// extractFindings gets all findings from outputs
func (a *CorrelationAggregator) extractFindings(outputs []*CorrelatorOutput) []Finding {
	var findings []Finding
	for _, output := range outputs {
		for _, finding := range output.Findings {
			// Add correlator name to finding for tracking
			finding.Evidence.Traces = append(finding.Evidence.Traces, TraceSpan{
				Operation: output.CorrelatorName,
			})
			findings = append(findings, finding)
		}
	}
	return findings
}

// buildFinalResult constructs the final aggregated result
func (a *CorrelationAggregator) buildFinalResult(
	findings []Finding,
	causalChain []CausalLink,
	patterns []Pattern,
	confidence float64,
	outputs []*CorrelatorOutput,
) *FinalResult {
	// Sort findings by confidence
	sort.Slice(findings, func(i, j int) bool {
		return findings[i].Confidence > findings[j].Confidence
	})

	// Build timeline
	timeline := a.buildTimeline(findings)

	// Determine root cause
	rootCause := a.determineRootCause(findings, causalChain)

	// Build impact summary
	impact := a.buildImpactSummary(findings)

	// Generate remediation
	remediation := a.generateRemediation(findings, rootCause)

	// Collect all correlators that contributed
	correlators := make([]string, 0, len(outputs))
	for _, output := range outputs {
		if len(output.Findings) > 0 {
			correlators = append(correlators, output.CorrelatorName)
		}
	}

	// Collect all evidence
	evidence := make(map[string]Evidence)
	for _, finding := range findings {
		evidence[finding.ID] = finding.Evidence
	}

	return &FinalResult{
		ID:          fmt.Sprintf("corr-%d", time.Now().Unix()),
		Summary:     a.buildSummary(rootCause, impact),
		RootCause:   rootCause,
		Impact:      impact,
		Remediation: remediation,
		Confidence:  confidence,
		CausalChain: causalChain,
		Timeline:    timeline,
		Evidence:    evidence,
		Correlators: correlators,
		Timestamp:   time.Now(),
	}
}

// buildTimeline creates a timeline of events
func (a *CorrelationAggregator) buildTimeline(findings []Finding) []TimelineEvent {
	events := make([]TimelineEvent, 0, len(findings))

	for _, finding := range findings {
		events = append(events, TimelineEvent{
			Time:     finding.Timestamp,
			Event:    finding.Message,
			Source:   finding.Type,
			Severity: finding.Severity,
			Related:  finding.Impact.Resources,
		})
	}

	// Sort by time
	sort.Slice(events, func(i, j int) bool {
		return events[i].Time.Before(events[j].Time)
	})

	return events
}

// determineRootCause identifies the root cause from findings and causal chain
func (a *CorrelationAggregator) determineRootCause(findings []Finding, chain []CausalLink) string {
	// If we have a causal chain, use the first link
	if len(chain) > 0 {
		return chain[0].From
	}

	// Otherwise, use the highest confidence critical/high finding
	for _, finding := range findings {
		if finding.Severity == SeverityCritical || finding.Severity == SeverityHigh {
			return finding.Message
		}
	}

	// Fallback to highest confidence finding
	if len(findings) > 0 {
		return findings[0].Message
	}

	return "Unknown root cause"
}

// buildImpactSummary creates an impact summary
func (a *CorrelationAggregator) buildImpactSummary(findings []Finding) string {
	impacts := make(map[string]bool)

	for _, finding := range findings {
		if finding.Impact.UserImpact != "" {
			impacts[finding.Impact.UserImpact] = true
		}
	}

	if len(impacts) == 0 {
		return "System degradation detected"
	}

	summary := ""
	for impact := range impacts {
		if summary != "" {
			summary += ", "
		}
		summary += impact
	}

	return summary
}

// generateRemediation creates remediation steps
func (a *CorrelationAggregator) generateRemediation(findings []Finding, rootCause string) Remediation {
	remediation := Remediation{
		Automatic:     false,
		Steps:         []string{},
		Commands:      []string{},
		Preventive:    []string{},
		EstimatedTime: 5 * time.Minute,
	}

	// Collect unique remediation steps from findings
	steps := make(map[string]bool)
	commands := make(map[string]bool)

	for _, finding := range findings {
		// Add finding-specific remediation if available
		if finding.Type == "config_change" {
			steps["Review recent configuration changes"] = true
			commands["kubectl rollout undo deployment/..."] = true
		} else if finding.Type == "resource_exhaustion" {
			steps["Increase resource limits"] = true
			commands["kubectl edit deployment/..."] = true
		}
	}

	// Convert to slices
	for step := range steps {
		remediation.Steps = append(remediation.Steps, step)
	}
	for cmd := range commands {
		remediation.Commands = append(remediation.Commands, cmd)
	}

	// Add generic preventive measures
	remediation.Preventive = []string{
		"Implement resource monitoring alerts",
		"Add configuration validation",
		"Enable gradual rollout strategies",
	}

	return remediation
}

// buildSummary creates a one-line summary
func (a *CorrelationAggregator) buildSummary(rootCause, impact string) string {
	return fmt.Sprintf("%s causing %s", rootCause, impact)
}

// initializeDefaultRules sets up default aggregation rules
func (a *CorrelationAggregator) initializeDefaultRules() {
	a.rules = []AggregationRule{
		{
			Name:        "ConfigCascade",
			Priority:    100,
			Description: "Configuration change causing cascading failures",
			Condition: func(outputs []*CorrelatorOutput) bool {
				hasConfig := false
				hasCascade := false
				for _, output := range outputs {
					for _, finding := range output.Findings {
						if finding.Type == "config_change" {
							hasConfig = true
						}
						if finding.Type == "cascade_failure" {
							hasCascade = true
						}
					}
				}
				return hasConfig && hasCascade
			},
			Aggregate: func(outputs []*CorrelatorOutput) *FinalResult {
				return &FinalResult{
					ID:         fmt.Sprintf("config-cascade-%d", time.Now().Unix()),
					Summary:    "Configuration change triggered cascading failures",
					RootCause:  "Invalid configuration propagated through system",
					Impact:     "Multiple services experiencing failures",
					Confidence: 0.9,
					Remediation: Remediation{
						Automatic: false,
						Steps: []string{
							"Identify the configuration change",
							"Rollback to previous configuration",
							"Verify services are recovering",
						},
						EstimatedTime: 10 * time.Minute,
					},
				}
			},
		},
		{
			Name:        "ResourceExhaustion",
			Priority:    90,
			Description: "Resource exhaustion causing failures",
			Condition: func(outputs []*CorrelatorOutput) bool {
				for _, output := range outputs {
					for _, finding := range output.Findings {
						if finding.Type == "memory_exhaustion" || finding.Type == "cpu_exhaustion" {
							return true
						}
					}
				}
				return false
			},
			Aggregate: func(outputs []*CorrelatorOutput) *FinalResult {
				// Determine specific resource type
				resourceType := "resource"
				for _, output := range outputs {
					for _, finding := range output.Findings {
						if finding.Type == "memory_exhaustion" {
							resourceType = "memory"
							break
						} else if finding.Type == "cpu_exhaustion" {
							resourceType = "CPU"
							break
						}
					}
				}

				return &FinalResult{
					ID:         fmt.Sprintf("resource-exhaustion-%d", time.Now().Unix()),
					Summary:    fmt.Sprintf("%s exhaustion detected", resourceType),
					RootCause:  fmt.Sprintf("Container exceeding %s limits", resourceType),
					Impact:     "Service unavailable due to resource constraints",
					Confidence: 0.85,
					Remediation: Remediation{
						Automatic: true,
						Steps: []string{
							"Increase memory/CPU limits",
							"Investigate resource leak",
							"Implement horizontal scaling",
						},
						EstimatedTime: 15 * time.Minute,
					},
				}
			},
		},
	}
}

// SetRules allows setting custom aggregation rules
func (a *CorrelationAggregator) SetRules(rules []AggregationRule) {
	// Sort by priority
	sort.Slice(rules, func(i, j int) bool {
		return rules[i].Priority > rules[j].Priority
	})
	a.rules = rules
}

// AddRule adds a new aggregation rule
func (a *CorrelationAggregator) AddRule(rule AggregationRule) {
	a.rules = append(a.rules, rule)
	// Re-sort by priority
	sort.Slice(a.rules, func(i, j int) bool {
		return a.rules[i].Priority > a.rules[j].Priority
	})
}

// UpdateCorrelatorAccuracy updates accuracy tracking based on feedback
func (a *CorrelationAggregator) UpdateCorrelatorAccuracy(correlatorName string, correct bool) {
	current, exists := a.correlatorAccuracy[correlatorName]
	if !exists {
		current = 1.0
	}

	if correct {
		a.correlatorAccuracy[correlatorName] = current * 1.05 // 5% boost
		if a.correlatorAccuracy[correlatorName] > 1.5 {
			a.correlatorAccuracy[correlatorName] = 1.5 // Cap at 1.5x
		}
	} else {
		a.correlatorAccuracy[correlatorName] = current * 0.95 // 5% penalty
		if a.correlatorAccuracy[correlatorName] < 0.5 {
			a.correlatorAccuracy[correlatorName] = 0.5 // Floor at 0.5x
		}
	}

	a.logger.Debug("Updated correlator accuracy",
		zap.String("correlator", correlatorName),
		zap.Float64("accuracy", a.correlatorAccuracy[correlatorName]))
}

// calculateAgreementScore calculates how much correlators agree (0-1)
func (a *CorrelationAggregator) calculateAgreementScore(outputs []*CorrelatorOutput) float64 {
	if len(outputs) <= 1 {
		return 1.0 // Single correlator always agrees with itself
	}

	// Count finding types across all correlators
	typeCount := make(map[string]int)
	totalFindings := 0

	for _, output := range outputs {
		for _, finding := range output.Findings {
			typeCount[finding.Type]++
			totalFindings++
		}
	}

	if totalFindings == 0 {
		return 0.0
	}

	// Calculate agreement as ratio of shared findings
	agreementCount := 0
	for _, count := range typeCount {
		if count > 1 {
			// This finding type appears in multiple correlators
			agreementCount += count - 1
		}
	}

	return float64(agreementCount) / float64(totalFindings)
}
