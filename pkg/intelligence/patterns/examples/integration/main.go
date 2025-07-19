package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/patternrecognition"
)

func main() {
	fmt.Println("=== Tapio Pattern Recognition Integration Example ===\n")

	// Create semantic correlation engine with pattern recognition
	engine := collector.NewSemanticCorrelationEngine(100, 5*time.Second)

	// Configure pattern recognition
	patternConfig := patternrecognition.DefaultConfig()
	patternConfig.EnabledPatterns = []string{"memory_leak"}
	patternConfig.MinConfidenceScore = 0.7
	patternConfig.DefaultTimeWindow = 30 * time.Minute

	err := engine.ConfigurePatterns(patternConfig)
	if err != nil {
		log.Fatalf("Failed to configure patterns: %v", err)
	}

	// Start the engine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = engine.Start(ctx)
	if err != nil {
		log.Fatalf("Failed to start engine: %v", err)
	}
	defer engine.Stop()

	// Create a goroutine to monitor insights
	insightChan := make(chan collector.Insight, 10)
	go func() {
		for insight := range engine.Insights() {
			insightChan <- insight
		}
	}()

	// Simulate a memory leak scenario
	fmt.Println("Simulating memory leak scenario...")
	simulateMemoryLeak(engine)

	// Wait for pattern detection to process events
	fmt.Println("\nWaiting for pattern detection...")
	time.Sleep(6 * time.Second)

	// Collect insights
	var insights []collector.Insight
	timeout := time.After(2 * time.Second)

collectLoop:
	for {
		select {
		case insight := <-insightChan:
			insights = append(insights, insight)
		case <-timeout:
			break collectLoop
		}
	}

	// Display results
	fmt.Printf("\n=== Pattern Detection Results ===\n")
	fmt.Printf("Total insights generated: %d\n\n", len(insights))

	for i, insight := range insights {
		fmt.Printf("Insight #%d:\n", i+1)
		fmt.Printf("  ID: %s\n", insight.ID)
		fmt.Printf("  Type: %s\n", insight.Type)
		fmt.Printf("  Severity: %s\n", insight.Severity)
		fmt.Printf("  Title: %s\n", insight.Title)
		fmt.Printf("  Description: %s\n", insight.Description)
		fmt.Printf("  Timestamp: %s\n", insight.Timestamp.Format(time.RFC3339))
		fmt.Printf("  Related Events: %d\n", len(insight.RelatedEvents))

		if len(insight.Actions) > 0 {
			fmt.Printf("\n  Recommended Actions:\n")
			for j, action := range insight.Actions {
				fmt.Printf("    %d. %s\n", j+1, action.Title)
				fmt.Printf("       Risk: %s\n", action.Risk)
				fmt.Printf("       Impact: %s\n", action.EstimatedImpact)
				if len(action.Commands) > 0 {
					fmt.Printf("       Commands:\n")
					for _, cmd := range action.Commands {
						fmt.Printf("         - %s\n", cmd)
					}
				}
			}
		}

		if insight.Prediction != nil {
			fmt.Printf("\n  Prediction:\n")
			fmt.Printf("    Type: %s\n", insight.Prediction.Type)
			fmt.Printf("    Probability: %.2f\n", insight.Prediction.Probability)
			fmt.Printf("    Confidence: %.2f\n", insight.Prediction.Confidence)
		}

		fmt.Println()
	}

	// Show pattern statistics
	fmt.Println("=== Pattern Recognition Statistics ===")
	stats := engine.GetPatternStats()

	for patternID, matches := range stats.TotalMatches {
		fmt.Printf("\nPattern: %s\n", patternID)
		fmt.Printf("  Total Matches: %d\n", matches)

		if rate, exists := stats.MatchRate[patternID]; exists {
			fmt.Printf("  Match Rate: %.2f%%\n", rate*100)
		}

		if avgConf, exists := stats.AverageConfidence[patternID]; exists {
			fmt.Printf("  Average Confidence: %.2f\n", avgConf)
		}

		if procTime, exists := stats.ProcessingTime[patternID]; exists {
			fmt.Printf("  Avg Processing Time: %v\n", procTime)
		}

		if lastMatch, exists := stats.LastMatchTime[patternID]; exists && !lastMatch.IsZero() {
			fmt.Printf("  Last Match: %s\n", lastMatch.Format(time.RFC3339))
		}
	}
}

func simulateMemoryLeak(engine *collector.SemanticCorrelationEngine) {
	baseTime := time.Now()

	// Stage 1: eBPF detects high memory usage
	sendEvent(engine, collector.Event{
		ID:        fmt.Sprintf("ebpf-%d", time.Now().UnixNano()),
		Timestamp: baseTime,
		Source:    "ebpf",
		Type:      "memory_pressure",
		Severity:  collector.SeverityHigh,
		Data: map[string]interface{}{
			"node":      "worker-node-1",
			"usage":     88.5,
			"available": float64(150 * 1024 * 1024),
			"total":     float64(1024 * 1024 * 1024),
		},
	})

	// Simulate gradual memory increase
	time.Sleep(100 * time.Millisecond)

	// Stage 2: Memory continues to increase
	sendEvent(engine, collector.Event{
		ID:        fmt.Sprintf("ebpf-%d", time.Now().UnixNano()),
		Timestamp: baseTime.Add(2 * time.Minute),
		Source:    "ebpf",
		Type:      "memory_pressure",
		Severity:  collector.SeverityHigh,
		Data: map[string]interface{}{
			"node":      "worker-node-1",
			"usage":     92.0,
			"available": float64(80 * 1024 * 1024),
			"total":     float64(1024 * 1024 * 1024),
		},
	})

	time.Sleep(100 * time.Millisecond)

	// Stage 3: SystemD restarts the service
	sendEvent(engine, collector.Event{
		ID:        fmt.Sprintf("systemd-%d", time.Now().UnixNano()),
		Timestamp: baseTime.Add(3 * time.Minute),
		Source:    "systemd",
		Type:      "service_restart",
		Severity:  collector.SeverityHigh,
		Data: map[string]interface{}{
			"node":       "worker-node-1",
			"service":    "api-service",
			"event_type": "restart",
			"reason":     "memory limit exceeded",
		},
	})

	time.Sleep(100 * time.Millisecond)

	// Stage 4: Kubernetes evicts the pod
	sendEvent(engine, collector.Event{
		ID:        fmt.Sprintf("k8s-%d", time.Now().UnixNano()),
		Timestamp: baseTime.Add(4 * time.Minute),
		Source:    "kubernetes",
		Type:      "pod_evicted",
		Severity:  collector.SeverityCritical,
		Data: map[string]interface{}{
			"node":      "worker-node-1",
			"namespace": "production",
			"pod":       "api-service-7b9c4d6f5-xvn2p",
			"reason":    "Evicted",
			"message":   "The node was low on resource: memory. Container api was using 950Mi, which exceeds its request of 512Mi.",
		},
	})
}

func sendEvent(engine *collector.SemanticCorrelationEngine, event collector.Event) {
	// In a real implementation, events would come from registered collectors
	// For this example, we're simulating events being generated
	fmt.Printf("  Generated event: %s (Type: %s, Severity: %s)\n",
		event.ID, event.Type, event.Severity)

	// Note: In production, events flow from collectors to the engine automatically
	// This is just a simulation showing what events would trigger pattern detection
}
