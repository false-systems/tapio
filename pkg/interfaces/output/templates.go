package output

import (
	"fmt"
	"strings"
)

// ExplanationTemplate defines templates for different explanation types
type ExplanationTemplate struct {
	ID                     string            `json:"id"`
	Name                   string            `json:"name"`
	Description            string            `json:"description"`
	EventCategories        []string          `json:"event_categories"`
	Severities             []string          `json:"severities"`
	Audience               string            `json:"audience"`
	Language               string            `json:"language"`
	
	// Template content
	WhatHappenedTemplate   string            `json:"what_happened_template"`
	WhyItHappenedTemplate  string            `json:"why_it_happened_template"`
	WhatItMeansTemplate    string            `json:"what_it_means_template"`
	WhatToDoTemplate       string            `json:"what_to_do_template"`
	HowToPreventTemplate   string            `json:"how_to_prevent_template"`
	
	// Business impact templates
	BusinessImpactTemplate string            `json:"business_impact_template,omitempty"`
	UserImpactTemplate     string            `json:"user_impact_template,omitempty"`
	
	// Template variables
	Variables              map[string]string `json:"variables"`
	
	// Quality settings
	MinConfidence          float64           `json:"min_confidence"`
	Priority               int               `json:"priority"`
}

// TemplateManager manages explanation templates
type TemplateManager struct {
	templates map[string]*ExplanationTemplate
	config    *Config
}

// NewTemplateManager creates a new template manager
func NewTemplateManager(config *Config) *TemplateManager {
	tm := &TemplateManager{
		templates: make(map[string]*ExplanationTemplate),
		config:    config,
	}
	tm.loadDefaultTemplates()
	return tm
}

// loadDefaultTemplates loads the default templates
func (tm *TemplateManager) loadDefaultTemplates() {
	templates := []*ExplanationTemplate{
		// Memory-related templates
		{
			ID:                    "memory_leak_simple",
			Name:                  "Memory Leak Simple Explanation",
			EventCategories:       []string{"system.memory", "resource.exhaustion"},
			Severities:            []string{"critical", "high"},
			Audience:              "developer",
			Language:              "en",
			WhatHappenedTemplate:  "A memory leak was detected in {{.pod}} (namespace: {{.namespace}})",
			WhyItHappenedTemplate: "The application is consuming memory continuously without releasing it. Memory usage has increased by {{.memory_increase}}% over the last {{.time_window}}",
			WhatItMeansTemplate:   "If left unchecked, the pod will exhaust its memory limit and be terminated (OOMKilled), causing service disruption",
			WhatToDoTemplate:      "1. Review recent code changes for memory allocation issues\n2. Use memory profiling tools to identify the leak source\n3. Consider increasing memory limits temporarily while investigating",
			HowToPreventTemplate:  "Implement proper resource cleanup, use memory profiling in CI/CD, set up memory usage alerts",
			BusinessImpactTemplate: "Service reliability is at risk. Potential downtime could affect {{.affected_users}} users",
			MinConfidence:         0.7,
			Priority:              1,
		},
		{
			ID:                    "oom_prediction",
			Name:                  "OOM Prediction",
			EventCategories:       []string{"prediction.oom", "system.memory"},
			Severities:            []string{"high", "critical"},
			Audience:              "developer",
			Language:              "en",
			WhatHappenedTemplate:  "Pod {{.pod}} is predicted to run out of memory in {{.time_to_oom}}",
			WhyItHappenedTemplate: "Memory consumption is increasing at {{.memory_rate}} MB/minute. Current usage: {{.current_memory}}/{{.memory_limit}}",
			WhatItMeansTemplate:   "The pod will be forcefully terminated when it hits the memory limit, causing service interruption",
			WhatToDoTemplate:      "Take immediate action: 1. Scale horizontally to distribute load\n2. Restart the pod to buy time\n3. Investigate memory consumption patterns",
			HowToPreventTemplate:  "Set up predictive alerts, implement circuit breakers, review memory allocation patterns",
			BusinessImpactTemplate: "Imminent service disruption in {{.time_to_oom}}. Prepare incident response team",
			MinConfidence:         0.8,
			Priority:              1,
		},
		
		// Network-related templates
		{
			ID:                    "network_failure_simple",
			Name:                  "Network Failure Simple Explanation",
			EventCategories:       []string{"network.connectivity", "network.failure"},
			Severities:            []string{"critical", "high"},
			Audience:              "developer",
			Language:              "en",
			WhatHappenedTemplate:  "Network connectivity issues detected between {{.source}} and {{.destination}}",
			WhyItHappenedTemplate: "Connection failures: {{.failure_count}} in the last {{.time_window}}. Error: {{.error_message}}",
			WhatItMeansTemplate:   "Services cannot communicate reliably, causing request failures and degraded user experience",
			WhatToDoTemplate:      "1. Check network policies and firewall rules\n2. Verify service endpoints are correct\n3. Review recent network configuration changes",
			HowToPreventTemplate:  "Implement retry logic, use service mesh for resilience, monitor network metrics",
			UserImpactTemplate:    "Users may experience timeouts, slow responses, or complete failures when accessing affected features",
			MinConfidence:         0.7,
			Priority:              1,
		},
		
		// Performance-related templates
		{
			ID:                    "performance_degradation",
			Name:                  "Performance Degradation",
			EventCategories:       []string{"performance.latency", "performance.degradation"},
			Severities:            []string{"medium", "high"},
			Audience:              "developer",
			Language:              "en",
			WhatHappenedTemplate:  "Performance degradation detected in {{.service}}. Response time increased by {{.latency_increase}}%",
			WhyItHappenedTemplate: "P95 latency rose from {{.baseline_latency}}ms to {{.current_latency}}ms. Possible causes: increased load, resource contention, or code changes",
			WhatItMeansTemplate:   "Users are experiencing slower response times, which may lead to timeouts and poor user experience",
			WhatToDoTemplate:      "1. Check CPU and memory utilization\n2. Review recent deployments\n3. Analyze slow query logs\n4. Consider scaling resources",
			HowToPreventTemplate:  "Set up performance benchmarks, implement caching, optimize database queries, use APM tools",
			BusinessImpactTemplate: "User satisfaction declining. {{.affected_requests}}% of requests are slower than SLA",
			MinConfidence:         0.6,
			Priority:              2,
		},
		
		// CPU-related templates
		{
			ID:                    "cpu_throttling",
			Name:                  "CPU Throttling",
			EventCategories:       []string{"system.cpu", "resource.throttling"},
			Severities:            []string{"medium", "high"},
			Audience:              "developer",
			Language:              "en",
			WhatHappenedTemplate:  "CPU throttling detected on pod {{.pod}}. Throttled {{.throttle_percentage}}% of the time",
			WhyItHappenedTemplate: "The pod is trying to use more CPU than its limit allows ({{.cpu_limit}}). Current demand: {{.cpu_demand}}",
			WhatItMeansTemplate:   "Application performance is being artificially limited, causing slower processing and increased latency",
			WhatToDoTemplate:      "1. Review CPU limits and adjust if necessary\n2. Optimize CPU-intensive operations\n3. Consider horizontal scaling",
			HowToPreventTemplate:  "Profile CPU usage, set appropriate resource requests/limits, implement efficient algorithms",
			MinConfidence:         0.7,
			Priority:              2,
		},
		
		// Storage-related templates
		{
			ID:                    "disk_space_warning",
			Name:                  "Disk Space Warning",
			EventCategories:       []string{"storage.disk", "resource.exhaustion"},
			Severities:            []string{"medium", "high"},
			Audience:              "operator",
			Language:              "en",
			WhatHappenedTemplate:  "Disk space running low on {{.node}}. {{.disk_percentage}}% full ({{.disk_free}} free)",
			WhyItHappenedTemplate: "Disk usage has been growing at {{.growth_rate}} GB/day. Primary consumers: {{.top_consumers}}",
			WhatItMeansTemplate:   "When disk is full, pods may fail to write data, logs will be lost, and new pods cannot be scheduled",
			WhatToDoTemplate:      "1. Clean up old logs and temporary files\n2. Review retention policies\n3. Add storage capacity if needed",
			HowToPreventTemplate:  "Implement log rotation, set up disk usage alerts, use persistent volume claims appropriately",
			MinConfidence:         0.8,
			Priority:              2,
		},
		
		// Container-related templates
		{
			ID:                    "container_restart_loop",
			Name:                  "Container Restart Loop",
			EventCategories:       []string{"container.lifecycle", "stability.crash"},
			Severities:            []string{"high", "critical"},
			Audience:              "developer",
			Language:              "en",
			WhatHappenedTemplate:  "Container {{.container}} in pod {{.pod}} is in a restart loop. Restarted {{.restart_count}} times",
			WhyItHappenedTemplate: "Container is crashing shortly after startup. Last exit code: {{.exit_code}}. Reason: {{.exit_reason}}",
			WhatItMeansTemplate:   "The service is unavailable and cannot serve requests. This indicates a critical application or configuration issue",
			WhatToDoTemplate:      "1. Check container logs for error messages\n2. Verify configuration and secrets\n3. Check for missing dependencies\n4. Review recent code changes",
			HowToPreventTemplate:  "Add health checks, implement proper error handling, test container locally before deployment",
			BusinessImpactTemplate: "Service completely unavailable. All requests to this service are failing",
			MinConfidence:         0.9,
			Priority:              1,
		},
		
		// Security-related templates
		{
			ID:                    "security_policy_violation",
			Name:                  "Security Policy Violation",
			EventCategories:       []string{"security.policy", "security.violation"},
			Severities:            []string{"high", "critical"},
			Audience:              "operator",
			Language:              "en",
			WhatHappenedTemplate:  "Security policy violation detected: {{.violation_type}} in {{.resource}}",
			WhyItHappenedTemplate: "{{.policy_name}} policy was violated. Details: {{.violation_details}}",
			WhatItMeansTemplate:   "This represents a potential security risk that needs immediate attention to maintain cluster security",
			WhatToDoTemplate:      "1. Review the violation details\n2. Update resource configuration to comply\n3. Contact security team if needed",
			HowToPreventTemplate:  "Review security policies before deployment, use policy validation tools, implement security scanning in CI/CD",
			MinConfidence:         0.9,
			Priority:              1,
		},
	}
	
	for _, template := range templates {
		tm.templates[template.ID] = template
	}
}

// FindBestTemplate finds the best matching template for given criteria
func (tm *TemplateManager) FindBestTemplate(category string, severity string, audience string) *ExplanationTemplate {
	var bestTemplate *ExplanationTemplate
	var bestScore int
	
	for _, template := range tm.templates {
		score := 0
		
		// Check category match
		for _, cat := range template.EventCategories {
			if strings.Contains(category, cat) || strings.Contains(cat, category) {
				score += 3
				break
			}
		}
		
		// Check severity match
		for _, sev := range template.Severities {
			if sev == severity {
				score += 2
				break
			}
		}
		
		// Check audience match
		if template.Audience == audience {
			score += 1
		}
		
		if score > bestScore {
			bestScore = score
			bestTemplate = template
		}
	}
	
	return bestTemplate
}

// FillTemplate fills a template with variables
func FillTemplate(template string, variables map[string]string) string {
	result := template
	for key, value := range variables {
		placeholder := fmt.Sprintf("{{.%s}}", key)
		result = strings.ReplaceAll(result, placeholder, value)
	}
	// Remove any unfilled variables
	for strings.Contains(result, "{{.") {
		start := strings.Index(result, "{{.")
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], "}}")
		if end == -1 {
			break
		}
		result = result[:start] + "[unknown]" + result[start+end+2:]
	}
	return result
}

// GetTemplate returns a template by ID
func (tm *TemplateManager) GetTemplate(id string) *ExplanationTemplate {
	return tm.templates[id]
}

// AddTemplate adds a custom template
func (tm *TemplateManager) AddTemplate(template *ExplanationTemplate) {
	tm.templates[template.ID] = template
}