package output

import (
	"fmt"
	"time"

	"github.com/fatih/color"

	"github.com/falseyair/tapio/pkg/types"
)

type HumanFormatter struct{}

func NewHumanFormatter() *HumanFormatter {
	return &HumanFormatter{}
}

func (f *HumanFormatter) Print(result *types.CheckResult) error {
	f.printSummary(result.Summary)
	f.printProblems(result.Problems)
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
		f.printPrediction(problem.Prediction)
	}
}

func (f *HumanFormatter) printPrediction(pred *types.Prediction) {
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
