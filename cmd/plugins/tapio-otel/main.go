package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/yairfalse/tapio/pkg/plugins"
	"github.com/yairfalse/tapio/pkg/exports/plugins" // Legacy import for existing OTEL plugin
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
		Use:   "tapio-otel",
		Short: "OpenTelemetry export plugin for Tapio",
		Long: `Tapio OpenTelemetry Plugin exports observability data to OpenTelemetry collectors.

This plugin supports:
- Traces export via gRPC/HTTP
- Metrics export with custom attributes
- Resource detection and labeling
- Batch processing for performance
- Automatic reconnection with backoff`,
		Version: version,
		RunE:    runPlugin,
	}

	// Command-line flags
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "Path to configuration file")
	rootCmd.PersistentFlags().StringVar(&endpoint, "endpoint", "localhost:4317", "OTEL collector endpoint")
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Enable debug mode")

	// Environment variable binding
	viper.SetEnvPrefix("TAPIO_OTEL")
	viper.AutomaticEnv()

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runPlugin(cmd *cobra.Command, args []string) error {
	fmt.Printf("🚀 Starting Tapio OTEL Plugin v%s\n", version)

	// Create plugin using new SDK
	plugin := NewOTELPlugin()

	// Create plugin config
	config := plugins.Config{
		"endpoint":     endpoint,
		"service_name": "tapio-otel-plugin",
		"debug":        debug,
	}

	// Initialize plugin with config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := plugin.Initialize(ctx, config); err != nil {
		return fmt.Errorf("failed to initialize plugin: %w", err)
	}

	// Setup signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		fmt.Printf("Received signal %v, shutting down...\n", sig)
		cancel()
	}()

	// Start plugin
	if err := plugin.Start(ctx); err != nil {
		return fmt.Errorf("failed to start plugin: %w", err)
	}

	fmt.Printf("✅ OTEL Plugin started successfully\n")
	fmt.Printf("   Endpoint: %s\n", endpoint)
	fmt.Printf("   Config: %s\n", configPath)

	// Plugin health check
	health, err := plugin.HealthCheck(ctx)
	if err != nil {
		fmt.Printf("⚠️  Health check failed: %v\n", err)
	} else {
		fmt.Printf("💚 Health: %s - %s\n", health.Status, health.Message)
	}

	// Wait for shutdown signal
	<-ctx.Done()

	// Stop plugin
	if err := plugin.Stop(ctx); err != nil {
		fmt.Printf("Warning: Error stopping plugin: %v\n", err)
	}

	fmt.Printf("🏁 OTEL Plugin stopped\n")
	return nil
}

// NewOTELPlugin creates a new OTEL plugin using the SDK
func NewOTELPlugin() plugins.ExportPlugin {
	return &OTELPlugin{
		BasePlugin: plugins.NewBasePlugin("tapio-otel", version, "OpenTelemetry export plugin for Tapio"),
		legacyPlugin: nil, // Will be initialized on Start
	}
}

// OTELPlugin wraps the legacy OTEL plugin with the new SDK
type OTELPlugin struct {
	*plugins.BasePlugin
	legacyPlugin *plugins.NewOTELExportPlugin
}

// Export implements the ExportPlugin interface
func (p *OTELPlugin) Export(ctx context.Context, data plugins.ExportData) error {
	if p.legacyPlugin == nil {
		return fmt.Errorf("legacy plugin not initialized")
	}
	
	// Convert new SDK export data to legacy format
	legacyData := convertToLegacyExportData(data)
	
	// Use legacy plugin for actual export
	return p.legacyPlugin.Export(ctx, legacyData)
}

// SupportedFormats implements the ExportPlugin interface
func (p *OTELPlugin) SupportedFormats() []plugins.ExportFormat {
	return []plugins.ExportFormat{plugins.FormatOTEL}
}

// SupportedDataTypes implements the ExportPlugin interface
func (p *OTELPlugin) SupportedDataTypes() []plugins.DataType {
	return []plugins.DataType{
		plugins.DataTypeEvents,
		plugins.DataTypeMetrics,
		plugins.DataTypeTraces,
		plugins.DataTypeCorrelation,
	}
}

// SupportsBatching implements the ExportPlugin interface
func (p *OTELPlugin) SupportsBatching() bool {
	return true
}

// GetBatchConfig implements the ExportPlugin interface
func (p *OTELPlugin) GetBatchConfig() *plugins.BatchConfig {
	return &plugins.BatchConfig{
		BatchSize:    100,
		BatchTimeout: 5 * time.Second,
		MaxQueueSize: 1000,
	}
}

// Start overrides BasePlugin.Start to initialize legacy plugin
func (p *OTELPlugin) Start(ctx context.Context) error {
	// Call base plugin start
	if err := p.BasePlugin.Start(ctx); err != nil {
		return err
	}
	
	// Initialize legacy plugin
	p.legacyPlugin = plugins.NewOTELExportPlugin()
	
	// Configure legacy plugin with SDK config
	legacyConfig := convertToLegacyConfig(p.SDK().GetConfig())
	if err := p.legacyPlugin.Configure(legacyConfig); err != nil {
		return fmt.Errorf("failed to configure legacy plugin: %w", err)
	}
	
	// Start legacy plugin
	if err := p.legacyPlugin.Start(ctx); err != nil {
		return fmt.Errorf("failed to start legacy plugin: %w", err)
	}
	
	p.SDK().Log().Info("OTEL plugin started with legacy backend")
	return nil
}

// Stop overrides BasePlugin.Stop to stop legacy plugin
func (p *OTELPlugin) Stop(ctx context.Context) error {
	// Stop legacy plugin first
	if p.legacyPlugin != nil {
		if err := p.legacyPlugin.Stop(ctx); err != nil {
			p.SDK().Log().Error("Failed to stop legacy plugin: %v", err)
		}
	}
	
	// Call base plugin stop
	return p.BasePlugin.Stop(ctx)
}

// Helper functions for legacy compatibility

func convertToLegacyExportData(data plugins.ExportData) exports.ExportData {
	// Convert new SDK export data to legacy format
	return exports.ExportData{
		Type:      exports.DataType(data.Type),
		Format:    exports.ExportFormat(data.Format),
		Content:   data.Content,
		Metadata:  data.Metadata,
		Timestamp: data.Timestamp,
		Source:    data.Source,
		Tags:      data.Tags,
		Callback:  convertCallback(data.Callback),
	}
}

func convertCallback(cb func(*plugins.ExportResult)) func(*exports.ExportResult) {
	if cb == nil {
		return nil
	}
	return func(result *exports.ExportResult) {
		newResult := &plugins.ExportResult{
			Success:  result.Success,
			Error:    result.Error,
			Duration: result.Duration,
			Details:  result.Details,
		}
		cb(newResult)
	}
}

func convertToLegacyConfig(config plugins.Config) map[string]interface{} {
	// Convert new SDK config to legacy format
	legacyConfig := make(map[string]interface{})
	for k, v := range config {
		legacyConfig[k] = v
	}
	return legacyConfig
}