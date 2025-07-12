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
	// Correlation analysis temporarily disabled
	// f.printCorrelationAnalysis(nil)
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
