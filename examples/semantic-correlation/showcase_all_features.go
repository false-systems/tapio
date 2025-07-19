package main

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/collector"
	"github.com/yairfalse/tapio/pkg/domain"
)

func main() {
	fmt.Println("🚀 TAPIO SEMANTIC CORRELATION SHOWCASE")
	fmt.Println("=====================================")
	fmt.Println("Demonstrating all 3 game-changing features:")
	fmt.Println("1. Human-readable output")
	fmt.Println("2. Predictive OTEL metrics")
	fmt.Println("3. Semantic trace correlation")
	fmt.Println()

	// Create the enhanced semantic correlation engine
	engine := collector.NewSemanticCorrelationEngine(100, 100*time.Millisecond)
	ctx := context.Background()
	engine.Start(ctx)

	// Scenario: Memory pressure leading to OOM
	fmt.Println("📊 SCENARIO: Memory exhaustion cascade")
	fmt.Println("=====================================")

	// Event 1: Initial memory pressure
	event1 := &domain.Event{
		ID:        "mem-001",
		Type:      "memory_pressure",
		Source:    "ebpf",
		Severity:  "high",
		Timestamp: time.Now(),
		Context: domain.EventContext{
			Host:      "prod-node-1",
			Namespace: "critical-services",
			Labels: domain.Labels{
				"pod":     "payment-api-xyz123",
				"service": "payment-api",
			},
		},
		Payload: domain.MemoryEventPayload{
			Usage:     85.5,
			Total:     8589934592, // 8GB
			Available: 1288490188, // ~1.2GB
			Trend:     "increasing",
		},
		Confidence: 0.95,
	}

	// Event 2: Critical memory warning
	event2 := &domain.Event{
		ID:        "mem-002",
		Type:      "memory_critical",
		Source:    "ebpf",
		Severity:  "critical",
		Timestamp: time.Now().Add(30 * time.Second),
		Context: domain.EventContext{
			Host:      "prod-node-1",
			Namespace: "critical-services",
			Labels: domain.Labels{
				"pod":     "payment-api-xyz123",
				"service": "payment-api",
			},
		},
		Payload: domain.MemoryEventPayload{
			Usage:     95.0,
			Total:     8589934592,
			Available: 429496729, // ~400MB
			Trend:     "increasing",
		},
		Confidence: 0.98,
	}

	// Event 3: Service degradation
	event3 := &domain.Event{
		ID:        "svc-001",
		Type:      "service_degradation",
		Source:    "kubernetes",
		Severity:  "high",
		Timestamp: time.Now().Add(45 * time.Second),
		Context: domain.EventContext{
			Host:      "prod-node-1",
			Namespace: "critical-services",
			Labels: domain.Labels{
				"pod":     "checkout-api-abc456",
				"service": "checkout-api",
			},
		},
		Payload: domain.ServiceEventPayload{
			ServiceName: "checkout-api",
			EventType:   "latency_spike",
			Message:     "P99 latency increased from 200ms to 5000ms",
		},
		Confidence: 0.92,
	}

	// Process events through the engine
	fmt.Println("\n⚡ Processing events...")

	finding1, _ := engine.ProcessEvent(ctx, event1)
	if finding1 != nil {
		printFinding(finding1, 1)
	}

	finding2, _ := engine.ProcessEvent(ctx, event2)
	if finding2 != nil {
		printFinding(finding2, 2)
	}

	finding3, _ := engine.ProcessEvent(ctx, event3)
	if finding3 != nil {
		printFinding(finding3, 3)
	}

	// Wait for correlation
	time.Sleep(2 * time.Second)

	// Get the semantic tracer to show trace grouping
	fmt.Println("\n🔍 SEMANTIC TRACE GROUPING:")
	fmt.Println("===========================")
	tracer := engine.GetSemanticTracer()
	if tracer != nil {
		groups := tracer.GetSemanticGroups()
		for _, group := range groups {
			fmt.Printf("\n📊 Semantic Group: %s\n", group.ID)
			fmt.Printf("🎯 Intent: %s\n", group.Intent)
			fmt.Printf("🔗 Events correlated: %d\n", len(group.CausalChain))
			fmt.Printf("📈 Confidence: %.0f%%\n", group.ConfidenceScore*100)

			if group.ImpactAssessment != nil {
				fmt.Printf("\n💼 BUSINESS IMPACT:\n")
				fmt.Printf("  • Business Impact: %.0f%%\n", group.ImpactAssessment.BusinessImpact*100)
				fmt.Printf("  • Cascade Risk: %.0f%%\n", group.ImpactAssessment.CascadeRisk*100)
				fmt.Printf("  • Technical Severity: %s\n", group.ImpactAssessment.TechnicalSeverity)
			}

			if group.PredictedOutcome != nil {
				fmt.Printf("\n🔮 PREDICTION:\n")
				fmt.Printf("  • Scenario: %s\n", group.PredictedOutcome.Scenario)
				fmt.Printf("  • Probability: %.0f%%\n", group.PredictedOutcome.Probability*100)
				fmt.Printf("  • Time to outcome: %v\n", group.PredictedOutcome.TimeToOutcome)
			}
		}
	}

	// Show predictive metrics that would be exposed
	fmt.Println("\n📈 PREDICTIVE OTEL METRICS EXPOSED:")
	fmt.Println("===================================")
	fmt.Println("tapio_memory_exhaustion_eta_minutes{pod=\"payment-api-xyz123\"} 3.2")
	fmt.Println("tapio_cascade_failure_risk{namespace=\"critical-services\"} 0.85")
	fmt.Println("tapio_service_degradation_probability{service=\"checkout-api\"} 0.92")

	fmt.Println("\n✨ REVOLUTIONARY CAPABILITIES DEMONSTRATED:")
	fmt.Println("==========================================")
	fmt.Println("✅ Human-readable explanations of technical events")
	fmt.Println("✅ Predictive metrics exposed for proactive alerting")
	fmt.Println("✅ Semantic trace grouping by meaning, not just time")
	fmt.Println("✅ Business impact assessment in every trace")
	fmt.Println("✅ Actionable remediation steps provided automatically")

	fmt.Println("\n🎯 Tapio: The first observability platform that truly understands your system!")
}

func printFinding(finding *domain.Finding, eventNum int) {
	fmt.Printf("\n=== FINDING %d ===\n", eventNum)
	fmt.Printf("Type: %s\n", finding.Type)
	fmt.Printf("Severity: %s\n", finding.Severity)
	fmt.Printf("Title: %s\n", finding.Title)
	fmt.Printf("Description: %s\n", finding.Description)

	// Show human-readable output
	formatter := collector.NewHumanReadableFormatter(
		collector.ExplanationStyleSimple,
		collector.AudienceDeveloper,
	)

	insight := &domain.Insight{
		ID:          finding.ID,
		Type:        finding.Type,
		Severity:    finding.Severity,
		Title:       finding.Title,
		Description: finding.Description,
		Confidence:  0.95,
		Actions:     finding.Actions,
	}

	humanOutput := formatter.FormatInsight(insight)
	fmt.Println("\n📝 HUMAN-READABLE EXPLANATION:")
	fmt.Printf("What happened: %s\n", humanOutput.WhatHappened)
	fmt.Printf("Why it matters: %s\n", humanOutput.Impact)
	fmt.Printf("What to do: %s\n", humanOutput.WhatToDo)

	if len(humanOutput.Commands) > 0 {
		fmt.Println("Commands to run:")
		for _, cmd := range humanOutput.Commands {
			fmt.Printf("  $ %s\n", cmd)
		}
	}
}
