package main

import (
	"context"
	"fmt"
	"log"
	"time"
	
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/patternrecognition"
)

func main() {
	// Create pattern recognition engine
	config := patternrecognition.DefaultConfig()
	engine := patternrecognition.Engine(config)
	
	// Example 1: Detect memory leak pattern
	fmt.Println("=== Example 1: Memory Leak Detection ===")
	detectMemoryLeak(engine)
	
	// Example 2: Show pattern statistics
	fmt.Println("\n=== Example 2: Pattern Statistics ===")
	showPatternStats(engine)
	
	// Example 3: Register custom pattern
	fmt.Println("\n=== Example 3: Custom Pattern ===")
	registerCustomPattern(engine)
}

func detectMemoryLeak(engine patternrecognition.PatternRecognitionEngine) {
	ctx := context.Background()
	
	// Simulate a memory leak scenario
	baseTime := time.Now().Add(-30 * time.Minute)
	
	events := []domain.Event{
		// eBPF detects high memory usage
		{
			ID:        domain.EventID("ebpf-001"),
			Type:      domain.EventTypeMemory,
			Source:    domain.SourceEBPF,
			Severity:  domain.SeverityWarn,
			Timestamp: baseTime,
			Payload: domain.MemoryEventPayload{
				Usage:     85.0,
				Available: 150 * 1024 * 1024,
				Total:     1024 * 1024 * 1024,
			},
			Context: domain.EventContext{
				Host: "worker-node-1",
			},
			Metadata: domain.EventMetadata{
				SchemaVersion: "v1",
				ProcessedAt:   time.Now(),
				ProcessedBy:   "ebpf-collector",
				Annotations: map[string]string{
					"process": "api-service",
				},
			},
		},
		// Memory usage continues to increase
		{
			ID:        domain.EventID("ebpf-002"),
			Type:      domain.EventTypeMemory,
			Source:    domain.SourceEBPF,
			Severity:  domain.SeverityError,
			Timestamp: baseTime.Add(5 * time.Minute),
			Payload: domain.MemoryEventPayload{
				Usage:     92.0,
				Available: 80 * 1024 * 1024,
				Total:     1024 * 1024 * 1024,
			},
			Context: domain.EventContext{
				Host: "worker-node-1",
			},
			Metadata: domain.EventMetadata{
				SchemaVersion: "v1",
				ProcessedAt:   time.Now(),
				ProcessedBy:   "ebpf-collector",
			},
		},
		// SystemD restarts the service due to memory
		{
			ID:        domain.EventID("systemd-001"),
			Type:      domain.EventTypeService,
			Source:    domain.SourceSystemd,
			Severity:  domain.SeverityError,
			Timestamp: baseTime.Add(8 * time.Minute),
			Payload: domain.ServiceEventPayload{
				ServiceName: "api-service",
				EventType:   "restart",
				OldState:    "running",
				NewState:    "failed",
				ExitCode:    intPtr(137), // SIGKILL
			},
			Context: domain.EventContext{
				Host: "worker-node-1",
			},
			Metadata: domain.EventMetadata{
				SchemaVersion: "v1",
				ProcessedAt:   time.Now(),
				ProcessedBy:   "systemd-collector",
				Annotations: map[string]string{
					"reason": "memory limit exceeded",
				},
			},
		},
		// Kubernetes evicts the pod
		{
			ID:        domain.EventID("k8s-001"),
			Type:      domain.EventTypeKubernetes,
			Source:    domain.SourceK8s,
			Severity:  domain.SeverityCritical,
			Timestamp: baseTime.Add(10 * time.Minute),
			Payload: domain.KubernetesEventPayload{
				Resource: domain.ResourceRef{
					Kind:      "Pod",
					Name:      "api-service-7b9c4d6f5-xvn2p",
					Namespace: "production",
				},
				EventType: "Warning",
				Reason:    "Evicted",
				Message:   "The node was low on resource: memory. Container api was using 950Mi, which exceeds its request of 512Mi.",
				Count:     1,
			},
			Context: domain.EventContext{
				Host:      "worker-node-1",
				Namespace: "production",
			},
			Metadata: domain.EventMetadata{
				SchemaVersion: "v1",
				ProcessedAt:   time.Now(),
				ProcessedBy:   "k8s-collector",
			},
		},
	}
	
	// Detect patterns
	matches, err := engine.DetectPatterns(ctx, events)
	if err != nil {
		log.Fatalf("Failed to detect patterns: %v", err)
	}
	
	// Display results
	fmt.Printf("Detected %d pattern(s)\n", len(matches))
	
	for i, match := range matches {
		fmt.Printf("\nPattern Match #%d:\n", i+1)
		fmt.Printf("  Pattern: %s (%s)\n", match.Pattern.Name, match.Pattern.ID)
		fmt.Printf("  Category: %s\n", match.Pattern.Category)
		fmt.Printf("  Priority: %s\n", match.Pattern.Priority)
		fmt.Printf("  Confidence: %.2f\n", match.Confidence)
		fmt.Printf("  Detected: %s\n", match.Detected.Format("15:04:05"))
		
		fmt.Printf("\n  Correlation:\n")
		fmt.Printf("    Type: %s\n", match.Correlation.Type)
		fmt.Printf("    Description: %s\n", match.Correlation.Description)
		fmt.Printf("    Event Count: %d\n", len(match.Correlation.Events))
		fmt.Printf("    Confidence: %.2f\n", match.Correlation.Confidence.Overall)
		
		fmt.Printf("\n  Events involved:\n")
		for _, eventRef := range match.Correlation.Events {
			fmt.Printf("    - %s (role: %s, weight: %.2f)\n", 
				eventRef.EventID, eventRef.Role, eventRef.Weight)
		}
	}
}

func showPatternStats(engine patternrecognition.PatternRecognitionEngine) {
	stats := engine.GetPatternStats()
	
	fmt.Println("Pattern Statistics:")
	
	// Get supported patterns
	patterns := engine.GetSupportedPatterns()
	
	for _, pattern := range patterns {
		patternID := pattern.ID
		
		fmt.Printf("\n%s (%s):\n", pattern.Name, patternID)
		fmt.Printf("  Enabled: %v\n", pattern.Enabled)
		fmt.Printf("  Total Matches: %d\n", stats.TotalMatches[patternID])
		
		if matchRate, exists := stats.MatchRate[patternID]; exists {
			fmt.Printf("  Match Rate: %.2f%%\n", matchRate*100)
		}
		
		if avgConf, exists := stats.AverageConfidence[patternID]; exists {
			fmt.Printf("  Average Confidence: %.2f\n", avgConf)
		}
		
		if procTime, exists := stats.ProcessingTime[patternID]; exists {
			fmt.Printf("  Avg Processing Time: %v\n", procTime)
		}
		
		if lastMatch, exists := stats.LastMatchTime[patternID]; exists && !lastMatch.IsZero() {
			fmt.Printf("  Last Match: %s\n", lastMatch.Format("15:04:05"))
		}
	}
}

func registerCustomPattern(engine patternrecognition.PatternRecognitionEngine) {
	// Create a simple custom pattern
	customPattern := &SimpleAnomalyPattern{
		BasePattern: patternrecognition.NewBasePattern(
			"simple_anomaly",
			"Simple Anomaly Detection",
			"Detects simple anomalies based on error event clusters",
			patternrecognition.PatternCategoryCustom,
		),
	}
	
	// Configure the pattern
	customPattern.SetTimeWindow(5 * time.Minute)
	customPattern.SetMinConfidence(0.6)
	customPattern.SetPriority(patternrecognition.PatternPriorityMedium)
	
	// Register the pattern
	err := engine.RegisterPattern(customPattern)
	if err != nil {
		log.Printf("Failed to register custom pattern: %v", err)
		return
	}
	
	fmt.Println("Successfully registered custom pattern: Simple Anomaly Detection")
	
	// Test the custom pattern
	ctx := context.Background()
	
	// Create test events
	events := []domain.Event{
		{
			ID:        domain.EventID("error-001"),
			Type:      domain.EventTypeSystem,
			Source:    domain.SourceSystemd,
			Severity:  domain.SeverityError,
			Timestamp: time.Now().Add(-3 * time.Minute),
			Context:   domain.EventContext{Host: "test-host"},
			Metadata:  domain.EventMetadata{SchemaVersion: "v1"},
		},
		{
			ID:        domain.EventID("error-002"),
			Type:      domain.EventTypeSystem,
			Source:    domain.SourceSystemd,
			Severity:  domain.SeverityError,
			Timestamp: time.Now().Add(-2 * time.Minute),
			Context:   domain.EventContext{Host: "test-host"},
			Metadata:  domain.EventMetadata{SchemaVersion: "v1"},
		},
		{
			ID:        domain.EventID("error-003"),
			Type:      domain.EventTypeSystem,
			Source:    domain.SourceSystemd,
			Severity:  domain.SeverityError,
			Timestamp: time.Now().Add(-1 * time.Minute),
			Context:   domain.EventContext{Host: "test-host"},
			Metadata:  domain.EventMetadata{SchemaVersion: "v1"},
		},
	}
	
	matches, err := engine.DetectPatterns(ctx, events)
	if err != nil {
		log.Printf("Failed to detect patterns: %v", err)
		return
	}
	
	fmt.Printf("\nCustom pattern detected %d match(es)\n", len(matches))
}

// SimpleAnomalyPattern is an example custom pattern
type SimpleAnomalyPattern struct {
	*patternrecognition.BasePattern
}

func (p *SimpleAnomalyPattern) Match(ctx context.Context, events []domain.Event) ([]domain.Correlation, error) {
	// Simple logic: detect 3+ error events within time window
	errorEvents := make([]domain.Event, 0)
	
	for _, event := range events {
		if event.Severity >= domain.SeverityError {
			errorEvents = append(errorEvents, event)
		}
	}
	
	if len(errorEvents) >= 3 {
		description := fmt.Sprintf("Detected %d error events in %s time window", 
			len(errorEvents), p.TimeWindow())
		
		correlation := p.CreateCorrelation(errorEvents, 0.7, description)
		return []domain.Correlation{correlation}, nil
	}
	
	return nil, nil
}

func (p *SimpleAnomalyPattern) CanMatch(event domain.Event) bool {
	return event.Severity >= domain.SeverityError
}

// Helper function
func intPtr(i int32) *int32 {
	return &i
}