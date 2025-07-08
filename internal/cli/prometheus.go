package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	
	"github.com/falseyair/tapio/pkg/metrics"
	"github.com/falseyair/tapio/pkg/simple"
)

var (
	metricsAddr     string
	updateInterval  time.Duration
)

var prometheusCmd = &cobra.Command{
	Use:   "prometheus",
	Short: "Start Prometheus metrics exporter",
	Long: `Start a Prometheus metrics server that exports Tapio health and prediction metrics.

The server continuously monitors your cluster using Tapio's intelligence engine
and provides metrics that can be scraped by Prometheus for alerting and dashboards.

Features:
  • OOM prediction metrics with precise timing
  • Pod health status and cluster health scores
  • Memory leak detection and growth rate tracking
  • Zero-configuration auto-discovery`,
	Example: `  # Start metrics server on default port
  tapio prometheus

  # Start on custom port with faster updates  
  tapio prometheus --addr :9090 --interval 10s

  # Test the metrics
  curl http://localhost:8080/metrics | grep tapio`,
	RunE: runPrometheus,
}

func init() {
	prometheusCmd.Flags().StringVar(&metricsAddr, "addr", ":8080", 
		"Address to listen on for metrics HTTP server")
	prometheusCmd.Flags().DurationVar(&updateInterval, "interval", 30*time.Second, 
		"How often to update metrics by scanning the cluster")
}

func runPrometheus(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("🌲 Starting Tapio Prometheus Exporter...")

	// Create simple checker
	checker, err := simple.NewChecker()
	if err != nil {
		return fmt.Errorf("failed to create checker: %w", err)
	}
	fmt.Println("ℹ️  Using Kubernetes API metrics")
	
	// Create Prometheus exporter
	exporter := metrics.NewPrometheusExporter(checker, nil)

	// Start periodic metrics updates in background
	go exporter.StartPeriodicUpdates(ctx, updateInterval)

	// Start metrics server in a goroutine
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- exporter.StartMetricsServer(metricsAddr)
	}()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	fmt.Println("🚀 Tapio Prometheus exporter is running!")
	fmt.Printf("📊 Metrics available at: http://%s/metrics\n", metricsAddr)
	fmt.Println("💡 Use Ctrl+C to stop")

	select {
	case err := <-serverErr:
		return fmt.Errorf("metrics server failed: %w", err)
	case sig := <-sigChan:
		fmt.Printf("\n📡 Received signal %v, shutting down gracefully...\n", sig)
		cancel()
		
		// Give server a moment to finish current requests
		time.Sleep(1 * time.Second)
		fmt.Println("✅ Tapio Prometheus exporter stopped")
		return nil
	}
}