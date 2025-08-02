package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/kubeapi"
	"github.com/yairfalse/tapio/pkg/integrations/nats"
	"github.com/yairfalse/tapio/pkg/integrations/transformer"
)

// Example: Full integration flow with OTEL traces
// Collector → NATS → Transformer → Correlation
func main() {
	// 1. Start NATS server (normally external)
	fmt.Println("🚀 Starting NATS-based event processing pipeline...")

	// 2. Initialize NATS event publisher
	publisher, err := nats.NewEventPublisher(&nats.PublisherConfig{
		URL:        "nats://localhost:4222", // Assumes NATS running locally
		StreamName: "TAPIO_EVENTS",
		Name:       "tapio-publisher",
	})
	if err != nil {
		log.Fatal("Failed to create NATS publisher:", err)
	}
	defer publisher.Close()

	// 3. Initialize event transformer
	transformer := transformer.NewEventTransformer()

	// 4. Create a kubeapi collector with OTEL support
	collector, err := kubeapi.NewCollector("test-kubeapi")
	if err != nil {
		log.Fatal("Failed to create collector:", err)
	}

	// 5. Start collector
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := collector.Start(ctx); err != nil {
		log.Fatal("Failed to start collector:", err)
	}

	fmt.Println("✅ Collector started, processing events...")

	// 6. Process events from collector
	eventCount := 0
	traceGroups := make(map[string][]string) // traceID -> event types

	for {
		select {
		case event := <-collector.Events():
			eventCount++

			fmt.Printf("📥 Raw Event #%d:\n", eventCount)
			fmt.Printf("   Type: %s\n", event.Type)
			fmt.Printf("   TraceID: %s\n", event.TraceID)
			fmt.Printf("   SpanID: %s\n", event.SpanID)

			// Track trace groups for correlation demo
			if event.TraceID != "" {
				traceGroups[event.TraceID] = append(traceGroups[event.TraceID], event.Type)
			}

			// 7. Publish raw event to NATS
			if err := publisher.PublishRawEvent(ctx, event); err != nil {
				log.Printf("❌ Failed to publish raw event: %v", err)
				continue
			}

			// 8. Transform to unified event
			unified, err := transformer.Transform(ctx, event)
			if err != nil {
				log.Printf("❌ Failed to transform event: %v", err)
				continue
			}

			// 9. Publish unified event to NATS
			if err := publisher.PublishUnifiedEvent(ctx, unified); err != nil {
				log.Printf("❌ Failed to publish unified event: %v", err)
				continue
			}

			fmt.Printf("✅ Published to NATS:\n")
			fmt.Printf("   Raw subjects: events.raw.%s, traces.%s\n", event.Type, event.TraceID)
			if unified.TraceContext != nil {
				fmt.Printf("   Unified subjects: events.unified.*, traces.%s\n", unified.TraceContext.TraceID)
			}
			fmt.Printf("   Semantic: %s (%s)\n", unified.Semantic.Intent, unified.Semantic.Category)
			fmt.Println()

			// Stop after processing a few events
			if eventCount >= 5 {
				fmt.Println("🎯 Processed 5 events, stopping...")
				break
			}

		case <-ctx.Done():
			fmt.Println("⏰ Context timeout")
			break
		}
	}

	// Show correlation possibilities
	fmt.Println("\n🔗 Correlation Analysis:")
	fmt.Printf("📊 Total events processed: %d\n", eventCount)
	fmt.Printf("📋 Unique traces: %d\n", len(traceGroups))

	for traceID, eventTypes := range traceGroups {
		fmt.Printf("   Trace %s: %v\n", traceID[:8], eventTypes)
	}

	fmt.Println("\n✨ Next: Correlation engine would subscribe to 'traces.*' and group related events!")
}
