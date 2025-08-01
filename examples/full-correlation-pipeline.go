package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/kubeapi"
	"github.com/yairfalse/tapio/pkg/integrations/nats"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
)

// Full end-to-end correlation pipeline example
// Collector → NATS → Correlation Engine → Results
func main() {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	fmt.Println("🚀 Starting Tapio Full Correlation Pipeline Demo")
	fmt.Println("=================================================")

	// 1. Create NATS Event Publisher
	logger.Info("Setting up NATS event publisher...")
	publisher, err := nats.NewEventPublisher(&nats.PublisherConfig{
		URL:        "nats://localhost:4222", // Assumes NATS server running
		StreamName: "TAPIO_DEMO",
		Name:       "demo-publisher",
	})
	if err != nil {
		log.Fatal("Failed to create NATS publisher:", err)
	}
	defer publisher.Close()

	// 2. Create Correlation Engine
	logger.Info("Setting up correlation engine...")
	correlationEngine := correlation.NewMultiDimensionalEngine(
		logger,
		correlation.EngineConfig{
			TemporalWindow:    10 * time.Second,
			CausalWindow:      5 * time.Second,
			MinConfidence:     0.7,
			MinCorrelation:    0.5,
			MaxGraphSize:      1000,
			MaxCorrelations:   100,
			EnableOwnership:   true,
			EnableSpatial:     true,
			EnableTemporal:    true,
			EnableCausal:      true,
			EnableSemantic:    true,
			EnableDependency:  true,
		},
	)

	// 3. Create NATS Correlation Subscriber
	logger.Info("Setting up NATS correlation subscriber...")
	subscriber, err := correlation.NewNATSSubscriber(
		&correlation.NATSSubscriberConfig{
			URL:               "nats://localhost:4222",
			StreamName:        "TAPIO_DEMO",
			Name:              "demo-correlator",
			TraceSubjects:     []string{"traces.>"},
			RawEventSubjects:  []string{"events.raw.>"},
			CorrelationWindow: 5 * time.Second,
			MinEventsForCorr:  2,
			WorkerCount:       4,
			Logger:            logger,
		},
		correlationEngine,
	)
	if err != nil {
		log.Fatal("Failed to create NATS subscriber:", err)
	}
	defer subscriber.Stop()

	// 4. Create Test Collector
	logger.Info("Setting up test collector...")
	collector, err := kubeapi.NewCollector("demo-collector")
	if err != nil {
		log.Fatal("Failed to create collector:", err)
	}

	// 5. Start everything
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start correlation subscriber
	if err := subscriber.Start(ctx); err != nil {
		log.Fatal("Failed to start subscriber:", err)
	}

	// Start collector
	if err := collector.Start(ctx); err != nil {
		log.Fatal("Failed to start collector:", err)
	}

	// 6. Process correlation results
	go func() {
		logger.Info("Starting correlation results processor...")
		for {
			select {
			case results := <-subscriber.Results():
				processCorrelationResults(logger, results)
			case <-ctx.Done():
				return
			}
		}
	}()

	// 7. Generate sample events and publish events from collector
	go func() {
		logger.Info("Starting event generation and publishing...")
		
		eventCount := 0
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case event := <-collector.Events():
				eventCount++
				
				logger.Info("Publishing event from collector",
					zap.Int("count", eventCount),
					zap.String("type", event.Type),
					zap.String("trace_id", event.TraceID))

				// Publish to NATS
				if err := publisher.PublishRawEvent(ctx, event); err != nil {
					logger.Error("Failed to publish event", zap.Error(err))
					continue
				}

			case <-ticker.C:
				// Generate some synthetic correlated events
				generateSyntheticEvents(ctx, publisher, logger, eventCount)
				eventCount += 3

			case <-ctx.Done():
				return
			}
		}
	}()

	// 8. Handle shutdown gracefully
	fmt.Println("\n📡 Pipeline running! Press Ctrl+C to stop...")
	fmt.Println("Expected flow:")
	fmt.Println("  1. Collector generates events with OTEL trace IDs")
	fmt.Println("  2. Events published to NATS (type + trace subjects)")
	fmt.Println("  3. Correlation subscriber groups by trace ID")
	fmt.Println("  4. Correlation engine finds relationships")
	fmt.Println("  5. Results show root causes and impacts")
	fmt.Println()

	// Wait for signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\n🛑 Shutting down pipeline...")
	cancel()
	time.Sleep(2 * time.Second)
	fmt.Println("✅ Pipeline stopped gracefully")
}

// generateSyntheticEvents creates correlated events for demo
func generateSyntheticEvents(ctx context.Context, publisher *nats.EventPublisher, logger *zap.Logger, baseCount int) {
	traceID := fmt.Sprintf("demo-trace-%d", time.Now().Unix())
	
	// Simulate a Pod OOM scenario across multiple components
	events := []collectors.RawEvent{
		{
			Type:      "kubeapi",
			TraceID:   traceID,
			SpanID:    "span-k8s-1",
			Timestamp: time.Now(),
			Data:      []byte(`{"type": "Warning", "reason": "OOMKilling", "object": {"kind": "Pod", "name": "api-server"}}`),
			Metadata:  map[string]string{"namespace": "production"},
		},
		{
			Type:      "systemd",
			TraceID:   traceID,
			SpanID:    "span-systemd-2",
			Timestamp: time.Now().Add(100 * time.Millisecond),
			Data:      []byte(`{"message": "Container process killed by OOM killer", "level": "error"}`),
			Metadata:  map[string]string{"container": "api-server"},
		},
		{
			Type:      "ebpf",
			TraceID:   traceID,
			SpanID:    "span-ebpf-3",
			Timestamp: time.Now().Add(200 * time.Millisecond),
			Data:      []byte(`{"syscall": "kill", "signal": "SIGKILL", "target_pid": "1234"}`),
			Metadata:  map[string]string{"process": "java"},
		},
	}

	logger.Info("Generating synthetic correlated events",
		zap.String("trace_id", traceID),
		zap.Int("event_count", len(events)))

	for i, event := range events {
		if err := publisher.PublishRawEvent(ctx, event); err != nil {
			logger.Error("Failed to publish synthetic event", 
				zap.Int("index", i),
				zap.Error(err))
		}
	}
}

// processCorrelationResults handles correlation results
func processCorrelationResults(logger *zap.Logger, results []*correlation.MultiDimCorrelationResult) {
	fmt.Printf("\n🔗 CORRELATION RESULTS (%d found)\n", len(results))
	fmt.Println("=" + strings.Repeat("=", 50))

	for i, result := range results {
		fmt.Printf("\n📊 Result #%d:\n", i+1)
		fmt.Printf("  🆔 ID: %s\n", result.ID)
		fmt.Printf("  🏷️  Type: %s\n", result.Type)
		fmt.Printf("  📈 Confidence: %.2f\n", result.Confidence)
		
		if result.RootCause != nil {
			fmt.Printf("  🎯 Root Cause: %s\n", result.RootCause.Reasoning)
		}
		
		fmt.Printf("  📅 Created: %s\n", result.CreatedAt.Format("15:04:05.000"))
		
		fmt.Printf("  🔗 Correlated Events: %d\n", len(result.Events))
		
		// Show event types in this correlation
		eventTypes := make(map[string]int)
		for _, eventID := range result.Events {
			// This is simplified - in real implementation you'd look up event details
			eventTypes["various"]++
		}
		
		for eventType, count := range eventTypes {
			fmt.Printf("    - %s: %d events\n", eventType, count)
		}

		if result.Impact != nil {
			fmt.Printf("  💥 Impact:\n")
			fmt.Printf("    - Severity: %s\n", result.Impact.Severity)
		}

		if result.RootCause != nil && len(result.RootCause.Evidence) > 0 {
			fmt.Printf("  🕵️  Evidence:\n")
			for j, evidence := range result.RootCause.Evidence {
				if j < 3 { // Show first 3 pieces of evidence
					fmt.Printf("    - %s\n", evidence)
				}
			}
			if len(result.RootCause.Evidence) > 3 {
				fmt.Printf("    - ... and %d more\n", len(result.RootCause.Evidence)-3)
			}
		}

		if result.Recommendation != "" {
			fmt.Printf("  🛠️  Recommendation: %s\n", result.Recommendation)
		}
	}

	fmt.Println()
	logger.Info("Processed correlation results", zap.Int("count", len(results)))
}