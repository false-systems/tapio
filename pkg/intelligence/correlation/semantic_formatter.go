package correlation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/trace"
)

// ExplanationStyle defines the style of explanation
type ExplanationStyle int

const (
	StyleSimple ExplanationStyle = iota
	StyleDetailed
	StyleTechnical
	StyleExecutive
)

// Audience defines the target audience for explanations
type Audience int

const (
	AudienceDeveloper Audience = iota
	AudienceOperator
	AudienceManager
	AudienceExecutive
)

// HumanReadableFormatter formats insights for human consumption
type HumanReadableFormatter struct {
	style    ExplanationStyle
	audience Audience
	tracer   trace.Tracer
}

// NewHumanReadableFormatter creates a new formatter
func NewHumanReadableFormatter(style ExplanationStyle, audience Audience) *HumanReadableFormatter {
	return &HumanReadableFormatter{
		style:    style,
		audience: audience,
		tracer:   nil, // Can be set later if needed
	}
}

// HumanReadableExplanation contains formatted explanation of an insight
type HumanReadableExplanation struct {
	Title           string
	Summary         string
	Details         []string
	Impact          string
	Recommendation  string
	Timeline        []string
	TechnicalNotes  []string
	BusinessContext string
	NextSteps       []string
	Confidence      string
	Evidence        []string
}

// extractCorrelationData extracts data from various insight types
func extractCorrelationData(insight Insight) (relatedEvents []string, resources []AffectedResource, actions []ActionableItem, prediction *Prediction) {
	// Since Insight is domain.Insight struct, extract from metadata
	if insight.Metadata != nil {
		if events, ok := insight.Metadata["related_events"].([]string); ok {
			relatedEvents = events
		}
	}

	return
}

// FormatInsight formats an insight into human-readable explanation
func (hrf *HumanReadableFormatter) FormatInsight(insight Insight) *HumanReadableExplanation {
	if hrf.tracer != nil {
		_, span := hrf.tracer.Start(context.Background(), "formatter.format_insight")
		defer span.End()
	}

	explanation := &HumanReadableExplanation{
		Title:          insight.Title,
		Summary:        insight.Description,
		Details:        []string{},
		Timeline:       []string{},
		TechnicalNotes: []string{},
		NextSteps:      []string{},
		Evidence:       []string{},
	}

	// Extract correlation-specific data
	relatedEvents, resources, actions, prediction := extractCorrelationData(insight)

	// Set confidence based on insight metadata
	if insight.Metadata != nil {
		if conf, ok := insight.Metadata["confidence"].(float64); ok {
			explanation.Confidence = hrf.formatConfidence(conf)
		}
	}

	// Format based on style
	ctx := context.Background()
	switch hrf.style {
	case StyleSimple:
		hrf.formatSimple(ctx, insight, explanation, relatedEvents, resources, actions, prediction)
	case StyleDetailed:
		hrf.formatDetailed(ctx, insight, explanation, relatedEvents, resources, actions, prediction)
	case StyleTechnical:
		hrf.formatTechnical(ctx, insight, explanation, relatedEvents, resources, actions, prediction)
	case StyleExecutive:
		hrf.formatExecutive(ctx, insight, explanation, relatedEvents, resources, actions, prediction)
	}

	return explanation
}

// formatSimple creates a simple, concise explanation
func (hrf *HumanReadableFormatter) formatSimple(ctx context.Context, insight Insight, explanation *HumanReadableExplanation, relatedEvents []string, resources []AffectedResource, actions []ActionableItem, prediction *Prediction) {
	// Simple impact statement
	explanation.Impact = hrf.createSimpleImpact(insight, resources)

	// Simple recommendation
	if len(actions) > 0 {
		explanation.Recommendation = actions[0].Title
	} else {
		explanation.Recommendation = "Monitor the situation and investigate if it persists"
	}

	// Basic timeline
	if len(relatedEvents) > 0 {
		explanation.Timeline = append(explanation.Timeline,
			fmt.Sprintf("Issue started at %s", insight.Timestamp.Format("15:04:05")))
		explanation.Timeline = append(explanation.Timeline,
			fmt.Sprintf("Affecting %d events", len(relatedEvents)))
	}
}

// formatDetailed creates a detailed explanation with more context
func (hrf *HumanReadableFormatter) formatDetailed(ctx context.Context, insight Insight, explanation *HumanReadableExplanation, relatedEvents []string, resources []AffectedResource, actions []ActionableItem, prediction *Prediction) {
	// Detailed description
	explanation.Details = append(explanation.Details, "What happened:")
	explanation.Details = append(explanation.Details, fmt.Sprintf("- %s", insight.Description))

	// Add severity information
	explanation.Details = append(explanation.Details, fmt.Sprintf("- Severity: %s", insight.Severity))

	// Resource information
	if len(resources) > 0 {
		explanation.Details = append(explanation.Details, "\nAffected resources:")
		for _, resource := range resources {
			explanation.Details = append(explanation.Details,
				fmt.Sprintf("- %s: %s", resource.Type, resource.Name))
		}
	}

	// Impact analysis
	hrf.formatImpact(ctx, insight, explanation, prediction)

	// Recommendations
	if len(actions) > 0 {
		explanation.Details = append(explanation.Details, "\nRecommended actions:")
		for i, action := range actions {
			explanation.Details = append(explanation.Details,
				fmt.Sprintf("%d. %s", i+1, action.Title))
			if action.Description != "" {
				explanation.Details = append(explanation.Details,
					fmt.Sprintf("   %s", action.Description))
			}
		}
	}

	// Timeline
	hrf.createTimeline(ctx, insight, explanation, relatedEvents)
}

// formatTechnical creates a technical explanation for developers/operators
func (hrf *HumanReadableFormatter) formatTechnical(ctx context.Context, insight Insight, explanation *HumanReadableExplanation, relatedEvents []string, resources []AffectedResource, actions []ActionableItem, prediction *Prediction) {
	// Technical summary
	explanation.TechnicalNotes = append(explanation.TechnicalNotes,
		fmt.Sprintf("Event Type: %s", insight.Type))
	explanation.TechnicalNotes = append(explanation.TechnicalNotes,
		fmt.Sprintf("Correlation ID: %s", insight.ID))

	// Add metadata
	if insight.Metadata != nil {
		explanation.TechnicalNotes = append(explanation.TechnicalNotes, "\nMetadata:")
		for key, value := range insight.Metadata {
			explanation.TechnicalNotes = append(explanation.TechnicalNotes,
				fmt.Sprintf("  %s: %v", key, value))
		}
	}

	// Detailed event information
	if len(relatedEvents) > 0 {
		explanation.TechnicalNotes = append(explanation.TechnicalNotes,
			fmt.Sprintf("\nRelated Events (%d):", len(relatedEvents)))
		for i, eventID := range relatedEvents {
			if i < 5 { // Limit to first 5
				explanation.TechnicalNotes = append(explanation.TechnicalNotes,
					fmt.Sprintf("  - %s", eventID))
			}
		}
		if len(relatedEvents) > 5 {
			explanation.TechnicalNotes = append(explanation.TechnicalNotes,
				fmt.Sprintf("  ... and %d more", len(relatedEvents)-5))
		}
	}

	// Technical actions
	if len(actions) > 0 {
		explanation.TechnicalNotes = append(explanation.TechnicalNotes, "\nTechnical Actions:")
		for _, action := range actions {
			explanation.TechnicalNotes = append(explanation.TechnicalNotes,
				fmt.Sprintf("- %s", action.Title))
			if len(action.Commands) > 0 {
				explanation.TechnicalNotes = append(explanation.TechnicalNotes, "  Commands:")
				for _, cmd := range action.Commands {
					explanation.TechnicalNotes = append(explanation.TechnicalNotes,
						fmt.Sprintf("    $ %s", cmd))
				}
			}
		}
	}

	// Add remediation details
	hrf.formatRemediation(ctx, insight, explanation, actions, prediction)
}

// formatExecutive creates an executive-level explanation
func (hrf *HumanReadableFormatter) formatExecutive(ctx context.Context, insight Insight, explanation *HumanReadableExplanation, relatedEvents []string, resources []AffectedResource, actions []ActionableItem, prediction *Prediction) {
	// Business impact focus
	explanation.BusinessContext = hrf.createBusinessContext(insight, resources)

	// Executive summary
	severity := insight.Severity
	impactLevel := "low"
	if severity == domain.SeverityCritical {
		impactLevel = "critical"
	} else if severity == domain.SeverityHigh {
		impactLevel = "high"
	}

	explanation.Summary = fmt.Sprintf(
		"A %s impact issue has been detected affecting %d resources. %s",
		impactLevel,
		len(resources),
		hrf.createBusinessImpactStatement(resources),
	)

	// High-level recommendations
	explanation.NextSteps = []string{
		"Engineering team has been notified",
		"Automated remediation is in progress",
		"Full resolution expected within 30 minutes",
	}

	// Risk assessment
	if prediction != nil && prediction.Probability > 0.7 {
		explanation.NextSteps = append(explanation.NextSteps,
			fmt.Sprintf("Risk of recurrence: %.0f%%", prediction.Probability*100))
	}
}

// formatImpact formats the impact section
func (hrf *HumanReadableFormatter) formatImpact(ctx context.Context, insight Insight, explanation *HumanReadableExplanation, prediction *Prediction) {
	impact := []string{}

	// Severity-based impact
	switch insight.Severity {
	case domain.SeverityCritical:
		impact = append(impact, "Critical: Immediate action required")
		impact = append(impact, "Service availability is severely impacted")
	case domain.SeverityHigh:
		impact = append(impact, "High: Significant degradation detected")
		impact = append(impact, "User experience may be affected")
	case domain.SeverityMedium:
		impact = append(impact, "Medium: Potential issues detected")
		impact = append(impact, "Monitoring closely for escalation")
	default:
		impact = append(impact, "Low: Minor issue detected")
		impact = append(impact, "No immediate action required")
	}

	// Prediction-based impact
	if prediction != nil {
		if prediction.Probability > 0.8 {
			impact = append(impact,
				fmt.Sprintf("High likelihood (%.0f%%) of: %s",
					prediction.Probability*100, prediction.Scenario))
		}
		if prediction.TimeWindow > 0 {
			impact = append(impact,
				fmt.Sprintf("Expected within: %s", hrf.formatDuration(prediction.TimeWindow)))
		}
	}

	explanation.Impact = strings.Join(impact, ". ")
}

// formatRemediation formats remediation steps
func (hrf *HumanReadableFormatter) formatRemediation(ctx context.Context, insight Insight, explanation *HumanReadableExplanation, actions []ActionableItem, prediction *Prediction) {
	remediation := []string{}

	// Immediate steps
	remediation = append(remediation, "Immediate steps:")
	if len(actions) > 0 {
		for i, action := range actions {
			if i < 3 { // Limit to top 3 immediate actions
				remediation = append(remediation, fmt.Sprintf("%d. %s", i+1, action.Title))
			}
		}
	} else {
		remediation = append(remediation, "1. Review system logs")
		remediation = append(remediation, "2. Check resource utilization")
		remediation = append(remediation, "3. Verify service dependencies")
	}

	// Preventive measures
	if prediction != nil && len(prediction.Actions) > 0 {
		remediation = append(remediation, "\nPreventive measures:")
		for i, action := range prediction.Actions {
			remediation = append(remediation, fmt.Sprintf("- %s", action.Description))
			if i >= 2 {
				break
			}
		}
	}

	explanation.Recommendation = strings.Join(remediation, "\n")
}

// createTimeline creates an event timeline
func (hrf *HumanReadableFormatter) createTimeline(ctx context.Context, insight Insight, explanation *HumanReadableExplanation, relatedEvents []string) {
	timeline := []string{}

	baseTime := insight.Timestamp
	timeline = append(timeline, fmt.Sprintf("T+0: %s - Issue detected",
		baseTime.Format("15:04:05")))

	// Add key milestones
	if len(relatedEvents) > 0 {
		timeline = append(timeline, fmt.Sprintf("T+0: %d related events identified",
			len(relatedEvents)))
	}

	// Add current status
	now := time.Now()
	elapsed := now.Sub(baseTime)
	timeline = append(timeline, fmt.Sprintf("T+%s: Current - Analyzing and responding",
		hrf.formatDuration(elapsed)))

	explanation.Timeline = timeline
}

// formatDuration formats a duration in human-readable form
func (hrf *HumanReadableFormatter) formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0f seconds", d.Seconds())
	} else if d < time.Hour {
		return fmt.Sprintf("%.0f minutes", d.Minutes())
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%.1f hours", d.Hours())
	}
	return fmt.Sprintf("%.1f days", d.Hours()/24)
}

// formatConfidence formats confidence level
func (hrf *HumanReadableFormatter) formatConfidence(conf float64) string {
	if conf > 0.9 {
		return "Very High"
	} else if conf > 0.7 {
		return "High"
	} else if conf > 0.5 {
		return "Medium"
	}
	return "Low"
}

// createSimpleImpact creates a simple impact statement
func (hrf *HumanReadableFormatter) createSimpleImpact(insight Insight, resources []AffectedResource) string {
	if len(resources) == 0 {
		return "No direct resource impact detected"
	}

	// Group resources by type
	typeCount := make(map[string]int)
	for _, res := range resources {
		typeCount[res.Type]++
	}

	// Create impact statement
	impacts := []string{}
	for resType, count := range typeCount {
		impacts = append(impacts, fmt.Sprintf("%d %s(s)", count, resType))
	}

	return fmt.Sprintf("Affecting %s", strings.Join(impacts, ", "))
}

// createBusinessContext creates business context explanation
func (hrf *HumanReadableFormatter) createBusinessContext(insight Insight, resources []AffectedResource) string {
	context := []string{}

	// Identify critical resources
	criticalCount := 0
	for _, res := range resources {
		if hrf.isBusinessCritical(res) {
			criticalCount++
		}
	}

	if criticalCount > 0 {
		context = append(context, fmt.Sprintf("%d business-critical resources affected",
			criticalCount))
	}

	// Add revenue impact if available
	if insight.Metadata != nil {
		if impact, ok := insight.Metadata["revenue_impact"].(float64); ok && impact > 0 {
			context = append(context, fmt.Sprintf("Potential revenue impact: $%.0f/hour",
				impact))
		}
	}

	if len(context) == 0 {
		return "Limited business impact detected"
	}

	return strings.Join(context, ". ")
}

// createBusinessImpactStatement creates a business impact statement
func (hrf *HumanReadableFormatter) createBusinessImpactStatement(resources []AffectedResource) string {
	// Check for critical services
	criticalServices := []string{}
	for _, res := range resources {
		if res.Type == "service" && hrf.isBusinessCritical(res) {
			criticalServices = append(criticalServices, res.Name)
		}
	}

	if len(criticalServices) > 0 {
		return fmt.Sprintf("Critical services affected: %s",
			strings.Join(criticalServices, ", "))
	}

	return "Infrastructure components affected"
}

// isBusinessCritical checks if a resource is business critical
func (hrf *HumanReadableFormatter) isBusinessCritical(resource AffectedResource) bool {
	criticalServices := map[string]bool{
		"payment-service": true,
		"auth-service":    true,
		"order-service":   true,
		"api-gateway":     true,
	}

	if resource.Type == "service" {
		return criticalServices[resource.Name]
	}

	// Critical infrastructure
	if resource.Type == "database" || resource.Type == "load-balancer" {
		return true
	}

	return false
}

// AffectedResource represents a resource affected by an issue
type AffectedResource struct {
	Type        string
	Name        string
	Impact      string
	Status      string
	LastUpdated time.Time
	Metadata    map[string]interface{}
}

// Prediction represents a predicted outcome
type Prediction struct {
	Scenario    string
	Probability float64
	TimeWindow  time.Duration
	Impact      string
	Actions     []ActionItem
}

// ActionItem represents a single action item
type ActionItem struct {
	ID          string
	Type        string
	Description string
	Priority    string
	Command     string
	Risk        string
}

// ActionableItem represents a recommended action
type ActionableItem struct {
	Title            string
	Description      string
	Commands         []string
	Risk             string
	EstimatedImpact  string
	EstimatedTime    time.Duration
	AutomationLevel  string
	RequiredApproval bool
}

// Note: CorrelationInsight is already defined in insight_types.go
