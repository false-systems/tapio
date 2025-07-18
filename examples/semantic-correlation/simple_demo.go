package main

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/collector"
)

func main() {
	fmt.Println("🚀 TAPIO SEMANTIC CORRELATION FEATURES DEMO")
	fmt.Println("==========================================")
	fmt.Println()

	// Feature 1: Human-Readable Output
	fmt.Println("📝 FEATURE 1: HUMAN-READABLE OUTPUT")
	fmt.Println("===================================")
	
	formatter := collector.NewHumanReadableFormatter(
		collector.StyleSimple,
		collector.AudienceDeveloper,
	)
	
	// Create a sample insight
	insight := collector.Insight{
		ID:          "demo-001",
		Type:        "memory_pressure",
		Severity:    collector.SeverityHigh,
		Title:       "High Memory Usage Detected",
		Description: "Memory usage at 95% on payment-api pod",
		Events:      []collector.Event{},
		Actions: []collector.Action{
			{
				Type:        "command",
				Description: "Check memory usage",
				Command:     "kubectl top pod payment-api-xyz123",
			},
		},
		CreatedAt: time.Now(),
	}
	
	humanOutput := formatter.FormatInsight(insight)
	fmt.Printf("What happened: %s\n", humanOutput.WhatHappened)
	fmt.Printf("Why it matters: %s\n", humanOutput.Impact)
	fmt.Printf("What to do: %s\n", humanOutput.WhatToDo)
	if len(humanOutput.Commands) > 0 {
		fmt.Println("Commands:")
		for _, cmd := range humanOutput.Commands {
			fmt.Printf("  $ %s\n", cmd)
		}
	}
	
	fmt.Println()
	fmt.Println("📈 FEATURE 2: PREDICTIVE METRICS")
	fmt.Println("================================")
	fmt.Println("Metrics that would be exposed to Prometheus:")
	fmt.Println()
	fmt.Println("# Memory exhaustion prediction")
	fmt.Println("tapio_memory_exhaustion_eta_minutes{pod=\"payment-api-xyz123\"} 12.5")
	fmt.Println()
	fmt.Println("# CPU exhaustion prediction")  
	fmt.Println("tapio_cpu_exhaustion_eta_minutes{pod=\"api-gateway-abc456\"} 45.2")
	fmt.Println()
	fmt.Println("# Cascade failure risk")
	fmt.Println("tapio_cascade_failure_risk{namespace=\"production\"} 0.75")
	
	fmt.Println()
	fmt.Println("🔍 FEATURE 3: SEMANTIC OTEL TRACE CORRELATION")
	fmt.Println("============================================")
	fmt.Println("Example trace attributes generated:")
	fmt.Println()
	fmt.Println("Trace 1: Memory Exhaustion Investigation")
	fmt.Println("  semantic.group_id = \"memory_exhaustion_12345\"")
	fmt.Println("  semantic.intent = \"memory_exhaustion_investigation\"")
	fmt.Println("  semantic.confidence = 0.95")
	fmt.Println("  correlation.dimension = \"temporal_spatial_causal\"")
	fmt.Println("  impact.business = 0.85")
	fmt.Println("  prediction.scenario = \"oom_kill_cascade\"")
	fmt.Println()
	fmt.Println("Key Innovation: Events grouped by MEANING, not just time!")
	fmt.Println("  - Memory pressure at T0")
	fmt.Println("  - OOM warning at T0+30s")  
	fmt.Println("  - Service degradation at T0+45s")
	fmt.Println("  → All linked in ONE semantic trace group!")
	
	fmt.Println()
	fmt.Println("✨ INTEGRATION COMPLETE")
	fmt.Println("======================")
	fmt.Println("These 3 features are now integrated into the production")
	fmt.Println("semantic correlation engine, making Tapio the first")
	fmt.Println("observability platform that truly understands your system!")
}