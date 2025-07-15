package output

import (
	"fmt"
	"time"

	"github.com/fatih/color"

	"github.com/yairfalse/tapio/pkg/types"
)

type HumanFormatter struct{}

func NewHumanFormatter() *HumanFormatter {
	return &HumanFormatter{}
}

// PrintExplanation prints a human-friendly explanation
func (f *HumanFormatter) PrintExplanation(explanation *types.Explanation) error {
	bold := color.New(color.Bold).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()
	cyan := color.New(color.FgCyan).SprintFunc()

	// Header
	fmt.Printf("\n%s %s\n", bold("Analyzing:"),
		cyan(fmt.Sprintf("%s/%s", explanation.Resource.Kind, explanation.Resource.Name)))
	if explanation.Resource.Namespace != "" {
		fmt.Printf("%s %s\n", bold("Namespace:"), explanation.Resource.Namespace)
	}
	fmt.Println()

	// Summary
	fmt.Printf("%s %s\n\n", bold("Summary:"), explanation.Summary)

	// Reality Check
	if explanation.Analysis != nil && explanation.Analysis.RealityCheck != nil {
		fmt.Printf("%s\n", bold("Current Status:"))
		rc := explanation.Analysis.RealityCheck
		fmt.Printf("  - Memory: %s\n", rc.ActualMemory)
		fmt.Printf("  - Restarts: %s\n", rc.RestartPattern)
		if len(rc.ErrorPatterns) > 0 && rc.ErrorPatterns[0] != "No error patterns detected" {
			fmt.Printf("  - Errors: %s\n", red(rc.ErrorPatterns[0]))
		}
		fmt.Println()
	}

	// Root Causes
	if len(explanation.RootCauses) > 0 {
		fmt.Printf("%s\n", bold("Findings:"))
		for i, cause := range explanation.RootCauses {
			confidenceColor := green
			if cause.Confidence < 0.7 {
				confidenceColor = yellow
			}
			fmt.Printf("  %d. %s %s\n", i+1, cause.Title,
				confidenceColor(fmt.Sprintf("(%.0f%% confidence)", cause.Confidence*100)))
			fmt.Printf("     %s\n", cause.Description)
			if len(cause.Evidence) > 0 {
				for _, evidence := range cause.Evidence {
					fmt.Printf("     - %s\n", evidence)
				}
			}
			fmt.Println()
		}
	}

	// Solutions
	if len(explanation.Solutions) > 0 {
		fmt.Printf("%s\n", bold("Recommended Actions:"))
		for i, solution := range explanation.Solutions {
			urgencyLabel := ""
			if solution.Urgency == types.SeverityCritical {
				urgencyLabel = red("[CRITICAL]")
			} else if solution.Urgency == types.SeverityWarning {
				urgencyLabel = yellow("[WARNING]")
			}

			fmt.Printf("\n  %s %s %s\n", bold(fmt.Sprintf("%d.", i+1)), solution.Title, urgencyLabel)
			fmt.Printf("     %s\n", solution.Description)

			if len(solution.Commands) > 0 {
				fmt.Printf("     %s\n", bold("Run these commands:"))
				for _, cmd := range solution.Commands {
					fmt.Printf("     %s %s\n", cyan("$"), cmd)
				}
			}
		}
		fmt.Println()
	}

	return nil
}

func (f *HumanFormatter) Print(result *types.CheckResult) error {
	f.printSummary(result.Summary)
	f.printProblems(result.Problems)
	f.printCorrelationAnalysis(result.CorrelationAnalysis)
	f.printQuickFixes(result.QuickFixes)
	return nil
}

func (f *HumanFormatter) printSummary(summary types.Summary) {
	green := color.New(color.FgGreen, color.Bold).SprintFunc()
	yellow := color.New(color.FgYellow, color.Bold).SprintFunc()
	red := color.New(color.FgRed, color.Bold).SprintFunc()

	if summary.HealthyPods > 0 {
		fmt.Printf("%s %d pods healthy\n", green("HEALTHY:"), summary.HealthyPods)
	}

	if summary.WarningPods > 0 {
		fmt.Printf("%s %d pods have warnings\n", yellow("WARNING:"), summary.WarningPods)
	}

	if summary.CriticalPods > 0 {
		fmt.Printf("%s %d pods failing\n", red("CRITICAL:"), summary.CriticalPods)
	}

	if summary.TotalPods == 0 {
		fmt.Println("No pods found in the specified scope")
	}
}

func (f *HumanFormatter) printProblems(problems []types.Problem) {
	if len(problems) == 0 {
		return
	}

	fmt.Println()
	for i := range problems {
		f.printProblem(&problems[i])
	}
}

func (f *HumanFormatter) printProblem(problem *types.Problem) {
	var label string
	var colorFunc func(...interface{}) string

	switch problem.Severity {
	case types.SeverityCritical:
		label = "CRITICAL:"
		colorFunc = color.New(color.FgRed, color.Bold).SprintFunc()
	case types.SeverityWarning:
		label = "WARNING:"
		colorFunc = color.New(color.FgYellow, color.Bold).SprintFunc()
	default:
		label = "INFO:"
		colorFunc = color.New(color.FgBlue, color.Bold).SprintFunc()
	}

	resourceName := fmt.Sprintf("%s/%s", problem.Resource.Kind, problem.Resource.Name)
	fmt.Printf("%s %s: %s\n", colorFunc(label), resourceName, problem.Title)

	if problem.Description != "" {
		fmt.Printf("   %s\n", problem.Description)
	}

	if problem.Prediction != nil {
		f.printProblemPrediction(problem.Prediction)
	}
}

func (f *HumanFormatter) printProblemPrediction(pred *types.Prediction) {
	duration := pred.TimeToFailure.Round(time.Minute)
	confidence := int(pred.Confidence * 100)

	fmt.Printf("   PREDICTION: Will fail in %v (%d%% confidence)\n", duration, confidence)
	if pred.Reason != "" {
		fmt.Printf("   REASON: %s\n", pred.Reason)
	}
}

func (f *HumanFormatter) printQuickFixes(fixes []types.QuickFix) {
	if len(fixes) == 0 {
		return
	}

	fmt.Println("\nQuick fixes available:")
	for _, fix := range fixes {
		urgencyLabel := f.getUrgencyLabel(fix.Urgency)
		fmt.Printf("  %s %s\n", urgencyLabel, fix.Command)
		if fix.Description != "" {
			fmt.Printf("     %s\n", fix.Description)
		}
	}
}

func (f *HumanFormatter) getUrgencyLabel(urgency types.Severity) string {
	red := color.New(color.FgRed, color.Bold).SprintFunc()
	yellow := color.New(color.FgYellow, color.Bold).SprintFunc()
	blue := color.New(color.FgBlue).SprintFunc()

	switch urgency {
	case types.SeverityCritical:
		return red("[URGENT]")
	case types.SeverityWarning:
		return yellow("[FIX]")
	default:
		return blue("[INFO]")
	}
}

func (f *HumanFormatter) printCorrelationAnalysis(analysis interface{}) {
	if analysis == nil {
		return
	}

	// Type assert to map for flexible handling
	analysisMap, ok := analysis.(map[string]interface{})
	if !ok {
		return
	}

	// Check if this is intelligent analysis
	analysisType, _ := analysisMap["analysis_type"].(string)
	if analysisType != "intelligent" {
		// Simple analysis - just show basic patterns
		f.printSimpleCorrelationAnalysis(analysisMap)
		return
	}

	bold := color.New(color.Bold).SprintFunc()
	cyan := color.New(color.FgCyan).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()

	fmt.Printf("\n%s\n", bold("📊 Correlation Analysis:"))

	// Critical patterns warning
	if critical, ok := analysisMap["critical_patterns_detected"].(bool); ok && critical {
		fmt.Printf("%s Critical patterns detected requiring immediate attention\n\n", red("⚠️ "))
	}

	// Display insights
	if insights, ok := analysisMap["insights"].([]interface{}); ok && len(insights) > 0 {
		fmt.Printf("%s\n", bold("Key Insights:"))
		for i, insight := range insights {
			if insightMap, ok := insight.(map[string]interface{}); ok {
				f.printInsight(i+1, insightMap)
			}
		}
	}

	// Display patterns
	if patterns, ok := analysisMap["patterns"].([]interface{}); ok && len(patterns) > 0 {
		fmt.Printf("\n%s\n", bold("Detected Patterns:"))
		for _, pattern := range patterns {
			if patternMap, ok := pattern.(map[string]interface{}); ok {
				f.printPattern(patternMap)
			}
		}
	}

	// Display recommendations
	if recommendations, ok := analysisMap["recommendations"].([]interface{}); ok && len(recommendations) > 0 {
		fmt.Printf("\n%s\n", bold("Actionable Recommendations:"))
		for i, rec := range recommendations {
			if recMap, ok := rec.(map[string]interface{}); ok {
				f.printRecommendation(i+1, recMap)
			}
		}
	}

	// Display timeline summary
	if timeline, ok := analysisMap["timeline"].(map[string]interface{}); ok {
		fmt.Printf("\n%s\n", bold("Timeline Summary:"))
		if totalEvents, ok := timeline["total_events"].(int); ok {
			fmt.Printf("  Total events analyzed: %s\n", cyan(fmt.Sprintf("%d", totalEvents)))
		}
		if eventsBySeverity, ok := timeline["events_by_severity"].(map[string]interface{}); ok {
			fmt.Printf("  Events by severity: ")
			first := true
			for sev, count := range eventsBySeverity {
				if !first {
					fmt.Printf(", ")
				}
				first = false
				colorFunc := green
				if sev == "critical" || sev == "high" {
					colorFunc = red
				} else if sev == "medium" {
					colorFunc = yellow
				}
				fmt.Printf("%s: %s", sev, colorFunc(fmt.Sprintf("%v", count)))
			}
			fmt.Println()
		}
	}
}

func (f *HumanFormatter) printSimpleCorrelationAnalysis(analysisMap map[string]interface{}) {
	// Handle simple correlation analysis output
	if patterns, ok := analysisMap["patterns"].(map[string]int); ok && len(patterns) > 0 {
		bold := color.New(color.Bold).SprintFunc()
		fmt.Printf("\n%s\n", bold("Pattern Analysis:"))
		for pattern, count := range patterns {
			fmt.Printf("  - %s issues: %d\n", pattern, count)
		}
	}

	if insights, ok := analysisMap["insights"].([]string); ok && len(insights) > 0 {
		bold := color.New(color.Bold).SprintFunc()
		yellow := color.New(color.FgYellow).SprintFunc()
		fmt.Printf("\n%s\n", bold("Insights:"))
		for _, insight := range insights {
			fmt.Printf("  %s %s\n", yellow("⚡"), insight)
		}
	}
}

func (f *HumanFormatter) printInsight(num int, insight map[string]interface{}) {
	bold := color.New(color.Bold).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	cyan := color.New(color.FgCyan).SprintFunc()

	title, _ := insight["Title"].(string)
	description, _ := insight["Description"].(string)
	severity, _ := insight["Severity"].(string)
	resourceName, _ := insight["ResourceName"].(string)
	namespace, _ := insight["Namespace"].(string)

	severityLabel := ""
	switch severity {
	case "critical":
		severityLabel = red("[CRITICAL]")
	case "high":
		severityLabel = red("[HIGH]")
	case "medium":
		severityLabel = yellow("[MEDIUM]")
	}

	fmt.Printf("\n  %s %s %s\n", bold(fmt.Sprintf("%d.", num)), title, severityLabel)
	if resourceName != "" {
		fmt.Printf("     Resource: %s\n", cyan(fmt.Sprintf("%s/%s", namespace, resourceName)))
	}
	fmt.Printf("     %s\n", description)

	// Print evidence if available
	if evidence, ok := insight["Evidence"].([]interface{}); ok && len(evidence) > 0 {
		fmt.Printf("     Evidence:\n")
		for _, ev := range evidence {
			if evMap, ok := ev.(map[string]interface{}); ok {
				desc, _ := evMap["Description"].(string)
				fmt.Printf("       - %s\n", desc)
			}
		}
	}

	// Print prediction if available
	if prediction, ok := insight["Prediction"].(map[string]interface{}); ok {
		predType, _ := prediction["Type"].(string)
		probability, _ := prediction["Probability"].(float64)
		desc, _ := prediction["Description"].(string)
		fmt.Printf("     %s Prediction: %s (%.0f%% probability)\n", yellow("⚠️"), predType, probability*100)
		if desc != "" {
			fmt.Printf("       %s\n", desc)
		}
	}
}

func (f *HumanFormatter) printPattern(pattern map[string]interface{}) {
	cyan := color.New(color.FgCyan).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()

	patternType, _ := pattern["Type"].(string)
	description, _ := pattern["Description"].(string)
	confidence, _ := pattern["Confidence"].(float64)

	icon := "📈"
	switch patternType {
	case "burst":
		icon = "💥"
	case "cascade":
		icon = "🌊"
	case "repeating":
		icon = "🔄"
	}

	fmt.Printf("  %s %s pattern: %s ", icon, cyan(patternType), description)
	if confidence > 0 {
		fmt.Printf("%s\n", yellow(fmt.Sprintf("(%.0f%% confidence)", confidence*100)))
	} else {
		fmt.Println()
	}
}

func (f *HumanFormatter) printRecommendation(num int, rec map[string]interface{}) {
	bold := color.New(color.Bold).SprintFunc()
	cyan := color.New(color.FgCyan).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()

	description, _ := rec["Description"].(string)
	command, _ := rec["Command"].(string)
	impact, _ := rec["Impact"].(string)
	risk, _ := rec["Risk"].(string)

	fmt.Printf("\n  %s %s\n", bold(fmt.Sprintf("%d.", num)), description)
	if command != "" {
		fmt.Printf("     %s %s\n", cyan("$"), command)
	}
	if impact != "" {
		fmt.Printf("     Impact: %s\n", green(impact))
	}
	if risk != "" {
		riskColor := green
		if risk == "medium" {
			riskColor = yellow
		} else if risk == "high" {
			riskColor = color.New(color.FgRed).SprintFunc()
		}
		fmt.Printf("     Risk: %s\n", riskColor(risk))
	}
}
