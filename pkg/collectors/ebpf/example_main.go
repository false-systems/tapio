package ebpf

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// ExampleMain demonstrates the complete dual-path eBPF collector implementation
func ExampleMain() {
	log.Println("🚀 Starting Tapio eBPF Collector with Dual-Path Architecture")

	// Create dual-path processor configuration
	processorConfig := &ProcessorConfig{
		RawBufferSize:      100000,
		SemanticBufferSize: 10000,
		WorkerCount:        8,
		BatchSize:          1000,
		FlushInterval:      time.Second,
		EnableRawPath:      true, // Enable Hubble-style raw access
		EnableSemanticPath: true, // Enable semantic correlation
		RawRetentionPeriod: 24 * time.Hour,
		RawStorageBackend:  "memory",
		SemanticBatchSize:  100,
		TapioServerAddr:    "localhost:50051",
		MaxMemoryUsage:     1024 * 1024 * 1024, // 1GB
		MetricsInterval:    time.Minute,
	}

	// Create filter configuration for intelligent event processing
	_ = &FilterConfig{
		EnableRawFiltering:   true,
		EnableSemanticFilter: true,
		EnableSampling:       true,
		DefaultSampleRate:    0.1,  // 10% sampling by default
		HighValueSampleRate:  1.0,  // 100% for high-value events
		LowValueSampleRate:   0.01, // 1% for low-value events
		AdaptiveSampling:     true,
		MaxEventsPerSecond:   10000,
		MinImportanceScore:   0.3, // Only send events with importance > 0.3 to semantic layer
		EnableRateLimit:      true,
		GlobalRateLimit:      50000,
		CacheSize:            10000,
		CacheTTL:             5 * time.Minute,
	}

	// Create dual-path processor
	processor := NewDualPathProcessor(processorConfig)

	// Add raw event formatter for raw events
	rawFormatter := NewRawEventFormatter(&RawEventFormatterOptions{
		IncludeTimestamp: true,
		IncludeMetadata:  false,
		ColorOutput:      true,
		VerboseMode:      false,
	})

	// Create raw event sink for formatted output
	rawSink := &RawEventFormatterSink{
		formatter: rawFormatter,
		output:    os.Stdout,
	}
	processor.AddRawEventSink(rawSink)

	// Create semantic event sink for domain events
	semanticSink := &DomainEventSink{
		output: os.Stdout,
	}
	processor.AddSemanticEventSink(semanticSink)

	// Create eBPF collector configuration
	collectorConfig := Config{
		Name:               "dual-path-ebpf-collector",
		Enabled:            true,
		EventBufferSize:    10000,
		EnableNetwork:      true,
		EnableMemory:       true,
		EnableProcess:      true,
		EnableFile:         true,
		RingBufferSize:     64 * 1024,
		EventRateLimit:     10000,
		BatchSize:          100,
		CollectionInterval: time.Millisecond * 100,
		MaxEventsPerSecond: 10000,
		RetentionPeriod:    "24h",
		Timeout:            30 * time.Second,
	}

	// Create eBPF collector with dual-path processing
	collector, err := NewCollector(collectorConfig)
	if err != nil {
		log.Fatalf("Failed to create eBPF collector: %v", err)
	}

	// Start the processor first
	if err := processor.Start(); err != nil {
		log.Fatalf("Failed to start dual-path processor: %v", err)
	}

	// Start the collector
	if err := collector.Start(context.Background()); err != nil {
		log.Fatalf("Failed to start eBPF collector: %v", err)
	}

	log.Println("✅ eBPF Collector with Dual-Path Architecture started successfully!")
	log.Println("📊 Raw events (Hubble-style) will be displayed below")
	log.Println("🧠 Semantic events will be sent to Tapio correlation engine")
	log.Println("🔍 Raw events are also stored for Hubble-style querying")

	// Set up graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start periodic metrics reporting
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				reportMetrics(collector, processor)
			case <-sigCh:
				return
			}
		}
	}()

	// Wait for shutdown signal
	<-sigCh
	log.Println("🛑 Shutting down eBPF collector...")

	// Graceful shutdown
	if err := collector.Stop(); err != nil {
		log.Printf("Error stopping collector: %v", err)
	}

	if err := processor.Stop(); err != nil {
		log.Printf("Error stopping processor: %v", err)
	}

	log.Println("✅ eBPF collector stopped successfully")
}

// RawEventFormatterSink implements RawEventSink for formatted output
type RawEventFormatterSink struct {
	formatter *RawEventFormatter
	output    *os.File
}

func (s *RawEventFormatterSink) Send(ctx context.Context, event *RawEvent) error {
	formatted := s.formatter.FormatEvent(event)
	_, err := s.output.WriteString(formatted + "\n")
	return err
}

func (s *RawEventFormatterSink) SendBatch(ctx context.Context, events []*RawEvent) error {
	for _, event := range events {
		if err := s.Send(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func (s *RawEventFormatterSink) Close() error {
	// Don't close stdout
	return nil
}

// DomainEventSink implements SemanticEventSink for domain events
type DomainEventSink struct {
	output *os.File
}

func (s *DomainEventSink) Send(ctx context.Context, event *domain.Event) error {
	log.Printf("🧠 Semantic Event: %s [%s] %s (importance: %.2f)",
		event.Type, event.Severity, event.Message, event.Confidence)
	return nil
}

func (s *DomainEventSink) SendBatch(ctx context.Context, events []*domain.Event) error {
	for _, event := range events {
		if err := s.Send(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func (s *DomainEventSink) Close() error {
	return nil
}

// reportMetrics prints periodic metrics
func reportMetrics(collector Collector, processor *DualPathProcessor) {
	log.Println("📈 === METRICS REPORT ===")

	// Collector metrics
	collectorStats := collector.Statistics()
	log.Printf("🔧 Collector: events_collected=%v, events_dropped=%v, bytes_processed=%v",
		collectorStats.EventsCollected,
		collectorStats.EventsDropped,
		collectorStats.BytesProcessed)

	// Calculate events per second based on uptime
	uptime := time.Since(collectorStats.StartTime).Seconds()
	if uptime > 0 {
		eventsPerSec := float64(collectorStats.EventsCollected) / uptime
		log.Printf("⚡ Processing Rate: %.1f events/sec", eventsPerSec)
	}

	// Processor metrics
	processorStats := processor.GetStatistics()
	log.Printf("🔄 Processor: raw_processed=%v, semantic_sent=%v, errors=%v",
		processorStats["raw_events_processed"],
		processorStats["semantic_events_sent"],
		processorStats["errors_count"])

	if filterRatio, ok := processorStats["filter_filter_ratio"]; ok {
		log.Printf("🧹 Filter Efficiency: %.1f%% events sent to semantic layer",
			filterRatio.(float64)*100)
	}

	log.Println("📈 === END METRICS ===")
}

// DemoQueryInterface demonstrates Hubble-style querying
func DemoQueryInterface(processor *DualPathProcessor) {
	log.Println("🔍 Demonstrating Hubble-style raw event querying...")

	// Query for network events in the last 5 minutes
	filter := &EventFilter{
		EventTypes: []EventType{EventTypeNetwork},
	}

	events, err := processor.QueryRawEvents(context.Background(), filter)
	if err != nil {
		log.Printf("Error querying events: %v", err)
		return
	}

	log.Printf("📊 Found %d network events in storage", len(events))

	// Format and display first few events
	formatter := NewRawEventFormatter(&RawEventFormatterOptions{
		IncludeTimestamp: true,
		VerboseMode:      true,
		ColorOutput:      false, // For log output
	})

	for i, event := range events {
		if i >= 5 { // Show only first 5 for demo
			break
		}
		formatted := formatter.FormatEvent(event)
		log.Printf("📝 Raw Event %d: %s", i+1, formatted)
	}

	// Demonstrate JSON export (Hubble compatible)
	if len(events) > 0 {
		jsonEvent, err := formatter.FormatEventJSON(events[0])
		if err == nil {
			log.Printf("📋 JSON Export (compatible format):\n%s", jsonEvent)
		}
	}
}

// Integration example with existing eBPF collector
func IntegrateWithExistingCollector() {
	log.Println("🔌 Integrating dual-path processor with existing eBPF collector...")

	// Create existing collector with default config
	existingConfig := DefaultConfig()
	existingCollector, err := NewCollector(existingConfig)
	if err != nil {
		log.Fatalf("Failed to create existing collector: %v", err)
	}

	// Create dual-path processor for enhanced processing
	processorConfig := DefaultProcessorConfig()
	processorConfig.TapioServerAddr = "localhost:50051"
	dualProcessor := NewDualPathProcessor(processorConfig)

	// Start both
	ctx := context.Background()

	if err := existingCollector.Start(ctx); err != nil {
		log.Fatalf("Failed to start existing collector: %v", err)
	}

	if err := dualProcessor.Start(); err != nil {
		log.Fatalf("Failed to start dual processor: %v", err)
	}

	// Bridge events from existing collector to dual processor
	go func() {
		for event := range existingCollector.Events() {
			// Convert domain.Event to RawEvent for dual processing
			rawEvent := convertDomainEventToRaw(&event)
			if err := dualProcessor.ProcessRawEvent(rawEvent); err != nil {
				log.Printf("Error processing event in dual processor: %v", err)
			}
		}
	}()

	log.Println("✅ Integration complete - events flow through both paths")
}

// convertDomainEventToRaw converts domain events to raw events for dual processing
func convertDomainEventToRaw(event *domain.Event) *RawEvent {
	return &RawEvent{
		Type:      EventTypeProcess, // Map from domain.EventType
		Timestamp: uint64(event.Timestamp.UnixNano()),
		PID:       uint32(event.Context.PID),
		UID:       uint32(event.Context.UID),
		GID:       uint32(event.Context.GID),
		Comm:      event.Context.Comm,
		Data:      nil,        // Raw data not available from domain event
		Details:   event.Data, // Use domain event data as details
	}
}
