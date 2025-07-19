package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/yairfalse/tapio/pkg/collector"
	"github.com/yairfalse/tapio/pkg/config"
	"github.com/yairfalse/tapio/pkg/grpc"
	"github.com/yairfalse/tapio/pkg/monitoring"
	"github.com/yairfalse/tapio/pkg/shutdown"
)

const (
	// Version information
	version = "1.0.0"

	// Default configuration
	defaultConfigPath     = "/etc/tapio/collector.yaml"
	defaultServerEndpoint = "tapio-server:9090"

	// Resource limits (DaemonSet pattern)
	defaultMaxMemoryMB = 100
	defaultMaxCPUMilli = 10 // 1% CPU
)

var (
	configPath     string
	serverEndpoint string
	logLevel       string
	debug          bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "tapio-collector",
		Short: "Lightweight data collector for Tapio observability platform",
		Long: `Tapio Collector is a lightweight, resource-efficient data collection agent that runs as a DaemonSet on every Kubernetes node.

It collects system events, eBPF data, and Kubernetes metrics, then streams them to the central tapio-server for correlation and analysis.

Features:
- Pluggable collector architecture for different data sources
- Resource usage <100MB memory, <1% CPU under normal load
- Automatic reconnection with exponential backoff
- Graceful degradation when server is unavailable
- Hot-reload of configuration without restart`,
		Version: version,
		RunE:    runCollector,
	}

	// Command-line flags
	rootCmd.PersistentFlags().StringVar(&configPath, "config", defaultConfigPath, "Path to configuration file")
	rootCmd.PersistentFlags().StringVar(&serverEndpoint, "server", defaultServerEndpoint, "gRPC server endpoint")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Enable debug mode")

	// Environment variable binding
	viper.SetEnvPrefix("TAPIO_COLLECTOR")
	viper.AutomaticEnv()

	// Execute command
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runCollector(cmd *cobra.Command, args []string) error {
	// Initialize configuration
	cfg, err := loadConfiguration()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Setup context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize shutdown handler
	shutdownHandler := shutdown.NewHandler(30 * time.Second)
	defer shutdownHandler.Shutdown()

	// Setup signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		fmt.Printf("Received signal %v, initiating graceful shutdown...\n", sig)
		shutdownHandler.Start()
		cancel()
	}()

	// Initialize resource monitoring
	monitor := monitoring.NewResourceMonitor(monitoring.ResourceLimits{
		MaxMemoryMB: defaultMaxMemoryMB,
		MaxCPUMilli: defaultMaxCPUMilli,
	})
	shutdownHandler.Register("resource-monitor", func(ctx context.Context) error { return monitor.Shutdown() })

	// Initialize gRPC client for server communication
	grpcClient, err := initializeGRPCClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize gRPC client: %w", err)
	}
	shutdownHandler.Register("grpc-client", func(ctx context.Context) error { return grpcClient.Stop() })

	// Initialize collector manager
	collectorManager, err := initializeCollectorManager(cfg, grpcClient)
	if err != nil {
		return fmt.Errorf("failed to initialize collector manager: %w", err)
	}
	shutdownHandler.Register("collector-manager", func(ctx context.Context) error { return collectorManager.Stop() })

	// Start all components
	fmt.Printf("🚀 Starting Tapio Collector v%s\n", version)
	fmt.Printf("   Server endpoint: %s\n", serverEndpoint)
	fmt.Printf("   Config path: %s\n", configPath)
	fmt.Printf("   Resource limits: %dMB memory, %dm CPU\n", defaultMaxMemoryMB, defaultMaxCPUMilli)

	// Start resource monitoring
	if err := monitor.Start(ctx); err != nil {
		return fmt.Errorf("failed to start resource monitor: %w", err)
	}

	// Start gRPC client
	if err := grpcClient.Start(ctx); err != nil {
		return fmt.Errorf("failed to start gRPC client: %w", err)
	}

	// Start collector manager
	if err := collectorManager.Start(ctx); err != nil {
		return fmt.Errorf("failed to start collector manager: %w", err)
	}

	fmt.Printf("✅ Tapio Collector started successfully\n")

	// Setup periodic status reporting
	statusTicker := time.NewTicker(30 * time.Second)
	defer statusTicker.Stop()

	// Main run loop
	for {
		select {
		case <-ctx.Done():
			fmt.Printf("🛑 Collector shutdown initiated\n")
			return nil

		case <-statusTicker.C:
			printStatus(monitor, grpcClient, collectorManager)

		case <-shutdownHandler.Wait():
			fmt.Printf("🏁 Collector shutdown completed\n")
			return nil
		}
	}
}

func loadConfiguration() (*collector.Config, error) {
	// Set configuration defaults
	viper.SetDefault("collector.enabled_collectors", []string{"ebpf", "k8s", "systemd"})
	viper.SetDefault("collector.sampling_rate", 1.0)
	viper.SetDefault("collector.max_events_per_sec", 10000)
	viper.SetDefault("collector.buffer_size", 10000)

	viper.SetDefault("grpc.server_endpoints", []string{serverEndpoint})
	viper.SetDefault("grpc.tls_enabled", false)
	viper.SetDefault("grpc.max_batch_size", 100)
	viper.SetDefault("grpc.batch_timeout", "100ms")
	viper.SetDefault("grpc.reconnect_enabled", true)
	viper.SetDefault("grpc.max_reconnect_attempts", 10)

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
	cfg := &collector.Config{
		SamplingRate:      viper.GetFloat64("collector.sampling_rate"),
		MaxEventsPerSec:   viper.GetInt("collector.max_events_per_sec"),
		EnabledCollectors: viper.GetStringSlice("collector.enabled_collectors"),
	}

	// Override with command-line flags if provided
	if serverEndpoint != defaultServerEndpoint {
		// Server endpoint override would go here
	}

	return cfg, nil
}

func initializeGRPCClient(cfg *collector.Config) (*grpc.Client, error) {
	// Create gRPC client configuration
	grpcConfig := grpc.DefaultClientConfig()

	// Create node information for registration
	nodeInfo := &grpc.NodeInfo{
		NodeId:       os.Getenv("NODE_NAME"),
		Hostname:     getHostname(),
		Os:           getOS(),
		Architecture: getArchitecture(),
		Region:       getRegion(),
		Labels:       getNodeLabels(),
	}

	// Create and configure gRPC client
	grpcClient := grpc.NewClient(grpcConfig, nodeInfo)

	return collector.NewGRPCStreamingClient(grpcClient), nil
}

func initializeCollectorManager(cfg *collector.Config, grpcClient *grpc.Client) (*collector.SimpleManager, error) {
	// Create collector manager
	manager := collector.NewSimpleManager(collector.DefaultManagerConfig())

	// Register enabled collectors
	for _, collectorName := range cfg.EnabledCollectors {
		collector, err := collector.CreateCollector(collectorName, cfg)
		if err != nil {
			fmt.Printf("⚠️  Failed to create collector %s: %v\n", collectorName, err)
			continue
		}

		if err := manager.Register(collector); err != nil {
			fmt.Printf("⚠️  Failed to register collector %s: %v\n", collectorName, err)
			continue
		}

		fmt.Printf("✅ Registered collector: %s\n", collectorName)
	}

	return manager, nil
}

func printStatus(monitor *monitoring.ResourceMonitor, grpcClient *grpc.Client, manager *collector.SimpleManager) {
	// Get resource usage
	usage := monitor.GetUsage()

	// Get gRPC client status
	clientStats := grpcClient.GetStats()

	// Get collector health
	health := manager.GetHealth()

	fmt.Printf("📊 Status - Memory: %.1fMB, CPU: %.1f%%, Events/sec: %.1f, Connected: %v\n",
		usage.MemoryMB,
		usage.CPUPercent,
		clientStats.EventsPerSecond,
		clientStats.Connected,
	)

	// Print collector health summary
	healthyCount := 0
	for name, h := range health {
		if h.Status == collector.HealthStatusHealthy {
			healthyCount++
		} else {
			fmt.Printf("⚠️  Collector %s: %s - %s\n", name, h.Status, h.Message)
		}
	}

	if healthyCount == len(health) && len(health) > 0 {
		fmt.Printf("✅ All %d collectors healthy\n", healthyCount)
	}
}

// Utility functions for node information
func getHostname() string {
	if hostname, err := os.Hostname(); err == nil {
		return hostname
	}
	return "unknown"
}

func getOS() string {
	// This would be detected from runtime.GOOS
	return "linux"
}

func getArchitecture() string {
	// This would be detected from runtime.GOARCH
	return "amd64"
}

func getRegion() string {
	// This could be detected from cloud metadata or configuration
	return os.Getenv("AWS_REGION")
}

func getNodeLabels() map[string]string {
	labels := make(map[string]string)

	// Standard Kubernetes labels
	if podName := os.Getenv("POD_NAME"); podName != "" {
		labels["tapio.pod"] = podName
	}
	if namespace := os.Getenv("POD_NAMESPACE"); namespace != "" {
		labels["tapio.namespace"] = namespace
	}
	if nodeName := os.Getenv("NODE_NAME"); nodeName != "" {
		labels["tapio.node"] = nodeName
	}

	labels["tapio.component"] = "collector"
	labels["tapio.version"] = version

	return labels
}
