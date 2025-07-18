package simple

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf"
)

// SimpleEnhancedExplainer provides basic enhanced explanations
type SimpleEnhancedExplainer struct {
	ebpfMonitor ebpf.Monitor
}

// NewSimpleEnhancedExplainer creates a new simple enhanced explainer
func NewSimpleEnhancedExplainer(ebpfMonitor ebpf.Monitor) *SimpleEnhancedExplainer {
	return &SimpleEnhancedExplainer{
		ebpfMonitor: ebpfMonitor,
	}
}

// ExplainResource provides enhanced explanation for a specific resource
func (e *SimpleEnhancedExplainer) ExplainResource(ctx context.Context, resource *types.ResourceRef, problems []types.Problem) (*types.Explanation, error) {
	startTime := time.Now()

	// Basic analysis with pattern detection
	explanation := &types.Explanation{
		Resource:   resource,
		Problems:   problems,
		Summary:    e.generateSummary(resource, problems),
		Analysis:   e.generateAnalysis(resource, problems),
		RootCauses: e.generateRootCauses(problems),
		Solutions:  e.generateSolutions(resource, problems),
		Timestamp:  time.Now(),
	}

	// Add eBPF insights if available
	if e.ebpfMonitor != nil && e.ebpfMonitor.IsAvailable() {
		e.addEBPFInsights(ctx, resource, explanation)
	}

	// Add prediction if patterns suggest issues
	if prediction := e.generatePrediction(problems); prediction != nil {
		explanation.Prediction = prediction
	}

	// Add learning information
	explanation.Learning = e.generateLearning(problems)

	// Ensure analysis completes quickly
	if time.Since(startTime) > 3*time.Second {
		fmt.Printf("[WARN] Analysis took %v, exceeding 3s target\n", time.Since(startTime))
	}

	return explanation, nil
}

// AnalyzeProblems provides simple correlation analysis
func (e *SimpleEnhancedExplainer) AnalyzeProblems(ctx context.Context, problems []types.Problem) (interface{}, error) {
	if len(problems) == 0 {
		return map[string]interface{}{
			"analysis_type": "enhanced_simple",
			"patterns":      map[string]int{},
			"insights":      []string{},
		}, nil
	}

	// Simple pattern analysis
	patterns := make(map[string]int)
	namespaces := make(map[string]int)

	for _, problem := range problems {
		// Pattern detection
		if strings.Contains(strings.ToLower(problem.Title), "memory") {
			patterns["memory"]++
		}
		if strings.Contains(strings.ToLower(problem.Title), "restart") {
			patterns["restart"]++
		}
		if strings.Contains(strings.ToLower(problem.Title), "network") {
			patterns["network"]++
		}
		if strings.Contains(strings.ToLower(problem.Title), "cpu") {
			patterns["cpu"]++
		}

		namespaces[problem.Resource.Namespace]++
	}

	// Generate insights
	insights := []string{}
	if patterns["memory"] >= 2 {
		insights = append(insights, "Memory pressure pattern detected across multiple resources")
	}
	if patterns["restart"] >= 2 {
		insights = append(insights, "Restart pattern indicates potential instability")
	}
	if patterns["network"] >= 2 {
		insights = append(insights, "Network-related issues affecting multiple resources")
	}
	if len(namespaces) == 1 && len(problems) >= 3 {
		insights = append(insights, "Issues concentrated in single namespace")
	}
	if len(patterns) >= 3 {
		insights = append(insights, "Multiple issue types suggest systemic problems")
	}

	// Add eBPF insights if available
	if e.ebpfMonitor != nil && e.ebpfMonitor.IsAvailable() {
		insights = append(insights, "eBPF monitoring available for deep analysis")
	}

	return map[string]interface{}{
		"analysis_type":          "enhanced_simple",
		"patterns":               patterns,
		"namespace_distribution": namespaces,
		"insights":               insights,
		"confidence":             e.calculateConfidence(patterns, len(problems)),
		"total_problems":         len(problems),
	}, nil
}

// generateSummary creates an intelligent summary based on problems
func (e *SimpleEnhancedExplainer) generateSummary(resource *types.ResourceRef, problems []types.Problem) string {
	if len(problems) == 0 {
		return fmt.Sprintf("Resource %s/%s appears healthy", resource.Kind, resource.Name)
	}

	// Find most critical problem
	var criticalProblem *types.Problem
	for _, problem := range problems {
		if problem.Severity == types.SeverityCritical {
			criticalProblem = &problem
			break
		}
	}

	if criticalProblem != nil {
		return fmt.Sprintf("Critical issue: %s - %s", criticalProblem.Title, criticalProblem.Description)
	}

	return fmt.Sprintf("Found %d issues affecting %s/%s", len(problems), resource.Kind, resource.Name)
}

// generateAnalysis creates enhanced analysis
func (e *SimpleEnhancedExplainer) generateAnalysis(resource *types.ResourceRef, problems []types.Problem) *types.Analysis {
	analysis := &types.Analysis{
		KubernetesView: &types.KubernetesView{
			Status:     "Analyzing",
			Phase:      "Enhanced Analysis",
			Conditions: []string{"Pattern detection active"},
			Resources:  map[string]string{"analysis": "enhanced"},
			Events:     []string{"Enhanced analysis in progress"},
		},
		RealityCheck: &types.RealityCheck{
			ActualMemory:   "Gathering enhanced data...",
			RestartPattern: "Analyzing patterns...",
			ErrorPatterns:  []string{},
			NetworkIssues:  []string{},
		},
	}

	// Analyze problems for reality check
	for _, problem := range problems {
		if strings.Contains(strings.ToLower(problem.Title), "memory") {
			analysis.RealityCheck.ActualMemory = "Memory issues detected"
		}
		if strings.Contains(strings.ToLower(problem.Title), "restart") {
			analysis.RealityCheck.RestartPattern = "Restart patterns detected"
		}
		if strings.Contains(strings.ToLower(problem.Title), "error") {
			analysis.RealityCheck.ErrorPatterns = append(analysis.RealityCheck.ErrorPatterns, problem.Title)
		}
		if strings.Contains(strings.ToLower(problem.Title), "network") {
			analysis.RealityCheck.NetworkIssues = append(analysis.RealityCheck.NetworkIssues, problem.Title)
		}
	}

	return analysis
}

// generateRootCauses creates intelligent root cause analysis
func (e *SimpleEnhancedExplainer) generateRootCauses(problems []types.Problem) []types.RootCause {
	var rootCauses []types.RootCause

	// Pattern-based root cause detection
	memoryCount := 0
	restartCount := 0
	networkCount := 0

	for _, problem := range problems {
		if strings.Contains(strings.ToLower(problem.Title), "memory") {
			memoryCount++
		}
		if strings.Contains(strings.ToLower(problem.Title), "restart") {
			restartCount++
		}
		if strings.Contains(strings.ToLower(problem.Title), "network") {
			networkCount++
		}
	}

	// Memory pressure root cause
	if memoryCount >= 2 {
		rootCauses = append(rootCauses, types.RootCause{
			Title:       "Memory Pressure",
			Description: "Multiple memory-related issues indicate system-wide memory pressure",
			Evidence:    []string{fmt.Sprintf("%d memory-related problems detected", memoryCount)},
			Confidence:  0.85,
		})
	}

	// Instability root cause
	if restartCount >= 2 {
		rootCauses = append(rootCauses, types.RootCause{
			Title:       "System Instability",
			Description: "Frequent restarts indicate underlying stability issues",
			Evidence:    []string{fmt.Sprintf("%d restart-related problems detected", restartCount)},
			Confidence:  0.80,
		})
	}

	// Network issues root cause
	if networkCount >= 2 {
		rootCauses = append(rootCauses, types.RootCause{
			Title:       "Network Connectivity Issues",
			Description: "Multiple network-related problems suggest connectivity issues",
			Evidence:    []string{fmt.Sprintf("%d network-related problems detected", networkCount)},
			Confidence:  0.75,
		})
	}

	// Add general root cause if no specific patterns found
	if len(rootCauses) == 0 && len(problems) > 0 {
		rootCauses = append(rootCauses, types.RootCause{
			Title:       "Resource Issues",
			Description: "Multiple issues detected requiring investigation",
			Evidence:    []string{fmt.Sprintf("%d problems identified", len(problems))},
			Confidence:  0.6,
		})
	}

	return rootCauses
}

// generateSolutions creates intelligent solutions
func (e *SimpleEnhancedExplainer) generateSolutions(resource *types.ResourceRef, problems []types.Problem) []types.Solution {
	var solutions []types.Solution

	// Pattern-based solutions
	hasMemoryIssues := false
	hasRestartIssues := false
	hasNetworkIssues := false

	for _, problem := range problems {
		if strings.Contains(strings.ToLower(problem.Title), "memory") {
			hasMemoryIssues = true
		}
		if strings.Contains(strings.ToLower(problem.Title), "restart") {
			hasRestartIssues = true
		}
		if strings.Contains(strings.ToLower(problem.Title), "network") {
			hasNetworkIssues = true
		}
	}

	// Memory solutions
	if hasMemoryIssues {
		solutions = append(solutions, types.Solution{
			Title:       "Address Memory Issues",
			Description: "Investigate and resolve memory-related problems",
			Commands: []string{
				"kubectl top pods",
				fmt.Sprintf("kubectl describe pod %s -n %s", resource.Name, resource.Namespace),
				"kubectl get events --sort-by=.metadata.creationTimestamp",
			},
			Urgency:    types.SeverityCritical,
			Difficulty: "medium",
			Risk:       "low",
		})
	}

	// Restart solutions
	if hasRestartIssues {
		solutions = append(solutions, types.Solution{
			Title:       "Investigate Restart Patterns",
			Description: "Analyze restart causes and implement stability improvements",
			Commands: []string{
				fmt.Sprintf("kubectl logs %s -n %s --previous", resource.Name, resource.Namespace),
				fmt.Sprintf("kubectl describe pod %s -n %s", resource.Name, resource.Namespace),
				"kubectl get events --field-selector involvedObject.name=" + resource.Name,
			},
			Urgency:    types.SeverityWarning,
			Difficulty: "medium",
			Risk:       "low",
		})
	}

	// Network solutions
	if hasNetworkIssues {
		solutions = append(solutions, types.Solution{
			Title:       "Resolve Network Issues",
			Description: "Investigate and fix network connectivity problems",
			Commands: []string{
				"kubectl get services",
				"kubectl get endpoints",
				fmt.Sprintf("kubectl describe service -n %s", resource.Namespace),
			},
			Urgency:    types.SeverityWarning,
			Difficulty: "medium",
			Risk:       "low",
		})
	}

	// Default investigation solution
	if len(solutions) == 0 {
		solutions = append(solutions, types.Solution{
			Title:       "General Investigation",
			Description: "Perform comprehensive resource analysis",
			Commands: []string{
				fmt.Sprintf("kubectl describe %s %s -n %s", resource.Kind, resource.Name, resource.Namespace),
				fmt.Sprintf("kubectl logs %s -n %s", resource.Name, resource.Namespace),
				"kubectl get events --sort-by=.metadata.creationTimestamp",
			},
			Urgency:    types.SeverityWarning,
			Difficulty: "easy",
			Risk:       "low",
		})
	}

	return solutions
}

// generatePrediction creates prediction based on patterns
func (e *SimpleEnhancedExplainer) generatePrediction(problems []types.Problem) *types.PredictionSummary {
	if len(problems) < 2 {
		return nil
	}

	// Look for escalating patterns
	criticalCount := 0
	for _, problem := range problems {
		if problem.Severity == types.SeverityCritical {
			criticalCount++
		}
	}

	if criticalCount >= 2 {
		return &types.PredictionSummary{
			Type:        "Pattern-based escalation",
			TimeToEvent: 10 * time.Minute,
			Confidence:  0.75,
			Impact:      []string{"Service degradation", "Potential failures"},
		}
	}

	return nil
}

// generateLearning creates learning information
func (e *SimpleEnhancedExplainer) generateLearning(problems []types.Problem) *types.Learning {
	return &types.Learning{
		ConceptExplanation: "Enhanced analysis uses pattern recognition to identify root causes across multiple symptoms",
		WhyItMatters:       "Understanding patterns helps prevent cascading failures and improves system reliability",
		CommonMistakes:     []string{"Treating symptoms individually", "Ignoring pattern correlations", "Reactive vs proactive monitoring"},
		BestPractices:      []string{"Monitor system patterns", "Use correlation analysis", "Implement predictive alerting", "Regular health assessments"},
	}
}

// addEBPFInsights adds eBPF-based insights
func (e *SimpleEnhancedExplainer) addEBPFInsights(ctx context.Context, resource *types.ResourceRef, explanation *types.Explanation) {
	if explanation.Analysis == nil {
		return
	}

	// Try to get memory stats
	if memStats := e.ebpfMonitor.GetMemoryStats(); len(memStats) > 0 {
		// Extract memory usage from the stats map
		if currentUsage, ok := memStats["current_usage"]; ok {
			explanation.Analysis.RealityCheck.ActualMemory = fmt.Sprintf("eBPF: %v bytes current usage", currentUsage)
		}
	}
}

// calculateConfidence calculates overall confidence based on patterns
func (e *SimpleEnhancedExplainer) calculateConfidence(patterns map[string]int, totalProblems int) float64 {
	if totalProblems == 0 {
		return 0.0
	}

	// Base confidence on pattern strength
	confidence := 0.5 // Base confidence

	// Increase confidence for clear patterns
	for _, count := range patterns {
		if count >= 2 {
			confidence += 0.1 // Add 10% for each pattern
		}
	}

	// Cap at 95%
	if confidence > 0.95 {
		confidence = 0.95
	}

	return confidence
}
