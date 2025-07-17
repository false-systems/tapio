package humanoutput

import (
	"context"
	"strings"
	"testing"
	"time"
	
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestGenerateInsight(t *testing.T) {
	generator := NewGenerator(DefaultConfig())
	
	tests := []struct {
		name     string
		finding  *domain.Finding
		wantErr  bool
		validate func(*testing.T, *HumanInsight)
	}{
		{
			name: "memory_leak_finding",
			finding: &domain.Finding{
				ID:          "test-001",
				Type:        domain.FindingMemoryLeak,
				Severity:    domain.SeverityCritical,
				Title:       "Memory Leak Detected",
				Description: "Continuous memory growth detected",
				Timestamp:   time.Now(),
				Evidence: []domain.Evidence{
					{
						Type:        "metric",
						Source:      domain.SourceK8s,
						Description: "Memory usage increased by 50%",
						Data: map[string]interface{}{
							"pod":             "test-pod",
							"namespace":       "default",
							"memory_increase": "50",
							"time_window":     "2 hours",
						},
						Timestamp: time.Now(),
						Weight:    0.9,
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, insight *HumanInsight) {
				if insight.Severity != "critical" {
					t.Errorf("Expected severity critical, got %s", insight.Severity)
				}
				if !strings.Contains(insight.WhatHappened, "memory leak") {
					t.Errorf("Expected WhatHappened to contain 'memory leak', got %s", insight.WhatHappened)
				}
				if insight.IsUrgent != true {
					t.Errorf("Expected IsUrgent to be true for critical finding")
				}
				if insight.Emoji != "🚨" {
					t.Errorf("Expected critical emoji, got %s", insight.Emoji)
				}
			},
		},
		{
			name: "network_issue_finding",
			finding: &domain.Finding{
				ID:          "test-002",
				Type:        domain.FindingNetworkIssue,
				Severity:    domain.SeverityError,
				Title:       "Network Connectivity Issue",
				Description: "Services unable to communicate",
				Timestamp:   time.Now(),
			},
			wantErr: false,
			validate: func(t *testing.T, insight *HumanInsight) {
				if insight.Severity != "error" {
					t.Errorf("Expected severity error, got %s", insight.Severity)
				}
				if !strings.Contains(strings.ToLower(insight.WhatHappened), "network") {
					t.Errorf("Expected WhatHappened to contain 'network', got %s", insight.WhatHappened)
				}
			},
		},
		{
			name:     "nil_finding",
			finding:  nil,
			wantErr:  true,
			validate: nil,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			insight, err := generator.GenerateInsight(context.Background(), tt.finding)
			
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateInsight() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			
			if !tt.wantErr && tt.validate != nil {
				tt.validate(t, insight)
			}
		})
	}
}

func TestGenerateEventExplanation(t *testing.T) {
	generator := NewGenerator(DefaultConfig())
	
	event := &domain.Event{
		ID:        domain.EventID("test-event-001"),
		Type:      domain.EventTypeNetwork,
		Source:    domain.SourceK8s,
		Severity:  domain.SeverityCritical,
		Timestamp: time.Now(),
		Context: domain.EventContext{
			Namespace: "production",
			Resource: &domain.ResourceRef{
				Kind:      "Pod",
				Name:      "test-pod",
				Namespace: "production",
			},
		},
		Payload: domain.NetworkEventPayload{
			Protocol:          "tcp",
			ConnectionsFailed: 100,
		},
	}
	
	insight, err := generator.GenerateEventExplanation(context.Background(), event)
	if err != nil {
		t.Fatalf("Failed to generate event explanation: %v", err)
	}
	
	// Validate basic properties
	if insight.Severity != "critical" {
		t.Errorf("Expected severity critical, got %s", insight.Severity)
	}
	
	if insight.Timeline == "" {
		t.Errorf("Expected Timeline to be set")
	}
	
	if len(insight.Commands) == 0 {
		t.Errorf("Expected Commands to be populated")
	}
}

func TestGenerateReport(t *testing.T) {
	generator := NewGenerator(DefaultConfig())
	
	findings := []*domain.Finding{
		{
			ID:          "finding-1",
			Type:        domain.FindingMemoryLeak,
			Severity:    domain.SeverityCritical,
			Title:       "Critical Memory Leak",
			Description: "Memory leak in production",
			Timestamp:   time.Now(),
		},
		{
			ID:          "finding-2",
			Type:        domain.FindingNetworkIssue,
			Severity:    domain.SeverityError,
			Title:       "Network Connectivity Issue",
			Description: "Intermittent network failures",
			Timestamp:   time.Now(),
		},
		{
			ID:          "finding-3",
			Type:        domain.FindingAnomalous,
			Severity:    domain.SeverityWarn,
			Title:       "Anomalous Behavior Detected",
			Description: "Unusual pattern in service behavior",
			Timestamp:   time.Now(),
		},
	}
	
	report, err := generator.GenerateReport(context.Background(), findings)
	if err != nil {
		t.Fatalf("Failed to generate report: %v", err)
	}
	
	// Validate report structure
	if report.Title == "" {
		t.Errorf("Expected report title to be set")
	}
	
	if len(report.Insights) != len(findings) {
		t.Errorf("Expected %d insights, got %d", len(findings), len(report.Insights))
	}
	
	if report.OverallHealth == "" {
		t.Errorf("Expected overall health assessment")
	}
	
	if report.Summary == "" {
		t.Errorf("Expected report summary")
	}
}

func TestTemplateMatching(t *testing.T) {
	config := DefaultConfig()
	tm := NewTemplateManager(config)
	
	tests := []struct {
		name     string
		category string
		severity string
		audience string
		wantNil  bool
	}{
		{
			name:     "memory_leak_match",
			category: "memory_leak",
			severity: "critical",
			audience: "developer",
			wantNil:  false,
		},
		{
			name:     "network_issue_match",
			category: "network.failure",
			severity: "critical",
			audience: "developer",
			wantNil:  false,
		},
		{
			name:     "no_match",
			category: "unknown.type",
			severity: "trace",
			audience: "developer",
			wantNil:  true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			template := tm.FindBestTemplate(tt.category, tt.severity, tt.audience)
			
			if (template == nil) != tt.wantNil {
				t.Errorf("FindBestTemplate() returned nil = %v, want nil = %v", 
					template == nil, tt.wantNil)
			}
		})
	}
}

func TestQualityChecks(t *testing.T) {
	generator := NewGenerator(DefaultConfig())
	
	insight := &HumanInsight{
		WhatHappened:  "A simple event occurred in the system.",
		WhyItHappened: "The system detected an issue.",
		WhatItMeans:   "This could affect performance.",
		WhatToDo:      "Check the logs.",
		Severity:      "critical",
		Commands:      []string{"kubectl logs"},
	}
	
	generator.performQualityCheck(insight)
	
	// Validate quality metrics
	if insight.ReadabilityScore == 0 {
		t.Errorf("Expected readability score to be calculated")
	}
	
	if insight.ComplexityScore == 0 {
		t.Errorf("Expected complexity score to be calculated")
	}
	
	if !insight.IsUrgent {
		t.Errorf("Expected critical severity to be marked as urgent")
	}
	
	if !insight.IsActionable {
		t.Errorf("Expected insight with commands to be actionable")
	}
	
	if insight.EstimatedReadTime == 0 {
		t.Errorf("Expected read time to be estimated")
	}
}

func TestVariableExtraction(t *testing.T) {
	generator := NewGenerator(DefaultConfig())
	
	// Test finding variable extraction
	finding := &domain.Finding{
		ID:          "test-finding",
		Type:        domain.FindingMemoryLeak,
		Severity:    domain.SeverityCritical,
		Title:       "Test Finding",
		Timestamp:   time.Now(),
		Evidence: []domain.Evidence{
			{
				Data: map[string]interface{}{
					"pod":       "test-pod",
					"namespace": "test-ns",
				},
			},
		},
	}
	
	vars := generator.extractFindingVariables(finding)
	
	if vars["type"] != "memory_leak" {
		t.Errorf("Expected type to be 'memory_leak', got %s", vars["type"])
	}
	
	if vars["severity"] != "critical" {
		t.Errorf("Expected severity to be 'critical', got %s", vars["severity"])
	}
	
	if vars["pod"] != "test-pod" {
		t.Errorf("Expected pod to be 'test-pod', got %s", vars["pod"])
	}
}

func TestTemplateFilling(t *testing.T) {
	template := "A memory leak was detected in {{.pod}} (namespace: {{.namespace}})"
	variables := map[string]string{
		"pod":       "api-service",
		"namespace": "production",
	}
	
	result := FillTemplate(template, variables)
	expected := "A memory leak was detected in api-service (namespace: production)"
	
	if result != expected {
		t.Errorf("FillTemplate() = %s, want %s", result, expected)
	}
	
	// Test with missing variables
	templateWithMissing := "Pod {{.pod}} in {{.missing}} namespace"
	result = FillTemplate(templateWithMissing, variables)
	
	if !strings.Contains(result, "[unknown]") {
		t.Errorf("Expected missing variable to be replaced with [unknown]")
	}
}