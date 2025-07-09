package correlation

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// OutputFormat defines the output format type
type OutputFormat int

const (
	// FormatHuman formats output for human readability
	FormatHuman OutputFormat = iota
	// FormatJSON formats output as JSON
	FormatJSON
	// FormatYAML formats output as YAML
	FormatYAML
	// FormatTable formats output as a table
	FormatTable
)

// FormatterConfig configures the output formatter
type FormatterConfig struct {
	Format        OutputFormat `json:"format"`
	ShowEvidence  bool         `json:"show_evidence"`
	ShowMetadata  bool         `json:"show_metadata"`
	ShowTimestamp bool         `json:"show_timestamp"`
	MaxWidth      int          `json:"max_width"`
	UseColors     bool         `json:"use_colors"`
	Verbose       bool         `json:"verbose"`
}

// DefaultFormatterConfig returns default formatter configuration
func DefaultFormatterConfig() FormatterConfig {
	return FormatterConfig{
		Format:        FormatHuman,
		ShowEvidence:  true,
		ShowMetadata:  false,
		ShowTimestamp: true,
		MaxWidth:      80,
		UseColors:     true,
		Verbose:       false,
	}
}

// Formatter handles output formatting for findings
type Formatter struct {
	config FormatterConfig
}

// NewFormatter creates a new formatter with the given configuration
func NewFormatter(config FormatterConfig) *Formatter {
	return &Formatter{
		config: config,
	}
}

// FormatFindings formats a slice of findings according to the configured format
func (f *Formatter) FormatFindings(findings []Finding) string {
	if len(findings) == 0 {
		return f.formatNoFindings()
	}

	switch f.config.Format {
	case FormatHuman:
		return f.formatHuman(findings)
	case FormatTable:
		return f.formatTable(findings)
	default:
		return f.formatHuman(findings)
	}
}

// FormatEngineMetrics formats engine metrics
func (f *Formatter) FormatEngineMetrics(metrics EngineMetrics) string {
	var sb strings.Builder

	if f.config.UseColors {
		sb.WriteString("\033[1;36m📊 Engine Metrics\033[0m\n")
	} else {
		sb.WriteString("📊 Engine Metrics\n")
	}

	sb.WriteString("================\n")
	sb.WriteString(fmt.Sprintf("Total Executions: %d\n", metrics.TotalExecutions))
	sb.WriteString(fmt.Sprintf("Successful: %d\n", metrics.SuccessfulExecutions))
	sb.WriteString(fmt.Sprintf("Failed: %d\n", metrics.FailedExecutions))
	sb.WriteString(fmt.Sprintf("Total Findings: %d\n", metrics.TotalFindings))
	sb.WriteString(fmt.Sprintf("Average Execution Time: %v\n", metrics.AverageExecutionTime))
	sb.WriteString(fmt.Sprintf("Last Execution: %v\n", metrics.LastExecutionTime.Format(time.RFC3339)))

	if f.config.Verbose && len(metrics.RuleMetrics) > 0 {
		sb.WriteString("\n📋 Rule Metrics:\n")
		for ruleID, ruleMetrics := range metrics.RuleMetrics {
			sb.WriteString(fmt.Sprintf("  %s:\n", ruleID))
			sb.WriteString(fmt.Sprintf("    Executions: %d\n", ruleMetrics.ExecutionCount))
			sb.WriteString(fmt.Sprintf("    Success Rate: %.1f%%\n", float64(ruleMetrics.SuccessCount)/float64(ruleMetrics.ExecutionCount)*100))
			sb.WriteString(fmt.Sprintf("    Avg Confidence: %.2f\n", ruleMetrics.AverageConfidence))
			sb.WriteString(fmt.Sprintf("    Avg Duration: %v\n", ruleMetrics.AverageExecutionTime))
		}
	}

	return sb.String()
}

// formatNoFindings returns a message when no findings are available
func (f *Formatter) formatNoFindings() string {
	if f.config.UseColors {
		return "\033[1;32m✅ No correlation findings detected\033[0m\n\nYour system appears to be operating normally based on the available data sources."
	}
	return "✅ No correlation findings detected\n\nYour system appears to be operating normally based on the available data sources."
}

// formatHuman formats findings in a human-readable format
func (f *Formatter) formatHuman(findings []Finding) string {
	var sb strings.Builder

	// Summary header
	sb.WriteString(f.formatSummaryHeader(findings))
	sb.WriteString("\n")

	// Group findings by severity
	severityGroups := f.groupBySeverity(findings)

	// Display findings in order of severity
	severities := []Severity{SeverityCritical, SeverityError, SeverityWarning, SeverityInfo}

	for _, severity := range severities {
		if severityFindings, exists := severityGroups[severity]; exists {
			sb.WriteString(f.formatSeverityGroup(severity, severityFindings))
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// formatTable formats findings as a table
func (f *Formatter) formatTable(findings []Finding) string {
	var sb strings.Builder

	// Table header
	sb.WriteString(f.formatTableHeader())
	sb.WriteString("\n")

	// Table separator
	sb.WriteString(strings.Repeat("-", f.config.MaxWidth))
	sb.WriteString("\n")

	// Table rows
	for _, finding := range findings {
		sb.WriteString(f.formatTableRow(finding))
		sb.WriteString("\n")
	}

	return sb.String()
}

// formatSummaryHeader creates a summary header for the findings
func (f *Formatter) formatSummaryHeader(findings []Finding) string {
	var sb strings.Builder

	if f.config.UseColors {
		sb.WriteString("\033[1;34m🔍 Correlation Analysis Results\033[0m\n")
	} else {
		sb.WriteString("🔍 Correlation Analysis Results\n")
	}

	sb.WriteString("==================================\n")

	if f.config.ShowTimestamp {
		sb.WriteString(fmt.Sprintf("Generated: %s\n", time.Now().Format(time.RFC3339)))
	}

	// Count findings by severity
	severityCounts := make(map[Severity]int)
	for _, finding := range findings {
		severityCounts[finding.Severity]++
	}

	sb.WriteString(fmt.Sprintf("Total Findings: %d\n", len(findings)))

	if count, exists := severityCounts[SeverityCritical]; exists && count > 0 {
		sb.WriteString(fmt.Sprintf("🚨 Critical: %d\n", count))
	}
	if count, exists := severityCounts[SeverityError]; exists && count > 0 {
		sb.WriteString(fmt.Sprintf("❌ Error: %d\n", count))
	}
	if count, exists := severityCounts[SeverityWarning]; exists && count > 0 {
		sb.WriteString(fmt.Sprintf("⚠️  Warning: %d\n", count))
	}
	if count, exists := severityCounts[SeverityInfo]; exists && count > 0 {
		sb.WriteString(fmt.Sprintf("ℹ️  Info: %d\n", count))
	}

	return sb.String()
}

// formatSeverityGroup formats a group of findings with the same severity
func (f *Formatter) formatSeverityGroup(severity Severity, findings []Finding) string {
	var sb strings.Builder

	// Section header
	sb.WriteString(f.formatSeverityHeader(severity))
	sb.WriteString("\n")

	// Sort findings by confidence (highest first)
	sort.Slice(findings, func(i, j int) bool {
		return findings[i].Confidence > findings[j].Confidence
	})

	// Format each finding
	for i, finding := range findings {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(f.formatFinding(finding))
	}

	return sb.String()
}

// formatSeverityHeader creates a header for a severity group
func (f *Formatter) formatSeverityHeader(severity Severity) string {
	var icon, colorCode string

	switch severity {
	case SeverityCritical:
		icon = "🚨"
		colorCode = "\033[1;31m" // Bold red
	case SeverityError:
		icon = "❌"
		colorCode = "\033[1;91m" // Bold light red
	case SeverityWarning:
		icon = "⚠️"
		colorCode = "\033[1;33m" // Bold yellow
	case SeverityInfo:
		icon = "ℹ️"
		colorCode = "\033[1;36m" // Bold cyan
	default:
		icon = "•"
		colorCode = "\033[0m"
	}

	header := fmt.Sprintf("%s %s Findings", icon, strings.ToUpper(severity.String()))

	if f.config.UseColors {
		return fmt.Sprintf("%s%s\033[0m", colorCode, header)
	}
	return header
}

// formatFinding formats a single finding
func (f *Formatter) formatFinding(finding Finding) string {
	var sb strings.Builder

	// Title and confidence
	titleLine := fmt.Sprintf("• %s", finding.Title)
	if f.config.UseColors {
		confidence := f.getConfidenceColor(finding.Confidence)
		titleLine = fmt.Sprintf("• %s %s(%.0f%% confidence)\033[0m", finding.Title, confidence, finding.Confidence*100)
	} else {
		titleLine = fmt.Sprintf("• %s (%.0f%% confidence)", finding.Title, finding.Confidence*100)
	}

	sb.WriteString(titleLine)
	sb.WriteString("\n")

	// Description
	if finding.Description != "" {
		sb.WriteString(f.wrapText(finding.Description, "  "))
		sb.WriteString("\n")
	}

	// Resource information
	if finding.Resource != nil {
		sb.WriteString(fmt.Sprintf("  Resource: %s/%s", finding.Resource.Kind, finding.Resource.Name))
		if finding.Resource.Namespace != "" {
			sb.WriteString(fmt.Sprintf(" (namespace: %s)", finding.Resource.Namespace))
		}
		sb.WriteString("\n")
	}

	// Prediction information
	if finding.Prediction != nil {
		sb.WriteString(f.formatPrediction(finding.Prediction))
	}

	// Evidence
	if f.config.ShowEvidence && len(finding.Evidence) > 0 {
		sb.WriteString("  Evidence:\n")
		for _, evidence := range finding.Evidence {
			sb.WriteString(fmt.Sprintf("    • %s (%.0f%% confidence)\n", evidence.Description, evidence.Confidence*100))
		}
	}

	// Tags
	if len(finding.Tags) > 0 {
		sb.WriteString(fmt.Sprintf("  Tags: %s\n", strings.Join(finding.Tags, ", ")))
	}

	// Metadata
	if f.config.ShowMetadata && len(finding.Metadata) > 0 {
		sb.WriteString("  Metadata:\n")
		for key, value := range finding.Metadata {
			sb.WriteString(fmt.Sprintf("    %s: %v\n", key, value))
		}
	}

	return sb.String()
}

// formatPrediction formats prediction information
func (f *Formatter) formatPrediction(prediction *Prediction) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("  🔮 Prediction: %s", prediction.Event))

	if prediction.TimeToEvent > 0 {
		sb.WriteString(fmt.Sprintf(" in %v", prediction.TimeToEvent))
	}

	sb.WriteString(fmt.Sprintf(" (%.0f%% confidence)\n", prediction.Confidence*100))

	if len(prediction.Factors) > 0 {
		sb.WriteString("  Contributing factors:\n")
		for _, factor := range prediction.Factors {
			sb.WriteString(fmt.Sprintf("    • %s\n", factor))
		}
	}

	if len(prediction.Mitigation) > 0 {
		sb.WriteString("  Recommended actions:\n")
		for _, action := range prediction.Mitigation {
			sb.WriteString(fmt.Sprintf("    • %s\n", action))
		}
	}

	return sb.String()
}

// formatTableHeader creates a table header
func (f *Formatter) formatTableHeader() string {
	return fmt.Sprintf("%-15s %-10s %-10s %-20s %s", "SEVERITY", "CONFIDENCE", "RULE", "RESOURCE", "TITLE")
}

// formatTableRow formats a single finding as a table row
func (f *Formatter) formatTableRow(finding Finding) string {
	resource := "N/A"
	if finding.Resource != nil {
		resource = fmt.Sprintf("%s/%s", finding.Resource.Kind, finding.Resource.Name)
	}

	title := finding.Title
	if len(title) > 30 {
		title = title[:27] + "..."
	}

	return fmt.Sprintf("%-15s %-10.0f%% %-10s %-20s %s",
		strings.ToUpper(finding.Severity.String()),
		finding.Confidence*100,
		finding.RuleID,
		resource,
		title,
	)
}

// groupBySeverity groups findings by their severity level
func (f *Formatter) groupBySeverity(findings []Finding) map[Severity][]Finding {
	groups := make(map[Severity][]Finding)

	for _, finding := range findings {
		groups[finding.Severity] = append(groups[finding.Severity], finding)
	}

	return groups
}

// getConfidenceColor returns the appropriate color code for confidence level
func (f *Formatter) getConfidenceColor(confidence float64) string {
	if !f.config.UseColors {
		return ""
	}

	switch {
	case confidence >= 0.9:
		return "\033[1;32m" // Bold green
	case confidence >= 0.7:
		return "\033[1;33m" // Bold yellow
	case confidence >= 0.5:
		return "\033[1;91m" // Bold light red
	default:
		return "\033[1;90m" // Bold gray
	}
}

// wrapText wraps text to fit within the specified width with prefix
func (f *Formatter) wrapText(text string, prefix string) string {
	if f.config.MaxWidth <= 0 {
		return prefix + text
	}

	maxLineWidth := f.config.MaxWidth - len(prefix)
	if maxLineWidth <= 0 {
		return prefix + text
	}

	words := strings.Fields(text)
	if len(words) == 0 {
		return prefix
	}

	var sb strings.Builder
	currentLine := prefix + words[0]

	for _, word := range words[1:] {
		if len(currentLine)+1+len(word) <= f.config.MaxWidth {
			currentLine += " " + word
		} else {
			sb.WriteString(currentLine)
			sb.WriteString("\n")
			currentLine = prefix + word
		}
	}

	sb.WriteString(currentLine)
	return sb.String()
}

// FormatSummary formats a brief summary of findings
func (f *Formatter) FormatSummary(findings []Finding) string {
	if len(findings) == 0 {
		return "No findings detected"
	}

	severityCounts := make(map[Severity]int)
	for _, finding := range findings {
		severityCounts[finding.Severity]++
	}

	var parts []string
	if count := severityCounts[SeverityCritical]; count > 0 {
		parts = append(parts, fmt.Sprintf("%d critical", count))
	}
	if count := severityCounts[SeverityError]; count > 0 {
		parts = append(parts, fmt.Sprintf("%d error", count))
	}
	if count := severityCounts[SeverityWarning]; count > 0 {
		parts = append(parts, fmt.Sprintf("%d warning", count))
	}
	if count := severityCounts[SeverityInfo]; count > 0 {
		parts = append(parts, fmt.Sprintf("%d info", count))
	}

	return fmt.Sprintf("Found %d findings: %s", len(findings), strings.Join(parts, ", "))
}
