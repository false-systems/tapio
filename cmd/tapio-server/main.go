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
	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/yairfalse/tapio/pkg/grpc"
	"github.com/yairfalse/tapio/pkg/metrics"
	"github.com/yairfalse/tapio/pkg/monitoring"
	"github.com/yairfalse/tapio/pkg/server/adapters/correlation"
	"github.com/yairfalse/tapio/pkg/server/api"
	"github.com/yairfalse/tapio/pkg/server/logging"
	"github.com/yairfalse/tapio/pkg/shutdown"
)

const (
	// Version information
	version = "1.0.0"

	// Default configuration
	defaultConfigPath = "/etc/tapio/server.yaml"
	defaultGRPCPort   = 9090
	defaultRESTPort   = 8080
	defaultAddress    = "0.0.0.0"

	// Resource limits (Deployment pattern)
	defaultMaxMemoryMB = 500 // Higher for server processing
	defaultMaxCPUMilli = 500 // 50% CPU
)

var (
	configPath   string
	grpcPort     int
	restPort     int
	restEnabled  bool
	grpcEnabled  bool
	address      string
	logLevel     string
	debug        bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "tapio-server",
		Short: "Central server for Tapio observability platform",
		Long: `Tapio Server is the central processing engine that receives streaming data from lightweight collectors, performs correlation analysis, and provides observability insights.

It processes events from eBPF, Kubernetes, and system sources to detect patterns, predict issues, and provide actionable recommendations.

Features:
- High-performance gRPC streaming (165k+ events/sec)
- Real-time correlation engine with pattern detection
- REST API for CLI and GUI connectivity
- Automatic backpressure and flow control
- Prometheus metrics integration
- Dual protocol support (gRPC and REST)`,
		Version: version,
		RunE:    runServer,
	}

	// Command-line flags
	rootCmd.PersistentFlags().StringVar(&configPath, "config", defaultConfigPath, "Path to configuration file")
	rootCmd.PersistentFlags().IntVar(&grpcPort, "grpc-port", defaultGRPCPort, "gRPC server port")
	rootCmd.PersistentFlags().IntVar(&restPort, "rest-port", defaultRESTPort, "REST API server port")
	rootCmd.PersistentFlags().BoolVar(&restEnabled, "rest-enabled", true, "Enable REST API server")
	rootCmd.PersistentFlags().BoolVar(&grpcEnabled, "grpc-enabled", true, "Enable gRPC server")
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

	// Initialize perfect correlation engine optimized for opinionated data
	correlationEngine, err := initializePerfectCorrelationEngine(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize perfect correlation engine: %w", err)
	}
	shutdownHandler.RegisterCleanup("perfect-correlation-engine", correlationEngine.Stop)

	// Initialize metrics collector
	metricsCollector, err := initializeMetricsCollector(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize metrics collector: %w", err)
	}
	shutdownHandler.RegisterCleanup("metrics-collector", metricsCollector.Stop)

	// Initialize correlation adapter for REST API
	var correlationAdapter *correlation.CorrelationAdapter
	var restServer *api.RESTServer
	
	if restEnabled {
		// Create logger for correlation adapter
		logger := logging.NewZapLogger("info")
		correlationAdapter = correlation.NewCorrelationAdapter(logger)
		correlationAdapter.Enable()
		
		// Initialize REST API server
		restServer = api.NewRESTServer(restPort, correlationAdapter)
		shutdownHandler.RegisterCleanup("rest-server", func() error {
			return restServer.Stop(ctx)
		})
	}

	// Initialize gRPC server
	var grpcServer *grpc.Server
	if grpcEnabled {
		grpcServer, err = initializeGRPCServer(cfg, correlationEngine, metricsCollector)
		if err != nil {
			return fmt.Errorf("failed to initialize gRPC server: %w", err)
		}
		shutdownHandler.RegisterCleanup("grpc-server", grpcServer.Stop)
	}

	// Start all components
	fmt.Printf("🚀 Starting Tapio Server v%s\n", version)
	fmt.Printf("   Bind address: %s\n", address)
	if grpcEnabled {
		fmt.Printf("   gRPC port: %d\n", grpcPort)
	}
	if restEnabled {
		fmt.Printf("   REST port: %d\n", restPort)
	}
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
	if grpcEnabled && grpcServer != nil {
		if err := grpcServer.Start(); err != nil {
			return fmt.Errorf("failed to start gRPC server: %w", err)
		}
	}
	
	// Start REST API server
	if restEnabled && restServer != nil {
		go func() {
			fmt.Printf("📡 Starting REST API server on port %d\n", restPort)
			if err := restServer.Start(ctx); err != nil {
				fmt.Printf("❌ REST API server error: %v\n", err)
			}
		}()
	}

	fmt.Printf("✅ Tapio Server started successfully\n")
	if grpcEnabled {
		fmt.Printf("📡 Listening for collector connections on %s:%d (gRPC)\n", address, grpcPort)
	}
	if restEnabled {
		fmt.Printf("🌐 REST API available on %s:%d\n", address, restPort)
	}

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
		GRPCPort:            viper.GetInt("server.grpc_port"),
		RESTPort:            viper.GetInt("server.rest_port"),
		RESTEnabled:         viper.GetBool("server.rest_enabled"),
		GRPCEnabled:         viper.GetBool("server.grpc_enabled"),
		TLSEnabled:          viper.GetBool("server.tls_enabled"),
		MaxConcurrentStreams: viper.GetUint32("server.max_concurrent_streams"),
		MaxEventsPerSec:     viper.GetUint32("server.max_events_per_sec"),
		MaxBatchSize:        viper.GetUint32("server.max_batch_size"),

		Correlation: CorrelationConfig{
			Enabled:             viper.GetBool("correlation.enabled"),
			BufferSize:          viper.GetInt("correlation.buffer_size"),
			AnalysisWindow:      viper.GetDuration("correlation.analysis_window"),
			MaxCorrelationDepth: viper.GetInt("correlation.max_correlation_depth"),
		},

		Metrics: MetricsConfig{
			PrometheusEnabled:  viper.GetBool("metrics.prometheus_enabled"),
			PrometheusPort:     viper.GetInt("metrics.prometheus_port"),
			CollectionInterval: viper.GetDuration("metrics.collection_interval"),
		},

		Resources: ResourceConfig{
			MaxMemoryMB: viper.GetInt("resources.max_memory_mb"),
			MaxCPUMilli: viper.GetInt("resources.max_cpu_milli"),
		},
		
		REST: RESTConfig{
			Enabled:      viper.GetBool("rest.enabled"),
			Port:         viper.GetInt("rest.port"),
			Host:         viper.GetString("rest.host"),
			EnableCORS:   viper.GetBool("rest.enable_cors"),
			ReadTimeout:  viper.GetDuration("rest.read_timeout"),
			WriteTimeout: viper.GetDuration("rest.write_timeout"),
		},
	}

	// Override with command-line flags if provided
	if grpcPort != defaultGRPCPort {
		cfg.GRPCPort = grpcPort
	}
	if restPort != defaultRESTPort {
		cfg.RESTPort = restPort
		// Also update REST config
		cfg.REST.Port = restPort
	}
	if address != defaultAddress {
		cfg.Address = address
		cfg.REST.Host = address
	}
	// Override enable flags
	cfg.RESTEnabled = restEnabled
	cfg.GRPCEnabled = grpcEnabled
	cfg.REST.Enabled = restEnabled

	return cfg, nil
}

func initializePerfectCorrelationEngine(cfg *ServerConfig) (*correlation.PerfectEngine, error) {
	// Create perfect configuration optimized for opinionated data
	perfectConfig := &correlation.PerfectConfig{
		// Semantic correlation optimized for our format
		SemanticSimilarityThreshold: 0.85, // High precision for quality data
		SemanticEmbeddingDimension:  512,  // Standard embedding size
		OntologyTagWeight:           0.7,  // Strong weight for curated ontology
		IntentCorrelationEnabled:    true, // Leverage intent classification

		// Behavioral correlation for entity intelligence
		BehavioralAnomalyThreshold: 0.7,  // Early anomaly detection
		EntityTrustThreshold:       0.6,  // Moderate trust threshold
		BehaviorVectorDimension:    256,  // Optimized behavior vector size
		BehaviorChangeDetection:    true, // Enable change detection

		// Temporal correlation for real-time processing
		TemporalWindow:             cfg.Correlation.AnalysisWindow,
		PatternDetectionWindow:     time.Hour,
		PeriodicityDetectionWindow: 24 * time.Hour,

		// Causality analysis for root cause detection
		CausalityDepth:           cfg.Correlation.MaxCorrelationDepth,
		CausalityConfidenceMin:   0.6,
		RootCauseAnalysisEnabled: true,

		// AI processing for future enhancement
		AIEnabled:              true,
		AIFeatureProcessing:    true,
		DenseFeatureDimension:  256,
		GraphFeatureProcessing: true,

		// Performance optimization for 500k+ events/sec
		MaxEventsInMemory:  cfg.Correlation.BufferSize,
		CorrelationWorkers: 8,     // Parallel workers
		PatternCacheSize:   10000, // Pattern cache
		EntityCacheSize:    50000, // Entity behavior cache
	}

	engine, err := correlation.NewPerfectEngine(perfectConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create perfect correlation engine: %w", err)
	}

	fmt.Printf("✅ Initialized perfect correlation engine optimized for opinionated data\n")
	fmt.Printf("   - Semantic similarity threshold: %.2f\n", perfectConfig.SemanticSimilarityThreshold)
	fmt.Printf("   - Behavioral anomaly threshold: %.2f\n", perfectConfig.BehavioralAnomalyThreshold)
	fmt.Printf("   - AI features enabled: %v\n", perfectConfig.AIEnabled)
	fmt.Printf("   - Causality chain depth: %d\n", perfectConfig.CausalityDepth)
	fmt.Printf("   - Correlation workers: %d\n", perfectConfig.CorrelationWorkers)

	return engine, nil
}

func initializeMetricsCollector(cfg *ServerConfig) (*metrics.Collector, error) {
	metricsConfig := metrics.Config{
		PrometheusEnabled:  cfg.Metrics.PrometheusEnabled,
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
	grpcConfig.Port = cfg.GRPCPort
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
	GRPCPort             int
	RESTPort             int
	RESTEnabled          bool
	GRPCEnabled          bool
	TLSEnabled           bool
	MaxConcurrentStreams uint32
	MaxEventsPerSec      uint32
	MaxBatchSize         uint32

	Correlation CorrelationConfig
	Metrics     MetricsConfig
	Resources   ResourceConfig
	REST        RESTConfig
}

type CorrelationConfig struct {
	Enabled             bool
	BufferSize          int
	AnalysisWindow      time.Duration
	MaxCorrelationDepth int
}

type MetricsConfig struct {
	PrometheusEnabled  bool
	PrometheusPort     int
	CollectionInterval time.Duration
}

type ResourceConfig struct {
	MaxMemoryMB int
	MaxCPUMilli int
}

type RESTConfig struct {
	Enabled      bool          `mapstructure:"enabled"`
	Port         int           `mapstructure:"port"`
	Host         string        `mapstructure:"host"`
	EnableCORS   bool          `mapstructure:"enable_cors"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

// EventProcessor integrates with correlation engine and metrics
type EventProcessor struct {
	engine           *correlation.Engine
	metricsCollector *metrics.Collector

	// Statistics
	eventsProcessed  uint64
	batchesProcessed uint64
	processingErrors uint64
}

func NewEventProcessor(engine *correlation.Engine, metricsCollector *metrics.Collector) *EventProcessor {
	return &EventProcessor{
		engine:           engine,
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
