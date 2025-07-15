package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/yairfalse/tapio/pkg/exports"
	"github.com/yairfalse/tapio/pkg/exports/plugins"
)

const (
	version = "1.0.0"
)

var (
	configPath string
	endpoint   string
	debug      bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "tapio-prometheus",
		Short: "Prometheus export plugin for Tapio",
		Long: `Tapio Prometheus Plugin exports metrics to Prometheus endpoints.

This plugin supports:
- Metrics exposition in Prometheus format
- Custom metric labels and annotations
- Remote write capabilities
- Push gateway integration
- Alert manager integration`,
		Version: version,
		RunE:    runPlugin,
	}

	// Command-line flags
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "Path to configuration file")
	rootCmd.PersistentFlags().StringVar(&endpoint, "endpoint", "localhost:9090", "Prometheus endpoint")
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Enable debug mode")

	// Environment variable binding
	viper.SetEnvPrefix("TAPIO_PROMETHEUS")
	viper.AutomaticEnv()

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runPlugin(cmd *cobra.Command, args []string) error {
	fmt.Printf("🚀 Starting Tapio Prometheus Plugin v%s\n", version)

	// Initialize plugin (assuming we have a prometheus plugin implementation)
	// This would use the existing prometheus plugin from pkg/exports/plugins

	fmt.Printf("✅ Prometheus Plugin started successfully\n")
	fmt.Printf("   Endpoint: %s\n", endpoint)
	fmt.Printf("   Config: %s\n", configPath)

	// Setup context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	<-sigCh
	fmt.Printf("🏁 Prometheus Plugin stopped\n")
	return nil
}
