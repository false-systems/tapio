package correlation

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CorrelatorConfig configures threshold values for correlators
type CorrelatorConfig struct {
	// Memory pressure thresholds
	MinMemoryEvents int `json:"min_memory_events"`

	// Service failure thresholds
	MinRestartEvents int `json:"min_restart_events"` // For crash loop detection
	MinErrorEvents   int `json:"min_error_events"`   // For service degradation

	// Network issue thresholds
	MinDNSEvents        int `json:"min_dns_events"`        // For DNS resolution failures
	MinConnectionEvents int `json:"min_connection_events"` // For connection issues

	// Security threat thresholds
	MinAuthFailures      int `json:"min_auth_failures"`       // For brute force detection
	MinFailuresPerSource int `json:"min_failures_per_source"` // For brute force per source

	// Analysis thresholds
	EventRateThreshold     float64 `json:"event_rate_threshold"`     // Events per minute
	CriticalEventThreshold int     `json:"critical_event_threshold"` // Critical events count
	TrendAnalysisWindow    int     `json:"trend_analysis_window"`    // Minutes for trend analysis
}

// DefaultCorrelatorConfig returns default configuration values
func DefaultCorrelatorConfig() CorrelatorConfig {
	return CorrelatorConfig{
		MinMemoryEvents:        2,
		MinRestartEvents:       3,
		MinErrorEvents:         5,
		MinDNSEvents:           3,
		MinConnectionEvents:    5,
		MinAuthFailures:        5,
		MinFailuresPerSource:   3,
		EventRateThreshold:     1000.0,
		CriticalEventThreshold: 10,
		TrendAnalysisWindow:    5,
	}
}

// MemoryPressureCorrelator correlates memory-related events
type MemoryPressureCorrelator struct {
	config CorrelatorConfig
}

// NewMemoryPressureCorrelator creates a new memory pressure correlator
func NewMemoryPressureCorrelator(config CorrelatorConfig) *MemoryPressureCorrelator {
	return &MemoryPressureCorrelator{config: config}
}

func (m *MemoryPressureCorrelator) Name() string {
	return "memory_pressure"
}

func (m *MemoryPressureCorrelator) Correlate(events []TimelineEvent) []CorrelationResult {
	var results []CorrelationResult

	// Group events by entity
	entityEvents := make(map[string][]TimelineEvent)
	for _, event := range events {
		key := fmt.Sprintf("%s:%s", event.Entity.Type, event.Entity.Name)
		entityEvents[key] = append(entityEvents[key], event)
	}

	// Look for memory pressure patterns
	for entity, entityEventList := range entityEvents {
		var memoryEvents []TimelineEvent
		var oomEvents []TimelineEvent
		var throttleEvents []TimelineEvent

		for _, event := range entityEventList {
			if strings.Contains(strings.ToLower(event.Message), "memory") ||
				strings.Contains(strings.ToLower(event.Message), "oom") {
				memoryEvents = append(memoryEvents, event)
			}

			if strings.Contains(strings.ToLower(event.Message), "oom") ||
				strings.Contains(strings.ToLower(event.Message), "out of memory") {
				oomEvents = append(oomEvents, event)
			}

			if strings.Contains(strings.ToLower(event.Message), "throttl") {
				throttleEvents = append(throttleEvents, event)
			}
		}

		// Check for memory pressure correlation
		if len(memoryEvents) >= m.config.MinMemoryEvents || len(oomEvents) > 0 {
			severity := "warning"
			confidence := 0.7
			description := fmt.Sprintf("Memory pressure detected for %s", entity)

			if len(oomEvents) > 0 {
				severity = "critical"
				confidence = 0.95
				description = fmt.Sprintf("Out of memory condition detected for %s", entity)
			}

			var allEvents []string
			for _, e := range memoryEvents {
				allEvents = append(allEvents, e.ID)
			}

			result := CorrelationResult{
				ID:          fmt.Sprintf("mem_pressure_%s_%s", entity, uuid.New().String()),
				Type:        "memory_pressure",
				Confidence:  confidence,
				Events:      allEvents,
				Description: description,
				Severity:    severity,
				Impact: ImpactAssessment{
					Scope:       "container",
					Affected:    []string{entity},
					Severity:    severity,
					Description: "Memory pressure may cause performance degradation or container restarts",
				},
				Remediation: []RemediationStep{
					{
						Action:      "increase_memory_limit",
						Target:      entity,
						Description: "Increase memory limits for the affected container",
						Automated:   false,
						Priority:    1,
					},
					{
						Action:      "optimize_memory_usage",
						Target:      entity,
						Description: "Review and optimize application memory usage",
						Automated:   false,
						Priority:    2,
					},
				},
				Metadata: map[string]interface{}{
					"entity":          entity,
					"memory_events":   len(memoryEvents),
					"oom_events":      len(oomEvents),
					"throttle_events": len(throttleEvents),
				},
			}

			results = append(results, result)
		}
	}

	return results
}

// ServiceFailureCorrelator correlates service failure events
type ServiceFailureCorrelator struct {
	config CorrelatorConfig
}

// NewServiceFailureCorrelator creates a new service failure correlator
func NewServiceFailureCorrelator(config CorrelatorConfig) *ServiceFailureCorrelator {
	return &ServiceFailureCorrelator{config: config}
}

func (s *ServiceFailureCorrelator) Name() string {
	return "service_failure"
}

func (s *ServiceFailureCorrelator) Correlate(events []TimelineEvent) []CorrelationResult {
	var results []CorrelationResult

	// Group events by service
	serviceEvents := make(map[string][]TimelineEvent)
	for _, event := range events {
		if event.Entity.Type == "service" {
			serviceEvents[event.Entity.Name] = append(serviceEvents[event.Entity.Name], event)
		}
	}

	// Look for failure patterns
	for service, eventList := range serviceEvents {
		var failureEvents []TimelineEvent
		var restartEvents []TimelineEvent
		var errorEvents []TimelineEvent

		for _, event := range eventList {
			if event.EventType == "ServiceEventFailure" ||
				strings.Contains(strings.ToLower(event.Message), "failed") {
				failureEvents = append(failureEvents, event)
			}

			if event.EventType == "ServiceEventRestart" ||
				strings.Contains(strings.ToLower(event.Message), "restart") {
				restartEvents = append(restartEvents, event)
			}

			if event.Severity == "error" || event.Severity == "critical" {
				errorEvents = append(errorEvents, event)
			}
		}

		// Check for crash loop pattern
		if len(restartEvents) >= s.config.MinRestartEvents {
			var eventIDs []string
			for _, e := range restartEvents {
				eventIDs = append(eventIDs, e.ID)
			}

			result := CorrelationResult{
				ID:          fmt.Sprintf("crash_loop_%s_%s", service, uuid.New().String()),
				Type:        "crash_loop",
				Confidence:  0.9,
				Events:      eventIDs,
				Description: fmt.Sprintf("Service %s is in a crash loop with %d restarts", service, len(restartEvents)),
				Severity:    "critical",
				Impact: ImpactAssessment{
					Scope:       "service",
					Affected:    []string{service},
					Severity:    "critical",
					Description: "Service is unavailable due to repeated crashes",
				},
				Remediation: []RemediationStep{
					{
						Action:      "check_logs",
						Target:      service,
						Description: "Review service logs for root cause",
						Automated:   false,
						Priority:    1,
					},
					{
						Action:      "rollback",
						Target:      service,
						Description: "Consider rolling back to previous version",
						Automated:   false,
						Priority:    2,
					},
				},
				Metadata: map[string]interface{}{
					"service":       service,
					"restart_count": len(restartEvents),
					"failure_count": len(failureEvents),
					"error_count":   len(errorEvents),
				},
			}

			results = append(results, result)
		}

		// Check for service degradation
		if len(errorEvents) >= s.config.MinErrorEvents && len(failureEvents) == 0 {
			var eventIDs []string
			for _, e := range errorEvents {
				eventIDs = append(eventIDs, e.ID)
			}

			result := CorrelationResult{
				ID:          fmt.Sprintf("service_degraded_%s_%s", service, uuid.New().String()),
				Type:        "service_degradation",
				Confidence:  0.7,
				Events:      eventIDs,
				Description: fmt.Sprintf("Service %s showing signs of degradation", service),
				Severity:    "warning",
				Impact: ImpactAssessment{
					Scope:       "service",
					Affected:    []string{service},
					Severity:    "warning",
					Description: "Service is operational but experiencing errors",
				},
				Remediation: []RemediationStep{
					{
						Action:      "monitor",
						Target:      service,
						Description: "Increase monitoring of service metrics",
						Automated:   true,
						Priority:    1,
					},
				},
				Metadata: map[string]interface{}{
					"service":     service,
					"error_count": len(errorEvents),
				},
			}

			results = append(results, result)
		}
	}

	return results
}

// NetworkIssueCorrelator correlates network-related events
type NetworkIssueCorrelator struct {
	config CorrelatorConfig
}

// NewNetworkIssueCorrelator creates a new network issue correlator
func NewNetworkIssueCorrelator(config CorrelatorConfig) *NetworkIssueCorrelator {
	return &NetworkIssueCorrelator{config: config}
}

func (n *NetworkIssueCorrelator) Name() string {
	return "network_issue"
}

func (n *NetworkIssueCorrelator) Correlate(events []TimelineEvent) []CorrelationResult {
	var results []CorrelationResult

	var networkEvents []TimelineEvent
	var dnsEvents []TimelineEvent
	var connectionEvents []TimelineEvent
	var timeoutEvents []TimelineEvent

	for _, event := range events {
		message := strings.ToLower(event.Message)

		if strings.Contains(message, "network") ||
			strings.Contains(message, "connection") ||
			strings.Contains(message, "socket") {
			networkEvents = append(networkEvents, event)
		}

		if strings.Contains(message, "dns") ||
			strings.Contains(message, "resolve") ||
			strings.Contains(message, "nxdomain") {
			dnsEvents = append(dnsEvents, event)
		}

		if strings.Contains(message, "connection refused") ||
			strings.Contains(message, "connection reset") ||
			strings.Contains(message, "connection timeout") {
			connectionEvents = append(connectionEvents, event)
		}

		if strings.Contains(message, "timeout") {
			timeoutEvents = append(timeoutEvents, event)
		}
	}

	// Check for DNS issues
	if len(dnsEvents) >= n.config.MinDNSEvents {
		var eventIDs []string
		for _, e := range dnsEvents {
			eventIDs = append(eventIDs, e.ID)
		}

		result := CorrelationResult{
			ID:          fmt.Sprintf("dns_issue_%s", uuid.New().String()),
			Type:        "dns_resolution_failure",
			Confidence:  0.85,
			Events:      eventIDs,
			Description: "Multiple DNS resolution failures detected",
			Severity:    "high",
			Impact: ImpactAssessment{
				Scope:       "cluster",
				Affected:    []string{"dns-service"},
				Severity:    "high",
				Description: "Services unable to resolve DNS names",
			},
			Remediation: []RemediationStep{
				{
					Action:      "check_dns_service",
					Target:      "kube-dns",
					Description: "Verify DNS service is running",
					Automated:   true,
					Priority:    1,
				},
				{
					Action:      "check_dns_config",
					Target:      "cluster",
					Description: "Verify DNS configuration",
					Automated:   false,
					Priority:    2,
				},
			},
			Metadata: map[string]interface{}{
				"dns_events": len(dnsEvents),
			},
		}

		results = append(results, result)
	}

	// Check for connection issues
	if len(connectionEvents) >= n.config.MinConnectionEvents {
		var eventIDs []string
		affectedServices := make(map[string]bool)

		for _, e := range connectionEvents {
			eventIDs = append(eventIDs, e.ID)
			affectedServices[e.Entity.Name] = true
		}

		var affected []string
		for service := range affectedServices {
			affected = append(affected, service)
		}

		result := CorrelationResult{
			ID:          fmt.Sprintf("connection_issue_%s", uuid.New().String()),
			Type:        "network_connectivity",
			Confidence:  0.8,
			Events:      eventIDs,
			Description: "Multiple connection failures detected",
			Severity:    "high",
			Impact: ImpactAssessment{
				Scope:       "service",
				Affected:    affected,
				Severity:    "high",
				Description: "Services experiencing connectivity issues",
			},
			Remediation: []RemediationStep{
				{
					Action:      "check_network_policies",
					Target:      "cluster",
					Description: "Review network policies",
					Automated:   false,
					Priority:    1,
				},
				{
					Action:      "check_firewall",
					Target:      "infrastructure",
					Description: "Check firewall rules",
					Automated:   false,
					Priority:    2,
				},
			},
			Metadata: map[string]interface{}{
				"connection_events": len(connectionEvents),
				"timeout_events":    len(timeoutEvents),
				"affected_services": len(affected),
			},
		}

		results = append(results, result)
	}

	return results
}

// SecurityThreatCorrelator correlates security-related events
type SecurityThreatCorrelator struct {
	config CorrelatorConfig
}

// NewSecurityThreatCorrelator creates a new security threat correlator
func NewSecurityThreatCorrelator(config CorrelatorConfig) *SecurityThreatCorrelator {
	return &SecurityThreatCorrelator{config: config}
}

func (s *SecurityThreatCorrelator) Name() string {
	return "security_threat"
}

func (s *SecurityThreatCorrelator) Correlate(events []TimelineEvent) []CorrelationResult {
	var results []CorrelationResult

	var authFailures []TimelineEvent
	var privilegeEscalation []TimelineEvent
	var suspiciousActivity []TimelineEvent
	var securityViolations []TimelineEvent

	for _, event := range events {
		message := strings.ToLower(event.Message)

		if strings.Contains(message, "authentication failed") ||
			strings.Contains(message, "login failed") ||
			strings.Contains(message, "unauthorized") {
			authFailures = append(authFailures, event)
		}

		if strings.Contains(message, "privilege") ||
			strings.Contains(message, "escalation") ||
			strings.Contains(message, "sudo") {
			privilegeEscalation = append(privilegeEscalation, event)
		}

		if strings.Contains(message, "suspicious") ||
			strings.Contains(message, "anomaly") ||
			strings.Contains(message, "unusual") {
			suspiciousActivity = append(suspiciousActivity, event)
		}

		if strings.Contains(message, "security") ||
			strings.Contains(message, "violation") ||
			strings.Contains(message, "breach") {
			securityViolations = append(securityViolations, event)
		}
	}

	// Check for brute force attempts
	if len(authFailures) >= s.config.MinAuthFailures {
		// Group by source
		sourceFailures := make(map[string][]TimelineEvent)
		for _, event := range authFailures {
			source := event.Entity.Name
			if source == "" {
				source = "unknown"
			}
			sourceFailures[source] = append(sourceFailures[source], event)
		}

		for source, failures := range sourceFailures {
			if len(failures) >= s.config.MinFailuresPerSource {
				var eventIDs []string
				for _, e := range failures {
					eventIDs = append(eventIDs, e.ID)
				}

				result := CorrelationResult{
					ID:          fmt.Sprintf("brute_force_%s_%s", source, uuid.New().String()),
					Type:        "brute_force_attempt",
					Confidence:  0.9,
					Events:      eventIDs,
					Description: fmt.Sprintf("Possible brute force attack from %s", source),
					Severity:    "critical",
					Impact: ImpactAssessment{
						Scope:       "security",
						Affected:    []string{source},
						Severity:    "critical",
						Description: "Potential unauthorized access attempt",
					},
					Remediation: []RemediationStep{
						{
							Action:      "block_source",
							Target:      source,
							Description: "Block source IP/user",
							Automated:   true,
							Priority:    1,
						},
						{
							Action:      "review_logs",
							Target:      "security",
							Description: "Review security logs for compromise",
							Automated:   false,
							Priority:    2,
						},
					},
					Metadata: map[string]interface{}{
						"source":        source,
						"failure_count": len(failures),
						"time_window":   "5m",
					},
				}

				results = append(results, result)
			}
		}
	}

	// Check for privilege escalation attempts
	if len(privilegeEscalation) > 0 {
		var eventIDs []string
		for _, e := range privilegeEscalation {
			eventIDs = append(eventIDs, e.ID)
		}

		result := CorrelationResult{
			ID:          fmt.Sprintf("priv_escalation_%s", uuid.New().String()),
			Type:        "privilege_escalation",
			Confidence:  0.85,
			Events:      eventIDs,
			Description: "Potential privilege escalation detected",
			Severity:    "critical",
			Impact: ImpactAssessment{
				Scope:       "security",
				Affected:    []string{"cluster"},
				Severity:    "critical",
				Description: "Unauthorized privilege escalation attempt",
			},
			Remediation: []RemediationStep{
				{
					Action:      "audit_permissions",
					Target:      "rbac",
					Description: "Audit RBAC permissions",
					Automated:   false,
					Priority:    1,
				},
				{
					Action:      "incident_response",
					Target:      "security-team",
					Description: "Initiate incident response",
					Automated:   true,
					Priority:    1,
				},
			},
			Metadata: map[string]interface{}{
				"event_count": len(privilegeEscalation),
			},
		}

		results = append(results, result)
	}

	return results
}

// PatternAnalyzer analyzes the timeline for patterns
type PatternAnalyzer struct {
	config CorrelatorConfig
}

// NewPatternAnalyzer creates a new pattern analyzer
func NewPatternAnalyzer(config CorrelatorConfig) *PatternAnalyzer {
	return &PatternAnalyzer{config: config}
}

func (p *PatternAnalyzer) Name() string {
	return "pattern_analyzer"
}

func (p *PatternAnalyzer) Analyze(timeline *Timeline) []AnalysisResult {
	var results []AnalysisResult

	// Find patterns
	patterns := timeline.FindPatterns()

	if len(patterns) > 0 {
		var insights []string
		details := make(map[string]interface{})

		// Count pattern types
		patternCounts := make(map[string]int)
		for _, pattern := range patterns {
			patternCounts[pattern.Type]++
		}

		// Generate insights
		for patternType, count := range patternCounts {
			insights = append(insights, fmt.Sprintf("Found %d %s patterns", count, patternType))
		}

		details["pattern_counts"] = patternCounts
		details["total_patterns"] = len(patterns)

		result := AnalysisResult{
			Type:      "pattern_analysis",
			Summary:   fmt.Sprintf("Detected %d patterns in timeline", len(patterns)),
			Details:   details,
			Insights:  insights,
			Timestamp: time.Now(),
		}

		results = append(results, result)
	}

	return results
}

// AnomalyAnalyzer detects anomalies in the timeline
type AnomalyAnalyzer struct {
	config CorrelatorConfig
}

// NewAnomalyAnalyzer creates a new anomaly analyzer
func NewAnomalyAnalyzer(config CorrelatorConfig) *AnomalyAnalyzer {
	return &AnomalyAnalyzer{config: config}
}

func (a *AnomalyAnalyzer) Name() string {
	return "anomaly_analyzer"
}

func (a *AnomalyAnalyzer) Analyze(timeline *Timeline) []AnalysisResult {
	var results []AnalysisResult

	stats := timeline.GetStatistics()

	// Check for unusual event rates
	if stats.TotalEvents > 0 {
		duration := stats.TimeRange.End.Sub(stats.TimeRange.Start)
		if duration > 0 {
			eventsPerMinute := float64(stats.TotalEvents) / duration.Minutes()

			if eventsPerMinute > a.config.EventRateThreshold {
				result := AnalysisResult{
					Type:    "anomaly_detection",
					Summary: fmt.Sprintf("Unusually high event rate: %.2f events/minute", eventsPerMinute),
					Details: map[string]interface{}{
						"events_per_minute": eventsPerMinute,
						"total_events":      stats.TotalEvents,
						"duration":          duration.String(),
					},
					Insights: []string{
						"Event rate is significantly higher than normal",
						"This may indicate a system issue or attack",
					},
					Timestamp: time.Now(),
				}

				results = append(results, result)
			}
		}
	}

	// Check for severity distribution anomalies
	if critical, exists := stats.EventsBySeverity["critical"]; exists && critical > a.config.CriticalEventThreshold {
		result := AnalysisResult{
			Type:    "anomaly_detection",
			Summary: fmt.Sprintf("High number of critical events: %d", critical),
			Details: map[string]interface{}{
				"critical_events":       critical,
				"severity_distribution": stats.EventsBySeverity,
			},
			Insights: []string{
				"Unusually high number of critical events",
				"System may be experiencing severe issues",
			},
			Timestamp: time.Now(),
		}

		results = append(results, result)
	}

	return results
}

// TrendAnalyzer analyzes trends in the timeline
type TrendAnalyzer struct {
	config CorrelatorConfig
}

// NewTrendAnalyzer creates a new trend analyzer
func NewTrendAnalyzer(config CorrelatorConfig) *TrendAnalyzer {
	return &TrendAnalyzer{config: config}
}

func (t *TrendAnalyzer) Name() string {
	return "trend_analyzer"
}

func (t *TrendAnalyzer) Analyze(timeline *Timeline) []AnalysisResult {
	var results []AnalysisResult

	// Get events in time windows
	now := time.Now()
	windowDuration := time.Duration(t.config.TrendAnalysisWindow) * time.Minute
	last5Min := timeline.GetEvents(&TimeRange{
		Start: now.Add(-windowDuration),
		End:   now,
	})

	last15Min := timeline.GetEvents(&TimeRange{
		Start: now.Add(-3 * windowDuration),
		End:   now.Add(-windowDuration),
	})

	if len(last5Min) > 0 && len(last15Min) > 0 {
		recentRate := float64(len(last5Min)) / float64(t.config.TrendAnalysisWindow)
		previousRate := float64(len(last15Min)) / float64(2*t.config.TrendAnalysisWindow)

		if recentRate > previousRate*2 {
			result := AnalysisResult{
				Type:    "trend_analysis",
				Summary: "Event rate is increasing rapidly",
				Details: map[string]interface{}{
					"recent_rate":   recentRate,
					"previous_rate": previousRate,
					"increase":      (recentRate / previousRate) * 100,
				},
				Insights: []string{
					fmt.Sprintf("Event rate increased by %.0f%%", ((recentRate/previousRate)-1)*100),
					"This trend may indicate developing issues",
				},
				Timestamp: time.Now(),
			}

			results = append(results, result)
		}
	}

	return results
}
