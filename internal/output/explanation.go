package output

import (
	"fmt"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/falseyair/tapio/pkg/types"
)

// PrintExplanation prints a detailed explanation in human-readable format
func (f *HumanFormatter) PrintExplanation(explanation *types.Explanation) error {
	f.printExplanationHeader(explanation)
	f.printAnalysis(explanation.Analysis)
	f.printRootCauses(explanation.RootCauses)
	
	if explanation.Prediction != nil {
		f.printPrediction(explanation.Prediction)
	}
	
	f.printSolutions(explanation.Solutions)
	
	if explanation.Learning != nil {
		f.printLearning(explanation.Learning)
	}
	
	return nil
}

func (f *HumanFormatter) printExplanationHeader(explanation *types.Explanation) {
	blue := color.New(color.FgBlue, color.Bold).SprintFunc()
	red := color.New(color.FgRed, color.Bold).SprintFunc()
	
	// Add some spacing for better readability
	fmt.Println()
	
	// Check if we have eBPF-detected issues
	hasEBPFInsights := false
	for _, cause := range explanation.RootCauses {
		if strings.Contains(cause.Title, "eBPF") || strings.Contains(cause.Title, "Memory leak detected") {
			hasEBPFInsights = true
			break
		}
	}
	
	// Use appropriate header based on content
	if hasEBPFInsights {
		fmt.Printf("%s %s\n\n", red("ANALYSIS:"), explanation.Summary)
	} else if strings.Contains(explanation.Summary, "healthy") {
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
		
		// Print eBPF insights if available
		if analysis.RealityCheck.EBPFInsights != nil {
			f.printEBPFInsights(analysis.RealityCheck.EBPFInsights)
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

	// Print kernel insights if available
	if analysis.KernelInsights != nil {
		f.printKernelInsights(analysis.KernelInsights)
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
	fmt.Printf("%s\n", green("HOW TO FIX:"))

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

// printEBPFInsights prints eBPF data in a friendly format
func (f *HumanFormatter) printEBPFInsights(insights *types.EBPFInsights) {
	magenta := color.New(color.FgMagenta, color.Bold).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()
	
	// Memory overview
	if insights.TotalMemory > 0 {
		fmt.Printf("  • %s Memory: %s", magenta("eBPF Reality:"), f.humanizeBytes(insights.TotalMemory))
		if insights.MemoryGrowthRate > 0 {
			growthPerMin := insights.MemoryGrowthRate * 60
			fmt.Printf(" (growing at %s/min, currently %s)", 
				red(f.humanizeBytes(uint64(growthPerMin))),
				f.humanizeBytes(insights.TotalMemory))
		}
		fmt.Println()
	}
	
	// Process details
	for _, proc := range insights.Processes {
		fmt.Printf("  • Process %s %s: %s %s",
			yellow("PID"),
			yellow(fmt.Sprintf("%d", proc.PID)),
			proc.Command,
			f.humanizeBytes(proc.MemoryUsage))
		
		if proc.AllocationRate > 0 {
			fmt.Printf(" [+%s/sec]", f.humanizeBytes(uint64(proc.AllocationRate)))
		}
		fmt.Println()
		
		if proc.MemoryLeakSignature != "" {
			fmt.Printf("    %s", proc.MemoryLeakSignature)
			fmt.Println()
		}
	}
	
	// Syscall patterns
	if insights.SyscallPattern != "" {
		fmt.Printf("  • %s %s\n", magenta("Syscall Pattern:"), insights.SyscallPattern)
	}
}

// printKernelInsights prints kernel-level analysis
func (f *HumanFormatter) printKernelInsights(insights *types.KernelInsights) {
	magenta := color.New(color.FgMagenta, color.Bold).SprintFunc()
	
	fmt.Printf("\n  %s\n", magenta("[KERNEL ANALYSIS]"))
	
	if insights.MemoryPressure != "" {
		fmt.Printf("  • Memory: %s\n", insights.MemoryPressure)
	}
	if insights.HeapAnalysis != "" {
		fmt.Printf("  • Heap: %s\n", insights.HeapAnalysis)
	}
	if insights.NetworkCorrelation != "" {
		fmt.Printf("  • Network: %s\n", insights.NetworkCorrelation)
	}
	if insights.DiskIO != "" {
		fmt.Printf("  • Disk I/O: %s\n", insights.DiskIO)
	}
	if insights.CPUOverhead != "" {
		fmt.Printf("  • CPU: %s\n", insights.CPUOverhead)
	}
}

// printPrediction prints future failure predictions
func (f *HumanFormatter) printPrediction(pred *types.PredictionSummary) {
	red := color.New(color.FgRed, color.Bold).SprintFunc()
	yellow := color.New(color.FgYellow, color.Bold).SprintFunc()
	
	fmt.Printf("\n%s\n", red("[PREDICTION]"))
	
	// Time to event
	timeStr := f.formatDuration(pred.TimeToEvent)
	fmt.Printf("  %s %s in %s (%.0f%% confidence)\n",
		yellow("WARNING:"),
		pred.Type,
		red(timeStr),
		pred.Confidence*100)
	
	// Impact
	if len(pred.Impact) > 0 {
		fmt.Println("  Expected impact:")
		for _, impact := range pred.Impact {
			fmt.Printf("    • %s\n", impact)
		}
	}
	fmt.Println()
}

// Helper to format duration in a friendly way
func (f *HumanFormatter) formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0f seconds", d.Seconds())
	} else if d < time.Hour {
		return fmt.Sprintf("%.0f minutes", d.Minutes())
	} else {
		return fmt.Sprintf("%.1f hours", d.Hours())
	}
}

// Helper to humanize byte sizes
func (f *HumanFormatter) humanizeBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}