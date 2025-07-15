package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/yairfalse/tapio/pkg/di"
	"github.com/yairfalse/tapio/pkg/relay"
	"go.uber.org/zap"
)

var (
	cfgFile string
	logger  *zap.Logger
)

var rootCmd = &cobra.Command{
	Use:   "tapio-relay",
	Short: "Tapio Relay - Intelligent event aggregation and routing",
	Long: `Tapio Relay acts as an intelligent aggregation layer between collectors and consumers.
	
Features:
- High-performance event buffering and batching
- Intelligent routing to multiple destinations
- Native OTEL export for enterprise observability
- Event aggregation and pattern detection
- Zero-configuration with sensible defaults`,
	RunE: runRelay,
}

func init() {
	cobra.OnInitialize(initConfig)
	
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is /etc/tapio/relay.yaml)")
	
	// Server flags
	rootCmd.Flags().Int("port", 9095, "gRPC port for collectors")
	rootCmd.Flags().String("engine", "localhost:9090", "Tapio Engine endpoint")
	
	// OTEL flags
	rootCmd.Flags().Bool("otel", true, "Enable OTEL export")
	rootCmd.Flags().String("otel-endpoint", "localhost:4317", "OTEL collector endpoint")
	
	// Performance flags
	rootCmd.Flags().Int("buffer-size", 100000, "Event buffer size")
	rootCmd.Flags().Int("batch-size", 1000, "Batch size for processing")
	rootCmd.Flags().Duration("flush-interval", 1, "Flush interval in seconds")
	
	// Bind flags to viper
	viper.BindPFlags(rootCmd.Flags())
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.AddConfigPath("/etc/tapio")
		viper.AddConfigPath(".")
		viper.SetConfigName("relay")
	}
	
	viper.SetEnvPrefix("TAPIO_RELAY")
	viper.AutomaticEnv()
	
	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("Using config file:", viper.ConfigFileUsed())
	}
}

func runRelay(cmd *cobra.Command, args []string) error {
	// Initialize logger
	var err error
	logger, err = zap.NewProduction()
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}
	defer logger.Sync()
	
	// Create configuration
	config := &relay.Config{
		GRPCPort:          viper.GetInt("port"),
		EngineEndpoint:    viper.GetString("engine"),
		OTELEnabled:       viper.GetBool("otel"),
		OTELEndpoint:      viper.GetString("otel-endpoint"),
		BufferSize:        viper.GetInt("buffer-size"),
		BatchSize:         viper.GetInt("batch-size"),
		FlushInterval:     viper.GetDuration("flush-interval"),
		AggregationWindow: viper.GetDuration("aggregation-window"),
	}
	
	logger.Info("Starting Tapio Relay",
		zap.Int("port", config.GRPCPort),
		zap.String("engine", config.EngineEndpoint),
		zap.Bool("otel", config.OTELEnabled),
	)
	
	// Create DI container
	container := di.NewContainer()
	container.Set("logger", logger)
	container.Set("relay.config", config)
	
	// Register modules
	modules := []di.Module{
		di.NewCoreModule(),
		di.NewRelayModule(),
	}
	
	for _, module := range modules {
		if err := module.Configure(container); err != nil {
			return fmt.Errorf("failed to configure module %s: %w", module.ID(), err)
		}
		if err := module.Provide(); err != nil {
			return fmt.Errorf("failed to provide module %s: %w", module.ID(), err)
		}
	}
	
	// Start modules
	ctx := context.Background()
	for _, module := range modules {
		if err := module.Start(ctx); err != nil {
			return fmt.Errorf("failed to start module %s: %w", module.ID(), err)
		}
	}
	
	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	
	logger.Info("Tapio Relay is running. Press Ctrl+C to stop.")
	<-sigChan
	
	logger.Info("Shutting down Tapio Relay...")
	
	// Stop modules in reverse order
	for i := len(modules) - 1; i >= 0; i-- {
		if err := modules[i].Stop(); err != nil {
			logger.Error("Failed to stop module", 
				zap.String("module", modules[i].ID()), 
				zap.Error(err))
		}
	}
	
	logger.Info("Tapio Relay stopped")
	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}