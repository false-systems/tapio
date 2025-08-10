package aggregator

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
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
	storage            CorrelationStorage // Storage backend for correlations
	correlators        []CorrelatorInfo   // Available correlators and their health
	graphStore         GraphStore         // Graph database for complex queries

	// OTEL instrumentation
	tracer                   trace.Tracer
	correlatorOutputsCounter metric.Int64Counter
	conflictsResolvedCounter metric.Int64Counter
	patternsFoundCounter     metric.Int64Counter
	aggregationDuration      metric.Float64Histogram
	agreementScoreGauge      metric.Float64Gauge
	queryDurationHistogram   metric.Float64Histogram
	feedbackCounter          metric.Int64Counter
}

// NewCorrelationAggregator creates a new aggregator
func NewCorrelationAggregator(logger *zap.Logger, config AggregatorConfig) *CorrelationAggregator {
	// Initialize OTEL instrumentation directly (Level 2 can use OTEL cross-cutting concern)
	tracer := otel.Tracer("correlation-aggregator")
	meter := otel.Meter("correlation-aggregator")

	// Create metrics
	correlatorOutputsCounter, err := meter.Int64Counter(
		"aggregator_correlator_outputs_total",
		metric.WithDescription("Total number of correlator outputs processed"),
	)
	if err != nil {
		logger.Warn("Failed to create correlator outputs counter", zap.Error(err))
	}

	conflictsResolvedCounter, err := meter.Int64Counter(
		"aggregator_conflicts_resolved_total",
		metric.WithDescription("Total number of conflicts resolved"),
	)
	if err != nil {
		logger.Warn("Failed to create conflicts resolved counter", zap.Error(err))
	}

	patternsFoundCounter, err := meter.Int64Counter(
		"aggregator_patterns_found_total",
		metric.WithDescription("Total number of patterns found"),
	)
	if err != nil {
		logger.Warn("Failed to create patterns found counter", zap.Error(err))
	}

	aggregationDuration, err := meter.Float64Histogram(
		"aggregator_aggregation_duration_ms",
		metric.WithDescription("Aggregation processing duration in milliseconds"),
	)
	if err != nil {
		logger.Warn("Failed to create aggregation duration histogram", zap.Error(err))
	}

	agreementScoreGauge, err := meter.Float64Gauge(
		"aggregator_agreement_score",
		metric.WithDescription("Agreement score between correlators (0-1)"),
	)
	if err != nil {
		logger.Warn("Failed to create agreement score gauge", zap.Error(err))
	}

	queryDurationHistogram, err := meter.Float64Histogram(
		"aggregator_query_duration_ms",
		metric.WithDescription("Query processing duration in milliseconds"),
	)
	if err != nil {
		logger.Warn("Failed to create query duration histogram", zap.Error(err))
	}

	feedbackCounter, err := meter.Int64Counter(
		"aggregator_feedback_total",
		metric.WithDescription("Total feedback received"),
	)
	if err != nil {
		logger.Warn("Failed to create feedback counter", zap.Error(err))
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
		correlators:        []CorrelatorInfo{},

		// OTEL instrumentation
		tracer:                   tracer,
		correlatorOutputsCounter: correlatorOutputsCounter,
		conflictsResolvedCounter: conflictsResolvedCounter,
		patternsFoundCounter:     patternsFoundCounter,
		aggregationDuration:      aggregationDuration,
		agreementScoreGauge:      agreementScoreGauge,
		queryDurationHistogram:   queryDurationHistogram,
		feedbackCounter:          feedbackCounter,
	}

	// Initialize default rules
	agg.initializeDefaultRules()

	return agg
}

// NewCorrelationAggregatorWithStorage creates a new aggregator with storage backend
func NewCorrelationAggregatorWithStorage(logger *zap.Logger, config AggregatorConfig, storage CorrelationStorage, graphStore GraphStore) *CorrelationAggregator {
	agg := NewCorrelationAggregator(logger, config)
	agg.storage = storage
	agg.graphStore = graphStore
	return agg
}

// Aggregate combines multiple correlator outputs into a final result
func (a *CorrelationAggregator) Aggregate(ctx context.Context, outputs []*CorrelatorOutput, event *domain.UnifiedEvent) (*FinalResult, error) {
	ctx, span := a.tracer.Start(ctx, "aggregator.aggregate")
	defer span.End()

	start := time.Now()
	defer func() {
		// Record aggregation duration
		duration := time.Since(start).Seconds() * 1000 // Convert to milliseconds
		if a.aggregationDuration != nil {
			a.aggregationDuration.Record(ctx, duration, metric.WithAttributes(
				attribute.Int("correlator_count", len(outputs)),
			))
		}
	}()

	// Validate inputs
	if len(outputs) == 0 {
		span.SetAttributes(attribute.String("error", "no_correlator_outputs"))
		return nil, fmt.Errorf("no correlator outputs to aggregate")
	}

	span.SetAttributes(
		attribute.Int("correlator_count", len(outputs)),
		attribute.String("event_id", event.ID),
	)

	a.logger.Info("Starting aggregation",
		zap.Int("correlator_count", len(outputs)),
		zap.String("event_id", event.ID))

	// Record correlator outputs metric
	if a.correlatorOutputsCounter != nil {
		a.correlatorOutputsCounter.Add(ctx, int64(len(outputs)))
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

	// Record conflicts resolved metric
	if a.conflictsResolvedCounter != nil && conflictsResolved > 0 {
		a.conflictsResolvedCounter.Add(ctx, int64(conflictsResolved))
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

	// Record patterns found metric
	if a.patternsFoundCounter != nil && len(patterns) > 0 {
		a.patternsFoundCounter.Add(ctx, int64(len(patterns)))
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

	// Record final metrics - These are already handled elsewhere in the function

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

// validateQuery validates correlation query parameters
func (a *CorrelationAggregator) validateQuery(query CorrelationQuery) error {
	if query.ResourceType == "" {
		return fmt.Errorf("resource type is required")
	}
	if query.Name == "" {
		return fmt.Errorf("resource name is required")
	}
	// Namespace can be empty for cluster-scoped resources
	return nil
}

// isMoreRelevant determines if result1 is more relevant than result2
func (a *CorrelationAggregator) isMoreRelevant(result1, result2 *StoredCorrelation) bool {
	// Higher confidence is more relevant
	if result1.Confidence > result2.Confidence+0.1 {
		return true
	}
	if result2.Confidence > result1.Confidence+0.1 {
		return false
	}

	// If confidence is similar, prefer more recent
	return result1.Timestamp.After(result2.Timestamp)
}

// convertToAggregatedResult converts a stored correlation to an aggregated result
func (a *CorrelationAggregator) convertToAggregatedResult(stored *StoredCorrelation) *AggregatedResult {
	if stored == nil || stored.Result == nil {
		return nil
	}

	result := &AggregatedResult{
		ID: stored.ID,
		Resource: ResourceRef{
			Type:      stored.ResourceType,
			Namespace: stored.Namespace,
			Name:      stored.Name,
		},
		Confidence:  stored.Confidence,
		CreatedAt:   stored.Timestamp,
		Correlators: stored.Correlators,
	}

	// Convert root cause
	if stored.Result.RootCause != "" {
		result.RootCause = &RootCause{
			Type:        "detected",
			Description: stored.Result.RootCause,
			Confidence:  stored.Result.Confidence,
		}
	}

	// Convert impact
	if stored.Result.Impact != "" {
		result.Impact = &ImpactAnalysis{
			Scope:      "service",
			Severity:   stored.Severity,
			UserImpact: stored.Result.Impact,
		}
	}

	// Convert remediation
	if stored.Result.Remediation.Steps != nil && len(stored.Result.Remediation.Steps) > 0 {
		steps := []RemediationStep{}
		for i, step := range stored.Result.Remediation.Steps {
			steps = append(steps, RemediationStep{
				Order:       i + 1,
				Description: step,
				Manual:      !stored.Result.Remediation.Automatic,
				RiskLevel:   "medium",
			})
		}

		result.Remediation = &RemediationPlan{
			Automatic:     stored.Result.Remediation.Automatic,
			Steps:         steps,
			EstimatedTime: stored.Result.Remediation.EstimatedTime,
			RiskLevel:     "medium",
		}
	}

	// Copy timeline and causal chain
	result.Timeline = stored.Result.Timeline
	result.CausalChain = stored.Result.CausalChain
	result.Evidence = stored.Result.Evidence

	return result
}

// performGraphAnalysis performs real-time graph-based analysis when no stored correlations exist
func (a *CorrelationAggregator) performGraphAnalysis(ctx context.Context, query CorrelationQuery) (*AggregatedResult, error) {
	a.logger.Debug("Performing graph analysis",
		zap.String("resource", query.Name))

	// Build Cypher query to find related issues
	cypherQuery := `
		MATCH (r:Resource {type: $type, namespace: $namespace, name: $name})
		OPTIONAL MATCH (r)-[rel:DEPENDS_ON|OWNED_BY|AFFECTS*1..3]-(related)
		OPTIONAL MATCH (e:Event)-[:AFFECTS]->(r)
		WHERE e.timestamp > datetime() - duration('PT1H')
		RETURN r, collect(DISTINCT related) as related, collect(DISTINCT e) as events
		LIMIT 100
	`

	params := map[string]interface{}{
		"type":      query.ResourceType,
		"namespace": query.Namespace,
		"name":      query.Name,
	}

	graphResults, err := a.graphStore.ExecuteQuery(ctx, cypherQuery, params)
	if err != nil {
		return nil, fmt.Errorf("graph query failed: %w", err)
	}

	// Analyze results to determine root cause
	// Graph results processing will be enhanced in future iterations
	resultCount := 0
	if graphResults != nil {
		// Try to determine result count safely
		if resultsSlice, ok := graphResults.([]interface{}); ok {
			resultCount = len(resultsSlice)
		} else if resultsMap, ok := graphResults.(map[string]interface{}); ok {
			resultCount = len(resultsMap)
		}
	}
	result := &AggregatedResult{
		ID: fmt.Sprintf("graph-%d-%d", time.Now().Unix(), resultCount),
		Resource: ResourceRef{
			Type:      query.ResourceType,
			Namespace: query.Namespace,
			Name:      query.Name,
		},
		ProcessingTime: time.Since(time.Now()),
		CreatedAt:      time.Now(),
		Correlators:    []string{"GraphAnalyzer"},
	}

	// Process graph results to extract insights
	// This is a simplified version - real implementation would be more sophisticated
	result.RootCause = &RootCause{
		Type:        "graph_analysis",
		Description: "Analysis based on graph relationships",
		Confidence:  0.7,
	}

	result.Confidence = 0.7

	return result, nil
}

// recordLearningEvent records a learning event when feedback indicates incorrect analysis
func (a *CorrelationAggregator) recordLearningEvent(ctx context.Context, result *StoredCorrelation, feedback CorrelationFeedback) {
	a.logger.Info("Recording learning event",
		zap.String("correlation_id", result.ID),
		zap.String("comment", feedback.Comment),
		zap.Bool("correct_rc", feedback.CorrectRC))

	// In a real implementation, this would:
	// 1. Store the learning event in a dedicated learning store
	// 2. Trigger retraining or rule adjustment
	// 3. Update correlator weights
	// 4. Potentially notify administrators

	// For now, just log and update accuracy
	for _, correlatorName := range result.Correlators {
		if accuracy, exists := a.correlatorAccuracy[correlatorName]; exists {
			// Reduce accuracy more significantly for incorrect root cause
			newAccuracy := accuracy * 0.9
			if newAccuracy < 0.3 {
				newAccuracy = 0.3 // Floor at 30%
			}
			a.correlatorAccuracy[correlatorName] = newAccuracy
		}
	}
}

// QueryCorrelations queries for correlations based on resource criteria
func (a *CorrelationAggregator) QueryCorrelations(ctx context.Context, query CorrelationQuery) (*AggregatedResult, error) {
	start := time.Now()

	// Validate query parameters
	if err := a.validateQuery(query); err != nil {
		return nil, fmt.Errorf("invalid query: %w", err)
	}

	a.logger.Debug("Querying correlations",
		zap.String("resource_type", query.ResourceType),
		zap.String("namespace", query.Namespace),
		zap.String("name", query.Name))

	// If no storage configured, return error
	if a.storage == nil {
		return nil, fmt.Errorf("no storage backend configured")
	}

	// Query stored correlations for the resource
	results, err := a.storage.GetByResource(ctx, query.ResourceType, query.Namespace, query.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to query storage: %w", err)
	}

	if len(results) == 0 {
		// If no stored correlations, try graph-based analysis if available
		if a.graphStore != nil {
			return a.performGraphAnalysis(ctx, query)
		}
		return nil, ErrNotFound
	}

	// Find the most relevant correlation based on confidence and recency
	var bestResult *StoredCorrelation
	for _, result := range results {
		if bestResult == nil || a.isMoreRelevant(result, bestResult) {
			bestResult = result
		}
	}

	// Convert to AggregatedResult
	aggResult := a.convertToAggregatedResult(bestResult)
	aggResult.ProcessingTime = time.Since(start)

	// Record metrics

	return aggResult, nil
}

// ListCorrelations returns a paginated list of correlations
func (a *CorrelationAggregator) ListCorrelations(ctx context.Context, limit, offset int) (*CorrelationList, error) {
	// Validate pagination parameters
	if limit <= 0 {
		limit = 10 // Default limit
	}
	if limit > 100 {
		limit = 100 // Max limit
	}
	if offset < 0 {
		offset = 0
	}

	a.logger.Debug("Listing correlations",
		zap.Int("limit", limit),
		zap.Int("offset", offset))

	if a.storage == nil {
		return nil, fmt.Errorf("no storage backend configured")
	}

	// Get recent correlations from storage
	allResults, err := a.storage.GetRecent(ctx, limit+offset+100) // Get extra for counting total
	if err != nil {
		return nil, fmt.Errorf("failed to list correlations: %w", err)
	}

	// Apply pagination
	summaries := []CorrelationSummary{}
	total := len(allResults)

	// Calculate the actual slice boundaries
	startIdx := offset
	endIdx := offset + limit
	if startIdx > total {
		startIdx = total
	}
	if endIdx > total {
		endIdx = total
	}

	// Convert to summaries for the requested page
	for i := startIdx; i < endIdx && i < len(allResults); i++ {
		result := allResults[i]
		summary := CorrelationSummary{
			ID: result.ID,
			Resource: ResourceRef{
				Type:      result.ResourceType,
				Namespace: result.Namespace,
				Name:      result.Name,
			},
			RootCause: result.RootCause,
			Severity:  result.Severity,
			CreatedAt: result.Timestamp,
		}
		summaries = append(summaries, summary)
	}

	return &CorrelationList{
		Correlations: summaries,
		Total:        total,
		Limit:        limit,
		Offset:       offset,
	}, nil
}

// GetCorrelation retrieves a specific correlation by ID
func (a *CorrelationAggregator) GetCorrelation(ctx context.Context, id string) (*AggregatedResult, error) {
	if id == "" {
		return nil, fmt.Errorf("correlation ID is required")
	}

	a.logger.Debug("Getting correlation", zap.String("id", id))

	if a.storage == nil {
		return nil, fmt.Errorf("no storage backend configured")
	}

	// Try to get from storage
	result, err := a.storage.GetByID(ctx, id)
	if err != nil {
		if err == ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get correlation: %w", err)
	}

	// Convert to AggregatedResult
	aggResult := a.convertToAggregatedResult(result)

	// Record metrics

	return aggResult, nil
}

// SubmitFeedback submits user feedback for a correlation
func (a *CorrelationAggregator) SubmitFeedback(ctx context.Context, id string, feedback CorrelationFeedback) error {
	if id == "" {
		return fmt.Errorf("correlation ID is required")
	}

	a.logger.Info("Processing feedback",
		zap.String("correlation_id", id),
		zap.Bool("useful", feedback.Useful),
		zap.Bool("correct_rc", feedback.CorrectRC))

	// Record feedback metric
	if a.feedbackCounter != nil {
		a.feedbackCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.Bool("useful", feedback.Useful),
			attribute.Bool("correct_root_cause", feedback.CorrectRC),
		))
	}

	// Get the correlation to find contributing correlators
	if a.storage != nil {
		result, err := a.storage.GetByID(ctx, id)
		if err != nil {
			return fmt.Errorf("failed to get correlation for feedback: %w", err)
		}

		// Update accuracy for each contributing correlator
		for _, correlatorName := range result.Correlators {
			a.UpdateCorrelatorAccuracy(correlatorName, feedback.CorrectRC)
		}

		// Store feedback with the correlation
		if err := a.storage.StoreFeedback(ctx, id, feedback); err != nil {
			a.logger.Error("Failed to store feedback",
				zap.String("correlation_id", id),
				zap.Error(err))
			// Don't fail the operation if storage fails
		}

		// If feedback indicates incorrect root cause, create a learning event
		if !feedback.CorrectRC && a.config.EnableLearning {
			a.recordLearningEvent(ctx, result, feedback)
		}
	}

	// Record feedback metrics

	return nil
}

// Health checks if the aggregator is healthy
func (a *CorrelationAggregator) Health(ctx context.Context) error {
	var errors []error

	// Check storage health
	if a.storage != nil {
		if err := a.storage.HealthCheck(ctx); err != nil {
			errors = append(errors, fmt.Errorf("storage unhealthy: %w", err))
		}
	}

	// Check graph store health
	if a.graphStore != nil {
		if err := a.graphStore.HealthCheck(ctx); err != nil {
			errors = append(errors, fmt.Errorf("graph store unhealthy: %w", err))
		}
	}

	// Check correlator health
	for _, correlator := range a.correlators {
		if correlator.HealthCheck != nil {
			if err := correlator.HealthCheck(ctx); err != nil {
				errors = append(errors, fmt.Errorf("correlator %s unhealthy: %w", correlator.Name, err))
			}
		}
	}

	// OTEL instrumentation is healthy by design (global providers)

	if len(errors) > 0 {
		return fmt.Errorf("health check failed: %v", errors)
	}

	a.logger.Debug("Health check passed")
	return nil
}
