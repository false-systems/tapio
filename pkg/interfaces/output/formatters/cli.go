package formatters

import (
	"fmt"
	"strings"
	"time"

)

// CLIFormatter formats universal data for command-line output
type CLIFormatter struct {
	useColor   bool
	verbosity  int
	maxWidth   int
	timeFormat string
}

// CLIConfig holds configuration for CLI formatter
type CLIConfig struct {
	UseColor   bool
	Verbosity  int // 0=minimal, 1=normal, 2=detailed, 3=debug
	MaxWidth   int
	TimeFormat string
}

// DefaultCLIConfig returns default CLI configuration
func DefaultCLIConfig() *CLIConfig {
	return &CLIConfig{
		UseColor:   true,
		Verbosity:  1,
		MaxWidth:   120,
		TimeFormat: "15:04:05",
	}
}

// NewCLIFormatter creates a new CLI formatter
func NewCLIFormatter(config *CLIConfig) *CLIFormatter {
	if config == nil {
		config = DefaultCLIConfig()
	}

	return &CLIFormatter{
		useColor:   config.UseColor,
		verbosity:  config.Verbosity,
		maxWidth:   config.MaxWidth,
		timeFormat: config.TimeFormat,
	}
}

// FormatMetric formats a universal metric for CLI display
func (f *CLIFormatter) FormatMetric(metric *universal.UniversalMetric) string {
	if metric == nil {
		return ""
	}

	var builder strings.Builder

	// Format header with target info
	target := f.formatTarget(metric.Target)
	timestamp := metric.Timestamp.Format(f.timeFormat)

	// Basic format: [timestamp] target: metric_name = value unit
	builder.WriteString(fmt.Sprintf("[%s] %s: %s = %.2f %s",
		timestamp, target, metric.Name, metric.Value, metric.Unit))

	// Add quality indicator if low confidence
	if metric.Quality.Confidence < 0.8 {
		builder.WriteString(fmt.Sprintf(" [confidence:%.1f]", metric.Quality.Confidence))
	}

	// Add labels in verbose mode
	if f.verbosity >= 2 && len(metric.Labels) > 0 {
		builder.WriteString(" {")
		first := true
		for k, v := range metric.Labels {
			if !first {
				builder.WriteString(", ")
			}
			builder.WriteString(fmt.Sprintf("%s=%q", k, v))
			first = false
		}
		builder.WriteString("}")
	}

	return builder.String()
}

// FormatEvent formats a universal event for CLI display
func (f *CLIFormatter) FormatEvent(event *universal.UniversalEvent) string {
	if event == nil {
		return ""
	}

	var builder strings.Builder

	// Format with level indicator
	levelIndicator := f.getLevelIndicator(event.Level)
	timestamp := event.Timestamp.Format(f.timeFormat)
	target := f.formatTarget(event.Target)

	// Basic format: [timestamp] [LEVEL] target: event_type - message
	builder.WriteString(fmt.Sprintf("[%s] %s %s: %s - %s",
		timestamp, levelIndicator, target, event.Type, event.Message))

	// Add details in verbose mode
	if f.verbosity >= 2 && len(event.Details) > 0 {
		builder.WriteString("\n  Details:")
		for k, v := range event.Details {
			builder.WriteString(fmt.Sprintf("\n    %s: %v", k, v))
		}
	}

	// Add source in debug mode
	if f.verbosity >= 3 {
		builder.WriteString(fmt.Sprintf("\n  Source: %s", event.Quality.Source))
		builder.WriteString(fmt.Sprintf("\n  Confidence: %.1f", event.Quality.Confidence))
	}

	return builder.String()
}

// FormatPrediction formats a universal prediction for CLI display
func (f *CLIFormatter) FormatPrediction(prediction *universal.UniversalPrediction) string {
	if prediction == nil {
		return ""
	}

	var builder strings.Builder

	// Format header
	target := f.formatTarget(prediction.Target)
	impactIndicator := f.getImpactIndicator(prediction.Impact)

	// Basic format: [IMPACT] target: prediction_type in time_to_event (probability%)
	builder.WriteString(fmt.Sprintf("%s %s: %s in %s (%.0f%% probability)",
		impactIndicator, target, prediction.Type,
		f.formatDuration(prediction.TimeToEvent), prediction.Probability*100))

	// Add description
	if prediction.Description != "" {
		builder.WriteString(fmt.Sprintf("\n  %s", prediction.Description))
	}

	// Add factors in verbose mode
	if f.verbosity >= 1 && len(prediction.Factors) > 0 {
		builder.WriteString("\n  Contributing factors:")
		for _, factor := range prediction.Factors {
			builder.WriteString(fmt.Sprintf("\n    • %s", factor))
		}
	}

	// Add mitigations in normal mode
	if f.verbosity >= 1 && len(prediction.Mitigations) > 0 {
		builder.WriteString("\n  Mitigations:")
		for _, mitigation := range prediction.Mitigations {
			builder.WriteString(fmt.Sprintf("\n    → %s", mitigation.Description))
		}
	}

	// Add metadata in debug mode
	if f.verbosity >= 3 && len(prediction.Quality.Metadata) > 0 {
		builder.WriteString("\n  Metadata:")
		for k, v := range prediction.Quality.Metadata {
			builder.WriteString(fmt.Sprintf("\n    %s: %v", k, v))
		}
	}

	return builder.String()
}

// FormatExplanation formats a human-readable explanation from universal data
func (f *CLIFormatter) FormatExplanation(data *universal.UniversalDataset) string {
	if data == nil || len(data.Predictions) == 0 {
		return "No issues or predictions found."
	}

	var builder strings.Builder

	// Group predictions by target
	targetPredictions := make(map[string][]*universal.UniversalPrediction)
	for i := range data.Predictions {
		pred := &data.Predictions[i]
		key := f.formatTarget(pred.Target)
		targetPredictions[key] = append(targetPredictions[key], pred)
	}

	// Format each target's predictions
	first := true
	for target, preds := range targetPredictions {
		if !first {
			builder.WriteString("\n\n")
		}
		first = false

		builder.WriteString(fmt.Sprintf("=== %s ===\n", target))

		// Sort by severity and time to event
		for i, pred := range preds {
			if i > 0 {
				builder.WriteString("\n")
			}
			builder.WriteString(f.FormatPrediction(pred))
		}
	}

	// Add summary in verbose mode
	if f.verbosity >= 1 {
		builder.WriteString(fmt.Sprintf("\n\nSummary: %d predictions across %d targets",
			len(data.Predictions), len(targetPredictions)))

		// Count by impact
		impactCounts := make(map[universal.ImpactLevel]int)
		for _, pred := range data.Predictions {
			impactCounts[pred.Impact]++
		}

		if len(impactCounts) > 0 {
			builder.WriteString(" (")
			first = true
			for impact, count := range impactCounts {
				if !first {
					builder.WriteString(", ")
				}
				builder.WriteString(fmt.Sprintf("%d %s", count, impact))
				first = false
			}
			builder.WriteString(")")
		}
	}

	return builder.String()
}

// formatTarget formats a target for display
func (f *CLIFormatter) formatTarget(target universal.Target) string {
	switch target.Type {
	case universal.TargetTypePod:
		if target.Namespace != "" && target.Namespace != "default" {
			return fmt.Sprintf("pod/%s/%s", target.Namespace, target.Name)
		}
		return fmt.Sprintf("pod/%s", target.Name)

	case universal.TargetTypeContainer:
		if target.Namespace != "" && target.Namespace != "default" {
			return fmt.Sprintf("container/%s/%s/%s", target.Namespace, target.Name, target.Container)
		}
		return fmt.Sprintf("container/%s/%s", target.Name, target.Container)

	case universal.TargetTypeNode:
		return fmt.Sprintf("node/%s", target.Name)

	case universal.TargetTypeProcess:
		if target.PID > 0 {
			return fmt.Sprintf("process/%s[%d]", target.Name, target.PID)
		}
		return fmt.Sprintf("process/%s", target.Name)

	default:
		return target.Name
	}
}

// getLevelIndicator returns a visual indicator for event level
func (f *CLIFormatter) getLevelIndicator(level universal.EventLevel) string {
	if !f.useColor {
		return fmt.Sprintf("[%s]", strings.ToUpper(string(level)))
	}

	// Use color codes
	switch level {
	case universal.EventLevelCritical:
		return "\033[31m[CRITICAL]\033[0m" // Red
	case universal.EventLevelError:
		return "\033[31m[ERROR]\033[0m" // Red
	case universal.EventLevelWarning:
		return "\033[33m[WARNING]\033[0m" // Yellow
	case universal.EventLevelInfo:
		return "\033[36m[INFO]\033[0m" // Cyan
	case universal.EventLevelDebug:
		return "\033[90m[DEBUG]\033[0m" // Gray
	default:
		return "[UNKNOWN]"
	}
}

// getImpactIndicator returns a visual indicator for impact level
func (f *CLIFormatter) getImpactIndicator(impact universal.ImpactLevel) string {
	if !f.useColor {
		return fmt.Sprintf("[%s]", strings.ToUpper(string(impact)))
	}

	// Use color codes with symbols
	switch impact {
	case universal.ImpactLevelCritical:
		return "\033[31m⚠️  [CRITICAL]\033[0m" // Red with warning
	case universal.ImpactLevelHigh:
		return "\033[91m[HIGH]\033[0m" // Light red
	case universal.ImpactLevelMedium:
		return "\033[33m[MEDIUM]\033[0m" // Yellow
	case universal.ImpactLevelLow:
		return "\033[36m[LOW]\033[0m" // Cyan
	default:
		return "[UNKNOWN]"
	}
}

// formatDuration formats a duration in human-readable form
func (f *CLIFormatter) formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0f seconds", d.Seconds())
	} else if d < time.Hour {
		return fmt.Sprintf("%.1f minutes", d.Minutes())
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%.1f hours", d.Hours())
	} else {
		return fmt.Sprintf("%.1f days", d.Hours()/24)
	}
}

// TableFormatter formats data in table format
type TableFormatter struct {
	*CLIFormatter
}

// NewTableFormatter creates a table formatter
func NewTableFormatter(config *CLIConfig) *TableFormatter {
	return &TableFormatter{
		CLIFormatter: NewCLIFormatter(config),
	}
}

// FormatMetricsTable formats metrics in a table
func (tf *TableFormatter) FormatMetricsTable(metrics []*universal.UniversalMetric) string {
	if len(metrics) == 0 {
		return "No metrics available"
	}

	var builder strings.Builder

	// Header
	builder.WriteString("Target                          | Metric                  | Value        | Unit    | Confidence\n")
	builder.WriteString("--------------------------------|-------------------------|--------------|---------|----------\n")

	// Rows
	for _, metric := range metrics {
		target := tf.formatTarget(metric.Target)
		if len(target) > 30 {
			target = target[:27] + "..."
		}

		name := metric.Name
		if len(name) > 23 {
			name = name[:20] + "..."
		}

		builder.WriteString(fmt.Sprintf("%-30s | %-23s | %12.2f | %-7s | %.1f\n",
			target, name, metric.Value, metric.Unit, metric.Quality.Confidence))
	}

	return builder.String()
}
