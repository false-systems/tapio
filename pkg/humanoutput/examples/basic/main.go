package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/humanoutput"
)

func main() {
	// Create a new generator with default config
	config := humanoutput.DefaultConfig()
	generator := humanoutput.NewGenerator(config)

	// Example 1: Generate insight from a finding
	fmt.Println("=== Example 1: Finding to Human Insight ===")

	finding := &domain.Finding{
		ID:          domain.FindingID("finding-memory-leak-123"),
		Type:        domain.FindingMemoryLeak,
		Title:       "Memory Leak Detected in API Service",
		Description: "API service pod showing continuous memory growth",
		Severity:    domain.SeverityCritical,
		Confidence:  domain.FloatToConfidenceScore(0.95),
		Timestamp:   time.Now(),
		Evidence: []domain.Evidence{
			{
				Type:        "memory_trend",
				Source:      domain.SourceEBPF,
				Description: "Memory usage increased by 45% over 2 hours",
				Data: map[string]interface{}{
					"pod":             "api-service-7b9c4d6f5-xvn2p",
					"namespace":       "production",
					"memory_increase": "45",
					"time_window":     "2 hours",
					"current_memory":  "3.2Gi",
					"memory_limit":    "4Gi",
				},
				Timestamp: time.Now(),
				Weight:    0.9,
			},
			{
				Type:        "behavioral_analysis",
				Source:      domain.SourceK8s,
				Description: "No corresponding increase in request volume",
				Data: map[string]interface{}{
					"requests_per_minute": "stable",
					"gc_frequency":        "increasing",
				},
				Timestamp: time.Now(),
				Weight:    0.8,
			},
		},
		Impact: domain.Impact{
			Scope: []string{"api-service", "production-namespace"},
			Affected: []domain.ResourceRef{
				{
					Kind:      "Pod",
					Name:      "api-service-7b9c4d6f5-xvn2p",
					Namespace: "production",
				},
			},
			Risk:         "service_failure",
			Consequences: []string{"OOM kill imminent", "Service downtime", "User impact"},
		},
	}

	insight, err := generator.GenerateInsight(context.Background(), finding)
	if err != nil {
		log.Fatalf("Failed to generate insight: %v", err)
	}
	printInsight(insight)

	// Example 2: Generate explanation from an event
	fmt.Println("\n=== Example 2: Event to Human Explanation ===")

	event := &domain.Event{
		ID:         domain.EventID("event-network-002"),
		Type:       domain.EventTypeNetwork,
		Source:     domain.SourceK8s,
		Severity:   domain.SeverityCritical,
		Timestamp:  time.Now(),
		Confidence: 0.9,
		Context: domain.EventContext{
			Namespace: "production",
			Container: "frontend-deployment-5d7c8d9f6b-kl9mn",
			Host:      "node-1",
		},
		Payload: domain.NetworkEventPayload{
			Protocol:          "TCP",
			SourceIP:          "10.0.1.100",
			DestinationIP:     "10.0.1.200",
			SourcePort:        8080,
			DestinationPort:   3000,
			ConnectionsFailed: 127,
		},
		Metadata: domain.EventMetadata{
			SchemaVersion: "v1",
			ProcessedAt:   time.Now(),
			ProcessedBy:   "tapio-collector",
			Annotations: map[string]string{
				"source":      "frontend-service",
				"destination": "backend-service",
				"time_window": "5 minutes",
			},
		},
	}

	eventInsight, err := generator.GenerateEventExplanation(context.Background(), event)
	if err != nil {
		log.Fatalf("Failed to generate event explanation: %v", err)
	}
	printInsight(eventInsight)

	// Example 3: Generate report from multiple findings
	fmt.Println("\n=== Example 3: Multiple Findings Report ===")

	findings := []*domain.Finding{
		finding, // Use the memory leak finding from above
		{
			ID:          domain.FindingID("finding-cpu-throttling-456"),
			Type:        domain.FindingType("cpu_throttling"),
			Severity:    domain.SeverityWarn,
			Title:       "CPU Throttling on Worker Pods",
			Description: "Worker pods experiencing CPU throttling",
			Timestamp:   time.Now().Add(-30 * time.Minute),
			Confidence:  domain.FloatToConfidenceScore(0.85),
			Evidence: []domain.Evidence{
				{
					Type:        "cpu_metrics",
					Source:      domain.SourceEBPF,
					Description: "CPU throttling detected on worker pod",
					Data: map[string]interface{}{
						"pod":                 "worker-deployment-8f6d9c5b4-xyz123",
						"namespace":           "production",
						"throttle_percentage": "35",
						"cpu_limit":           "500m",
						"cpu_demand":          "750m",
					},
					Timestamp: time.Now(),
					Weight:    0.8,
				},
			},
			Impact: domain.Impact{
				Scope: []string{"worker-pods"},
				Risk:  "performance_degradation",
			},
		},
		{
			ID:          domain.FindingID("finding-disk-space-789"),
			Type:        domain.FindingType("disk_space_warning"),
			Severity:    domain.SeverityWarn,
			Title:       "Low Disk Space on Database Node",
			Description: "Database node running low on disk space",
			Timestamp:   time.Now().Add(-1 * time.Hour),
			Confidence:  domain.FloatToConfidenceScore(0.9),
			Evidence: []domain.Evidence{
				{
					Type:        "disk_metrics",
					Source:      domain.SourceSystemd,
					Description: "Low disk space detected on database node",
					Data: map[string]interface{}{
						"node":            "db-node-1",
						"disk_percentage": "85",
						"disk_free":       "150GB",
						"growth_rate":     "5",
						"top_consumers":   "logs (45GB), database files (280GB)",
					},
					Timestamp: time.Now(),
					Weight:    0.9,
				},
			},
			Impact: domain.Impact{
				Scope: []string{"database-node"},
				Risk:  "storage_exhaustion",
			},
		},
	}

	report, err := generator.GenerateReport(context.Background(), findings)
	if err != nil {
		log.Fatalf("Failed to generate report: %v", err)
	}
	printReport(report)

	// Example 4: Generate system summary
	fmt.Println("\n=== Example 4: System Summary ===")

	events := []*domain.Event{
		event, // Use the network event from above
		{
			ID:         domain.EventID("event-memory-003"),
			Type:       domain.EventTypeMemory,
			Source:     domain.SourceEBPF,
			Severity:   domain.SeverityWarn,
			Timestamp:  time.Now().Add(-15 * time.Minute),
			Confidence: 0.8,
			Context: domain.EventContext{
				Namespace: "production",
				Container: "api-service-pod",
				Host:      "node-2",
			},
			Payload: domain.MemoryEventPayload{
				Usage:     75.0,
				Available: 1024 * 1024 * 1024,     // 1GB
				Total:     4 * 1024 * 1024 * 1024, // 4GB
			},
		},
	}

	summary, err := generator.GenerateSummary(context.Background(), events)
	if err != nil {
		log.Fatalf("Failed to generate summary: %v", err)
	}
	printSummary(summary)

	// Example 5: Demonstrate JSON output
	demonstrateJSONOutput(insight)
}

func printInsight(insight *humanoutput.HumanInsight) {
	fmt.Printf("%s %s\n", insight.Emoji, insight.Title)
	fmt.Printf("Generated: %s\n", insight.GeneratedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Confidence: %.1f%% | Style: %s | Audience: %s\n",
		insight.Confidence*100, insight.Style, insight.Audience)

	if insight.WhatHappened != "" {
		fmt.Printf("\n🔍 What happened:\n%s\n", insight.WhatHappened)
	}

	if insight.WhyItHappened != "" {
		fmt.Printf("\n🤔 Why it happened:\n%s\n", insight.WhyItHappened)
	}

	if insight.WhatItMeans != "" {
		fmt.Printf("\n📊 What it means:\n%s\n", insight.WhatItMeans)
	}

	if insight.WhatToDo != "" {
		fmt.Printf("\n🛠️  What to do:\n%s\n", insight.WhatToDo)
	}

	if insight.HowToPrevent != "" {
		fmt.Printf("\n🛡️  How to prevent:\n%s\n", insight.HowToPrevent)
	}

	if len(insight.Commands) > 0 {
		fmt.Printf("\n💻 Useful commands:\n")
		for _, cmd := range insight.Commands {
			fmt.Printf("  %s\n", cmd)
		}
	}

	if len(insight.RecommendedActions) > 0 {
		fmt.Printf("\n💡 Recommended actions:\n")
		for _, action := range insight.RecommendedActions {
			fmt.Printf("  [%s] %s\n", action.Priority, action.Title)
			if action.Description != "" {
				fmt.Printf("      %s\n", action.Description)
			}
		}
	}

	fmt.Printf("\n📈 Quality metrics:\n")
	fmt.Printf("  Readability: %.1f | Complexity: %.1f | Read time: %s\n",
		insight.ReadabilityScore, insight.ComplexityScore, insight.EstimatedReadTime)

	if insight.IsUrgent {
		fmt.Printf("⚠️  URGENT: Immediate attention required\n")
	}

	if insight.RequiresEscalation {
		fmt.Printf("📞 ESCALATION: Should be escalated to senior staff\n")
	}
}

func printReport(report *humanoutput.HumanReport) {
	fmt.Printf("📋 %s\n", report.Title)
	fmt.Printf("Generated: %s\n", report.GeneratedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Period: %s to %s\n",
		report.Period.Start.Format("15:04"), report.Period.End.Format("15:04"))
	fmt.Printf("Overall Health: %s\n", report.OverallHealth)

	if report.Summary != "" {
		fmt.Printf("\nSummary:\n%s\n", report.Summary)
	}

	if len(report.Insights) > 0 {
		fmt.Printf("\n🔍 Key Insights (%d total):\n", len(report.Insights))
		for i, insight := range report.Insights {
			fmt.Printf("  %d. [%s] %s\n", i+1, insight.Severity, insight.Title)
		}
	}

	if len(report.Trends) > 0 {
		fmt.Printf("\n📈 Trends:\n")
		for _, trend := range report.Trends {
			fmt.Printf("  • %s: %s (%s)\n", trend.Name, trend.Direction, trend.Description)
		}
	}

	if len(report.Recommendations) > 0 {
		fmt.Printf("\n💡 Recommendations:\n")
		for _, rec := range report.Recommendations {
			fmt.Printf("  • %s\n", rec)
		}
	}

	fmt.Printf("\n⏱️  Estimated read time: %s\n", report.EstimatedReadTime)
}

func printSummary(summary *humanoutput.HumanSummary) {
	fmt.Printf("📊 %s\n", summary.Title)
	fmt.Printf("Generated: %s\n", summary.GeneratedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("\nSystem Health: %s\n", summary.SystemHealth)
	fmt.Printf("\nOverview:\n%s\n", summary.Overview)

	if len(summary.KeyMetrics) > 0 {
		fmt.Printf("\n📈 Key Metrics:\n")
		for key, value := range summary.KeyMetrics {
			fmt.Printf("  - %s: %s\n", key, value)
		}
	}

	if len(summary.ActiveIssues) > 0 {
		fmt.Printf("\n⚠️  Active Issues:\n")
		for _, issue := range summary.ActiveIssues {
			fmt.Printf("  - [%s] %s (Duration: %s)\n",
				issue.Severity, issue.Title, issue.Duration)
		}
	}

	if len(summary.NextSteps) > 0 {
		fmt.Printf("\n👉 Next Steps:\n")
		for _, step := range summary.NextSteps {
			fmt.Printf("  %s\n", step)
		}
	}
}

// Also demonstrate JSON output
func demonstrateJSONOutput(insight *humanoutput.HumanInsight) {
	fmt.Println("\n=== JSON Output Example ===")
	data, err := json.MarshalIndent(insight, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal to JSON: %v", err)
		return
	}
	fmt.Println(string(data))
}
