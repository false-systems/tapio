package output

import (
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/falseyair/tapio/pkg/types"
)

// PrintExplanation prints a detailed explanation in human-readable format
func (f *HumanFormatter) PrintExplanation(explanation *types.Explanation) error {
	f.printExplanationHeader(explanation)
	f.printAnalysis(explanation.Analysis)
	f.printRootCauses(explanation.RootCauses)
	f.printSolutions(explanation.Solutions)
	
	if explanation.Learning != nil {
		f.printLearning(explanation.Learning)
	}
	
	return nil
}

func (f *HumanFormatter) printExplanationHeader(explanation *types.Explanation) {
	blue := color.New(color.FgBlue, color.Bold).SprintFunc()
	fmt.Printf("%s %s\n\n", blue("ANALYSIS:"), explanation.Summary)
}

func (f *HumanFormatter) printAnalysis(analysis *types.Analysis) {
	if analysis == nil {
		return
	}

	cyan := color.New(color.FgCyan, color.Bold).SprintFunc()
	fmt.Printf("%s\n", cyan("WHAT I SEE:"))

	if analysis.KubernetesView != nil {
		fmt.Printf("  • Kubernetes says: Pod %s", analysis.KubernetesView.Status)
		if len(analysis.KubernetesView.Resources) > 0 {
			var resources []string
			for key, value := range analysis.KubernetesView.Resources {
				resources = append(resources, fmt.Sprintf("%s %s", strings.Replace(key, "_", " ", -1), value))
			}
			if len(resources) > 0 {
				fmt.Printf(", %s", strings.Join(resources, ", "))
			}
		}
		fmt.Println()

		if len(analysis.KubernetesView.Events) > 0 {
			fmt.Printf("  • Recent events: %s\n", strings.Join(analysis.KubernetesView.Events, "; "))
		}
	}

	if analysis.RealityCheck != nil {
		if analysis.RealityCheck.RestartPattern != "" {
			fmt.Printf("  • Reality check: %s\n", analysis.RealityCheck.RestartPattern)
		}
		for _, pattern := range analysis.RealityCheck.ErrorPatterns {
			fmt.Printf("  • Error pattern: %s\n", pattern)
		}
	}

	if analysis.Correlation != nil {
		for _, discrepancy := range analysis.Correlation.Discrepancies {
			fmt.Printf("  • Discrepancy: %s\n", discrepancy)
		}
		for _, pattern := range analysis.Correlation.Patterns {
			fmt.Printf("  • Pattern: %s\n", pattern)
		}
	}

	fmt.Println()
}

func (f *HumanFormatter) printRootCauses(causes []types.RootCause) {
	if len(causes) == 0 {
		return
	}

	yellow := color.New(color.FgYellow, color.Bold).SprintFunc()
	fmt.Printf("%s\n", yellow("WHY THIS HAPPENS:"))

	for _, cause := range causes {
		confidence := int(cause.Confidence * 100)
		fmt.Printf("  %s (%d%% confidence)\n", cause.Title, confidence)
		if cause.Description != "" {
			// Wrap long descriptions
			wrapped := f.wrapText(cause.Description, 70)
			fmt.Printf("  %s\n", wrapped)
		}
		if len(cause.Evidence) > 0 {
			fmt.Printf("  Evidence: %s\n", strings.Join(cause.Evidence, ", "))
		}
		fmt.Println()
	}
}

func (f *HumanFormatter) printSolutions(solutions []types.Solution) {
	if len(solutions) == 0 {
		return
	}

	green := color.New(color.FgGreen, color.Bold).SprintFunc()
	fmt.Printf("%s\n", green("HOW TO FIX:"))

	for _, solution := range solutions {
		urgencyColor := f.getSolutionColor(solution.Urgency)
		fmt.Printf("  %s %s\n", urgencyColor(fmt.Sprintf("[%s]", strings.ToUpper(string(solution.Urgency)))), solution.Title)
		
		if solution.Description != "" {
			wrapped := f.wrapText(solution.Description, 70)
			fmt.Printf("    %s\n", wrapped)
		}
		
		if len(solution.Commands) > 0 {
			fmt.Println("    Commands:")
			for _, cmd := range solution.Commands {
				if strings.HasPrefix(cmd, "#") {
					fmt.Printf("      %s\n", color.New(color.FgBlue).Sprint(cmd))
				} else {
					fmt.Printf("      %s\n", color.New(color.FgWhite, color.Bold).Sprint(cmd))
				}
			}
		}
		
		fmt.Printf("    Difficulty: %s, Risk: %s\n\n", solution.Difficulty, solution.Risk)
	}
}

func (f *HumanFormatter) printLearning(learning *types.Learning) {
	if learning == nil {
		return
	}

	magenta := color.New(color.FgMagenta, color.Bold).SprintFunc()
	fmt.Printf("%s\n", magenta("LEARN MORE:"))

	if learning.ConceptExplanation != "" {
		wrapped := f.wrapText(learning.ConceptExplanation, 70)
		fmt.Printf("  %s\n\n", wrapped)
	}

	if learning.WhyItMatters != "" {
		wrapped := f.wrapText(learning.WhyItMatters, 70)
		fmt.Printf("  Why this matters: %s\n\n", wrapped)
	}

	if len(learning.CommonMistakes) > 0 {
		fmt.Println("  Common mistakes:")
		for _, mistake := range learning.CommonMistakes {
			wrapped := f.wrapText(mistake, 66)
			fmt.Printf("    • %s\n", wrapped)
		}
		fmt.Println()
	}

	if len(learning.BestPractices) > 0 {
		fmt.Println("  Best practices:")
		for _, practice := range learning.BestPractices {
			wrapped := f.wrapText(practice, 66)
			fmt.Printf("    • %s\n", wrapped)
		}
		fmt.Println()
	}
}

func (f *HumanFormatter) getSolutionColor(urgency types.Severity) func(...interface{}) string {
	switch urgency {
	case types.SeverityCritical:
		return color.New(color.FgRed, color.Bold).SprintFunc()
	case types.SeverityWarning:
		return color.New(color.FgYellow, color.Bold).SprintFunc()
	default:
		return color.New(color.FgBlue, color.Bold).SprintFunc()
	}
}

// wrapText wraps long text to fit within the specified width
func (f *HumanFormatter) wrapText(text string, width int) string {
	if len(text) <= width {
		return text
	}
	
	words := strings.Fields(text)
	if len(words) == 0 {
		return text
	}
	
	var lines []string
	var currentLine []string
	currentLength := 0
	
	for _, word := range words {
		// If adding this word would exceed the width, start a new line
		if currentLength+len(word)+len(currentLine) > width && len(currentLine) > 0 {
			lines = append(lines, strings.Join(currentLine, " "))
			currentLine = []string{word}
			currentLength = len(word)
		} else {
			currentLine = append(currentLine, word)
			currentLength += len(word)
		}
	}
	
	// Add the last line
	if len(currentLine) > 0 {
		lines = append(lines, strings.Join(currentLine, " "))
	}
	
	return strings.Join(lines, "\n  ")
}