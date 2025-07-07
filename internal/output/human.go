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
	green := color.New(color.FgGreen).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()

	if summary.HealthyPods > 0 {
		fmt.Printf("%s %d pods healthy\n", green("✅"), summary.HealthyPods)
	}

	if summary.WarningPods > 0 {
		fmt.Printf("%s %d pods have warnings\n", yellow("⚠️"), summary.WarningPods)
	}

	if summary.CriticalPods > 0 {
		fmt.Printf("%s %d pods failing\n", red("❌"), summary.CriticalPods)
	}

	if summary.TotalPods == 0 {
		fmt.Println("🤔 No pods found in the specified scope")
	}
}

func (f *HumanFormatter) printProblems(problems []types.Problem) {
	if len(problems) == 0 {
		return
	}

	fmt.Println()
	for _, problem := range problems {
		f.printProblem(problem)
	}
}

func (f *HumanFormatter) printProblem(problem types.Problem) {
	var icon string
	var colorFunc func(...interface{}) string

	switch problem.Severity {
	case types.SeverityCritical:
		icon = "❌"
		colorFunc = color.New(color.FgRed).SprintFunc()
	case types.SeverityWarning:
		icon = "⚠️"
		colorFunc = color.New(color.FgYellow).SprintFunc()
	default:
		icon = "ℹ️"
		colorFunc = color.New(color.FgBlue).SprintFunc()
	}

	resourceName := fmt.Sprintf("%s/%s", problem.Resource.Kind, problem.Resource.Name)
	fmt.Printf("%s %s: %s\n", icon, colorFunc(resourceName), problem.Title)

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

	fmt.Printf("   🔮 Will fail in %v (%d%% confidence)\n", duration, confidence)
	if pred.Reason != "" {
		fmt.Printf("   📋 Reason: %s\n", pred.Reason)
	}
}

func (f *HumanFormatter) printQuickFixes(fixes []types.QuickFix) {
	if len(fixes) == 0 {
		return
	}

	fmt.Println("\n🔧 Quick fixes available:")
	for _, fix := range fixes {
		urgencyIcon := f.getUrgencyIcon(fix.Urgency)
		fmt.Printf("  %s %s\n", urgencyIcon, fix.Command)
		if fix.Description != "" {
			fmt.Printf("     %s\n", fix.Description)
		}
	}
}

func (f *HumanFormatter) getUrgencyIcon(urgency types.Severity) string {
	switch urgency {
	case types.SeverityCritical:
		return "🚨"
	case types.SeverityWarning:
		return "⚡"
	default:
		return "→"
	}
}