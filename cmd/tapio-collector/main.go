package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/yairfalse/tapio/pkg/collectors/ebpf"
	// K8s and SystemD collectors disabled pending additional fixes
	// "github.com/yairfalse/tapio/pkg/collectors/k8s"
	// "github.com/yairfalse/tapio/pkg/collectors/systemd"
	"github.com/yairfalse/tapio/pkg/dataflow"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

const (
	version = "2.0.0"
)

var (
	// Configuration flags
	serverAddress   string
	otelEndpoint    string
	enableEBPF      bool
	enableK8s       bool
	enableSystemd   bool
	bufferSize      int
	flushInterval   time.Duration
	grpcInsecure    bool
	correlationMode string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "tapio-collector",
		Short: "Tapio Collector with OTEL Semantic Correlation",
		Long: `Tapio Collector v2.0 - Unified collector with semantic correlation

Features:
- Multiple collectors: eBPF, Kubernetes, SystemD
- OTEL semantic correlation with trace context propagation
- Intelligent event grouping and impact assessment
- gRPC streaming to Tapio server
- 165k+ events/sec throughput`,
		Version: version,
		RunE:    runCollector,
	}

	// Collector flags
	rootCmd.Flags().StringVar(&serverAddress, "server", "localhost:9090", "Tapio server address")
	rootCmd.Flags().StringVar(&otelEndpoint, "otel-endpoint", "localhost:4317", "OTEL collector endpoint")
	rootCmd.Flags().BoolVar(&enableEBPF, "enable-ebpf", true, "Enable eBPF collector")
	rootCmd.Flags().BoolVar(&enableK8s, "enable-k8s", true, "Enable Kubernetes collector")
	rootCmd.Flags().BoolVar(&enableSystemd, "enable-systemd", true, "Enable SystemD collector")
	rootCmd.Flags().IntVar(&bufferSize, "buffer-size", 1000, "Event buffer size")
	rootCmd.Flags().DurationVar(&flushInterval, "flush-interval", time.Second, "Event flush interval")
	rootCmd.Flags().BoolVar(&grpcInsecure, "grpc-insecure", true, "Use insecure gRPC connection")
	rootCmd.Flags().StringVar(&correlationMode, "correlation", "semantic", "Correlation mode (semantic|basic)")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runCollector(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Initialize OTEL tracing
	tp, err := initOTEL(ctx)
	if err != nil {
		return fmt.Errorf("failed to initialize OTEL: %w", err)
	}
	defer tp.Shutdown(ctx)

	// Create CollectorManager (manages all collectors)
	manager := NewCollectorManager()

	// Initialize collectors based on flags
	collectorsEnabled := 0

	if enableEBPF {
		// Create eBPF collector with gRPC server connection
		config := ebpf.DefaultConfig()

		collector, err := ebpf.NewCollector(config)
		if err == nil {
			// Create adapter to handle gRPC connection
			ebpfAdapter := &EBPFCollectorAdapter{
				collector:     collector,
				serverAddress: serverAddress,
				eventChan:     make(chan domain.UnifiedEvent, 1000),
			}

			if err := ebpfAdapter.Start(ctx); err != nil {
				log.Printf("⚠️  eBPF adapter failed to start: %v", err)
			} else {
				manager.AddCollector("ebpf", ebpfAdapter)
				collectorsEnabled++
				log.Printf("✅ eBPF collector enabled with gRPC connection to %s", serverAddress)
			}
		} else {
			log.Printf("⚠️  eBPF collector disabled: %v", err)
		}
	}

	// K8s and SystemD collectors disabled pending additional fixes
	/*
		if enableK8s {
			collector, err := k8s.NewCollector(k8s.DefaultConfig())
			if err == nil {
				manager.AddCollector("k8s", collector)
				log.Printf("✅ Kubernetes collector enabled")
				collectorsEnabled++
			} else {
				log.Printf("⚠️  Kubernetes collector disabled: %v", err)
			}
		}

		if enableSystemd {
			collector, err := systemd.NewCollector(systemd.DefaultConfig())
			if err == nil {
				manager.AddCollector("systemd", collector)
				log.Printf("✅ SystemD collector enabled")
				collectorsEnabled++
			} else {
				log.Printf("⚠️  SystemD collector disabled: %v", err)
			}
		}
	*/

	if collectorsEnabled == 0 {
		log.Printf("⚠️  No collectors enabled - check configuration")
	} else {
		log.Printf("🚀 %d collector(s) enabled", collectorsEnabled)
	}

	// Create event channels
	inputEvents := make(chan domain.Event, bufferSize)
	outputEvents := make(chan domain.Event, bufferSize)

	// Create and configure TapioDataFlow for OTEL semantic correlation
	dataFlowConfig := dataflow.Config{
		EnableSemanticGrouping: correlationMode == "semantic",
		GroupRetentionPeriod:   30 * time.Minute,
		ServiceName:            "tapio-collector",
		ServiceVersion:         version,
		Environment:            "production",
		BufferSize:             bufferSize,
		FlushInterval:          flushInterval,
	}

	dataFlow := dataflow.NewTapioDataFlow(dataFlowConfig)
	dataFlow.Connect(inputEvents, outputEvents)

	// Create ServerBridge for forwarding to Tapio server
	bridgeConfig := dataflow.BridgeConfig{
		ServerAddress: serverAddress,
		BufferSize:    bufferSize / 2,
		FlushInterval: flushInterval * 2,
		MaxBatchSize:  100,
		EnableTracing: true,
	}

	bridge, err := dataflow.NewServerBridge(bridgeConfig, dataFlow)
	if err != nil {
		return fmt.Errorf("failed to create server bridge: %w", err)
	}

	// Start all components
	log.Printf("🚀 Starting Tapio Collector v%s", version)
	log.Printf("   Server: %s", serverAddress)
	log.Printf("   OTEL Endpoint: %s", otelEndpoint)
	log.Printf("   Correlation: %s", correlationMode)
	log.Printf("   Buffer Size: %d", bufferSize)

	// Start collector manager
	if err := manager.Start(ctx); err != nil {
		return fmt.Errorf("failed to start collector manager: %w", err)
	}

	// Start data flow
	if err := dataFlow.Start(); err != nil {
		return fmt.Errorf("failed to start data flow: %w", err)
	}

	// Start server bridge
	if err := bridge.Start(); err != nil {
		return fmt.Errorf("failed to start server bridge: %w", err)
	}

	// Create event converter for backward compatibility
	converter := domain.NewEventConverter()

	// Route events from collectors through OTEL semantic correlation
	go func() {
		for unifiedEvent := range manager.Events() {
			// Convert UnifiedEvent to Event for dataflow compatibility
			event := converter.FromUnifiedEvent(&unifiedEvent)
			if event != nil {
				select {
				case inputEvents <- *event:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	// Process enriched events (these go to server via bridge)
	go func() {
		eventCount := 0
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case event := <-outputEvents:
				eventCount++
				// Events are automatically forwarded to server by bridge
				// Log sample events for monitoring
				if eventCount%1000 == 0 {
					log.Printf("📊 Processed %d events, latest: %s", eventCount, event.ID)
				}

			case <-ticker.C:
				// Status report
				stats := manager.Statistics()
				log.Printf("📈 Status: Events=%d, Active Collectors=%d",
					eventCount, stats.ActiveCollectors)

			case <-ctx.Done():
				return
			}
		}
	}()

	log.Printf("✅ Tapio Collector started successfully")

	// Wait for shutdown signal
	<-sigCh
	log.Printf("🛑 Shutting down...")

	// Graceful shutdown
	cancel()

	// Stop components
	manager.Stop()
	dataFlow.Stop()
	bridge.Stop()

	// Close channels
	close(inputEvents)
	close(outputEvents)

	log.Printf("👋 Tapio Collector stopped")
	return nil
}

// initOTEL initializes OpenTelemetry tracing
func initOTEL(ctx context.Context) (*sdktrace.TracerProvider, error) {
	// Create OTLP exporter
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(otelEndpoint),
	}
	if grpcInsecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	exporter, err := otlptrace.New(ctx, otlptracegrpc.NewClient(opts...))
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	// Create trace provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String("tapio-collector"),
			semconv.ServiceVersionKey.String(version),
			semconv.DeploymentEnvironmentKey.String("production"),
		)),
	)

	otel.SetTracerProvider(tp)
	return tp, nil
}

// CollectorManager manages multiple collectors
type CollectorManager struct {
	collectors map[string]Collector
	eventChan  chan domain.UnifiedEvent
	ctx        context.Context
	cancel     context.CancelFunc
}

// Collector interface for all collector types
type Collector interface {
	Start(ctx context.Context) error
	Stop() error
	Events() <-chan domain.UnifiedEvent
	Health() domain.HealthStatus
}

// NewCollectorManager creates a new collector manager
func NewCollectorManager() *CollectorManager {
	return &CollectorManager{
		collectors: make(map[string]Collector),
		eventChan:  make(chan domain.UnifiedEvent, 10000),
	}
}

// AddCollector adds a collector to the manager
func (cm *CollectorManager) AddCollector(name string, collector Collector) {
	cm.collectors[name] = collector
}

// Start starts all collectors
func (cm *CollectorManager) Start(ctx context.Context) error {
	cm.ctx, cm.cancel = context.WithCancel(ctx)

	// Start all collectors
	for name, collector := range cm.collectors {
		if err := collector.Start(cm.ctx); err != nil {
			return fmt.Errorf("failed to start %s collector: %w", name, err)
		}

		// Route events from collector to manager channel
		go func(name string, c Collector) {
			for event := range c.Events() {
				select {
				case cm.eventChan <- event:
				case <-cm.ctx.Done():
					return
				}
			}
		}(name, collector)
	}

	return nil
}

// Stop stops all collectors
func (cm *CollectorManager) Stop() {
	if cm.cancel != nil {
		cm.cancel()
	}

	for name, collector := range cm.collectors {
		if err := collector.Stop(); err != nil {
			log.Printf("Error stopping %s collector: %v", name, err)
		}
	}

	close(cm.eventChan)
}

// Events returns the merged event channel
func (cm *CollectorManager) Events() <-chan domain.UnifiedEvent {
	return cm.eventChan
}

// Statistics returns collector statistics
func (cm *CollectorManager) Statistics() struct {
	ActiveCollectors int
	TotalEvents      int64
} {
	return struct {
		ActiveCollectors int
		TotalEvents      int64
	}{
		ActiveCollectors: len(cm.collectors),
		TotalEvents:      0, // TODO: Track this
	}
}

// EBPFCollectorAdapter adapts the eBPF collector to include gRPC connectivity
type EBPFCollectorAdapter struct {
	collector     ebpf.Collector
	serverAddress string
	eventChan     chan domain.UnifiedEvent
	ctx           context.Context
	cancel        context.CancelFunc
}

// Start starts the eBPF collector with gRPC processor
func (a *EBPFCollectorAdapter) Start(ctx context.Context) error {
	a.ctx, a.cancel = context.WithCancel(ctx)

	// Start the underlying eBPF collector
	if err := a.collector.Start(a.ctx); err != nil {
		return fmt.Errorf("failed to start eBPF collector: %w", err)
	}

	// Note: The eBPF collector already handles event processing internally
	// We just need to bridge the events to our channel

	// Bridge events from collector to processor and output channel
	go func() {
		for {
			select {
			case event, ok := <-a.collector.Events():
				if !ok {
					close(a.eventChan)
					return
				}

				// The event is already a UnifiedEvent from the eBPF collector
				// Just pass it through to the event channel
				select {
				case a.eventChan <- event:
				case <-a.ctx.Done():
					return
				}

			case <-a.ctx.Done():
				return
			}
		}
	}()

	return nil
}

// Stop stops the eBPF collector and processor
func (a *EBPFCollectorAdapter) Stop() error {
	if a.cancel != nil {
		a.cancel()
	}

	if err := a.collector.Stop(); err != nil {
		return fmt.Errorf("failed to stop collector: %w", err)
	}

	return nil
}

// Events returns the event channel
func (a *EBPFCollectorAdapter) Events() <-chan domain.UnifiedEvent {
	return a.eventChan
}

// Health returns the health status
func (a *EBPFCollectorAdapter) Health() domain.HealthStatus {
	// Get health from underlying collector
	health := a.collector.Health()

	// Convert from ebpf.Health to domain.HealthStatus
	switch health.Status {
	case ebpf.HealthStatusHealthy:
		return domain.HealthHealthy
	case ebpf.HealthStatusDegraded:
		return domain.HealthDegraded
	case ebpf.HealthStatusUnhealthy:
		return domain.HealthUnhealthy
	default:
		return domain.HealthUnknown
	}
}
