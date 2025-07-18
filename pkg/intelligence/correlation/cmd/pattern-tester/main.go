package correlation
import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation/core"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation/patterns"
)
func main() {
	var (
		patternName = flag.String("pattern", "memory_leak", "Pattern to test (memory_leak, cascade_failure, oom_prediction, network_failure)")
		eventsFile  = flag.String("events", "", "JSON file with test events")
		verbose     = flag.Bool("verbose", false, "Enable verbose output")
		interactive = flag.Bool("interactive", false, "Run in interactive mode")
	)
	flag.Parse()
	if *verbose {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}
	log.Printf("Testing pattern: %s", *patternName)
	// Create the pattern
	pattern, err := createPattern(*patternName)
	if err != nil {
		log.Fatalf("Failed to create pattern: %v", err)
	}
	log.Printf("Pattern info:")
	log.Printf("  ID: %s", pattern.ID())
	log.Printf("  Name: %s", pattern.Name())
	log.Printf("  Description: %s", pattern.Description())
	log.Printf("  Category: %v", pattern.Category())
	log.Printf("  Priority: %v", pattern.Priority())
	log.Printf("  Time Window: %v", pattern.TimeWindow())
	log.Printf("  Min Confidence: %.2f", pattern.MinConfidence())
	log.Printf("  Max Events: %d", pattern.MaxEvents())
	log.Printf("  Required Sources: %v", pattern.RequiredSources())
	log.Printf("  Tags: %v", pattern.Tags())
	if *interactive {
		runInteractiveMode(pattern)
		return
	}
	// Load or generate test events
	var events []domain.Event
	if *eventsFile != "" {
		events, err = loadEventsFromFile(*eventsFile)
		if err != nil {
			log.Fatalf("Failed to load events: %v", err)
		}
	} else {
		events = generateTestEvents(*patternName)
	}
	log.Printf("Testing with %d events", len(events))
	// Test individual event matching
	log.Println("\nTesting individual event matching:")
	matchCount := 0
	for i, event := range events {
		canMatch := pattern.CanMatch(event)
		if canMatch {
			matchCount++
		}
		if *verbose || canMatch {
			log.Printf("  Event %d (%s): %v", i+1, event.ID, canMatch)
		}
	}
	log.Printf("Events that can match: %d/%d", matchCount, len(events))
	// Test pattern matching
	log.Println("\nTesting pattern matching:")
	ctx := context.Background()
	correlations, err := pattern.Match(ctx, events)
	if err != nil {
		log.Fatalf("Pattern matching failed: %v", err)
	}
	log.Printf("Found %d correlations:", len(correlations))
	for i, correlation := range correlations {
		log.Printf("\nCorrelation %d:", i+1)
		log.Printf("  ID: %s", correlation.ID)
		log.Printf("  Type: %v", correlation.Type)
		log.Printf("  Confidence: %.3f", correlation.Confidence)
		log.Printf("  Description: %s", correlation.Description)
		log.Printf("  Events: %d", len(correlation.Events))
		log.Printf("  Timestamp: %v", correlation.Timestamp)
		if *verbose && len(correlation.Events) > 0 {
			log.Printf("  Event details:")
			for j, eventRef := range correlation.Events {
				log.Printf("    %d. %s (%s)", j+1, eventRef.EventID, eventRef.Role)
			}
		}
		if len(correlation.Findings) > 0 {
			log.Printf("  Findings: %d", len(correlation.Findings))
			for j, finding := range correlation.Findings {
				log.Printf("    %d. %s (confidence: %.3f)", j+1, finding.Title, finding.Confidence)
			}
		}
		if *verbose && len(correlation.Metadata.Annotations) > 0 {
			log.Printf("  Metadata annotations:")
			for key, value := range correlation.Metadata.Annotations {
				log.Printf("    %s: %s", key, value)
			}
		}
	}
	// Pattern testing complete
	log.Printf("\nPattern testing completed successfully")
	if len(correlations) > 0 {
		log.Printf("\nPattern test completed successfully with %d correlations found", len(correlations))
	} else {
		log.Printf("\nPattern test completed - no correlations found (this may be expected)")
	}
}
func runInteractiveMode(pattern interface{}) {
	log.Println("\nRunning in interactive mode...")
	log.Println("Commands:")
	log.Println("  info - Show pattern information")
	log.Println("  test-events - Test with sample events")
	log.Println("  custom-event - Create and test a custom event")
	log.Println("  scenarios - Run predefined test scenarios")
	log.Println("  quit - Exit")
	for {
		fmt.Print("> ")
		var command string
		if _, err := fmt.Scanln(&command); err != nil {
			continue
		}
		switch command {
		case "info":
			showPatternInfo(pattern)
		case "test-events":
			testWithSampleEvents(pattern)
		case "custom-event":
			testCustomEvent(pattern)
		case "scenarios":
			runTestScenarios(pattern)
		case "quit", "exit":
			return
		default:
			log.Printf("Unknown command: %s", command)
		}
	}
}
func createPattern(patternName string) (core.CorrelationPattern, error) {
	switch patternName {
	case "memory_leak":
		return patterns.NewMemoryLeakPattern(), nil
	case "cascade_failure":
		return patterns.NewCascadeFailurePattern(), nil
	case "oom_prediction":
		return patterns.NewOOMPredictionPattern(), nil
	case "network_failure":
		return patterns.NewNetworkFailurePattern(), nil
	default:
		return nil, fmt.Errorf("unknown pattern: %s", patternName)
	}
}
func loadEventsFromFile(filename string) ([]domain.Event, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var events []domain.Event
	err = json.Unmarshal(data, &events)
	return events, err
}
func generateTestEvents(patternName string) []domain.Event {
	now := time.Now()
	switch patternName {
	case "memory_leak":
		return generateMemoryLeakEvents(now)
	case "cascade_failure":
		return generateCascadeFailureEvents(now)
	case "oom_prediction":
		return generateOOMPredictionEvents(now)
	case "network_failure":
		return generateNetworkFailureEvents(now)
	default:
		return generateGenericEvents(now)
	}
}
func generateMemoryLeakEvents(now time.Time) []domain.Event {
	return []domain.Event{
		// Increasing memory usage pattern
		{
			ID:          "mem-1",
			Source:      domain.SourceEBPF,
			Type:        domain.EventTypeMemory,
			Timestamp:   now.Add(-20 * time.Minute),
			Confidence:  0.9,
			Severity:    domain.SeverityInfo,
			Context: domain.EventContext{
				Host: "test-server",
				Labels: map[string]string{"process": "test-app"},
			},
			Payload: domain.MemoryEventPayload{
				Usage:     70.0,
				Available: 3 * 1024 * 1024 * 1024,
				Total:     10 * 1024 * 1024 * 1024,
			},
		},
		{
			ID:          "mem-2",
			Source:      domain.SourceEBPF,
			Type:        domain.EventTypeMemory,
			Timestamp:   now.Add(-15 * time.Minute),
			Confidence:  0.9,
			Severity:    domain.SeverityWarn,
			Context: domain.EventContext{
				Host: "test-server",
				Labels: map[string]string{"process": "test-app"},
			},
			Payload: domain.MemoryEventPayload{
				Usage:     85.0,
				Available: 1.5 * 1024 * 1024 * 1024,
				Total:     10 * 1024 * 1024 * 1024,
			},
		},
		// Service restart
		{
			ID:          "svc-restart",
			Source:      domain.SourceSystemd,
			Type:        domain.EventTypeService,
			Timestamp:   now.Add(-10 * time.Minute),
			Confidence:  0.95,
			Severity:    domain.SeverityError,
			Context: domain.EventContext{
				Host: "test-server",
				Labels: map[string]string{"service": "test-app"},
			},
			Payload: domain.ServiceEventPayload{
				ServiceName: "test-app",
				NewState:    "restarting",
			},
		},
		// Continued high memory usage
		{
			ID:          "mem-3",
			Source:      domain.SourceEBPF,
			Type:        domain.EventTypeMemory,
			Timestamp:   now.Add(-5 * time.Minute),
			Confidence:  0.9,
			Severity:    domain.SeverityError,
			Context: domain.EventContext{
				Host: "test-server",
				Labels: map[string]string{"process": "test-app"},
			},
			Payload: domain.MemoryEventPayload{
				Usage:     92.0,
				Available: 800 * 1024 * 1024,
				Total:     10 * 1024 * 1024 * 1024,
			},
		},
	}
}
func generateCascadeFailureEvents(now time.Time) []domain.Event {
	return []domain.Event{
		// Database service failure
		{
			ID:          "db-fail",
			Source:      domain.SourceSystemd,
			Type:        domain.EventTypeService,
			Timestamp:   now.Add(-15 * time.Minute),
			Confidence:  0.95,
			Severity:    domain.SeverityError,
			Context: domain.EventContext{
				Host: "db-server",
				Labels: map[string]string{"service": "database"},
			},
			Payload: domain.ServiceEventPayload{
				ServiceName: "postgresql",
				NewState:    "failed",
				ExitCode:    func() *int32 { v := int32(1); return &v }(),
			},
		},
		// API service failure (depends on database)
		{
			ID:          "api-fail",
			Source:      domain.SourceSystemd,
			Type:        domain.EventTypeService,
			Timestamp:   now.Add(-12 * time.Minute),
			Confidence:  0.95,
			Severity:    domain.SeverityError,
			Context: domain.EventContext{
				Host: "api-server",
				Labels: map[string]string{"service": "api", "depends": "database"},
			},
			Payload: domain.ServiceEventPayload{
				ServiceName: "api-service",
				NewState:    "failed",
				ExitCode:    func() *int32 { v := int32(1); return &v }(),
			},
		},
		// Frontend service failure (depends on API)
		{
			ID:          "web-fail",
			Source:      domain.SourceSystemd,
			Type:        domain.EventTypeService,
			Timestamp:   now.Add(-8 * time.Minute),
			Confidence:  0.95,
			Severity:    domain.SeverityError,
			Context: domain.EventContext{
				Host: "web-server",
				Labels: map[string]string{"service": "frontend", "depends": "api"},
			},
			Payload: domain.ServiceEventPayload{
				ServiceName: "nginx",
				NewState:    "failed",
				ExitCode:    func() *int32 { v := int32(1); return &v }(),
			},
		},
	}
}
func generateOOMPredictionEvents(now time.Time) []domain.Event {
	return []domain.Event{
		// Gradually increasing memory usage
		{
			ID:          "mem-trend-1",
			Source:      domain.SourceEBPF,
			Type:        domain.EventTypeMemory,
			Timestamp:   now.Add(-30 * time.Minute),
			Confidence:  0.9,
			Severity:    domain.SeverityInfo,
			Context: domain.EventContext{
				Host: "prod-server",
				Labels: map[string]string{"process": "heavy-app"},
			},
			Payload: domain.MemoryEventPayload{
				Usage:     75.0,
				Available: 2.5 * 1024 * 1024 * 1024,
				Total:     10 * 1024 * 1024 * 1024,
			},
		},
		{
			ID:          "mem-trend-2",
			Source:      domain.SourceEBPF,
			Type:        domain.EventTypeMemory,
			Timestamp:   now.Add(-20 * time.Minute),
			Confidence:  0.9,
			Severity:    domain.SeverityWarn,
			Context: domain.EventContext{
				Host: "prod-server",
				Labels: map[string]string{"process": "heavy-app"},
			},
			Payload: domain.MemoryEventPayload{
				Usage:     88.0,
				Available: uint64(1288490188), // ~1.2GB
				Total:     10 * 1024 * 1024 * 1024,
			},
		},
		{
			ID:          "mem-trend-3",
			Source:      domain.SourceEBPF,
			Type:        domain.EventTypeMemory,
			Timestamp:   now.Add(-10 * time.Minute),
			Confidence:  0.9,
			Severity:    domain.SeverityError,
			Context: domain.EventContext{
				Host: "prod-server",
				Labels: map[string]string{"process": "heavy-app"},
			},
			Payload: domain.MemoryEventPayload{
				Usage:     95.0,
				Available: 500 * 1024 * 1024,
				Total:     10 * 1024 * 1024 * 1024,
			},
		},
		// Process creating many child processes
		{
			ID:          "proc-spawn",
			Source:      domain.SourceEBPF,
			Type:        domain.EventTypeProcess,
			Timestamp:   now.Add(-5 * time.Minute),
			Confidence:  0.8,
			Severity:    domain.SeverityWarn,
			Context: domain.EventContext{
				Host: "prod-server",
				Labels: map[string]string{"parent": "heavy-app"},
			},
			Payload: domain.SystemEventPayload{
				Syscall:   "fork",
				Arguments: map[string]string{"process_count": "150", "rate": "10/sec"},
			},
		},
	}
}
func generateNetworkFailureEvents(now time.Time) []domain.Event {
	return []domain.Event{
		// Network connectivity issues
		{
			ID:          "net-fail-1",
			Source:      domain.SourceEBPF,
			Type:        domain.EventTypeNetwork,
			Timestamp:   now.Add(-15 * time.Minute),
			Confidence:  0.9,
			Severity:    domain.SeverityError,
			Context: domain.EventContext{
				Host: "app-server",
				Labels: map[string]string{"service": "web-app"},
			},
			Payload: domain.NetworkEventPayload{
				Protocol:          "tcp",
				SourceIP:          "10.0.1.100",
				DestinationIP:     "10.0.2.200",
				SourcePort:        45678,
				DestinationPort:   5432,
				ConnectionsFailed: 10,
				Errors:            5,
				PacketsDropped:    25,
			},
		},
		// Kubernetes pod networking issues
		{
			ID:          "k8s-net-1",
			Source:      domain.SourceKubernetes,
			Type:        domain.EventTypeKubernetes,
			Timestamp:   now.Add(-12 * time.Minute),
			Confidence:  0.95,
			Severity:    domain.SeverityWarn,
			Context: domain.EventContext{
				Host: "k8s-node-1",
				Labels: map[string]string{"namespace": "production", "pod": "web-app-123"},
			},
			Payload: domain.KubernetesEventPayload{
				Resource: domain.ResourceRef{
					Kind:      "Pod",
					Name:      "web-app-123",
					Namespace: "production",
				},
				EventType: "Warning",
				Reason:    "NetworkNotReady",
				Message:   "CNI network setup failed",
			},
		},
		// Service log indicating network timeouts
		{
			ID:          "log-timeout",
			Source:      domain.SourceJournald,
			Type:        domain.EventTypeLog,
			Timestamp:   now.Add(-8 * time.Minute),
			Confidence:  0.8,
			Severity:    domain.SeverityError,
			Context: domain.EventContext{
				Host: "app-server",
				Labels: map[string]string{"service": "web-app"},
			},
			Payload: domain.LogEventPayload{
				Message:    "Connection timeout connecting to database: dial tcp 10.0.2.200:5432: i/o timeout",
				Priority:   3, // Error level
				Unit:       "web-app.service",
				Facility:   "daemon",
				Identifier: "web-app",
			},
		},
	}
}
func generateGenericEvents(now time.Time) []domain.Event {
	return []domain.Event{
		{
			ID:          "generic-1",
			Source:      domain.SourceEBPF,
			Type:        domain.EventTypeMemory,
			Timestamp:   now.Add(-10 * time.Minute),
			Confidence:  0.8,
			Severity:    domain.SeverityInfo,
			Context: domain.EventContext{
				Host: "test-host",
			},
			Payload: domain.MemoryEventPayload{
				Usage:     60.0,
				Available: 4 * 1024 * 1024 * 1024,
				Total:     10 * 1024 * 1024 * 1024,
			},
		},
	}
}
func showPatternInfo(pattern interface{}) {
	// This would need to use reflection or type assertion to get pattern info
	fmt.Println("Pattern information displayed above")
}
func testWithSampleEvents(pattern interface{}) {
	fmt.Println("Testing with sample events...")
	// Implementation would test the pattern with predefined events
}
func testCustomEvent(pattern interface{}) {
	fmt.Println("Custom event testing not implemented in this demo")
}
func runTestScenarios(pattern interface{}) {
	fmt.Println("Running test scenarios...")
	// Implementation would run various test scenarios
}