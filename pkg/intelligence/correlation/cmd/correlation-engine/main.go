package correlation
import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
	"github.com/falseyair/tapio/pkg/domain"
	"github.com/falseyair/tapio/pkg/intelligence/correlation/core"
	"github.com/falseyair/tapio/pkg/intelligence/correlation/internal"
)
func main() {
	var (
		configFile     = flag.String("config", "", "Configuration file path")
		bufferSize     = flag.Int("buffer-size", 1000, "Event buffer size")
		timeWindow     = flag.Duration("time-window", 5*time.Minute, "Default correlation time window")
		minConfidence  = flag.Float64("min-confidence", 0.7, "Minimum confidence score")
		maxEvents      = flag.Int("max-events", 50, "Maximum concurrent events")
		mode           = flag.String("mode", "interactive", "Run mode: interactive, test, health")
		testEvents     = flag.String("test-events", "", "JSON file with test events for test mode")
		enablePatterns = flag.String("patterns", "all", "Comma-separated list of patterns to enable (all, memory_leak, cascade_failure, oom_prediction, network_failure)")
		verbose        = flag.Bool("verbose", false, "Enable verbose logging")
	)
	flag.Parse()
	if *verbose {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}
	// Create engine configuration
	config := core.EngineConfig{
		Enabled:              true,
		EventBufferSize:      *bufferSize,
		OutputBufferSize:     100,
		DefaultTimeWindow:    *timeWindow,
		MinConfidenceScore:   *minConfidence,
		MaxConcurrentEvents:  *maxEvents,
		ProcessingTimeout:    30 * time.Second,
		CleanupInterval:      1 * time.Hour,
		EventRetentionTime:   24 * time.Hour,
		AlgorithmWeights:     make(map[string]float64),
	}
	// Load config from file if specified
	if *configFile != "" {
		if err := loadConfigFromFile(*configFile, &config); err != nil {
			log.Fatalf("Failed to load config from file: %v", err)
		}
	}
	log.Printf("Starting correlation engine with mode: %s", *mode)
	log.Printf("Configuration: buffer=%d, window=%v, confidence=%.2f, patterns=%s", 
		config.EventBufferSize, config.DefaultTimeWindow, config.MinConfidenceScore, *enablePatterns)
	// Create and start the correlation engine
	engine, err := internal.NewCorrelationEngine(config)
	if err != nil {
		log.Fatalf("Failed to create correlation engine: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("Received shutdown signal")
		cancel()
	}()
	// Start the engine
	err = engine.Start(ctx)
	if err != nil {
		log.Fatalf("Failed to start correlation engine: %v", err)
	}
	defer func() {
		if err := engine.Stop(); err != nil {
			log.Printf("Error stopping engine: %v", err)
		}
	}()
	log.Println("Correlation engine started successfully")
	// Run based on mode
	switch *mode {
	case "interactive":
		runInteractiveMode(ctx, engine)
	case "test":
		runTestMode(ctx, engine, *testEvents)
	case "health":
		runHealthCheck(engine)
	case "demo":
		runDemoMode(ctx, engine)
	default:
		log.Fatalf("Unknown mode: %s", *mode)
	}
}
func runInteractiveMode(ctx context.Context, engine core.CorrelationEngine) {
	log.Println("Running in interactive mode...")
	log.Println("Commands:")
	log.Println("  health - Show engine health")
	log.Println("  stats - Show engine statistics")
	log.Println("  patterns - List registered patterns")
	log.Println("  analyze - Analyze recent time window")
	log.Println("  demo - Run demo with sample events")
	log.Println("  quit - Exit")
	for {
		select {
		case <-ctx.Done():
			return
		default:
			fmt.Print("> ")
			var command string
			if _, err := fmt.Scanln(&command); err != nil {
				continue
			}
			switch command {
			case "health":
				showHealth(engine)
			case "stats":
				showStatistics(engine)
			case "patterns":
				showPatterns(engine)
			case "analyze":
				analyzeRecentWindow(ctx, engine)
			case "demo":
				runDemo(ctx, engine)
			case "quit", "exit":
				return
			default:
				log.Printf("Unknown command: %s", command)
			}
		}
	}
}
func runTestMode(ctx context.Context, engine core.CorrelationEngine, testEventsFile string) {
	log.Println("Running in test mode...")
	var events []domain.Event
	if testEventsFile != "" {
		var err error
		events, err = loadTestEvents(testEventsFile)
		if err != nil {
			log.Fatalf("Failed to load test events: %v", err)
		}
	} else {
		events = generateSampleEvents()
	}
	log.Printf("Processing %d test events...", len(events))
	// Process events and analyze
	correlations, err := engine.ProcessEvents(ctx, events)
	if err != nil {
		log.Fatalf("Failed to process events: %v", err)
	}
	log.Printf("Found %d correlations:", len(correlations))
	for i, correlation := range correlations {
		log.Printf("  %d. %s (confidence: %.2f, events: %d)", 
			i+1, correlation.Description, correlation.Confidence, len(correlation.Events))
	}
	// Show final statistics
	showStatistics(engine)
}
func runHealthCheck(engine core.CorrelationEngine) {
	log.Println("Running health check...")
	health := engine.Health()
	fmt.Printf("Status: %v\n", health.Status)
	fmt.Printf("Message: %s\n", health.Message)
	fmt.Printf("Events Processed: %d\n", health.EventsProcessed)
	fmt.Printf("Correlations Found: %d\n", health.CorrelationsFound)
	fmt.Printf("Error Count: %d\n", health.ErrorCount)
	fmt.Printf("Buffer Utilization: %.2f%%\n", health.BufferUtilization*100)
	fmt.Printf("Processing Latency: %v\n", health.ProcessingLatency)
	fmt.Printf("Active Patterns: %d\n", health.ActivePatterns)
	if health.Status == core.HealthStatusHealthy {
		os.Exit(0)
	} else {
		os.Exit(1)
	}
}
func runDemoMode(ctx context.Context, engine core.CorrelationEngine) {
	log.Println("Running demo mode with simulated events...")
	// Generate a realistic sequence of events
	events := generateDemoEvents()
	log.Printf("Generated %d demo events", len(events))
	// Process events in batches to simulate real-time
	batchSize := 5
	for i := 0; i < len(events); i += batchSize {
		end := i + batchSize
		if end > len(events) {
			end = len(events)
		}
		batch := events[i:end]
		log.Printf("Processing batch %d-%d...", i+1, end)
		correlations, err := engine.ProcessEvents(ctx, batch)
		if err != nil {
			log.Printf("Error processing batch: %v", err)
			continue
		}
		if len(correlations) > 0 {
			log.Printf("Found %d correlations in batch:", len(correlations))
			for _, correlation := range correlations {
				log.Printf("  - %s (confidence: %.2f)", 
					correlation.Description, correlation.Confidence)
			}
		}
		// Small delay to simulate real-time processing
		time.Sleep(2 * time.Second)
	}
	// Analyze the full time window
	log.Println("Analyzing full time window...")
	start := time.Now().Add(-30 * time.Minute)
	end := time.Now()
	correlations, err := engine.AnalyzeTimeWindow(ctx, start, end)
	if err != nil {
		log.Printf("Error analyzing time window: %v", err)
	} else {
		log.Printf("Total correlations found in time window: %d", len(correlations))
	}
	showStatistics(engine)
}
func showHealth(engine core.CorrelationEngine) {
	health := engine.Health()
	fmt.Printf("Engine Health:\n")
	fmt.Printf("  Status: %v\n", health.Status)
	fmt.Printf("  Message: %s\n", health.Message)
	fmt.Printf("  Events Processed: %d\n", health.EventsProcessed)
	fmt.Printf("  Correlations Found: %d\n", health.CorrelationsFound)
	fmt.Printf("  Error Count: %d\n", health.ErrorCount)
	fmt.Printf("  Buffer Utilization: %.2f%%\n", health.BufferUtilization*100)
	fmt.Printf("  Processing Latency: %v\n", health.ProcessingLatency)
	fmt.Printf("  Active Patterns: %d\n", health.ActivePatterns)
}
func showStatistics(engine core.CorrelationEngine) {
	stats := engine.Statistics()
	fmt.Printf("Engine Statistics:\n")
	fmt.Printf("  Start Time: %v\n", stats.StartTime)
	fmt.Printf("  Events Processed: %d\n", stats.EventsProcessed)
	fmt.Printf("  Correlations Found: %d\n", stats.CorrelationsFound)
	fmt.Printf("  Patterns Matched: %d\n", stats.PatternsMatched)
	fmt.Printf("  Processing Errors: %d\n", stats.ProcessingErrors)
	fmt.Printf("  Average Latency: %v\n", stats.AverageLatency)
	fmt.Printf("  Events Per Second: %.2f\n", stats.EventsPerSecond)
	fmt.Printf("  Correlations Per Hour: %.2f\n", stats.CorrelationsPerHour)
}
func showPatterns(engine core.CorrelationEngine) {
	patterns := engine.ListPatterns()
	fmt.Printf("Registered Patterns (%d):\n", len(patterns))
	for i, pattern := range patterns {
		fmt.Printf("  %d. %s (%s)\n", i+1, pattern.Name(), pattern.ID())
		fmt.Printf("     Category: %v, Priority: %v\n", pattern.Category(), pattern.Priority())
		fmt.Printf("     Time Window: %v, Min Confidence: %.2f\n", 
			pattern.TimeWindow(), pattern.MinConfidence())
	}
}
func analyzeRecentWindow(ctx context.Context, engine core.CorrelationEngine) {
	start := time.Now().Add(-10 * time.Minute)
	end := time.Now()
	fmt.Printf("Analyzing time window: %v to %v\n", start.Format(time.RFC3339), end.Format(time.RFC3339))
	correlations, err := engine.AnalyzeTimeWindow(ctx, start, end)
	if err != nil {
		fmt.Printf("Error analyzing time window: %v\n", err)
		return
	}
	fmt.Printf("Found %d correlations:\n", len(correlations))
	for i, correlation := range correlations {
		fmt.Printf("  %d. %s\n", i+1, correlation.Description)
		fmt.Printf("     Type: %v, Confidence: %.2f\n", correlation.Type, correlation.Confidence)
		fmt.Printf("     Events: %d, Timestamp: %v\n", 
			len(correlation.Events), correlation.Timestamp.Format(time.RFC3339))
	}
}
func runDemo(ctx context.Context, engine core.CorrelationEngine) {
	events := generateSampleEvents()
	fmt.Printf("Processing %d sample events...\n", len(events))
	correlations, err := engine.ProcessEvents(ctx, events)
	if err != nil {
		fmt.Printf("Error processing events: %v\n", err)
		return
	}
	fmt.Printf("Found %d correlations:\n", len(correlations))
	for i, correlation := range correlations {
		fmt.Printf("  %d. %s (confidence: %.2f)\n", 
			i+1, correlation.Description, correlation.Confidence)
	}
}
func loadConfigFromFile(filename string, config *core.EngineConfig) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, config)
}
func loadTestEvents(filename string) ([]domain.Event, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var events []domain.Event
	err = json.Unmarshal(data, &events)
	return events, err
}
func generateSampleEvents() []domain.Event {
	now := time.Now()
	return []domain.Event{
		{
			ID:          "mem-1",
			Source:      domain.SourceEBPF,
			Type:        domain.EventTypeMemory,
			Timestamp:   now.Add(-10 * time.Minute),
			Confidence:  0.9,
			Severity:    domain.SeverityWarn,
			Context: domain.EventContext{
				Host: "prod-server-1",
				Labels: map[string]string{"service": "web-app"},
			},
			Payload: domain.MemoryEventPayload{
				Usage:     85.0,
				Available: 1024 * 1024 * 1024,
				Total:     8 * 1024 * 1024 * 1024,
			},
		},
		{
			ID:          "svc-1",
			Source:      domain.SourceSystemd,
			Type:        domain.EventTypeService,
			Timestamp:   now.Add(-8 * time.Minute),
			Confidence:  0.95,
			Severity:    domain.SeverityError,
			Context: domain.EventContext{
				Host: "prod-server-1",
				Labels: map[string]string{"service": "web-app"},
			},
			Payload: domain.ServiceEventPayload{
				ServiceName: "web-app",
				NewState:       "restarting",
			},
		},
		{
			ID:          "k8s-1",
			Source:      domain.SourceKubernetes,
			Type:        domain.EventTypeKubernetes,
			Timestamp:   now.Add(-5 * time.Minute),
			Confidence:  0.9,
			Severity:    domain.SeverityWarn,
			Context: domain.EventContext{
				Host: "prod-server-1",
				Labels: map[string]string{"namespace": "production", "pod": "web-app-123"},
			},
			Payload: domain.KubernetesEventPayload{
				Resource: domain.ResourceRef{
					Kind:      "Pod",
					Name:      "web-app-123",
					Namespace: "production",
				},
				EventType: "Warning",
				Reason:    "Evicted",
				Message:   "Pod evicted due to memory pressure",
			},
		},
	}
}
func generateDemoEvents() []domain.Event {
	now := time.Now()
	var events []domain.Event
	// Memory leak scenario
	for i := 0; i < 10; i++ {
		usage := 60.0 + float64(i*3) // Gradually increasing memory usage
		events = append(events, domain.Event{
			ID:          domain.EventID(fmt.Sprintf("mem-%d", i)),
			Source:      domain.SourceEBPF,
			Type:        domain.EventTypeMemory,
			Timestamp:   now.Add(-time.Duration(30-i*3) * time.Minute),
			Confidence:  0.9,
			Severity:    getSeverityForUsage(usage),
			Context: domain.EventContext{
				Host: "demo-server",
				Labels: map[string]string{"process": "demo-app"},
			},
			Payload: domain.MemoryEventPayload{
				Usage:     usage,
				Available: uint64((100 - usage) * 1024 * 1024 * 10),
				Total:     uint64(1024 * 1024 * 1024),
			},
		})
	}
	// Service restarts
	events = append(events, domain.Event{
		ID:          "restart-1",
		Source:      domain.SourceSystemd,
		Type:        domain.EventTypeService,
		Timestamp:   now.Add(-15 * time.Minute),
		Confidence:  0.95,
		Severity:    domain.SeverityWarn,
		Context: domain.EventContext{
			Host: "demo-server",
			Labels: map[string]string{"service": "demo-app"},
		},
		Payload: domain.ServiceEventPayload{
			ServiceName: "demo-app",
			NewState:       "restarting",
		},
	})
	// Network issues
	events = append(events, domain.Event{
		ID:          "net-1",
		Source:      domain.SourceEBPF,
		Type:        domain.EventTypeNetwork,
		Timestamp:   now.Add(-12 * time.Minute),
		Confidence:  0.8,
		Severity:    domain.SeverityError,
		Context: domain.EventContext{
			Host: "demo-server",
			Labels: map[string]string{"service": "demo-app"},
		},
		Payload: domain.NetworkEventPayload{
			Protocol:          "tcp",
			SourceIP:          "10.0.0.1",
			DestinationIP:     "10.0.0.2",
			ConnectionsFailed: 5,
			Errors:            3,
		},
	})
	return events
}
func getSeverityForUsage(usage float64) domain.Severity {
	if usage >= 90 {
		return domain.SeverityCritical
	} else if usage >= 80 {
		return domain.SeverityError
	} else if usage >= 70 {
		return domain.SeverityWarn
	} else {
		return domain.SeverityInfo
	}
}