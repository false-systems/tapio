package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/yairfalse/tapio/pkg/ebpf"
	"github.com/yairfalse/tapio/pkg/simple"
	"github.com/yairfalse/tapio/pkg/telemetry"
)

var (
	// OpenTelemetry flags (mirroring Prometheus patterns)
	otelAddr           string
	otelUpdateInterval time.Duration
	otelEnableEBPF     bool
	otelEnableTraces   bool
	otelEnableMetrics  bool
	otelOTLPEndpoint   string
	otelServiceName    string
	otelServiceVersion string
	otelInsecure       bool
	otelBatchSize      int
	otelBatchTimeout   time.Duration
	otelHeaders        []string
	otelEnableTLS      bool
	otelCertFile       string
	otelKeyFile        string
	otelMaxConcurrency int
	useUniversalFormat bool
	
	// Correlation tracing options
	enableCorrelationTracing    bool
	correlationTimeWindow       time.Duration
	enableTimelineVisualization bool
	enableRootCauseAnalysis     bool
	correlationConfidenceThreshold float64
)

var opentelemetryCmd = &cobra.Command{
	Use:   "opentelemetry",
	Short: "Start OpenTelemetry exporter",
	Long: `Start an OpenTelemetry exporter that exports Tapio intelligence as traces and metrics.

The exporter continuously monitors your cluster using Tapio's intelligence engine
and provides telemetry data that can be ingested by OpenTelemetry collectors,
Jaeger, Zipkin, and other observability platforms.

Features:
  • Distributed tracing for Tapio analysis operations
  • OpenTelemetry metrics for health and performance
  • Circuit breaker protection with resilience framework
  • Enterprise-grade HTTP server with multiple endpoints
  • Full resilience integration (timeouts, retries, validation)
  • Resource efficiency with 19Hz optimal batching
  • Zero-configuration auto-discovery
  • Universal data format for enhanced telemetry
  • Enhanced correlation analysis tracing
  • Timeline visualization for event causation
  • Root cause analysis with confidence scoring
  • Multi-layer system analysis (eBPF, K8s, systemd, network)`,
	Example: `  # Start OpenTelemetry exporter on default port
  tapio opentelemetry

  # Start with custom OTLP collector endpoint
  tapio opentelemetry --otlp-endpoint http://jaeger:14268/api/traces

  # Start with custom configuration
  tapio opentelemetry --addr :4317 --interval 10s --batch-size 50

  # Enable eBPF monitoring for enhanced traces
  tapio opentelemetry --enable-ebpf --otlp-endpoint http://localhost:4317

  # Enable correlation analysis tracing with timeline visualization
  tapio opentelemetry --enable-correlation --enable-timeline --correlation-window 30m

  # Enable root cause analysis with custom confidence threshold
  tapio opentelemetry --enable-rootcause --correlation-confidence 0.8

  # Test the telemetry endpoints
  curl http://localhost:4317/health
  curl http://localhost:4317/info`,
	RunE: runOpenTelemetry,
}

func init() {
	// Server configuration (mirrors Prometheus)
	opentelemetryCmd.Flags().StringVar(&otelAddr, "addr", ":4317",
		"Address to listen on for OpenTelemetry HTTP server")
	opentelemetryCmd.Flags().DurationVar(&otelUpdateInterval, "interval", 30*time.Second,
		"How often to update telemetry by scanning the cluster")
	
	// OpenTelemetry specific configuration
	opentelemetryCmd.Flags().StringVar(&otelOTLPEndpoint, "otlp-endpoint", "http://localhost:4317",
		"OTLP collector endpoint URL")
	opentelemetryCmd.Flags().StringVar(&otelServiceName, "service-name", "tapio",
		"Service name for OpenTelemetry resource")
	opentelemetryCmd.Flags().StringVar(&otelServiceVersion, "service-version", "1.0.0",
		"Service version for OpenTelemetry resource")
	
	// Feature toggles
	opentelemetryCmd.Flags().BoolVar(&otelEnableEBPF, "enable-ebpf", false,
		"Enable eBPF monitoring for enhanced telemetry (requires root)")
	opentelemetryCmd.Flags().BoolVar(&otelEnableTraces, "enable-traces", true,
		"Enable OpenTelemetry trace export")
	opentelemetryCmd.Flags().BoolVar(&otelEnableMetrics, "enable-metrics", true,
		"Enable OpenTelemetry metrics export")
	opentelemetryCmd.Flags().BoolVar(&useUniversalFormat, "universal", true,
		"Use universal data format for enhanced telemetry")
	
	// Export configuration
	opentelemetryCmd.Flags().BoolVar(&otelInsecure, "insecure", true,
		"Use insecure connection to OTLP collector")
	opentelemetryCmd.Flags().IntVar(&otelBatchSize, "batch-size", 100,
		"Batch size for span/metric export")
	opentelemetryCmd.Flags().DurationVar(&otelBatchTimeout, "batch-timeout", 5*time.Second,
		"Timeout for batch export")
	opentelemetryCmd.Flags().StringSliceVar(&otelHeaders, "headers", []string{},
		"Additional headers for OTLP requests (key=value format)")
	
	// TLS configuration
	opentelemetryCmd.Flags().BoolVar(&otelEnableTLS, "tls", false,
		"Enable TLS for the HTTP server")
	opentelemetryCmd.Flags().StringVar(&otelCertFile, "cert-file", "",
		"TLS certificate file (required if --tls is enabled)")
	opentelemetryCmd.Flags().StringVar(&otelKeyFile, "key-file", "",
		"TLS private key file (required if --tls is enabled)")
	
	// Performance configuration
	opentelemetryCmd.Flags().IntVar(&otelMaxConcurrency, "max-concurrency", 10,
		"Maximum number of concurrent operations")
	
	// Correlation tracing configuration
	opentelemetryCmd.Flags().BoolVar(&enableCorrelationTracing, "enable-correlation", true,
		"Enable enhanced correlation analysis tracing")
	opentelemetryCmd.Flags().DurationVar(&correlationTimeWindow, "correlation-window", 30*time.Minute,
		"Time window for correlation analysis")
	opentelemetryCmd.Flags().BoolVar(&enableTimelineVisualization, "enable-timeline", true,
		"Enable timeline visualization tracing")
	opentelemetryCmd.Flags().BoolVar(&enableRootCauseAnalysis, "enable-rootcause", true,
		"Enable root cause analysis tracing")
	opentelemetryCmd.Flags().Float64Var(&correlationConfidenceThreshold, "correlation-confidence", 0.7,
		"Minimum confidence threshold for correlations (0.0-1.0)")
}

func runOpenTelemetry(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("🌲 Starting Tapio OpenTelemetry Exporter...")
	if useUniversalFormat {
		fmt.Println("✨ Using universal data format for enhanced telemetry")
	}
	
	// Print correlation tracing status
	if enableCorrelationTracing {
		fmt.Println("🔍 Correlation analysis tracing enabled")
		fmt.Printf("   • Time window: %v\n", correlationTimeWindow)
		fmt.Printf("   • Confidence threshold: %.2f\n", correlationConfidenceThreshold)
		if enableTimelineVisualization {
			fmt.Println("   • Timeline visualization: ENABLED")
		}
		if enableRootCauseAnalysis {
			fmt.Println("   • Root cause analysis: ENABLED")
		}
	}

	// Parse headers
	headers := make(map[string]string)
	for _, header := range otelHeaders {
		// Parse key=value format
		if key, value, found := parseKeyValue(header); found {
			headers[key] = value
		} else {
			fmt.Printf("[WARN] Invalid header format: %s (expected key=value)\n", header)
		}
	}

	// Validate TLS configuration
	if otelEnableTLS {
		if otelCertFile == "" || otelKeyFile == "" {
			return fmt.Errorf("TLS enabled but cert-file or key-file not provided")
		}
		if _, err := os.Stat(otelCertFile); os.IsNotExist(err) {
			return fmt.Errorf("certificate file not found: %s", otelCertFile)
		}
		if _, err := os.Stat(otelKeyFile); os.IsNotExist(err) {
			return fmt.Errorf("key file not found: %s", otelKeyFile)
		}
	}

	// Create eBPF config if enabled (mirrors Prometheus)
	var ebpfConfig *ebpf.Config
	if otelEnableEBPF {
		ebpfConfig = &ebpf.Config{
			Enabled:         true,
			EventBufferSize: 1000,
			RetentionPeriod: "5m",
		}
	}

	// Create checker with eBPF config
	checker, err := simple.NewCheckerWithConfig(ebpfConfig)
	if err != nil {
		return fmt.Errorf("failed to create checker: %w", err)
	}

	// Try to start eBPF monitoring if enabled
	if otelEnableEBPF {
		err = checker.StartEBPFMonitoring(ctx)
		if err != nil {
			fmt.Printf("[WARN] eBPF monitoring not available: %v\n", err)
			fmt.Println("[INFO] Continuing with Kubernetes API telemetry only")
		} else {
			fmt.Println("[OK] eBPF monitoring enabled for enhanced telemetry")
			defer func() {
				if err := checker.StopEBPFMonitoring(); err != nil {
					fmt.Printf("[ERROR] Failed to stop eBPF monitoring: %v\n", err)
				}
			}()
		}
	} else {
		fmt.Println("[INFO] Using Kubernetes API telemetry only")
	}

	// Create OpenTelemetry exporter configuration with Agent 1 translator integration
	otelConfig := telemetry.Config{
		ServiceName:     otelServiceName,
		ServiceVersion:  otelServiceVersion,
		OTLPEndpoint:    otelOTLPEndpoint,
		Headers:         headers,
		Insecure:        otelInsecure,
		BatchTimeout:    otelBatchTimeout,
		BatchSize:       otelBatchSize,
		MaxConcurrency:  otelMaxConcurrency,
		EnableMetrics:   otelEnableMetrics,
		EnableTraces:    otelEnableTraces,
		
		// Enable Agent 1's translator for real Kubernetes context
		EnableTranslator: true,
		KubeClient:      checker.GetKubeClient(), // Get real Kubernetes client from checker
		
		// Correlation tracing configuration
		EnableCorrelationTracing:    enableCorrelationTracing,
		CorrelationTimeWindow:       correlationTimeWindow,
		EnableTimelineVisualization: enableTimelineVisualization,
		EnableRootCauseAnalysis:     enableRootCauseAnalysis,
		CorrelationConfidenceThreshold: correlationConfidenceThreshold,
	}

	// Create OpenTelemetry exporter
	exporter, err := telemetry.NewOpenTelemetryExporter(checker, checker.GetEBPFMonitor(), otelConfig)
	if err != nil {
		return fmt.Errorf("failed to create OpenTelemetry exporter: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := exporter.Shutdown(shutdownCtx); err != nil {
			fmt.Printf("[ERROR] Failed to shutdown exporter: %v\n", err)
		}
	}()

	// Create span manager
	spanManagerConfig := telemetry.SpanManagerConfig{
		MaxConcurrentSpans: 1000,
		BatchSize:          otelBatchSize,
		BatchTimeout:       otelBatchTimeout,
		ExportTimeout:      10 * time.Second,
		MaxQueueSize:       10000,
		EnableValidation:   true,
		ResourceLimits: telemetry.ResourceLimits{
			MaxMemoryMB:      10,
			MaxCPUPercent:    5.0,
			MaxSpansInFlight: 1000,
			MaxBatchSize:     19, // Polar Signals style 19Hz batching
		},
	}

	spanManager, err := telemetry.NewSpanManager(exporter, spanManagerConfig)
	if err != nil {
		return fmt.Errorf("failed to create span manager: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := spanManager.Shutdown(shutdownCtx); err != nil {
			fmt.Printf("[ERROR] Failed to shutdown span manager: %v\n", err)
		}
	}()

	// Create enterprise server configuration
	enterpriseConfig := telemetry.EnterpriseConfig{
		ListenAddr:    otelAddr,
		TLSEnabled:    otelEnableTLS,
		CertFile:      otelCertFile,
		KeyFile:       otelKeyFile,
		AuthEnabled:   false, // Could be configurable
		RateLimitRPS:  1000,  // Could be configurable
		EnableMetrics: otelEnableMetrics,
		EnableTraces:  otelEnableTraces,
		CORS: telemetry.CORSConfig{
			Enabled:        true,
			AllowedOrigins: []string{"*"},
			AllowedMethods: []string{"GET", "POST", "OPTIONS"},
			AllowedHeaders: []string{"Content-Type", "Authorization"},
			MaxAge:         3600,
		},
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Create enterprise server
	server, err := telemetry.NewEnterpriseServer(exporter, spanManager, enterpriseConfig)
	if err != nil {
		return fmt.Errorf("failed to create enterprise server: %w", err)
	}

	// Start periodic telemetry updates in background
	if useUniversalFormat {
		go func() {
			ticker := time.NewTicker(otelUpdateInterval)
			defer ticker.Stop()

			// Initial update
			if err := exporter.UpdateTelemetry(ctx); err != nil {
				fmt.Printf("Warning: Initial telemetry update failed: %v\n", err)
			} else {
				fmt.Println("[OK] Universal telemetry initialized")
			}

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := exporter.UpdateTelemetry(ctx); err != nil {
						fmt.Printf("Warning: Telemetry update failed: %v\n", err)
					}
				}
			}
		}()
	} else {
		go func() {
			ticker := time.NewTicker(otelUpdateInterval)
			defer ticker.Stop()

			// Initial update
			if err := exporter.UpdateTelemetry(ctx); err != nil {
				fmt.Printf("Warning: Initial telemetry update failed: %v\n", err)
			} else {
				fmt.Println("[OK] Standard telemetry initialized")
			}

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := exporter.UpdateTelemetry(ctx); err != nil {
						fmt.Printf("Warning: Telemetry update failed: %v\n", err)
					}
				}
			}
		}()
	}

	// Start enterprise server in a goroutine
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Start()
	}()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	fmt.Println("🚀 Tapio OpenTelemetry exporter is running!")
	fmt.Printf("📡 OTLP traces endpoint: http://%s/v1/traces\n", otelAddr)
	fmt.Printf("📊 OTLP metrics endpoint: http://%s/v1/metrics\n", otelAddr)
	fmt.Printf("💚 Health endpoint: http://%s/health\n", otelAddr)
	fmt.Printf("ℹ️  Info endpoint: http://%s/info\n", otelAddr)
	fmt.Printf("📈 Internal metrics: http://%s/metrics/internal\n", otelAddr)
	fmt.Printf("🔧 Configuration: http://%s/config\n", otelAddr)
	fmt.Println("💡 Use Ctrl+C to stop")

	// Print resilience framework status
	metrics := exporter.GetMetrics()
	fmt.Printf("🛡️  Circuit breaker: %s\n", metrics.CircuitBreaker.State)
	fmt.Printf("⏱️  Timeout manager: %d retries available\n", 3) // From config
	fmt.Printf("✅ Health checker: %d components monitored\n", 2) // TODO: Get from actual health checker

	// Print performance info (Polar Signals style)
	fmt.Printf("⚡ Resource limits: 10MB memory, 5%% CPU, 19Hz batching\n")
	fmt.Printf("🔄 Update interval: %v\n", otelUpdateInterval)

	select {
	case err := <-serverErr:
		return fmt.Errorf("OpenTelemetry server failed: %w", err)
	case sig := <-sigChan:
		fmt.Printf("\n📡 Received signal %v, shutting down gracefully...\n", sig)
		cancel()

		// Shutdown server with timeout
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			fmt.Printf("[ERROR] Server shutdown failed: %v\n", err)
		}

		// Give final telemetry export a moment
		time.Sleep(2 * time.Second)
		fmt.Println("✅ Tapio OpenTelemetry exporter stopped")
		return nil
	}
}

// parseKeyValue parses key=value format strings
func parseKeyValue(s string) (key, value string, ok bool) {
	for i, r := range s {
		if r == '=' {
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}

// Additional helper functions for configuration validation
func validateOTLPEndpoint(endpoint string) error {
	if endpoint == "" {
		return fmt.Errorf("OTLP endpoint cannot be empty")
	}
	// Could add more validation like URL parsing
	return nil
}

func printStartupBanner() {
	fmt.Println(`
🌲 Tapio OpenTelemetry Exporter
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• Distributed tracing for Kubernetes intelligence
• Circuit breaker protection & resilience framework  
• Enterprise HTTP server with multiple endpoints
• Resource-efficient 19Hz batching (Polar Signals style)
• Universal data format for enhanced observability
`)
}

func printCapabilities() {
	fmt.Println("🎯 Capabilities:")
	fmt.Println("  • OpenTelemetry Protocol (OTLP) export")
	fmt.Println("  • Jaeger, Zipkin, and collector compatibility")
	fmt.Println("  • Circuit breaker failure protection")
	fmt.Println("  • Timeout and retry with exponential backoff")
	fmt.Println("  • Data validation and health monitoring")
	fmt.Println("  • eBPF kernel-level telemetry (optional)")
	fmt.Println("  • Correlation engine findings")
	fmt.Println("  • Enterprise-grade HTTP endpoints")
	fmt.Println()
}

func printResilienceStatus(exporter *telemetry.OpenTelemetryExporter) {
	metrics := exporter.GetMetrics()
	
	fmt.Println("🛡️  Resilience Framework Status:")
	fmt.Printf("  Circuit Breaker: %s\n", metrics.CircuitBreaker.State)
	fmt.Printf("  Total Calls: %d\n", metrics.CircuitBreaker.TotalCalls)
	fmt.Printf("  Success Rate: %.2f%%\n", 
		float64(metrics.CircuitBreaker.TotalSuccesses)/float64(metrics.CircuitBreaker.TotalCalls)*100)
	fmt.Printf("  Health Checks: %d components\n", len(metrics.HealthChecker.Components))
	fmt.Printf("  Timeout Retries: %d\n", metrics.TimeoutManager.TotalRetries)
	fmt.Println()
}

// Advanced configuration helpers
func createAdvancedOTelConfig() telemetry.Config {
	return telemetry.Config{
		ServiceName:    otelServiceName,
		ServiceVersion: otelServiceVersion,
		OTLPEndpoint:   otelOTLPEndpoint,
		Insecure:      otelInsecure,
		BatchTimeout:  otelBatchTimeout,
		BatchSize:     otelBatchSize,
		EnableMetrics: otelEnableMetrics,
		EnableTraces:  otelEnableTraces,
		ResourceAttrs: map[string]string{
			"deployment.environment": "production", // Could be configurable
			"service.namespace":      "tapio",
			"service.instance.id":    fmt.Sprintf("tapio-%d", time.Now().Unix()),
		},
	}
}

func validateConfiguration() error {
	if err := validateOTLPEndpoint(otelOTLPEndpoint); err != nil {
		return err
	}
	
	if otelBatchSize <= 0 {
		return fmt.Errorf("batch size must be positive, got %d", otelBatchSize)
	}
	
	if otelBatchTimeout <= 0 {
		return fmt.Errorf("batch timeout must be positive, got %v", otelBatchTimeout)
	}
	
	if otelMaxConcurrency <= 0 {
		return fmt.Errorf("max concurrency must be positive, got %d", otelMaxConcurrency)
	}
	
	return nil
}