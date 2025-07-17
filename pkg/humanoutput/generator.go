package humanoutput

import (
	"context"
	"fmt"
	"strings"
	"time"
	
	"github.com/yairfalse/tapio/pkg/domain"
)

// Generator is the main human output generator
type Generator struct {
	config          *Config
	templateManager *TemplateManager
}

// NewGenerator creates a new human output generator
func NewGenerator(config *Config) *Generator {
	if config == nil {
		config = DefaultConfig()
	}
	
	return &Generator{
		config:          config,
		templateManager: NewTemplateManager(config),
	}
}

// GenerateInsight converts a finding into human-readable text
func (g *Generator) GenerateInsight(ctx context.Context, finding *domain.Finding) (*HumanInsight, error) {
	if finding == nil {
		return nil, fmt.Errorf("finding is nil")
	}
	
	insight := &HumanInsight{
		Title:       finding.Title,
		Severity:    string(finding.Severity),
		Language:    g.config.DefaultLanguage,
		Style:       g.config.ExplanationStyle,
		Audience:    g.config.Audience,
		GeneratedAt: time.Now(),
		GeneratedBy: "template",
		Confidence:  0.8,
	}
	
	// Extract variables from finding
	variables := g.extractFindingVariables(finding)
	
	// Find best matching template
	template := g.templateManager.FindBestTemplate(string(finding.Type), string(finding.Severity), g.config.Audience)
	if template != nil {
		insight.TemplateUsed = template.ID
		insight.WhatHappened = FillTemplate(template.WhatHappenedTemplate, variables)
		insight.WhyItHappened = FillTemplate(template.WhyItHappenedTemplate, variables)
		insight.WhatItMeans = FillTemplate(template.WhatItMeansTemplate, variables)
		insight.WhatToDo = FillTemplate(template.WhatToDoTemplate, variables)
		insight.HowToPrevent = FillTemplate(template.HowToPreventTemplate, variables)
		
		if template.BusinessImpactTemplate != "" {
			insight.BusinessImpact = FillTemplate(template.BusinessImpactTemplate, variables)
		}
		if template.UserImpactTemplate != "" {
			insight.UserImpact = FillTemplate(template.UserImpactTemplate, variables)
		}
		
		insight.Confidence = template.MinConfidence
	} else {
		// Fallback to generic explanation
		insight = g.generateGenericFindingExplanation(finding)
	}
	
	// Add context and recommendations
	if g.config.IncludeContext {
		g.enhanceWithContext(insight, finding)
	}
	
	if g.config.IncludeRecommendations {
		g.addRecommendations(insight, finding)
	}
	
	// Add emoji if enabled
	if g.config.IncludeEmoji {
		insight.Emoji = g.getEmojiForSeverity(finding.Severity)
	}
	
	// Perform quality checks
	if g.config.EnableQualityCheck {
		g.performQualityCheck(insight)
	}
	
	return insight, nil
}

// GenerateEventExplanation converts an event into human-readable text
func (g *Generator) GenerateEventExplanation(ctx context.Context, event *domain.Event) (*HumanInsight, error) {
	if event == nil {
		return nil, fmt.Errorf("event is nil")
	}
	
	insight := &HumanInsight{
		Title:       fmt.Sprintf("%s Event in %s", event.Type, event.Context.Namespace),
		Severity:    string(event.Severity),
		Language:    g.config.DefaultLanguage,
		Style:       g.config.ExplanationStyle,
		Audience:    g.config.Audience,
		GeneratedAt: time.Now(),
		GeneratedBy: "template",
		Confidence:  0.7,
	}
	
	// Extract variables from event
	variables := g.extractEventVariables(event)
	
	// Find best matching template
	template := g.templateManager.FindBestTemplate(string(event.Type), string(event.Severity), g.config.Audience)
	if template != nil {
		insight.TemplateUsed = template.ID
		insight.WhatHappened = FillTemplate(template.WhatHappenedTemplate, variables)
		insight.WhyItHappened = FillTemplate(template.WhyItHappenedTemplate, variables)
		insight.WhatItMeans = FillTemplate(template.WhatItMeansTemplate, variables)
		insight.WhatToDo = FillTemplate(template.WhatToDoTemplate, variables)
		insight.HowToPrevent = FillTemplate(template.HowToPreventTemplate, variables)
		
		if template.BusinessImpactTemplate != "" {
			insight.BusinessImpact = FillTemplate(template.BusinessImpactTemplate, variables)
		}
		if template.UserImpactTemplate != "" {
			insight.UserImpact = FillTemplate(template.UserImpactTemplate, variables)
		}
		
		insight.Confidence = template.MinConfidence
	} else {
		// Fallback to generic explanation
		insight = g.generateGenericEventExplanation(event)
	}
	
	// Add timeline
	insight.Timeline = fmt.Sprintf("Detected at %s", event.Timestamp.Format("2006-01-02 15:04:05"))
	
	// Add context and recommendations
	if g.config.IncludeContext {
		g.enhanceEventWithContext(insight, event)
	}
	
	if g.config.IncludeCommands {
		g.addEventCommands(insight, event)
	}
	
	// Add emoji if enabled
	if g.config.IncludeEmoji {
		insight.Emoji = g.getEmojiForSeverity(event.Severity)
	}
	
	// Perform quality checks
	if g.config.EnableQualityCheck {
		g.performQualityCheck(insight)
	}
	
	return insight, nil
}

// GenerateReport creates a human-readable report from multiple findings
func (g *Generator) GenerateReport(ctx context.Context, findings []*domain.Finding) (*HumanReport, error) {
	if len(findings) == 0 {
		return nil, fmt.Errorf("no findings provided")
	}
	
	report := &HumanReport{
		Title:       "System Health Report",
		GeneratedAt: time.Now(),
		Insights:    make([]*HumanInsight, 0, len(findings)),
	}
	
	// Generate insights for each finding
	for _, finding := range findings {
		insight, err := g.GenerateInsight(ctx, finding)
		if err != nil {
			continue // Skip failed insights
		}
		report.Insights = append(report.Insights, insight)
	}
	
	// Generate summary
	report.Summary = g.generateReportSummary(report.Insights)
	
	// Identify trends
	report.Trends = g.identifyTrends(findings)
	
	// Generate recommendations
	report.Recommendations = g.generateReportRecommendations(report.Insights)
	
	// Assess overall health
	report.OverallHealth = g.assessOverallHealth(report.Insights)
	
	// Calculate read time
	report.EstimatedReadTime = g.calculateReportReadTime(report)
	
	return report, nil
}

// GenerateSummary creates a summary of system state from events
func (g *Generator) GenerateSummary(ctx context.Context, events []*domain.Event) (*HumanSummary, error) {
	if len(events) == 0 {
		return nil, fmt.Errorf("no events provided")
	}
	
	summary := &HumanSummary{
		Title:        "System State Summary",
		GeneratedAt:  time.Now(),
		KeyMetrics:   make(map[string]string),
		ActiveIssues: make([]IssueSummary, 0),
	}
	
	// Analyze events
	eventsByCategory := g.groupEventsByCategory(events)
	eventsBySeverity := g.groupEventsBySeverity(events)
	
	// Generate overview
	summary.Overview = g.generateSummaryOverview(events, eventsByCategory, eventsBySeverity)
	
	// Extract key metrics
	summary.KeyMetrics = g.extractKeyMetrics(events)
	
	// Identify active issues
	summary.ActiveIssues = g.identifyActiveIssues(events)
	
	// Assess system health
	summary.SystemHealth = g.assessSystemHealth(events)
	
	// Generate next steps
	summary.NextSteps = g.generateNextSteps(summary.ActiveIssues)
	
	return summary, nil
}

// Helper methods for variable extraction
func (g *Generator) extractFindingVariables(finding *domain.Finding) map[string]string {
	variables := make(map[string]string)
	
	variables["type"] = string(finding.Type)
	variables["severity"] = string(finding.Severity)
	variables["title"] = finding.Title
	variables["timestamp"] = finding.Timestamp.Format("2006-01-02 15:04:05")
	
	// Extract from evidence if available
	if len(finding.Evidence) > 0 && finding.Evidence[0].Data != nil {
		if dataMap, ok := finding.Evidence[0].Data.(map[string]interface{}); ok {
			for k, v := range dataMap {
				variables[k] = fmt.Sprintf("%v", v)
			}
		}
	}
	
	// Add common computed values
	variables["time_ago"] = g.formatTimeAgo(finding.Timestamp)
	
	return variables
}

func (g *Generator) extractEventVariables(event *domain.Event) map[string]string {
	variables := make(map[string]string)
	
	variables["event_id"] = string(event.ID)
	variables["category"] = string(event.Type)
	variables["severity"] = string(event.Severity)
	variables["timestamp"] = event.Timestamp.Format("2006-01-02 15:04:05")
	
	// Extract context
	if event.Context.Namespace != "" {
		variables["namespace"] = event.Context.Namespace
	}
	if event.Context.Container != "" {
		variables["container"] = event.Context.Container
	}
	if event.Context.Host != "" {
		variables["host"] = event.Context.Host
	}
	// Extract from resource if available
	if event.Context.Resource != nil {
		variables["pod"] = event.Context.Resource.Name
		variables["resource_kind"] = event.Context.Resource.Kind
		variables["resource_namespace"] = event.Context.Resource.Namespace
	}
	
	// Extract metadata annotations if available
	if event.Metadata.Annotations != nil {
		for k, v := range event.Metadata.Annotations {
			variables["annotation_"+k] = v
		}
	}
	
	// Extract from payload if it's a map
	if event.Payload != nil {
		// Try to extract data from payload if it supports it
		// This is a simplified approach - you may need to type-switch on specific payload types
	}
	
	// Add common computed values
	variables["time_ago"] = g.formatTimeAgo(event.Timestamp)
	
	return variables
}

// Generic explanation generators
func (g *Generator) generateGenericFindingExplanation(finding *domain.Finding) *HumanInsight {
	insight := &HumanInsight{
		Title:          finding.Title,
		WhatHappened:   finding.Description,
		WhyItHappened:  "The system detected an issue based on monitoring data and correlation analysis",
		WhatItMeans:    g.generateGenericImpact(finding.Severity),
		WhatToDo:       "Review the finding details and take appropriate action based on severity",
		HowToPrevent:   "Monitor system metrics and set up alerts for early detection",
		Severity:       string(finding.Severity),
		GeneratedBy:    "generic",
		GeneratedAt:    time.Now(),
		Confidence:     0.5,
	}
	
	// Set urgency based on severity
	switch finding.Severity {
	case domain.SeverityCritical:
		insight.IsUrgent = true
		insight.RequiresEscalation = true
	case domain.SeverityError:
		insight.IsUrgent = true
	case domain.SeverityWarn:
		insight.IsActionable = true
	}
	
	return insight
}

func (g *Generator) generateGenericEventExplanation(event *domain.Event) *HumanInsight {
	insight := &HumanInsight{
		Title:          fmt.Sprintf("%s Event", event.Type),
		WhatHappened:   fmt.Sprintf("A %s event occurred in the system", event.Type),
		WhyItHappened:  "The system detected a condition that triggered this event",
		WhatItMeans:    g.generateGenericImpact(event.Severity),
		WhatToDo:       "Review the event details and context to determine appropriate action",
		HowToPrevent:   "Monitor for patterns and implement preventive measures",
		Severity:       string(event.Severity),
		GeneratedBy:    "generic",
		GeneratedAt:    time.Now(),
		Confidence:     0.5,
	}
	
	// Add context if available
	if event.Context.Namespace != "" {
		insight.WhatHappened = fmt.Sprintf("%s in namespace %s", insight.WhatHappened, event.Context.Namespace)
	}
	
	return insight
}

func (g *Generator) generateGenericImpact(severity domain.Severity) string {
	switch severity {
	case domain.SeverityCritical:
		return "This is a critical issue that requires immediate attention to prevent service disruption"
	case domain.SeverityError:
		return "This is a high-priority issue that could impact service availability or performance"
	case domain.SeverityWarn:
		return "This issue should be addressed to maintain optimal system performance"
	case domain.SeverityInfo:
		return "This is a minor issue that should be monitored but poses minimal risk"
	default:
		return "The impact of this issue should be evaluated based on your system requirements"
	}
}

// Enhancement methods
func (g *Generator) enhanceWithContext(insight *HumanInsight, finding *domain.Finding) {
	// Add business impact based on severity and insights
	if insight.BusinessImpact == "" {
		switch finding.Severity {
		case domain.SeverityCritical:
			insight.BusinessImpact = "Critical business impact - service availability at risk"
		case domain.SeverityError:
			insight.BusinessImpact = "High business impact - user experience degraded"
		case domain.SeverityWarn:
			insight.BusinessImpact = "Moderate business impact - performance affected"
		default:
			insight.BusinessImpact = "Low business impact - minimal user effect"
		}
	}
	
	// Add technical details from evidence
	if len(finding.Evidence) > 0 && insight.TechnicalDetails == "" {
		details := make([]string, 0, len(finding.Evidence))
		for _, evidence := range finding.Evidence {
			details = append(details, evidence.Description)
		}
		insight.TechnicalDetails = strings.Join(details, "; ")
	}
}

func (g *Generator) enhanceEventWithContext(insight *HumanInsight, event *domain.Event) {
	// Add impact based on severity
	switch event.Severity {
	case domain.SeverityCritical:
		insight.BusinessImpact = "Critical impact - immediate action required"
		insight.UserImpact = "Users experiencing service disruption"
	case domain.SeverityError:
		insight.BusinessImpact = "High impact - service degradation occurring"
		insight.UserImpact = "Some users may experience issues"
	case domain.SeverityWarn:
		insight.BusinessImpact = "Moderate impact - potential issues detected"
	}
}

func (g *Generator) addRecommendations(insight *HumanInsight, finding *domain.Finding) {
	actions := make([]RecommendedAction, 0)
	
	// Add type-specific recommendations
	if strings.Contains(string(finding.Type), "memory") {
		actions = append(actions, RecommendedAction{
			Title:       "Analyze Memory Usage",
			Description: "Use memory profiling tools to identify memory leaks",
			Type:        "documentation",
			Priority:    "high",
		})
	}
	
	if strings.Contains(string(finding.Type), "network") {
		actions = append(actions, RecommendedAction{
			Title:       "Check Network Configuration",
			Description: "Review network policies and service endpoints",
			Type:        "configuration",
			Priority:    "high",
		})
	}
	
	if strings.Contains(string(finding.Type), "performance") {
		actions = append(actions, RecommendedAction{
			Title:       "Performance Analysis",
			Description: "Run performance profiling to identify bottlenecks",
			Type:        "documentation",
			Priority:    "medium",
		})
	}
	
	insight.RecommendedActions = actions
}

func (g *Generator) addEventCommands(insight *HumanInsight, event *domain.Event) {
	commands := make([]string, 0)
	
	// Add context-specific commands
	if event.Context.Namespace != "" && event.Context.Resource != nil && event.Context.Resource.Name != "" {
		commands = append(commands, 
			fmt.Sprintf("kubectl describe %s %s -n %s", 
				strings.ToLower(event.Context.Resource.Kind), 
				event.Context.Resource.Name, 
				event.Context.Namespace),
			fmt.Sprintf("kubectl logs %s -n %s", event.Context.Resource.Name, event.Context.Namespace),
		)
	}
	
	// Add type-specific commands
	switch event.Type {
	case domain.EventTypeSystem:
		commands = append(commands, "kubectl top pods", "kubectl top nodes")
	case domain.EventTypeNetwork:
		commands = append(commands, "kubectl get networkpolicies", "kubectl get services")
	case domain.EventTypeMemory, domain.EventTypeCPU:
		commands = append(commands, "kubectl top pods --sort-by=memory", "kubectl describe nodes")
	}
	
	insight.Commands = commands
}

// Quality check methods
func (g *Generator) performQualityCheck(insight *HumanInsight) {
	// Calculate readability score
	insight.ReadabilityScore = g.calculateReadabilityScore(insight)
	
	// Calculate complexity score
	insight.ComplexityScore = g.calculateComplexityScore(insight)
	
	// Determine if urgent
	insight.IsUrgent = strings.Contains(strings.ToLower(insight.WhatHappened), "critical") ||
		strings.Contains(strings.ToLower(insight.WhatHappened), "failure") ||
		insight.Severity == "critical"
	
	// Determine if actionable
	insight.IsActionable = len(insight.Commands) > 0 || 
		len(insight.RecommendedActions) > 0 || 
		insight.WhatToDo != ""
	
	// Estimate read time
	wordCount := len(strings.Fields(insight.WhatHappened + " " + 
		insight.WhyItHappened + " " + 
		insight.WhatItMeans + " " + 
		insight.WhatToDo))
	insight.EstimatedReadTime = time.Duration(wordCount/200) * time.Minute // 200 WPM
}

func (g *Generator) calculateReadabilityScore(insight *HumanInsight) float64 {
	totalText := insight.WhatHappened + " " + insight.WhyItHappened + " " + insight.WhatToDo
	words := strings.Fields(totalText)
	sentences := strings.Count(totalText, ".") + strings.Count(totalText, "!") + strings.Count(totalText, "?")
	
	if sentences == 0 {
		return 0.5
	}
	
	avgWordsPerSentence := float64(len(words)) / float64(sentences)
	
	// Simpler sentences = higher readability
	if avgWordsPerSentence < 15 {
		return 0.9
	} else if avgWordsPerSentence < 25 {
		return 0.7
	} else {
		return 0.5
	}
}

func (g *Generator) calculateComplexityScore(insight *HumanInsight) float64 {
	totalText := insight.WhatHappened + " " + insight.WhyItHappened + " " + insight.WhatToDo
	
	// Count technical terms
	technicalTerms := []string{
		"kubernetes", "pod", "container", "service", "node", 
		"cpu", "memory", "network", "deployment", "replica",
		"namespace", "cluster", "api", "endpoint", "latency",
	}
	
	complexityScore := 0.0
	lowerText := strings.ToLower(totalText)
	
	for _, term := range technicalTerms {
		if strings.Contains(lowerText, term) {
			complexityScore += 0.1
		}
	}
	
	if complexityScore > 1.0 {
		complexityScore = 1.0
	}
	
	return complexityScore
}

// Utility methods
func (g *Generator) getEmojiForSeverity(severity domain.Severity) string {
	switch severity {
	case domain.SeverityCritical:
		return "🚨"
	case domain.SeverityError:
		return "⚠️"
	case domain.SeverityWarn:
		return "⚡"
	case domain.SeverityInfo:
		return "ℹ️"
	default:
		return "📊"
	}
}

func (g *Generator) formatTimeAgo(t time.Time) string {
	duration := time.Since(t)
	
	if duration < time.Minute {
		return "just now"
	} else if duration < time.Hour {
		minutes := int(duration.Minutes())
		if minutes == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", minutes)
	} else if duration < 24*time.Hour {
		hours := int(duration.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	} else {
		days := int(duration.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}

// Report generation helpers
func (g *Generator) generateReportSummary(insights []*HumanInsight) string {
	criticalCount := 0
	highCount := 0
	
	for _, insight := range insights {
		switch insight.Severity {
		case "critical":
			criticalCount++
		case "high":
			highCount++
		}
	}
	
	if criticalCount > 0 {
		return fmt.Sprintf("System requires immediate attention: %d critical and %d high-priority issues detected", 
			criticalCount, highCount)
	} else if highCount > 0 {
		return fmt.Sprintf("System has %d high-priority issues that should be addressed soon", highCount)
	} else {
		return "System is operating normally with minor issues that can be addressed during regular maintenance"
	}
}

func (g *Generator) identifyTrends(findings []*domain.Finding) []Trend {
	// Simplified trend identification
	trends := make([]Trend, 0)
	
	// Count findings by type
	typeCounts := make(map[string]int)
	for _, f := range findings {
		typeCounts[string(f.Type)]++
	}
	
	// Identify patterns
	for fType, count := range typeCounts {
		if count >= 3 {
			trends = append(trends, Trend{
				Name:        fmt.Sprintf("Recurring %s Issues", fType),
				Direction:   "degrading",
				Description: fmt.Sprintf("Multiple %s issues detected (%d occurrences)", fType, count),
				Impact:      "System stability may be affected",
				Confidence:  0.7,
			})
		}
	}
	
	return trends
}

func (g *Generator) generateReportRecommendations(insights []*HumanInsight) []string {
	recommendations := make([]string, 0)
	
	hasMemoryIssues := false
	hasNetworkIssues := false
	hasPerformanceIssues := false
	
	for _, insight := range insights {
		if strings.Contains(strings.ToLower(insight.Title), "memory") {
			hasMemoryIssues = true
		}
		if strings.Contains(strings.ToLower(insight.Title), "network") {
			hasNetworkIssues = true
		}
		if strings.Contains(strings.ToLower(insight.Title), "performance") {
			hasPerformanceIssues = true
		}
	}
	
	if hasMemoryIssues {
		recommendations = append(recommendations, 
			"Implement memory profiling and monitoring across all services",
			"Review and adjust memory limits based on actual usage patterns")
	}
	
	if hasNetworkIssues {
		recommendations = append(recommendations,
			"Audit network policies and service mesh configuration",
			"Implement network resilience patterns (retries, circuit breakers)")
	}
	
	if hasPerformanceIssues {
		recommendations = append(recommendations,
			"Conduct performance profiling to identify bottlenecks",
			"Consider horizontal scaling for affected services")
	}
	
	return recommendations
}

func (g *Generator) assessOverallHealth(insights []*HumanInsight) string {
	criticalCount := 0
	highCount := 0
	urgentCount := 0
	
	for _, insight := range insights {
		switch insight.Severity {
		case "critical":
			criticalCount++
		case "high":
			highCount++
		}
		if insight.IsUrgent {
			urgentCount++
		}
	}
	
	if criticalCount > 0 || urgentCount > 2 {
		return "Critical - Immediate action required"
	} else if highCount > 2 {
		return "Degraded - Multiple issues affecting system"
	} else if highCount > 0 {
		return "Warning - Some issues need attention"
	} else {
		return "Healthy - System operating normally"
	}
}

func (g *Generator) calculateReportReadTime(report *HumanReport) time.Duration {
	wordCount := 0
	
	// Count words in summary and recommendations
	wordCount += len(strings.Fields(report.Summary))
	wordCount += len(strings.Fields(strings.Join(report.Recommendations, " ")))
	
	// Count words in insights
	for _, insight := range report.Insights {
		wordCount += len(strings.Fields(insight.WhatHappened + " " + insight.WhatItMeans))
	}
	
	// Assume 200 words per minute reading speed
	return time.Duration(wordCount/200) * time.Minute
}

// Summary generation helpers
func (g *Generator) groupEventsByCategory(events []*domain.Event) map[domain.EventType][]*domain.Event {
	grouped := make(map[domain.EventType][]*domain.Event)
	for _, event := range events {
		grouped[event.Type] = append(grouped[event.Type], event)
	}
	return grouped
}

func (g *Generator) groupEventsBySeverity(events []*domain.Event) map[domain.Severity][]*domain.Event {
	grouped := make(map[domain.Severity][]*domain.Event)
	for _, event := range events {
		grouped[event.Severity] = append(grouped[event.Severity], event)
	}
	return grouped
}

func (g *Generator) generateSummaryOverview(events []*domain.Event, 
	byCategory map[domain.EventType][]*domain.Event,
	bySeverity map[domain.Severity][]*domain.Event) string {
	
	totalEvents := len(events)
	criticalCount := len(bySeverity[domain.SeverityCritical])
	highCount := len(bySeverity[domain.SeverityError])
	
	overview := fmt.Sprintf("Analyzed %d events over the monitoring period. ", totalEvents)
	
	if criticalCount > 0 {
		overview += fmt.Sprintf("Found %d critical issues requiring immediate attention. ", criticalCount)
	}
	
	if highCount > 0 {
		overview += fmt.Sprintf("Detected %d high-priority issues. ", highCount)
	}
	
	// Add category breakdown
	if len(byCategory) > 0 {
		overview += "Events span across: "
		categories := make([]string, 0, len(byCategory))
		for cat, evts := range byCategory {
			categories = append(categories, fmt.Sprintf("%s (%d)", cat, len(evts)))
		}
		overview += strings.Join(categories, ", ")
	}
	
	return overview
}

func (g *Generator) extractKeyMetrics(events []*domain.Event) map[string]string {
	metrics := make(map[string]string)
	
	// Count events by severity
	severityCounts := make(map[domain.Severity]int)
	for _, event := range events {
		severityCounts[event.Severity]++
	}
	
	metrics["Total Events"] = fmt.Sprintf("%d", len(events))
	metrics["Critical Events"] = fmt.Sprintf("%d", severityCounts[domain.SeverityCritical])
	metrics["High Priority"] = fmt.Sprintf("%d", severityCounts[domain.SeverityError])
	
	// Time range
	if len(events) > 0 {
		earliest := events[0].Timestamp
		latest := events[0].Timestamp
		
		for _, event := range events {
			if event.Timestamp.Before(earliest) {
				earliest = event.Timestamp
			}
			if event.Timestamp.After(latest) {
				latest = event.Timestamp
			}
		}
		
		metrics["Time Range"] = fmt.Sprintf("%s to %s", 
			earliest.Format("15:04"), 
			latest.Format("15:04"))
	}
	
	return metrics
}

func (g *Generator) identifyActiveIssues(events []*domain.Event) []IssueSummary {
	issues := make([]IssueSummary, 0)
	
	// Group related events (simplified)
	recentCritical := make([]*domain.Event, 0)
	recentHigh := make([]*domain.Event, 0)
	
	cutoff := time.Now().Add(-1 * time.Hour)
	
	for _, event := range events {
		if event.Timestamp.After(cutoff) {
			switch event.Severity {
			case domain.SeverityCritical:
				recentCritical = append(recentCritical, event)
			case domain.SeverityError:
				recentHigh = append(recentHigh, event)
			}
		}
	}
	
	// Create issue summaries for recent critical events
	for _, event := range recentCritical {
		issues = append(issues, IssueSummary{
			Title:    fmt.Sprintf("%s in %s", event.Type, event.Context.Namespace),
			Severity: "critical",
			Duration: time.Since(event.Timestamp),
			Impact:   "Service availability affected",
			Status:   "active",
		})
	}
	
	// Add high priority issues if space allows
	for i, event := range recentHigh {
		if len(issues) >= 5 {
			break
		}
		issues = append(issues, IssueSummary{
			Title:    fmt.Sprintf("%s issue #%d", event.Type, i+1),
			Severity: "high",
			Duration: time.Since(event.Timestamp),
			Impact:   "Performance degraded",
			Status:   "monitoring",
		})
	}
	
	return issues
}

func (g *Generator) assessSystemHealth(events []*domain.Event) string {
	// Count recent critical events
	recentCritical := 0
	recentHigh := 0
	cutoff := time.Now().Add(-1 * time.Hour)
	
	for _, event := range events {
		if event.Timestamp.After(cutoff) {
			switch event.Severity {
			case domain.SeverityCritical:
				recentCritical++
			case domain.SeverityError:
				recentHigh++
			}
		}
	}
	
	if recentCritical > 0 {
		return "Critical - System experiencing severe issues"
	} else if recentHigh > 3 {
		return "Degraded - Multiple high-priority issues detected"
	} else if recentHigh > 0 {
		return "Warning - Some issues require attention"
	} else {
		return "Healthy - System operating within normal parameters"
	}
}

func (g *Generator) generateNextSteps(issues []IssueSummary) []string {
	steps := make([]string, 0)
	
	hasCritical := false
	hasHigh := false
	
	for _, issue := range issues {
		if issue.Severity == "critical" {
			hasCritical = true
		}
		if issue.Severity == "high" {
			hasHigh = true
		}
	}
	
	if hasCritical {
		steps = append(steps,
			"1. Address critical issues immediately to restore service availability",
			"2. Engage incident response team for critical issues",
			"3. Monitor affected services closely for recovery")
	} else if hasHigh {
		steps = append(steps,
			"1. Review and prioritize high-severity issues",
			"2. Plan remediation for performance issues",
			"3. Update monitoring thresholds if needed")
	} else {
		steps = append(steps,
			"1. Continue routine monitoring",
			"2. Review system metrics for optimization opportunities",
			"3. Update documentation with recent learnings")
	}
	
	return steps
}