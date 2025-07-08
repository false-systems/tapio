package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	
	"github.com/falseyair/tapio/pkg/ebpf"
	"github.com/falseyair/tapio/pkg/metrics"
	"github.com/falseyair/tapio/pkg/simple"
)

var (
	metricsAddr     string
	updateInterval  time.Duration
	enableEBPF      bool
)

var prometheusCmd = &cobra.Command{
	Use:   "prometheus",
	Short: "Start Prometheus metrics exporter",
	Long: `Start a Prometheus metrics server that exports Tapio health and prediction metrics.

The server continuously monitors your cluster using Tapio's intelligence engine
and provides metrics that can be scraped by Prometheus for alerting and dashboards.

Features:
  • OOM prediction metrics with precise timing
  • eBPF kernel-level insights (requires root)
  • Pod health status and cluster health scores
  • Memory leak detection and growth rate tracking
  • Zero-configuration auto-discovery`,
	Example: `  # Start metrics server on default port
  tapio prometheus

  # Start on custom port with faster updates  
  tapio prometheus --addr :9090 --interval 10s

  # Run with eBPF monitoring (requires root)
  sudo tapio prometheus --addr :8080

  # Test the metrics
  curl http://localhost:8080/metrics | grep tapio`,
	RunE: runPrometheus,
}

func init() {
	prometheusCmd.Flags().StringVar(&metricsAddr, "addr", ":8080", 
		"Address to listen on for metrics HTTP server")
	prometheusCmd.Flags().DurationVar(&updateInterval, "interval", 30*time.Second, 
		"How often to update metrics by scanning the cluster")
	prometheusCmd.Flags().BoolVar(&enableEBPF, "ebpf", true, 
		"Enable eBPF kernel monitoring (requires root privileges)")
}

func runPrometheus(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("🌲 Starting Tapio Prometheus Exporter...")

	// Create checker - try enhanced first, fall back to simple
	var checker metrics.CheckerInterface
	var collector interface{}
	
	if enableEBPF {
		// Try to create enhanced checker with eBPF
		enhancedChecker, err := simple.NewEnhancedChecker()
		if err == nil {
			checker = enhancedChecker
			if hasEBPF := enhancedChecker.HasEBPF(); hasEBPF {
				collector = enhancedChecker.GetEBPFCollector()
				fmt.Println("✅ eBPF kernel monitoring enabled for metrics")
			} else {
				fmt.Println("⚠️  eBPF not available - metrics will use Kubernetes API only")
			}
			defer enhancedChecker.Close()
		} else {
			fmt.Printf("⚠️  Enhanced checker unavailable: %v\n", err)
			fmt.Println("   Falling back to simple checker...")
		}
	}
	
	// Fall back to simple checker if enhanced is not available
	if checker == nil {
		simpleChecker, err := simple.NewChecker()
		if err != nil {
			return fmt.Errorf("failed to create checker: %w", err)
		}
		checker = simpleChecker
		fmt.Println("ℹ️  Using simple checker - Kubernetes API metrics only")
	}
	
	// Create Prometheus exporter
	var ebpfCollector *ebpf.Collector
	if c, ok := collector.(*ebpf.Collector); ok {
		ebpfCollector = c
	}
	
	concreteChecker, ok := checker.(*simple.Checker)
	if !ok {
		return fmt.Errorf("checker must be a *simple.Checker")
	}
	exporter := metrics.NewPrometheusExporter(concreteChecker, ebpfCollector)

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