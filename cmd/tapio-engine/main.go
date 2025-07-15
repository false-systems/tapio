package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/yairfalse/tapio/pkg/di"
)

const (
	// Version information
	version = "1.0.0"

	// Default configuration
	defaultConfigPath = "/etc/tapio/server.yaml"
	defaultPort       = 9090
	defaultAddress    = "0.0.0.0"

	// Resource limits (Deployment pattern)
	defaultMaxMemoryMB = 500 // Higher for server processing
	defaultMaxCPUMilli = 500 // 50% CPU
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
		Use:   "tapio-engine",
		Short: "Central correlation engine for Tapio observability platform",
		Long: `Tapio Engine is the central correlation engine that processes streaming data from collectors, performs intelligent correlation analysis, and provides observability insights.

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
	viper.SetEnvPrefix("TAPIO_ENGINE")
	viper.AutomaticEnv()

	// Execute command
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runServer(cmd *cobra.Command, args []string) error {
	fmt.Printf("🚀 Starting Tapio Engine v%s\n", version)
	fmt.Printf("   Port: %d\n", port)
	fmt.Printf("   Address: %s\n", address)
	fmt.Printf("   Config: %s\n", configPath)
	fmt.Printf("   Using dependency injection architecture\n")

	// Create application with DI container
	app := di.NewEngineApplication()

	// Run application (includes signal handling and graceful shutdown)
	if err := app.Run(); err != nil {
		return fmt.Errorf("failed to run engine application: %w", err)
	}

	return nil
}

// Legacy functions removed - now handled by DI modules
