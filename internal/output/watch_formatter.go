package output

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/yairfalse/tapio/pkg/types"
)

// WatchEvent represents an event from the watch stream
type WatchEvent struct {
	Type      string               `json:"type"`
	Resource  *types.ResourceInfo  `json:"resource"`
	Problem   *types.Problem       `json:"problem,omitempty"`
	Timestamp time.Time            `json:"timestamp"`
	Sequence  uint64               `json:"sequence"`
}

// WatchEventFormatter formats watch events for output
type WatchEventFormatter interface {
	Format(event *WatchEvent)
}

// StreamFormatter formats events for human-readable streaming output
type StreamFormatter struct {
	lastEventTime time.Time
	eventCount    int
}

// NewStreamFormatter creates a new stream formatter
func NewStreamFormatter() *StreamFormatter {
	return &StreamFormatter{
		lastEventTime: time.Now(),
	}
}

// Format formats a watch event for streaming output
func (f *StreamFormatter) Format(event *WatchEvent) {
	f.eventCount++

	// Format timestamp
	timestamp := event.Timestamp.Format("15:04:05")
	
	// Determine event icon and color based on type and severity
	icon, msgColor := f.getEventStyle(event)
	
	// Build the main message
	message := f.buildMessage(event)
	
	// Print the formatted event
	fmt.Printf("[%s] %s %s\n", 
		color.HiBlackString(timestamp),
		icon,
		msgColor(message))
	
	// Print additional details for problems
	if event.Problem != nil && event.Problem.Severity != types.SeverityHealthy {
		f.printProblemDetails(event.Problem)
	}
	
	// Add spacing for critical events
	if event.Problem != nil && event.Problem.Severity == types.SeverityCritical {
		fmt.Println()
	}
}

func (f *StreamFormatter) getEventStyle(event *WatchEvent) (string, func(...interface{}) string) {
	// No problem means it's an informational event
	if event.Problem == nil {
		switch event.Type {
		case "ADDED":
			return "✨", color.GreenString
		case "MODIFIED":
			return "↻", color.BlueString
		case "DELETED":
			return "✗", color.YellowString
		default:
			return "•", fmt.Sprint
		}
	}
	
	// Style based on problem severity
	switch event.Problem.Severity {
	case types.SeverityCritical:
		return "🔥", color.RedString
	case types.SeverityWarning:
		return "⚠️", color.YellowString
	case types.SeverityHealthy:
		return "✅", color.GreenString
	default:
		return "ℹ️", color.CyanString
	}
}

func (f *StreamFormatter) buildMessage(event *WatchEvent) string {
	resource := event.Resource
	resourceID := fmt.Sprintf("%s/%s", strings.ToLower(resource.Kind), resource.Name)
	
	if resource.Namespace != "" && resource.Namespace != "default" {
		resourceID = fmt.Sprintf("%s (%s)", resourceID, resource.Namespace)
	}
	
	// If there's a problem, use its title
	if event.Problem != nil && event.Problem.Title != "" {
		return fmt.Sprintf("%s: %s", resourceID, event.Problem.Title)
	}
	
	// Otherwise, describe the event
	switch event.Type {
	case "ADDED":
		return fmt.Sprintf("%s created", resourceID)
	case "MODIFIED":
		return fmt.Sprintf("%s updated", resourceID)
	case "DELETED":
		return fmt.Sprintf("%s deleted", resourceID)
	default:
		return fmt.Sprintf("%s %s", resourceID, strings.ToLower(event.Type))
	}
}

func (f *StreamFormatter) printProblemDetails(problem *types.Problem) {
	// Print description if available
	if problem.Description != "" {
		lines := strings.Split(problem.Description, "\n")
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				fmt.Printf("         %s\n", color.HiBlackString(line))
			}
		}
	}
	
	// Print next steps if available
	if len(problem.NextSteps) > 0 {
		fmt.Printf("         %s\n", color.CyanString("Next steps:"))
		for i, step := range problem.NextSteps {
			fmt.Printf("         %s %s\n", 
				color.HiBlackString(fmt.Sprintf("[%d]", i+1)),
				step)
		}
	}
	
	// Print suggested fix if available
	if problem.SuggestedFix != "" {
		fmt.Printf("         %s %s\n",
			color.GreenString("Fix:"),
			problem.SuggestedFix)
	}
	
	// Print prediction if available
	if problem.Prediction != nil {
		predictionMsg := fmt.Sprintf("Predicted failure in %s (%.0f%% confidence)",
			problem.Prediction.TimeToFailure.Round(time.Minute),
			problem.Prediction.Confidence*100)
		fmt.Printf("         %s %s\n",
			color.MagentaString("⏰"),
			color.MagentaString(predictionMsg))
	}
}

// JSONStreamFormatter formats events as JSON for streaming
type JSONStreamFormatter struct {
	encoder *json.Encoder
}

// NewJSONStreamFormatter creates a new JSON stream formatter
func NewJSONStreamFormatter() *JSONStreamFormatter {
	return &JSONStreamFormatter{
		encoder: json.NewEncoder(color.Output),
	}
}

// Format formats a watch event as JSON
func (f *JSONStreamFormatter) Format(event *WatchEvent) {
	f.encoder.Encode(event)
}

// SummaryFormatter formats periodic summaries during watch
type SummaryFormatter struct {
	startTime     time.Time
	lastSummary   time.Time
	summaryPeriod time.Duration
}

// NewSummaryFormatter creates a new summary formatter
func NewSummaryFormatter(period time.Duration) *SummaryFormatter {
	now := time.Now()
	return &SummaryFormatter{
		startTime:     now,
		lastSummary:   now,
		summaryPeriod: period,
	}
}

// ShouldPrintSummary returns true if it's time for a summary
func (f *SummaryFormatter) ShouldPrintSummary() bool {
	return time.Since(f.lastSummary) >= f.summaryPeriod
}

// PrintSummary prints a watch summary
func (f *SummaryFormatter) PrintSummary(stats WatchStats) {
	f.lastSummary = time.Now()
	runtime := time.Since(f.startTime).Round(time.Second)
	
	// Build summary line
	parts := []string{
		fmt.Sprintf("Runtime: %s", runtime),
		fmt.Sprintf("Events: %d", stats.TotalEvents),
	}
	
	if stats.ActiveProblems > 0 {
		parts = append(parts, color.YellowString("Problems: %d", stats.ActiveProblems))
	}
	
	if stats.CriticalCount > 0 {
		parts = append(parts, color.RedString("Critical: %d", stats.CriticalCount))
	}
	
	summary := strings.Join(parts, " | ")
	fmt.Printf("\n%s %s\n\n", color.HiBlackString("📊"), summary)
}

// WatchStats contains statistics for watch summary
type WatchStats struct {
	TotalEvents    int64
	ActiveProblems int
	CriticalCount  int
	WarningCount   int
	ResourceCount  int
}