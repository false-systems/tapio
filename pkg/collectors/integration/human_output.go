package collector

import (
    "fmt"
    "strings"
    "time"
)

// HumanReadableFormatter provides simple, clear explanations for technical events
type HumanReadableFormatter struct {
    style    ExplanationStyle
    audience Audience
}

// ExplanationStyle defines how explanations are formatted
type ExplanationStyle string

const (
    StyleSimple    ExplanationStyle = "simple"
    StyleTechnical ExplanationStyle = "technical"
    StyleExecutive ExplanationStyle = "executive"
)

// Audience defines who the explanation is for
type Audience string

const (
    AudienceDeveloper Audience = "developer"
    AudienceOperator  Audience = "operator"
    AudienceBusiness  Audience = "business"
)

// HumanReadableExplanation contains the What/Why/Impact/Action format
type HumanReadableExplanation struct {
    // Core explanation
    WhatHappened   string `json:"what_happened"`
    WhyItHappened  string `json:"why_it_happened"`
    Impact         string `json:"impact"`
    WhatToDo       string `json:"what_to_do"`
    
    // Additional context
    Urgency        string   `json:"urgency"`
    Commands       []string `json:"commands,omitempty"`
    RelatedEvents  []string `json:"related_events,omitempty"`
    
    // Metadata
    Confidence     float64  `json:"confidence"`
    ReadableScore  float64  `json:"readable_score"`
}

// NewHumanReadableFormatter creates a new formatter with the specified style
func NewHumanReadableFormatter(style ExplanationStyle, audience Audience) *HumanReadableFormatter {
    return &HumanReadableFormatter{
        style:    style,
        audience: audience,
    }
}

// FormatInsight converts an Insight into a human-readable explanation
func (f *HumanReadableFormatter) FormatInsight(insight Insight) *HumanReadableExplanation {
    explanation := &HumanReadableExplanation{
        RelatedEvents: insight.RelatedEvents,
        Confidence:    0.8, // Default confidence
    }
    
    // Format based on insight type
    switch {
    case strings.Contains(insight.Type, "pattern:memory_leak"):
        f.formatMemoryLeakPattern(insight, explanation)
    case strings.Contains(insight.Type, "memory"):
        f.formatMemoryInsight(insight, explanation)
    case strings.Contains(insight.Type, "network"):
        f.formatNetworkInsight(insight, explanation)
    case strings.Contains(insight.Type, "service"):
        f.formatServiceInsight(insight, explanation)
    default:
        f.formatGenericInsight(insight, explanation)
    }
    
    // Add urgency level
    explanation.Urgency = f.getUrgencyLevel(insight.Severity)
    
    // Calculate readability score
    explanation.ReadableScore = f.calculateReadabilityScore(explanation)
    
    return explanation
}

// formatMemoryInsight formats memory-related insights
func (f *HumanReadableFormatter) formatMemoryInsight(insight Insight, explanation *HumanReadableExplanation) {
    switch f.style {
    case StyleSimple:
        explanation.WhatHappened = "Your application is running out of memory"
        explanation.WhyItHappened = "The service is using more memory than expected"
        explanation.Impact = "The application might crash or become very slow"
        explanation.WhatToDo = "Check which part of your application is using too much memory"
    
    case StyleTechnical:
        explanation.WhatHappened = fmt.Sprintf("Memory pressure detected: %s", insight.Title)
        explanation.WhyItHappened = "Memory consumption exceeded configured thresholds"
        explanation.Impact = "Risk of OOM kills, performance degradation, and service instability"
        explanation.WhatToDo = "Analyze memory profiles, check for leaks, adjust resource limits"
        explanation.Commands = []string{
            "kubectl top pods --sort-by=memory",
            "kubectl describe pod <pod-name>",
            "kubectl logs <pod-name> | grep -i memory",
        }
    
    case StyleExecutive:
        explanation.WhatHappened = "Service reliability issue detected"
        explanation.WhyItHappened = "Resource constraints affecting service performance"
        explanation.Impact = "Potential service disruption affecting user experience"
        explanation.WhatToDo = "Engineering team needs to investigate and optimize resource usage"
    }
}

// formatNetworkInsight formats network-related insights
func (f *HumanReadableFormatter) formatNetworkInsight(insight Insight, explanation *HumanReadableExplanation) {
    switch f.style {
    case StyleSimple:
        explanation.WhatHappened = "Network connection problems detected"
        explanation.WhyItHappened = "Services cannot talk to each other properly"
        explanation.Impact = "Users might see errors or slow responses"
        explanation.WhatToDo = "Check if all services are running and network settings are correct"
    
    case StyleTechnical:
        explanation.WhatHappened = fmt.Sprintf("Network connectivity issue: %s", insight.Title)
        explanation.WhyItHappened = "Inter-service communication failures detected"
        explanation.Impact = "Service dependencies unavailable, potential cascading failures"
        explanation.WhatToDo = "Verify network policies, service mesh configuration, and DNS resolution"
        explanation.Commands = []string{
            "kubectl get networkpolicies",
            "kubectl get services",
            "kubectl exec <pod> -- nslookup <service>",
        }
    
    case StyleExecutive:
        explanation.WhatHappened = "Service communication breakdown"
        explanation.WhyItHappened = "Infrastructure networking issues"
        explanation.Impact = "Customer transactions may fail or experience delays"
        explanation.WhatToDo = "Infrastructure team investigating connectivity issues"
    }
}

// formatServiceInsight formats service-related insights
func (f *HumanReadableFormatter) formatServiceInsight(insight Insight, explanation *HumanReadableExplanation) {
    switch f.style {
    case StyleSimple:
        explanation.WhatHappened = "A service stopped working"
        explanation.WhyItHappened = "The service crashed or was stopped"
        explanation.Impact = "Parts of the application might not work"
        explanation.WhatToDo = "Restart the service and check why it stopped"
    
    case StyleTechnical:
        explanation.WhatHappened = fmt.Sprintf("Service failure: %s", insight.Title)
        explanation.WhyItHappened = "Service crashed or became unresponsive"
        explanation.Impact = "Dependent services affected, potential data loss"
        explanation.WhatToDo = "Check service logs, restart service, investigate root cause"
        explanation.Commands = []string{
            "kubectl get pods -l app=<service>",
            "kubectl logs <pod-name> --previous",
            "kubectl rollout restart deployment/<service>",
        }
    
    case StyleExecutive:
        explanation.WhatHappened = "Critical service outage"
        explanation.WhyItHappened = "Application component failure"
        explanation.Impact = "Service unavailable to customers"
        explanation.WhatToDo = "Emergency response team engaged for immediate recovery"
    }
}

// formatMemoryLeakPattern formats memory leak pattern detection
func (f *HumanReadableFormatter) formatMemoryLeakPattern(insight Insight, explanation *HumanReadableExplanation) {
    confidence := 0.0
    if insight.Prediction != nil {
        confidence = insight.Prediction.Confidence
    }
    
    switch f.style {
    case StyleSimple:
        explanation.WhatHappened = "Your application has a memory leak"
        explanation.WhyItHappened = "The application keeps using more memory without releasing it"
        explanation.Impact = "The application will eventually crash when it runs out of memory"
        explanation.WhatToDo = "Restart the service now and fix the memory leak in the code"
        explanation.Confidence = confidence
    
    case StyleTechnical:
        explanation.WhatHappened = fmt.Sprintf("Memory leak detected (confidence: %.0f%%)", confidence*100)
        explanation.WhyItHappened = "Continuous memory growth pattern identified over time"
        explanation.Impact = "Imminent OOM kill, service will crash within estimated timeframe"
        explanation.WhatToDo = "Immediate restart recommended, implement memory profiling"
        explanation.Commands = []string{
            "kubectl rollout restart deployment/<service>",
            "kubectl exec <pod> -- jmap -histo <pid>",
            "kubectl top pod <pod> --containers",
        }
        explanation.Confidence = confidence
    
    case StyleExecutive:
        explanation.WhatHappened = "Predictive alert: Service stability at risk"
        explanation.WhyItHappened = "Software defect causing resource exhaustion"
        explanation.Impact = fmt.Sprintf("Service will fail within hours (%.0f%% certainty)", confidence*100)
        explanation.WhatToDo = "Preventive action required to avoid customer impact"
        explanation.Confidence = confidence
    }
    
    // Add specific actions if available
    if len(insight.Actions) > 0 {
        commands := []string{}
        for _, action := range insight.Actions {
            commands = append(commands, action.Commands...)
        }
        if len(commands) > 0 && f.style == StyleTechnical {
            explanation.Commands = commands
        }
    }
}

// formatGenericInsight formats generic insights
func (f *HumanReadableFormatter) formatGenericInsight(insight Insight, explanation *HumanReadableExplanation) {
    explanation.WhatHappened = insight.Title
    explanation.WhyItHappened = insight.Description
    explanation.Impact = f.getImpactDescription(insight.Severity)
    explanation.WhatToDo = "Review the event details and take appropriate action"
}

// getUrgencyLevel returns human-readable urgency level
func (f *HumanReadableFormatter) getUrgencyLevel(severity Severity) string {
    switch severity {
    case SeverityCritical:
        return "URGENT - Immediate action required"
    case SeverityHigh:
        return "High - Action needed soon"
    case SeverityMedium:
        return "Medium - Should be addressed"
    case SeverityLow:
        return "Low - Monitor the situation"
    default:
        return "Info - For your awareness"
    }
}

// getImpactDescription returns impact description based on severity
func (f *HumanReadableFormatter) getImpactDescription(severity Severity) string {
    switch f.audience {
    case AudienceBusiness:
        switch severity {
        case SeverityCritical:
            return "Major business disruption - customers are affected"
        case SeverityHigh:
            return "Significant impact on service quality"
        case SeverityMedium:
            return "Some users may experience issues"
        default:
            return "Minimal business impact"
        }
    default:
        switch severity {
        case SeverityCritical:
            return "Service is down or will fail imminently"
        case SeverityHigh:
            return "Major functionality impaired"
        case SeverityMedium:
            return "Degraded performance or partial failure"
        default:
            return "Minor issue or informational"
        }
    }
}

// calculateReadabilityScore calculates how readable the explanation is
func (f *HumanReadableFormatter) calculateReadabilityScore(explanation *HumanReadableExplanation) float64 {
    totalText := explanation.WhatHappened + " " + explanation.WhyItHappened + " " + explanation.WhatToDo
    words := strings.Fields(totalText)
    sentences := strings.Count(totalText, ".") + strings.Count(totalText, "!") + strings.Count(totalText, "?")
    
    if sentences == 0 {
        sentences = 1
    }
    
    avgWordsPerSentence := float64(len(words)) / float64(sentences)
    
    // Simple readability calculation
    if avgWordsPerSentence < 15 {
        return 0.9 // Very readable
    } else if avgWordsPerSentence < 25 {
        return 0.7 // Readable
    } else {
        return 0.5 // Complex
    }
}

// FormatAsStory creates a narrative explanation for complex incidents
func (f *HumanReadableFormatter) FormatAsStory(insights []Insight) string {
    if len(insights) == 0 {
        return ""
    }
    
    var story strings.Builder
    
    // Title
    story.WriteString(fmt.Sprintf("## Incident Story: %s\n\n", insights[0].Title))
    
    // Summary
    story.WriteString("### What's Happening\n")
    story.WriteString(f.createSummary(insights))
    story.WriteString("\n\n")
    
    // Timeline
    story.WriteString("### Timeline of Events\n")
    for i, insight := range insights {
        story.WriteString(fmt.Sprintf("%d. **%s** - %s\n", 
            i+1, 
            insight.Timestamp.Format("15:04:05"), 
            insight.Title))
    }
    story.WriteString("\n")
    
    // Impact
    story.WriteString("### Impact\n")
    story.WriteString(f.createImpactSummary(insights))
    story.WriteString("\n\n")
    
    // Actions
    story.WriteString("### Recommended Actions\n")
    story.WriteString(f.createActionSummary(insights))
    
    return story.String()
}

// createSummary creates a narrative summary
func (f *HumanReadableFormatter) createSummary(insights []Insight) string {
    if len(insights) == 1 {
        return insights[0].Description
    }
    
    // Find the most severe insight
    mostSevere := insights[0]
    for _, insight := range insights[1:] {
        if insight.Severity > mostSevere.Severity {
            mostSevere = insight
        }
    }
    
    return fmt.Sprintf("Multiple related issues detected, starting with %s. "+
        "The situation escalated over %s with %d total events.",
        mostSevere.Title,
        f.formatDuration(insights[len(insights)-1].Timestamp.Sub(insights[0].Timestamp)),
        len(insights))
}

// createImpactSummary summarizes the impact
func (f *HumanReadableFormatter) createImpactSummary(insights []Insight) string {
    hasHighSeverity := false
    for _, insight := range insights {
        if insight.Severity >= SeverityHigh {
            hasHighSeverity = true
            break
        }
    }
    
    if hasHighSeverity {
        return "This incident is causing significant disruption to services. " +
               "Users are likely experiencing errors or degraded performance."
    }
    
    return "The impact is currently limited but may escalate if not addressed."
}

// createActionSummary creates action summary
func (f *HumanReadableFormatter) createActionSummary(insights []Insight) string {
    actions := []string{}
    
    for _, insight := range insights {
        if len(insight.Actions) > 0 {
            for _, action := range insight.Actions {
                actions = append(actions, fmt.Sprintf("- %s", action.Title))
            }
        }
    }
    
    if len(actions) == 0 {
        return "Review the timeline and investigate the root cause."
    }
    
    return strings.Join(actions, "\n")
}

// formatDuration formats a duration in human-readable form
func (f *HumanReadableFormatter) formatDuration(d time.Duration) string {
    if d < time.Minute {
        return fmt.Sprintf("%d seconds", int(d.Seconds()))
    } else if d < time.Hour {
        return fmt.Sprintf("%d minutes", int(d.Minutes()))
    } else {
        return fmt.Sprintf("%.1f hours", d.Hours())
    }
}