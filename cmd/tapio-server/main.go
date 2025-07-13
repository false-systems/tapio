package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/yairfalse/tapio/pkg/config"
	"github.com/yairfalse/tapio/pkg/grpc"
	"github.com/yairfalse/tapio/pkg/monitoring"
	"github.com/yairfalse/tapio/pkg/shutdown"
	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/yairfalse/tapio/pkg/metrics"
)

const (
	// Version information
	version = "1.0.0"
	
	// Default configuration
	defaultConfigPath = "/etc/tapio/server.yaml"
	defaultPort = 9090
	defaultAddress = "0.0.0.0"
	
	// Resource limits (Deployment pattern)
	defaultMaxMemoryMB = 500  // Higher for server processing
	defaultMaxCPUMilli = 500  // 50% CPU
)

var (
	configPath string
	port       int
	address    string
	logLevel   string
	debug      bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:     "tapio-server",
		Short:   "Central server for Tapio observability platform",
		Long: `Tapio Server is the central processing engine that receives streaming data from lightweight collectors, performs correlation analysis, and provides observability insights.

It processes events from eBPF, Kubernetes, and system sources to detect patterns, predict issues, and provide actionable recommendations.

Features:
- High-performance gRPC streaming (165k+ events/sec)
- Real-time correlation engine with pattern detection
- Automatic backpressure and flow control
- Prometheus metrics integration
- RESTful API for queries and health checks`,
		Version: version,
		RunE:    runServer,
	}

	// Command-line flags
	rootCmd.PersistentFlags().StringVar(&configPath, "config", defaultConfigPath, "Path to configuration file")
	rootCmd.PersistentFlags().IntVar(&port, "port", defaultPort, "gRPC server port")
	rootCmd.PersistentFlags().StringVar(&address, "address", defaultAddress, "Server bind address")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Enable debug mode")

	// Environment variable binding
	viper.SetEnvPrefix("TAPIO_SERVER")
	viper.AutomaticEnv()

	// Execute command
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runServer(cmd *cobra.Command, args []string) error {
	// Initialize configuration
	cfg, err := loadConfiguration()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Setup context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize shutdown handler
	shutdownHandler := shutdown.NewHandler()
	defer shutdownHandler.Shutdown()

	// Setup signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		fmt.Printf("Received signal %v, initiating graceful shutdown...\n", sig)
		shutdownHandler.Initiate()
		cancel()
	}()

	// Initialize resource monitoring
	monitor := monitoring.NewResourceMonitor(monitoring.ResourceLimits{
		MaxMemoryMB: defaultMaxMemoryMB,
		MaxCPUMilli: defaultMaxCPUMilli,
	})
	shutdownHandler.RegisterCleanup("resource-monitor", monitor.Shutdown)

	// Initialize correlation engine
	correlationEngine, err := initializeCorrelationEngine(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize correlation engine: %w", err)
	}
	shutdownHandler.RegisterCleanup("correlation-engine", correlationEngine.Stop)

	// Initialize metrics collector
	metricsCollector, err := initializeMetricsCollector(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize metrics collector: %w", err)
	}
	shutdownHandler.RegisterCleanup("metrics-collector", metricsCollector.Stop)

	// Initialize gRPC server
	grpcServer, err := initializeGRPCServer(cfg, correlationEngine, metricsCollector)
	if err != nil {
		return fmt.Errorf("failed to initialize gRPC server: %w", err)
	}
	shutdownHandler.RegisterCleanup("grpc-server", grpcServer.Stop)

	// Start all components
	fmt.Printf("🚀 Starting Tapio Server v%s\n", version)
	fmt.Printf("   Bind address: %s:%d\n", address, port)
	fmt.Printf("   Config path: %s\n", configPath)
	fmt.Printf("   Resource limits: %dMB memory, %dm CPU\n", defaultMaxMemoryMB, defaultMaxCPUMilli)
	fmt.Printf("   Target throughput: 165,000 events/sec\n")

	// Start resource monitoring
	if err := monitor.Start(ctx); err != nil {
		return fmt.Errorf("failed to start resource monitor: %w", err)
	}

	// Start correlation engine
	if err := correlationEngine.Start(ctx); err != nil {
		return fmt.Errorf("failed to start correlation engine: %w", err)
	}

	// Start metrics collector
	if err := metricsCollector.Start(ctx); err != nil {
		return fmt.Errorf("failed to start metrics collector: %w", err)
	}

	// Start gRPC server
	if err := grpcServer.Start(); err != nil {
		return fmt.Errorf("failed to start gRPC server: %w", err)
	}

	fmt.Printf("✅ Tapio Server started successfully\n")
	fmt.Printf("📡 Listening for collector connections on %s:%d\n", address, port)

	// Setup periodic status reporting
	statusTicker := time.NewTicker(30 * time.Second)
	defer statusTicker.Stop()

	// Main run loop
	for {
		select {
		case <-ctx.Done():
			fmt.Printf("🛑 Server shutdown initiated\n")
			return nil

		case <-statusTicker.C:
			printStatus(monitor, grpcServer, correlationEngine, metricsCollector)

		case <-shutdownHandler.Done():
			fmt.Printf("🏁 Server shutdown completed\n")
			return nil
		}
	}
}

func loadConfiguration() (*ServerConfig, error) {
	// Set configuration defaults
	viper.SetDefault("server.address", defaultAddress)
	viper.SetDefault("server.port", defaultPort)
	viper.SetDefault("server.tls_enabled", true)
	viper.SetDefault("server.max_concurrent_streams", 1000)
	viper.SetDefault("server.max_events_per_sec", 165000)
	viper.SetDefault("server.max_batch_size", 1000)
	
	viper.SetDefault("correlation.enabled", true)
	viper.SetDefault("correlation.buffer_size", 100000)
	viper.SetDefault("correlation.analysis_window", "5m")
	viper.SetDefault("correlation.max_correlation_depth", 10)
	
	viper.SetDefault("metrics.prometheus_enabled", true)
	viper.SetDefault("metrics.prometheus_port", 9091)
	viper.SetDefault("metrics.collection_interval", "15s")
	
	viper.SetDefault("resources.max_memory_mb", defaultMaxMemoryMB)
	viper.SetDefault("resources.max_cpu_milli", defaultMaxCPUMilli)

	// Load configuration file if it exists
	if _, err := os.Stat(configPath); err == nil {
		viper.SetConfigFile(configPath)
		if err := viper.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
		}
		fmt.Printf("📄 Loaded configuration from %s\n", configPath)
	} else {
		fmt.Printf("📄 Using default configuration (file not found: %s)\n", configPath)
	}

	// Create configuration struct
	cfg := &ServerConfig{
		Address:              viper.GetString("server.address"),
		Port:                viper.GetInt("server.port"),
		TLSEnabled:          viper.GetBool("server.tls_enabled"),
		MaxConcurrentStreams: viper.GetUint32("server.max_concurrent_streams"),
		MaxEventsPerSec:     viper.GetUint32("server.max_events_per_sec"),
		MaxBatchSize:        viper.GetUint32("server.max_batch_size"),
		
		Correlation: CorrelationConfig{
			Enabled:              viper.GetBool("correlation.enabled"),
			BufferSize:          viper.GetInt("correlation.buffer_size"),
			AnalysisWindow:      viper.GetDuration("correlation.analysis_window"),
			MaxCorrelationDepth: viper.GetInt("correlation.max_correlation_depth"),
		},
		
		Metrics: MetricsConfig{
			PrometheusEnabled:    viper.GetBool("metrics.prometheus_enabled"),
			PrometheusPort:      viper.GetInt("metrics.prometheus_port"),
			CollectionInterval:  viper.GetDuration("metrics.collection_interval"),
		},
		
		Resources: ResourceConfig{
			MaxMemoryMB: viper.GetInt("resources.max_memory_mb"),
			MaxCPUMilli: viper.GetInt("resources.max_cpu_milli"),
		},
	}

	// Override with command-line flags if provided
	if port != defaultPort {
		cfg.Port = port
	}
	if address != defaultAddress {
		cfg.Address = address
	}

	return cfg, nil
}

func initializeCorrelationEngine(cfg *ServerConfig) (*correlation.Engine, error) {
	correlationConfig := correlation.Config{
		Enabled:              cfg.Correlation.Enabled,
		BufferSize:          cfg.Correlation.BufferSize,
		AnalysisWindow:      cfg.Correlation.AnalysisWindow,
		MaxCorrelationDepth: cfg.Correlation.MaxCorrelationDepth,
		WorkerCount:         4, // Parallel analysis workers
	}

	engine := correlation.NewEngine(correlationConfig)
	
	// Register built-in correlation rules
	if err := engine.RegisterBuiltinRules(); err != nil {
		return nil, fmt.Errorf("failed to register builtin correlation rules: %w", err)
	}

	fmt.Printf("✅ Initialized correlation engine with %d rules\n", engine.RuleCount())
	
	return engine, nil
}

func initializeMetricsCollector(cfg *ServerConfig) (*metrics.Collector, error) {
	metricsConfig := metrics.Config{
		PrometheusEnabled:   cfg.Metrics.PrometheusEnabled,
		PrometheusPort:     cfg.Metrics.PrometheusPort,
		CollectionInterval: cfg.Metrics.CollectionInterval,
	}

	collector := metrics.NewCollector(metricsConfig)
	
	fmt.Printf("✅ Initialized metrics collector (Prometheus: %v, Port: %d)\n", 
		cfg.Metrics.PrometheusEnabled, cfg.Metrics.PrometheusPort)
	
	return collector, nil
}

func initializeGRPCServer(cfg *ServerConfig, engine *correlation.Engine, metricsCollector *metrics.Collector) (*grpc.Server, error) {
	// Create event processor that integrates with correlation engine
	eventProcessor := NewEventProcessor(engine, metricsCollector)

	// Create gRPC server configuration
	grpcConfig := grpc.DefaultServerConfig()
	grpcConfig.Address = cfg.Address
	grpcConfig.Port = cfg.Port
	grpcConfig.TLSEnabled = cfg.TLSEnabled
	grpcConfig.MaxConcurrentStreams = cfg.MaxConcurrentStreams
	grpcConfig.DefaultEventsPerSec = cfg.MaxEventsPerSec
	grpcConfig.MaxBatchSize = cfg.MaxBatchSize

	// Create gRPC server
	server := grpc.NewServer(grpcConfig, eventProcessor)

	fmt.Printf("✅ Initialized gRPC server (TLS: %v, Max streams: %d)\n", 
		cfg.TLSEnabled, cfg.MaxConcurrentStreams)

	return server, nil
}

func printStatus(monitor *monitoring.ResourceMonitor, server *grpc.Server, engine *correlation.Engine, metricsCollector *metrics.Collector) {
	// Get resource usage
	usage := monitor.GetUsage()
	
	// Get server statistics
	serverStats := server.GetStats()
	
	// Get correlation engine status
	engineStats := engine.GetStats()
	
	// Get metrics collector status
	metricsStats := metricsCollector.GetStats()

	fmt.Printf("📊 Status - Memory: %.1fMB, CPU: %.1f%%, Active Connections: %d, Events/sec: %.1f\n",
		usage.MemoryMB,
		usage.CPUPercent,
		serverStats.ActiveConnections,
		serverStats.EventsPerSecond,
	)

	fmt.Printf("🔍 Correlation - Rules: %d, Active correlations: %d, Insights generated: %d\n",
		engineStats.ActiveRules,
		engineStats.ActiveCorrelations,
		engineStats.InsightsGenerated,
	)

	fmt.Printf("📈 Metrics - Prometheus metrics: %d, Collection errors: %d\n",
		metricsStats.MetricsExported,
		metricsStats.CollectionErrors,
	)

	// Check for alerts
	if usage.MemoryMB > float64(defaultMaxMemoryMB)*0.8 {
		fmt.Printf("⚠️  High memory usage: %.1fMB (limit: %dMB)\n", usage.MemoryMB, defaultMaxMemoryMB)
	}
	
	if usage.CPUPercent > float64(defaultMaxCPUMilli)*0.8/10 {
		fmt.Printf("⚠️  High CPU usage: %.1f%% (limit: %.1f%%)\n", usage.CPUPercent, float64(defaultMaxCPUMilli)/10)
	}
}

// Configuration structures
type ServerConfig struct {
	Address              string
	Port                int
	TLSEnabled          bool
	MaxConcurrentStreams uint32
	MaxEventsPerSec     uint32
	MaxBatchSize        uint32
	
	Correlation CorrelationConfig
	Metrics     MetricsConfig
	Resources   ResourceConfig
}

type CorrelationConfig struct {
	Enabled              bool
	BufferSize          int
	AnalysisWindow      time.Duration
	MaxCorrelationDepth int
}

type MetricsConfig struct {
	PrometheusEnabled   bool
	PrometheusPort     int
	CollectionInterval time.Duration
}

type ResourceConfig struct {
	MaxMemoryMB int
	MaxCPUMilli int
}

// EventProcessor integrates with correlation engine and metrics
type EventProcessor struct {
	engine          *correlation.Engine
	metricsCollector *metrics.Collector
	
	// Statistics
	eventsProcessed   uint64
	batchesProcessed  uint64
	processingErrors  uint64
}

func NewEventProcessor(engine *correlation.Engine, metricsCollector *metrics.Collector) *EventProcessor {
	return &EventProcessor{
		engine:          engine,
		metricsCollector: metricsCollector,
	}
}

func (ep *EventProcessor) ProcessEvents(ctx context.Context, events []*grpc.UnifiedEvent) error {
	// Convert gRPC events to correlation engine format
	correlationEvents := make([]*correlation.Event, len(events))
	for i, event := range events {
		correlationEvents[i] = convertToCorrelationEvent(event)
	}
	
	// Process through correlation engine
	if err := ep.engine.ProcessEvents(ctx, correlationEvents); err != nil {
		ep.processingErrors++
		return fmt.Errorf("correlation processing failed: %w", err)
	}
	
	// Update metrics
	ep.metricsCollector.RecordEventsProcessed(len(events))
	ep.eventsProcessed += uint64(len(events))
	
	return nil
}

func (ep *EventProcessor) ProcessEventBatch(ctx context.Context, batch *grpc.EventBatch) (*grpc.EventAck, error) {
	// Process events through correlation engine
	if err := ep.ProcessEvents(ctx, batch.Events); err != nil {
		return nil, err
	}
	
	ep.batchesProcessed++
	
	// Return acknowledgment
	return &grpc.EventAck{
		BatchId:        batch.BatchId,
		ProcessedCount: uint32(len(batch.Events)),
		FailedCount:    0,
	}, nil
}

func (ep *EventProcessor) GetProcessingStats() grpc.ProcessingStats {
	return grpc.ProcessingStats{
		EventsProcessed:   ep.eventsProcessed,
		EventsFailed:      ep.processingErrors,
		BatchesProcessed:  ep.batchesProcessed,
		AvgProcessingTime: time.Microsecond * 100, // Placeholder
		LastProcessedAt:   time.Now(),
		ErrorRate:         float64(ep.processingErrors) / float64(ep.eventsProcessed),
	}
}

func convertToCorrelationEvent(grpcEvent *grpc.UnifiedEvent) *correlation.Event {
	// Convert gRPC event format to correlation engine format
	// This is a simplified conversion - real implementation would be more comprehensive
	return &correlation.Event{
		ID:        grpcEvent.Id,
		Timestamp: grpcEvent.Timestamp.AsTime(),
		Source:    grpcEvent.Source.CollectorType,
		Type:      grpcEvent.Metadata.EventType,
		Severity:  convertSeverity(grpcEvent.Metadata.Severity),
		Data:      convertEventData(grpcEvent),
	}
}

func convertSeverity(grpcSeverity grpc.EventSeverity) correlation.Severity {
	switch grpcSeverity {
	case grpc.EventSeverity_SEVERITY_CRITICAL:
		return correlation.SeverityCritical
	case grpc.EventSeverity_SEVERITY_HIGH:
		return correlation.SeverityHigh
	case grpc.EventSeverity_SEVERITY_MEDIUM:
		return correlation.SeverityMedium
	case grpc.EventSeverity_SEVERITY_LOW:
		return correlation.SeverityLow
	default:
		return correlation.SeverityLow
	}
}

func convertEventData(grpcEvent *grpc.UnifiedEvent) map[string]interface{} {
	data := make(map[string]interface{})
	
	// Convert entity context
	if grpcEvent.Entity != nil {
		data["entity_type"] = grpcEvent.Entity.EntityType.String()
		data["entity_id"] = grpcEvent.Entity.EntityId
		data["entity_name"] = grpcEvent.Entity.EntityName
	}
	
	// Convert specific event data based on type
	switch eventData := grpcEvent.Data.(type) {
	case *grpc.UnifiedEvent_Network:
		data["protocol"] = eventData.Network.Protocol
		data["src_ip"] = eventData.Network.SrcIp
		data["dst_ip"] = eventData.Network.DstIp
		data["src_port"] = eventData.Network.SrcPort
		data["dst_port"] = eventData.Network.DstPort
	case *grpc.UnifiedEvent_Memory:
		data["memory_usage"] = eventData.Memory.MemoryUsage
		data["memory_limit"] = eventData.Memory.MemoryLimit
		data["oom_score"] = eventData.Memory.OomScore
	// Add more event type conversions as needed
	}
	
	// Convert attributes
	for key, value := range grpcEvent.Attributes {
		data[key] = convertAttributeValue(value)
	}
	
	return data
}

func convertAttributeValue(attr *grpc.AttributeValue) interface{} {
	switch value := attr.Value.(type) {
	case *grpc.AttributeValue_StringValue:
		return value.StringValue
	case *grpc.AttributeValue_IntValue:
		return value.IntValue
	case *grpc.AttributeValue_FloatValue:
		return value.FloatValue
	case *grpc.AttributeValue_BoolValue:
		return value.BoolValue
	default:
		return nil
	}
}