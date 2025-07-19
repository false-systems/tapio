//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"github.com/yairfalse/tapio/pkg/collector"
	"time"
)

func main() {
	// Create formatter for simple explanations
	formatter := collector.NewHumanReadableFormatter(collector.StyleSimple, collector.AudienceDeveloper)

	// Example 1: Memory leak detection
	fmt.Println("=== MEMORY LEAK DETECTION ===")
	memoryLeakInsight := collector.Insight{
		Type:     "pattern:memory_leak",
		Severity: collector.SeverityCritical,
		Title:    "Memory Leak Detected in Payment Service",
		Prediction: &collector.Prediction{
			Confidence:  0.92,
			TimeToEvent: 45 * time.Minute,
		},
	}

	explanation := formatter.FormatInsight(memoryLeakInsight)
	printExplanation("Simple Style", explanation)

	// Switch to technical style
	techFormatter := collector.NewHumanReadableFormatter(collector.StyleTechnical, collector.AudienceDeveloper)
	techExplanation := techFormatter.FormatInsight(memoryLeakInsight)
	printExplanation("Technical Style", techExplanation)

	// Example 2: Complex incident story
	fmt.Println("\n\n=== INCIDENT STORY ===")
	baseTime := time.Now().Add(-30 * time.Minute)

	insights := []collector.Insight{
		{
			Title:       "API Response Times Increasing",
			Description: "95th percentile latency increased from 200ms to 800ms",
			Severity:    collector.SeverityMedium,
			Timestamp:   baseTime,
		},
		{
			Title:       "High Memory Alert",
			Description: "Memory usage exceeded 80%",
			Severity:    collector.SeverityHigh,
			Timestamp:   baseTime.Add(10 * time.Minute),
		},
		{
			Title:       "Memory Leak Confirmed",
			Description: "Pattern analysis confirms memory leak",
			Severity:    collector.SeverityCritical,
			Timestamp:   baseTime.Add(20 * time.Minute),
			Prediction: &collector.Prediction{
				Confidence: 0.88,
			},
		},
	}

	story := formatter.FormatAsStory(insights)
	fmt.Println(story)
}

func printExplanation(style string, exp *collector.HumanReadableExplanation) {
	fmt.Printf("\n--- %s ---\n", style)
	fmt.Printf("What happened: %s\n", exp.WhatHappened)
	fmt.Printf("Why it happened: %s\n", exp.WhyItHappened)
	fmt.Printf("Impact: %s\n", exp.Impact)
	fmt.Printf("What to do: %s\n", exp.WhatToDo)
	fmt.Printf("Urgency: %s\n", exp.Urgency)
	fmt.Printf("Confidence: %.0f%%\n", exp.Confidence*100)

	if len(exp.Commands) > 0 {
		fmt.Println("Commands:")
		for _, cmd := range exp.Commands {
			fmt.Printf("  $ %s\n", cmd)
		}
	}
}

// Run with: go run demo_human_output.go
