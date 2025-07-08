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
	
	// Add some spacing for better readability
	fmt.Println()
	
	// Use friendly emoji and language based on the summary
	if strings.Contains(explanation.Summary, "healthy") {
		fmt.Printf("%s %s\n\n", blue("GOOD NEWS:"), explanation.Summary)
	} else if strings.Contains(explanation.Summary, "killed") || strings.Contains(explanation.Summary, "crash") {
		fmt.Printf("%s %s\n\n", blue("OH NO:"), explanation.Summary)
	} else {
		fmt.Printf("%s %s\n\n", blue("HEADS UP:"), explanation.Summary)
	}
}

func (f *HumanFormatter) printAnalysis(analysis *types.Analysis) {
	if analysis == nil {
		return
	}

	cyan := color.New(color.FgCyan, color.Bold).SprintFunc()
	fmt.Printf("%s\n", cyan("HERE'S WHAT'S HAPPENING:"))

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
	fmt.Printf("%s\n", yellow("HERE'S WHY:"))

	for _, cause := range causes {
		confidence := int(cause.Confidence * 100)
		confidenceText := f.getConfidenceText(confidence)
		fmt.Printf("  %s %s\n", cause.Title, confidenceText)
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
	fmt.Printf("%s\n", green("LET'S FIX IT:"))

	for _, solution := range solutions {
		urgencyColor := f.getSolutionColor(solution.Urgency)
		urgencyLabel := f.getFriendlyUrgencyLabel(solution.Urgency)
		fmt.Printf("  %s %s\n", urgencyColor(urgencyLabel), solution.Title)
		
		if solution.Description != "" {
			wrapped := f.wrapText(solution.Description, 70)
			fmt.Printf("    %s\n", wrapped)
		}
		
		if len(solution.Commands) > 0 {
			fmt.Println("    Try this:")
			for _, cmd := range solution.Commands {
				if strings.HasPrefix(cmd, "#") {
					// Comments in a softer color
					fmt.Printf("      %s\n", color.New(color.FgHiBlack).Sprint(cmd))
				} else if cmd == "" {
					// Empty lines for spacing
					fmt.Println()
				} else {
					// Commands in a box-like format for clarity
					fmt.Printf("      $ %s\n", color.New(color.FgWhite, color.Bold).Sprint(cmd))
				}
			}
		}
		
		difficultyText := f.getFriendlyDifficulty(solution.Difficulty)
		riskText := f.getFriendlyRisk(solution.Risk)
		gray := color.New(color.FgHiBlack).SprintFunc()
		fmt.Printf("    %s\n\n", gray(fmt.Sprintf("%s • %s", difficultyText, riskText)))
	}
}

func (f *HumanFormatter) printLearning(learning *types.Learning) {
	if learning == nil {
		return
	}

	magenta := color.New(color.FgMagenta, color.Bold).SprintFunc()
	fmt.Printf("%s\n", magenta("WANT TO UNDERSTAND BETTER?"))

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

func (f *HumanFormatter) getFriendlyUrgencyLabel(urgency types.Severity) string {
	switch urgency {
	case types.SeverityCritical:
		return "[DO THIS FIRST]"
	case types.SeverityWarning:
		return "[WHEN YOU CAN]"
	default:
		return "[FOR MORE INFO]"
	}
}

func (f *HumanFormatter) getConfidenceText(confidence int) string {
	gray := color.New(color.FgHiBlack).SprintFunc()
	if confidence >= 90 {
		return gray("(I'm pretty sure)")
	} else if confidence >= 70 {
		return gray("(probably)")
	} else {
		return gray("(maybe)")
	}
}

func (f *HumanFormatter) getFriendlyDifficulty(difficulty string) string {
	switch difficulty {
	case "easy":
		return "Takes 2 minutes"
	case "medium":
		return "Takes 5-10 minutes"
	case "hard":
		return "Might take a while"
	default:
		return "Difficulty unknown"
	}
}

func (f *HumanFormatter) getFriendlyRisk(risk string) string {
	switch risk {
	case "low":
		return "Safe to try"
	case "medium":
		return "Test first if possible"
	case "high":
		return "Be careful with this one"
	default:
		return "Risk unknown"
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