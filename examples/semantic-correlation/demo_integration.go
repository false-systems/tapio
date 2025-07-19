package main

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/collector"
)

func main() {
	fmt.Println("🚀 TAPIO SEMANTIC CORRELATION - COMPLETE INTEGRATION DEMO")
	fmt.Println("=========================================================")
	fmt.Println()

	// Create the enhanced semantic correlation engine
	engine := collector.NewSemanticCorrelationEngine(100, 100*time.Millisecond)
	ctx := context.Background()
	engine.Start(ctx)

	// Create a human-readable formatter
	formatter := collector.NewHumanReadableFormatter(
		collector.StyleSimple,
		collector.AudienceDeveloper,
	)

	fmt.Println("📊 FEATURE 1: HUMAN-READABLE OUTPUT")
	fmt.Println("===================================")

	// Simulate a memory pressure event
	event1 := collector.Event{
		ID:        "demo-001",
		Type:      collector.EventTypeSystem,
		Severity:  collector.SeverityHigh,
		Timestamp: time.Now(),
		Context: collector.EventContext{
			Host:      "prod-server-1",
			Namespace: "production",
			Pod:       "api-server-xyz",
		},
		Data: map[string]interface{}{
			"type":                 "memory_pressure",
			"memory_usage_percent": 85.5,
			"trend":                "increasing",
		},
	}

	// Create an insight from the event
	insight := collector.Insight{
		ID:          "insight-001",
		Type:        "memory_pressure",
		Severity:    collector.SeverityHigh,
		Title:       "High Memory Usage Detected",
		Description: "Memory usage at 85.5% and increasing rapidly",
		Confidence:  0.95,
		Pattern:     "memory_exhaustion",
		Score:       0.85,
	}

	// Format for human consumption
	humanOutput := formatter.FormatInsight(&insight)
	fmt.Printf("What happened: %s\n", humanOutput.WhatHappened)
	fmt.Printf("Why it matters: %s\n", humanOutput.Impact)
	fmt.Printf("What to do: %s\n", humanOutput.WhatToDo)
	fmt.Printf("Urgency: %s\n", humanOutput.Urgency)
	fmt.Println()

	fmt.Println("📈 FEATURE 2: PREDICTIVE METRICS (would be exposed to Prometheus)")
	fmt.Println("================================================================")
	fmt.Println("# HELP tapio_memory_exhaustion_eta_minutes Predicted time until memory exhaustion")
	fmt.Println("# TYPE tapio_memory_exhaustion_eta_minutes gauge")
	fmt.Println("tapio_memory_exhaustion_eta_minutes{pod=\"api-server-xyz\",namespace=\"production\"} 12.5")
	fmt.Println()
	fmt.Println("# HELP tapio_cascade_failure_risk Risk score for cascade failure (0-1)")
	fmt.Println("# TYPE tapio_cascade_failure_risk gauge")
	fmt.Println("tapio_cascade_failure_risk{namespace=\"production\",root_cause=\"memory_pressure\"} 0.75")
	fmt.Println()

	fmt.Println("🔍 FEATURE 3: SEMANTIC OTEL TRACE CORRELATION")
	fmt.Println("============================================")

	// Get the semantic tracer
	tracer := engine.GetSemanticTracer()
	if tracer != nil {
		fmt.Println("Semantic tracer is active!")
		fmt.Println()
		fmt.Println("Example OTEL trace attributes that would be generated:")
		fmt.Println("- semantic.group_id = \"memory_exhaustion_investigation_12345\"")
		fmt.Println("- semantic.intent = \"memory_exhaustion_investigation\"")
		fmt.Println("- semantic.confidence = 0.95")
		fmt.Println("- correlation.dimension = \"temporal_spatial_causal\"")
		fmt.Println("- impact.business = 0.85")
		fmt.Println("- prediction.scenario = \"oom_kill_cascade\"")
		fmt.Println("- prediction.time_to_outcome_seconds = 750")
	}

	fmt.Println()
	fmt.Println("✨ INTEGRATION SUMMARY")
	fmt.Println("======================")
	fmt.Println("✅ Human-readable output: Technical events → Plain English")
	fmt.Println("✅ Predictive metrics: Future failures → Prometheus metrics")
	fmt.Println("✅ Semantic traces: Related events → Grouped by meaning")
	fmt.Println()
	fmt.Println("🎯 Result: Tapio is the first observability platform that:")
	fmt.Println("   • Explains what's happening in human terms")
	fmt.Println("   • Predicts failures before they occur")
	fmt.Println("   • Understands the semantic meaning of events")
	fmt.Println("   • Provides actionable remediation automatically")

	// Cleanup
	engine.Stop()
}
